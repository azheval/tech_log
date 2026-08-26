package trace

import (
	"reflect"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorCorrelatesInterleavedThreads(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client-a", "call-a", "tx-a"))
	collector.Consume(event(base.Add(time.Second), "CALL", "source-a", "rphost", "2", "client-b", "call-b", "tx-b"))
	collector.Consume(event(base.Add(2*time.Second), "Context", "source-a", "rphost", "2", "client-b", "call-b", "tx-b"))
	collector.Consume(event(base.Add(3*time.Second), "SDBL", "source-a", "rphost", "1", "client-a", "call-a", "tx-a"))
	collector.Consume(event(base.Add(4*time.Second), "DBMSSQL", "source-a", "rphost", "2", "client-b", "call-b", "tx-b"))
	collector.Consume(event(base.Add(5*time.Second), "EXCP", "source-a", "rphost", "1", "client-a", "call-a", "tx-a"))

	result := collector.Result()
	if got, want := len(result.Traces), 2; got != want {
		t.Fatalf("traces = %d, want %d", got, want)
	}
	if got, want := spanNames(result.Traces[0]), []string{"CALL", "SDBL", "EXCP"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first trace = %v, want %v", got, want)
	}
	if got, want := spanNames(result.Traces[1]), []string{"CALL", "Context", "DBMSSQL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second trace = %v, want %v", got, want)
	}
	if result.Quality.CorrelatedEvents != 4 || result.Quality.OrphanEvents != 0 || result.Quality.MissingContextTraces != 1 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorTracksMissingContextAndOrphans(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 2, MaxTraces: 2})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "DBPOSTGRS", "source-a", "rphost", "9", "", "", ""))
	collector.Consume(event(base.Add(time.Second), "QERR", "source-a", "rphost", "9", "", "", ""))
	collector.Consume(event(base.Add(2*time.Second), "CALL", "source-a", "rphost", "1", "", "first", ""))
	collector.Consume(event(base.Add(3*time.Second), "SDBL", "source-a", "rphost", "1", "", "first", ""))

	result := collector.Result()
	if got, want := spanNames(result.Traces[0]), []string{"CALL", "SDBL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	if result.Quality.OrphanEvents != 2 || result.Quality.MissingContextTraces != 1 || result.Quality.Contexts != 0 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorUsesOptionalIdentifiersForSameThread(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call-1", ""))
	collector.Consume(event(base.Add(time.Second), "CALL", "source-a", "rphost", "1", "client", "call-2", ""))
	collector.Consume(event(base.Add(2*time.Second), "Context", "source-a", "rphost", "1", "client", "call-1", ""))
	collector.Consume(event(base.Add(3*time.Second), "Context", "source-a", "rphost", "1", "client", "call-2", ""))

	result := collector.Result()
	if got, want := spanNames(result.Traces[0]), []string{"CALL", "Context"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first trace = %v, want %v", got, want)
	}
	if got, want := spanNames(result.Traces[1]), []string{"CALL", "Context"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second trace = %v, want %v", got, want)
	}
}

func TestCollectorRetentionAndDeterministicOrder(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 1, MaxTraces: 1})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base.Add(2*time.Second), "CALL", "source-b", "rphost", "2", "", "b", ""))
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "", "a", ""))

	result := collector.Result()
	if len(result.Traces) != 2 { // one completed plus one open; each retention limit is independent.
		t.Fatalf("traces = %d, want 2", len(result.Traces))
	}
	if !result.Traces[0].StartedAt.Equal(base) || !result.Traces[1].StartedAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("traces are not chronological: %+v", result.Traces)
	}
	if result.Quality.EvictedOpenTraces != 1 || result.Quality.RetainedCompletedTraces != 1 || result.Quality.RetainedOpenTraces != 1 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorDoesNotDoubleCountCompletedMissingContextTrace(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "", "call", ""))
	// Same identity completes the first incomplete trace and opens another.
	collector.Consume(event(base.Add(time.Second), "CALL", "source-a", "rphost", "1", "", "call", ""))

	result := collector.Result()
	if result.Quality.MissingContextTraces != 2 {
		t.Fatalf("MissingContextTraces = %d, want 2", result.Quality.MissingContextTraces)
	}
}

func TestCollectorCorrelatesSCALLAndReliableVRSEvents(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call-1", "tx"))
	collector.Consume(event(base.Add(time.Second), "SCALL", "source-a", "rphost", "1", "client", "call-1", "tx"))
	// A single complete lane is sufficient when a VRS event has no trace ID.
	collector.Consume(event(base.Add(2*time.Second), "VRSREQUEST", "source-a", "rphost", "1", "", "", ""))
	collector.Consume(event(base.Add(3*time.Second), "VRSRESPONSE", "source-a", "rphost", "1", "client", "call-1", "tx"))

	result := collector.Result()
	if got, want := spanNames(result.Traces[0]), []string{"CALL", "SCALL", "VRSREQUEST", "VRSRESPONSE"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	if result.Quality.ServerCalls != 1 || result.Quality.CorrelatedVRS != 2 || result.Quality.OrphanEvents != 0 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorLeavesAmbiguousVRSLaneUncorrelated(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call-1", ""))
	collector.Consume(event(base.Add(time.Second), "CALL", "source-a", "rphost", "1", "client", "call-2", ""))
	// No VRS request/response pairing is inferred from unknown VRS fields. With
	// two otherwise compatible calls, an event without a trace ID is ambiguous.
	collector.Consume(event(base.Add(2*time.Second), "VRSREQUEST", "source-a", "rphost", "1", "", "", ""))

	result := collector.Result()
	if got, want := spanNames(result.Traces[0]), []string{"CALL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first trace = %v, want %v", got, want)
	}
	if got, want := spanNames(result.Traces[1]), []string{"CALL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second trace = %v, want %v", got, want)
	}
	if result.Quality.AmbiguousVRS != 1 || result.Quality.OrphanEvents != 1 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorEnrichesNearestCompatibleErrorWithEXCPCNTX(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call", "tx"))
	collector.Consume(event(base.Add(10*time.Second), "EXCP", "source-a", "rphost", "1", "client", "call", "tx"))
	context := event(base.Add(12*time.Second), "EXCPCNTX", "source-a", "rphost", "1", "client", "call", "tx")
	context.Fields["Module"] = "Sales.Document"
	context.Fields["Context"] = "Posting"
	collector.Consume(context)
	// Advance beyond the window so the pending context is resolved before the
	// final snapshot, including errors written before their context.
	collector.Consume(event(base.Add(2*time.Minute), "OTHER", "source-a", "rphost", "1", "", "", ""))

	result := collector.Result()
	trace := result.Traces[0]
	if got, want := spanNames(trace), []string{"CALL", "EXCP", "EXCPCNTX"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	if trace.Spans[1].Fields["Module"] != "Sales.Document" || trace.Spans[1].Fields["Context"] != "Posting" {
		t.Fatalf("EXCP was not enriched: %+v", trace.Spans[1].Fields)
	}
	if result.Quality.CorrelatedErrorContexts != 1 || result.Quality.OrphanEvents != 0 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorEnrichesErrorWhenEXCPCNTXPrecedesIt(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call", "tx"))
	context := event(base.Add(5*time.Second), "EXCPCNTX", "source-a", "rphost", "1", "client", "call", "tx")
	context.Fields["Module"] = "Inventory"
	collector.Consume(context)
	collector.Consume(event(base.Add(10*time.Second), "QERR", "source-a", "rphost", "1", "client", "call", "tx"))

	result := collector.Result()
	trace := result.Traces[0]
	if got, want := spanNames(trace), []string{"CALL", "QERR", "EXCPCNTX"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	if trace.Spans[1].Fields["Module"] != "Inventory" || result.Quality.CorrelatedErrorContexts != 1 {
		t.Fatalf("trace/quality = %+v / %+v", trace, result.Quality)
	}
}

func TestCollectorDoesNotAttachAmbiguousEXCPCNTX(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 4, MaxTraces: 4})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "CALL", "source-a", "rphost", "1", "client", "call", "tx"))
	collector.Consume(event(base.Add(5*time.Second), "EXCP", "source-a", "rphost", "1", "client", "call", "tx"))
	collector.Consume(event(base.Add(10*time.Second), "EXCPCNTX", "source-a", "rphost", "1", "client", "call", "tx"))
	collector.Consume(event(base.Add(15*time.Second), "QERR", "source-a", "rphost", "1", "client", "call", "tx"))
	collector.Consume(event(base.Add(2*time.Minute), "OTHER", "source-a", "rphost", "1", "", "", ""))

	result := collector.Result()
	if got, want := spanNames(result.Traces[0]), []string{"CALL", "EXCP", "QERR"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	if result.Quality.AmbiguousErrorContexts != 1 || result.Quality.CorrelatedErrorContexts != 0 || result.Quality.OrphanEvents != 1 {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestCollectorBoundsPendingErrorContexts(t *testing.T) {
	collector := newCollector(t, Options{MaxOpenTraces: 1, MaxTraces: 1})
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	collector.Consume(event(base, "EXCPCNTX", "source-a", "rphost", "1", "", "", ""))
	collector.Consume(event(base.Add(time.Second), "EXCPCNTX", "source-a", "rphost", "1", "", "", ""))
	if got := len(collector.pendingErrorContexts); got != 1 {
		t.Fatalf("pending contexts = %d, want 1", got)
	}
	if collector.quality.DroppedErrorContexts != 1 || collector.quality.OrphanEvents != 1 {
		t.Fatalf("quality = %+v", collector.quality)
	}
}

func TestNewCollectorRejectsNegativeRetention(t *testing.T) {
	if _, err := NewCollector(Options{MaxOpenTraces: -1}); err == nil {
		t.Fatal("negative max open accepted")
	}
	if _, err := NewCollector(Options{MaxTraces: -1}); err == nil {
		t.Fatal("negative max traces accepted")
	}
}

func newCollector(t *testing.T, options Options) *Collector {
	t.Helper()
	collector, err := NewCollector(options)
	if err != nil {
		t.Fatal(err)
	}
	return collector
}

func event(at time.Time, name, source, process, thread, clientID, callID, trans string) techlog.Event {
	fields := map[string]string{"process": process, "OSThread": thread}
	if clientID != "" {
		fields["ClientID"] = clientID
	}
	if callID != "" {
		fields["CallID"] = callID
	}
	if trans != "" {
		fields["Trans"] = trans
	}
	return techlog.Event{Timestamp: at, Name: name, Source: source, DurationMicros: 10, Fields: fields, Raw: name}
}

func spanNames(trace Trace) []string {
	result := make([]string, len(trace.Spans))
	for i, span := range trace.Spans {
		result[i] = span.Event
	}
	return result
}
