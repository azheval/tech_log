package output

import (
	"bytes"
	"fmt"

	"techlog-stat/internal/model"
	"techlog-stat/internal/report/overview"
)

func RenderSummary(report model.ContextReport) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Report: %s\n", reportTitle(report.Meta.Report))
	fmt.Fprintf(&buf, "GeneratedAt: %s\n", report.Meta.FinishedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&buf, "InputRoot: %s\n", report.Meta.InputRoot)
	fmt.Fprintf(&buf, "Glob: %s\n", report.Meta.Glob)
	fmt.Fprintf(&buf, "FilesMatched: %d\n", report.Meta.FilesMatched)
	fmt.Fprintf(&buf, "FilesProcessed: %d\n", report.Meta.FilesProcessed)
	fmt.Fprintf(&buf, "FilesFailed: %d\n", report.Meta.FilesFailed)
	fmt.Fprintf(&buf, "Workers: %d\n", report.Meta.Workers)
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "TotalEvents: %d\n", report.Totals.EventCount)
	fmt.Fprintf(&buf, "TotalDurationMs: %s\n", formatHumanFloat(report.Totals.DurationMS))
	fmt.Fprintf(&buf, "AverageDurationMs: %s\n", formatHumanFloat(report.Totals.AverageDuration))
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "TopContextsByDuration:\n")

	for _, row := range report.Rows {
		fmt.Fprintf(&buf, "%d. Context=%s\n", row.Rank, row.Context)
		fmt.Fprintf(&buf, "   ShortContext=%s\n", row.ShortContext)
		fmt.Fprintf(&buf, "   TotalMs=%s\n", formatHumanFloat(row.TotalDurationMS))
		fmt.Fprintf(&buf, "   TimePct=%s\n", formatPercent(row.TimePct))
		fmt.Fprintf(&buf, "   Count=%d\n", row.Count)
		fmt.Fprintf(&buf, "   CountPct=%s\n", formatPercent(row.CountPct))
		fmt.Fprintf(&buf, "   AvgMs=%s\n", formatHumanFloat(row.AverageMS))
		fmt.Fprintf(&buf, "\n")
	}

	return withUTF8BOM(buf.Bytes())
}

func RenderErrorSummary(report model.ErrorReport) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Report: %s\n", reportTitle(report.Meta.Report))
	fmt.Fprintf(&buf, "GeneratedAt: %s\n", report.Meta.FinishedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&buf, "InputRoot: %s\n", report.Meta.InputRoot)
	fmt.Fprintf(&buf, "Glob: %s\n", report.Meta.Glob)
	fmt.Fprintf(&buf, "FilesMatched: %d\n", report.Meta.FilesMatched)
	fmt.Fprintf(&buf, "FilesProcessed: %d\n", report.Meta.FilesProcessed)
	fmt.Fprintf(&buf, "FilesFailed: %d\n", report.Meta.FilesFailed)
	fmt.Fprintf(&buf, "Workers: %d\n", report.Meta.Workers)
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "TotalEvents: %d\n", report.Totals.EventCount)
	fmt.Fprintf(&buf, "TotalDurationMs: %s\n", formatHumanFloat(report.Totals.DurationMS))
	fmt.Fprintf(&buf, "AverageDurationMs: %s\n", formatHumanFloat(report.Totals.AverageDuration))
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "TopErrorsByCount:\n")

	for _, row := range report.Rows {
		fmt.Fprintf(&buf, "%d. Event=%s\n", row.Rank, row.Event)
		fmt.Fprintf(&buf, "   Description=%s\n", row.Description)
		fmt.Fprintf(&buf, "   ShortDescription=%s\n", row.ShortDescription)
		fmt.Fprintf(&buf, "   TotalMs=%s\n", formatHumanFloat(row.TotalDurationMS))
		fmt.Fprintf(&buf, "   TimePct=%s\n", formatPercent(row.TimePct))
		fmt.Fprintf(&buf, "   Count=%d\n", row.Count)
		fmt.Fprintf(&buf, "   CountPct=%s\n", formatPercent(row.CountPct))
		fmt.Fprintf(&buf, "   AvgMs=%s\n", formatHumanFloat(row.AverageMS))
		fmt.Fprintf(&buf, "\n")
	}

	return withUTF8BOM(buf.Bytes())
}

func RenderOverviewText(report overview.OverviewResult) []byte {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "Report: Technological Log Overview")
	fmt.Fprintf(&buf, "GeneratedAt: %s\n", report.Meta.FinishedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&buf, "InputRoot: %s\nGlob: %s\n", report.Meta.InputRoot, report.Meta.Glob)
	fmt.Fprintf(&buf, "FilesMatched: %d\nFilesProcessed: %d\nFilesFailed: %d\n", report.Meta.FilesMatched, report.Meta.FilesProcessed, report.Meta.FilesFailed)
	fmt.Fprintf(&buf, "EventsParsed: %d\nMalformedHeaders: %d\nOrphanLines: %d\nBytesRead: %d\n\n", report.Quality.EventsParsed, report.Quality.MalformedHeaders, report.Quality.OrphanLines, report.Quality.BytesRead)
	fmt.Fprintf(&buf, "TotalEvents: %d\nTotalDurationMicros: %s\nAverageDurationMicros: %s\n\n", report.Totals.Count, formatHumanFloat(report.Totals.Duration.Sum), formatHumanFloat(report.Totals.Duration.Mean))
	fmt.Fprintln(&buf, "EventTypes:")
	for _, row := range report.EventTypes {
		duration := row.Stats.Duration
		fmt.Fprintf(&buf, "%s: count=%d total_us=%s p95_us=%s max_us=%s\n", row.Event, row.Stats.Count, formatHumanFloat(duration.Sum), formatHumanFloat(duration.P95), formatHumanFloat(duration.Max))
	}
	fmt.Fprintln(&buf, "\nTopSQL:")
	for index, row := range report.SQLRows {
		if index == 20 {
			fmt.Fprintf(&buf, "... %d more SQL rows in sql.csv and run.json\n", len(report.SQLRows)-index)
			break
		}
		fmt.Fprintf(&buf, "%d. %s count=%d total_us=%d p95_us=%d query=%s\n", index+1, row.EventType, row.Count, row.TotalDurationMicros, row.P95DurationMicros, row.NormalizedQuery)
	}
	fmt.Fprintln(&buf, "\nTraceQuality:")
	quality := report.TraceQuality
	fmt.Fprintf(&buf, "EventsConsumed=%d Calls=%d Contexts=%d CorrelatedEvents=%d OrphanEvents=%d MissingContextTraces=%d\n", quality.EventsConsumed, quality.Calls, quality.Contexts, quality.CorrelatedEvents, quality.OrphanEvents, quality.MissingContextTraces)
	fmt.Fprintf(&buf, "EvictedOpenTraces=%d EvictedCompletedTraces=%d RetainedOpenTraces=%d RetainedCompletedTraces=%d\n", quality.EvictedOpenTraces, quality.EvictedCompletedTraces, quality.RetainedOpenTraces, quality.RetainedCompletedTraces)
	fmt.Fprintln(&buf, "\nLocksSummary:")
	locks := report.Locks
	fmt.Fprintf(&buf, "LockEvents=%d Types=%d Contexts=%d Tables=%d Regions=%d TopConflicts=%d Samples=%d ExplicitRelations=%d\n", locks.Quality.LockEvents, len(locks.ByEvent), len(locks.ByContext), len(locks.ByTable), len(locks.ByRegion), len(locks.TopConflicts), len(locks.Samples), len(locks.Relations))
	fmt.Fprintln(&buf, "LockQuality:")
	fmt.Fprintf(&buf, "IgnoredEvents=%d MissingContext=%d MissingLocks=%d MissingRegions=%d EventsWithExplicitRelation=%d\n", locks.Quality.IgnoredEvents, locks.Quality.MissingContext, locks.Quality.MissingLocks, locks.Quality.MissingRegions, locks.Quality.EventsWithExplicitRelation)
	fmt.Fprintln(&buf, "TopLockConflicts:")
	for index, row := range locks.TopConflicts {
		if index == 20 {
			fmt.Fprintf(&buf, "... %d more conflicts in locks.csv and run.json\n", len(locks.TopConflicts)-index)
			break
		}
		fmt.Fprintf(&buf, "%d. %s context=%s tables=%v regions=%v count=%d total_us=%d p95_us=%d\n", index+1, row.EventType, row.Context, row.Tables, row.Regions, row.Stats.Count, row.Stats.TotalMicros, row.Stats.P95Micros)
	}
	fmt.Fprintln(&buf, "\nSCALLSummary:")
	fmt.Fprintf(&buf, "Calls=%d Groups=%d Interfaces=%d Methods=%d SlowSamples=%d\n", report.SCALL.Quality.CallEvents, len(report.SCALL.ByCall), len(report.SCALL.ByInterface), len(report.SCALL.ByMethod), len(report.SCALL.SlowSamples))
	for index, row := range report.SCALL.ByCall {
		if index == 10 {
			fmt.Fprintf(&buf, "... %d more SCALL rows in scall.csv and run.json\n", len(report.SCALL.ByCall)-index)
			break
		}
		fmt.Fprintf(&buf, "%d. interface=%s iname=%s method=%s count=%d total_us=%s p95_us=%s\n", index+1, row.Interface, row.IName, row.Method, row.Metrics.Duration.Count, formatHumanFloat(row.Metrics.Duration.Sum), formatHumanFloat(row.Metrics.Duration.P95))
	}
	fmt.Fprintln(&buf, "\nWebSummary:")
	fmt.Fprintf(&buf, "Requests=%d Responses=%d Matched=%d OrphanResponses=%d CacheEvents=%d CacheHits=%d CacheMisses=%d\n", report.Web.Quality.Requests, report.Web.Quality.Responses, report.Web.Quality.MatchedResponses, report.Web.Quality.OrphanResponses, report.Web.Quality.CacheEvents, report.Web.Quality.CacheHits, report.Web.Quality.CacheMisses)
	for index, row := range report.Web.Requests {
		if index == 10 {
			fmt.Fprintf(&buf, "... %d more web rows in web.csv and run.json\n", len(report.Web.Requests)-index)
			break
		}
		fmt.Fprintf(&buf, "%d. %s %s status=%s count=%d total_us=%d p95_us=%d\n", index+1, row.Method, row.URI, row.Status, row.Count, row.Stats.TotalMicros, row.Stats.P95Micros)
	}
	fmt.Fprintln(&buf, "\nSessionsSummary:")
	fmt.Fprintf(&buf, "LifecycleEvents=%d Completed=%d Open=%d OrphanFinishes=%d Peak=%d DurationSource=timestamp_pair\n", report.Sessions.Quality.LifecycleEvents, report.Sessions.Quality.CompletedSessions, report.Sessions.Quality.OpenSessions, report.Sessions.Quality.UnmatchedFinishes, report.Sessions.Peak)
	fmt.Fprintln(&buf, "\nProcessSummary:")
	fmt.Fprintf(&buf, "PROC=%d SCOM=%d ProcessGroups=%d Operations=%d ExplicitRelations=%d\n", report.ProcessStats.Quality.PROCEvents, report.ProcessStats.Quality.SCOMEvents, len(report.ProcessStats.PROCByProcess), len(report.ProcessStats.SCOMByOperation), len(report.ProcessStats.ExplicitProcessRelations))
	fmt.Fprintln(&buf, "\nLicenseSummary:")
	fmt.Fprintf(&buf, "LIC=%d HASP=%d Failures=%d Expired=%d WrongType=%d LicenseGroups=%d HASPGroups=%d\n", report.Licenses.Quality.LicenseEvents, report.Licenses.Quality.HASPEvents, report.Licenses.Quality.Failures, report.Licenses.Quality.Expired, report.Licenses.Quality.WrongType, len(report.Licenses.Licenses), len(report.Licenses.HASP))
	fmt.Fprintln(&buf, "\nErrorContextSummary:")
	fmt.Fprintf(&buf, "Errors=%d Contexts=%d Matched=%d Orphan=%d Ambiguous=%d Groups=%d\n", report.ErrorContext.Quality.ErrorEvents, report.ErrorContext.Quality.ContextEvents, report.ErrorContext.Quality.MatchedContexts, report.ErrorContext.Quality.OrphanContexts, report.ErrorContext.Quality.AmbiguousContexts, len(report.ErrorContext.Groups))
	fmt.Fprintln(&buf, "\nFileDBSummary:")
	fmt.Fprintf(&buf, "DBV8DBEng=%d FuncGroups=%d Tables=%d Categories=%d Classes=%d ExplicitErrors=%d\n", report.FileDB.Quality.DBV8DBEngEvents, len(report.FileDB.ByFunc), len(report.FileDB.ByTable), len(report.FileDB.ByCategory), len(report.FileDB.ByClass), report.FileDB.Quality.ExplicitErrorEvents)
	return withUTF8BOM(buf.Bytes())
}

func RenderRawContextText(report model.RawContextReport) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Report: %s (Raw)\n", reportTitle(report.Meta.Report))
	fmt.Fprintf(&buf, "GeneratedAt: %s\n", report.Meta.FinishedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&buf, "InputRoot: %s\n", report.Meta.InputRoot)
	fmt.Fprintf(&buf, "Glob: %s\n", report.Meta.Glob)
	fmt.Fprintf(&buf, "FilesMatched: %d\n", report.Meta.FilesMatched)
	fmt.Fprintf(&buf, "FilesProcessed: %d\n", report.Meta.FilesProcessed)
	fmt.Fprintf(&buf, "FilesFailed: %d\n", report.Meta.FilesFailed)
	fmt.Fprintf(&buf, "Workers: %d\n", report.Meta.Workers)
	fmt.Fprintf(&buf, "TopPerHour: %d\n\n", report.Meta.TopN)

	for _, day := range report.Days {
		fmt.Fprintf(&buf, "Date: %s\n", day.Date)
		for _, hour := range day.Hours {
			fmt.Fprintf(&buf, "Hour: %s\n", hour.Hour.Format("15:00"))
			for idx, event := range hour.Events {
				fmt.Fprintf(&buf, "%d. Timestamp=%s\n", idx+1, event.Timestamp.Format("2006-01-02 15:04:05.000000"))
				fmt.Fprintf(&buf, "   Event=%s\n", event.Event)
				fmt.Fprintf(&buf, "   File=%s\n", event.File)
				fmt.Fprintf(&buf, "   DurationMicros=%d\n", event.DurationMicros)
				fmt.Fprintf(&buf, "   DurationMs=%s\n", formatHumanFloat(event.DurationMS))
				fmt.Fprintf(&buf, "   Context=%s\n", event.Context)
				fmt.Fprintf(&buf, "   ShortContext=%s\n", event.ShortContext)
			}
			fmt.Fprintf(&buf, "\n")
		}
	}

	return withUTF8BOM(buf.Bytes())
}

func RenderRawErrorText(report model.RawErrorReport) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Report: %s (Raw)\n", reportTitle(report.Meta.Report))
	fmt.Fprintf(&buf, "GeneratedAt: %s\n", report.Meta.FinishedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&buf, "InputRoot: %s\n", report.Meta.InputRoot)
	fmt.Fprintf(&buf, "Glob: %s\n", report.Meta.Glob)
	fmt.Fprintf(&buf, "FilesMatched: %d\n", report.Meta.FilesMatched)
	fmt.Fprintf(&buf, "FilesProcessed: %d\n", report.Meta.FilesProcessed)
	fmt.Fprintf(&buf, "FilesFailed: %d\n", report.Meta.FilesFailed)
	fmt.Fprintf(&buf, "Workers: %d\n", report.Meta.Workers)
	fmt.Fprintf(&buf, "TopPerHour: %d\n\n", report.Meta.TopN)

	for _, day := range report.Days {
		fmt.Fprintf(&buf, "Date: %s\n", day.Date)
		for _, hour := range day.Hours {
			fmt.Fprintf(&buf, "Hour: %s\n", hour.Hour.Format("15:00"))
			for idx, event := range hour.Events {
				fmt.Fprintf(&buf, "%d. Timestamp=%s\n", idx+1, event.Timestamp.Format("2006-01-02 15:04:05.000000"))
				fmt.Fprintf(&buf, "   Event=%s\n", event.Event)
				fmt.Fprintf(&buf, "   File=%s\n", event.File)
				fmt.Fprintf(&buf, "   DurationMicros=%d\n", event.DurationMicros)
				fmt.Fprintf(&buf, "   DurationMs=%s\n", formatHumanFloat(event.DurationMS))
				fmt.Fprintf(&buf, "   Description=%s\n", event.Description)
				fmt.Fprintf(&buf, "   ShortDescription=%s\n", event.ShortDescription)
			}
			fmt.Fprintf(&buf, "\n")
		}
	}

	return withUTF8BOM(buf.Bytes())
}
