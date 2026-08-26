// Package processstats provides conservative analysis of PROC and SCOM events.
package processstats

import (
	"sort"
	"strings"
	"time"

	"techlog-stat/internal/stats"
	"techlog-stat/internal/techlog"
)

const missingValue = "(missing)"

// Options controls retained PROC raw samples. Zero values use safe defaults.
type Options struct {
	SampleLimit int `json:"sample_limit"`
}

// FieldQuality records incomplete input without making up process relations.
type FieldQuality struct {
	Missing   int64 `json:"missing"`
	Malformed int64 `json:"malformed"`
}

// Quality describes the event population and the conservative SCOM parser.
type Quality struct {
	IgnoredEvents               int64        `json:"ignored_events"`
	PROCEvents                  int64        `json:"proc_events"`
	SCOMEvents                  int64        `json:"scom_events"`
	Process                     FieldQuality `json:"process"`
	OSThread                    FieldQuality `json:"os_thread"`
	Source                      FieldQuality `json:"source"`
	Func                        FieldQuality `json:"func"`
	UnknownFunc                 int64        `json:"unknown_func"`
	KnownFuncMissingNames       int64        `json:"known_func_missing_names"`
	KnownFuncMalformedArguments int64        `json:"known_func_malformed_arguments"`
}

// EventMetrics reports occurrences and the duration encoded in the individual
// log-event header. EventDuration must not be interpreted as process uptime.
type EventMetrics struct {
	Occurrences   int64         `json:"occurrences"`
	EventDuration stats.Summary `json:"event_duration"`
}

// Aggregate is a deterministic aggregation by one named dimension.
type Aggregate struct {
	Key     string       `json:"key"`
	Metrics EventMetrics `json:"metrics"`
}

// SCOMAggregate is a composite SCOM operation/process/source grouping.
type SCOMAggregate struct {
	Operation string       `json:"operation"`
	Process   string       `json:"process"`
	Source    string       `json:"source"`
	Metrics   EventMetrics `json:"metrics"`
}

// ProcSample preserves one of the bounded slowest raw PROC events.
type ProcSample struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"`
	Process       string    `json:"process"`
	OSThread      string    `json:"os_thread"`
	EventDuration int64     `json:"event_duration"`
	Raw           string    `json:"raw"`
}

// ProcessRelation is emitted only for the two documented SCOM function
// shapes. It never infers names from arbitrary Func values or event fields.
type ProcessRelation struct {
	Operation              string `json:"operation"`
	Process                string `json:"process"`
	LogSource              string `json:"log_source"`
	SourceProcessName      string `json:"source_process_name"`
	DestinationProcessName string `json:"destination_process_name"`
	Occurrences            int64  `json:"occurrences"`
}

// Result is a deterministic snapshot of PROC and SCOM analysis.
type Result struct {
	Quality                      Quality           `json:"quality"`
	PROCByProcess                []Aggregate       `json:"proc_by_process"`
	PROCByOSThread               []Aggregate       `json:"proc_by_os_thread"`
	PROCBySource                 []Aggregate       `json:"proc_by_source"`
	PROCSLowSamples              []ProcSample      `json:"proc_slow_samples"`
	SCOMByOperation              []Aggregate       `json:"scom_by_operation"`
	SCOMByProcess                []Aggregate       `json:"scom_by_process"`
	SCOMBySource                 []Aggregate       `json:"scom_by_source"`
	SCOMByOperationProcessSource []SCOMAggregate   `json:"scom_by_operation_process_source"`
	ExplicitProcessRelations     []ProcessRelation `json:"explicit_process_relations"`
}

// Collector receives normalized techlog events and is not safe for concurrent use.
type Collector struct {
	limit                                                 int
	quality                                               Quality
	procProcess, procThread, procSource                   map[string]*bucket
	scomOperation, scomProcess, scomSource, scomComposite map[string]*bucket
	samples                                               []ProcSample
	relations                                             map[string]*ProcessRelation
}

type bucket struct {
	count    int64
	duration stats.NumericStats
}

// NewCollector creates an empty collector.
func NewCollector(options Options) *Collector {
	limit := options.SampleLimit
	if limit <= 0 {
		limit = 20
	}
	return &Collector{
		limit:       limit,
		procProcess: make(map[string]*bucket), procThread: make(map[string]*bucket), procSource: make(map[string]*bucket),
		scomOperation: make(map[string]*bucket), scomProcess: make(map[string]*bucket), scomSource: make(map[string]*bucket), scomComposite: make(map[string]*bucket),
		relations: make(map[string]*ProcessRelation),
	}
}

// Consume handles PROC and SCOM. Other events are counted but ignored.
func (c *Collector) Consume(event techlog.Event) {
	c.ensureMaps()
	switch event.Name {
	case "PROC":
		c.consumePROC(event)
	case "SCOM":
		c.consumeSCOM(event)
	default:
		c.quality.IgnoredEvents++
	}
}

func (c *Collector) consumePROC(event techlog.Event) {
	c.quality.PROCEvents++
	process := c.text(event.Fields, "process", &c.quality.Process)
	thread := c.text(event.Fields, "OSThread", &c.quality.OSThread)
	source := c.source(event.Source)
	c.add(c.procProcess, process, event.DurationMicros)
	c.add(c.procThread, thread, event.DurationMicros)
	c.add(c.procSource, source, event.DurationMicros)
	c.collectSample(ProcSample{Timestamp: event.Timestamp, Source: source, Process: process, OSThread: thread, EventDuration: event.DurationMicros, Raw: event.Raw})
}

func (c *Collector) consumeSCOM(event techlog.Event) {
	c.quality.SCOMEvents++
	process := c.text(event.Fields, "process", &c.quality.Process)
	source := c.source(event.Source)
	function, ok := c.function(event.Fields)
	if !ok {
		return
	}
	c.add(c.scomOperation, function.Operation, event.DurationMicros)
	c.add(c.scomProcess, process, event.DurationMicros)
	c.add(c.scomSource, source, event.DurationMicros)
	c.add(c.scomComposite, scomKey(function.Operation, process, source), event.DurationMicros)
	if !knownOperation(function.Operation) {
		c.quality.UnknownFunc++
		return
	}
	if len(function.Arguments) < 3 {
		c.quality.KnownFuncMalformedArguments++
		return
	}
	sourceName, destinationName := strings.TrimSpace(function.Arguments[1]), strings.TrimSpace(function.Arguments[2])
	if sourceName == "" || destinationName == "" {
		c.quality.KnownFuncMissingNames++
		return
	}
	key := relationKey(function.Operation, process, source, sourceName, destinationName)
	if relation := c.relations[key]; relation != nil {
		relation.Occurrences++
		return
	}
	c.relations[key] = &ProcessRelation{Operation: function.Operation, Process: process, LogSource: source, SourceProcessName: sourceName, DestinationProcessName: destinationName, Occurrences: 1}
}

// Function is the deliberately shallow, lossless representation of Func. Args
// are split only at top-level commas; nested or quoted forms are rejected.
type Function struct {
	Operation string   `json:"operation"`
	Arguments []string `json:"arguments"`
}

// ParseFunction parses the simple Func forms emitted by SCOM. It rejects
// ambiguous nesting, quoting, and trailing syntax rather than guessing.
func ParseFunction(value string) (Function, bool) {
	value = strings.TrimSpace(value)
	open := strings.IndexByte(value, '(')
	if open <= 0 || !strings.HasSuffix(value, ")") {
		return Function{}, false
	}
	operation := strings.TrimSpace(value[:open])
	inside := value[open+1 : len(value)-1]
	if operation == "" || strings.ContainsAny(inside, "()'\"") {
		return Function{}, false
	}
	var arguments []string
	if inside != "" {
		arguments = strings.Split(inside, ",")
		for i := range arguments {
			arguments[i] = strings.TrimSpace(arguments[i])
		}
	} else {
		arguments = []string{}
	}
	return Function{Operation: operation, Arguments: arguments}, true
}

// Result returns deterministic aggregates, samples, and explicit relations.
func (c *Collector) Result() Result {
	result := Result{
		Quality:       c.quality,
		PROCByProcess: c.rows(c.procProcess), PROCByOSThread: c.rows(c.procThread), PROCBySource: c.rows(c.procSource), PROCSLowSamples: append([]ProcSample(nil), c.samples...),
		SCOMByOperation: c.rows(c.scomOperation), SCOMByProcess: c.rows(c.scomProcess), SCOMBySource: c.rows(c.scomSource), SCOMByOperationProcessSource: c.scomRows(),
	}
	for _, relation := range c.relations {
		result.ExplicitProcessRelations = append(result.ExplicitProcessRelations, *relation)
	}
	sort.Slice(result.ExplicitProcessRelations, func(i, j int) bool {
		return relationSortKey(result.ExplicitProcessRelations[i]) < relationSortKey(result.ExplicitProcessRelations[j])
	})
	return result
}

func (c *Collector) ensureMaps() {
	if c.procProcess != nil {
		return
	}
	c.procProcess, c.procThread, c.procSource = make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket)
	c.scomOperation, c.scomProcess, c.scomSource, c.scomComposite = make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket)
	c.relations = make(map[string]*ProcessRelation)
	if c.limit <= 0 {
		c.limit = 20
	}
}
func (c *Collector) text(fields map[string]string, name string, quality *FieldQuality) string {
	if fields != nil && strings.TrimSpace(fields[name]) != "" {
		return strings.TrimSpace(fields[name])
	}
	quality.Missing++
	return missingValue
}
func (c *Collector) source(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	c.quality.Source.Missing++
	return missingValue
}
func (c *Collector) function(fields map[string]string) (Function, bool) {
	if fields == nil || strings.TrimSpace(fields["Func"]) == "" {
		c.quality.Func.Missing++
		return Function{}, false
	}
	f, ok := ParseFunction(fields["Func"])
	if !ok {
		c.quality.Func.Malformed++
		return Function{}, false
	}
	return f, true
}
func knownOperation(operation string) bool {
	return operation == "setSrcProcessName" || operation == "new ServerProcessData"
}
func (c *Collector) add(groups map[string]*bucket, key string, duration int64) {
	b := groups[key]
	if b == nil {
		b = &bucket{}
		groups[key] = b
	}
	b.count++
	if duration >= 0 {
		b.duration.Add(float64(duration))
	}
}
func (b *bucket) metrics() EventMetrics {
	return EventMetrics{Occurrences: b.count, EventDuration: b.duration.Finalize()}
}
func (c *Collector) rows(groups map[string]*bucket) []Aggregate {
	result := make([]Aggregate, 0, len(groups))
	for key, bucket := range groups {
		result = append(result, Aggregate{Key: key, Metrics: bucket.metrics()})
	}
	sort.Slice(result, func(i, j int) bool { return less(result[i].Metrics, result[j].Metrics, result[i].Key, result[j].Key) })
	return result
}
func (c *Collector) scomRows() []SCOMAggregate {
	result := make([]SCOMAggregate, 0, len(c.scomComposite))
	for key, bucket := range c.scomComposite {
		parts := strings.Split(key, "\x00")
		result = append(result, SCOMAggregate{Operation: parts[0], Process: parts[1], Source: parts[2], Metrics: bucket.metrics()})
	}
	sort.Slice(result, func(i, j int) bool {
		return less(result[i].Metrics, result[j].Metrics, scomKey(result[i].Operation, result[i].Process, result[i].Source), scomKey(result[j].Operation, result[j].Process, result[j].Source))
	})
	return result
}
func less(left, right EventMetrics, leftKey, rightKey string) bool {
	if left.EventDuration.Sum != right.EventDuration.Sum {
		return left.EventDuration.Sum > right.EventDuration.Sum
	}
	return leftKey < rightKey
}
func scomKey(operation, process, source string) string {
	return operation + "\x00" + process + "\x00" + source
}
func relationKey(operation, process, source, sourceName, destinationName string) string {
	return operation + "\x00" + process + "\x00" + source + "\x00" + sourceName + "\x00" + destinationName
}
func relationSortKey(relation ProcessRelation) string {
	return relationKey(relation.Operation, relation.Process, relation.LogSource, relation.SourceProcessName, relation.DestinationProcessName)
}
func (c *Collector) collectSample(sample ProcSample) {
	c.samples = append(c.samples, sample)
	sort.Slice(c.samples, func(i, j int) bool {
		if c.samples[i].EventDuration != c.samples[j].EventDuration {
			return c.samples[i].EventDuration > c.samples[j].EventDuration
		}
		if !c.samples[i].Timestamp.Equal(c.samples[j].Timestamp) {
			return c.samples[i].Timestamp.Before(c.samples[j].Timestamp)
		}
		return procSampleKey(c.samples[i]) < procSampleKey(c.samples[j])
	})
	if len(c.samples) > c.limit {
		c.samples = c.samples[:c.limit]
	}
}
func procSampleKey(sample ProcSample) string {
	return sample.Source + "\x00" + sample.Process + "\x00" + sample.OSThread + "\x00" + sample.Raw
}
