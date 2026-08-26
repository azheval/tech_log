// Package scallstats provides aggregation for SCALL (server call) events.
// It deliberately has no dependency on the command or reporting packages.
package scallstats

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/stats"
	"techlog-stat/internal/techlog"
)

const missingValue = "(missing)"

// Options controls the amount of raw-call detail retained in a result. A zero
// SampleLimit uses a conservative default of 20.
type Options struct {
	SampleLimit int `json:"sample_limit"`
}

// FieldQuality describes missing and malformed source properties. Malformed is
// applicable to numeric properties; text dimensions are either present or
// missing.
type FieldQuality struct {
	Missing   int64 `json:"missing"`
	Malformed int64 `json:"malformed"`
}

// Quality makes SCALL input completeness visible to callers.
type Quality struct {
	IgnoredEvents int64        `json:"ignored_events"`
	CallEvents    int64        `json:"call_events"`
	Duration      FieldQuality `json:"duration"`
	Interface     FieldQuality `json:"interface"`
	IName         FieldQuality `json:"iname"`
	Method        FieldQuality `json:"method"`
	Context       FieldQuality `json:"context"`
	User          FieldQuality `json:"user"`
	Database      FieldQuality `json:"database"`
	Process       FieldQuality `json:"process"`
	InBytes       FieldQuality `json:"in_bytes"`
	OutBytes      FieldQuality `json:"out_bytes"`
	CPUTime       FieldQuality `json:"cpu_time"`
	Memory        FieldQuality `json:"memory"`
	MemoryPeak    FieldQuality `json:"memory_peak"`
	CallWait      FieldQuality `json:"call_wait"`
}

// Metrics contains duration and optional SCALL numerical-property summaries.
// Duration is measured in microseconds, as are CPUTime and CallWait when the
// technological log uses its usual units. Byte and memory units are preserved
// exactly as recorded by the source event.
type Metrics struct {
	Duration   stats.Summary `json:"duration"`
	InBytes    stats.Summary `json:"in_bytes"`
	OutBytes   stats.Summary `json:"out_bytes"`
	CPUTime    stats.Summary `json:"cpu_time"`
	Memory     stats.Summary `json:"memory"`
	MemoryPeak stats.Summary `json:"memory_peak"`
	CallWait   stats.Summary `json:"call_wait"`
}

// Aggregate is a single-dimension SCALL grouping.
type Aggregate struct {
	Key     string  `json:"key"`
	Metrics Metrics `json:"metrics"`
}

// Call is an SCALL grouping by Interface, IName, and Method. Missing source
// dimensions are represented as "(missing)" so every SCALL event remains
// visible in the composite grouping.
type Call struct {
	Interface string  `json:"interface"`
	IName     string  `json:"iname"`
	Method    string  `json:"method"`
	Metrics   Metrics `json:"metrics"`
}

// Sample is one of the bounded slowest observed SCALL events.
type Sample struct {
	Timestamp      time.Time `json:"timestamp"`
	Source         string    `json:"source"`
	DurationMicros int64     `json:"duration_micros"`
	Interface      string    `json:"interface"`
	IName          string    `json:"iname"`
	Method         string    `json:"method"`
	Context        string    `json:"context"`
	User           string    `json:"user"`
	Database       string    `json:"database"`
	Process        string    `json:"process"`
	Raw            string    `json:"raw"`
}

// Result is a deterministic snapshot. Aggregates are ordered by total call
// duration descending, with a lexical key tie-breaker. Samples are ordered by
// duration descending and then stable source-event properties.
type Result struct {
	Quality     Quality     `json:"quality"`
	ByCall      []Call      `json:"by_call"`
	ByInterface []Aggregate `json:"by_interface"`
	ByIName     []Aggregate `json:"by_iname"`
	ByMethod    []Aggregate `json:"by_method"`
	ByContext   []Aggregate `json:"by_context"`
	ByUser      []Aggregate `json:"by_user"`
	ByDatabase  []Aggregate `json:"by_database"`
	ByProcess   []Aggregate `json:"by_process"`
	SlowSamples []Sample    `json:"slow_samples"`
}

// Collector receives normalized events. It is not safe for concurrent use.
type Collector struct {
	limit   int
	quality Quality
	calls   map[string]*bucket
	byInt   map[string]*bucket
	byName  map[string]*bucket
	byMeth  map[string]*bucket
	byCtx   map[string]*bucket
	byUser  map[string]*bucket
	byDB    map[string]*bucket
	byProc  map[string]*bucket
	samples []Sample
}

type bucket struct {
	duration, inBytes, outBytes, cpuTime, memory, memoryPeak, callWait stats.NumericStats
}

type dimensions struct {
	interfaceID, iname, method, context, user, database, process string
}

// NewCollector creates an empty SCALL collector.
func NewCollector(options Options) *Collector {
	limit := options.SampleLimit
	if limit <= 0 {
		limit = 20
	}
	return &Collector{
		limit: limit,
		calls: make(map[string]*bucket), byInt: make(map[string]*bucket), byName: make(map[string]*bucket), byMeth: make(map[string]*bucket),
		byCtx: make(map[string]*bucket), byUser: make(map[string]*bucket), byDB: make(map[string]*bucket), byProc: make(map[string]*bucket),
	}
}

// Consume aggregates only SCALL events. It accepts missing dimensions and
// missing optional numeric fields, recording both in Result().Quality.
func (c *Collector) Consume(event techlog.Event) {
	if event.Name != "SCALL" {
		c.quality.IgnoredEvents++
		return
	}
	c.ensureMaps()
	c.quality.CallEvents++
	dims := c.dimensions(event.Fields)
	values := c.values(event)

	c.add(c.calls, callKey(dims), event.DurationMicros, values)
	c.add(c.byInt, dims.interfaceID, event.DurationMicros, values)
	c.add(c.byName, dims.iname, event.DurationMicros, values)
	c.add(c.byMeth, dims.method, event.DurationMicros, values)
	c.add(c.byCtx, dims.context, event.DurationMicros, values)
	c.add(c.byUser, dims.user, event.DurationMicros, values)
	c.add(c.byDB, dims.database, event.DurationMicros, values)
	c.add(c.byProc, dims.process, event.DurationMicros, values)
	c.collectSample(Sample{Timestamp: event.Timestamp, Source: event.Source, DurationMicros: event.DurationMicros, Interface: dims.interfaceID, IName: dims.iname, Method: dims.method, Context: dims.context, User: dims.user, Database: dims.database, Process: dims.process, Raw: event.Raw})
}

// Result returns a copy of the accumulated deterministic result.
func (c *Collector) Result() Result {
	return Result{
		Quality: c.quality, ByCall: c.callRows(), ByInterface: c.aggregateRows(c.byInt), ByIName: c.aggregateRows(c.byName), ByMethod: c.aggregateRows(c.byMeth),
		ByContext: c.aggregateRows(c.byCtx), ByUser: c.aggregateRows(c.byUser), ByDatabase: c.aggregateRows(c.byDB), ByProcess: c.aggregateRows(c.byProc),
		SlowSamples: append([]Sample(nil), c.samples...),
	}
}

func (c *Collector) ensureMaps() {
	if c.calls != nil {
		return
	}
	// Supporting a zero Collector is useful to code that embeds a collector.
	c.calls, c.byInt, c.byName, c.byMeth = make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket)
	c.byCtx, c.byUser, c.byDB, c.byProc = make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket), make(map[string]*bucket)
	if c.limit <= 0 {
		c.limit = 20
	}
}

func (c *Collector) dimensions(fields map[string]string) dimensions {
	return dimensions{
		interfaceID: c.text(fields, "Interface", &c.quality.Interface), iname: c.text(fields, "IName", &c.quality.IName), method: c.text(fields, "Method", &c.quality.Method),
		context: c.text(fields, "Context", &c.quality.Context), user: c.text(fields, "Usr", &c.quality.User), database: c.text(fields, "DataBase", &c.quality.Database), process: c.text(fields, "process", &c.quality.Process),
	}
}

func (c *Collector) text(fields map[string]string, name string, quality *FieldQuality) string {
	if fields == nil {
		quality.Missing++
		return missingValue
	}
	if value := strings.TrimSpace(fields[name]); value != "" {
		return value
	}
	quality.Missing++
	return missingValue
}

type values struct {
	inBytes, outBytes, cpuTime, memory, memoryPeak, callWait *float64
}

func (c *Collector) values(event techlog.Event) values {
	if event.DurationMicros < 0 {
		c.quality.Duration.Malformed++
	}
	return values{
		inBytes: c.number(event.Fields, "InBytes", &c.quality.InBytes), outBytes: c.number(event.Fields, "OutBytes", &c.quality.OutBytes),
		cpuTime: c.number(event.Fields, "CpuTime", &c.quality.CPUTime), memory: c.number(event.Fields, "Memory", &c.quality.Memory),
		memoryPeak: c.number(event.Fields, "MemoryPeak", &c.quality.MemoryPeak), callWait: c.number(event.Fields, "CallWait", &c.quality.CallWait),
	}
}

func (c *Collector) number(fields map[string]string, name string, quality *FieldQuality) *float64 {
	if fields == nil || strings.TrimSpace(fields[name]) == "" {
		quality.Missing++
		return nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(fields[name]), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		quality.Malformed++
		return nil
	}
	return &value
}

func (c *Collector) add(groups map[string]*bucket, key string, duration int64, values values) {
	b := groups[key]
	if b == nil {
		b = &bucket{}
		groups[key] = b
	}
	if duration >= 0 {
		b.duration.Add(float64(duration))
	}
	addOptional(&b.inBytes, values.inBytes)
	addOptional(&b.outBytes, values.outBytes)
	addOptional(&b.cpuTime, values.cpuTime)
	addOptional(&b.memory, values.memory)
	addOptional(&b.memoryPeak, values.memoryPeak)
	addOptional(&b.callWait, values.callWait)
}

func addOptional(target *stats.NumericStats, value *float64) {
	if value != nil {
		target.Add(*value)
	}
}

func (c *Collector) callRows() []Call {
	result := make([]Call, 0, len(c.calls))
	for key, bucket := range c.calls {
		parts := strings.Split(key, "\x00")
		result = append(result, Call{Interface: parts[0], IName: parts[1], Method: parts[2], Metrics: bucket.metrics()})
	}
	sort.Slice(result, func(i, j int) bool {
		return lessMetrics(result[i].Metrics, result[j].Metrics, callKey(dimensions{interfaceID: result[i].Interface, iname: result[i].IName, method: result[i].Method}), callKey(dimensions{interfaceID: result[j].Interface, iname: result[j].IName, method: result[j].Method}))
	})
	return result
}

func (c *Collector) aggregateRows(groups map[string]*bucket) []Aggregate {
	result := make([]Aggregate, 0, len(groups))
	for key, bucket := range groups {
		result = append(result, Aggregate{Key: key, Metrics: bucket.metrics()})
	}
	sort.Slice(result, func(i, j int) bool {
		return lessMetrics(result[i].Metrics, result[j].Metrics, result[i].Key, result[j].Key)
	})
	return result
}

func (b *bucket) metrics() Metrics {
	return Metrics{Duration: b.duration.Finalize(), InBytes: b.inBytes.Finalize(), OutBytes: b.outBytes.Finalize(), CPUTime: b.cpuTime.Finalize(), Memory: b.memory.Finalize(), MemoryPeak: b.memoryPeak.Finalize(), CallWait: b.callWait.Finalize()}
}
func lessMetrics(left, right Metrics, leftKey, rightKey string) bool {
	if left.Duration.Sum != right.Duration.Sum {
		return left.Duration.Sum > right.Duration.Sum
	}
	return leftKey < rightKey
}
func callKey(d dimensions) string { return d.interfaceID + "\x00" + d.iname + "\x00" + d.method }

func (c *Collector) collectSample(sample Sample) {
	c.samples = append(c.samples, sample)
	sort.Slice(c.samples, func(i, j int) bool { return lessSample(c.samples[i], c.samples[j]) })
	if len(c.samples) > c.limit {
		c.samples = c.samples[:c.limit]
	}
}

func lessSample(left, right Sample) bool {
	if left.DurationMicros != right.DurationMicros {
		return left.DurationMicros > right.DurationMicros
	}
	if !left.Timestamp.Equal(right.Timestamp) {
		return left.Timestamp.Before(right.Timestamp)
	}
	return sampleKey(left) < sampleKey(right)
}
func sampleKey(s Sample) string {
	return s.Source + "\x00" + s.Interface + "\x00" + s.IName + "\x00" + s.Method + "\x00" + s.Context + "\x00" + s.User + "\x00" + s.Database + "\x00" + s.Process + "\x00" + s.Raw
}
