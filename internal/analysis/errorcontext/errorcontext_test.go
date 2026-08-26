package errorcontext

import (
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func logEvent(name string, at time.Time, fields map[string]string) techlog.Event {
	return techlog.Event{Name: name, Timestamp: at, Fields: fields, Source: "server.log", Raw: name + " raw"}
}
func baseTime() time.Time { return time.Date(2026, 3, 1, 2, 3, 4, 0, time.UTC) }

func TestEXCPThenContextEnrichesError(t *testing.T) {
	c := NewCollector(Options{})
	base := baseTime()
	c.Consume(logEvent("EXCP", base, map[string]string{"Exception": "DatabaseException", "Descr": "failed 10.1.2.3", "process": "rphost", "OSThread": "10", "Usr": "alice"}))
	c.Consume(logEvent("EXCPCNTX", base.Add(time.Microsecond), map[string]string{"SrcName": "Module.Form", "process": "rphost", "OSThread": "10", "DataBase": "db"}))
	r := c.Result()
	if r.Quality.MatchedContexts != 1 || len(r.Errors) != 1 {
		t.Fatalf("result = %+v", r)
	}
	error := r.Errors[0]
	if error.Module != "Module.Form" || error.Database != "db" || len(error.Context) != 4 {
		t.Fatalf("error not enriched: %+v", error)
	}
	if r.Groups[0].Description != "failed <ip>" {
		t.Fatalf("normalization = %q", r.Groups[0].Description)
	}
}

func TestContextBeforeAndAfterError(t *testing.T) {
	c := NewCollector(Options{})
	base := baseTime()
	c.Consume(logEvent("EXCPCNTX", base, map[string]string{"Context": "request-1", "process": "rphost", "OSThread": "9"}))
	c.Consume(logEvent("QERR", base.Add(time.Microsecond), map[string]string{"Exception": "QueryError", "Descr": "bad", "process": "rphost", "OSThread": "9"}))
	c.Consume(logEvent("EXCPCNTX", base.Add(2*time.Microsecond), map[string]string{"SrcName": "PROC", "process": "rphost", "OSThread": "9"}))
	r := c.Result()
	if r.Quality.MatchedContexts != 2 || len(r.Errors) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if r.Errors[0].Module != "PROC" || len(r.Errors[0].Context) != 4 {
		t.Fatalf("contexts not merged: %+v", r.Errors[0])
	}
}

func TestTwoThreadsNeverCrossMatch(t *testing.T) {
	c := NewCollector(Options{})
	base := baseTime()
	c.Consume(logEvent("EXCP", base, map[string]string{"Exception": "one", "process": "rphost", "OSThread": "1"}))
	c.Consume(logEvent("EXCP", base.Add(time.Microsecond), map[string]string{"Exception": "two", "process": "rphost", "OSThread": "2"}))
	c.Consume(logEvent("EXCPCNTX", base.Add(2*time.Microsecond), map[string]string{"SrcName": "thread-two", "process": "rphost", "OSThread": "2"}))
	r := c.Result()
	if len(r.Errors) != 2 || r.Quality.MatchedContexts != 1 {
		t.Fatalf("result = %+v", r)
	}
	for _, error := range r.Errors {
		if error.Exception == "one" && error.Module != "" {
			t.Fatalf("context crossed threads: %+v", error)
		}
	}
}

func TestAmbiguityIsNotInvented(t *testing.T) {
	c := NewCollector(Options{})
	base := baseTime()
	fields := map[string]string{"Exception": "same", "process": "rphost", "OSThread": "1"}
	c.Consume(logEvent("EXCP", base, fields))
	c.Consume(logEvent("QERR", base, fields))
	c.Consume(logEvent("EXCPCNTX", base, map[string]string{"SrcName": "PROC", "process": "rphost", "OSThread": "1"}))
	r := c.Result()
	if r.Quality.AmbiguousContexts != 1 || len(r.Orphans) != 1 {
		t.Fatalf("result = %+v", r)
	}
	for _, error := range r.Errors {
		if error.Module != "" {
			t.Fatalf("ambiguous context was attached: %+v", error)
		}
	}
}

func TestOrphanAndRealSrcNamePROC(t *testing.T) {
	base := baseTime()
	c := NewCollector(Options{})
	c.Consume(logEvent("EXCPCNTX", base, map[string]string{"SrcName": "orphan", "process": "rphost", "OSThread": "1"}))
	c.Consume(logEvent("EXCP", base.Add(2*time.Minute), map[string]string{"Exception": "DatabaseException8", "Descr": "missing file", "process": "1cv8", "OSThread": "35280"}))
	c.Consume(logEvent("EXCPCNTX", base.Add(2*time.Minute+time.Microsecond), map[string]string{"SrcName": "PROC", "process": "1cv8", "OSThread": "35280"}))
	r := c.Result()
	if r.Quality.OrphanContexts != 1 || len(r.Orphans) != 1 {
		t.Fatalf("orphans = %+v, quality = %+v", r.Orphans, r.Quality)
	}
	if len(r.Errors) != 1 || r.Errors[0].Module != "PROC" {
		t.Fatalf("SrcName=PROC was not preserved: %+v", r.Errors)
	}
}
