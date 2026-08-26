package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/analysis/lockstats"
	"techlog-stat/internal/model"
	"techlog-stat/internal/report/overview"
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

// RenderOverviewEventTypesCSV writes aggregate statistics for each event type.
func RenderOverviewEventTypesCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"event", "count", "total_duration_micros", "average_micros", "p50_micros", "p90_micros", "p95_micros", "p99_micros", "max_micros"}); err != nil {
		return nil, err
	}
	for _, row := range report.EventTypes {
		duration := row.Stats.Duration
		if err := writer.Write([]string{
			row.Event, strconv.FormatInt(row.Stats.Count, 10), formatHumanFloat(duration.Sum), formatHumanFloat(duration.Mean),
			formatHumanFloat(duration.P50), formatHumanFloat(duration.P90), formatHumanFloat(duration.P95), formatHumanFloat(duration.P99), formatHumanFloat(duration.Max),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewSQLCSV writes one row per normalized SQL/SDBL fingerprint.
func RenderOverviewSQLCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"fingerprint", "event_type", "normalized_query", "sample", "count", "total_duration_micros", "mean_duration_micros", "p50_duration_micros", "p95_duration_micros", "p99_duration_micros", "rows_sum", "rows_max", "contexts", "users", "databases"}); err != nil {
		return nil, err
	}
	for _, row := range report.SQLRows {
		if err := writer.Write([]string{
			row.Fingerprint, row.EventType, row.NormalizedQuery, row.Sample, strconv.FormatInt(row.Count, 10),
			strconv.FormatInt(row.TotalDurationMicros, 10), formatHumanFloat(row.MeanDurationMicros), strconv.FormatInt(row.P50DurationMicros, 10),
			strconv.FormatInt(row.P95DurationMicros, 10), strconv.FormatInt(row.P99DurationMicros, 10), strconv.FormatInt(row.RowsSum, 10), strconv.FormatInt(row.RowsMax, 10),
			strings.Join(row.Contexts, " | "), strings.Join(row.Users, " | "), strings.Join(row.Databases, " | "),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewTracesCSV flattens each trace into its spans for spreadsheet analysis.
func RenderOverviewTracesCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"trace_id", "trace_started_at", "trace_last_at", "source", "process", "os_thread", "client_id", "call_id", "trans", "span_index", "span_timestamp", "event", "duration_micros", "level", "fields_json", "raw"}); err != nil {
		return nil, err
	}
	for _, item := range report.Traces {
		for index, span := range item.Spans {
			fields, err := json.Marshal(span.Fields)
			if err != nil {
				return nil, err
			}
			if err := writer.Write([]string{
				item.ID, item.StartedAt.Format(time.RFC3339Nano), item.LastAt.Format(time.RFC3339Nano), item.Source, item.Process, item.OSThread, item.ClientID, item.CallID, item.Trans,
				strconv.Itoa(index + 1), span.Timestamp.Format(time.RFC3339Nano), span.Event, strconv.FormatInt(span.DurationMicros, 10), strconv.Itoa(span.Level), string(fields), span.Raw,
			}); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewLocksCSV flattens lock aggregates, conflicts, relations, and
// retained raw samples into one spreadsheet-friendly file.
func RenderOverviewLocksCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "event_type", "context", "tables", "regions", "count", "total_duration_micros", "min_duration_micros", "max_duration_micros", "mean_duration_micros", "p50_duration_micros", "p95_duration_micros", "p99_duration_micros", "waiter", "blocker", "timestamp", "source", "raw"}); err != nil {
		return nil, err
	}
	writeAggregate := func(kind string, rows []lockstats.Aggregate) error {
		for _, row := range rows {
			stats := row.Stats
			if err := writer.Write([]string{kind, row.Key, "", "", "", "", strconv.FormatInt(stats.Count, 10), strconv.FormatInt(stats.TotalMicros, 10), strconv.FormatInt(stats.MinMicros, 10), strconv.FormatInt(stats.MaxMicros, 10), formatHumanFloat(stats.MeanMicros), strconv.FormatInt(stats.P50Micros, 10), strconv.FormatInt(stats.P95Micros, 10), strconv.FormatInt(stats.P99Micros, 10), "", "", "", "", ""}); err != nil {
				return err
			}
		}
		return nil
	}
	locks := report.Locks
	for _, group := range []struct {
		kind string
		rows []lockstats.Aggregate
	}{{"event", locks.ByEvent}, {"context", locks.ByContext}, {"table", locks.ByTable}, {"region", locks.ByRegion}} {
		if err := writeAggregate(group.kind, group.rows); err != nil {
			return nil, err
		}
	}
	for _, row := range locks.TopConflicts {
		stats := row.Stats
		if err := writer.Write([]string{"conflict", "", row.EventType, row.Context, strings.Join(row.Tables, " | "), strings.Join(row.Regions, " | "), strconv.FormatInt(stats.Count, 10), strconv.FormatInt(stats.TotalMicros, 10), strconv.FormatInt(stats.MinMicros, 10), strconv.FormatInt(stats.MaxMicros, 10), formatHumanFloat(stats.MeanMicros), strconv.FormatInt(stats.P50Micros, 10), strconv.FormatInt(stats.P95Micros, 10), strconv.FormatInt(stats.P99Micros, 10), "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range locks.Relations {
		if err := writer.Write([]string{"relation", "", row.EventType, row.Context, "", "", "", "", "", "", "", "", "", "", row.Waiter, row.Blocker, "", row.Source, ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range locks.Samples {
		if err := writer.Write([]string{"sample", "", row.EventType, row.Context, strings.Join(row.Tables, " | "), strings.Join(row.Regions, " | "), "", strconv.FormatInt(row.DurationMicros, 10), "", "", "", "", "", "", "", "", row.Timestamp.Format(time.RFC3339Nano), row.Source, row.Raw}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewSCALLCSV flattens SCALL aggregates and bounded slow samples.
func RenderOverviewSCALLCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "interface", "iname", "method", "timestamp", "source", "context", "user", "database", "process", "count", "total_duration_micros", "mean_duration_micros", "p95_duration_micros", "in_bytes_total", "out_bytes_total", "cpu_time_total", "memory_mean", "memory_peak_max", "call_wait_total", "raw"}); err != nil {
		return nil, err
	}
	writeMetrics := func(kind, key, iface, iname, method string, metricsRows []string) error {
		return writer.Write(append([]string{kind, key, iface, iname, method, "", "", "", "", "", ""}, metricsRows...))
	}
	for _, row := range report.SCALL.ByCall {
		m := row.Metrics
		if err := writeMetrics("call", row.Interface+" | "+row.IName+" | "+row.Method, row.Interface, row.IName, row.Method, scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByInterface {
		m := group.Metrics
		if err := writeMetrics("interface", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByIName {
		m := group.Metrics
		if err := writeMetrics("iname", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByMethod {
		m := group.Metrics
		if err := writeMetrics("method", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByContext {
		m := group.Metrics
		if err := writeMetrics("context", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByUser {
		m := group.Metrics
		if err := writeMetrics("user", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByDatabase {
		m := group.Metrics
		if err := writeMetrics("database", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, group := range report.SCALL.ByProcess {
		m := group.Metrics
		if err := writeMetrics("process", group.Key, "", "", "", scallMetrics(m.Duration.Count, m.Duration.Sum, m.Duration.Mean, m.Duration.P95, m.InBytes.Sum, m.OutBytes.Sum, m.CPUTime.Sum, m.Memory.Mean, m.MemoryPeak.Max, m.CallWait.Sum)); err != nil {
			return nil, err
		}
	}
	for _, row := range report.SCALL.SlowSamples {
		if err := writer.Write([]string{"sample", "", row.Interface, row.IName, row.Method, row.Timestamp.Format(time.RFC3339Nano), row.Source, row.Context, row.User, row.Database, row.Process, "", strconv.FormatInt(row.DurationMicros, 10), "", "", "", "", "", "", "", "", row.Raw}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

func scallMetrics(count uint64, total, mean, p95, inBytes, outBytes, cpu, memory, memoryPeak, callWait float64) []string {
	return []string{strconv.FormatUint(count, 10), formatHumanFloat(total), formatHumanFloat(mean), formatHumanFloat(p95), formatHumanFloat(inBytes), formatHumanFloat(outBytes), formatHumanFloat(cpu), formatHumanFloat(memory), formatHumanFloat(memoryPeak), formatHumanFloat(callWait), ""}
}

// RenderOverviewWebCSV flattens web request/cache aggregates and samples.
func RenderOverviewWebCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "method", "uri", "status", "status_class", "action", "result", "count", "total_duration_micros", "mean_duration_micros", "p95_duration_micros", "request_bytes", "response_bytes", "bytes", "timestamp", "source", "raw"}); err != nil {
		return nil, err
	}
	for _, row := range report.Web.Requests {
		if err := writer.Write([]string{"request", row.Method, row.URI, row.Status, row.StatusClass, "", row.Result, strconv.FormatInt(row.Count, 10), strconv.FormatInt(row.Stats.TotalMicros, 10), formatHumanFloat(row.Stats.MeanMicros), strconv.FormatInt(row.Stats.P95Micros, 10), strconv.FormatInt(row.RequestBytes, 10), strconv.FormatInt(row.ResponseBytes, 10), "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Web.Cache {
		if err := writer.Write([]string{"cache", row.Method, row.URI, row.Status, "", row.Action, row.Result, strconv.FormatInt(row.Count, 10), strconv.FormatInt(row.Stats.TotalMicros, 10), formatHumanFloat(row.Stats.MeanMicros), strconv.FormatInt(row.Stats.P95Micros, 10), "", "", strconv.FormatInt(row.Bytes, 10), "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, sample := range report.Web.SlowSamples {
		if err := writer.Write([]string{"slow_sample", sample.Method, sample.URI, sample.Status, "", "", sample.Result, "", strconv.FormatInt(sample.DurationMicros, 10), "", "", "", "", "", sample.Timestamp.Format(time.RFC3339Nano), sample.Source, sample.RequestRaw + "\n" + sample.ResponseRaw}); err != nil {
			return nil, err
		}
	}
	for _, sample := range report.Web.ErrorSamples {
		if err := writer.Write([]string{"error_sample", sample.Method, sample.URI, sample.Status, "", "", sample.Result, "", strconv.FormatInt(sample.DurationMicros, 10), "", "", "", "", "", sample.Timestamp.Format(time.RFC3339Nano), sample.Source, sample.RequestRaw + "\n" + sample.ResponseRaw}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewSessionsCSV flattens session aggregates, retained lifecycles,
// incomplete records, and the activity timeline.
func RenderOverviewSessionsCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "event_type", "id", "timestamp", "finished_at", "duration_micros", "count", "active", "action", "reason", "user", "application", "computer", "database", "process", "source", "confidence"}); err != nil {
		return nil, err
	}
	for _, row := range report.Sessions.ByEvent {
		if err := writer.Write([]string{"event", row.Key, "", "", "", "", strconv.FormatInt(row.Stats.TotalMicros, 10), strconv.FormatInt(row.Stats.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.ByUser {
		if err := writer.Write([]string{"user", row.Key, "", "", "", "", "", strconv.FormatInt(row.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.ByApplication {
		if err := writer.Write([]string{"application", row.Key, "", "", "", "", "", strconv.FormatInt(row.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.ByComputer {
		if err := writer.Write([]string{"computer", row.Key, "", "", "", "", "", strconv.FormatInt(row.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.ByDatabase {
		if err := writer.Write([]string{"database", row.Key, "", "", "", "", "", strconv.FormatInt(row.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.ByProcess {
		if err := writer.Write([]string{"process", row.Key, "", "", "", "", "", strconv.FormatInt(row.Count, 10), "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.Sessions {
		if err := writer.Write([]string{"session", "", row.EventType, row.ID, row.StartedAt.Format(time.RFC3339Nano), row.FinishedAt.Format(time.RFC3339Nano), strconv.FormatInt(row.DurationMicros, 10), "", "", row.StartAction + " -> " + row.FinishAction, "", row.User, row.Application, row.Computer, row.Database, row.Process, row.Source, row.Confidence}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.Unclosed {
		if err := writer.Write([]string{"unclosed", "", row.EventType, row.ID, row.Timestamp.Format(time.RFC3339Nano), "", "", "", "", row.Action, row.Reason, row.User, row.Application, row.Computer, row.Database, row.Process, row.Source, ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.OrphanFinishes {
		if err := writer.Write([]string{"orphan_finish", "", row.EventType, row.ID, row.Timestamp.Format(time.RFC3339Nano), "", "", "", "", row.Action, row.Reason, row.User, row.Application, row.Computer, row.Database, row.Process, row.Source, ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Sessions.Timeline {
		if err := writer.Write([]string{"timeline", "", "", "", row.Timestamp.Format(time.RFC3339Nano), "", "", "", strconv.Itoa(row.Active), "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewProcessesCSV flattens PROC and SCOM metrics, relations, and samples.
func RenderOverviewProcessesCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "operation", "process", "os_thread", "source", "source_process", "destination_process", "count", "total_duration_micros", "mean_duration_micros", "p95_duration_micros", "timestamp", "raw"}); err != nil {
		return nil, err
	}
	writeAggregate := func(kind string, key string, count int64, total, mean, p95 float64) error {
		return writer.Write([]string{kind, key, "", "", "", "", "", "", strconv.FormatInt(count, 10), formatHumanFloat(total), formatHumanFloat(mean), formatHumanFloat(p95), "", ""})
	}
	for _, row := range report.ProcessStats.PROCByProcess {
		if err := writeAggregate("proc_process", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.PROCByOSThread {
		if err := writeAggregate("proc_os_thread", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.PROCBySource {
		if err := writeAggregate("proc_source", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.SCOMByOperation {
		if err := writeAggregate("scom_operation", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.SCOMByProcess {
		if err := writeAggregate("scom_process", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.SCOMBySource {
		if err := writeAggregate("scom_source", row.Key, row.Metrics.Occurrences, row.Metrics.EventDuration.Sum, row.Metrics.EventDuration.Mean, row.Metrics.EventDuration.P95); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.SCOMByOperationProcessSource {
		if err := writer.Write([]string{"scom_composite", "", row.Operation, row.Process, "", row.Source, "", "", strconv.FormatInt(row.Metrics.Occurrences, 10), formatHumanFloat(row.Metrics.EventDuration.Sum), formatHumanFloat(row.Metrics.EventDuration.Mean), formatHumanFloat(row.Metrics.EventDuration.P95), "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.ExplicitProcessRelations {
		if err := writer.Write([]string{"relation", "", row.Operation, row.Process, "", row.LogSource, row.SourceProcessName, row.DestinationProcessName, strconv.FormatInt(row.Occurrences, 10), "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ProcessStats.PROCSLowSamples {
		if err := writer.Write([]string{"proc_sample", "", "", row.Process, row.OSThread, row.Source, "", "", "", strconv.FormatInt(row.EventDuration, 10), "", "", row.Timestamp.Format(time.RFC3339Nano), row.Raw}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewLicensesCSV writes license, HASP, safe system-summary, and
// retained sample rows to a single spreadsheet-friendly file.
func RenderOverviewLicensesCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "func", "result", "origin", "process", "user", "count", "total_duration_micros", "mean_duration_micros", "p95_duration_micros", "acquire", "release", "success", "failure", "expired", "wrong_type", "timestamp", "source", "event", "classification", "text"}); err != nil {
		return nil, err
	}
	for _, row := range report.Licenses.Licenses {
		if err := writer.Write([]string{"license", "", row.Func, row.Result, "", row.Process, row.User, strconv.FormatInt(row.Count, 10), strconv.FormatInt(row.Stats.TotalMicros, 10), formatHumanFloat(row.Stats.MeanMicros), strconv.FormatInt(row.Stats.P95Micros, 10), strconv.FormatInt(row.Acquire, 10), strconv.FormatInt(row.Release, 10), strconv.FormatInt(row.Success, 10), strconv.FormatInt(row.Failure, 10), strconv.FormatInt(row.Expired, 10), strconv.FormatInt(row.WrongType, 10), "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Licenses.HASP {
		if err := writer.Write([]string{"hasp", "", "", "", row.Origin, row.Process, row.User, strconv.FormatInt(row.Count, 10), strconv.FormatInt(row.Stats.TotalMicros, 10), formatHumanFloat(row.Stats.MeanMicros), strconv.FormatInt(row.Stats.P95Micros, 10), "", "", "", "", "", "", "", "", "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, group := range []struct {
		kind   string
		values map[string]int64
	}{{"system_os_family", report.Licenses.Systems.OSFamilies}, {"system_type", report.Licenses.Systems.SystemTypes}, {"system_memory", report.Licenses.Systems.MemoryBuckets}} {
		keys := make([]string, 0, len(group.values))
		for key := range group.values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := writer.Write([]string{group.kind, key, "", "", "", "", "", strconv.FormatInt(group.values[key], 10), "", "", "", "", "", "", "", "", "", "", "", "", "", ""}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range report.Licenses.SlowSamples {
		if err := writer.Write([]string{"slow_sample", "", row.Func, row.Result, "", row.Process, row.User, "", strconv.FormatInt(row.DurationMicros, 10), "", "", "", "", "", "", "", "", row.Timestamp.Format(time.RFC3339Nano), row.Source, row.Event, row.Classification, row.Text}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.Licenses.ErrorSamples {
		if err := writer.Write([]string{"error_sample", "", row.Func, row.Result, "", row.Process, row.User, "", strconv.FormatInt(row.DurationMicros, 10), "", "", "", "", "", "", "", "", "", row.Timestamp.Format(time.RFC3339Nano), row.Source, row.Event, row.Classification, row.Text}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewFileDBCSV flattens DBV8DBEng aggregates and bounded safe
// samples. File names have already been normalized by filedbstats.
func RenderOverviewFileDBCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "key", "func", "class", "table_name", "category", "file_name", "trans", "database", "process", "user", "count", "total_duration_micros", "mean_duration_micros", "p95_duration_micros", "rows_count", "rows_sum", "rows_affected_count", "rows_affected_sum", "timestamp", "duration_micros", "error_field"}); err != nil {
		return nil, err
	}
	write := func(kind, key string, count, total, p95, rowsCount, rowsSum, affectedCount, affectedSum int64, mean float64) error {
		return writer.Write([]string{kind, key, "", "", "", "", "", "", "", "", "", strconv.FormatInt(count, 10), strconv.FormatInt(total, 10), formatHumanFloat(mean), strconv.FormatInt(p95, 10), strconv.FormatInt(rowsCount, 10), strconv.FormatInt(rowsSum, 10), strconv.FormatInt(affectedCount, 10), strconv.FormatInt(affectedSum, 10), "", "", ""})
	}
	for _, row := range report.FileDB.ByFunc {
		if err := write("func", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByTable {
		if err := write("table", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByCategory {
		if err := write("category", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByFile {
		if err := write("file", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByDatabase {
		if err := write("database", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByProcess {
		if err := write("process", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByUser {
		if err := write("user", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ByClass {
		if err := write("class", row.Key, row.Count, row.Duration.TotalMicros, row.Duration.P95Micros, row.Rows.Rows.Count, row.Rows.Rows.Sum, row.Rows.RowsAffected.Count, row.Rows.RowsAffected.Sum, row.Duration.MeanMicros); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.SlowSamples {
		rows, affected := "", ""
		if row.HasRows {
			rows = strconv.FormatInt(row.Rows, 10)
		}
		if row.HasRowsAffected {
			affected = strconv.FormatInt(row.RowsAffected, 10)
		}
		if err := writer.Write([]string{"slow_sample", "", row.Func, string(row.Class), row.TableName, row.Category, row.FileName, row.Trans, row.Database, row.Process, row.User, "", "", "", "", "", rows, "", affected, row.Timestamp.Format(time.RFC3339Nano), strconv.FormatInt(row.DurationMicros, 10), row.ErrorField}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.FileDB.ErrorSamples {
		rows, affected := "", ""
		if row.HasRows {
			rows = strconv.FormatInt(row.Rows, 10)
		}
		if row.HasRowsAffected {
			affected = strconv.FormatInt(row.RowsAffected, 10)
		}
		if err := writer.Write([]string{"error_sample", "", row.Func, string(row.Class), row.TableName, row.Category, row.FileName, row.Trans, row.Database, row.Process, row.User, "", "", "", "", "", rows, "", affected, row.Timestamp.Format(time.RFC3339Nano), strconv.FormatInt(row.DurationMicros, 10), row.ErrorField}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

// RenderOverviewErrorContextsCSV flattens normalized error groups, retained
// enriched errors, and uncorrelated EXCPCNTX samples.
func RenderOverviewErrorContextsCSV(report overview.OverviewResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"kind", "signature", "event_type", "timestamp", "exception", "description", "module", "source", "process", "os_thread", "user", "database", "count", "reason", "context_json", "raw"}); err != nil {
		return nil, err
	}
	for _, row := range report.ErrorContext.Groups {
		if err := writer.Write([]string{"group", row.Signature, "", "", row.Exception, row.Description, row.Module, row.Source, row.Process, "", row.User, row.Database, strconv.FormatInt(row.Count, 10), "", "", ""}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ErrorContext.Errors {
		context, err := json.Marshal(row.Context)
		if err != nil {
			return nil, err
		}
		if err := writer.Write([]string{"error", "", row.EventType, row.Timestamp.Format(time.RFC3339Nano), row.Exception, row.Description, row.Module, row.Source, row.Process, row.OSThread, row.User, row.Database, "", "", string(context), row.Raw}); err != nil {
			return nil, err
		}
	}
	for _, row := range report.ErrorContext.Orphans {
		fields, err := json.Marshal(row.Fields)
		if err != nil {
			return nil, err
		}
		if err := writer.Write([]string{"orphan_context", "", "", row.Timestamp.Format(time.RFC3339Nano), "", "", "", row.Source, row.Process, row.OSThread, "", "", "", row.Reason, string(fields), ""}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

func RenderRawContextsCSV(report model.RawContextReport) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write([]string{
		"date",
		"hour",
		"timestamp",
		"event",
		"file",
		"duration_micros",
		"duration_ms",
		"context",
		"short_context",
	}); err != nil {
		return nil, err
	}

	for _, day := range report.Days {
		for _, hour := range day.Hours {
			for _, event := range hour.Events {
				record := []string{
					day.Date,
					hour.Hour.Format("15:00"),
					event.Timestamp.Format("2006-01-02 15:04:05.000000"),
					event.Event,
					event.File,
					strconv.FormatInt(event.DurationMicros, 10),
					formatHumanFloat(event.DurationMS),
					event.Context,
					event.ShortContext,
				}
				if err := writer.Write(record); err != nil {
					return nil, err
				}
			}
		}
	}

	writer.Flush()
	return withUTF8BOM(buf.Bytes()), writer.Error()
}

func RenderRawErrorsCSV(report model.RawErrorReport) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write([]string{
		"date",
		"hour",
		"timestamp",
		"event",
		"file",
		"duration_micros",
		"duration_ms",
		"description",
		"short_description",
	}); err != nil {
		return nil, err
	}

	for _, day := range report.Days {
		for _, hour := range day.Hours {
			for _, event := range hour.Events {
				record := []string{
					day.Date,
					hour.Hour.Format("15:00"),
					event.Timestamp.Format("2006-01-02 15:04:05.000000"),
					event.Event,
					event.File,
					strconv.FormatInt(event.DurationMicros, 10),
					formatHumanFloat(event.DurationMS),
					event.Description,
					event.ShortDescription,
				}
				if err := writer.Write(record); err != nil {
					return nil, err
				}
			}
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
