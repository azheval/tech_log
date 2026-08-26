package scallstats

import (
	"math"
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorAggregatesSCALLDimensionsAndMetrics(t *testing.T) {
	collector := NewCollector(Options{SampleLimit: 2})
	hour := time.Date(2026, 3, 23, 6, 0, 0, 0, time.UTC)
	input := "00:00.000000-10,SCALL,1,process=rphost,Usr=alice,DataBase=main,Context='Document.Write',Interface=iface,IName=IObject,Method=4,InBytes=100,OutBytes=200,CpuTime=2,Memory=50,MemoryPeak=75,CallWait=1\n" +
		"00:00.000001-20,SCALL,1,process=rphost,Usr=alice,DataBase=main,Context='Document\n.Write',Interface=iface,IName=IObject,Method=4,InBytes=300,OutBytes=400,CpuTime=4,Memory=70,MemoryPeak=90,CallWait=3\n" +
		"00:00.000002-40,SCALL,1,process=rmngr,Usr=bob,DataBase=main,Context=Read,Interface=iface,IName=IObject,Method=5,InBytes=500,OutBytes=600,CpuTime=6,Memory=80,MemoryPeak=120,CallWait=5\n" +
		"00:00.000003-100,SCALL,1,process=rmngr,Usr=bob,DataBase=main,Context=Read,Interface=other,IName=IOther,Method=1,InBytes=700,OutBytes=800,CpuTime=8,Memory=100,MemoryPeak=150,CallWait=7\n"
	_, err := techlog.Parse(strings.NewReader(input), "26032306.log", hour, func(event techlog.Event) error { collector.Consume(event); return nil })
	if err != nil {
		t.Fatal(err)
	}
	got := collector.Result()
	if got.Quality.CallEvents != 4 || len(got.ByCall) != 3 {
		t.Fatalf("result = %+v", got)
	}
	row := got.ByCall[2] // iface/IObject/4 has total 30; other is first.
	if row.Interface != "iface" || row.Method != "4" || row.Metrics.Duration.Count != 2 || row.Metrics.Duration.Sum != 30 || row.Metrics.Duration.P50 != 15 || row.Metrics.InBytes.Sum != 400 || row.Metrics.MemoryPeak.Max != 90 {
		t.Fatalf("combined row = %+v", row)
	}
	if len(got.ByContext) != 3 || got.ByContext[0].Key != "Read" || got.ByContext[0].Metrics.Duration.Sum != 140 {
		t.Fatalf("context rows = %+v", got.ByContext)
	}
	if len(got.SlowSamples) != 2 || got.SlowSamples[0].DurationMicros != 100 || got.SlowSamples[1].DurationMicros != 40 {
		t.Fatalf("samples = %+v", got.SlowSamples)
	}
}

func TestCollectorReportsMissingAndMalformedFields(t *testing.T) {
	collector := NewCollector(Options{})
	collector.Consume(techlog.Event{Name: "SDBL"})
	collector.Consume(techlog.Event{Name: "SCALL", DurationMicros: 2, Fields: map[string]string{"Interface": " i ", "IName": "", "Method": "  ", "InBytes": "bad", "OutBytes": "-5", "CpuTime": "3.5", "Memory": "", "MemoryPeak": "NaN", "CallWait": "7"}})
	got := collector.Result()
	if got.Quality.IgnoredEvents != 1 || got.Quality.CallEvents != 1 {
		t.Fatalf("quality = %+v", got.Quality)
	}
	if got.Quality.IName.Missing != 1 || got.Quality.Method.Missing != 1 || got.Quality.Context.Missing != 1 || got.Quality.User.Missing != 1 || got.Quality.Database.Missing != 1 || got.Quality.Process.Missing != 1 {
		t.Fatalf("dimension quality = %+v", got.Quality)
	}
	if got.Quality.InBytes.Malformed != 1 || got.Quality.OutBytes.Malformed != 1 || got.Quality.Memory.Missing != 1 || got.Quality.MemoryPeak.Malformed != 1 {
		t.Fatalf("numeric quality = %+v", got.Quality)
	}
	row := got.ByCall[0]
	if row.Interface != "i" || row.IName != missingValue || row.Metrics.CPUTime.Sum != 3.5 || row.Metrics.CallWait.Sum != 7 || !math.IsNaN(row.Metrics.InBytes.Mean) {
		t.Fatalf("row = %+v", row)
	}
}

func TestCollectorSlowSamplesHaveDeterministicTieBreakers(t *testing.T) {
	collector := NewCollector(Options{SampleLimit: 2})
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, event := range []techlog.Event{
		{Name: "SCALL", DurationMicros: 5, Timestamp: stamp, Source: "z", Fields: requiredFields("z")},
		{Name: "SCALL", DurationMicros: 9, Timestamp: stamp, Source: "b", Fields: requiredFields("b")},
		{Name: "SCALL", DurationMicros: 9, Timestamp: stamp, Source: "a", Fields: requiredFields("a")},
	} {
		collector.Consume(event)
	}
	samples := collector.Result().SlowSamples
	if len(samples) != 2 || samples[0].Source != "a" || samples[1].Source != "b" {
		t.Fatalf("samples = %+v", samples)
	}
}

func requiredFields(method string) map[string]string {
	return map[string]string{"Interface": "i", "IName": "n", "Method": method, "Context": "c", "Usr": "u", "DataBase": "d", "process": "p"}
}
