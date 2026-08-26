package techlog

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseHandlesBOMEOFAndLongLine(t *testing.T) {
	longDescription := strings.Repeat("x", 4*1024*1024+1)
	input := "\ufeff00:00.000001-1,SDBL,1,Context=First\n" +
		"00:00.000002-2,EXCP,1,Descr='" + longDescription + "'" // Deliberately no final newline.
	var events []Event
	stats, err := Parse(strings.NewReader(input), "source", time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.BytesRead != int64(len(input)) || stats.LinesRead != 2 || stats.Events != 2 || len(events) != 2 {
		t.Fatalf("stats/events = %+v / %d", stats, len(events))
	}
	if got := len(events[1].Fields["Descr"]); got != len(longDescription) {
		t.Fatalf("long Descr length = %d, want %d", got, len(longDescription))
	}
}

func TestParseFieldsHandlesQuotedCommasAndDoubledQuotes(t *testing.T) {
	fields := parseFields(`Sql='select ''quoted'', a,b', Name="a,b", Empty='', Plain=ok`)
	if fields["Sql"] != "select ''quoted'', a,b" || fields["Name"] != "a,b" || fields["Empty"] != "" || fields["Plain"] != "ok" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestParseRejectsInvalidTimestamps(t *testing.T) {
	input := "" +
		"00:00.-1-1,SDBL,1,Context=x\n" +
		"00:60.000000-1,SDBL,1,Context=x\n" +
		"00:00.1234567-1,SDBL,1,Context=x\n"
	stats, err := Parse(strings.NewReader(input), "source", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Events != 0 || stats.MalformedHeaders != 3 {
		t.Fatalf("stats = %+v, want three malformed headers", stats)
	}
}

func FuzzParseNeverPanicsAndKeepsStatsConsistent(f *testing.F) {
	for _, seed := range [][]byte{
		nil, []byte("orphan\n"), []byte("00:00.000000-1,SDBL,1,Context=x\n"),
		[]byte("\xef\xbb\xbf00:00.000001-1,EXCP,1,Descr='a,b'"), []byte("00:00.-1-1,SDBL,1\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		var emitted int64
		stats, err := Parse(bytes.NewReader(input), "fuzz", time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), func(Event) error {
			emitted++
			return nil
		})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if stats.BytesRead != int64(len(input)) || stats.Events != emitted {
			t.Fatalf("inconsistent stats=%+v emitted=%d input=%d", stats, emitted, len(input))
		}
		if stats.Events < 0 || stats.MalformedHeaders < 0 || stats.OrphanLines < 0 || stats.Events+stats.MalformedHeaders > stats.LinesRead {
			t.Fatalf("invalid stats=%+v", stats)
		}
	})
}

func FuzzParseFieldsNeverPanics(f *testing.F) {
	for _, seed := range []string{"", "A=1", "Sql='a,b'", `Name="a,b",Sql='it''s'`, "===", "A='unterminated"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		fields := parseFields(input)
		if fields == nil {
			t.Fatal("parseFields returned nil")
		}
		for key := range fields {
			if strings.TrimSpace(key) == "" {
				t.Fatalf("empty field key in %#v", fields)
			}
		}
	})
}

func BenchmarkParseLargeSynthetic(b *testing.B) {
	const event = "00:00.123456-123,SDBL,1,process=rphost,OSThread=7,Sql='SELECT * FROM T WHERE id=42',Context=Module.Run\n"
	data := strings.Repeat(event, 50_000)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		stats, err := Parse(strings.NewReader(data), "benchmark", time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), nil)
		if err != nil || stats.Events != 50_000 {
			b.Fatalf("Parse() stats=%+v err=%v", stats, err)
		}
	}
}
