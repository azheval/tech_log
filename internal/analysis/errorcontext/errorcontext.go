// Package errorcontext conservatively joins EXCPCNTX records to EXCP and QERR.
package errorcontext

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

// Options bounds correlation state and retained raw error samples. Window is
// the maximum timestamp distance in either direction for a possible match.
type Options struct {
	Window       time.Duration `json:"window"`
	PendingLimit int           `json:"pending_limit"`
	SampleLimit  int           `json:"sample_limit"`
}

// Field is a deterministic context property attached to an enriched error.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Error is a bounded raw-error sample. Raw is retained only for samples, never
// in aggregate groups. Context is sourced solely from EXCPCNTX fields.
type Error struct {
	EventType   string    `json:"event_type"`
	Timestamp   time.Time `json:"timestamp"`
	Exception   string    `json:"exception"`
	Description string    `json:"description"`
	Module      string    `json:"module"`
	Source      string    `json:"source"`
	Process     string    `json:"process"`
	OSThread    string    `json:"os_thread"`
	User        string    `json:"user"`
	Database    string    `json:"database"`
	Context     []Field   `json:"context"`
	Raw         string    `json:"raw"`
}

// Group is a deterministic aggregate of finalized, normalized error records.
type Group struct {
	Signature   string `json:"signature"`
	Exception   string `json:"exception"`
	Description string `json:"description"`
	Module      string `json:"module"`
	Source      string `json:"source"`
	Process     string `json:"process"`
	User        string `json:"user"`
	Database    string `json:"database"`
	Count       int64  `json:"count"`
}

// ContextSample is an EXCPCNTX record that could not be safely correlated.
// Raw is intentionally excluded so only bounded error samples retain raw text.
type ContextSample struct {
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Process   string    `json:"process"`
	OSThread  string    `json:"os_thread"`
	Fields    []Field   `json:"fields"`
}

// Quality exposes matching coverage and bounded-retention losses.
type Quality struct {
	IgnoredEvents          int64 `json:"ignored_events"`
	ErrorEvents            int64 `json:"error_events"`
	ContextEvents          int64 `json:"context_events"`
	MatchedContexts        int64 `json:"matched_contexts"`
	OrphanContexts         int64 `json:"orphan_contexts"`
	AmbiguousContexts      int64 `json:"ambiguous_contexts"`
	MissingException       int64 `json:"missing_exception"`
	DroppedPendingErrors   int64 `json:"dropped_pending_errors"`
	DroppedPendingContexts int64 `json:"dropped_pending_contexts"`
	DiscardedSamples       int64 `json:"discarded_samples"`
}

// Result is a deterministic snapshot. Groups cover all finalized errors while
// Errors and Orphans retain only bounded samples.
type Result struct {
	Quality Quality         `json:"quality"`
	Groups  []Group         `json:"groups"`
	Errors  []Error         `json:"errors"`
	Orphans []ContextSample `json:"orphans"`
}

// Collector receives events in log order. It is not safe for concurrent use.
type Collector struct {
	window                    time.Duration
	pendingLimit, sampleLimit int
	quality                   Quality
	errors                    []*record
	pending                   []*contextRecord
	samples                   []Error
	orphans                   []ContextSample
	groups                    map[string]*Group
}
type record struct {
	value            Error
	context, anchors map[string]string
}
type contextRecord struct {
	at                      time.Time
	source, process, thread string
	fields                  map[string]string
}

// NewCollector creates an empty collector with conservative defaults.
func NewCollector(options Options) *Collector {
	window := time.Minute
	if options.Window > 0 {
		window = options.Window
	}
	pendingLimit, sampleLimit := 256, 100
	if options.PendingLimit > 0 {
		pendingLimit = options.PendingLimit
	}
	if options.SampleLimit > 0 {
		sampleLimit = options.SampleLimit
	}
	return &Collector{window: window, pendingLimit: pendingLimit, sampleLimit: sampleLimit, groups: make(map[string]*Group)}
}

// Consume accepts EXCP, QERR and EXCPCNTX. A context is joined only when there
// is exactly one nearest compatible error inside Window. It never guesses when
// equally-near candidates make the relationship ambiguous.
func (c *Collector) Consume(event techlog.Event) {
	c.finalizeExpired(event.Timestamp)
	switch event.Name {
	case "EXCP", "QERR":
		c.quality.ErrorEvents++
		record := &record{value: errorFrom(event), context: make(map[string]string), anchors: copyFields(event.Fields)}
		if record.value.Exception == "" {
			c.quality.MissingException++
		}
		c.errors = append(c.errors, record)
		c.matchPending()
		c.boundErrors()
	case "EXCPCNTX":
		c.quality.ContextEvents++
		context := contextFrom(event)
		switch c.attach(context) {
		case attached:
			c.quality.MatchedContexts++
		case ambiguous:
			c.quality.AmbiguousContexts++
			c.addOrphan(context, "ambiguous compatible errors")
		case none:
			c.pending = append(c.pending, context)
			c.boundPending()
		}
	default:
		c.quality.IgnoredEvents++
	}
}

// Result finalizes outstanding records at end-of-input and returns stable
// groups and bounded samples. Repeated calls are idempotent.
func (c *Collector) Result() Result {
	for _, record := range c.errors {
		c.finalize(record)
	}
	c.errors = nil
	for _, context := range c.pending {
		c.quality.OrphanContexts++
		c.addOrphan(context, "no compatible error within window")
	}
	c.pending = nil
	groups := make([]Group, 0, len(c.groups))
	for _, group := range c.groups {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Signature < groups[j].Signature
	})
	return Result{Quality: c.quality, Groups: groups, Errors: append([]Error(nil), c.samples...), Orphans: append([]ContextSample(nil), c.orphans...)}
}

func (c *Collector) finalizeExpired(now time.Time) {
	cutoff := now.Add(-c.window)
	keptErrors := c.errors[:0]
	for _, record := range c.errors {
		if record.value.Timestamp.Before(cutoff) {
			c.finalize(record)
		} else {
			keptErrors = append(keptErrors, record)
		}
	}
	c.errors = keptErrors
	keptContexts := c.pending[:0]
	for _, context := range c.pending {
		if context.at.Before(cutoff) {
			c.quality.OrphanContexts++
			c.addOrphan(context, "no compatible error within window")
		} else {
			keptContexts = append(keptContexts, context)
		}
	}
	c.pending = keptContexts
}
func (c *Collector) boundErrors() {
	for len(c.errors) > c.pendingLimit {
		c.quality.DroppedPendingErrors++
		c.finalize(c.errors[0])
		c.errors = c.errors[1:]
	}
}
func (c *Collector) boundPending() {
	for len(c.pending) > c.pendingLimit {
		c.quality.DroppedPendingContexts++
		c.quality.OrphanContexts++
		c.addOrphan(c.pending[0], "pending context retention limit")
		c.pending = c.pending[1:]
	}
}
func (c *Collector) matchPending() {
	kept := c.pending[:0]
	for _, context := range c.pending {
		switch c.attach(context) {
		case attached:
			c.quality.MatchedContexts++
		case ambiguous:
			c.quality.AmbiguousContexts++
			c.addOrphan(context, "ambiguous compatible errors")
		default:
			kept = append(kept, context)
		}
	}
	c.pending = kept
}

type match int

const (
	none match = iota
	attached
	ambiguous
)

func (c *Collector) attach(context *contextRecord) match {
	var nearest *record
	var distance time.Duration
	tie := false
	for _, candidate := range c.errors {
		if !compatible(candidate, context) {
			continue
		}
		d := candidate.value.Timestamp.Sub(context.at)
		if d < 0 {
			d = -d
		}
		if d > c.window {
			continue
		}
		if nearest == nil || d < distance {
			nearest, distance, tie = candidate, d, false
			continue
		}
		if d == distance {
			tie = true
		}
	}
	if nearest == nil {
		return none
	}
	if tie {
		return ambiguous
	}
	for key, value := range context.fields {
		if value != "" && nearest.context[key] == "" {
			nearest.context[key] = value
		}
	}
	enrich(&nearest.value, context)
	return attached
}

func compatible(error *record, context *contextRecord) bool {
	if error.value.Source == "" || context.source == "" || error.value.Source != context.source {
		return false
	}
	if context.process != "" && error.value.Process != "" && context.process != error.value.Process {
		return false
	}
	if context.thread != "" && error.value.OSThread != "" && context.thread != error.value.OSThread {
		return false
	}
	for _, key := range []string{"Context", "ClientID", "t:clientID", "SessionID", "ID"} {
		left, right := error.anchors[key], context.fields[key]
		if left != "" && right != "" && left != right {
			return false
		}
	}
	return true
}
func errorFrom(event techlog.Event) Error {
	return Error{EventType: event.Name, Timestamp: event.Timestamp, Exception: first(event.Fields, "Exception", "Error", "Err"), Description: first(event.Fields, "Descr", "Description", "Message", "Txt"), Module: first(event.Fields, "Module", "ModuleName", "SrcName"), Source: event.Source, Process: first(event.Fields, "process", "Process", "p:processName"), OSThread: first(event.Fields, "OSThread", "OsThread"), User: first(event.Fields, "Usr", "User", "UserName"), Database: first(event.Fields, "DataBase", "Database", "IB"), Raw: event.Raw}
}
func contextFrom(event techlog.Event) *contextRecord {
	return &contextRecord{at: event.Timestamp, source: event.Source, process: first(event.Fields, "process", "Process", "p:processName"), thread: first(event.Fields, "OSThread", "OsThread"), fields: copyFields(event.Fields)}
}
func first(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}
func copyFields(fields map[string]string) map[string]string {
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		if value = strings.TrimSpace(value); value != "" {
			result[key] = value
		}
	}
	return result
}
func enrich(error *Error, context *contextRecord) {
	if error.Process == "" {
		error.Process = context.process
	}
	if error.OSThread == "" {
		error.OSThread = context.thread
	}
	if error.Module == "" {
		error.Module = first(context.fields, "Module", "ModuleName", "SrcName")
	}
	if error.User == "" {
		error.User = first(context.fields, "Usr", "User", "UserName")
	}
	if error.Database == "" {
		error.Database = first(context.fields, "DataBase", "Database", "IB")
	}
	error.Context = fields(context.fields)
}
func fields(values map[string]string) []Field {
	result := make([]Field, 0, len(values))
	for key, value := range values {
		result = append(result, Field{Key: key, Value: value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func (c *Collector) finalize(record *record) {
	record.value.Context = fields(record.context)
	key := signature(record.value)
	group := c.groups[key]
	if group == nil {
		group = &Group{Signature: key, Exception: normalize(record.value.Exception), Description: normalize(record.value.Description), Module: normalize(record.value.Module), Source: normalize(record.value.Source), Process: normalize(record.value.Process), User: normalize(record.value.User), Database: normalize(record.value.Database)}
		c.groups[key] = group
	}
	group.Count++
	c.addSample(record.value)
}
func (c *Collector) addSample(error Error) {
	c.samples = append(c.samples, error)
	sort.Slice(c.samples, func(i, j int) bool { return sampleKey(c.samples[i]) < sampleKey(c.samples[j]) })
	if len(c.samples) > c.sampleLimit {
		c.samples = c.samples[:c.sampleLimit]
		c.quality.DiscardedSamples++
	}
}
func (c *Collector) addOrphan(context *contextRecord, reason string) {
	c.orphans = append(c.orphans, ContextSample{Timestamp: context.at, Reason: reason, Source: context.source, Process: context.process, OSThread: context.thread, Fields: fields(context.fields)})
	sort.Slice(c.orphans, func(i, j int) bool { return orphanKey(c.orphans[i]) < orphanKey(c.orphans[j]) })
	if len(c.orphans) > c.sampleLimit {
		c.orphans = c.orphans[:c.sampleLimit]
		c.quality.DiscardedSamples++
	}
}
func sampleKey(error Error) string {
	return error.Timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + error.EventType + "\x00" + error.Exception + "\x00" + error.Description + "\x00" + error.Module + "\x00" + error.Source + "\x00" + error.Process
}
func orphanKey(context ContextSample) string {
	key := context.Timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + context.Reason + "\x00" + context.Source + "\x00" + context.Process + "\x00" + context.OSThread
	for _, field := range context.Fields {
		key += "\x00" + field.Key + "=" + field.Value
	}
	return key
}

var uuidRE = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
var ipRE = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var dateRE = regexp.MustCompile(`\b(?:\d{4}[-/]\d{2}[-/]\d{2}|\d{2}[.]\d{2}[.]\d{4})(?:[ T]\d{2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?)?\b`)

func normalize(value string) string {
	value = uuidRE.ReplaceAllString(value, "<uuid>")
	value = ipRE.ReplaceAllString(value, "<ip>")
	value = dateRE.ReplaceAllString(value, "<date>")
	return strings.Join(strings.Fields(value), " ")
}
func signature(error Error) string {
	return strings.Join([]string{error.EventType, normalize(error.Exception), normalize(error.Description), normalize(error.Module), normalize(error.Source), normalize(error.Process), normalize(error.User), normalize(error.Database)}, "\x00")
}
