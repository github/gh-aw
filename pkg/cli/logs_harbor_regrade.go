package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

func exportLogsHarbor(ctx context.Context, outputDir string, runs []ProcessedRun, selectors []string) error {
	if len(selectors) == 0 {
		return logsExportHarborJob(ctx, outputDir, runs)
	}
	definitions, err := loadAdditionalGraders(selectors)
	if err != nil {
		return err
	}
	regraded, err := regradeProcessedRuns(ctx, outputDir, runs, definitions)
	if err != nil {
		return err
	}
	byIdentity := make(map[string]*RegradedRun, len(regraded))
	for i := range regraded {
		record := &regraded[i]
		byIdentity[record.Identity] = record
	}
	// Keep report/cache records independent of the derived export.
	exportRuns := append([]ProcessedRun(nil), runs...)
	for i := range exportRuns {
		exportRuns[i].Regrading = byIdentity[harborRunIdentity(exportRuns[i])]
	}
	return logsExportHarborJob(ctx, outputDir, exportRuns)
}

func harborRegradedRewards(dir string, regrading *RegradedRun, metadata map[string]any, rewards map[string]json.Number, mapping map[string]any) (map[string]json.Number, map[string]any, error) {
	if regrading == nil {
		return rewards, mapping, nil
	}
	const source = "gh-aw/regrading.json"
	if err := harborWriteJSON(filepath.Join(dir, filepath.FromSlash(source)), regrading); err != nil {
		return nil, nil, err
	}
	metadata["regrading"] = source
	if rewards == nil {
		rewards = make(map[string]json.Number)
	}
	if mapping == nil {
		mapping = map[string]any{"metrics": map[string]any{}}
	}
	metrics, ok := mapping["metrics"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("invalid Harbor reward mapping")
	}
	seen := make(map[string]struct{})
	for index, raw := range regrading.Results {
		var result struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Value  json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, nil, err
		}
		if _, exists := seen[result.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate additional grader reward identity %q", result.ID)
		}
		seen[result.ID] = struct{}{}
		if result.ID == "" || (result.Status != "pass" && result.Status != "fail") {
			continue
		}
		value, ok := harborRawNumericValue(result.Value)
		if !ok {
			continue
		}
		key := "gh-aw.regraded." + result.ID
		if _, exists := rewards[key]; exists {
			return nil, nil, fmt.Errorf("additional grader reward key conflicts with recorded reward %q", key)
		}
		rewards[key] = value
		metrics[key] = map[string]any{
			"grader_id": result.ID, "status": result.Status, "source_file": source,
			"source_field":   fmt.Sprintf("results/%d/value", index),
			"transformation": "none; additional in-memory grader numeric JSON value",
			"provenance":     "offline regrading; not an original recorded score",
		}
	}
	mapping["additional_policy"] = "additional status=pass/fail finite numeric values only; originals unchanged; provenance in gh-aw/regrading.json"
	return rewards, mapping, nil
}
