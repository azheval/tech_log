// Package sqlstats groups SQL and SDBL events by a literal-independent query
// fingerprint. It is intentionally independent from CLI and report packages.
package sqlstats

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"techlog-stat/internal/techlog"
)

// Row is one deterministic query group. All durations are in microseconds.
type Row struct {
	Fingerprint         string
	EventType           string
	NormalizedQuery     string
	Sample              string
	Count               int64
	TotalDurationMicros int64
	MinDurationMicros   int64
	MaxDurationMicros   int64
	MeanDurationMicros  float64
	P50DurationMicros   int64
	P95DurationMicros   int64
	P99DurationMicros   int64
	RowsSum             int64
	RowsMax             int64
	Contexts            []string
	Users               []string
	Databases           []string
}

// Collector receives normalized technological-log events and aggregates events
// that contain either the Sql or Sdbl field. A Collector is not safe for
// concurrent use; callers that parse concurrently should use one per worker.
type Collector struct{ groups map[string]*group }

type group struct {
	eventType, normalized, sample string
	count, total, min, max        int64
	durations                     []int64
	rowsSum, rowsMax              int64
	contexts, users, databases    map[string]struct{}
}

// NewCollector creates an empty SQL/SDBL collector.
func NewCollector() *Collector { return &Collector{groups: make(map[string]*group)} }

// Consume adds event when it contains a non-empty Sql or Sdbl property.
// Sql is preferred when both fields are present, so one source event is never
// counted twice.
func (c *Collector) Consume(event techlog.Event) {
	if c.groups == nil {
		c.groups = make(map[string]*group)
	}
	query := queryFrom(event.Fields)
	if query == "" {
		return
	}
	normalized := Normalize(query)
	if normalized == "" {
		return
	}
	fingerprint := event.Name + "\x00" + normalized
	g := c.groups[fingerprint]
	if g == nil {
		g = &group{eventType: event.Name, normalized: normalized, sample: compactWhitespace(query), min: event.DurationMicros, max: event.DurationMicros, contexts: make(map[string]struct{}), users: make(map[string]struct{}), databases: make(map[string]struct{})}
		c.groups[fingerprint] = g
	}
	sample := compactWhitespace(query)
	if sample != "" && sample < g.sample {
		g.sample = sample
	}
	g.count++
	g.total += event.DurationMicros
	if event.DurationMicros < g.min {
		g.min = event.DurationMicros
	}
	if event.DurationMicros > g.max {
		g.max = event.DurationMicros
	}
	g.durations = append(g.durations, event.DurationMicros)
	if rows, ok := integerField(event.Fields, "Rows"); ok {
		g.rowsSum += rows
		if rows > g.rowsMax {
			g.rowsMax = rows
		}
	}
	addField(g.contexts, event.Fields, "Context")
	addField(g.users, event.Fields, "Usr")
	addField(g.databases, event.Fields, "DataBase")
}

// Rows returns a stable snapshot ordered by total duration descending, then
// event type and normalized query. Percentiles use the nearest-rank method.
func (c *Collector) Rows() []Row {
	rows := make([]Row, 0, len(c.groups))
	for fingerprint, g := range c.groups {
		durations := append([]int64(nil), g.durations...)
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		rows = append(rows, Row{Fingerprint: fingerprint, EventType: g.eventType, NormalizedQuery: g.normalized, Sample: g.sample, Count: g.count, TotalDurationMicros: g.total, MinDurationMicros: g.min, MaxDurationMicros: g.max, MeanDurationMicros: float64(g.total) / float64(g.count), P50DurationMicros: percentile(durations, .50), P95DurationMicros: percentile(durations, .95), P99DurationMicros: percentile(durations, .99), RowsSum: g.rowsSum, RowsMax: g.rowsMax, Contexts: sortedKeys(g.contexts), Users: sortedKeys(g.users), Databases: sortedKeys(g.databases)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalDurationMicros != rows[j].TotalDurationMicros {
			return rows[i].TotalDurationMicros > rows[j].TotalDurationMicros
		}
		if rows[i].EventType != rows[j].EventType {
			return rows[i].EventType < rows[j].EventType
		}
		return rows[i].NormalizedQuery < rows[j].NormalizedQuery
	})
	return rows
}

// Normalize replaces quoted-string and numeric literals with ? and collapses
// whitespace. SQL keywords and identifiers retain their original spelling.
func Normalize(query string) string {
	var out strings.Builder
	for i := 0; i < len(query); {
		ch := query[i]
		if isSpace(ch) {
			next := i + 1
			for next < len(query) && isSpace(query[next]) {
				next++
			}
			if out.Len() > 0 && lastByte(&out) != ' ' && !isTightOperator(lastByte(&out)) && (next == len(query) || !isTightOperator(query[next])) {
				out.WriteByte(' ')
			}
			i = next
			continue
		}
		if ch == '\'' {
			quote := ch
			out.WriteByte('?')
			i++
			for i < len(query) {
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if ch == '"' { // SQL quoted identifier: retain it, including digits inside it.
			out.WriteByte(ch)
			i++
			for i < len(query) {
				out.WriteByte(query[i])
				if query[i] == '"' {
					if i+1 < len(query) && query[i+1] == '"' {
						out.WriteByte(query[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if isNumberStart(query, i) {
			end := numberEnd(query, i)
			if end > i && (end == len(query) || !isIdentifier(query[end])) {
				out.WriteByte('?')
				i = end
				continue
			}
		}
		out.WriteByte(ch)
		i++
	}
	return strings.TrimSpace(out.String())
}

func queryFrom(fields map[string]string) string {
	if fields == nil {
		return ""
	}
	if query := strings.TrimSpace(fields["Sql"]); query != "" {
		return query
	}
	return strings.TrimSpace(fields["Sdbl"])
}
func integerField(fields map[string]string, key string) (int64, bool) {
	value := strings.TrimSpace(fields[key])
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}
func addField(set map[string]struct{}, fields map[string]string, key string) {
	if value := strings.TrimSpace(fields[key]); value != "" {
		set[value] = struct{}{}
	}
}
func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
func compactWhitespace(value string) string { return strings.Join(strings.Fields(value), " ") }
func isSpace(ch byte) bool                  { return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' }
func isTightOperator(ch byte) bool          { return ch == '=' || ch == '<' || ch == '>' || ch == '!' }
func isIdentifier(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}
func isNumberStart(value string, index int) bool {
	return value[index] >= '0' && value[index] <= '9' && (index == 0 || !isIdentifier(value[index-1]))
}
func numberEnd(value string, index int) int {
	i := index
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i < len(value) && value[i] == '.' {
		i++
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
	}
	if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
		j := i + 1
		if j < len(value) && (value[j] == '+' || value[j] == '-') {
			j++
		}
		start := j
		for j < len(value) && value[j] >= '0' && value[j] <= '9' {
			j++
		}
		if j > start {
			i = j
		}
	}
	return i
}
func lastByte(builder *strings.Builder) byte { return builder.String()[builder.Len()-1] }
