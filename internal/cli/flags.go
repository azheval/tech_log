package cli

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"techlog-stat/internal/config"
)

type filterFlags []string

func (f *filterFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *filterFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func Parse(args []string) (config.Config, error) {
	var cfg config.Config
	if len(args) == 0 {
		return cfg, fmt.Errorf("report name is required, for example: techlog-stat sdbl-context --input <dir> --glob <pattern> --output <dir>")
	}

	cfg.Report = config.NormalizeReport(args[0])
	cfg.Mode = config.ModeAggregate
	formatDefault := "text,csv,json"
	if cfg.Report == config.ReportCompare {
		formatDefault = "text,csv,json,html"
	}

	fs := flag.NewFlagSet(cfg.Report, flag.ContinueOnError)
	fs.StringVar(&cfg.InputRoot, "input", "", "root directory with logs")
	fs.StringVar(&cfg.Glob, "glob", "*/*.log", "file mask relative to input root; use / as separator, for example */*.log or rphost_*/*.*")
	fs.StringVar(&cfg.OutputDir, "output", "", "output directory for report files")
	fs.StringVar(&cfg.Mode, "mode", config.ModeAggregate, "output mode: aggregate or raw")

	formats := fs.String("format", formatDefault, "comma-separated output formats")
	fs.IntVar(&cfg.TopN, "top", 100, "number of ranked rows to write")
	fs.IntVar(&cfg.Workers, "workers", 1, "file-level parallelism")
	fs.StringVar(&cfg.Listen, "listen", "127.0.0.1:8080", "loopback address for the local web interface")
	fs.IntVar(&cfg.MaxRuns, "max-runs", 8, "maximum completed and active web runs retained in memory")
	fs.IntVar(&cfg.MaxConcurrentRuns, "max-concurrent", 1, "maximum concurrently running web analyses")
	fs.StringVar(&cfg.BaselinePath, "baseline", "", "baseline overview JSON for compare")
	fs.StringVar(&cfg.CurrentPath, "current", "", "current overview JSON for compare")
	fs.Float64Var(&cfg.ThresholdPercent, "threshold-pct", 5, "percent change threshold for compare")
	fs.Float64Var(&cfg.ThresholdAbsMicros, "threshold-abs-us", 0, "absolute duration delta threshold in microseconds for compare")
	bucket := fs.String("bucket", "1m", "time bucket for analyze, for example 1m, 5m, or 1h")
	duration := fs.String("duration", "", "minimum event duration filter; bare number means seconds, examples: 5, 5s, 500ms")
	dateFrom := fs.String("date-from", "", "inclusive date filter in YYYY-MM-DD")
	dateTo := fs.String("date-to", "", "inclusive date filter in YYYY-MM-DD")
	timeFrom := fs.String("time-from", "", "time-of-day filter from HH:MM or HH:MM:SS")
	timeTo := fs.String("time-to", "", "time-of-day filter to HH:MM or HH:MM:SS")

	var rawFilters filterFlags
	fs.Var(&rawFilters, "filter", "raw event filter in key=value form; can be passed multiple times")

	if err := fs.Parse(args[1:]); err != nil {
		return cfg, err
	}

	cfg.Mode = config.NormalizeMode(cfg.Mode)
	cfg.Formats = splitList(*formats)
	for _, raw := range rawFilters {
		filter, err := config.ParseFilter(raw)
		if err != nil {
			return cfg, err
		}
		cfg.Filters = append(cfg.Filters, filter)
	}
	minDurationMicros, err := config.ParseMinDurationMicros(*duration)
	if err != nil {
		return cfg, err
	}
	cfg.MinDurationMicros = minDurationMicros
	cfg.BucketInterval, err = time.ParseDuration(*bucket)
	if err != nil {
		return cfg, fmt.Errorf("invalid --bucket %q: %w", *bucket, err)
	}
	cfg.TimeRange.DateFrom, cfg.TimeRange.HasDateFrom, err = config.ParseDate(*dateFrom)
	if err != nil {
		return cfg, err
	}
	cfg.TimeRange.DateTo, cfg.TimeRange.HasDateTo, err = config.ParseDate(*dateTo)
	if err != nil {
		return cfg, err
	}
	cfg.TimeRange.TimeFrom, cfg.TimeRange.HasTimeFrom, err = config.ParseTimeOfDay(*timeFrom)
	if err != nil {
		return cfg, err
	}
	cfg.TimeRange.TimeTo, cfg.TimeRange.HasTimeTo, err = config.ParseTimeOfDay(*timeTo)
	if err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	if cfg.Report == config.ReportCompare {
		for _, format := range cfg.Formats {
			switch format {
			case "text", "csv", "json", "html":
			default:
				return cfg, fmt.Errorf("unsupported compare format: %s", format)
			}
		}
	}

	return cfg, nil
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
