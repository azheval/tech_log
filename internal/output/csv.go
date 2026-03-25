package output

import (
	"bytes"
	"encoding/csv"
	"strconv"

	"techlog-stat/internal/model"
)

func RenderContextsCSV(report model.ContextReport) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write([]string{
		"rank",
		"context",
		"short_context",
		"total_duration_ms",
		"time_pct",
		"count",
		"count_pct",
		"avg_duration_ms",
	}); err != nil {
		return nil, err
	}

	for _, row := range report.Rows {
		record := []string{
			strconv.Itoa(row.Rank),
			row.Context,
			row.ShortContext,
			formatHumanFloat(row.TotalDurationMS),
			formatPercent(row.TimePct),
			strconv.FormatInt(row.Count, 10),
			formatPercent(row.CountPct),
			formatHumanFloat(row.AverageMS),
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

func RenderErrorRowsCSV(report model.ErrorReport) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write([]string{
		"rank",
		"event",
		"description",
		"short_description",
		"total_duration_ms",
		"time_pct",
		"count",
		"count_pct",
		"avg_duration_ms",
	}); err != nil {
		return nil, err
	}

	for _, row := range report.Rows {
		record := []string{
			strconv.Itoa(row.Rank),
			row.Event,
			row.Description,
			row.ShortDescription,
			formatHumanFloat(row.TotalDurationMS),
			formatPercent(row.TimePct),
			strconv.FormatInt(row.Count, 10),
			formatPercent(row.CountPct),
			formatHumanFloat(row.AverageMS),
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

func formatHumanFloat(v float64) string {
	switch {
	case v >= 1:
		return strconv.FormatFloat(v, 'f', 2, 64)
	case v >= 0.01:
		return strconv.FormatFloat(v, 'f', 3, 64)
	default:
		return strconv.FormatFloat(v, 'f', 4, 64)
	}
}

func formatPercent(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
