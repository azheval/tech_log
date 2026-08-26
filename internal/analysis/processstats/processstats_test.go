package processstats

import (
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorPROCAndSCOMWithRealFunctionShapes(t *testing.T) {
	collector := NewCollector(Options{SampleLimit: 2})
	hour := time.Date(2026, 3, 23, 6, 0, 0, 0, time.UTC)
	input := "00:00.000000-20,PROC,0,process=1cv8,OSThread=35280\n" +
		"00:00.000001-50,PROC,0,process=1cv8,OSThread=35280,Finish=success\n" +
		"00:00.000002-2,SCOM,1,process=1cv8,OSThread=35280,Func='new ServerProcessData(20f0fa3ccc0,RHostRoot,RHostRoot)'\n" +
		"00:00.000003-4,SCOM,1,process=1cv8,OSThread=35280,Func='setSrcProcessName(20f0fa3ccc0,RHostRoot,RemoteDebugger)'\n" +
		"00:00.000004-8,SCOM,1,process=1cv8,Func='delete ServerProcessData(20f0fa3ccc0,RHostRoot,RHostRoot)'\n"
	_, err := techlog.Parse(strings.NewReader(input), "26032306.log", hour, func(event techlog.Event) error { collector.Consume(event); return nil })
	if err != nil {
		t.Fatal(err)
	}
	got := collector.Result()
	if got.Quality.PROCEvents != 2 || got.Quality.SCOMEvents != 3 || got.PROCByProcess[0].Metrics.Occurrences != 2 || got.PROCByProcess[0].Metrics.EventDuration.Sum != 70 {
		t.Fatalf("result = %+v", got)
	}
	if len(got.PROCSLowSamples) != 2 || got.PROCSLowSamples[0].EventDuration != 50 || got.PROCSLowSamples[1].EventDuration != 20 {
		t.Fatalf("samples = %+v", got.PROCSLowSamples)
	}
	if got.SCOMByOperation[0].Key != "delete ServerProcessData" || got.SCOMByOperation[0].Metrics.EventDuration.Sum != 8 {
		t.Fatalf("operations = %+v", got.SCOMByOperation)
	}
	if len(got.ExplicitProcessRelations) != 2 || got.ExplicitProcessRelations[0].Operation != "new ServerProcessData" || got.ExplicitProcessRelations[1].DestinationProcessName != "RemoteDebugger" {
		t.Fatalf("relations = %+v", got.ExplicitProcessRelations)
	}
}

func TestSCOMMultilineAndUnknownOrMalformedFuncQuality(t *testing.T) {
	collector := NewCollector(Options{})
	hour := time.Date(2026, 3, 23, 6, 0, 0, 0, time.UTC)
	input := "00:00.000000-1,SCOM,1,process=p,Func='customCall(alpha,\nbeta)'\n" +
		"00:00.000001-1,SCOM,1,process=p,Func='bad(func())'\n" +
		"00:00.000002-1,SCOM,1,process=p\n" +
		"00:00.000003-1,SCOM,1,process=p,Func='new ServerProcessData(id,,)'\n"
	_, err := techlog.Parse(strings.NewReader(input), "26032306.log", hour, func(event techlog.Event) error { collector.Consume(event); return nil })
	if err != nil {
		t.Fatal(err)
	}
	got := collector.Result()
	if got.Quality.UnknownFunc != 1 || got.Quality.Func.Malformed != 1 || got.Quality.Func.Missing != 1 || got.Quality.KnownFuncMissingNames != 1 {
		t.Fatalf("quality = %+v", got.Quality)
	}
	if len(got.SCOMByOperation) != 2 || got.SCOMByOperation[0].Key != "customCall" {
		t.Fatalf("operations = %+v", got.SCOMByOperation)
	}
}

func TestParseFunctionRejectsAmbiguousSyntax(t *testing.T) {
	function, ok := ParseFunction("setSrcProcessName(id, src, dst)")
	if !ok || function.Operation != "setSrcProcessName" || len(function.Arguments) != 3 || function.Arguments[1] != "src" {
		t.Fatalf("function = %+v, ok=%v", function, ok)
	}
	for _, value := range []string{"unknown", "f(a(b))", "f('quoted')", "(a)"} {
		if _, ok := ParseFunction(value); ok {
			t.Fatalf("ParseFunction(%q) accepted ambiguous input", value)
		}
	}
}
