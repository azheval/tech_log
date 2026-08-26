// Package lockstats provides conservative aggregation for lock-related events.
package lockstats

import (
	"math"
	"sort"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

// Options controls bounded result slices. Zero uses safe defaults.
type Options struct {
	SampleLimit      int
	TopConflictLimit int
}

// DurationStats contains duration statistics in microseconds.
type DurationStats struct {
	Count, TotalMicros, MinMicros, MaxMicros int64
	MeanMicros                               float64
	P50Micros, P95Micros, P99Micros          int64
}

// Aggregate is a duration aggregate keyed by a single event, context, table,
// or region value.
type Aggregate struct {
	Key   string
	Stats DurationStats
}

// Conflict describes an observed event-level combination. Tables and regions
// are merely co-present properties; this package does not infer a pairwise
// table-region relationship.
type Conflict struct {
	EventType string
	Context   string
	Tables    []string
	Regions   []string
	Stats     DurationStats
}

// Sample preserves a bounded set of slowest raw lock events for investigation.
type Sample struct {
	Timestamp      time.Time
	Source         string
	EventType      string
	DurationMicros int64
	Context        string
	Tables         []string
	Regions        []string
	Raw            string
}

// Relation is reported only when a source event contains explicit waiter and
// blocker fields. No relation is inferred from Locks, Regions, or Context.
type Relation struct {
	EventType string
	Waiter    string
	Blocker   string
	Context   string
	Source    string
}

// Quality exposes data completeness for the lock event population.
type Quality struct {
	IgnoredEvents, LockEvents                    int64
	MissingContext, MissingLocks, MissingRegions int64
	EventsWithExplicitRelation                   int64
}

// Result is a stable snapshot, ordered by duration descending and then key.
type Result struct {
	Quality      Quality
	ByEvent      []Aggregate
	ByContext    []Aggregate
	ByTable      []Aggregate
	ByRegion     []Aggregate
	TopConflicts []Conflict
	Samples      []Sample
	Relations    []Relation
}

// Collector accepts techlog.Event values. It is not safe for concurrent use.
type Collector struct {
	limit, conflictLimit                         int
	quality                                      Quality
	events, contexts, tables, regions, conflicts map[string]*bucket
	samples                                      []Sample
	relations                                    map[string]Relation
}

type bucket struct {
	count, total, min, max int64
	values                 []int64
}

// NewCollector creates a collector. The optional Options keeps the normal use
// terse while allowing callers to bound retained raw events.
func NewCollector(options ...Options) *Collector {
	limit := 20
	conflictLimit := 50
	if len(options) > 0 && options[0].SampleLimit > 0 {
		limit = options[0].SampleLimit
	}
	if len(options) > 0 && options[0].TopConflictLimit > 0 {
		conflictLimit = options[0].TopConflictLimit
	}
	return &Collector{limit: limit, conflictLimit: conflictLimit, events: make(map[string]*bucket), contexts: make(map[string]*bucket), tables: make(map[string]*bucket), regions: make(map[string]*bucket), conflicts: make(map[string]*bucket), relations: make(map[string]Relation)}
}

// Consume handles TLOCK, TTIMEOUT, and TDEADLOCK; other event types are
// counted as ignored. It never fabricates waiter/blocker information.
func (c *Collector) Consume(event techlog.Event) {
	if !isLockEvent(event.Name) {
		c.quality.IgnoredEvents++
		return
	}
	c.quality.LockEvents++
	context := strings.TrimSpace(event.Fields["Context"])
	tables, regions := dimensions(event.Fields)
	if context == "" {
		c.quality.MissingContext++
	}
	if len(tables) == 0 {
		c.quality.MissingLocks++
	}
	if len(regions) == 0 {
		c.quality.MissingRegions++
	}
	add(c.events, event.Name, event.DurationMicros)
	if context != "" {
		add(c.contexts, context, event.DurationMicros)
	}
	for _, table := range tables {
		add(c.tables, table, event.DurationMicros)
	}
	for _, region := range regions {
		add(c.regions, region, event.DurationMicros)
	}
	add(c.conflicts, conflictKey(event.Name, context, tables, regions), event.DurationMicros)
	c.collectSample(Sample{Timestamp: event.Timestamp, Source: event.Source, EventType: event.Name, DurationMicros: event.DurationMicros, Context: context, Tables: tables, Regions: regions, Raw: event.Raw})
	if relation, ok := explicitRelation(event, context); ok {
		c.quality.EventsWithExplicitRelation++
		c.relations[relation.EventType+"\x00"+relation.Waiter+"\x00"+relation.Blocker+"\x00"+relation.Context+"\x00"+relation.Source] = relation
	}
}

// Result returns deterministic aggregates and samples.
func (c *Collector) Result() Result {
	result := Result{Quality: c.quality, ByEvent: aggregates(c.events), ByContext: aggregates(c.contexts), ByTable: aggregates(c.tables), ByRegion: aggregates(c.regions), Samples: append([]Sample(nil), c.samples...)}
	for key, value := range c.conflicts {
		parts := strings.Split(key, "\x00")
		result.TopConflicts = append(result.TopConflicts, Conflict{EventType: parts[0], Context: parts[1], Tables: splitKey(parts[2]), Regions: splitKey(parts[3]), Stats: stats(value)})
	}
	sort.Slice(result.TopConflicts, func(i, j int) bool {
		return lessStats(result.TopConflicts[i].Stats, result.TopConflicts[j].Stats, conflictSortKey(result.TopConflicts[i]), conflictSortKey(result.TopConflicts[j]))
	})
	if len(result.TopConflicts) > c.conflictLimit {
		result.TopConflicts = result.TopConflicts[:c.conflictLimit]
	}
	for _, relation := range c.relations {
		result.Relations = append(result.Relations, relation)
	}
	sort.Slice(result.Relations, func(i, j int) bool { return relationKey(result.Relations[i]) < relationKey(result.Relations[j]) })
	return result
}

func isLockEvent(name string) bool {
	return name == "TLOCK" || name == "TTIMEOUT" || name == "TDEADLOCK"
}
func add(groups map[string]*bucket, key string, duration int64) {
	b := groups[key]
	if b == nil {
		b = &bucket{min: duration, max: duration}
		groups[key] = b
	}
	b.count++
	b.total += duration
	if duration < b.min {
		b.min = duration
	}
	if duration > b.max {
		b.max = duration
	}
	b.values = append(b.values, duration)
}
func aggregates(groups map[string]*bucket) []Aggregate {
	result := make([]Aggregate, 0, len(groups))
	for key, value := range groups {
		result = append(result, Aggregate{Key: key, Stats: stats(value)})
	}
	sort.Slice(result, func(i, j int) bool { return lessStats(result[i].Stats, result[j].Stats, result[i].Key, result[j].Key) })
	return result
}
func stats(b *bucket) DurationStats {
	values := append([]int64(nil), b.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return DurationStats{Count: b.count, TotalMicros: b.total, MinMicros: b.min, MaxMicros: b.max, MeanMicros: float64(b.total) / float64(b.count), P50Micros: percentile(values, .5), P95Micros: percentile(values, .95), P99Micros: percentile(values, .99)}
}
func percentile(values []int64, ratio float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(ratio*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
func lessStats(left, right DurationStats, leftKey, rightKey string) bool {
	if left.TotalMicros != right.TotalMicros {
		return left.TotalMicros > right.TotalMicros
	}
	return leftKey < rightKey
}

func dimensions(fields map[string]string) ([]string, []string) {
	tables := valuesFor(fields, []string{"Table", "TableName", "Таблица"})
	regions := valuesFor(fields, []string{"Region", "Область"})
	lockTables, lockRegions := extractNamed(fields["Locks"])
	tables = append(tables, lockTables...)
	regions = append(regions, lockRegions...)
	// Regions is often a bare list rather than name=value pairs. Treat each
	// listed value as a region, but never make an analogous table guess.
	if raw := fields["Regions"]; raw != "" {
		_, namedRegions := extractNamed(raw)
		regions = append(regions, namedRegions...)
		for _, value := range splitValues(raw) {
			if _, _, named := splitNamed(value); !named {
				regions = append(regions, value)
			}
		}
	}
	return uniqueSorted(tables), uniqueSorted(regions)
}
func valuesFor(fields map[string]string, keys []string) []string {
	var values []string
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			values = append(values, splitValues(value)...)
		}
	}
	return values
}
func extractNamed(value string) ([]string, []string) {
	var tables, regions []string
	for _, token := range splitValues(value) {
		key, value, ok := splitNamed(token)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "table", "tablename", "таблица":
			tables = append(tables, value)
		case "region", "regions", "область":
			regions = append(regions, value)
		}
	}
	return tables, regions
}
func splitValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
}
func splitNamed(token string) (string, string, bool) {
	if index := strings.IndexAny(token, "=:"); index > 0 {
		key := strings.TrimSpace(token[:index])
		value := strings.TrimSpace(token[index+1:])
		return key, value, key != "" && value != ""
	}
	return "", "", false
}
func uniqueSorted(values []string) []string {
	set := make(map[string]struct{})
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func conflictKey(event, context string, tables, regions []string) string {
	return event + "\x00" + context + "\x00" + strings.Join(tables, "\x1f") + "\x00" + strings.Join(regions, "\x1f")
}
func splitKey(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "\x1f")
}
func conflictSortKey(value Conflict) string {
	return conflictKey(value.EventType, value.Context, value.Tables, value.Regions)
}
func (c *Collector) collectSample(sample Sample) {
	c.samples = append(c.samples, sample)
	sort.Slice(c.samples, func(i, j int) bool {
		if c.samples[i].DurationMicros != c.samples[j].DurationMicros {
			return c.samples[i].DurationMicros > c.samples[j].DurationMicros
		}
		if !c.samples[i].Timestamp.Equal(c.samples[j].Timestamp) {
			return c.samples[i].Timestamp.Before(c.samples[j].Timestamp)
		}
		return sampleKey(c.samples[i]) < sampleKey(c.samples[j])
	})
	if len(c.samples) > c.limit {
		c.samples = c.samples[:c.limit]
	}
}
func sampleKey(value Sample) string {
	return value.EventType + "\x00" + value.Source + "\x00" + value.Context + "\x00" + strings.Join(value.Tables, "\x1f") + "\x00" + strings.Join(value.Regions, "\x1f") + "\x00" + value.Raw
}
func explicitRelation(event techlog.Event, context string) (Relation, bool) {
	for _, pair := range [][2]string{{"Waiter", "Blocker"}, {"WaiterConnection", "BlockerConnection"}, {"WaitConnection", "BlockConnection"}} {
		waiter, blocker := strings.TrimSpace(event.Fields[pair[0]]), strings.TrimSpace(event.Fields[pair[1]])
		if waiter != "" && blocker != "" {
			return Relation{EventType: event.Name, Waiter: waiter, Blocker: blocker, Context: context, Source: event.Source}, true
		}
	}
	return Relation{}, false
}
func relationKey(value Relation) string {
	return value.EventType + "\x00" + value.Waiter + "\x00" + value.Blocker + "\x00" + value.Context + "\x00" + value.Source
}
