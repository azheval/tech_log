package output

import (
	"encoding/json"

	"techlog-stat/internal/model"
)

func RenderRunJSON(report model.ContextReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func RenderErrorRunJSON(report model.ErrorReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
