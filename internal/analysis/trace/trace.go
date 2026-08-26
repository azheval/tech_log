// Package trace correlates related technological-log events into call traces.
package trace

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

const (
	defaultMaxOpenTraces = 10_000
	defaultMaxTraces     = 10_000
	// An EXCPCNTX can be written just before or just after the error it
	// describes. Keep enough history for that bounded local correlation, but
	// never turn an old, still-open CALL into an error-context candidate.
	errorContextWindow = time.Minute
)

// Options controls bounded retention. A zero limit selects a safe default.
type Options struct {
	MaxOpenTraces int
	MaxTraces     int
}

// Span is one event correlated to a Trace. Raw and Fields are retained for
// drill-down; Fields is an independent copy of the parser's map.
type Span struct {
	Timestamp      time.Time         `json:"timestamp"`
	Event          string            `json:"event"`
	DurationMicros int64             `json:"duration_micros"`
	Level          int               `json:"level"`
	Fields         map[string]string `json:"fields"`
	Raw            string            `json:"raw"`
}

// Trace is the chain beginning with CALL and its related Context, database,
// and error events.
type Trace struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Process   string    `json:"process,omitempty"`
	OSThread  string    `json:"os_thread,omitempty"`
	ClientID  string    `json:"client_id,omitempty"`
	CallID    string    `json:"call_id,omitempty"`
	Trans     string    `json:"trans,omitempty"`
	StartedAt time.Time `json:"started_at"`
	LastAt    time.Time `json:"last_at"`
	Spans     []Span    `json:"spans"`

	hasContext bool
	sequence   uint64
}

// Quality describes correlation outcomes and retention pressure.
type Quality struct {
	EventsConsumed          int64 `json:"events_consumed"`
	Calls                   int64 `json:"calls"`
	Contexts                int64 `json:"contexts"`
	ServerCalls             int64 `json:"server_calls"`
	VRSRequests             int64 `json:"vrs_requests"`
	VRSResponses            int64 `json:"vrs_responses"`
	CorrelatedVRS           int64 `json:"correlated_vrs"`
	AmbiguousVRS            int64 `json:"ambiguous_vrs"`
	ErrorContexts           int64 `json:"error_contexts"`
	CorrelatedErrorContexts int64 `json:"correlated_error_contexts"`
	AmbiguousErrorContexts  int64 `json:"ambiguous_error_contexts"`
	ExpiredErrorContexts    int64 `json:"expired_error_contexts"`
	DroppedErrorContexts    int64 `json:"dropped_error_contexts"`
	DroppedErrorCandidates  int64 `json:"dropped_error_candidates"`
	CorrelatedEvents        int64 `json:"correlated_events"`
	IgnoredEvents           int64 `json:"ignored_events"`
	OrphanEvents            int64 `json:"orphan_events"`
	MissingContextTraces    int64 `json:"missing_context_traces"`
	EvictedOpenTraces       int64 `json:"evicted_open_traces"`
	EvictedCompletedTraces  int64 `json:"evicted_completed_traces"`
	RetainedOpenTraces      int   `json:"retained_open_traces"`
	RetainedCompletedTraces int   `json:"retained_completed_traces"`
}

// Result is a deterministic snapshot of the collector state.
type Result struct {
	Traces  []Trace `json:"traces"`
	Quality Quality `json:"quality"`
}

// Collector incrementally correlates events. It is intentionally not safe for
// concurrent Consume calls; callers should preserve parser order.
type Collector struct {
	maxOpen int
	maxDone int
	next    uint64
	active  map[string]*Trace
	done    []*Trace
	// pendingErrorContexts and errorCandidates are both capped at maxOpen.
	// They are separate from active traces because EXCPCNTX may precede EXCP
	// and a CALL may complete while its error context is still within the
	// correlation window.
	pendingErrorContexts []techlog.Event
	errorCandidates      []errorCandidate
	quality              Quality
}

type errorCandidate struct {
	trace     *Trace
	spanIndex int
}

// NewCollector validates limits and returns an empty collector.
func NewCollector(options Options) (*Collector, error) {
	if options.MaxOpenTraces < 0 || options.MaxTraces < 0 {
		return nil, fmt.Errorf("trace retention limits must not be negative")
	}
	if options.MaxOpenTraces == 0 {
		options.MaxOpenTraces = defaultMaxOpenTraces
	}
	if options.MaxTraces == 0 {
		options.MaxTraces = defaultMaxTraces
	}
	return &Collector{maxOpen: options.MaxOpenTraces, maxDone: options.MaxTraces, active: make(map[string]*Trace)}, nil
}

// Consume processes one normalized event. CALL starts a trace. Context, SDBL,
// DBMSSQL, DBPOSTGRS, DBV8DBEng, SCALL, QERR and EXCP are attached to the best
// active trace matching source/process/OSThread and any available ClientID,
// CallID, or Trans values. VRS events deliberately use stricter matching: an
// explicit trace identifier, or a single compatible lane, is required.
func (c *Collector) Consume(event techlog.Event) {
	c.quality.EventsConsumed++
	switch event.Name {
	case "CALL":
		c.consumeCall(event)
	case "Context", "SDBL", "DBMSSQL", "DBPOSTGRS", "DBV8DBEng", "SCALL", "QERR", "EXCP":
		c.consumeRelated(event)
	case "EXCPCNTX":
		c.consumeErrorContext(event)
	case "VRSREQUEST", "VRSRESPONSE":
		c.consumeVRS(event)
	default:
		c.quality.IgnoredEvents++
	}
	// Events arrive in parser order. Resolving only after the current event
	// gives an error at either edge of the window a chance to participate.
	c.resolveExpiredErrorContexts(event.Timestamp)
	c.pruneErrorCandidates(event.Timestamp)
}

func (c *Collector) consumeCall(event techlog.Event) {
	c.quality.Calls++
	identity := makeIdentity(event)
	// A repeated complete identity marks a new call on the same execution lane.
	if previous := c.active[identity]; previous != nil {
		c.complete(previous)
	}
	c.next++
	trace := &Trace{
		ID:        fmt.Sprintf("%s#%d", identity, c.next),
		Source:    event.Source,
		Process:   event.Fields["process"],
		OSThread:  event.Fields["OSThread"],
		ClientID:  event.Fields["ClientID"],
		CallID:    event.Fields["CallID"],
		Trans:     event.Fields["Trans"],
		StartedAt: event.Timestamp,
		LastAt:    event.Timestamp,
		Spans:     []Span{span(event)},
		sequence:  c.next,
	}
	c.active[identity] = trace
	c.evictOpenIfNeeded()
}

func (c *Collector) consumeRelated(event techlog.Event) {
	if event.Name == "Context" {
		c.quality.Contexts++
	}
	if event.Name == "SCALL" {
		c.quality.ServerCalls++
	}
	trace := c.match(event)
	if trace == nil {
		c.quality.OrphanEvents++
		return
	}
	trace.Spans = append(trace.Spans, span(event))
	trace.LastAt = event.Timestamp
	if event.Name == "Context" {
		trace.hasContext = true
	}
	c.quality.CorrelatedEvents++
	if event.Name == "EXCP" || event.Name == "QERR" {
		c.addErrorCandidate(trace, len(trace.Spans)-1)
	}
}

func (c *Collector) consumeVRS(event techlog.Event) {
	if event.Name == "VRSREQUEST" {
		c.quality.VRSRequests++
	} else {
		c.quality.VRSResponses++
	}
	trace, ambiguous := c.matchReliable(event)
	if ambiguous {
		c.quality.AmbiguousVRS++
		c.quality.OrphanEvents++
		return
	}
	if trace == nil {
		c.quality.OrphanEvents++
		return
	}
	trace.Spans = append(trace.Spans, span(event))
	trace.LastAt = event.Timestamp
	c.quality.CorrelatedEvents++
	c.quality.CorrelatedVRS++
}

// VRSREQUEST and VRSRESPONSE do not have a stable, documented pairing schema
// in this package. We therefore do not correlate a request to a response by
// guessed field names. Each event may join a CALL only through the established
// trace identifiers, or when exactly one complete execution lane is open.
func (c *Collector) matchReliable(event techlog.Event) (*Trace, bool) {
	candidates := make([]*Trace, 0, 1)
	for _, candidate := range c.active {
		if !sameLane(candidate.Source, candidate.Process, candidate.OSThread, event) {
			continue
		}
		if eventHasTraceIdentifier(event) {
			score, matches := optionalMatch(candidate, event)
			if !matches || score == 0 {
				continue
			}
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 1 {
		return candidates[0], false
	}
	return nil, len(candidates) > 1
}

func eventHasTraceIdentifier(event techlog.Event) bool {
	return event.Fields["ClientID"] != "" || event.Fields["CallID"] != "" || event.Fields["Trans"] != ""
}

func (c *Collector) consumeErrorContext(event techlog.Event) {
	c.quality.ErrorContexts++
	// Context is copied because the parser owns event.Fields. The queue itself
	// is bounded by maxOpen in addPendingErrorContext.
	c.addPendingErrorContext(techlog.Event{Timestamp: event.Timestamp, Name: event.Name, Fields: cloneFields(event.Fields), Raw: event.Raw, Source: event.Source})
}

func (c *Collector) addErrorCandidate(trace *Trace, spanIndex int) {
	c.errorCandidates = append(c.errorCandidates, errorCandidate{trace: trace, spanIndex: spanIndex})
	for len(c.errorCandidates) > c.maxOpen {
		c.errorCandidates = c.errorCandidates[1:]
		c.quality.DroppedErrorCandidates++
	}
}

func (c *Collector) addPendingErrorContext(event techlog.Event) {
	c.pendingErrorContexts = append(c.pendingErrorContexts, event)
	for len(c.pendingErrorContexts) > c.maxOpen {
		c.pendingErrorContexts = c.pendingErrorContexts[1:]
		c.quality.DroppedErrorContexts++
		c.quality.OrphanEvents++
	}
}

func (c *Collector) resolveExpiredErrorContexts(now time.Time) {
	cutoff := now.Add(-errorContextWindow)
	kept := c.pendingErrorContexts[:0]
	for _, context := range c.pendingErrorContexts {
		if !context.Timestamp.Before(cutoff) {
			kept = append(kept, context)
			continue
		}
		c.resolveErrorContext(context)
	}
	c.pendingErrorContexts = kept
}

func (c *Collector) pruneErrorCandidates(now time.Time) {
	cutoff := now.Add(-2 * errorContextWindow)
	kept := c.errorCandidates[:0]
	for _, candidate := range c.errorCandidates {
		if candidate.trace.Spans[candidate.spanIndex].Timestamp.Before(cutoff) {
			continue
		}
		kept = append(kept, candidate)
	}
	c.errorCandidates = kept
}

func (c *Collector) resolveErrorContext(context techlog.Event) {
	var nearest *errorCandidate
	var distance time.Duration
	ambiguous := false
	for index := range c.errorCandidates {
		candidate := &c.errorCandidates[index]
		errorSpan := candidate.trace.Spans[candidate.spanIndex]
		if !errorContextCompatible(errorSpan, candidate.trace, context) {
			continue
		}
		d := errorSpan.Timestamp.Sub(context.Timestamp)
		if d < 0 {
			d = -d
		}
		if d > errorContextWindow {
			continue
		}
		if nearest == nil || d < distance {
			nearest, distance, ambiguous = candidate, d, false
		} else if d == distance {
			ambiguous = true
		}
	}
	if nearest == nil {
		c.quality.ExpiredErrorContexts++
		c.quality.OrphanEvents++
		return
	}
	if ambiguous {
		c.quality.AmbiguousErrorContexts++
		c.quality.OrphanEvents++
		return
	}
	errorSpan := &nearest.trace.Spans[nearest.spanIndex]
	mergeMissingFields(errorSpan.Fields, context.Fields)
	nearest.trace.Spans = append(nearest.trace.Spans, span(context))
	if context.Timestamp.After(nearest.trace.LastAt) {
		nearest.trace.LastAt = context.Timestamp
	}
	c.quality.CorrelatedEvents++
	c.quality.CorrelatedErrorContexts++
}

func errorContextCompatible(errorSpan Span, trace *Trace, context techlog.Event) bool {
	if !sameLane(trace.Source, trace.Process, trace.OSThread, context) {
		return false
	}
	for _, pair := range [][2]string{{trace.ClientID, context.Fields["ClientID"]}, {trace.CallID, context.Fields["CallID"]}, {trace.Trans, context.Fields["Trans"]}} {
		// A context that declares an execution identifier must agree with the
		// CALL owning the candidate error. Treat a missing CALL identifier as
		// incompatible rather than broadening the match to the whole lane.
		if pair[1] != "" && (pair[0] == "" || pair[0] != pair[1]) {
			return false
		}
	}
	for _, key := range []string{"ClientID", "CallID", "Trans", "Context", "t:clientID", "SessionID", "ID"} {
		left, right := errorSpan.Fields[key], context.Fields[key]
		if left != "" && right != "" && left != right {
			return false
		}
	}
	return true
}

func sameLane(source, process, thread string, event techlog.Event) bool {
	return source != "" && process != "" && thread != "" && source == event.Source && process == event.Fields["process"] && thread == event.Fields["OSThread"]
}

func mergeMissingFields(target, extra map[string]string) {
	for key, value := range extra {
		if value != "" && target[key] == "" {
			target[key] = value
		}
	}
}

func (c *Collector) match(event techlog.Event) *Trace {
	var best *Trace
	bestScore := -1
	for _, candidate := range c.active {
		if candidate.Source != event.Source || candidate.Process != event.Fields["process"] || candidate.OSThread != event.Fields["OSThread"] {
			continue
		}
		score, matches := optionalMatch(candidate, event)
		if !matches {
			continue
		}
		if best == nil || score > bestScore || (score == bestScore && candidate.sequence > best.sequence) {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func optionalMatch(candidate *Trace, event techlog.Event) (int, bool) {
	fields := [][2]string{{candidate.ClientID, event.Fields["ClientID"]}, {candidate.CallID, event.Fields["CallID"]}, {candidate.Trans, event.Fields["Trans"]}}
	score := 0
	for _, pair := range fields {
		if pair[1] == "" {
			continue
		}
		if pair[0] == "" || pair[0] != pair[1] {
			return 0, false
		}
		score++
	}
	return score, true
}

func (c *Collector) evictOpenIfNeeded() {
	for len(c.active) > c.maxOpen {
		var oldest *Trace
		var oldestIdentity string
		for identity, candidate := range c.active {
			if oldest == nil || candidate.sequence < oldest.sequence {
				oldest = candidate
				oldestIdentity = identity
			}
		}
		delete(c.active, oldestIdentity)
		c.quality.EvictedOpenTraces++
		c.appendDone(oldest)
	}
}

func (c *Collector) complete(trace *Trace) {
	delete(c.active, identityForTrace(trace))
	c.appendDone(trace)
}

func (c *Collector) appendDone(trace *Trace) {
	if !trace.hasContext {
		c.quality.MissingContextTraces++
	}
	c.done = append(c.done, trace)
	for len(c.done) > c.maxDone {
		c.done = c.done[1:]
		c.quality.EvictedCompletedTraces++
	}
}

// Result returns completed and still-open traces sorted by Start time and a
// monotonic sequence, independent of map iteration order.
func (c *Collector) Result() Result {
	// End-of-input makes every remaining context decidable. Clearing the queue
	// keeps Result idempotent.
	for _, context := range c.pendingErrorContexts {
		c.resolveErrorContext(context)
	}
	c.pendingErrorContexts = nil
	all := make([]*Trace, 0, len(c.done)+len(c.active))
	all = append(all, c.done...)
	for _, trace := range c.active {
		all = append(all, trace)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].StartedAt.Before(all[j].StartedAt)
		}
		return all[i].sequence < all[j].sequence
	})
	traces := make([]Trace, len(all))
	// MissingContextTraces already includes every completed trace (including
	// completed traces that were subsequently evicted). Add only still-open
	// traces here; counting all result traces would double-count completed ones.
	missingContext := c.quality.MissingContextTraces
	for _, trace := range c.active {
		if !trace.hasContext {
			missingContext++
		}
	}
	for i, trace := range all {
		traces[i] = cloneTrace(*trace)
	}
	quality := c.quality
	quality.MissingContextTraces = missingContext
	quality.RetainedOpenTraces = len(c.active)
	quality.RetainedCompletedTraces = len(c.done)
	return Result{Traces: traces, Quality: quality}
}

func span(event techlog.Event) Span {
	return Span{Timestamp: event.Timestamp, Event: event.Name, DurationMicros: event.DurationMicros, Level: event.Level, Fields: cloneFields(event.Fields), Raw: event.Raw}
}

func cloneTrace(source Trace) Trace {
	source.Spans = append([]Span(nil), source.Spans...)
	for index := range source.Spans {
		source.Spans[index].Fields = cloneFields(source.Spans[index].Fields)
	}
	return source
}

func cloneFields(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func makeIdentity(event techlog.Event) string {
	return strings.Join([]string{event.Source, event.Fields["process"], event.Fields["OSThread"], event.Fields["ClientID"], event.Fields["CallID"], event.Fields["Trans"]}, "\x1f")
}

func identityForTrace(trace *Trace) string {
	return strings.Join([]string{trace.Source, trace.Process, trace.OSThread, trace.ClientID, trace.CallID, trace.Trans}, "\x1f")
}
