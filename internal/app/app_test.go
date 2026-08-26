package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"techlog-stat/internal/config"
	"techlog-stat/internal/model"
	comparereport "techlog-stat/internal/report/compare"
	"techlog-stat/internal/report/overview"
)

func TestRunAnalyzeWritesAllRequestedArtifacts(t *testing.T) {
	root := t.TempDir()
	writeLog(t, filepath.Join(root, "26082310.log"), ""+
		"00:00.000000-100,SDBL,1,Usr=alice,DataBase=main,process=rphost,Context=Module.Run,Sdbl='SELECT * FROM Items WHERE id=1',Rows=2\n"+
		"00:00.000001-50,TLOCK,1,Context=Module.Run,Locks='Table=Document.Sales,Region=Header'\n"+
		"00:00.000002-20,TTIMEOUT,1,Context=Module.Run,Regions=Header\n"+
		"00:00.000003-10,TDEADLOCK,1,Context=Module.Close,Locks='Table=Document.Sales,Region=Header',Waiter=conn-a,Blocker=conn-b\n")
	out := filepath.Join(root, "analyze")
	if err := Run(analyzeConfig(root, out, []string{"text", "csv", "json", "html"})); err != nil {
		t.Fatal(err)
	}
	requireFiles(t, out, "summary.txt", "event_types.csv", "sql.csv", "traces.csv", "locks.csv", "scall.csv", "web.csv", "sessions.csv", "processes.csv", "licenses.csv", "filedb.csv", "error_contexts.csv", "run.json", "report.html", "errors.log")
	data, err := os.ReadFile(filepath.Join(out, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report overview.OverviewResult
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("overview JSON is unreadable: %v", err)
	}
	if report.Totals.Count != 4 || report.Locks.Quality.LockEvents != 3 || len(report.SQLRows) != 1 {
		t.Fatalf("unexpected overview report: totals=%+v locks=%+v sql=%+v", report.Totals, report.Locks.Quality, report.SQLRows)
	}
}

func TestRunContextServeStartsAndStops(t *testing.T) {
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Report: config.ReportServe, InputRoot: root, Glob: "*.log", Listen: address, BucketInterval: time.Minute, TopN: 20, Workers: 1, MaxRuns: 2, MaxConcurrentRuns: 1}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunContext(ctx, cfg) }()
	url := "http://" + address + "/api/v1/health"
	started := false
	for attempt := 0; attempt < 50; attempt++ {
		response, requestErr := http.Get(url)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				started = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		cancel()
		t.Fatal("serve health endpoint did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
}

func TestRunCompareCreatesArtifactsAndClassifications(t *testing.T) {
	root := t.TempDir()
	baselineLogs := filepath.Join(root, "baseline-logs")
	currentLogs := filepath.Join(root, "current-logs")
	if err := os.MkdirAll(baselineLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(currentLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLog(t, filepath.Join(baselineLogs, "26082310.log"), "00:00.000000-100,SDBL,1,Context=Slow,Sdbl='select 1'\n00:00.000001-50,EXCP,1,Descr='same'\n")
	writeLog(t, filepath.Join(currentLogs, "26082310.log"), "00:00.000000-200,SDBL,1,Context=Slow,Sdbl='select 2'\n00:00.000001-50,EXCP,1,Descr='same'\n")
	baselineOut := filepath.Join(root, "baseline")
	currentOut := filepath.Join(root, "current")
	if err := Run(analyzeConfig(baselineLogs, baselineOut, []string{"json"})); err != nil {
		t.Fatal(err)
	}
	if err := Run(analyzeConfig(currentLogs, currentOut, []string{"json"})); err != nil {
		t.Fatal(err)
	}

	compareOut := filepath.Join(root, "compare")
	cfg := config.Config{Report: config.ReportCompare, OutputDir: compareOut, Formats: []string{"text", "csv", "json", "html"}, BaselinePath: filepath.Join(baselineOut, "run.json"), CurrentPath: filepath.Join(currentOut, "run.json"), ThresholdPercent: 10, ThresholdAbsMicros: 1}
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	requireFiles(t, compareOut, "summary.txt", "compare.csv", "run.json", "report.html")
	data, err := os.ReadFile(filepath.Join(compareOut, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result comparereport.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("compare JSON is unreadable: %v", err)
	}
	if result.Totals.Classification != comparereport.Regressed {
		t.Fatalf("totals classification = %s", result.Totals.Classification)
	}
	if eventClassification(result, "SDBL") != comparereport.Regressed || eventClassification(result, "EXCP") != comparereport.Unchanged {
		t.Fatalf("event classifications = %+v", result.EventTypes)
	}
}

func TestRunLegacyContextReportStillWorks(t *testing.T) {
	root := t.TempDir()
	writeLog(t, filepath.Join(root, "26082310.log"), "00:00.000000-1500,SDBL,1,Context=Legacy.Context\n")
	out := filepath.Join(root, "legacy")
	cfg := config.Config{Report: config.ReportSDBLContext, Mode: config.ModeAggregate, InputRoot: root, Glob: "*.log", OutputDir: out, Formats: []string{"text", "csv", "json", "html"}, TopN: 10, Workers: 1}
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	requireFiles(t, out, "summary.txt", "contexts.csv", "run.json", "report.html", "errors.log")
	data, err := os.ReadFile(filepath.Join(out, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report model.ContextReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("legacy JSON is unreadable: %v", err)
	}
	if report.Totals.EventCount != 1 || len(report.Rows) != 1 || report.Rows[0].Context != "Legacy.Context" {
		t.Fatalf("legacy report = %+v", report)
	}
}

func analyzeConfig(input, output string, formats []string) config.Config {
	return config.Config{Report: config.ReportAnalyze, Mode: config.ModeAggregate, InputRoot: input, Glob: "*.log", OutputDir: output, Formats: formats, BucketInterval: time.Minute, TopN: 20, Workers: 1}
}

func writeLog(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}
}

func eventClassification(result comparereport.Result, event string) comparereport.Classification {
	for _, change := range result.EventTypes {
		if change.Key == event {
			return change.Classification
		}
	}
	return ""
}
