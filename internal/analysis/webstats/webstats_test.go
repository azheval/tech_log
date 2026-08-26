package webstats

import (
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestCollectorMultilineHeadersBodyCacheAndNormalization(t *testing.T) {
	const input = "00:00.000000-0,VRSREQUEST,2,process=web,OSThread=7,Method=GET,URI='/E1CIB/items/123?token=secret&id=456',Headers='Accept: application/json\nContent-Length: 12',Body='line one, line two\nbody'\n" +
		"00:00.004000-0,VRSCACHE,2,process=web,OSThread=7,action=lookup,resource='/e1cib/items/999?id=9&token=x',Method=GET,Result=hit\n" +
		"00:00.010000-0,VRSRESPONSE,2,process=web,OSThread=7,Status=200,Headers='Server: 1C\nContent-Length: 34',Body='ok'\n"
	c := NewCollector(Options{SampleLimit: 2})
	parseEvents(t, input, c.Consume)
	r := c.Result()
	if r.Quality.MatchedResponses != 1 || r.Quality.CacheHits != 1 || r.Quality.OrphanCacheEvents != 0 {
		t.Fatalf("quality: %+v", r.Quality)
	}
	if len(r.Requests) != 1 {
		t.Fatalf("requests: %+v", r.Requests)
	}
	row := r.Requests[0]
	if row.URI != "/e1cib/items/{id}?id&token" || row.Stats.TotalMicros != 10_000 || row.RequestBytes != 12 || row.ResponseBytes != 34 {
		t.Fatalf("row: %+v", row)
	}
	if len(r.Cache) != 1 || r.Cache[0].URI != "/e1cib/items/{id}?id&token" || r.Cache[0].Result != "hit" {
		t.Fatalf("cache: %+v", r.Cache)
	}
}

func TestCollectorDoesNotGuessInterleavedResponses(t *testing.T) {
	c := NewCollector(Options{})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Consume(event(base, "VRSREQUEST", "a", map[string]string{"process": "p", "OSThread": "1", "Method": "GET", "URI": "/one"}))
	c.Consume(event(base.Add(time.Millisecond), "VRSREQUEST", "a", map[string]string{"process": "p", "OSThread": "1", "Method": "GET", "URI": "/two"}))
	c.Consume(event(base.Add(2*time.Millisecond), "VRSRESPONSE", "a", map[string]string{"process": "p", "OSThread": "1", "Status": "200"}))
	r := c.Result()
	if r.Quality.AmbiguousResponses != 1 || r.Quality.OrphanResponses != 1 || r.Quality.MatchedResponses != 0 || r.Quality.PendingRequests != 2 {
		t.Fatalf("quality: %+v", r.Quality)
	}
	if len(r.Requests) != 0 {
		t.Fatalf("unexpected matching: %+v", r.Requests)
	}
}

func TestCollectorOrphanResponseAndCache(t *testing.T) {
	c := NewCollector(Options{})
	c.Consume(event(time.Time{}, "VRSRESPONSE", "a", map[string]string{"process": "p", "OSThread": "1", "Status": "503"}))
	c.Consume(event(time.Time{}, "VRSCACHE", "a", map[string]string{"process": "p", "OSThread": "1", "Method": "GET", "resource": "/x?key=secret", "Result": "miss"}))
	r := c.Result()
	if r.Quality.OrphanResponses != 1 || r.Quality.OrphanCacheEvents != 1 || r.Quality.CacheMisses != 1 {
		t.Fatalf("quality: %+v", r.Quality)
	}
	if len(r.Cache) != 1 || r.Cache[0].URI != "/x?key" {
		t.Fatalf("cache: %+v", r.Cache)
	}
}

func TestNormalizeURI(t *testing.T) {
	got := NormalizeURI("/E1CIB/X/42?b=two&a=1&a=three")
	if got != "/e1cib/x/{id}?a&a&b" {
		t.Fatalf("NormalizeURI() = %q", got)
	}
}

func parseEvents(t *testing.T, input string, consume func(techlog.Event)) {
	t.Helper()
	_, err := techlog.Parse(strings.NewReader(input), "test", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), func(e techlog.Event) error { consume(e); return nil })
	if err != nil {
		t.Fatal(err)
	}
}
func event(timestamp time.Time, name, source string, fields map[string]string) techlog.Event {
	return techlog.Event{Timestamp: timestamp, Name: name, Source: source, Fields: fields}
}
