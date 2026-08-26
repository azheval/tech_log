// Package webstats collects VRS web request, response, and cache events.
// It correlates a response only when exactly one request is pending on its
// source/process/thread lane, so interleaved traffic is never guessed at.
package webstats

import (
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

const defaultSampleLimit = 20

type Options struct {
	SampleLimit int `json:"sample_limit"`
}
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
type RequestRow struct {
	Method             string        `json:"method"`
	URI                string        `json:"uri"`
	Status             string        `json:"status"`
	StatusClass        string        `json:"status_class"`
	Result             string        `json:"result"`
	Count              int64         `json:"count"`
	Stats              DurationStats `json:"stats"`
	RequestBytes       int64         `json:"request_bytes"`
	ResponseBytes      int64         `json:"response_bytes"`
	RequestsWithBytes  int64         `json:"requests_with_bytes"`
	ResponsesWithBytes int64         `json:"responses_with_bytes"`
}
type CacheRow struct {
	Method          string        `json:"method"`
	URI             string        `json:"uri"`
	Action          string        `json:"action"`
	Result          string        `json:"result"`
	Status          string        `json:"status"`
	Count           int64         `json:"count"`
	Stats           DurationStats `json:"stats"`
	Bytes           int64         `json:"bytes"`
	EventsWithBytes int64         `json:"events_with_bytes"`
}
type Sample struct {
	Timestamp      time.Time `json:"timestamp"`
	Source         string    `json:"source"`
	Method         string    `json:"method"`
	URI            string    `json:"uri"`
	Status         string    `json:"status"`
	Result         string    `json:"result"`
	DurationMicros int64     `json:"duration_micros"`
	RequestRaw     string    `json:"request_raw"`
	ResponseRaw    string    `json:"response_raw"`
}
type Quality struct {
	EventsConsumed       int64            `json:"events_consumed"`
	Requests             int64            `json:"requests"`
	Responses            int64            `json:"responses"`
	CacheEvents          int64            `json:"cache_events"`
	IgnoredEvents        int64            `json:"ignored_events"`
	MatchedResponses     int64            `json:"matched_responses"`
	OrphanResponses      int64            `json:"orphan_responses"`
	AmbiguousResponses   int64            `json:"ambiguous_responses"`
	OrphanCacheEvents    int64            `json:"orphan_cache_events"`
	AmbiguousCacheEvents int64            `json:"ambiguous_cache_events"`
	PendingRequests      int              `json:"pending_requests"`
	StatusClasses        map[string]int64 `json:"status_classes"`
	CacheHits            int64            `json:"cache_hits"`
	CacheMisses          int64            `json:"cache_misses"`
}
type Result struct {
	Quality      Quality      `json:"quality"`
	Requests     []RequestRow `json:"requests"`
	Cache        []CacheRow   `json:"cache"`
	SlowSamples  []Sample     `json:"slow_samples"`
	ErrorSamples []Sample     `json:"error_samples"`
}

type request struct {
	event             techlog.Event
	method, uri, lane string
	bytes             int64
	hasBytes          bool
}
type requestGroup struct {
	method, uri, status, result                                      string
	stats                                                            bucket
	requestBytes, responseBytes, requestWithBytes, responseWithBytes int64
}
type cacheGroup struct {
	method, uri, action, result, status string
	stats                               bucket
	bytes, withBytes                    int64
}
type bucket struct {
	count, total, min, max int64
	values                 []int64
}

// Collector is incremental and must be consumed in source order.
type Collector struct {
	limit        int
	pending      map[string][]*request
	recent       map[string][]*request
	requests     map[string]*requestGroup
	caches       map[string]*cacheGroup
	quality      Quality
	slow, errors []Sample
}

func NewCollector(options Options) *Collector {
	limit := options.SampleLimit
	if limit <= 0 {
		limit = defaultSampleLimit
	}
	return &Collector{limit: limit, pending: make(map[string][]*request), recent: make(map[string][]*request), requests: make(map[string]*requestGroup), caches: make(map[string]*cacheGroup), quality: Quality{StatusClasses: make(map[string]int64)}}
}

// Consume accepts VRSREQUEST, VRSRESPONSE, and VRSCACHE; all other events are ignored.
func (c *Collector) Consume(event techlog.Event) {
	c.quality.EventsConsumed++
	switch event.Name {
	case "VRSREQUEST":
		c.consumeRequest(event)
	case "VRSRESPONSE":
		c.consumeResponse(event)
	case "VRSCACHE":
		c.consumeCache(event)
	default:
		c.quality.IgnoredEvents++
	}
}
func (c *Collector) consumeRequest(event techlog.Event) {
	c.quality.Requests++
	r := &request{event: event, method: upper(field(event.Fields, "Method")), uri: NormalizeURI(field(event.Fields, "URI")), lane: lane(event)}
	r.bytes, r.hasBytes = bytesFrom(event.Fields)
	c.pending[r.lane] = append(c.pending[r.lane], r)
}
func (c *Collector) consumeResponse(event techlog.Event) {
	c.quality.Responses++
	matching := c.matchPending(event)
	if len(matching) != 1 {
		c.quality.OrphanResponses++
		if len(matching) > 1 {
			c.quality.AmbiguousResponses++
		}
		return
	}
	r := matching[0]
	c.removePending(r)
	c.remember(r)
	c.quality.MatchedResponses++
	status := field(event.Fields, "Status", "StatusCode", "statusCode")
	class := statusClass(status)
	if class != "" {
		c.quality.StatusClasses[class]++
	}
	result := field(event.Fields, "Result")
	duration := elapsed(r.event, event)
	responseBytes, hasResponseBytes := bytesFrom(event.Fields)
	key := strings.Join([]string{r.method, r.uri, status, result}, "\x00")
	g := c.requests[key]
	if g == nil {
		g = &requestGroup{method: r.method, uri: r.uri, status: status, result: result}
		c.requests[key] = g
	}
	g.stats.add(duration)
	if r.hasBytes {
		g.requestBytes += r.bytes
		g.requestWithBytes++
	}
	if hasResponseBytes {
		g.responseBytes += responseBytes
		g.responseWithBytes++
	}
	s := Sample{Timestamp: r.event.Timestamp, Source: r.event.Source, Method: r.method, URI: r.uri, Status: status, Result: result, DurationMicros: duration, RequestRaw: r.event.Raw, ResponseRaw: event.Raw}
	c.addSlow(s)
	if isErrorStatus(status) || strings.EqualFold(result, "error") {
		c.addError(s)
	}
}
func (c *Collector) consumeCache(event techlog.Event) {
	c.quality.CacheEvents++
	method := upper(field(event.Fields, "Method"))
	uri := NormalizeURI(field(event.Fields, "resource", "URI"))
	action := strings.ToLower(field(event.Fields, "action", "Action"))
	result := strings.ToLower(field(event.Fields, "Result"))
	status := field(event.Fields, "statusCode", "Status", "StatusCode")
	if result == "hit" {
		c.quality.CacheHits++
	}
	if result == "miss" {
		c.quality.CacheMisses++
	}
	if uri == "" || method == "" {
		c.quality.OrphanCacheEvents++
	} else {
		matches := c.matchCache(event, method, uri)
		if len(matches) != 1 {
			c.quality.OrphanCacheEvents++
			if len(matches) > 1 {
				c.quality.AmbiguousCacheEvents++
			}
		}
	}
	key := strings.Join([]string{method, uri, action, result, status}, "\x00")
	g := c.caches[key]
	if g == nil {
		g = &cacheGroup{method: method, uri: uri, action: action, result: result, status: status}
		c.caches[key] = g
	}
	g.stats.add(event.DurationMicros)
	if value, ok := bytesFrom(event.Fields); ok {
		g.bytes += value
		g.withBytes++
	}
}
func (c *Collector) matchPending(event techlog.Event) []*request {
	var result []*request
	for _, candidate := range c.pending[lane(event)] {
		if identityCompatible(candidate.event, event) {
			result = append(result, candidate)
		}
	}
	return result
}
func (c *Collector) remember(request *request) {
	items := append(c.recent[request.lane], request)
	// A small lane-local tail covers cache-create events emitted immediately
	// after responses without retaining the entire source stream.
	if len(items) > 16 {
		items = items[len(items)-16:]
	}
	c.recent[request.lane] = items
}
func (c *Collector) matchCache(event techlog.Event, method, uri string) []*request {
	var result []*request
	for _, candidate := range c.pending[lane(event)] {
		if candidate.method == method && candidate.uri == uri && identityCompatible(candidate.event, event) {
			result = append(result, candidate)
		}
	}
	if len(result) > 0 {
		return result
	}
	for _, candidate := range c.recent[lane(event)] {
		if candidate.method == method && candidate.uri == uri && identityCompatible(candidate.event, event) {
			result = append(result, candidate)
		}
	}
	return result
}
func (c *Collector) removePending(target *request) {
	items := c.pending[target.lane]
	for i, candidate := range items {
		if candidate == target {
			items = append(items[:i], items[i+1:]...)
			break
		}
	}
	if len(items) == 0 {
		delete(c.pending, target.lane)
	} else {
		c.pending[target.lane] = items
	}
}

// Result is a sorted snapshot that does not expose mutable state.
func (c *Collector) Result() Result {
	result := Result{Quality: c.quality}
	result.Quality.StatusClasses = cloneCounts(c.quality.StatusClasses)
	for _, g := range c.requests {
		result.Requests = append(result.Requests, RequestRow{Method: g.method, URI: g.uri, Status: g.status, StatusClass: statusClass(g.status), Result: g.result, Count: g.stats.count, Stats: g.stats.result(), RequestBytes: g.requestBytes, ResponseBytes: g.responseBytes, RequestsWithBytes: g.requestWithBytes, ResponsesWithBytes: g.responseWithBytes})
	}
	sort.Slice(result.Requests, func(i, j int) bool {
		if result.Requests[i].Stats.TotalMicros != result.Requests[j].Stats.TotalMicros {
			return result.Requests[i].Stats.TotalMicros > result.Requests[j].Stats.TotalMicros
		}
		return requestKey(result.Requests[i]) < requestKey(result.Requests[j])
	})
	for _, g := range c.caches {
		result.Cache = append(result.Cache, CacheRow{Method: g.method, URI: g.uri, Action: g.action, Result: g.result, Status: g.status, Count: g.stats.count, Stats: g.stats.result(), Bytes: g.bytes, EventsWithBytes: g.withBytes})
	}
	sort.Slice(result.Cache, func(i, j int) bool {
		if result.Cache[i].Stats.TotalMicros != result.Cache[j].Stats.TotalMicros {
			return result.Cache[i].Stats.TotalMicros > result.Cache[j].Stats.TotalMicros
		}
		return cacheKey(result.Cache[i]) < cacheKey(result.Cache[j])
	})
	result.SlowSamples = append([]Sample(nil), c.slow...)
	result.ErrorSamples = append([]Sample(nil), c.errors...)
	for _, items := range c.pending {
		result.Quality.PendingRequests += len(items)
	}
	return result
}

// NormalizeURI removes query values (which frequently carry IDs and tokens),
// preserves a stable lower-case path, and sorts parameter names.
func NormalizeURI(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "\"'")
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return normalizeURIText(value)
	}
	path := normalizePath(strings.ToLower(parsed.EscapedPath()))
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery == "" {
		return path
	}
	parts := strings.Split(parsed.RawQuery, "&")
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key, _, _ := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return path
	}
	return path + "?" + strings.Join(keys, "&")
}
func normalizeURIText(value string) string {
	before, query, ok := strings.Cut(value, "?")
	if !ok {
		return strings.ToLower(before)
	}
	return NormalizeURI(before + "?" + query)
}

func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if looksLikeID(part) {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}
func looksLikeID(value string) bool {
	if value == "" {
		return false
	}
	digits := true
	for _, char := range value {
		if char < '0' || char > '9' {
			digits = false
			break
		}
	}
	if digits {
		return true
	}
	// UUID-shaped values are identity-bearing even if a vendor uses upper case.
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
func (b *bucket) add(value int64) {
	if b.count == 0 {
		b.min = value
		b.max = value
	}
	b.count++
	b.total += value
	if value < b.min {
		b.min = value
	}
	if value > b.max {
		b.max = value
	}
	b.values = append(b.values, value)
}
func (b bucket) result() DurationStats {
	values := append([]int64(nil), b.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return DurationStats{Count: b.count, TotalMicros: b.total, MinMicros: b.min, MaxMicros: b.max, MeanMicros: float64(b.total) / float64(b.count), P50Micros: percentile(values, .5), P95Micros: percentile(values, .95), P99Micros: percentile(values, .99)}
}
func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(values)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(values) {
		i = len(values) - 1
	}
	return values[i]
}
func lane(event techlog.Event) string {
	return strings.Join([]string{event.Source, field(event.Fields, "process", "Process"), field(event.Fields, "OSThread", "Thread", "thread")}, "\x00")
}
func identityCompatible(left, right techlog.Event) bool {
	for _, key := range []string{"ClientID", "CallID", "Trans", "t:clientID", "ConnectionID", "SessionID"} {
		a, b := field(left.Fields, key), field(right.Fields, key)
		if a != "" && b != "" && a != b {
			return false
		}
	}
	return true
}
func elapsed(request, response techlog.Event) int64 {
	if !request.Timestamp.IsZero() && !response.Timestamp.IsZero() && !response.Timestamp.Before(request.Timestamp) {
		return response.Timestamp.Sub(request.Timestamp).Microseconds()
	}
	return response.DurationMicros
}
func field(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}
func upper(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }
func statusClass(status string) string {
	number, err := strconv.Atoi(status)
	if err != nil || number < 100 || number > 599 {
		return ""
	}
	return strconv.Itoa(number/100) + "xx"
}
func isErrorStatus(status string) bool { n, e := strconv.Atoi(status); return e == nil && n >= 400 }
func bytesFrom(fields map[string]string) (int64, bool) {
	for _, key := range []string{"Bytes", "BodyBytes", "ContentLength", "Content-Length", "InBytes", "OutBytes", "RequestBytes", "ResponseBytes"} {
		if n, ok := numberField(fields[key]); ok {
			return n, true
		}
	}
	headers := field(fields, "Headers", "Header")
	for _, line := range strings.FieldsFunc(headers, func(r rune) bool { return r == '\n' || r == '\r' }) {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			if n, ok := numberField(value); ok {
				return n, true
			}
		}
	}
	if body := field(fields, "Body"); body != "" {
		return int64(len(body)), true
	}
	return 0, false
}
func numberField(value string) (int64, bool) {
	n, e := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n, e == nil && n >= 0
}
func cloneCounts(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source))
	for k, v := range source {
		result[k] = v
	}
	return result
}
func requestKey(value RequestRow) string {
	return strings.Join([]string{value.Method, value.URI, value.Status, value.Result}, "\x00")
}
func cacheKey(value CacheRow) string {
	return strings.Join([]string{value.Method, value.URI, value.Action, value.Result, value.Status}, "\x00")
}
func (c *Collector) addSlow(sample Sample) {
	c.slow = append(c.slow, sample)
	sort.Slice(c.slow, func(i, j int) bool { return sampleLess(c.slow[i], c.slow[j]) })
	if len(c.slow) > c.limit {
		c.slow = c.slow[:c.limit]
	}
}
func (c *Collector) addError(sample Sample) {
	c.errors = append(c.errors, sample)
	sort.Slice(c.errors, func(i, j int) bool {
		if c.errors[i].Timestamp.Equal(c.errors[j].Timestamp) {
			return sampleKey(c.errors[i]) < sampleKey(c.errors[j])
		}
		return c.errors[i].Timestamp.Before(c.errors[j].Timestamp)
	})
	if len(c.errors) > c.limit {
		c.errors = c.errors[:c.limit]
	}
}
func sampleLess(a, b Sample) bool {
	if a.DurationMicros != b.DurationMicros {
		return a.DurationMicros > b.DurationMicros
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return sampleKey(a) < sampleKey(b)
}
func sampleKey(s Sample) string {
	return strings.Join([]string{s.Source, s.Method, s.URI, s.Status, s.Result, s.RequestRaw, s.ResponseRaw}, "\x00")
}
