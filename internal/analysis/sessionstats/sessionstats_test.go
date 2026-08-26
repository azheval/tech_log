package sessionstats

import (
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func event(name string, at time.Time, duration int64, fields map[string]string) techlog.Event {
	return techlog.Event{Name: name, Timestamp: at, DurationMicros: duration, Fields: fields, Source: "test.log"}
}

func TestInterleavedConnectionsBuildsTimelineAndPairedDurations(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewCollector(Options{})
	c.Consume(event("CONN", base, 99_000, map[string]string{"ClientID": "one", "Txt": "Connected, client=a", "Usr": "alice"}))
	c.Consume(event("CONN", base.Add(time.Microsecond), 0, map[string]string{"ClientID": "two", "Txt": "Connected, client=b", "Usr": "bob"}))
	c.Consume(event("CONN", base.Add(4*time.Microsecond), 0, map[string]string{"ClientID": "one", "Txt": "Connection closed"}))
	c.Consume(event("CONN", base.Add(10*time.Microsecond), 0, map[string]string{"ClientID": "two", "Txt": "Disconnected"}))

	r := c.Result()
	if r.Peak != 2 || len(r.Timeline) != 4 {
		t.Fatalf("peak/timeline = %d/%d, want 2/4", r.Peak, len(r.Timeline))
	}
	if r.Duration.Count != 2 || r.Duration.TotalMicros != 13 {
		t.Fatalf("duration = %+v, want two timestamp-paired sessions totaling 13us", r.Duration)
	}
	if r.Sessions[0].DurationSource != "timestamp_pair" || r.Sessions[0].Confidence != "high" {
		t.Fatalf("unexpected source/confidence: %+v", r.Sessions[0])
	}
	if r.Sessions[0].DurationMicros != 9 {
		t.Fatalf("slowest duration = %d, want 9; event duration must not be used", r.Sessions[0].DurationMicros)
	}
}

func TestMissingCloseAndOrphanFinishAreReported(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewCollector(Options{})
	c.Consume(event("SESN", base, 0, map[string]string{"ID": "open", "Func": "Start", "Usr": "alice", "IB": "db"}))
	c.Consume(event("SESN", base.Add(time.Second), 0, map[string]string{"ID": "orphan", "Func": "Finish"}))
	r := c.Result()
	if r.Quality.OpenSessions != 1 || len(r.Unclosed) != 1 || r.Unclosed[0].ID != "open" {
		t.Fatalf("unclosed = %+v, quality = %+v", r.Unclosed, r.Quality)
	}
	if r.Quality.UnmatchedFinishes != 1 || len(r.OrphanFinishes) != 1 || r.OrphanFinishes[0].ID != "orphan" {
		t.Fatalf("orphans = %+v, quality = %+v", r.OrphanFinishes, r.Quality)
	}
	// Result must be a snapshot, not mutate retained samples.
	if got := len(c.Result().Unclosed); got != 1 {
		t.Fatalf("second Result has %d unclosed records, want 1", got)
	}
}

func TestConnClosedTextPairsWithClientID(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewCollector(Options{})
	c.Consume(event("CONN", base, 0, map[string]string{"t:clientID": "7", "Txt": "Connected, client=a", "t:applicationName": "web", "t:computerName": "host"}))
	c.Consume(event("CONN", base.Add(2*time.Millisecond), 0, map[string]string{"t:clientID": "7", "Txt": "Connection closed"}))
	r := c.Result()
	if len(r.Sessions) != 1 {
		t.Fatalf("sessions = %#v", r.Sessions)
	}
	s := r.Sessions[0]
	if s.ID != "7" || s.DurationMicros != 2000 || s.Application != "web" || s.Computer != "host" {
		t.Fatalf("session = %+v", s)
	}
}

func TestSesnIDAndActionPair(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewCollector(Options{})
	c.Consume(event("SESN", base, 1, map[string]string{"ID": "s-1", "Action": "start", "Appl": "Designer", "process": "rphost"}))
	c.Consume(event("SESN", base.Add(5*time.Millisecond), 999999, map[string]string{"ID": "s-1", "Action": "finish"}))
	r := c.Result()
	if len(r.Sessions) != 1 {
		t.Fatalf("sessions = %#v", r.Sessions)
	}
	s := r.Sessions[0]
	if s.DurationMicros != 5000 || s.StartAction != "start" || s.FinishAction != "finish" || s.Application != "Designer" || s.Process != "rphost" {
		t.Fatalf("session = %+v", s)
	}
}
