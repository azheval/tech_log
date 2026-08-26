package filedbstats

import (
	"reflect"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorGroupsObservedPropertiesAndStatistics(t *testing.T) {
	c := NewCollector(Options{SlowSampleLimit: 2, ErrorSampleLimit: 2})
	for _, event := range []techlog.Event{
		{Name: "DBV8DBEng", DurationMicros: 10, Fields: map[string]string{"Func": "Read", "tableName": "Items", "CatName": "Catalog", "FileName": `C:\\srv\\data\\base.1cd`, "Rows": "0", "RowsAffected": "2", "Trans": "tx-1", "DataBase": "db", "process": "p", "Usr": "u"}},
		{Name: "DBV8DBEng", DurationMicros: 30, Fields: map[string]string{"Func": "Read", "tableName": "Items", "CatName": "Catalog", "FileName": "relative/file.1cd", "Rows": "3", "DataBase": "db", "process": "p", "Usr": "u2"}},
		{Name: "DBV8DBEng", DurationMicros: 20, Fields: map[string]string{"Func": "Write", "tableName": "Other", "RowsAffected": "4"}},
		{Name: "SDBL", DurationMicros: 99},
	} {
		c.Consume(event)
	}
	r := c.Result()
	if r.Quality.EventsConsumed != 4 || r.Quality.DBV8DBEngEvents != 3 || r.Quality.IgnoredEvents != 1 {
		t.Fatalf("quality = %+v", r.Quality)
	}
	want := Aggregate{Key: "Read", Count: 2, Duration: DurationStats{Count: 2, TotalMicros: 40, MinMicros: 10, MaxMicros: 30, MeanMicros: 20, P50Micros: 10, P95Micros: 30, P99Micros: 30}, Rows: RowsStats{Rows: ValueStats{Count: 2, Sum: 3, Min: 0, Max: 3, Mean: 1.5}, RowsAffected: ValueStats{Count: 1, Sum: 2, Min: 2, Max: 2, Mean: 2}}}
	if got := r.ByFunc[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("read aggregate = %+v, want %+v", got, want)
	}
	if len(r.ByFile) != 2 || r.ByFile[0].Key != "relative/file.1cd" || r.ByFile[1].Key != "<absolute-path>/base.1cd" {
		t.Fatalf("files disclose or group incorrectly: %+v", r.ByFile)
	}
	if len(r.ByClass) != 2 || r.ByClass[0].Key != "read" || r.ByClass[1].Key != "write" {
		t.Fatalf("classes = %+v", r.ByClass)
	}
}

func TestCollectorDoesNotInventMissingPropertiesOrOperation(t *testing.T) {
	c := NewCollector(Options{})
	c.Consume(techlog.Event{Name: "DBV8DBEng", DurationMicros: 1, Fields: map[string]string{"Trans": "begin", "Rows": "broken", "RowsAffected": "-2"}})
	r := c.Result()
	if len(r.ByFunc) != 0 || len(r.ByTable) != 0 || len(r.ByClass) != 1 || r.ByClass[0].Key != "other" {
		t.Fatalf("unexpected inferred groups: %+v", r)
	}
	if r.Quality.MissingFunc != 1 || r.Quality.MissingTableName != 1 || r.Quality.MalformedRows != 1 || r.Quality.MalformedRowsAffected != 1 || r.Quality.MissingRowsAffected != 0 {
		t.Fatalf("quality = %+v", r.Quality)
	}
	if got := r.ByClass[0].Rows.RowsAffected; got.Count != 0 {
		t.Fatalf("rows affected = %+v", got)
	}
}

func TestClassificationIsExactAndPathSafe(t *testing.T) {
	for input, want := range map[string]Class{"READ": ClassRead, "BeginTransaction": ClassTransactionBegin, "CommitTransaction": ClassTransactionCommit, "RollbackTransaction": ClassTransactionRollback, "transaction begin": ClassOther, "": ClassOther} {
		if got := ClassifyFunc(input); got != want {
			t.Errorf("ClassifyFunc(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{`C:\\secret\\root\\x.1cd`: "<absolute-path>/x.1cd", `/var/lib/1c/x.1cd`: "<absolute-path>/x.1cd", `relative\\x.1cd`: "relative/x.1cd"} {
		if got := NormalizeFileName(input); got != want {
			t.Errorf("NormalizeFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSamplesAreBoundedAndErrorsRequireExplicitMarker(t *testing.T) {
	c := NewCollector(Options{SlowSampleLimit: 2, ErrorSampleLimit: 1})
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, event := range []techlog.Event{
		{Name: "DBV8DBEng", Timestamp: stamp.Add(3 * time.Second), DurationMicros: 10, Fields: map[string]string{"Func": "Read", "FileName": `D:\\secret\\a.1cd`}},
		{Name: "DBV8DBEng", Timestamp: stamp.Add(2 * time.Second), DurationMicros: 30, Fields: map[string]string{"Func": "Write", "Error": "failed", "FileName": `D:\\secret\\b.1cd`}},
		{Name: "DBV8DBEng", Timestamp: stamp.Add(time.Second), DurationMicros: 20, Fields: map[string]string{"Func": "Write", "Result": "error", "FileName": `D:\\secret\\c.1cd`}},
	} {
		c.Consume(event)
	}
	r := c.Result()
	if got := []int64{r.SlowSamples[0].DurationMicros, r.SlowSamples[1].DurationMicros}; !reflect.DeepEqual(got, []int64{30, 20}) {
		t.Fatalf("slow = %+v", r.SlowSamples)
	}
	if len(r.ErrorSamples) != 1 || r.ErrorSamples[0].FileName != "<absolute-path>/c.1cd" || r.Quality.ExplicitErrorEvents != 2 || r.Quality.DroppedErrorSamples != 1 {
		t.Fatalf("errors = %+v quality=%+v", r.ErrorSamples, r.Quality)
	}
}
