package config

import "testing"

func TestParseMinDurationMicrosBareNumberMeansSeconds(t *testing.T) {
	got, err := ParseMinDurationMicros("5")
	if err != nil {
		t.Fatalf("ParseMinDurationMicros() error = %v", err)
	}
	if got != 5_000_000 {
		t.Fatalf("ParseMinDurationMicros(5) = %d, want %d", got, int64(5_000_000))
	}
}

func TestParseMinDurationMicrosSupportsMilliseconds(t *testing.T) {
	got, err := ParseMinDurationMicros("500ms")
	if err != nil {
		t.Fatalf("ParseMinDurationMicros() error = %v", err)
	}
	if got != 500_000 {
		t.Fatalf("ParseMinDurationMicros(500ms) = %d, want %d", got, int64(500_000))
	}
}

func TestMatchAllFiltersReturnsFalseWhenFieldMissing(t *testing.T) {
	ok := MatchAllFilters("process=1CV8C,Usr=DefUser", []Filter{{Key: "Usr", Value: "DefUser"}, {Key: "DataBase", Value: "conf_null"}})
	if ok {
		t.Fatalf("MatchAllFilters() = true, want false")
	}
}
