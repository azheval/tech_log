package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedAssetsWithCSP(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/app.js", "/styles.css"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: got status %d", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("GET %s: missing strict CSP", path)
		}
	}
}

func TestUIHasRequiredElementsAndNoInlineDataSink(t *testing.T) {
	index, err := assets.Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	b, err := io.ReadAll(index)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, id := range []string{"run-form", "runs", "tab-overview", "tab-events", "tab-sql", "tab-traces", "tab-locks", "tab-sources", "baseline", "current", "downloads", "search", "group-by", "severity", "limit", "reset-filters", "column-settings", "column-panel", "toggle-sidebar", "control-sidebar"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("missing required element %q", id)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "style=") || strings.Contains(html, "onclick=") {
		t.Error("index must remain CSP-compatible")
	}
	js, err := assets.Open("app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer js.Close()
	b, err = io.ReadAll(js)
	if err != nil {
		t.Fatal(err)
	}
	code := string(b)
	if strings.Contains(code, "innerHTML") {
		t.Error("app must not use innerHTML for data")
	}
	for _, route := range []string{"/config", "allowed_input_root", "default_request", "bucket_interval", "bucketControlValue", "min_duration_micros", "/runs", "payload.runs", "run.request.input_root", "/report/overview", "time_buckets", "contexts", "group-by", "locks: \"locks\"", "/report/trace", "/report/source-events", "/report/compare", "/cancel", "succeeded", "canceled", "files_completed", "files_matched", "progress.current", "filters: []", "comparisonRows", "body.error.message", "row.Sample", "row.ID", "row.value", "$(\"input\").value.trim()", "completedNow", "navigateTabs", "searchTimer", "statusLabel", "progressLabel", "sidebarStorageKey", "localStorage", "setSidebarCollapsed", "techlog-stat:column-settings:v1", "columnSettingsScope", "canDrillAggregate", "renderColumnPanel", "Restore default order"} {
		if !strings.Contains(code, route) {
			t.Errorf("app is missing contract route %q", route)
		}
	}
	for _, obsolete := range []string{"/aggregates/", "method: \"DELETE\"", "\"/compare\""} {
		if strings.Contains(code, obsolete) {
			t.Errorf("app still contains obsolete API usage %q", obsolete)
		}
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "sql.csv") || strings.Contains(string(readme), "sql_rows.csv") {
		t.Error("README must document the exact sql.csv download filename")
	}
}

func TestStylesheetProvidesUsableResponsiveLayout(t *testing.T) {
	b, err := os.ReadFile("styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	if len(css) < 5000 {
		t.Fatalf("stylesheet is unexpectedly small: %d bytes", len(css))
	}
	for _, rule := range []string{"--blue:", "--card:#fff", "--page-gutter:", ".app-header-inner{width:100%", ".app-shell{width:100%", ".sidebar", ".sidebar-collapsed .app-shell", ".sidebar-collapsed .sidebar{display:none}", ".sidebar-toggle", ".main-column{display:grid;min-width:0;max-width:100%", ".results-card{width:100%;max-width:100%;min-width:0", ".tabs", ".filters", ".table-wrap", "contain:inline-size;width:100%;max-width:100%", "overscroll-behavior:contain", ".data-table", ".advanced-options .field-grid>.form-field", ".column-panel", ".column-list", ".column-move", "@media(max-width:980px)", "@media(max-width:700px)", "@media(max-width:460px)"} {
		if !strings.Contains(css, rule) {
			t.Errorf("stylesheet is missing required layout rule %q", rule)
		}
	}
	for _, forbidden := range []string{"--page-max:", "max-width:var(--page-max)", "width:min(1520px,100%)", "padding:1rem max(1.25rem,calc((100vw - 1520px)/2))"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("stylesheet still has viewport-dependent alignment %q", forbidden)
		}
	}
}
