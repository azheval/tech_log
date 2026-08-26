package output

import (
	"encoding/json"

	"techlog-stat/internal/model"
	"techlog-stat/internal/report/overview"
)

func RenderRunJSON(report model.ContextReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func RenderErrorRunJSON(report model.ErrorReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func RenderRawContextJSON(report model.RawContextReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func RenderRawErrorJSON(report model.RawErrorReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func RenderOverviewJSON(report overview.OverviewResult) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
