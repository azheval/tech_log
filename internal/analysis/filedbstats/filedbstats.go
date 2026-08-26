// Package filedbstats provides conservative aggregation for DBV8DBEng events.
//
// DBV8DBEng property sets vary between platform versions. Consequently, this
// package aggregates a property only when that property is actually present;
// it does not turn an absent property into a synthetic value or relationship.
package filedbstats

import (
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

const defaultSampleLimit = 20

// Options controls maximum retained slow and explicit-error samples. A
// non-positive limit uses a conservative default.
type Options struct {
	SlowSampleLimit  int `json:"slow_sample_limit"`
	ErrorSampleLimit int `json:"error_sample_limit"`
}

// Class is a deliberately small operation classification. Other means Func
// was not one of the exact operation names recognized here.
type Class string

const (
	ClassRead                Class = "read"
	ClassWrite               Class = "write"
	ClassTransactionBegin    Class = "transaction_begin"
	ClassTransactionCommit   Class = "transaction_commit"
	ClassTransactionRollback Class = "transaction_rollback"
	ClassOther               Class = "other"
)

// DurationStats contains exact duration statistics in microseconds. Count is
// zero when no valid duration was observed, distinct from a measured zero.
type DurationStats struct {
	Count       int64   `json:"count"`
	TotalMicros int64   `json:"total_micros"`
	MinMicros   int64   `json:"min_micros"`
	MaxMicros   int64   `json:"max_micros"`
	MeanMicros  float64 `json:"mean_micros"`
	P50Micros   int64   `json:"p50_micros"`
	P95Micros   int64   `json:"p95_micros"`
	P99Micros   int64   `json:"p99_micros"`
}

// ValueStats contains statistics for an optional integer log property.
type ValueStats struct {
	Count int64   `json:"count"`
	Sum   int64   `json:"sum"`
	Min   int64   `json:"min"`
	Max   int64   `json:"max"`
	Mean  float64 `json:"mean"`
}

// RowsStats keeps Rows and RowsAffected separate rather than assuming they
// share a meaning.
type RowsStats struct {
	Rows         ValueStats `json:"rows"`
	RowsAffected ValueStats `json:"rows_affected"`
}

// Aggregate is one grouping row. Count is events carrying the grouping
// property (or every DBV8DBEng event for ByClass).
type Aggregate struct {
	Key      string        `json:"key"`
	Count    int64         `json:"count"`
	Duration DurationStats `json:"duration"`
	Rows     RowsStats     `json:"rows"`
}

// Sample is a bounded, path-safe event view. It excludes Raw and Source: both
// can disclose machine paths. HasRows preserves absent vs. observed zero.
type Sample struct {
	Timestamp       time.Time `json:"timestamp"`
	DurationMicros  int64     `json:"duration_micros"`
	Func            string    `json:"func"`
	Class           Class     `json:"class"`
	TableName       string    `json:"table_name"`
	Category        string    `json:"category"`
	FileName        string    `json:"file_name"`
	Trans           string    `json:"trans"`
	Database        string    `json:"database"`
	Process         string    `json:"process"`
	User            string    `json:"user"`
	Rows            int64     `json:"rows"`
	HasRows         bool      `json:"has_rows"`
	RowsAffected    int64     `json:"rows_affected"`
	HasRowsAffected bool      `json:"has_rows_affected"`
	ErrorField      string    `json:"error_field"`
}

// Quality reports input completeness and rejected measurements. Missing
// properties are tracked but never made into synthetic grouping values.
type Quality struct {
	EventsConsumed        int64 `json:"events_consumed"`
	IgnoredEvents         int64 `json:"ignored_events"`
	DBV8DBEngEvents       int64 `json:"dbv8dbeng_events"`
	MissingFunc           int64 `json:"missing_func"`
	MissingTableName      int64 `json:"missing_table_name"`
	MissingCatName        int64 `json:"missing_cat_name"`
	MissingFileName       int64 `json:"missing_file_name"`
	MissingTrans          int64 `json:"missing_trans"`
	MissingDataBase       int64 `json:"missing_database"`
	MissingProcess        int64 `json:"missing_process"`
	MissingUser           int64 `json:"missing_user"`
	MissingRows           int64 `json:"missing_rows"`
	MissingRowsAffected   int64 `json:"missing_rows_affected"`
	MalformedRows         int64 `json:"malformed_rows"`
	MalformedRowsAffected int64 `json:"malformed_rows_affected"`
	InvalidDuration       int64 `json:"invalid_duration"`
	ExplicitErrorEvents   int64 `json:"explicit_error_events"`
	DroppedSlowSamples    int64 `json:"dropped_slow_samples"`
	DroppedErrorSamples   int64 `json:"dropped_error_samples"`
}

// Result is a deterministic snapshot of the collected events.
type Result struct {
	Quality      Quality     `json:"quality"`
	ByFunc       []Aggregate `json:"by_func"`
	ByTable      []Aggregate `json:"by_table"`
	ByCategory   []Aggregate `json:"by_category"`
	ByFile       []Aggregate `json:"by_file"`
	ByDatabase   []Aggregate `json:"by_database"`
	ByProcess    []Aggregate `json:"by_process"`
	ByUser       []Aggregate `json:"by_user"`
	ByClass      []Aggregate `json:"by_class"`
	SlowSamples  []Sample    `json:"slow_samples"`
	ErrorSamples []Sample    `json:"error_samples"`
}

// Collector incrementally receives normalized events. It is not safe for
// concurrent use.
type Collector struct {
	slowLimit, errorLimit                                                  int
	quality                                                                Quality
	funcs, tables, categories, files, databases, processes, users, classes map[string]*bucket
	slow, errors                                                           []Sample
}

type bucket struct {
	count                     int64
	durations, rows, affected series
}
type series struct {
	count, sum, min, max int64
	values               []int64
}

// NewCollector creates an empty DBV8DBEng collector.
func NewCollector(options Options) *Collector {
	slowLimit := options.SlowSampleLimit
	if slowLimit <= 0 {
		slowLimit = defaultSampleLimit
	}
	errorLimit := options.ErrorSampleLimit
	if errorLimit <= 0 {
		errorLimit = defaultSampleLimit
	}
	return &Collector{slowLimit: slowLimit, errorLimit: errorLimit,
		funcs: make(map[string]*bucket), tables: make(map[string]*bucket), categories: make(map[string]*bucket), files: make(map[string]*bucket),
		databases: make(map[string]*bucket), processes: make(map[string]*bucket), users: make(map[string]*bucket), classes: make(map[string]*bucket)}
}

// Consume accepts DBV8DBEng only. Func classification uses only an exact
// operation name (case-insensitively); Trans never substitutes for Func.
func (c *Collector) Consume(event techlog.Event) {
	c.ensure()
	c.quality.EventsConsumed++
	if event.Name != "DBV8DBEng" {
		c.quality.IgnoredEvents++
		return
	}
	c.quality.DBV8DBEngEvents++
	funcName, hasFunc := c.field(event.Fields, "Func", &c.quality.MissingFunc)
	table, hasTable := c.field(event.Fields, "tableName", &c.quality.MissingTableName)
	category, hasCategory := c.field(event.Fields, "CatName", &c.quality.MissingCatName)
	file, hasFile := c.field(event.Fields, "FileName", &c.quality.MissingFileName)
	trans, _ := c.field(event.Fields, "Trans", &c.quality.MissingTrans)
	database, hasDatabase := c.field(event.Fields, "DataBase", &c.quality.MissingDataBase)
	process, hasProcess := c.field(event.Fields, "process", &c.quality.MissingProcess)
	user, hasUser := c.field(event.Fields, "Usr", &c.quality.MissingUser)
	if hasFile {
		file = NormalizeFileName(file)
	}
	rows, hasRows := c.number(event.Fields, "Rows", &c.quality.MissingRows, &c.quality.MalformedRows)
	affected, hasAffected := c.number(event.Fields, "RowsAffected", &c.quality.MissingRowsAffected, &c.quality.MalformedRowsAffected)
	duration, validDuration := event.DurationMicros, event.DurationMicros >= 0
	if !validDuration {
		c.quality.InvalidDuration++
	}
	class := ClassifyFunc(funcName)
	values := eventValues{duration: duration, validDuration: validDuration, rows: rows, hasRows: hasRows, affected: affected, hasAffected: hasAffected}
	if hasFunc {
		c.add(c.funcs, funcName, values)
	}
	if hasTable {
		c.add(c.tables, table, values)
	}
	if hasCategory {
		c.add(c.categories, category, values)
	}
	if hasFile {
		c.add(c.files, file, values)
	}
	if hasDatabase {
		c.add(c.databases, database, values)
	}
	if hasProcess {
		c.add(c.processes, process, values)
	}
	if hasUser {
		c.add(c.users, user, values)
	}
	c.add(c.classes, string(class), values)
	sample := Sample{Timestamp: event.Timestamp, DurationMicros: duration, Func: funcName, Class: class, TableName: table, Category: category, FileName: file, Trans: trans, Database: database, Process: process, User: user, Rows: rows, HasRows: hasRows, RowsAffected: affected, HasRowsAffected: hasAffected}
	if validDuration {
		c.addSlow(sample)
	}
	if marker := explicitError(event.Fields); marker != "" {
		c.quality.ExplicitErrorEvents++
		sample.ErrorField = marker
		c.addError(sample)
	}
}

// Result returns a stable copy which callers may modify.
func (c *Collector) Result() Result {
	c.ensure()
	return Result{Quality: c.quality, ByFunc: aggregates(c.funcs), ByTable: aggregates(c.tables), ByCategory: aggregates(c.categories), ByFile: aggregates(c.files), ByDatabase: aggregates(c.databases), ByProcess: aggregates(c.processes), ByUser: aggregates(c.users), ByClass: aggregates(c.classes), SlowSamples: append([]Sample(nil), c.slow...), ErrorSamples: append([]Sample(nil), c.errors...)}
}

// ClassifyFunc recognizes only exact DB-engine operation labels.
func ClassifyFunc(value string) Class {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read":
		return ClassRead
	case "write":
		return ClassWrite
	case "begintransaction":
		return ClassTransactionBegin
	case "committransaction":
		return ClassTransactionCommit
	case "rollbacktransaction":
		return ClassTransactionRollback
	default:
		return ClassOther
	}
}

// NormalizeFileName retains relative names but redacts every absolute path.
func NormalizeFileName(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "\"'")
	if value == "" {
		return ""
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	absolute := strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || (len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/')
	if absolute {
		base := path.Base(normalized)
		if base == "." || base == "/" || base == "" {
			return "<absolute-path>"
		}
		return "<absolute-path>/" + base
	}
	return path.Clean(normalized)
}

type eventValues struct {
	duration      int64
	validDuration bool
	rows          int64
	hasRows       bool
	affected      int64
	hasAffected   bool
}

func (c *Collector) ensure() {
	if c.funcs != nil {
		return
	}
	*c = *NewCollector(Options{SlowSampleLimit: c.slowLimit, ErrorSampleLimit: c.errorLimit})
}
func (c *Collector) field(fields map[string]string, key string, missing *int64) (string, bool) {
	if fields != nil {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value, true
		}
	}
	*missing++
	return "", false
}
func (c *Collector) number(fields map[string]string, key string, missing, malformed *int64) (int64, bool) {
	if fields == nil || strings.TrimSpace(fields[key]) == "" {
		*missing++
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(fields[key]), 10, 64)
	if err != nil || value < 0 {
		*malformed++
		return 0, false
	}
	return value, true
}
func (c *Collector) add(groups map[string]*bucket, key string, values eventValues) {
	b := groups[key]
	if b == nil {
		b = &bucket{}
		groups[key] = b
	}
	b.count++
	if values.validDuration {
		b.durations.add(values.duration)
	}
	if values.hasRows {
		b.rows.add(values.rows)
	}
	if values.hasAffected {
		b.affected.add(values.affected)
	}
}
func (s *series) add(value int64) {
	if s.count == 0 {
		s.min, s.max = value, value
	} else {
		if value < s.min {
			s.min = value
		}
		if value > s.max {
			s.max = value
		}
	}
	s.count++
	s.sum += value
	s.values = append(s.values, value)
}
func aggregates(groups map[string]*bucket) []Aggregate {
	result := make([]Aggregate, 0, len(groups))
	for key, b := range groups {
		result = append(result, Aggregate{Key: key, Count: b.count, Duration: durationStats(b.durations), Rows: RowsStats{Rows: valueStats(b.rows), RowsAffected: valueStats(b.affected)}})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Duration.TotalMicros != result[j].Duration.TotalMicros {
			return result[i].Duration.TotalMicros > result[j].Duration.TotalMicros
		}
		return result[i].Key < result[j].Key
	})
	return result
}
func durationStats(s series) DurationStats {
	if s.count == 0 {
		return DurationStats{}
	}
	values := append([]int64(nil), s.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return DurationStats{Count: s.count, TotalMicros: s.sum, MinMicros: s.min, MaxMicros: s.max, MeanMicros: float64(s.sum) / float64(s.count), P50Micros: percentile(values, .50), P95Micros: percentile(values, .95), P99Micros: percentile(values, .99)}
}
func valueStats(s series) ValueStats {
	if s.count == 0 {
		return ValueStats{}
	}
	return ValueStats{Count: s.count, Sum: s.sum, Min: s.min, Max: s.max, Mean: float64(s.sum) / float64(s.count)}
}
func percentile(values []int64, ratio float64) int64 {
	return values[int(math.Ceil(ratio*float64(len(values))))-1]
}
func explicitError(fields map[string]string) string {
	for _, key := range []string{"Error", "Err", "Exception"} {
		if fields != nil && strings.TrimSpace(fields[key]) != "" {
			return key
		}
	}
	for _, key := range []string{"Result", "Status"} {
		if fields != nil && strings.EqualFold(strings.TrimSpace(fields[key]), "error") {
			return key
		}
	}
	return ""
}
func (c *Collector) addSlow(sample Sample) {
	c.slow = append(c.slow, sample)
	sort.Slice(c.slow, func(i, j int) bool { return slowLess(c.slow[i], c.slow[j]) })
	if len(c.slow) > c.slowLimit {
		c.slow = c.slow[:c.slowLimit]
		c.quality.DroppedSlowSamples++
	}
}
func (c *Collector) addError(sample Sample) {
	c.errors = append(c.errors, sample)
	sort.Slice(c.errors, func(i, j int) bool { return errorLess(c.errors[i], c.errors[j]) })
	if len(c.errors) > c.errorLimit {
		c.errors = c.errors[:c.errorLimit]
		c.quality.DroppedErrorSamples++
	}
}
func slowLess(a, b Sample) bool {
	if a.DurationMicros != b.DurationMicros {
		return a.DurationMicros > b.DurationMicros
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return sampleKey(a) < sampleKey(b)
}
func errorLess(a, b Sample) bool {
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return sampleKey(a) < sampleKey(b)
}
func sampleKey(s Sample) string {
	return strings.Join([]string{s.Func, s.TableName, s.Category, s.FileName, s.Trans, s.Database, s.Process, s.User, s.ErrorField}, "\x00")
}
