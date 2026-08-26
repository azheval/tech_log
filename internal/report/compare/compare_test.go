package compare

import (
	"math"
	"reflect"
	"testing"

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

func TestCompareClassifiesNewRemovedAndExistingRows(t *testing.T) {
	base := overview.OverviewResult{
		Totals:     aggregate(100, 1),
		EventTypes: []overview.EventTypeStat{{Event: "OLD", Stats: aggregate(20, 1)}, {Event: "SAME", Stats: aggregate(80, 1)}},
		Users:      []overview.DimensionStat{{Value: "alice", Stats: aggregate(80, 1)}},
	}
	current := overview.OverviewResult{
		Totals:     aggregate(150, 2),
		EventTypes: []overview.EventTypeStat{{Event: "NEW", Stats: aggregate(50, 1)}, {Event: "SAME", Stats: aggregate(100, 1)}},
		Users:      []overview.DimensionStat{{Value: "alice", Stats: aggregate(100, 1)}, {Value: "bob", Stats: aggregate(50, 1)}},
	}

	got := Compare(base, current, Options{RegressionPercent: 10, ImprovementPercent: 10})
	if got.Totals.Classification != Regressed || *got.Totals.PercentDelta != 50 {
		t.Fatalf("total = %+v", got.Totals)
	}
	if got.EventTypes[0].Key != "NEW" || got.EventTypes[0].Classification != New {
		t.Fatalf("new event = %+v", got.EventTypes)
	}
	if got.EventTypes[1].Key != "SAME" || got.EventTypes[1].Classification != Regressed {
		t.Fatalf("changed event = %+v", got.EventTypes)
	}
	if got.EventTypes[2].Key != "OLD" || got.EventTypes[2].Classification != Removed {
		t.Fatalf("removed event = %+v", got.EventTypes)
	}
	if got.Users[0].Key != "bob" || got.Users[0].Classification != New {
		t.Fatalf("users = %+v", got.Users)
	}
}

func TestCompareHandlesZeroBaselineSafely(t *testing.T) {
	base := overview.OverviewResult{Totals: aggregate(0, 0), EventTypes: []overview.EventTypeStat{{Event: "ZERO", Stats: aggregate(0, 1)}}}
	current := overview.OverviewResult{Totals: aggregate(20, 1), EventTypes: []overview.EventTypeStat{{Event: "ZERO", Stats: aggregate(20, 1)}}}

	got := Compare(base, current, Options{})
	if got.Totals.Classification != New || got.Totals.PercentDelta != nil {
		t.Fatalf("zero total = %+v", got.Totals)
	}
	if got.EventTypes[0].Classification != Regressed || got.EventTypes[0].PercentDelta != nil {
		t.Fatalf("zero-duration existing row = %+v", got.EventTypes[0])
	}
}

func TestCompareAppliesThresholdsAndAbsoluteFloor(t *testing.T) {
	base := overview.OverviewResult{Totals: aggregate(100, 1)}
	current := overview.OverviewResult{Totals: aggregate(108, 1)}

	got := Compare(base, current, Options{RegressionPercent: 10, ImprovementPercent: 10})
	if got.Totals.Classification != Unchanged {
		t.Fatalf("below percentage threshold = %+v", got.Totals)
	}
	got = Compare(base, current, Options{RegressionPercent: 5, ImprovementPercent: 5, MinAbsoluteDeltaMicros: 10})
	if got.Totals.Classification != Unchanged {
		t.Fatalf("below absolute threshold = %+v", got.Totals)
	}
	got = Compare(base, overview.OverviewResult{Totals: aggregate(80, 1)}, Options{RegressionPercent: 10, ImprovementPercent: 10})
	if got.Totals.Classification != Improved {
		t.Fatalf("improvement = %+v", got.Totals)
	}
}

func TestCompareSortsDeterministicallyAndComparesSQLWhenProvided(t *testing.T) {
	base := overview.OverviewResult{EventTypes: []overview.EventTypeStat{{Event: "a", Stats: aggregate(100, 1)}, {Event: "b", Stats: aggregate(100, 1)}, {Event: "old", Stats: aggregate(50, 1)}}}
	current := overview.OverviewResult{EventTypes: []overview.EventTypeStat{{Event: "a", Stats: aggregate(140, 1)}, {Event: "b", Stats: aggregate(130, 1)}, {Event: "new", Stats: aggregate(10, 1)}}}
	got := Compare(base, current, Options{RegressionPercent: 10, ImprovementPercent: 10,
		BaseSQLFingerprints:    []SQLFingerprint{{Fingerprint: "select ?", Stats: aggregate(10, 1)}},
		CurrentSQLFingerprints: []SQLFingerprint{{Fingerprint: "select ?", Stats: aggregate(30, 1)}},
	})
	if names := changeNames(got.EventTypes); !reflect.DeepEqual(names, []string{"new", "a", "b", "old"}) {
		t.Fatalf("event ordering = %v", names)
	}
	if len(got.SQLFingerprints) != 1 || got.SQLFingerprints[0].Classification != Regressed {
		t.Fatalf("sql = %+v", got.SQLFingerprints)
	}
}

func TestCompareUsesOverviewSQLRowsAndReportsP95Reason(t *testing.T) {
	base := overview.OverviewResult{SQLRows: []sqlstats.Row{{Fingerprint: "direct", Count: 2, TotalDurationMicros: 100, MinDurationMicros: 40, MaxDurationMicros: 60, MeanDurationMicros: 50, P50DurationMicros: 50, P95DurationMicros: 60, P99DurationMicros: 60}}}
	current := overview.OverviewResult{SQLRows: []sqlstats.Row{{Fingerprint: "direct", Count: 2, TotalDurationMicros: 100, MinDurationMicros: 10, MaxDurationMicros: 200, MeanDurationMicros: 50, P50DurationMicros: 50, P95DurationMicros: 200, P99DurationMicros: 200}}}
	got := Compare(base, current, Options{RegressionPercent: 10, ImprovementPercent: 10,
		BaseSQLFingerprints:    []SQLFingerprint{{Fingerprint: "fallback", Stats: aggregate(1, 1)}},
		CurrentSQLFingerprints: []SQLFingerprint{{Fingerprint: "fallback", Stats: aggregate(100, 1)}},
	})
	if len(got.SQLFingerprints) != 1 || got.SQLFingerprints[0].Key != "direct" {
		t.Fatalf("direct SQL rows were not preferred: %+v", got.SQLFingerprints)
	}
	change := got.SQLFingerprints[0]
	if change.Classification != Regressed || change.DurationDelta.AbsoluteDelta != 0 || change.CountDelta.AbsoluteDelta != 0 || change.P95Delta.AbsoluteDelta != 140 {
		t.Fatalf("metric deltas = %+v", change)
	}
	if !hasReason(change.Reasons, "p95", Regressed) {
		t.Fatalf("p95 reason missing: %+v", change.Reasons)
	}
}

func TestCompareRejectsInvalidThresholds(t *testing.T) {
	if err := ValidateOptions(Options{RegressionPercent: math.NaN()}); err == nil {
		t.Fatal("NaN threshold accepted")
	}
}

func TestCompareSpecializedOverviewSections(t *testing.T) {
	base := overview.OverviewResult{
		SCALL:    scallstats.Result{ByCall: []scallstats.Call{{Interface: "I", IName: "Name", Method: "M", Metrics: scallstats.Metrics{Duration: stats.Summary{Count: 1, Sum: 10, P95: 10}}}}},
		Web:      webstats.Result{Requests: []webstats.RequestRow{{Method: "GET", URI: "/items", Status: "200", Result: "ok", Count: 1, Stats: webstats.DurationStats{Count: 1, TotalMicros: 10, P95Micros: 10}}}},
		Sessions: sessionstats.Result{ByEvent: []sessionstats.Aggregate{{Key: "SESN", Stats: sessionstats.DurationStats{Count: 1, TotalMicros: 10}}}},
		ProcessStats: processstats.Result{
			PROCByProcess:   []processstats.Aggregate{{Key: "rphost", Metrics: processstats.EventMetrics{Occurrences: 1, EventDuration: stats.Summary{Count: 1, Sum: 10, P95: 10}}}},
			SCOMByOperation: []processstats.Aggregate{{Key: "new ServerProcessData", Metrics: processstats.EventMetrics{Occurrences: 1, EventDuration: stats.Summary{Count: 1, Sum: 10, P95: 10}}}},
		},
		Licenses:     licensestats.Result{Licenses: []licensestats.LicenseRow{{Func: "getLicense", Result: "seize", Process: "rphost", User: "alice", Count: 1, Stats: licensestats.DurationStats{Count: 1, TotalMicros: 10, P95Micros: 10}}}},
		ErrorContext: errorcontext.Result{Groups: []errorcontext.Group{{Signature: "error-signature", Count: 1}}},
		FileDB:       filedbstats.Result{ByFunc: []filedbstats.Aggregate{{Key: "Read", Count: 1, Duration: filedbstats.DurationStats{Count: 1, TotalMicros: 10, P95Micros: 10}}}},
	}
	current := base
	current.SCALL.ByCall = append([]scallstats.Call(nil), base.SCALL.ByCall...)
	current.Web.Requests = append([]webstats.RequestRow(nil), base.Web.Requests...)
	current.Sessions.ByEvent = append([]sessionstats.Aggregate(nil), base.Sessions.ByEvent...)
	current.ProcessStats.PROCByProcess = append([]processstats.Aggregate(nil), base.ProcessStats.PROCByProcess...)
	current.ProcessStats.SCOMByOperation = append([]processstats.Aggregate(nil), base.ProcessStats.SCOMByOperation...)
	current.Licenses.Licenses = append([]licensestats.LicenseRow(nil), base.Licenses.Licenses...)
	current.ErrorContext.Groups = append([]errorcontext.Group(nil), base.ErrorContext.Groups...)
	current.FileDB.ByFunc = append([]filedbstats.Aggregate(nil), base.FileDB.ByFunc...)
	current.SCALL.ByCall[0].Metrics.Duration.Sum = 20
	current.SCALL.ByCall[0].Metrics.Duration.P95 = 20
	current.Web.Requests[0].Stats.TotalMicros = 20
	current.Web.Requests[0].Stats.P95Micros = 20
	current.Sessions.ByEvent[0].Stats.TotalMicros = 20
	current.ProcessStats.PROCByProcess[0].Metrics.EventDuration.Sum = 20
	current.ProcessStats.SCOMByOperation[0].Metrics.EventDuration.Sum = 20
	current.Licenses.Licenses[0].Stats.TotalMicros = 20
	current.FileDB.ByFunc[0].Duration.TotalMicros = 20
	current.ErrorContext.Groups[0].Count = 2

	got := Compare(base, current, Options{RegressionPercent: 10, ImprovementPercent: 10})
	for name, changes := range map[string][]Change{
		"scall": got.SCALLByCall, "web": got.WebRequests, "sessions": got.SessionByEvent, "proc": got.PROCByProcess,
		"scom": got.SCOMByOperation, "licenses": got.Licenses, "filedb": got.FileDBByFunc,
	} {
		if len(changes) != 1 || changes[0].Classification != Regressed || changes[0].DurationDelta.AbsoluteDelta != 10 {
			t.Fatalf("%s = %+v", name, changes)
		}
	}
	if len(got.ErrorGroups) != 1 || got.ErrorGroups[0].Classification != Unchanged || !hasReason(got.ErrorGroups[0].Reasons, "count", Regressed) {
		t.Fatalf("count-only errors must not be performance regressions: %+v", got.ErrorGroups)
	}
	if got.SCALLByCall[0].Key != compositeKey("I", "Name", "M") || got.WebRequests[0].Key != compositeKey("GET", "/items", "200", "ok") || got.Licenses[0].Key != compositeKey("getLicense", "seize", "rphost", "alice") {
		t.Fatalf("composite keys = scall=%q web=%q licenses=%q", got.SCALLByCall[0].Key, got.WebRequests[0].Key, got.Licenses[0].Key)
	}
}

func aggregate(sum float64, count int64) overview.Aggregate {
	return overview.Aggregate{Count: count, Duration: stats.Summary{Count: uint64(count), Sum: sum}}
}

func changeNames(changes []Change) []string {
	result := make([]string, len(changes))
	for i, change := range changes {
		result[i] = change.Key
	}
	return result
}

func hasReason(reasons []Reason, metric string, classification Classification) bool {
	for _, reason := range reasons {
		if reason.Metric == metric && reason.Classification == classification {
			return true
		}
	}
	return false
}
