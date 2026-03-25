package app

import (
	"fmt"
	"path/filepath"

	"techlog-stat/internal/config"
	"techlog-stat/internal/model"
	"techlog-stat/internal/output"
	"techlog-stat/internal/report/contextreport"
	"techlog-stat/internal/report/errorreport"
)

func Run(cfg config.Config) error {
	if err := output.EnsureDir(cfg.OutputDir); err != nil {
		return err
	}

	switch cfg.Report {
	case config.ReportSDBLContext, config.ReportCALLContext, config.ReportDBMSSQLContext, config.ReportPostgresContext, config.ReportFileDBContext, config.ReportLockContext, config.ReportTimeoutContext, config.ReportDeadlockContext:
		report, err := contextreport.Build(cfg)
		if err != nil {
			return err
		}
		return writeContextReport(cfg, report)
	case config.ReportErrorDescr:
		report, err := errorreport.Build(cfg)
		if err != nil {
			return err
		}
		return writeErrorReport(cfg, report)
	default:
		return fmt.Errorf("unsupported report: %s", cfg.Report)
	}
}

func writeContextReport(cfg config.Config, report model.ContextReport) error {
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "summary.txt"), output.RenderSummary(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderContextsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "contexts.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderRunJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "run.json"), data); err != nil {
			return err
		}
	}
	if err := output.WriteFile(filepath.Join(cfg.OutputDir, "errors.log"), output.RenderErrors(report.Errors)); err != nil {
		return err
	}
	return nil
}

func writeErrorReport(cfg config.Config, report model.ErrorReport) error {
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "summary.txt"), output.RenderErrorSummary(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderErrorRowsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "errors.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderErrorRunJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "run.json"), data); err != nil {
			return err
		}
	}
	if err := output.WriteFile(filepath.Join(cfg.OutputDir, "errors.log"), output.RenderErrors(report.Errors)); err != nil {
		return err
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, item := range values {
		if item == target {
			return true
		}
	}
	return false
}
