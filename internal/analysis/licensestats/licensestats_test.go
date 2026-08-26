package licensestats

import (
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/techlog"
)

func TestLicenseAndHASPRealisticMultiline(t *testing.T) {
	const input = "00:00.000000-9,LIC,1,process=1CV8C,OSThread=2,Func=getLicense,res=seize,txt='0, client, seize, 2475193431936, local Application;\n soft, file://C:/ProgramData/1C/licenses/old.lic, cached, validity date expired, long, 500000024933;\n soft, file://C:/ProgramData/1C/licenses/good.lic, cached, long, 8102511864G0, 558171398177522, client, 1, 0'\n" +
		"00:00.001000-1,HASP,2,process=1CV8C,Usr=DefUser,Txt='Computer parameters from cache:\nOS_0: Microsoft Windows 11 Pro\nSys Name_0: DELL-XPS17\nSys Type_0: XPS 17 9720, x64-based PC\nPhis Mem_0: 68388851712\nNET_0: TAP, 00:FF:A9:3E:0C:DD'\n"
	c := NewCollector(Options{SampleLimit: 2})
	parse(t, input, c.Consume)
	r := c.Result()
	if r.Quality.Expired != 1 || r.Quality.HASPCache != 1 || len(r.Licenses) != 1 || r.Licenses[0].Success != 1 || r.Licenses[0].Expired != 1 {
		t.Fatalf("result: %+v %+v", r.Quality, r.Licenses)
	}
	if r.Systems.OSFamilies["Windows"] != 1 || r.Systems.SystemTypes["x64"] != 1 || r.Systems.MemoryBuckets["64 GiB"] != 1 {
		t.Fatalf("systems: %+v", r.Systems)
	}
	if len(r.ErrorSamples) != 1 || strings.Contains(r.ErrorSamples[0].Text, "2475193431936") || strings.Contains(r.ErrorSamples[0].Text, "good.lic") {
		t.Fatalf("redaction: %+v", r.ErrorSamples)
	}
}
func TestExpiredFailureAndMissing(t *testing.T) {
	c := NewCollector(Options{})
	c.Consume(ev("LIC", map[string]string{"txt": "validity date expired, incorrect license type"}))
	c.Consume(ev("HASP", map[string]string{"Txt": "Computer parameters from OS:\nOS_0: Linux"}))
	r := c.Result()
	if r.Quality.MissingFunc != 1 || r.Quality.MissingResult != 1 || r.Quality.Expired != 1 || r.Quality.WrongType != 1 || r.Quality.HASPOS != 1 || r.Systems.OSFamilies["Linux"] != 1 {
		t.Fatalf("result: %+v %+v", r.Quality, r.Systems)
	}
}
func parse(t *testing.T, input string, consume func(techlog.Event)) {
	t.Helper()
	_, err := techlog.Parse(strings.NewReader(input), "test", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), func(e techlog.Event) error { consume(e); return nil })
	if err != nil {
		t.Fatal(err)
	}
}
func ev(name string, fields map[string]string) techlog.Event {
	return techlog.Event{Name: name, Fields: fields}
}
