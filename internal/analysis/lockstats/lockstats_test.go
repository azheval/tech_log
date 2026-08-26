package lockstats

import (
	"reflect"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorParsesMultilineLocksAndRegions(t *testing.T) {
	c := NewCollector(Options{SampleLimit: 2})
	c.Consume(techlog.Event{Name: "TLOCK", DurationMicros: 10, Timestamp: time.Unix(1, 0), Source: "a", Raw: "raw-lock", Fields: map[string]string{"Context": "Posting", "Locks": "Table=Document.Sales; Region=Header\nTable: Catalog.Items, Region: Item-1", "Regions": "Item-1; Item-2"}})
	c.Consume(techlog.Event{Name: "TLOCK", DurationMicros: 30, Timestamp: time.Unix(2, 0), Source: "b", Fields: map[string]string{"Context": "Posting", "Locks": "Table=Document.Sales,Region=Header"}})
	result := c.Result()
	if got := result.ByTable[0]; got.Key != "Document.Sales" || got.Stats.Count != 2 || got.Stats.TotalMicros != 40 || got.Stats.P50Micros != 10 || got.Stats.P95Micros != 30 {
		t.Fatalf("table aggregate=%+v", got)
	}
	if !reflect.DeepEqual(result.ByRegion[0:3], []Aggregate{{Key: "Header", Stats: DurationStats{Count: 2, TotalMicros: 40, MinMicros: 10, MaxMicros: 30, MeanMicros: 20, P50Micros: 10, P95Micros: 30, P99Micros: 30}}, {Key: "Item-1", Stats: DurationStats{Count: 1, TotalMicros: 10, MinMicros: 10, MaxMicros: 10, MeanMicros: 10, P50Micros: 10, P95Micros: 10, P99Micros: 10}}, {Key: "Item-2", Stats: DurationStats{Count: 1, TotalMicros: 10, MinMicros: 10, MaxMicros: 10, MeanMicros: 10, P50Micros: 10, P95Micros: 10, P99Micros: 10}}}) {
		t.Fatalf("regions=%+v", result.ByRegion)
	}
	if len(result.Samples) != 2 || result.Samples[0].DurationMicros != 30 || !reflect.DeepEqual(result.Samples[1].Tables, []string{"Catalog.Items", "Document.Sales"}) {
		t.Fatalf("samples=%+v", result.Samples)
	}
}

func TestCollectorTimeoutDeadlockAndExplicitRelations(t *testing.T) {
	c := NewCollector()
	c.Consume(techlog.Event{Name: "TTIMEOUT", DurationMicros: 50, Fields: map[string]string{"Context": "Timeout", "Regions": "R1"}})
	c.Consume(techlog.Event{Name: "TDEADLOCK", DurationMicros: 100, Fields: map[string]string{"Context": "Deadlock", "Locks": "Table=Doc", "Waiter": "conn-a", "Blocker": "conn-b"}})
	result := c.Result()
	if result.ByEvent[0].Key != "TDEADLOCK" || result.ByEvent[1].Key != "TTIMEOUT" {
		t.Fatalf("events=%+v", result.ByEvent)
	}
	if result.Quality.EventsWithExplicitRelation != 1 || len(result.Relations) != 1 || result.Relations[0].Waiter != "conn-a" {
		t.Fatalf("relations=%+v quality=%+v", result.Relations, result.Quality)
	}
}

func TestCollectorTracksMissingFieldsAndDoesNotInferRelations(t *testing.T) {
	c := NewCollector()
	c.Consume(techlog.Event{Name: "OTHER"})
	c.Consume(techlog.Event{Name: "TTIMEOUT", DurationMicros: 1, Fields: map[string]string{"Locks": "conn-a waits for conn-b"}})
	result := c.Result()
	if result.Quality.IgnoredEvents != 1 || result.Quality.LockEvents != 1 || result.Quality.MissingContext != 1 || result.Quality.MissingLocks != 1 || result.Quality.MissingRegions != 1 {
		t.Fatalf("quality=%+v", result.Quality)
	}
	if len(result.Relations) != 0 {
		t.Fatalf("unexpected inferred relation: %+v", result.Relations)
	}
}
