package output

import (
	"bytes"
	"fmt"

	"techlog-stat/internal/model"
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
