package output

import (
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/model"
)

func TestHTMLRenderersProduceUTF8AndEscapeContent(t *testing.T) {
	meta := model.RunMeta{Report: "sdbl-context", FinishedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC), InputRoot: `C:\logs`, FilesMatched: 1, FilesProcessed: 1, Workers: 2}
	contextReport := model.ContextReport{Meta: meta, Totals: model.Totals{EventCount: 2, DurationMS: 12.5, AverageDuration: 6.25}, Rows: []model.ContextRow{{Rank: 1, Context: `<script>alert(1)</script>`, ShortContext: "Клиент"}}}
	errorReport := model.ErrorReport{Meta: meta, Rows: []model.ErrorRow{{Rank: 1, Event: "EXCP", Description: `<img src=x onerror=1>`}}}
	rawContext := model.RawContextReport{Meta: meta, Days: []model.RawContextDay{{Date: "2026-08-23", Hours: []model.RawContextHour{{Events: []model.RawContextEvent{{Timestamp: meta.FinishedAt, Context: `<b>bad</b>`}}}}}}}
	rawError := model.RawErrorReport{Meta: meta, Days: []model.RawErrorDay{{Date: "2026-08-23", Hours: []model.RawErrorHour{{Events: []model.RawErrorEvent{{Timestamp: meta.FinishedAt, Description: `<b>bad</b>`}}}}}}}

	for name, render := range map[string]func() ([]byte, error){
		"context":     func() ([]byte, error) { return RenderContextHTML(contextReport) },
		"error":       func() ([]byte, error) { return RenderErrorHTML(errorReport) },
		"raw context": func() ([]byte, error) { return RenderRawContextHTML(rawContext) },
		"raw error":   func() ([]byte, error) { return RenderRawErrorHTML(rawError) },
	} {
		t.Run(name, func(t *testing.T) {
			data, err := render()
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.HasPrefix(text, "<!doctype html>") || !strings.Contains(text, `<meta charset="utf-8">`) {
				t.Fatalf("not a UTF-8 HTML document: %q", text[:min(len(text), 80)])
			}
			if strings.Contains(text, "<script>alert(1)</script>") || strings.Contains(text, "<img src=x onerror=1>") || strings.Contains(text, "<b>bad</b>") {
				t.Fatalf("unsafe markup was not escaped: %s", text)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
