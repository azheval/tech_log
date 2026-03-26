package contextreport

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"techlog-stat/internal/config"
)

func TestBuildAggregatesSingleLineAndMultilineSDBL(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-3,SDBL,1,process=1CV8C,Rows=1,Context=Ctx.One\n"+
		"56:15.431023-5,SDBL,1,process=1CV8C,Sdbl='SELECT 1'\n"+
		"FROM dual,Rows=1,Context=Ctx.Two\n"+
		"56:15.431031-1,OTHER,1,process=1CV8C,Context=Ignored\n")

	cfg := config.Config{Report: config.ReportSDBLContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"text", "csv", "json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Meta.FilesMatched != 1 || report.Meta.FilesProcessed != 1 {
		t.Fatalf("unexpected file counters: matched=%d processed=%d", report.Meta.FilesMatched, report.Meta.FilesProcessed)
	}
	if report.Totals.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", report.Totals.EventCount)
	}
	if !almostEqual(report.Totals.DurationMS, 0.008) {
		t.Fatalf("DurationMS = %.6f, want 0.008", report.Totals.DurationMS)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(report.Rows))
	}
	if report.Rows[0].Context != "Ctx.Two" || !almostEqual(report.Rows[0].TotalDurationMS, 0.005) {
		t.Fatalf("unexpected first row: %+v", report.Rows[0])
	}
}

func TestBuildMergesMultipleFilesByContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), "56:15.431019-3,SDBL,1,process=1CV8C,Rows=1,Context=Shared.Context\n")
	writeTestFile(t, filepath.Join(root, "procB_124", "26032306.log"), ""+
		"56:15.431023-7,SDBL,1,process=1CV8C,Rows=1,Context=Shared.Context\n"+
		"56:15.431031-2,SDBL,1,process=1CV8C,Rows=1,Context=Other.Context\n")

	cfg := config.Config{Report: config.ReportSDBLContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"text"}, TopN: 10, Workers: 2}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 3 {
		t.Fatalf("EventCount = %d, want 3", report.Totals.EventCount)
	}
	if report.Rows[0].Context != "Shared.Context" || report.Rows[0].Count != 2 || !almostEqual(report.Rows[0].TotalDurationMS, 0.010) {
		t.Fatalf("unexpected first row: %+v", report.Rows[0])
	}
}

func TestBuildSDBLContextAppliesRawFilters(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-3000,SDBL,1,process=1CV8C,Usr=DefUser,DataBase=conf_null,Context=Ctx.Accepted\n"+
		"56:15.431020-2000,SDBL,1,process=1CV8C,Usr=OtherUser,DataBase=conf_null,Context=Ctx.RejectedByUser\n"+
		"56:15.431021-1000,SDBL,1,process=1CV8C,Usr=DefUser,Context=Ctx.MissingDb\n")

	cfg := config.Config{
		Report:    config.ReportSDBLContext,
		InputRoot: root,
		Glob:      "*/*.log",
		OutputDir: filepath.Join(root, "out"),
		Formats:   []string{"json"},
		Filters: []config.Filter{
			{Key: "Usr", Value: "DefUser"},
			{Key: "DataBase", Value: "conf_null"},
		},
		TopN:    10,
		Workers: 1,
	}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", report.Totals.EventCount)
	}
	if len(report.Rows) != 1 || report.Rows[0].Context != "Ctx.Accepted" {
		t.Fatalf("unexpected rows: %+v", report.Rows)
	}
}

func TestBuildCALLContextUsesFollowingContextEvent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-3000,CALL,1,process=1CV8C,OSThread=100,Interface=iface-1,Method=1\n"+
		"56:15.431020-0,Context,1,process=1CV8C,OSThread=100,Context=Ctx.Call\n"+
		"56:15.431021-2000,CALL,1,process=1CV8C,OSThread=200,Interface=iface-2,Method=2\n"+
		"56:15.431022-0,Context,1,process=1CV8C,OSThread=200,Context=Other.Call\n")

	cfg := config.Config{Report: config.ReportCALLContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"csv"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", report.Totals.EventCount)
	}
	if report.Rows[0].Context != "Ctx.Call" || !almostEqual(report.Rows[0].TotalDurationMS, 3.0) {
		t.Fatalf("unexpected first row: %+v", report.Rows[0])
	}
}

func TestBuildDBMSSQLContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), "56:15.431019-4500,DBMSSQL,1,process=1CV8C,Sql=select 1,Context=Ctx.DB\n")

	cfg := config.Config{Report: config.ReportDBMSSQLContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 || report.Rows[0].Context != "Ctx.DB" || !almostEqual(report.Rows[0].TotalDurationMS, 4.5) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBuildPostgresContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), "56:15.431019-2500,DBPOSTGRS,1,process=1CV8C,Sql=select 1,Context=Ctx.PG\n")

	cfg := config.Config{Report: config.ReportPostgresContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 || report.Rows[0].Context != "Ctx.PG" || !almostEqual(report.Rows[0].TotalDurationMS, 2.5) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBuildFileDBContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), "56:15.431019-1250,DBV8DBEng,1,process=1CV8C,Func=readFile,Context=Ctx.File\n")

	cfg := config.Config{Report: config.ReportFileDBContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 || report.Rows[0].Context != "Ctx.File" || !almostEqual(report.Rows[0].TotalDurationMS, 1.25) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBuildLockContextAggregatesLockEvents(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,TLOCK,1,process=1CV8C,WaitConnections=1,Context=Ctx.Lock\n"+
		"56:15.431020-3000,TTIMEOUT,1,process=1CV8C,WaitConnections=2,Context=Ctx.Lock\n"+
		"56:15.431021-2000,TDEADLOCK,1,process=1CV8C,DeadlockConnectionIntersections=1,Context=Other.Lock\n")

	cfg := config.Config{Report: config.ReportLockContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 3 {
		t.Fatalf("EventCount = %d, want 3", report.Totals.EventCount)
	}
	if report.Rows[0].Context != "Ctx.Lock" || report.Rows[0].Count != 2 || !almostEqual(report.Rows[0].TotalDurationMS, 4.0) {
		t.Fatalf("unexpected first row: %+v", report.Rows[0])
	}
}

func TestBuildTimeoutContextUsesOnlyTTIMEOUT(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,TLOCK,1,process=1CV8C,Context=Ctx.Lock\n"+
		"56:15.431020-3000,TTIMEOUT,1,process=1CV8C,Context=Ctx.Timeout\n"+
		"56:15.431021-2000,TDEADLOCK,1,process=1CV8C,Context=Ctx.Deadlock\n")

	cfg := config.Config{Report: config.ReportTimeoutContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", report.Totals.EventCount)
	}
	if report.Rows[0].Context != "Ctx.Timeout" || !almostEqual(report.Rows[0].TotalDurationMS, 3.0) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBuildDeadlockContextUsesOnlyTDEADLOCK(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,TLOCK,1,process=1CV8C,Context=Ctx.Lock\n"+
		"56:15.431020-3000,TTIMEOUT,1,process=1CV8C,Context=Ctx.Timeout\n"+
		"56:15.431021-2000,TDEADLOCK,1,process=1CV8C,Context=Ctx.Deadlock\n")

	cfg := config.Config{Report: config.ReportDeadlockContext, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", report.Totals.EventCount)
	}
	if report.Rows[0].Context != "Ctx.Deadlock" || !almostEqual(report.Rows[0].TotalDurationMS, 2.0) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestConfigAliasesResolveForDatabaseReports(t *testing.T) {
	if got := config.NormalizeReport(config.ReportDBPOSTGRSContext); got != config.ReportPostgresContext {
		t.Fatalf("NormalizeReport(DBPOSTGRS) = %q, want %q", got, config.ReportPostgresContext)
	}
	if got := config.NormalizeReport(config.ReportDBV8DBEngContext); got != config.ReportFileDBContext {
		t.Fatalf("NormalizeReport(DBV8DBEng) = %q, want %q", got, config.ReportFileDBContext)
	}
	if got := config.NormalizeReport(config.ReportLocksContext); got != config.ReportLockContext {
		t.Fatalf("NormalizeReport(locks) = %q, want %q", got, config.ReportLockContext)
	}
}

func TestExtractContextTrimsQuotes(t *testing.T) {
	got := extractContext("SDBL,Rows=1,Context='Quoted.Context'", reportSpec{eventNames: makeEventSet("SDBL"), contextPattern: "Context="})
	if got != "Quoted.Context" {
		t.Fatalf("extractContext() = %q, want %q", got, "Quoted.Context")
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.000001
}
