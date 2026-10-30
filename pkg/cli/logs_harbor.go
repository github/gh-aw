package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const harborExportDirectory = "harbor"

// exportHarborJob imports recorded evidence, not runnable Harbor tasks. Only
// explicitly recorded numeric grader values become namespaced native rewards.
func exportHarborJob(ctx context.Context, outputDir string, runs []ProcessedRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	outputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if err := harborCheckPath(outputDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return err
	}
	destination := filepath.Join(outputDir, harborExportDirectory)
	if err := harborCheckDestination(destination); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(outputDir, ".harbor-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := harborExportJobRecords(ctx, outputDir, staging, runs); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return harborReplace(staging, destination)
}

func harborUniqueRuns(runs []ProcessedRun) (map[string]ProcessedRun, []string) {
	// Sorting and deduplication make job and trial identities independent of selection order.
	selected := make(map[string]ProcessedRun)
	for _, run := range runs {
		key := harborRunIdentity(run)
		if _, exists := selected[key]; !exists {
			selected[key] = run
		}
	}
	keys := make([]string, 0, len(selected))
	for key := range selected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return selected, keys
}

func harborExportJobRecords(ctx context.Context, outputDir, staging string, runs []ProcessedRun) error {
	selected, keys := harborUniqueRuns(runs)
	jobID := harborUUID("job\x00" + strings.Join(keys, "\x00"))
	results := make([]map[string]any, 0, len(keys))
	trialNames := make([]string, 0, len(keys))
	for _, key := range keys {
		run := selected[key]
		source := run.Run.LogsPath
		if source == "" {
			source = resolveAuditOutputDir(outputDir, run.Run.DatabaseID)
		}
		trialName := fmt.Sprintf("run-%d-attempt-%d-%s", run.Run.DatabaseID, harborAttempt(run.Run), harborUUID(key))
		result, err := harborExportTrial(ctx, staging, trialName, jobID, source, run)
		if err != nil {
			return fmt.Errorf("export Harbor run %d: %w", run.Run.DatabaseID, err)
		}
		results = append(results, result)
		trialNames = append(trialNames, trialName)
	}
	return harborWriteJobFiles(staging, jobID, results, trialNames)
}

func harborWriteJobFiles(staging, jobID string, results []map[string]any, trialNames []string) error {
	now := time.Now().UTC()
	config := map[string]any{
		"job_name": harborExportDirectory, "jobs_dir": ".", "agents": []any{harborAgentConfig()},
		"tasks": []any{}, "datasets": []any{}, "environment": map[string]any{"type": nil},
	}
	job := map[string]any{
		"id": jobID, "started_at": now, "finished_at": now, "n_total_trials": len(results),
		"stats":         harborJobStats(results),
		"trial_results": results,
	}
	metadata := map[string]any{
		"format": "gh-aw-harbor", "version": 1, "harbor_version": "0.25.0",
		"mode": "recorded-evidence", "trials": trialNames,
		"job_timestamps":            "export time, not workflow execution time",
		"trial_counts":              "imported records, not evaluation outcomes",
		"reward_mapping":            "recorded status=pass/fail finite JSON numeric grader values, unchanged, keyed gh-aw.<grader-id>; no synthetic aggregate or Harbor task success",
		"additional_reward_mapping": "when explicitly selected, offline status=pass/fail finite numeric values keyed gh-aw.regraded.<grader-id>; separate provenance in each trial's gh-aw/regrading.json",
		"runnable":                  false,
	}
	for name, value := range map[string]any{"config.json": config, "result.json": job, "gh-aw-export.json": metadata} {
		if err := harborWriteJSON(filepath.Join(staging, name), value); err != nil {
			return err
		}
	}
	return nil
}

func harborAttempt(run WorkflowRun) int {
	if run.Attempt > 0 {
		return run.Attempt
	}
	return 1
}

func harborRunIdentity(run ProcessedRun) string {
	host, repository := "unknown", run.Run.Repository
	if parsed, err := url.Parse(run.Run.URL); err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Host)
		if repository == "" {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) >= 2 {
				repository = strings.Join(parts[:2], "/")
			}
		}
	}
	if repository == "" && run.AwContext != nil {
		repository = run.AwContext.Repo
	}
	return fmt.Sprintf("%s/%s/%d/%d", host, strings.ToLower(repository), run.Run.DatabaseID, harborAttempt(run.Run))
}

func harborUUID(identity string) string {
	sum := sha256.Sum256([]byte("gh-aw-harbor\x00" + identity))
	sum[6] = (sum[6] & 0x0f) | 0x80 // RFC 9562 custom, deterministic UUID.
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func harborAgentConfig() map[string]any {
	return map[string]any{
		"name": "gh-aw",
		// An explicit non-native identity prevents Harbor's default oracle-agent inference.
		"import_path": "gh_aw.recorded:RecordedAgent",
	}
}

func harborRecordedTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func harborWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Reject every existing symlink ancestor, including the selected root itself.
func harborCheckPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("harbor export path is a symbolic link: %s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func harborCheckDestination(destination string) error {
	if err := harborCheckPath(destination); err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("harbor export destination is not a directory: %s", destination)
	}
	marker := filepath.Join(destination, "gh-aw-export.json")
	if err := harborCheckPath(marker); err != nil {
		return err
	}
	markerInfo, err := os.Lstat(marker)
	if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Size() > 64*1024 {
		return errors.New("refusing to replace unowned Harbor directory: invalid ownership marker")
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return fmt.Errorf("refusing to replace unowned Harbor directory: %w", err)
	}
	var document struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if json.Unmarshal(data, &document) != nil || document.Format != "gh-aw-harbor" || document.Version != 1 {
		return errors.New("refusing to replace unowned Harbor directory")
	}
	return nil
}

func harborReplace(staging, destination string) error {
	if err := harborCheckDestination(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		return os.Rename(staging, destination)
	}
	backup, err := os.MkdirTemp(filepath.Dir(destination), ".harbor-backup-")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		return errors.Join(err, os.Rename(backup, destination))
	}
	return os.RemoveAll(backup)
}
