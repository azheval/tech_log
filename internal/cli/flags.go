package cli

import (
	"flag"
	"fmt"
	"strings"

	"techlog-stat/internal/config"
)

func Parse(args []string) (config.Config, error) {
	var cfg config.Config
	if len(args) == 0 {
		return cfg, fmt.Errorf("report name is required, for example: techlog-stat sdbl-context --input <dir> --glob <pattern> --output <dir>")
	}

	cfg.Report = config.NormalizeReport(args[0])

	fs := flag.NewFlagSet(cfg.Report, flag.ContinueOnError)
	fs.StringVar(&cfg.InputRoot, "input", "", "root directory with logs")
	fs.StringVar(&cfg.Glob, "glob", "*/*.log", "file mask relative to input root; use / as separator, for example */*.log or rphost_*/*.*")
	fs.StringVar(&cfg.OutputDir, "output", "", "output directory for report files")

	formats := fs.String("format", "text,csv,json", "comma-separated output formats")
	fs.IntVar(&cfg.TopN, "top", 100, "number of ranked rows to write")
	fs.IntVar(&cfg.Workers, "workers", 1, "file-level parallelism")

	if err := fs.Parse(args[1:]); err != nil {
		return cfg, err
	}

	cfg.Formats = splitList(*formats)
	if err := cfg.Validate(); err != nil {
		return cfg, err
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
