// Package compare compares independently built overview reports.
package compare

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"techlog-stat/internal/analysis/errorcontext"
	"techlog-stat/internal/analysis/filedbstats"
	"techlog-stat/internal/analysis/licensestats"
	"techlog-stat/internal/analysis/processstats"
	"techlog-stat/internal/analysis/scallstats"
	"techlog-stat/internal/analysis/sessionstats"
	"techlog-stat/internal/analysis/sqlstats"
	"techlog-stat/internal/analysis/webstats"
	"techlog-stat/internal/report/overview"
	"techlog-stat/internal/stats"
)

const defaultThresholdPercent = 5

// Classification describes the material change from baseline to current.
type Classification string

const (
	Unchanged Classification = "unchanged"
	New       Classification = "new"
	Removed   Classification = "removed"
	Regressed Classification = "regressed"
	Improved  Classification = "improved"
)

// SQLFingerprint is optional pre-aggregated SQL data. Overview does not yet
// expose SQL fingerprints, but accepting it here keeps Compare useful as soon
// as an SQL analyzer supplies them.
type SQLFingerprint struct {
	Fingerprint string             `json:"fingerprint"`
	Stats       overview.Aggregate `json:"stats"`
}

// Options controls classifications. Zero thresholds use a 5% default, while a
// zero MinAbsoluteDeltaMicros means every non-zero duration change is eligible.
type Options struct {
	RegressionPercent       float64
	ImprovementPercent      float64
	P95RegressionPercent    float64
	P95ImprovementPercent   float64
	CountRegressionPercent  float64
	CountImprovementPercent float64
	MinAbsoluteDeltaMicros  float64
	BaseSQLFingerprints     []SQLFingerprint
	CurrentSQLFingerprints  []SQLFingerprint
}

// MetricDelta compares one numerical metric. PercentDelta is nil when the
// baseline is zero, so JSON consumers never receive Infinity or NaN.
type MetricDelta struct {
	Baseline      float64  `json:"baseline"`
	Current       float64  `json:"current"`
	AbsoluteDelta float64  `json:"absolute_delta"`
	PercentDelta  *float64 `json:"percent_delta,omitempty"`
}

// Reason identifies a metric that materially changed. Count changes are kept
// as reasons but do not alone mark performance as regressed or improved.
type Reason struct {
	Metric         string         `json:"metric"`
	Classification Classification `json:"classification"`
}

// Change is a comparison of the sum of duration microseconds for one key.
// PercentDelta is nil when Baseline has a zero duration sum, avoiding an
// infinity or a misleading divide-by-zero result in serialized output.
type Change struct {
	Key            string             `json:"key"`
	Baseline       overview.Aggregate `json:"baseline"`
	Current        overview.Aggregate `json:"current"`
	AbsoluteDelta  float64            `json:"absolute_delta_micros"`
	PercentDelta   *float64           `json:"percent_delta,omitempty"`
	CountDelta     MetricDelta        `json:"count_delta"`
	DurationDelta  MetricDelta        `json:"duration_delta_micros"`
	P95Delta       MetricDelta        `json:"p95_delta_micros"`
	Reasons        []Reason           `json:"reasons,omitempty"`
	Classification Classification     `json:"classification"`
}

// Result groups changes by the matching overview section.
type Result struct {
	Totals          Change   `json:"totals"`
	EventTypes      []Change `json:"event_types"`
	Users           []Change `json:"users"`
	Databases       []Change `json:"databases"`
	Processes       []Change `json:"processes"`
	SQLFingerprints []Change `json:"sql_fingerprints,omitempty"`
	SCALLByCall     []Change `json:"scall_by_call,omitempty"`
	WebRequests     []Change `json:"web_requests,omitempty"`
	SessionByEvent  []Change `json:"session_by_event,omitempty"`
	PROCByProcess   []Change `json:"proc_by_process,omitempty"`
	SCOMByOperation []Change `json:"scom_by_operation,omitempty"`
	Licenses        []Change `json:"licenses,omitempty"`
	ErrorGroups     []Change `json:"error_context_groups,omitempty"`
	FileDBByFunc    []Change `json:"file_db_by_func,omitempty"`
}

// Compare compares totals and all key-based overview groups. SQL fingerprints
// are compared only when supplied in Options. Invalid thresholds are replaced
// with their conservative defaults; callers that need strict validation can
// call ValidateOptions before Compare.
func Compare(base, current overview.OverviewResult, options Options) Result {
	if err := ValidateOptions(options); err != nil {
		options = Options{
			BaseSQLFingerprints:    options.BaseSQLFingerprints,
			CurrentSQLFingerprints: options.CurrentSQLFingerprints,
		}
	}
	options = normalizeOptions(options)
	return Result{
		Totals:          compareAggregate("total", base.Totals, current.Totals, options),
		EventTypes:      compareEventTypes(base.EventTypes, current.EventTypes, options),
		Users:           compareDimensions(base.Users, current.Users, options),
		Databases:       compareDimensions(base.Databases, current.Databases, options),
		Processes:       compareDimensions(base.Processes, current.Processes, options),
		SQLFingerprints: compareSQL(base.SQLRows, current.SQLRows, options),
		SCALLByCall:     compareSCALLCalls(base.SCALL.ByCall, current.SCALL.ByCall, options),
		WebRequests:     compareWebRequests(base.Web.Requests, current.Web.Requests, options),
		SessionByEvent:  compareSessionEvents(base.Sessions.ByEvent, current.Sessions.ByEvent, options),
		PROCByProcess:   compareProcessAggregates(base.ProcessStats.PROCByProcess, current.ProcessStats.PROCByProcess, options),
		SCOMByOperation: compareProcessAggregates(base.ProcessStats.SCOMByOperation, current.ProcessStats.SCOMByOperation, options),
		Licenses:        compareLicenses(base.Licenses.Licenses, current.Licenses.Licenses, options),
		ErrorGroups:     compareErrorGroups(base.ErrorContext.Groups, current.ErrorContext.Groups, options),
		FileDBByFunc:    compareFileDBAggregates(base.FileDB.ByFunc, current.FileDB.ByFunc, options),
	}
}

// ValidateOptions reports invalid numeric thresholds.
func ValidateOptions(options Options) error {
	for _, value := range []float64{options.RegressionPercent, options.ImprovementPercent, options.P95RegressionPercent, options.P95ImprovementPercent, options.CountRegressionPercent, options.CountImprovementPercent, options.MinAbsoluteDeltaMicros} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("comparison thresholds must be finite and non-negative")
		}
	}
	return nil
}

func normalizeOptions(options Options) Options {
	if options.RegressionPercent == 0 {
		options.RegressionPercent = defaultThresholdPercent
	}
	if options.ImprovementPercent == 0 {
		options.ImprovementPercent = defaultThresholdPercent
	}
	if options.P95RegressionPercent == 0 {
		options.P95RegressionPercent = options.RegressionPercent
	}
	if options.P95ImprovementPercent == 0 {
		options.P95ImprovementPercent = options.ImprovementPercent
	}
	if options.CountRegressionPercent == 0 {
		options.CountRegressionPercent = options.RegressionPercent
	}
	if options.CountImprovementPercent == 0 {
		options.CountImprovementPercent = options.ImprovementPercent
	}
	return options
}

func compareEventTypes(base, current []overview.EventTypeStat, options Options) []Change {
	baseValues := make(map[string]overview.Aggregate, len(base))
	currentValues := make(map[string]overview.Aggregate, len(current))
	for _, row := range base {
		baseValues[row.Event] = row.Stats
	}
	for _, row := range current {
		currentValues[row.Event] = row.Stats
	}
	return compareMaps(baseValues, currentValues, options)
}

func compareDimensions(base, current []overview.DimensionStat, options Options) []Change {
	baseValues := make(map[string]overview.Aggregate, len(base))
	currentValues := make(map[string]overview.Aggregate, len(current))
	for _, row := range base {
		baseValues[row.Value] = row.Stats
	}
	for _, row := range current {
		currentValues[row.Value] = row.Stats
	}
	return compareMaps(baseValues, currentValues, options)
}

func compareSQL(baseRows, currentRows []sqlstats.Row, options Options) []Change {
	baseValues := make(map[string]overview.Aggregate, len(baseRows))
	currentValues := make(map[string]overview.Aggregate, len(currentRows))
	if len(baseRows) > 0 || len(currentRows) > 0 {
		for _, row := range baseRows {
			baseValues[row.Fingerprint] = aggregateFromSQL(row)
		}
		for _, row := range currentRows {
			currentValues[row.Fingerprint] = aggregateFromSQL(row)
		}
		return compareMaps(baseValues, currentValues, options)
	}
	if len(options.BaseSQLFingerprints) == 0 && len(options.CurrentSQLFingerprints) == 0 {
		return nil
	}
	for _, row := range options.BaseSQLFingerprints {
		baseValues[row.Fingerprint] = row.Stats
	}
	for _, row := range options.CurrentSQLFingerprints {
		currentValues[row.Fingerprint] = row.Stats
	}
	return compareMaps(baseValues, currentValues, options)
}

func aggregateFromSQL(row sqlstats.Row) overview.Aggregate {
	return overview.Aggregate{Count: row.Count, Duration: stats.Summary{Count: uint64(row.Count), Sum: float64(row.TotalDurationMicros), Min: float64(row.MinDurationMicros), Max: float64(row.MaxDurationMicros), Mean: row.MeanDurationMicros, P50: float64(row.P50DurationMicros), P95: float64(row.P95DurationMicros), P99: float64(row.P99DurationMicros)}}
}

func compareSCALLCalls(base, current []scallstats.Call, options Options) []Change {
	return compareMaps(scallCallMap(base), scallCallMap(current), options)
}

func scallCallMap(rows []scallstats.Call) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[compositeKey(row.Interface, row.IName, row.Method)] = aggregateFromSummary(row.Metrics.Duration)
	}
	return values
}

func compareWebRequests(base, current []webstats.RequestRow, options Options) []Change {
	return compareMaps(webRequestMap(base), webRequestMap(current), options)
}

func webRequestMap(rows []webstats.RequestRow) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[compositeKey(row.Method, row.URI, row.Status, row.Result)] = aggregateFromWebDuration(row.Count, row.Stats)
	}
	return values
}

func compareSessionEvents(base, current []sessionstats.Aggregate, options Options) []Change {
	return compareMaps(sessionEventMap(base), sessionEventMap(current), options)
}

func sessionEventMap(rows []sessionstats.Aggregate) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[row.Key] = overview.Aggregate{Count: row.Stats.Count, Duration: stats.Summary{Count: uint64(row.Stats.Count), Sum: float64(row.Stats.TotalMicros), Min: float64(row.Stats.MinMicros), Max: float64(row.Stats.MaxMicros), Mean: row.Stats.MeanMicros}}
	}
	return values
}

func compareProcessAggregates(base, current []processstats.Aggregate, options Options) []Change {
	return compareMaps(processAggregateMap(base), processAggregateMap(current), options)
}

func processAggregateMap(rows []processstats.Aggregate) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[row.Key] = overview.Aggregate{Count: row.Metrics.Occurrences, Duration: row.Metrics.EventDuration}
	}
	return values
}

func compareLicenses(base, current []licensestats.LicenseRow, options Options) []Change {
	return compareMaps(licenseMap(base), licenseMap(current), options)
}

func licenseMap(rows []licensestats.LicenseRow) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[compositeKey(row.Func, row.Result, row.Process, row.User)] = aggregateFromLicenseDuration(row.Count, row.Stats)
	}
	return values
}

func compareErrorGroups(base, current []errorcontext.Group, options Options) []Change {
	return compareMaps(errorGroupMap(base), errorGroupMap(current), options)
}

func errorGroupMap(rows []errorcontext.Group) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		// Error-context groups have occurrence counts but no measured duration.
		// A zero Duration keeps count changes visible in Reasons while ensuring
		// they cannot be classified as a performance regression.
		values[row.Signature] = overview.Aggregate{Count: row.Count}
	}
	return values
}

func compareFileDBAggregates(base, current []filedbstats.Aggregate, options Options) []Change {
	return compareMaps(fileDBMap(base), fileDBMap(current), options)
}

func fileDBMap(rows []filedbstats.Aggregate) map[string]overview.Aggregate {
	values := make(map[string]overview.Aggregate, len(rows))
	for _, row := range rows {
		values[row.Key] = overview.Aggregate{Count: row.Count, Duration: stats.Summary{Count: uint64(row.Duration.Count), Sum: float64(row.Duration.TotalMicros), Min: float64(row.Duration.MinMicros), Max: float64(row.Duration.MaxMicros), Mean: row.Duration.MeanMicros, P50: float64(row.Duration.P50Micros), P95: float64(row.Duration.P95Micros), P99: float64(row.Duration.P99Micros)}}
	}
	return values
}

func aggregateFromSummary(summary stats.Summary) overview.Aggregate {
	return overview.Aggregate{Count: int64(summary.Count), Duration: summary}
}

func aggregateFromWebDuration(count int64, duration webstats.DurationStats) overview.Aggregate {
	return overview.Aggregate{Count: count, Duration: stats.Summary{Count: uint64(duration.Count), Sum: float64(duration.TotalMicros), Min: float64(duration.MinMicros), Max: float64(duration.MaxMicros), Mean: duration.MeanMicros, P50: float64(duration.P50Micros), P95: float64(duration.P95Micros), P99: float64(duration.P99Micros)}}
}

func aggregateFromLicenseDuration(count int64, duration licensestats.DurationStats) overview.Aggregate {
	return overview.Aggregate{Count: count, Duration: stats.Summary{Count: uint64(duration.Count), Sum: float64(duration.TotalMicros), Min: float64(duration.MinMicros), Max: float64(duration.MaxMicros), Mean: duration.MeanMicros, P50: float64(duration.P50Micros), P95: float64(duration.P95Micros), P99: float64(duration.P99Micros)}}
}

// compositeKey is stable and unambiguous for the specialized grouping rows.
// Length prefixes preserve distinct keys even when a source value has a NUL.
func compositeKey(values ...string) string {
	var builder strings.Builder
	for _, value := range values {
		builder.WriteString(fmt.Sprintf("%d:", len(value)))
		builder.WriteString(value)
	}
	return builder.String()
}

func compareMaps(base, current map[string]overview.Aggregate, options Options) []Change {
	keys := make(map[string]struct{}, len(base)+len(current))
	for key := range base {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}
	changes := make([]Change, 0, len(keys))
	for key := range keys {
		changes = append(changes, compareAggregate(key, base[key], current[key], options))
	}
	sortChanges(changes)
	return changes
}

func compareAggregate(key string, base, current overview.Aggregate, options Options) Change {
	baselineDuration := base.Duration.Sum
	currentDuration := current.Duration.Sum
	durationDelta := metricDelta(baselineDuration, currentDuration)
	countDelta := metricDelta(float64(base.Count), float64(current.Count))
	p95Delta := metricDelta(base.Duration.P95, current.Duration.P95)
	change := Change{Key: key, Baseline: base, Current: current, AbsoluteDelta: durationDelta.AbsoluteDelta, PercentDelta: durationDelta.PercentDelta, DurationDelta: durationDelta, CountDelta: countDelta, P95Delta: p95Delta}

	switch {
	case base.Count == 0 && current.Count > 0:
		change.Classification = New
		change.Reasons = []Reason{{Metric: "row", Classification: New}}
	case base.Count > 0 && current.Count == 0:
		change.Classification = Removed
		change.Reasons = []Reason{{Metric: "row", Classification: Removed}}
	default:
		durationClass := classifyMetric(durationDelta, options.RegressionPercent, options.ImprovementPercent, options.MinAbsoluteDeltaMicros)
		p95Class := classifyMetric(p95Delta, options.P95RegressionPercent, options.P95ImprovementPercent, 0)
		countClass := classifyMetric(countDelta, options.CountRegressionPercent, options.CountImprovementPercent, 0)
		change.Reasons = appendReason(change.Reasons, "duration_total", durationClass)
		change.Reasons = appendReason(change.Reasons, "p95", p95Class)
		change.Reasons = appendReason(change.Reasons, "count", countClass)
		change.Classification = performanceClassification(durationClass, p95Class)
	}
	return change
}

func metricDelta(base, current float64) MetricDelta {
	result := MetricDelta{Baseline: base, Current: current, AbsoluteDelta: current - base}
	if base != 0 && !math.IsNaN(base) && !math.IsNaN(current) {
		percent := result.AbsoluteDelta / base * 100
		result.PercentDelta = &percent
	}
	return result
}

func classifyMetric(delta MetricDelta, regressionPercent, improvementPercent, minAbsolute float64) Classification {
	if math.IsNaN(delta.Baseline) || math.IsNaN(delta.Current) || math.Abs(delta.AbsoluteDelta) < minAbsolute {
		return Unchanged
	}
	if delta.Baseline == 0 {
		if delta.AbsoluteDelta > 0 {
			return Regressed
		}
		if delta.AbsoluteDelta < 0 {
			return Improved
		}
		return Unchanged
	}
	if delta.PercentDelta == nil {
		return Unchanged
	}
	if *delta.PercentDelta >= regressionPercent {
		return Regressed
	}
	if *delta.PercentDelta <= -improvementPercent {
		return Improved
	}
	return Unchanged
}

func appendReason(reasons []Reason, metric string, classification Classification) []Reason {
	if classification == Unchanged {
		return reasons
	}
	return append(reasons, Reason{Metric: metric, Classification: classification})
}

func performanceClassification(duration, p95 Classification) Classification {
	if duration == Regressed || p95 == Regressed {
		return Regressed
	}
	if duration == Improved || p95 == Improved {
		return Improved
	}
	return Unchanged
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		left, right := changes[i], changes[j]
		if classificationRank(left.Classification) != classificationRank(right.Classification) {
			return classificationRank(left.Classification) < classificationRank(right.Classification)
		}
		leftMagnitude, rightMagnitude := math.Abs(left.AbsoluteDelta), math.Abs(right.AbsoluteDelta)
		if leftMagnitude != rightMagnitude {
			return leftMagnitude > rightMagnitude
		}
		return left.Key < right.Key
	})
}

func classificationRank(classification Classification) int {
	switch classification {
	case New:
		return 0
	case Regressed:
		return 1
	case Removed:
		return 2
	case Improved:
		return 3
	default:
		return 4
	}
}
