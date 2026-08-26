package cli

import (
	"reflect"
	"testing"

	"techlog-stat/internal/config"
)

func TestParseCompare(t *testing.T) {
	cfg, err := Parse([]string{"compare", "--baseline", "before.json", "--current", "after.json", "--output", "out", "--threshold-pct", "12.5", "--threshold-abs-us", "250", "--format", "text,csv,json,html"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Report != config.ReportCompare || cfg.BaselinePath != "before.json" || cfg.CurrentPath != "after.json" || cfg.OutputDir != "out" || cfg.ThresholdPercent != 12.5 || cfg.ThresholdAbsMicros != 250 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.Formats, []string{"text", "csv", "json", "html"}) {
		t.Fatalf("formats = %v", cfg.Formats)
	}
}

func TestParseCompareRequiresTwoInputsAndSupportedFormats(t *testing.T) {
	if _, err := Parse([]string{"compare", "--baseline", "before.json", "--output", "out"}); err == nil {
		t.Fatal("missing current accepted")
	}
	if _, err := Parse([]string{"compare", "--baseline", "before.json", "--current", "after.json", "--output", "out", "--format", "xml"}); err == nil {
		t.Fatal("unsupported format accepted")
	}
}

func TestParseExistingReportRemainsCompatible(t *testing.T) {
	cfg, err := Parse([]string{"sdbl-context", "--input", "logs", "--glob", "*.log", "--output", "out"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Report != config.ReportSDBLContext || cfg.BaselinePath != "" || cfg.CurrentPath != "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseServe(t *testing.T) {
	cfg, err := Parse([]string{"serve", "--input", "C:/v8/logs", "--glob", "**/*.log", "--listen", "127.0.0.1:9090", "--workers", "4", "--max-runs", "12", "--max-concurrent", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Report != config.ReportServe || cfg.InputRoot != "C:/v8/logs" || cfg.Listen != "127.0.0.1:9090" || cfg.Workers != 4 || cfg.MaxRuns != 12 || cfg.MaxConcurrentRuns != 2 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseServeRejectsNonLoopbackAndMissingInput(t *testing.T) {
	if _, err := Parse([]string{"serve"}); err == nil {
		t.Fatal("missing input accepted")
	}
	if _, err := Parse([]string{"serve", "--input", "logs", "--listen", "0.0.0.0:8080"}); err == nil {
		t.Fatal("non-loopback listen accepted")
	}
	if _, err := Parse([]string{"serve", "--input", "logs", "--listen", "127.0.0.1:0"}); err == nil {
		t.Fatal("zero listen port accepted")
	}
	if _, err := Parse([]string{"serve", "--input", "logs", "--listen", "localhost:not-a-port"}); err == nil {
		t.Fatal("non-numeric listen port accepted")
	}
}
