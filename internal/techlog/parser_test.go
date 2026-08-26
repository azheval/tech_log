package techlog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseSingleAndMultilineEvents(t *testing.T) {
	input := "\ufeff05:59.806001-17,SDBL,1,process=1CV8C,OSThread=4356,Sql='select a, b',Context='Module.Call'\r\n" +
		"05:59.806002-0,EXCP,2,process=1CV8C,Descr='first line\r\nsecond,line'\r\n"
	hour := time.Date(2026, 3, 23, 6, 0, 0, 0, time.Local)
	var events []Event
	stats, err := Parse(strings.NewReader(input), "26032306.log", hour, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Events != 2 || stats.MalformedHeaders != 0 || len(events) != 2 {
		t.Fatalf("unexpected parse result: stats=%+v events=%d", stats, len(events))
	}
	if got := events[0].Fields["Sql"]; got != "select a, b" {
		t.Fatalf("Sql = %q", got)
	}
	if got := events[1].Fields["Descr"]; got != "first line\nsecond,line" {
		t.Fatalf("Descr = %q", got)
	}
	if events[0].Timestamp.Minute() != 5 || events[0].Timestamp.Second() != 59 || events[0].Timestamp.Nanosecond() != 806001000 {
		t.Fatalf("timestamp = %s", events[0].Timestamp)
	}
}

func TestParseReportsOrphansAndMalformedHeader(t *testing.T) {
	input := "orphan\n05:00.000000-nope,SDBL,1,Context=x\n05:00.000001-1,SDBL,1,Context=y\n"
	stats, err := Parse(strings.NewReader(input), "source", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OrphanLines != 1 || stats.MalformedHeaders != 1 || stats.Events != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestParseContinuationCannotAlterHeaderStructure(t *testing.T) {
	input := "00:00.000000-1,EXCP,2\ncontinuation,with comma\n"
	var events []Event
	stats, err := Parse(strings.NewReader(input), "source", time.Now(), func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Events != 1 || stats.MalformedHeaders != 0 || len(events) != 1 {
		t.Fatalf("stats=%+v events=%+v", stats, events)
	}
	if events[0].Name != "EXCP" || events[0].Level != 2 || !strings.Contains(events[0].Raw, "continuation,with comma") {
		t.Fatalf("event=%+v", events[0])
	}
}

func TestParseStopsOnCallbackError(t *testing.T) {
	want := errors.New("stop")
	_, err := Parse(strings.NewReader("00:00.000000-1,SDBL,1,Context=x\n"), "source", time.Now(), func(Event) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestParseContextStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stats, err := ParseContext(ctx, strings.NewReader("00:00.000000-1,SDBL,1,Context=x\n00:00.000001-1,SDBL,1,Context=y\n"), "source", time.Now(), func(Event) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if stats.Events != 1 {
		t.Fatalf("events before cancellation = %d, want 1", stats.Events)
	}
}

func TestParseFileHourValidatesCalendarValues(t *testing.T) {
	got, err := ParseFileHour("C:/logs/rphost_1/26032306.log")
	if err != nil {
		t.Fatal(err)
	}
	if got.Format("2006-01-02 15") != "2026-03-23 06" {
		t.Fatalf("hour = %s", got)
	}
	if _, err := ParseFileHour("C:/logs/26133225.log"); err == nil {
		t.Fatal("expected invalid date error")
	}
}
