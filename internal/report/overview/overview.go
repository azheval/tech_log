// Package overview builds a general, single-pass view of technological logs.
package overview

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"techlog-stat/internal/analysis/errorcontext"
	"techlog-stat/internal/analysis/filedbstats"
	"techlog-stat/internal/analysis/licensestats"
	"techlog-stat/internal/analysis/lockstats"
	"techlog-stat/internal/analysis/processstats"
	"techlog-stat/internal/analysis/scallstats"
	"techlog-stat/internal/analysis/sessionstats"
	"techlog-stat/internal/analysis/sqlstats"
	"techlog-stat/internal/analysis/trace"
	"techlog-stat/internal/analysis/webstats"
	"techlog-stat/internal/config"
	"techlog-stat/internal/discovery"
	"techlog-stat/internal/stats"
	"techlog-stat/internal/techlog"
)

const unknownDimension = "(unknown)"

// Options configures a standalone overview build. It deliberately does not
// depend on the existing CLI/config packages so it can be integrated later.
type Options struct {
	InputRoot      string
	Glob           string
	BucketInterval time.Duration
	TopN           int
	// Workers is the maximum number of files parsed concurrently. A zero value
	// uses one worker, preserving the original sequential behaviour.
	Workers           int
	Filters           []config.Filter
	MinDurationMicros int64
	TimeRange         config.TimeRange
	// MaxOpenTraces and MaxTraces bound trace correlation memory. Zero uses
	// the safe defaults provided by the trace collector.
	MaxOpenTraces int
	MaxTraces     int
	// LockSampleLimit and LockTopConflicts bound lock investigation data.
	// Zero uses lockstats' safe defaults.
	LockSampleLimit  int
	LockTopConflicts int
	// The following limits are passed through to their corresponding
	// single-pass analyzers. Zero values retain each analyzer's safe defaults.
	SCALLSampleLimit         int
	WebSampleLimit           int
	SessionLimit             int
	SessionSampleLimit       int
	ProcessSampleLimit       int
	LicenseSampleLimit       int
	FileDBSlowSampleLimit    int
	FileDBErrorSampleLimit   int
	ErrorContextWindow       time.Duration
	ErrorContextPendingLimit int
	ErrorContextSampleLimit  int
	// Progress, when non-nil, is called serially as the build advances. It must
	// return promptly; callbacks are part of the parsing critical path.
	Progress func(Progress)
}

// Progress is a snapshot of an overview build. Counts are cumulative, while
// CurrentFile and Status identify the step that produced this snapshot.
// Status is one of "matched", "parsing", "completed", "failed", or
// "canceled".
type Progress struct {
	FilesMatched   int
	FilesCompleted int
	FilesFailed    int
	EventsParsed   int64
	EventsAccepted int64
	BytesRead      int64
	CurrentFile    string
	Status         string
}

// Meta describes the input and the lifetime of a single build.
type Meta struct {
	StartedAt      time.Time     `json:"started_at"`
	FinishedAt     time.Time     `json:"finished_at"`
	Duration       time.Duration `json:"duration_ns"`
	InputRoot      string        `json:"input_root"`
	Glob           string        `json:"glob"`
	FilesMatched   int           `json:"files_matched"`
	FilesProcessed int           `json:"files_processed"`
	FilesFailed    int           `json:"files_failed"`
}

// Quality records parse quality independently of the analytical results.
type Quality struct {
	BytesRead        int64 `json:"bytes_read"`
	LinesRead        int64 `json:"lines_read"`
	EventsParsed     int64 `json:"events_parsed"`
	MalformedHeaders int64 `json:"malformed_headers"`
	OrphanLines      int64 `json:"orphan_lines"`
}

// Aggregate is a count and the descriptive statistics of event durations in
// microseconds. Duration is finalized only after all input has been read.
type Aggregate struct {
	Count    int64         `json:"count"`
	Duration stats.Summary `json:"duration_micros"`
}

// EventTypeStat is an aggregate for one technological-log event name.
type EventTypeStat struct {
	Event string    `json:"event"`
	Stats Aggregate `json:"stats"`
}

// TimeBucketStat is an aggregate for a fixed, half-open time bucket.
type TimeBucketStat struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Stats Aggregate `json:"stats"`
}

// DimensionStat is an aggregate for one field value (Usr, DataBase, process).
type DimensionStat struct {
	Value string    `json:"value"`
	Stats Aggregate `json:"stats"`
}

// RawEvent preserves a single longest event for drill-down. Fields includes
// all parsed properties and Raw is the complete original record.
type RawEvent struct {
	Timestamp      time.Time         `json:"timestamp"`
	Event          string            `json:"event"`
	Level          int               `json:"level"`
	DurationMicros int64             `json:"duration_micros"`
	Source         string            `json:"source"`
	Usr            string            `json:"usr,omitempty"`
	DataBase       string            `json:"database,omitempty"`
	Process        string            `json:"process,omitempty"`
	Fields         map[string]string `json:"fields"`
	Raw            string            `json:"raw"`
}

// OverviewResult is the complete one-pass overview, ready for a future JSON,
// HTML, or HTTP presentation layer.
type OverviewResult struct {
	Meta         Meta                `json:"meta"`
	Quality      Quality             `json:"quality"`
	Totals       Aggregate           `json:"totals"`
	EventTypes   []EventTypeStat     `json:"event_types"`
	Buckets      []TimeBucketStat    `json:"buckets"`
	Users        []DimensionStat     `json:"users"`
	Databases    []DimensionStat     `json:"databases"`
	Processes    []DimensionStat     `json:"processes"`
	Contexts     []DimensionStat     `json:"contexts"`
	TopEvents    []RawEvent          `json:"top_events"`
	SQLRows      []sqlstats.Row      `json:"sql_rows"`
	Traces       []trace.Trace       `json:"traces"`
	TraceQuality trace.Quality       `json:"trace_quality"`
	Locks        lockstats.Result    `json:"locks"`
	SCALL        scallstats.Result   `json:"scall"`
	Web          webstats.Result     `json:"web"`
	Sessions     sessionstats.Result `json:"sessions"`
	ProcessStats processstats.Result `json:"process_stats"`
	Licenses     licensestats.Result `json:"licenses"`
	FileDB       filedbstats.Result  `json:"file_db"`
	ErrorContext errorcontext.Result `json:"error_context"`
	Errors       []string            `json:"errors,omitempty"`
	Matches      []string            `json:"matches"`
}

// Build discovers matching files and parses every matched file exactly once.
// A file-level parsing failure is retained in Errors and does not prevent other
// files from being analyzed. Discovery and option errors are returned directly.
func Build(options Options) (OverviewResult, error) {
	return BuildContext(context.Background(), options)
}

// BuildContext discovers and parses matching files, stopping promptly when
// ctx is cancelled. On cancellation it returns ctx.Err(); Build retains the
// historical non-context behaviour.
func BuildContext(ctx context.Context, options Options) (OverviewResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return OverviewResult{}, err
	}
	if options.InputRoot == "" {
		return OverviewResult{}, fmt.Errorf("overview input root is required")
	}
	if options.Glob == "" {
		return OverviewResult{}, fmt.Errorf("overview glob is required")
	}
	if options.BucketInterval <= 0 {
		return OverviewResult{}, fmt.Errorf("overview bucket interval must be positive")
	}
	if options.TopN < 0 {
		return OverviewResult{}, fmt.Errorf("overview top N must not be negative")
	}
	if options.Workers < 0 {
		return OverviewResult{}, fmt.Errorf("overview workers must not be negative")
	}
	if options.MinDurationMicros < 0 {
		return OverviewResult{}, fmt.Errorf("overview minimum duration must not be negative")
	}
	if err := options.TimeRange.Validate(); err != nil {
		return OverviewResult{}, err
	}

	bucketer, err := stats.NewBucketer(options.BucketInterval)
	if err != nil {
		return OverviewResult{}, err
	}
	files, err := discovery.FilesContext(ctx, options.InputRoot, options.Glob)
	if err != nil {
		return OverviewResult{}, err
	}

	started := time.Now()
	result := OverviewResult{
		Meta:    Meta{StartedAt: started, InputRoot: options.InputRoot, Glob: options.Glob, FilesMatched: len(files)},
		Matches: append([]string(nil), files...),
	}
	collector, err := newCollector(bucketer, options)
	if err != nil {
		return OverviewResult{}, err
	}
	progress := Progress{FilesMatched: len(files), Status: "matched"}
	reportProgress(options.Progress, progress)
	if err := consumeFilesInOrder(ctx, files, normalizeWorkers(options.Workers), options.matches, collector, &result, &progress, options.Progress); err != nil {
		reportProgress(options.Progress, Progress{FilesMatched: progress.FilesMatched, FilesCompleted: progress.FilesCompleted, FilesFailed: progress.FilesFailed, EventsParsed: progress.EventsParsed, EventsAccepted: progress.EventsAccepted, BytesRead: progress.BytesRead, CurrentFile: progress.CurrentFile, Status: "canceled"})
		return OverviewResult{}, err
	}
	collector.finalize(&result)
	result.Meta.FinishedAt = time.Now()
	result.Meta.Duration = result.Meta.FinishedAt.Sub(started)
	return result, nil
}

func reportProgress(callback func(Progress), progress Progress) {
	if callback != nil {
		callback(progress)
	}
}

func (options Options) matches(event techlog.Event) bool {
	return event.DurationMicros >= options.MinDurationMicros &&
		options.TimeRange.Match(event.Timestamp) &&
		config.MatchAllFilters(event.Raw, options.Filters)
}

const eventBatchSize = 256

type parsedBatch struct {
	events []techlog.Event
	stats  techlog.ParseStats
	err    error
	done   bool
}

// consumeFilesInOrder parses up to workers files concurrently, but invokes
// every stateful analyzer only from this goroutine and in discovery order. A
// later file has a one-batch channel and blocks once it is full, bounding event
// memory to at most roughly two batches per worker while preserving deterministic
// trace and lock correlation.
func consumeFilesInOrder(ctx context.Context, files []string, workers int, match func(techlog.Event) bool, collector *collector, result *OverviewResult, progress *Progress, callback func(Progress)) error {
	streams := make([]<-chan parsedBatch, len(files))
	var workersGroup sync.WaitGroup
	next := 0
	start := func(index int) {
		stream := make(chan parsedBatch, 1)
		streams[index] = stream
		workersGroup.Add(1)
		go func() {
			defer workersGroup.Done()
			parseFileBatches(ctx, files[index], stream)
		}()
	}
	defer workersGroup.Wait()
	for next < len(files) && next < workers {
		start(next)
		next++
	}

	for index, file := range files {
		progress.CurrentFile = file
		progress.Status = "parsing"
		reportProgress(callback, *progress)
		for {
			var batch parsedBatch
			var open bool
			select {
			case <-ctx.Done():
				return ctx.Err()
			case batch, open = <-streams[index]:
			}
			if !open {
				break
			}
			for _, event := range batch.events {
				progress.EventsParsed++
				if match(event) {
					collector.add(event)
					progress.EventsAccepted++
				}
			}
			if len(batch.events) > 0 {
				reportProgress(callback, *progress)
			}
			if !batch.done {
				continue
			}
			addParseStats(&result.Quality, batch.stats)
			progress.BytesRead += batch.stats.BytesRead
			if batch.err != nil {
				result.Meta.FilesFailed++
				progress.FilesFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", file, batch.err))
				progress.Status = "failed"
			} else {
				result.Meta.FilesProcessed++
				progress.FilesCompleted++
				progress.Status = "completed"
			}
			reportProgress(callback, *progress)
		}
		if next < len(files) {
			start(next)
			next++
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func parseFileBatches(ctx context.Context, path string, output chan<- parsedBatch) {
	defer close(output)
	events := make([]techlog.Event, 0, eventBatchSize)
	flush := func() error {
		if len(events) == 0 {
			return nil
		}
		select {
		case output <- parsedBatch{events: events}:
		case <-ctx.Done():
			return ctx.Err()
		}
		events = make([]techlog.Event, 0, eventBatchSize)
		return nil
	}
	parseStats, parseErr := techlog.ParseFileContext(ctx, path, func(event techlog.Event) error {
		events = append(events, event)
		if len(events) == eventBatchSize {
			return flush()
		}
		return nil
	})
	if parseErr != nil {
		if ctx.Err() != nil {
			return
		}
		if err := flush(); err != nil {
			return
		}
		select {
		case output <- parsedBatch{stats: parseStats, err: parseErr, done: true}:
		case <-ctx.Done():
		}
		return
	}
	if err := flush(); err != nil {
		return
	}
	select {
	case output <- parsedBatch{stats: parseStats, done: true}:
	case <-ctx.Done():
	}
}

func normalizeWorkers(workers int) int {
	if workers == 0 {
		return 1
	}
	return workers
}

type aggregate struct {
	count    int64
	duration stats.NumericStats
}

func (a *aggregate) add(durationMicros int64) {
	a.count++
	a.duration.Add(float64(durationMicros))
}

func (a *aggregate) result() Aggregate {
	if a.count == 0 {
		// Count already distinguishes an empty series from a measured zero.
		// Keep the public report JSON-serializable instead of leaking the NaN
		// sentinel used by the lower-level statistics primitive.
		return Aggregate{}
	}
	return Aggregate{Count: a.count, Duration: a.duration.Finalize()}
}

type collector struct {
	bucketer   stats.Bucketer
	total      aggregate
	byEvent    map[string]*aggregate
	byBucket   map[time.Time]*aggregate
	byUser     map[string]*aggregate
	byDatabase map[string]*aggregate
	byProcess  map[string]*aggregate
	byContext  map[string]*aggregate
	top        *stats.TopN[RawEvent]
	sql        *sqlstats.Collector
	traces     *trace.Collector
	locks      *lockstats.Collector
	scall      *scallstats.Collector
	web        *webstats.Collector
	sessions   *sessionstats.Collector
	processes  *processstats.Collector
	licenses   *licensestats.Collector
	fileDB     *filedbstats.Collector
	errors     *errorcontext.Collector
}

func newCollector(bucketer stats.Bucketer, options Options) (*collector, error) {
	traces, err := trace.NewCollector(trace.Options{MaxOpenTraces: options.MaxOpenTraces, MaxTraces: options.MaxTraces})
	if err != nil {
		return nil, err
	}
	return &collector{
		bucketer: bucketer, byEvent: make(map[string]*aggregate), byBucket: make(map[time.Time]*aggregate),
		byUser: make(map[string]*aggregate), byDatabase: make(map[string]*aggregate), byProcess: make(map[string]*aggregate), byContext: make(map[string]*aggregate),
		top: stats.NewTopN(options.TopN, betterRawEvent), sql: sqlstats.NewCollector(), traces: traces,
		locks:     lockstats.NewCollector(lockstats.Options{SampleLimit: options.LockSampleLimit, TopConflictLimit: options.LockTopConflicts}),
		scall:     scallstats.NewCollector(scallstats.Options{SampleLimit: options.SCALLSampleLimit}),
		web:       webstats.NewCollector(webstats.Options{SampleLimit: options.WebSampleLimit}),
		sessions:  sessionstats.NewCollector(sessionstats.Options{SessionLimit: options.SessionLimit, SampleLimit: options.SessionSampleLimit}),
		processes: processstats.NewCollector(processstats.Options{SampleLimit: options.ProcessSampleLimit}),
		licenses:  licensestats.NewCollector(licensestats.Options{SampleLimit: options.LicenseSampleLimit}),
		fileDB:    filedbstats.NewCollector(filedbstats.Options{SlowSampleLimit: options.FileDBSlowSampleLimit, ErrorSampleLimit: options.FileDBErrorSampleLimit}),
		errors:    errorcontext.NewCollector(errorcontext.Options{Window: options.ErrorContextWindow, PendingLimit: options.ErrorContextPendingLimit, SampleLimit: options.ErrorContextSampleLimit}),
	}, nil
}

func (c *collector) add(event techlog.Event) {
	// Every parsed event enters each analyzer during this same callback; no
	// additional file scan is needed for SQL or trace results.
	c.total.add(event.DurationMicros)
	addGrouped(c.byEvent, event.Name, event.DurationMicros)
	addGrouped(c.byBucket, c.bucketer.Start(event.Timestamp), event.DurationMicros)
	addGrouped(c.byUser, dimension(event.Fields, "Usr"), event.DurationMicros)
	addGrouped(c.byDatabase, dimension(event.Fields, "DataBase"), event.DurationMicros)
	addGrouped(c.byProcess, dimension(event.Fields, "process"), event.DurationMicros)
	contextValue := strings.Join(strings.Fields(event.Fields["Context"]), " ")
	if contextValue == "" {
		contextValue = unknownDimension
	}
	addGrouped(c.byContext, contextValue, event.DurationMicros)
	c.top.Add(rawEvent(event))
	c.sql.Consume(event)
	c.traces.Consume(event)
	c.locks.Consume(event)
	c.scall.Consume(event)
	c.web.Consume(event)
	c.sessions.Consume(event)
	c.processes.Consume(event)
	c.licenses.Consume(event)
	c.fileDB.Consume(event)
	c.errors.Consume(event)
}

func addGrouped[K comparable](groups map[K]*aggregate, key K, durationMicros int64) {
	entry := groups[key]
	if entry == nil {
		entry = &aggregate{}
		groups[key] = entry
	}
	entry.add(durationMicros)
}

func dimension(fields map[string]string, key string) string {
	if value := fields[key]; value != "" {
		return value
	}
	return unknownDimension
}

func rawEvent(event techlog.Event) RawEvent {
	return RawEvent{Timestamp: event.Timestamp, Event: event.Name, Level: event.Level, DurationMicros: event.DurationMicros,
		Source: event.Source, Usr: event.Fields["Usr"], DataBase: event.Fields["DataBase"], Process: event.Fields["process"],
		Fields: cloneFields(event.Fields), Raw: event.Raw}
}

func cloneFields(fields map[string]string) map[string]string {
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		result[key] = value
	}
	return result
}

func betterRawEvent(a, b RawEvent) bool {
	if a.DurationMicros != b.DurationMicros {
		return a.DurationMicros > b.DurationMicros
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	return a.Event < b.Event
}

func (c *collector) finalize(result *OverviewResult) {
	result.Totals = c.total.result()
	result.EventTypes = eventRows(c.byEvent)
	result.Buckets = bucketRows(c.byBucket, c.bucketer)
	result.Users = dimensionRows(c.byUser)
	result.Databases = dimensionRows(c.byDatabase)
	result.Processes = dimensionRows(c.byProcess)
	result.Contexts = dimensionRows(c.byContext)
	result.TopEvents = c.top.Items()
	result.SQLRows = c.sql.Rows()
	traces := c.traces.Result()
	result.Traces = traces.Traces
	result.TraceQuality = traces.Quality
	result.Locks = c.locks.Result()
	result.SCALL = c.scall.Result()
	result.Web = c.web.Result()
	result.Sessions = c.sessions.Result()
	result.ProcessStats = c.processes.Result()
	result.Licenses = c.licenses.Result()
	result.FileDB = c.fileDB.Result()
	result.ErrorContext = c.errors.Result()
}

func eventRows(groups map[string]*aggregate) []EventTypeStat {
	result := make([]EventTypeStat, 0, len(groups))
	for name, value := range groups {
		result = append(result, EventTypeStat{Event: name, Stats: value.result()})
	}
	sort.Slice(result, func(i, j int) bool {
		return aggregateBefore(result[i].Stats, result[j].Stats, result[i].Event, result[j].Event)
	})
	return result
}

func bucketRows(groups map[time.Time]*aggregate, bucketer stats.Bucketer) []TimeBucketStat {
	result := make([]TimeBucketStat, 0, len(groups))
	for start, value := range groups {
		result = append(result, TimeBucketStat{Start: start, End: start.Add(bucketer.Interval()), Stats: value.result()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Start.Before(result[j].Start) })
	return result
}

func dimensionRows(groups map[string]*aggregate) []DimensionStat {
	result := make([]DimensionStat, 0, len(groups))
	for name, value := range groups {
		result = append(result, DimensionStat{Value: name, Stats: value.result()})
	}
	sort.Slice(result, func(i, j int) bool {
		return aggregateBefore(result[i].Stats, result[j].Stats, result[i].Value, result[j].Value)
	})
	return result
}

func aggregateBefore(a, b Aggregate, aName, bName string) bool {
	if a.Duration.Sum != b.Duration.Sum {
		return a.Duration.Sum > b.Duration.Sum
	}
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	return aName < bName
}

func addParseStats(quality *Quality, value techlog.ParseStats) {
	quality.BytesRead += value.BytesRead
	quality.LinesRead += value.LinesRead
	quality.EventsParsed += value.Events
	quality.MalformedHeaders += value.MalformedHeaders
	quality.OrphanLines += value.OrphanLines
}

// SourceName returns a portable display name for a raw event source.
func SourceName(event RawEvent) string { return filepath.Base(event.Source) }
