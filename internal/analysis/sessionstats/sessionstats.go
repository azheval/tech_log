// Package sessionstats aggregates explicit SESN and CONN lifecycle records.
package sessionstats

import (
	"sort"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

// Options bounds retained investigation records. Zero values use safe defaults.
type Options struct {
	SessionLimit int `json:"session_limit"`
	SampleLimit  int `json:"sample_limit"`
}

// DurationStats describes paired session lifetimes in microseconds.
type DurationStats struct {
	Count       int64   `json:"count"`
	TotalMicros int64   `json:"total_micros"`
	MinMicros   int64   `json:"min_micros"`
	MaxMicros   int64   `json:"max_micros"`
	MeanMicros  float64 `json:"mean_micros"`
}

// Aggregate groups paired session durations by source event type.
type Aggregate struct {
	Key   string        `json:"key"`
	Stats DurationStats `json:"stats"`
}

// Dimension counts paired sessions that explicitly identify a property.
type Dimension struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// Session is an explicitly paired lifecycle. Duration is calculated only from
// Start and Finish timestamps, never from Event.DurationMicros.
type Session struct {
	EventType      string    `json:"event_type"`
	ID             string    `json:"id"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	DurationMicros int64     `json:"duration_micros"`
	DurationSource string    `json:"duration_source"`
	Confidence     string    `json:"confidence"`
	StartAction    string    `json:"start_action"`
	FinishAction   string    `json:"finish_action"`
	User           string    `json:"user"`
	Application    string    `json:"application"`
	Computer       string    `json:"computer"`
	Database       string    `json:"database"`
	Process        string    `json:"process"`
	Source         string    `json:"source"`
}

// Incomplete records an opening without a close, or an end without an opening.
type Incomplete struct {
	EventType   string    `json:"event_type"`
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Action      string    `json:"action"`
	Reason      string    `json:"reason"`
	User        string    `json:"user"`
	Application string    `json:"application"`
	Computer    string    `json:"computer"`
	Database    string    `json:"database"`
	Process     string    `json:"process"`
	Source      string    `json:"source"`
}

// TimelinePoint reports active sessions immediately after a lifecycle change.
type TimelinePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Active    int       `json:"active"`
}

// Quality describes lifecycle coverage and conservative handling of ambiguity.
type Quality struct {
	IgnoredEvents        int64 `json:"ignored_events"`
	LifecycleEvents      int64 `json:"lifecycle_events"`
	StartEvents          int64 `json:"start_events"`
	FinishEvents         int64 `json:"finish_events"`
	MissingID            int64 `json:"missing_id"`
	UnmatchedFinishes    int64 `json:"unmatched_finishes"`
	ReplacedOpenSessions int64 `json:"replaced_open_sessions"`
	OpenSessions         int64 `json:"open_sessions"`
	CompletedSessions    int64 `json:"completed_sessions"`
	DiscardedSessions    int64 `json:"discarded_sessions"`
	DiscardedSamples     int64 `json:"discarded_samples"`
}

// Result is a deterministic snapshot. Sessions and incomplete records are
// bounded; timeline and Peak cover every recognized lifecycle change.
type Result struct {
	Quality        Quality         `json:"quality"`
	Duration       DurationStats   `json:"duration"`
	ByEvent        []Aggregate     `json:"by_event"`
	ByUser         []Dimension     `json:"by_user"`
	ByApplication  []Dimension     `json:"by_application"`
	ByComputer     []Dimension     `json:"by_computer"`
	ByDatabase     []Dimension     `json:"by_database"`
	ByProcess      []Dimension     `json:"by_process"`
	Sessions       []Session       `json:"sessions"`
	Unclosed       []Incomplete    `json:"unclosed"`
	OrphanFinishes []Incomplete    `json:"orphan_finishes"`
	Timeline       []TimelinePoint `json:"timeline"`
	Peak           int             `json:"peak"`
}

// Collector consumes normalized events. It is intentionally not concurrent.
type Collector struct {
	sessionLimit, sampleLimit int
	quality                   Quality
	open                      map[string]openSession
	completed                 []Session
	unclosed, orphans         []Incomplete
	changes                   []change
	durations                 map[string]*durationBucket
	users, applications       map[string]int64
	computers, databases      map[string]int64
	processes                 map[string]int64
}
type openSession struct{ Session }
type change struct {
	at    time.Time
	delta int
	key   string
}
type durationBucket struct{ count, total, min, max int64 }

// NewCollector creates an empty collector.
func NewCollector(options Options) *Collector {
	sessionLimit, sampleLimit := 100, 100
	if options.SessionLimit > 0 {
		sessionLimit = options.SessionLimit
	}
	if options.SampleLimit > 0 {
		sampleLimit = options.SampleLimit
	}
	return &Collector{sessionLimit: sessionLimit, sampleLimit: sampleLimit, open: make(map[string]openSession), durations: make(map[string]*durationBucket), users: make(map[string]int64), applications: make(map[string]int64), computers: make(map[string]int64), databases: make(map[string]int64), processes: make(map[string]int64)}
}

// Consume recognizes only explicit start/open and finish/close records in SESN
// and CONN. SESN Attach is deliberately not treated as a boundary: its event
// duration does not establish a session lifetime.
func (c *Collector) Consume(event techlog.Event) {
	if event.Name != "SESN" && event.Name != "CONN" {
		c.quality.IgnoredEvents++
		return
	}
	action, kind := lifecycle(event)
	if kind == "" {
		return
	}
	c.quality.LifecycleEvents++
	id := identity(event)
	if id == "" {
		c.quality.MissingID++
		return
	}
	key := event.Name + "\x00" + id
	base := sessionFrom(event, id, action)
	if kind == "start" {
		c.quality.StartEvents++
		if old, exists := c.open[key]; exists {
			c.quality.ReplacedOpenSessions++
			c.addUnclosed(incomplete(old.Session, "replaced by subsequent open"))
			c.changes = append(c.changes, change{at: event.Timestamp, delta: -1, key: key})
		}
		c.open[key] = openSession{base}
		c.changes = append(c.changes, change{at: event.Timestamp, delta: 1, key: key})
		return
	}
	c.quality.FinishEvents++
	start, exists := c.open[key]
	if !exists {
		c.quality.UnmatchedFinishes++
		c.addOrphan(incomplete(base, "finish without observed open"))
		return
	}
	if event.Timestamp.Before(start.StartedAt) {
		c.quality.UnmatchedFinishes++
		c.addOrphan(incomplete(base, "finish timestamp precedes open"))
		return
	}
	delete(c.open, key)
	paired := start.Session
	paired.FinishedAt = event.Timestamp
	paired.DurationMicros = event.Timestamp.Sub(start.StartedAt).Microseconds()
	paired.DurationSource, paired.Confidence, paired.FinishAction = "timestamp_pair", "high", action
	mergeDimensions(&paired, base)
	c.quality.CompletedSessions++
	c.addDuration(paired)
	c.addDimensions(paired)
	c.addCompleted(paired)
	c.changes = append(c.changes, change{at: event.Timestamp, delta: -1, key: key})
}

// Result returns stable, bounded records and a timestamp-sorted concurrency
// timeline. Equal timestamps apply closes before opens, preventing artificial
// peaks while an identity is replaced.
func (c *Collector) Result() Result {
	quality := c.quality
	openKeys := make([]string, 0, len(c.open))
	for key := range c.open {
		openKeys = append(openKeys, key)
	}
	sort.Strings(openKeys)
	unclosed := append([]Incomplete(nil), c.unclosed...)
	for _, key := range openKeys {
		unclosed = append(unclosed, incomplete(c.open[key].Session, "not closed before end of input"))
	}
	unclosed = boundedIncomplete(unclosed, c.sampleLimit)
	quality.OpenSessions = int64(len(c.open))
	result := Result{Quality: quality, Sessions: append([]Session(nil), c.completed...), Unclosed: unclosed, OrphanFinishes: append([]Incomplete(nil), c.orphans...)}
	result.Duration, result.ByEvent = durationResult(c.durations)
	result.ByUser = dimensions(c.users)
	result.ByApplication = dimensions(c.applications)
	result.ByComputer = dimensions(c.computers)
	result.ByDatabase = dimensions(c.databases)
	result.ByProcess = dimensions(c.processes)
	result.Timeline, result.Peak = timeline(c.changes)
	return result
}

func lifecycle(event techlog.Event) (string, string) {
	values := []string{event.Fields["action"], event.Fields["Action"], event.Fields["Func"], event.Fields["Txt"], event.Fields["txt"]}
	for _, raw := range values {
		v := strings.ToLower(strings.Trim(strings.TrimSpace(raw), "'\""))
		if isStart(v) {
			return actionName(raw), "start"
		}
		if isFinish(v) {
			return actionName(raw), "finish"
		}
	}
	return "", ""
}
func isStart(v string) bool {
	return v == "start" || v == "open" || v == "restore" || strings.HasPrefix(v, "connected,") || strings.HasPrefix(v, "connected ") || strings.Contains(v, "connection opened")
}
func isFinish(v string) bool {
	return v == "finish" || v == "close" || v == "closed" || v == "end" || v == "detach" || strings.HasPrefix(v, "disconnected") || strings.Contains(v, "connection closed") || strings.HasPrefix(v, "closed,")
}
func actionName(value string) string { return strings.TrimSpace(strings.Trim(value, "'\"")) }

func identity(event techlog.Event) string {
	for _, key := range []string{"ID", "SessionID", "SesnID", "ConnectionID", "ConnID", "ClientID", "t:clientID"} {
		if value := strings.TrimSpace(event.Fields[key]); value != "" {
			return value
		}
	}
	return ""
}
func field(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}
func sessionFrom(event techlog.Event, id, action string) Session {
	return Session{EventType: event.Name, ID: id, StartedAt: event.Timestamp, StartAction: action, User: field(event.Fields, "Usr", "User", "UserName"), Application: field(event.Fields, "Appl", "Application", "applicationName", "t:applicationName"), Computer: field(event.Fields, "Computer", "ComputerName", "computerName", "t:computerName"), Database: field(event.Fields, "DataBase", "Database", "IB"), Process: field(event.Fields, "process", "Process", "p:processName"), Source: event.Source}
}
func mergeDimensions(target *Session, finish Session) {
	if target.User == "" {
		target.User = finish.User
	}
	if target.Application == "" {
		target.Application = finish.Application
	}
	if target.Computer == "" {
		target.Computer = finish.Computer
	}
	if target.Database == "" {
		target.Database = finish.Database
	}
	if target.Process == "" {
		target.Process = finish.Process
	}
	if target.Source == "" {
		target.Source = finish.Source
	}
}
func incomplete(session Session, reason string) Incomplete {
	return Incomplete{EventType: session.EventType, ID: session.ID, Timestamp: session.StartedAt, Action: session.StartAction, Reason: reason, User: session.User, Application: session.Application, Computer: session.Computer, Database: session.Database, Process: session.Process, Source: session.Source}
}
func (c *Collector) addCompleted(value Session) {
	c.completed = append(c.completed, value)
	sort.Slice(c.completed, func(i, j int) bool {
		if c.completed[i].DurationMicros != c.completed[j].DurationMicros {
			return c.completed[i].DurationMicros > c.completed[j].DurationMicros
		}
		return sessionKey(c.completed[i]) < sessionKey(c.completed[j])
	})
	if len(c.completed) > c.sessionLimit {
		c.completed = c.completed[:c.sessionLimit]
		c.quality.DiscardedSessions++
	}
}
func (c *Collector) addUnclosed(value Incomplete) {
	c.unclosed = append(c.unclosed, value)
	sort.Slice(c.unclosed, func(i, j int) bool { return incompleteKey(c.unclosed[i]) < incompleteKey(c.unclosed[j]) })
	if len(c.unclosed) > c.sampleLimit {
		c.unclosed = c.unclosed[:c.sampleLimit]
		c.quality.DiscardedSamples++
	}
}
func (c *Collector) addOrphan(value Incomplete) {
	c.orphans = append(c.orphans, value)
	sort.Slice(c.orphans, func(i, j int) bool { return incompleteKey(c.orphans[i]) < incompleteKey(c.orphans[j]) })
	if len(c.orphans) > c.sampleLimit {
		c.orphans = c.orphans[:c.sampleLimit]
		c.quality.DiscardedSamples++
	}
}
func sessionKey(v Session) string {
	return v.EventType + "\x00" + v.ID + "\x00" + v.StartedAt.UTC().Format(time.RFC3339Nano) + "\x00" + v.Source
}
func incompleteKey(v Incomplete) string {
	return v.EventType + "\x00" + v.ID + "\x00" + v.Timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + v.Reason + "\x00" + v.Source
}
func boundedIncomplete(values []Incomplete, limit int) []Incomplete {
	sort.Slice(values, func(i, j int) bool { return incompleteKey(values[i]) < incompleteKey(values[j]) })
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func (c *Collector) addDuration(session Session) {
	bucket := c.durations[session.EventType]
	if bucket == nil {
		bucket = &durationBucket{min: session.DurationMicros, max: session.DurationMicros}
		c.durations[session.EventType] = bucket
	}
	bucket.count++
	bucket.total += session.DurationMicros
	if session.DurationMicros < bucket.min {
		bucket.min = session.DurationMicros
	}
	if session.DurationMicros > bucket.max {
		bucket.max = session.DurationMicros
	}
}
func (c *Collector) addDimensions(session Session) {
	addDimension(c.users, session.User)
	addDimension(c.applications, session.Application)
	addDimension(c.computers, session.Computer)
	addDimension(c.databases, session.Database)
	addDimension(c.processes, session.Process)
}
func addDimension(values map[string]int64, key string) {
	if key != "" {
		values[key]++
	}
}
func durationResult(groups map[string]*durationBucket) (DurationStats, []Aggregate) {
	rows := make([]Aggregate, 0, len(groups))
	all := durationBucket{}
	for key, bucket := range groups {
		rows = append(rows, Aggregate{Key: key, Stats: durationBucketStats(bucket)})
		if all.count == 0 {
			all.min, all.max = bucket.min, bucket.max
		} else {
			if bucket.min < all.min {
				all.min = bucket.min
			}
			if bucket.max > all.max {
				all.max = bucket.max
			}
		}
		all.count += bucket.count
		all.total += bucket.total
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Stats.TotalMicros != rows[j].Stats.TotalMicros {
			return rows[i].Stats.TotalMicros > rows[j].Stats.TotalMicros
		}
		return rows[i].Key < rows[j].Key
	})
	return durationBucketStats(&all), rows
}
func durationBucketStats(bucket *durationBucket) DurationStats {
	if bucket == nil || bucket.count == 0 {
		return DurationStats{}
	}
	s := DurationStats{Count: bucket.count, TotalMicros: bucket.total, MinMicros: bucket.min, MaxMicros: bucket.max}
	s.MeanMicros = float64(s.TotalMicros) / float64(s.Count)
	return s
}
func dimensions(values map[string]int64) []Dimension {
	result := make([]Dimension, 0, len(values))
	for key, count := range values {
		result = append(result, Dimension{Key: key, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Key < result[j].Key
	})
	return result
}
func timeline(changes []change) ([]TimelinePoint, int) {
	changes = append([]change(nil), changes...)
	sort.Slice(changes, func(i, j int) bool {
		if !changes[i].at.Equal(changes[j].at) {
			return changes[i].at.Before(changes[j].at)
		}
		if changes[i].delta != changes[j].delta {
			return changes[i].delta < changes[j].delta
		}
		return changes[i].key < changes[j].key
	})
	active, peak := 0, 0
	points := make([]TimelinePoint, 0, len(changes))
	for _, change := range changes {
		active += change.delta
		if active < 0 {
			active = 0
		}
		if active > peak {
			peak = active
		}
		points = append(points, TimelinePoint{Timestamp: change.at, Active: active})
	}
	return points, peak
}
