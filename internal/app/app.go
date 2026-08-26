package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"techlog-stat/internal/config"
	"techlog-stat/internal/model"
	"techlog-stat/internal/output"
	comparereport "techlog-stat/internal/report/compare"
	"techlog-stat/internal/report/contextreport"
	"techlog-stat/internal/report/errorreport"
	"techlog-stat/internal/report/overview"
	"techlog-stat/internal/webserver"
	"techlog-stat/internal/webui"
)

func Run(cfg config.Config) error {
	return RunContext(context.Background(), cfg)
}

// RunContext executes a command and allows long-running analyze/serve work to
// stop cleanly when the caller cancels the context.
func RunContext(ctx context.Context, cfg config.Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Report == config.ReportServe {
		return runServe(ctx, cfg)
	}
	if err := output.EnsureDir(cfg.OutputDir); err != nil {
		return err
	}

	switch cfg.Report {
	case config.ReportCompare:
		report, err := comparereport.LoadAndCompare(cfg.BaselinePath, cfg.CurrentPath, comparereport.Options{
			RegressionPercent:      cfg.ThresholdPercent,
			ImprovementPercent:     cfg.ThresholdPercent,
			MinAbsoluteDeltaMicros: cfg.ThresholdAbsMicros,
		})
		if err != nil {
			return err
		}
		return writeCompareReport(cfg, report)
	case config.ReportAnalyze:
		report, err := overview.BuildContext(ctx, overview.Options{
			InputRoot: cfg.InputRoot, Glob: cfg.Glob, BucketInterval: cfg.BucketInterval, TopN: cfg.TopN, Workers: cfg.Workers,
			Filters: cfg.Filters, MinDurationMicros: cfg.MinDurationMicros, TimeRange: cfg.TimeRange,
		})
		if err != nil {
			return err
		}
		return writeOverviewReport(cfg, report)
	case config.ReportSDBLContext, config.ReportCALLContext, config.ReportDBMSSQLContext, config.ReportPostgresContext, config.ReportFileDBContext, config.ReportLockContext, config.ReportTimeoutContext, config.ReportDeadlockContext:
		if cfg.Mode == config.ModeRaw {
			report, err := contextreport.BuildRaw(cfg)
			if err != nil {
				return err
			}
			return writeRawContextReport(cfg, report)
		}
		report, err := contextreport.Build(cfg)
		if err != nil {
			return err
		}
		return writeContextReport(cfg, report)
	case config.ReportErrorDescr:
		if cfg.Mode == config.ModeRaw {
			report, err := errorreport.BuildRaw(cfg)
			if err != nil {
				return err
			}
			return writeRawErrorReport(cfg, report)
		}
		report, err := errorreport.Build(cfg)
		if err != nil {
			return err
		}
		return writeErrorReport(cfg, report)
	default:
		return fmt.Errorf("unsupported report: %s", cfg.Report)
	}
}

func runServe(ctx context.Context, cfg config.Config) error {
	manager, err := webserver.NewManager(webserver.ManagerOptions{
		MaxRuns:          cfg.MaxRuns,
		MaxActive:        cfg.MaxConcurrentRuns,
		AllowedInputRoot: cfg.InputRoot,
		DefaultRequest: webserver.RunRequest{
			InputRoot: cfg.InputRoot, Glob: cfg.Glob, BucketInterval: cfg.BucketInterval.String(),
			TopN: cfg.TopN, Workers: cfg.Workers, MinDurationMicros: cfg.MinDurationMicros,
		},
	})
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	api := webserver.NewHandler(manager, webserver.HandlerOptions{AllowedHosts: []string{cfg.Listen, host}})
	mux := http.NewServeMux()
	mux.Handle("/api", api)
	mux.Handle("/api/", api)
	mux.Handle("/", webui.Handler())
	serveErr := webserver.Serve(ctx, cfg.Listen, mux)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := manager.Shutdown(shutdownCtx)
	if serveErr != nil {
		return serveErr
	}
	return shutdownErr
}

func writeCompareReport(cfg config.Config, report comparereport.Result) error {
	if contains(cfg.Formats, "html") {
		data, err := output.RenderCompareHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "summary.txt"), output.RenderCompareText(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderCompareCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "compare.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderCompareJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "run.json"), data); err != nil {
			return err
		}
	}
	return nil
}

func writeOverviewReport(cfg config.Config, report overview.OverviewResult) error {
	if contains(cfg.Formats, "html") {
		data, err := output.RenderOverviewHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "summary.txt"), output.RenderOverviewText(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderOverviewEventTypesCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "event_types.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewSQLCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "sql.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewTracesCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "traces.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewLocksCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "locks.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewSCALLCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "scall.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewWebCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "web.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewSessionsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "sessions.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewProcessesCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "processes.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewLicensesCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "licenses.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewFileDBCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "filedb.csv"), data); err != nil {
			return err
		}
		data, err = output.RenderOverviewErrorContextsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "error_contexts.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderOverviewJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "run.json"), data); err != nil {
			return err
		}
	}
	return output.WriteFile(filepath.Join(cfg.OutputDir, "errors.log"), output.RenderErrors(report.Errors))
}

func writeContextReport(cfg config.Config, report model.ContextReport) error {
	if contains(cfg.Formats, "html") {
		data, err := output.RenderContextHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
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
	if contains(cfg.Formats, "html") {
		data, err := output.RenderErrorHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
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

func writeRawContextReport(cfg config.Config, report model.RawContextReport) error {
	if contains(cfg.Formats, "html") {
		data, err := output.RenderRawContextHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.txt"), output.RenderRawContextText(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderRawContextsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderRawContextJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.json"), data); err != nil {
			return err
		}
	}
	if err := output.WriteFile(filepath.Join(cfg.OutputDir, "errors.log"), output.RenderErrors(report.Errors)); err != nil {
		return err
	}
	return nil
}

func writeRawErrorReport(cfg config.Config, report model.RawErrorReport) error {
	if contains(cfg.Formats, "html") {
		data, err := output.RenderRawErrorHTML(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "report.html"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "text") {
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.txt"), output.RenderRawErrorText(report)); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "csv") {
		data, err := output.RenderRawErrorsCSV(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.csv"), data); err != nil {
			return err
		}
	}
	if contains(cfg.Formats, "json") {
		data, err := output.RenderRawErrorJSON(report)
		if err != nil {
			return err
		}
		if err := output.WriteFile(filepath.Join(cfg.OutputDir, "raw.json"), data); err != nil {
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
