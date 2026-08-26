package sqlstats

import (
	"reflect"
	"testing"

	"techlog-stat/internal/techlog"
)

func TestNormalizeIgnoresLiteralsAndWhitespace(t *testing.T) {
	left := Normalize("SELECT  *\nFROM Items WHERE id = 42 AND amount=1.50")
	right := Normalize("SELECT * FROM Items WHERE id = 7 AND amount = 9.25")
	if left != "SELECT * FROM Items WHERE id=? AND amount=?" || left != right {
		t.Fatalf("normalization differs: left=%q right=%q", left, right)
	}
}

func TestNormalizeQuotedStringsAndIdentifiers(t *testing.T) {
	if got, want := Normalize("select 'it''s, fine', \"name 42\" from t42 where code='x'"), "select ?, \"name 42\" from t42 where code=?"; got != want {
		t.Fatalf("Normalize() = %q, want %q", got, want)
	}
}

func TestCollectorGroupsAndCalculatesStatistics(t *testing.T) {
	c := NewCollector()
	for _, event := range []techlog.Event{
		{Name: "SDBL", DurationMicros: 10, Fields: map[string]string{"Sdbl": "SELECT *\nFROM T WHERE id=1", "Rows": "2", "Context": "A", "Usr": "u2", "DataBase": "db"}},
		{Name: "SDBL", DurationMicros: 20, Fields: map[string]string{"Sdbl": "SELECT * FROM T WHERE id=2", "Rows": "5", "Context": "B", "Usr": "u1", "DataBase": "db"}},
		{Name: "SDBL", DurationMicros: 30, Fields: map[string]string{"Sdbl": "SELECT * FROM T WHERE id=3", "Rows": "1", "Context": "A", "Usr": "u2", "DataBase": "other"}},
		{Name: "DBMSSQL", DurationMicros: 100, Fields: map[string]string{"Sql": "UPDATE T SET name='one' WHERE id=1"}},
	} {
		c.Consume(event)
	}
	rows := c.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].EventType != "DBMSSQL" || rows[1].EventType != "SDBL" {
		t.Fatalf("order = %+v", rows)
	}
	row := rows[1]
	if row.NormalizedQuery != "SELECT * FROM T WHERE id=?" || row.Count != 3 || row.TotalDurationMicros != 60 || row.MinDurationMicros != 10 || row.MaxDurationMicros != 30 || row.MeanDurationMicros != 20 || row.P50DurationMicros != 20 || row.P95DurationMicros != 30 || row.P99DurationMicros != 30 {
		t.Fatalf("unexpected duration group: %+v", row)
	}
	if row.RowsSum != 8 || row.RowsMax != 5 {
		t.Fatalf("rows stats = %+v", row)
	}
	if !reflect.DeepEqual(row.Contexts, []string{"A", "B"}) || !reflect.DeepEqual(row.Users, []string{"u1", "u2"}) || !reflect.DeepEqual(row.Databases, []string{"db", "other"}) {
		t.Fatalf("dimensions = %+v", row)
	}
}

func TestCollectorPrefersSqlAndHasDeterministicSample(t *testing.T) {
	c := NewCollector()
	c.Consume(techlog.Event{Name: "SDBL", DurationMicros: 1, Fields: map[string]string{"Sql": "select 9", "Sdbl": "select 99"}})
	c.Consume(techlog.Event{Name: "SDBL", DurationMicros: 2, Fields: map[string]string{"Sql": "select 1"}})
	rows := c.Rows()
	if len(rows) != 1 || rows[0].Count != 2 || rows[0].Sample != "select 1" {
		t.Fatalf("rows = %+v", rows)
	}
}
