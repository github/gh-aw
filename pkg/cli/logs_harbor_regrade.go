package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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
	exportRecord := harborSanitizeRegradedRun(regrading)
	if err := harborWriteJSON(filepath.Join(dir, filepath.FromSlash(source)), exportRecord); err != nil {
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
	for index, raw := range exportRecord.Results {
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

var harborLocalPathPattern = regexp.MustCompile(`(^|[\s("'=])([A-Za-z]:[\\/]|/)[^\s"'<>)]*`)

var harborSensitiveInlinePattern = regexp.MustCompile(`(?i)(\b(?:[a-z][a-z0-9]*[_-])*(?:token|api[_-]?key|secret|password|passwd|credential|client[_-]?secret|access[_-]?key(?:[_-]?id)?|secret[_-]?access[_-]?key)\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\\r\n]){16,}"|'(?:\\.|[^'\\\r\n]){16,}'|[^\s"'<>(),;{}&]{16,})`)
var harborBearerPattern = regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)[a-z0-9._~+/=-]{8,}`)
var harborCredentialPattern = regexp.MustCompile(`\b(?:gh[pousr]_[a-zA-Z0-9._-]{36,}|github_pat_[a-zA-Z0-9_]{82,}|sk-(?:proj-|ant-api03-)?[a-zA-Z0-9_-]{20,}|AIzaSy[a-zA-Z0-9_-]{33}|(?:AKIA|ASIA)[A-Z0-9]{16}|lin_api_[a-zA-Z0-9]{40}|xox[baprs]-[a-zA-Z0-9-]{20,}|eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,})\b`)

func harborSanitizeRegradedRun(record *RegradedRun) *RegradedRun {
	safe := *record
	safe.SourceDirectory = "gh-aw/evidence"
	safe.Run.LogsPath = "gh-aw/evidence"
	safe.SessionPath = harborRegradedEvidencePath(record.SourceDirectory, record.SessionPath)
	safe.SessionError = harborSanitizeSensitiveText(record.SessionError)
	safe.OriginalGraders = harborSanitizeErrorJSON(record.OriginalGraders)
	safe.Definitions = append([]RegradeDefinition(nil), record.Definitions...)
	for index := range safe.Definitions {
		safe.Definitions[index].Workflow = ""
		safe.Definitions[index].Selector = safe.Definitions[index].ID
	}
	safe.Results = make([]json.RawMessage, 0, len(record.Results))
	for _, raw := range record.Results {
		safe.Results = append(safe.Results, harborSanitizeErrorJSON(raw))
	}
	safe.OriginalEvidence = make([]RegradeOriginalEvidence, 0, len(record.OriginalEvidence))
	for _, evidence := range record.OriginalEvidence {
		evidence.Path = harborRegradedEvidencePath(record.SourceDirectory, evidence.Path)
		evidence.Error = harborSanitizeSensitiveText(evidence.Error)
		evidence.Data = harborSanitizeErrorJSON(evidence.Data)
		safe.OriginalEvidence = append(safe.OriginalEvidence, evidence)
	}
	return &safe
}

func harborRegradedEvidencePath(source, path string) string {
	if source == "" || path == "" {
		return ""
	}
	relative, err := filepath.Rel(source, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return ""
	}
	if relative == "." {
		return "gh-aw/evidence"
	}
	return filepath.ToSlash(filepath.Join("gh-aw/evidence", relative))
}

func harborSanitizeLocalPaths(value string) string {
	return harborLocalPathPattern.ReplaceAllString(value, `${1}[local path]`)
}

// Mask recognizable credentials, not arbitrary sensitive session prose.
func harborSanitizeSensitiveText(value string) string {
	value = harborSanitizeLocalPaths(value)
	value = harborSensitiveInlinePattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = harborBearerPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	return harborCredentialPattern.ReplaceAllString(value, "[REDACTED]")
}

func harborSanitizeErrorJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return raw
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return raw
	}
	harborSanitizeErrorFields(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

func harborSanitizeErrorFields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if message, ok := child.(string); ok && (key == "error" || key == "message") {
				typed[key] = harborSanitizeSensitiveText(message)
			} else {
				harborSanitizeErrorFields(child)
			}
		}
	case []any:
		for _, child := range typed {
			harborSanitizeErrorFields(child)
		}
	}
}
