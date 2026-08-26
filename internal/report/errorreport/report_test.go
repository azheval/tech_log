package errorreport

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"techlog-stat/internal/config"
)

func TestBuildErrorDescrAggregatesEXCPAndQERR(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,EXCP,1,process=1CV8C,Descr='Error on 127.0.0.1:1541 UUID 12345678-1234-1234-1234-1234567890ab'\n"+
		"56:15.431020-2000,EXCP,1,process=1CV8C,Descr='Error on 127.0.0.1:1541 UUID 12345678-1234-1234-1234-1234567890ab'\n"+
		"56:15.431021-500,QERR,1,process=1CV8C,Descr='Query failed'\n")

	cfg := config.Config{Report: config.ReportErrorDescr, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"text", "csv", "json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Totals.EventCount != 3 {
		t.Fatalf("EventCount = %d, want 3", report.Totals.EventCount)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(report.Rows))
	}
	if report.Rows[0].Event != "EXCP" || report.Rows[0].Count != 2 || !almostEqual(report.Rows[0].TotalDurationMS, 3.0) {
		t.Fatalf("unexpected first row: %+v", report.Rows[0])
	}
	if report.Rows[0].Description != "Error on {IPV4} UUID {UUID}" {
		t.Fatalf("normalized description = %q", report.Rows[0].Description)
	}
}

func TestBuildErrorDescrAppliesRawFilters(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,EXCP,1,process=1CV8C,Usr=DefUser,DataBase=conf_null,Descr='Accepted'\n"+
		"56:15.431020-2000,EXCP,1,process=1CV8C,Usr=OtherUser,DataBase=conf_null,Descr='Rejected'\n")

	cfg := config.Config{
		Report:    config.ReportErrorDescr,
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
	if len(report.Rows) != 1 || report.Rows[0].Description != "Accepted" {
		t.Fatalf("unexpected rows: %+v", report.Rows)
	}
}

func TestBuildErrorDescrParsesMultilineDescription(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "26032306.log"), ""+
		"56:15.431019-1000,EXCP,1,process=1CV8C,Descr='first line\n"+
		"second line with 127.0.0.1:1541'\n")

	cfg := config.Config{Report: config.ReportErrorDescr, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(report.Rows))
	}
	if got, want := report.Rows[0].Description, "first line second line with {IPV4}"; got != want {
		t.Fatalf("Description = %q, want %q", got, want)
	}
}

func TestBuildErrorDescrReportsInvalidLogFilename(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "procA_123", "invalid.log"), "56:15.431019-1000,EXCP,1,Descr='failure'\n")

	cfg := config.Config{Report: config.ReportErrorDescr, InputRoot: root, Glob: "*/*.log", OutputDir: filepath.Join(root, "out"), Formats: []string{"json"}, TopN: 10, Workers: 1}
	report, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Meta.FilesProcessed != 0 || report.Meta.FilesFailed != 1 {
		t.Fatalf("file counts = processed:%d failed:%d, want processed:0 failed:1", report.Meta.FilesProcessed, report.Meta.FilesFailed)
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "invalid log filename \"invalid.log\"") {
		t.Fatalf("Errors = %#v, want invalid filename diagnostic", report.Errors)
	}
}

func TestNormalizeReportAlias(t *testing.T) {
	if got := config.NormalizeReport(config.ReportEXCPDescr); got != config.ReportErrorDescr {
		t.Fatalf("NormalizeReport(excp-descr) = %q, want %q", got, config.ReportErrorDescr)
	}
}

func TestNormalizeDescriptionMasksDateTime(t *testing.T) {
	got := normalizeDescription("prefix \\x{043D}\\x{0430}\\x{0447}\\x{0430}\\x{0442}: 12.02.2026 \\x{0432} 10:20:30")
	if got != "prefix {DtTm}" {
		t.Fatalf("normalizeDescription() = %q", got)
	}
}

func TestNormalizeDescriptionMasksVolatileValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "Russian date and time",
			in:   "операция начат: 2.3.2026 в 9:05:07",
			want: "операция {DtTm}",
		},
		{
			name: "IPv4 endpoint",
			in:   "connection to 192.168.10.25:1541 failed",
			want: "connection to {IPV4} failed",
		},
		{
			name: "IPv6 endpoint with zone",
			in:   "connection to [fe80::a8b:ccff:fe00:42%Ethernet-1]:1541 failed",
			want: "connection to {IPV6} failed",
		},
		{
			name: "UUID",
			in:   "object 550E8400-E29B-41D4-A716-446655440000 is unavailable",
			want: "object {UUID} is unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDescription(tt.in); got != tt.want {
				t.Fatalf("normalizeDescription() = %q, want %q", got, tt.want)
			}
		})
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
