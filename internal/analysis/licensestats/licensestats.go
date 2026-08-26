// Package licensestats aggregates LIC and HASP without retaining hardware or
// license inventories. It accepts only fields explicitly present in events.
package licensestats

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/techlog"
)

const defaultLimit = 20

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
type LicenseRow struct {
	Func      string        `json:"func"`
	Result    string        `json:"result"`
	Process   string        `json:"process"`
	User      string        `json:"user"`
	Count     int64         `json:"count"`
	Stats     DurationStats `json:"stats"`
	Acquire   int64         `json:"acquire"`
	Release   int64         `json:"release"`
	Success   int64         `json:"success"`
	Failure   int64         `json:"failure"`
	Expired   int64         `json:"expired"`
	WrongType int64         `json:"wrong_type"`
}
type HASPRow struct {
	Origin  string        `json:"origin"`
	Process string        `json:"process"`
	User    string        `json:"user"`
	Count   int64         `json:"count"`
	Stats   DurationStats `json:"stats"`
}

// SystemSummary intentionally has no host name, serial, network adapter, BIOS,
// or hardware identifiers. Values are recorded only when the TXT explicitly
// provided a safe OS/system field.
type SystemSummary struct {
	OSFamilies    map[string]int64 `json:"os_families"`
	SystemTypes   map[string]int64 `json:"system_types"`
	MemoryBuckets map[string]int64 `json:"memory_buckets"`
}
type Sample struct {
	Timestamp      time.Time `json:"timestamp"`
	Source         string    `json:"source"`
	Event          string    `json:"event"`
	Func           string    `json:"func"`
	Result         string    `json:"result"`
	Process        string    `json:"process"`
	User           string    `json:"user"`
	Classification string    `json:"classification"`
	Text           string    `json:"text"`
	DurationMicros int64     `json:"duration_micros"`
}
type Quality struct {
	EventsConsumed int64 `json:"events_consumed"`
	LicenseEvents  int64 `json:"license_events"`
	HASPEvents     int64 `json:"hasp_events"`
	IgnoredEvents  int64 `json:"ignored_events"`
	MissingFunc    int64 `json:"missing_func"`
	MissingResult  int64 `json:"missing_result"`
	MissingText    int64 `json:"missing_text"`
	HASPCache      int64 `json:"hasp_cache"`
	HASPOS         int64 `json:"hasp_os"`
	HASPUnknown    int64 `json:"hasp_unknown"`
	Expired        int64 `json:"expired"`
	WrongType      int64 `json:"wrong_type"`
	Failures       int64 `json:"failures"`
}
type Result struct {
	Quality      Quality       `json:"quality"`
	Licenses     []LicenseRow  `json:"licenses"`
	HASP         []HASPRow     `json:"hasp"`
	Systems      SystemSummary `json:"systems"`
	SlowSamples  []Sample      `json:"slow_samples"`
	ErrorSamples []Sample      `json:"error_samples"`
}

type bucket struct {
	count, total, min, max int64
	values                 []int64
}
type licenseBucket struct {
	fn, res, process, user                                 string
	stats                                                  bucket
	acquire, release, success, failure, expired, wrongType int64
}
type haspBucket struct {
	origin, process, user string
	stats                 bucket
}
type Collector struct {
	limit        int
	quality      Quality
	licenses     map[string]*licenseBucket
	hasp         map[string]*haspBucket
	systems      SystemSummary
	slow, errors []Sample
}

func NewCollector(options Options) *Collector {
	limit := options.SampleLimit
	if limit <= 0 {
		limit = defaultLimit
	}
	return &Collector{limit: limit, licenses: make(map[string]*licenseBucket), hasp: make(map[string]*haspBucket), systems: SystemSummary{OSFamilies: make(map[string]int64), SystemTypes: make(map[string]int64), MemoryBuckets: make(map[string]int64)}}
}
func (c *Collector) Consume(event techlog.Event) {
	c.quality.EventsConsumed++
	switch event.Name {
	case "LIC":
		c.license(event)
	case "HASP":
		c.hardlock(event)
	default:
		c.quality.IgnoredEvents++
	}
}
func (c *Collector) license(event techlog.Event) {
	c.quality.LicenseEvents++
	fn := field(event.Fields, "Func")
	res := strings.ToLower(field(event.Fields, "res", "Res", "Result"))
	process := field(event.Fields, "process", "Process")
	user := field(event.Fields, "Usr", "User")
	txt := field(event.Fields, "txt", "Txt")
	if fn == "" {
		c.quality.MissingFunc++
	}
	if res == "" {
		c.quality.MissingResult++
	}
	if txt == "" {
		c.quality.MissingText++
	}
	key := strings.Join([]string{fn, res, process, user}, "\x00")
	g := c.licenses[key]
	if g == nil {
		g = &licenseBucket{fn: fn, res: res, process: process, user: user}
		c.licenses[key] = g
	}
	g.stats.add(event.DurationMicros)
	classification := classify(res, txt)
	if classification.acquire {
		g.acquire++
	}
	if classification.release {
		g.release++
	}
	if classification.expired {
		g.expired++
		c.quality.Expired++
	}
	if classification.wrongType {
		g.wrongType++
		c.quality.WrongType++
	}
	if classification.failure {
		g.failure++
		c.quality.Failures++
	}
	if classification.success {
		g.success++
	}
	s := Sample{Timestamp: event.Timestamp, Source: event.Source, Event: event.Name, Func: fn, Result: res, Process: process, User: user, Classification: classification.label(), DurationMicros: event.DurationMicros, Text: redact(txt)}
	c.addSlow(s)
	if classification.failure || classification.expired || classification.wrongType {
		c.addError(s)
	}
}
func (c *Collector) hardlock(event techlog.Event) {
	c.quality.HASPEvents++
	process := field(event.Fields, "process", "Process")
	user := field(event.Fields, "Usr", "User")
	txt := field(event.Fields, "Txt", "txt")
	origin := haspOrigin(txt)
	switch origin {
	case "cache":
		c.quality.HASPCache++
	case "os":
		c.quality.HASPOS++
	default:
		c.quality.HASPUnknown++
	}
	key := strings.Join([]string{origin, process, user}, "\x00")
	g := c.hasp[key]
	if g == nil {
		g = &haspBucket{origin: origin, process: process, user: user}
		c.hasp[key] = g
	}
	g.stats.add(event.DurationMicros)
	c.safeSystem(txt)
}
func (c *Collector) safeSystem(txt string) {
	for _, line := range strings.Split(txt, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "OS_0":
			if v := osFamily(value); v != "" {
				c.systems.OSFamilies[v]++
			}
		case "Sys Type_0":
			if v := systemType(value); v != "" {
				c.systems.SystemTypes[v]++
			}
		case "Phis Mem_0":
			if n, e := strconv.ParseInt(value, 10, 64); e == nil && n >= 0 {
				c.systems.MemoryBuckets[memoryBucket(n)]++
			}
		}
	}
}
func (c *Collector) Result() Result {
	r := Result{Quality: c.quality, Systems: cloneSystems(c.systems), SlowSamples: append([]Sample(nil), c.slow...), ErrorSamples: append([]Sample(nil), c.errors...)}
	for _, g := range c.licenses {
		r.Licenses = append(r.Licenses, LicenseRow{Func: g.fn, Result: g.res, Process: g.process, User: g.user, Count: g.stats.count, Stats: g.stats.result(), Acquire: g.acquire, Release: g.release, Success: g.success, Failure: g.failure, Expired: g.expired, WrongType: g.wrongType})
	}
	sort.Slice(r.Licenses, func(i, j int) bool {
		if r.Licenses[i].Stats.TotalMicros != r.Licenses[j].Stats.TotalMicros {
			return r.Licenses[i].Stats.TotalMicros > r.Licenses[j].Stats.TotalMicros
		}
		return licenseKey(r.Licenses[i]) < licenseKey(r.Licenses[j])
	})
	for _, g := range c.hasp {
		r.HASP = append(r.HASP, HASPRow{Origin: g.origin, Process: g.process, User: g.user, Count: g.stats.count, Stats: g.stats.result()})
	}
	sort.Slice(r.HASP, func(i, j int) bool {
		if r.HASP[i].Stats.TotalMicros != r.HASP[j].Stats.TotalMicros {
			return r.HASP[i].Stats.TotalMicros > r.HASP[j].Stats.TotalMicros
		}
		return haspKey(r.HASP[i]) < haspKey(r.HASP[j])
	})
	return r
}

type state struct{ acquire, release, success, failure, expired, wrongType bool }

func classify(res, txt string) state {
	lower := strings.ToLower(txt)
	s := state{acquire: res == "seize" || strings.Contains(strings.ToLower(res), "acquire"), release: res == "release" || strings.Contains(strings.ToLower(res), "release"), expired: strings.Contains(lower, "expired"), wrongType: strings.Contains(lower, "incorrect license type") || strings.Contains(lower, "wrong license type")}
	s.failure = strings.Contains(lower, "failed") || strings.Contains(lower, "error") || strings.Contains(lower, "denied") || s.expired || s.wrongType
	s.success = s.acquire && !s.failure
	if s.acquire && strings.Count(lower, "soft, file://") > 1 { // several attempted licenses may end in a successful one
		lines := strings.Split(lower, "\n")
		last := lines[len(lines)-1]
		if !strings.Contains(last, "expired") && !strings.Contains(last, "incorrect license type") {
			s.success = true
		}
	}
	return s
}
func (s state) label() string {
	var parts []string
	if s.acquire {
		parts = append(parts, "acquire")
	}
	if s.release {
		parts = append(parts, "release")
	}
	if s.success {
		parts = append(parts, "success")
	}
	if s.failure {
		parts = append(parts, "failure")
	}
	if s.expired {
		parts = append(parts, "expired")
	}
	if s.wrongType {
		parts = append(parts, "wrong-type")
	}
	return strings.Join(parts, ",")
}
func haspOrigin(txt string) string {
	lower := strings.ToLower(txt)
	if strings.Contains(lower, "from cache") {
		return "cache"
	}
	if strings.Contains(lower, "from os") {
		return "os"
	}
	return "unknown"
}
func osFamily(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "linux"):
		return "Linux"
	case strings.Contains(lower, "mac"):
		return "macOS"
	default:
		return "other"
	}
}
func systemType(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "x64") || strings.Contains(lower, "64-bit"):
		return "x64"
	case strings.Contains(lower, "x86") || strings.Contains(lower, "32-bit"):
		return "x86"
	case strings.Contains(lower, "arm"):
		return "ARM"
	default:
		return "other"
	}
}
func memoryBucket(value int64) string {
	const gib = int64(1024 * 1024 * 1024)
	return strconv.FormatInt((value+gib-1)/gib, 10) + " GiB"
}
func (b *bucket) add(v int64) {
	if b.count == 0 {
		b.min = v
		b.max = v
	}
	b.count++
	b.total += v
	if v < b.min {
		b.min = v
	}
	if v > b.max {
		b.max = v
	}
	b.values = append(b.values, v)
}
func (b bucket) result() DurationStats {
	v := append([]int64(nil), b.values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return DurationStats{Count: b.count, TotalMicros: b.total, MinMicros: b.min, MaxMicros: b.max, MeanMicros: float64(b.total) / float64(b.count), P50Micros: percentile(v, .5), P95Micros: percentile(v, .95), P99Micros: percentile(v, .99)}
}
func percentile(v []int64, p float64) int64 {
	if len(v) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(v)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(v) {
		i = len(v) - 1
	}
	return v[i]
}
func field(f map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(f[k]); v != "" {
			return v
		}
	}
	return ""
}
func licenseKey(v LicenseRow) string {
	return strings.Join([]string{v.Func, v.Result, v.Process, v.User}, "\x00")
}
func haspKey(v HASPRow) string { return strings.Join([]string{v.Origin, v.Process, v.User}, "\x00") }
func cloneSystems(s SystemSummary) SystemSummary {
	return SystemSummary{OSFamilies: cloneMap(s.OSFamilies), SystemTypes: cloneMap(s.SystemTypes), MemoryBuckets: cloneMap(s.MemoryBuckets)}
}
func cloneMap(s map[string]int64) map[string]int64 {
	r := make(map[string]int64, len(s))
	for k, v := range s {
		r[k] = v
	}
	return r
}
func (c *Collector) addSlow(s Sample) {
	c.slow = append(c.slow, s)
	sort.Slice(c.slow, func(i, j int) bool {
		if c.slow[i].DurationMicros != c.slow[j].DurationMicros {
			return c.slow[i].DurationMicros > c.slow[j].DurationMicros
		}
		if !c.slow[i].Timestamp.Equal(c.slow[j].Timestamp) {
			return c.slow[i].Timestamp.Before(c.slow[j].Timestamp)
		}
		return sampleKey(c.slow[i]) < sampleKey(c.slow[j])
	})
	if len(c.slow) > c.limit {
		c.slow = c.slow[:c.limit]
	}
}
func (c *Collector) addError(s Sample) {
	c.errors = append(c.errors, s)
	sort.Slice(c.errors, func(i, j int) bool {
		if !c.errors[i].Timestamp.Equal(c.errors[j].Timestamp) {
			return c.errors[i].Timestamp.Before(c.errors[j].Timestamp)
		}
		return sampleKey(c.errors[i]) < sampleKey(c.errors[j])
	})
	if len(c.errors) > c.limit {
		c.errors = c.errors[:c.limit]
	}
}
func sampleKey(s Sample) string {
	return strings.Join([]string{s.Source, s.Event, s.Func, s.Result, s.Process, s.User, s.Text}, "\x00")
}

var filePath = regexp.MustCompile(`(?i)file://[^,;\s]+`)
var serial = regexp.MustCompile(`\b\d{10,}\b`)
var mac = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)

func redact(value string) string {
	value = filePath.ReplaceAllString(value, "file://[redacted].lic")
	value = serial.ReplaceAllString(value, "[id]")
	return mac.ReplaceAllString(value, "[mac]")
}
