package output

import (
	"bytes"
	"strings"
	"testing"
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
	"techlog-stat/internal/report/overview"
	"techlog-stat/internal/stats"
)

func TestOverviewRenderersProduceAllFormats(t *testing.T) {
	duration := stats.Summary{Count: 2, Sum: 300, Min: 100, Max: 200, Mean: 150, P50: 150, P90: 190, P95: 195, P99: 199}
	report := overview.OverviewResult{
		Meta:         overview.Meta{FinishedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC), FilesProcessed: 1},
		Totals:       overview.Aggregate{Count: 2, Duration: duration},
		Quality:      overview.Quality{EventsParsed: 2},
		EventTypes:   []overview.EventTypeStat{{Event: "<script>bad</script>", Stats: overview.Aggregate{Count: 2, Duration: duration}}},
		Buckets:      []overview.TimeBucketStat{{Start: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 23, 10, 1, 0, 0, time.UTC), Stats: overview.Aggregate{Count: 2, Duration: duration}}},
		SQLRows:      []sqlstats.Row{{Fingerprint: "fingerprint", EventType: "SDBL", NormalizedQuery: "<script>sql</script>", Count: 2, TotalDurationMicros: 300, P95DurationMicros: 195, Contexts: []string{"Module.Run"}}},
		Traces:       []trace.Trace{{ID: "trace-1", Source: "log", Process: "rphost", OSThread: "7", StartedAt: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), LastAt: time.Date(2026, 8, 23, 10, 0, 1, 0, time.UTC), Spans: []trace.Span{{Timestamp: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), Event: "SDBL", DurationMicros: 300, Fields: map[string]string{"Context": "Module.Run"}, Raw: "<img src=x onerror=1>"}}}},
		TraceQuality: trace.Quality{EventsConsumed: 2, CorrelatedEvents: 1, RetainedOpenTraces: 1},
		Locks: lockstats.Result{
			Quality:      lockstats.Quality{LockEvents: 2, MissingContext: 1, MissingLocks: 1, MissingRegions: 1, EventsWithExplicitRelation: 1},
			ByEvent:      []lockstats.Aggregate{{Key: "TDEADLOCK", Stats: lockstats.DurationStats{Count: 1, TotalMicros: 200, MaxMicros: 200, P95Micros: 200}}},
			ByContext:    []lockstats.Aggregate{{Key: "<b>Posting</b>", Stats: lockstats.DurationStats{Count: 1, TotalMicros: 200}}},
			ByTable:      []lockstats.Aggregate{{Key: "Document.Sales", Stats: lockstats.DurationStats{Count: 1, TotalMicros: 200}}},
			ByRegion:     []lockstats.Aggregate{{Key: "Header", Stats: lockstats.DurationStats{Count: 1, TotalMicros: 200}}},
			TopConflicts: []lockstats.Conflict{{EventType: "TDEADLOCK", Context: "Posting", Tables: []string{"Document.Sales"}, Regions: []string{"Header"}, Stats: lockstats.DurationStats{Count: 1, TotalMicros: 200, P95Micros: 200}}},
			Relations:    []lockstats.Relation{{EventType: "TDEADLOCK", Waiter: "conn-a", Blocker: "conn-b", Context: "Posting", Source: "log"}},
			Samples:      []lockstats.Sample{{EventType: "TDEADLOCK", DurationMicros: 200, Context: "Posting", Raw: "<script>lock</script>"}},
		},
		SCALL:        scallstats.Result{Quality: scallstats.Quality{CallEvents: 1}, ByCall: []scallstats.Call{{Interface: "iface", IName: "IObject", Method: "4", Metrics: scallstats.Metrics{Duration: stats.Summary{Count: 1, Sum: 10}}}}},
		Web:          webstats.Result{Quality: webstats.Quality{Requests: 1}, Requests: []webstats.RequestRow{{Method: "GET", URI: "/api/items/{id}", Status: "200", Count: 1}}},
		Sessions:     sessionstats.Result{Quality: sessionstats.Quality{LifecycleEvents: 2, CompletedSessions: 1}, Sessions: []sessionstats.Session{{EventType: "SESN", ID: "session-1", StartedAt: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 8, 23, 10, 0, 1, 0, time.UTC), DurationMicros: 1_000_000}}},
		ProcessStats: processstats.Result{Quality: processstats.Quality{SCOMEvents: 1}, ExplicitProcessRelations: []processstats.ProcessRelation{{Operation: "new ServerProcessData", Process: "1cv8", Occurrences: 1}}},
		Licenses:     licensestats.Result{Quality: licensestats.Quality{LicenseEvents: 1}, Licenses: []licensestats.LicenseRow{{Func: "getLicense", Result: "seize", Count: 1}}},
		FileDB:       filedbstats.Result{Quality: filedbstats.Quality{DBV8DBEngEvents: 1}, ByFunc: []filedbstats.Aggregate{{Key: "Read", Count: 1, Duration: filedbstats.DurationStats{Count: 1, TotalMicros: 10}}}},
		ErrorContext: errorcontext.Result{Quality: errorcontext.Quality{ErrorEvents: 1}, Groups: []errorcontext.Group{{Signature: "signature", Exception: "DatabaseException", Count: 1}}},
	}

	jsonData, err := RenderOverviewJSON(report)
	if err != nil || !bytes.Contains(jsonData, []byte(`"event_types"`)) {
		t.Fatalf("JSON = %s, err = %v", jsonData, err)
	}
	csvData, err := RenderOverviewEventTypesCSV(report)
	if err != nil || !bytes.HasPrefix(csvData, []byte{0xEF, 0xBB, 0xBF}) || !bytes.Contains(csvData, []byte("event,count")) {
		t.Fatalf("CSV = %q, err = %v", csvData, err)
	}
	textData := RenderOverviewText(report)
	if !bytes.HasPrefix(textData, []byte{0xEF, 0xBB, 0xBF}) || !bytes.Contains(textData, []byte("EventTypes:")) {
		t.Fatalf("text = %q", textData)
	}
	if !bytes.Contains(textData, []byte("TopSQL:")) || !bytes.Contains(textData, []byte("TraceQuality:")) || !bytes.Contains(textData, []byte("LocksSummary:")) || !bytes.Contains(textData, []byte("TopLockConflicts:")) {
		t.Fatalf("overview details missing from text = %q", textData)
	}
	if bytes.Contains(textData, []byte(`\n`)) || !bytes.Contains(textData, []byte("GeneratedAt: 2026-08-23T12:00:00Z\n")) {
		t.Fatalf("text must contain real line breaks, got %q", textData)
	}
	sqlData, err := RenderOverviewSQLCSV(report)
	if err != nil || !bytes.Contains(sqlData, []byte("fingerprint,event_type")) || !bytes.Contains(sqlData, []byte("Module.Run")) {
		t.Fatalf("SQL CSV = %q, err = %v", sqlData, err)
	}
	tracesData, err := RenderOverviewTracesCSV(report)
	if err != nil || !bytes.Contains(tracesData, []byte("trace_id,trace_started_at")) || !bytes.Contains(tracesData, []byte("trace-1")) {
		t.Fatalf("traces CSV = %q, err = %v", tracesData, err)
	}
	locksData, err := RenderOverviewLocksCSV(report)
	if err != nil || !bytes.Contains(locksData, []byte("kind,key,event_type")) || !bytes.Contains(locksData, []byte("TDEADLOCK")) || !bytes.Contains(locksData, []byte("conn-a")) {
		t.Fatalf("locks CSV = %q, err = %v", locksData, err)
	}
	for _, renderer := range []struct {
		name string
		run  func(overview.OverviewResult) ([]byte, error)
		want string
	}{
		{"SCALL", RenderOverviewSCALLCSV, "iface"},
		{"web", RenderOverviewWebCSV, "/api/items/{id}"},
		{"sessions", RenderOverviewSessionsCSV, "session-1"},
		{"processes", RenderOverviewProcessesCSV, "new ServerProcessData"},
		{"licenses", RenderOverviewLicensesCSV, "getLicense"},
		{"file DB", RenderOverviewFileDBCSV, "Read"},
		{"error contexts", RenderOverviewErrorContextsCSV, "DatabaseException"},
	} {
		data, err := renderer.run(report)
		if err != nil || !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) || !bytes.Contains(data, []byte(renderer.want)) {
			t.Fatalf("%s CSV = %q, err = %v", renderer.name, data, err)
		}
	}
	htmlData, err := RenderOverviewHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlData)
	if !strings.Contains(html, `<canvas id="timeline"`) || !strings.Contains(html, `<details class="trace"`) || !strings.Contains(html, `<details class="trace lock-sample"`) || !strings.Contains(html, "Locks — Summary") || !strings.Contains(html, `id="global-search"`) || !strings.Contains(html, `data-tab="locks"`) || !strings.Contains(html, `data-tab="events"`) || !strings.Contains(html, "SCALL: Server Calls") || !strings.Contains(html, "DBV8DBEng: File Database") || !strings.Contains(html, "localStorage") || !strings.Contains(html, "aria-sort") || strings.Contains(html, "<script>bad</script>") || strings.Contains(html, "<script>sql</script>") || strings.Contains(html, "<img src=x onerror=1>") || strings.Contains(html, "<script>lock</script>") || strings.Contains(html, "https://") {
		t.Fatalf("unexpected dashboard HTML: %s", html)
	}
}

func TestRenderOverviewJSONUsesSnakeCaseNewAnalysisKeys(t *testing.T) {
	report := overview.OverviewResult{
		SCALL:        scallstats.Result{Quality: scallstats.Quality{CallEvents: 1}},
		Web:          webstats.Result{Quality: webstats.Quality{MatchedResponses: 2}},
		Sessions:     sessionstats.Result{Quality: sessionstats.Quality{CompletedSessions: 3}},
		ProcessStats: processstats.Result{Quality: processstats.Quality{PROCEvents: 4}},
		Licenses:     licensestats.Result{Quality: licensestats.Quality{LicenseEvents: 5}},
		ErrorContext: errorcontext.Result{Quality: errorcontext.Quality{MatchedContexts: 6}},
		FileDB:       filedbstats.Result{Quality: filedbstats.Quality{DBV8DBEngEvents: 7}},
	}
	data, err := RenderOverviewJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"call_events", "matched_responses", "completed_sessions", "proc_events", "license_events", "matched_contexts", "dbv8dbeng_events"} {
		if !bytes.Contains(data, []byte(`"`+key+`"`)) {
			t.Fatalf("JSON missing %q: %s", key, data)
		}
	}
	for _, key := range []string{"CallEvents", "MatchedResponses", "CompletedSessions", "PROCEvents", "LicenseEvents", "MatchedContexts", "DBV8DBEngEvents"} {
		if bytes.Contains(data, []byte(`"`+key+`"`)) {
			t.Fatalf("JSON leaked PascalCase key %q: %s", key, data)
		}
	}
}

func TestOverviewHTMLBoundsSQLTracesSpansAndRawPreview(t *testing.T) {
	sqlRows := make([]sqlstats.Row, maxOverviewSQLRows+1)
	shownSQL, omittedSQL := boundedSQLRows(sqlRows, maxOverviewSQLRows)
	if len(shownSQL) != maxOverviewSQLRows || omittedSQL != 1 {
		t.Fatalf("SQL bound = %d shown, %d omitted", len(shownSQL), omittedSQL)
	}
	spans := make([]trace.Span, maxTraceSpans+1)
	traces := make([]trace.Trace, maxOverviewTraces+1)
	for index := range traces {
		traces[index].Spans = spans
	}
	shownTraces, omittedTraces := boundedTraces(traces, maxOverviewTraces, maxTraceSpans)
	if len(shownTraces) != maxOverviewTraces || omittedTraces != 1 || len(shownTraces[0].Spans) != maxTraceSpans || shownTraces[0].SpansOmitted != 1 {
		t.Fatalf("trace bound = %+v, omitted=%d", shownTraces[0], omittedTraces)
	}
	if got := []rune(overviewPreview(strings.Repeat("я", maxRawPreviewRunes+1))); len(got) != maxRawPreviewRunes+2 {
		t.Fatalf("raw preview rune count = %d, want %d", len(got), maxRawPreviewRunes+2)
	}
}
