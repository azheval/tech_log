package compare

import (
	"encoding/json"
	"fmt"
	"os"

	"techlog-stat/internal/report/overview"
)

// LoadOverview reads a JSON artifact emitted for an overview report.
func LoadOverview(path string) (overview.OverviewResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return overview.OverviewResult{}, fmt.Errorf("read overview %s: %w", path, err)
	}
	var result overview.OverviewResult
	if err := json.Unmarshal(data, &result); err != nil {
		return overview.OverviewResult{}, fmt.Errorf("decode overview %s: %w", path, err)
	}
	return result, nil
}

// LoadAndCompare reads two overview JSON artifacts and compares them.
func LoadAndCompare(baselinePath, currentPath string, options Options) (Result, error) {
	baseline, err := LoadOverview(baselinePath)
	if err != nil {
		return Result{}, err
	}
	current, err := LoadOverview(currentPath)
	if err != nil {
		return Result{}, err
	}
	if err := ValidateOptions(options); err != nil {
		return Result{}, err
	}
	return Compare(baseline, current, options), nil
}
