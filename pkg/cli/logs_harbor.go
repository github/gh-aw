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

	"github.com/github/gh-aw/pkg/atif"
)

const harborExportDirectory = "harbor"

// exportHarborJob imports recorded evidence, not runnable Harbor tasks. Only
// explicitly recorded numeric grader values become namespaced native rewards.
func exportHarborJob(ctx context.Context, outputDir string, runs []ProcessedRun) error { //nolint:largefunc // Keep deterministic native job assembly and the staged publication/rollback lifecycle together.
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
	now := time.Now().UTC()
	config := map[string]any{
		"job_name": harborExportDirectory, "jobs_dir": ".", "agents": []any{harborAgentConfig()},
		"tasks": []any{}, "datasets": []any{}, "environment": map[string]any{"type": nil},
	}
	job := map[string]any{
		"id": jobID, "started_at": now, "finished_at": now, "n_total_trials": len(results),
		"stats":         map[string]any{"n_completed_trials": len(results), "n_errored_trials": 0, "evals": map[string]any{}},
		"trial_results": results,
	}
	metadata := map[string]any{
		"format": "gh-aw-harbor", "version": 1, "harbor_version": "0.25.0",
		"mode": "recorded-evidence", "trials": trialNames,
		"job_timestamps": "export time, not workflow execution time",
		"trial_counts":   "imported records, not evaluation outcomes",
		"reward_mapping": "recorded status=pass/fail finite JSON numeric grader values, unchanged, keyed gh-aw.<grader-id>; no synthetic aggregate or Harbor task success",
		"runnable":       false,
	}
	for name, value := range map[string]any{"config.json": config, "result.json": job, "gh-aw-export.json": metadata} {
		if err := harborWriteJSON(filepath.Join(staging, name), value); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return harborReplace(staging, destination)
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

func harborExportTrial(ctx context.Context, staging, name, jobID, source string, processed ProcessedRun) (map[string]any, error) { //nolint:largefunc // Assemble the native trial and its evidence/provenance sidecar in one transaction so their fields cannot diverge.
	dir := filepath.Join(staging, name)
	evidence := filepath.Join(dir, "gh-aw", "evidence")
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		return nil, err
	}
	files, err := harborSnapshot(ctx, source, evidence)
	if err != nil {
		return nil, err
	}
	sessionPath, err := findUnifiedSessionPath(evidence)
	if err != nil {
		return nil, err
	}
	sessionStatus := "missing"
	agentInfo, agentProvenance := harborRecordedAgent(evidence)
	if sessionPath != "" {
		content, err := readATIFSessionFile(sessionPath)
		if err != nil {
			return nil, err
		}
		content, err = normalizeATIFSession(ctx, content, sessionPath, evidence)
		if err != nil {
			return nil, err
		}
		trajectory, err := convertSessionATIF(ctx, content)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Join(dir, "agent"), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "agent", "trajectory.json"), trajectory, 0o600); err != nil {
			return nil, err
		}
		var identity struct {
			Agent atif.Agent `json:"agent"`
		}
		if err := json.Unmarshal(trajectory, &identity); err != nil {
			return nil, err
		}
		if identity.Agent.Name != "gh-aw-unified" {
			if agentInfo["name"] != identity.Agent.Name {
				agentInfo["version"] = "unknown"
				delete(agentInfo, "model_info")
			}
			agentInfo["name"] = identity.Agent.Name
		} else if agentInfo["name"] == "gh-aw" {
			agentInfo["name"] = identity.Agent.Name
		}
		if identity.Agent.Version != "unknown" {
			agentInfo["version"] = identity.Agent.Version
		}
		if identity.Agent.ModelName != "" {
			agentInfo["model_info"] = map[string]any{"name": identity.Agent.ModelName}
		}
		agentProvenance = "agent/trajectory.json#/agent via existing ATIF converter; missing identity fields fall back to " + agentProvenance
		sessionStatus = "recorded"
	}
	run := processed.Run
	run.LogsPath = "gh-aw/evidence"
	config := map[string]any{
		"task": map[string]any{"path": "gh-aw/evidence"}, "trial_name": name,
		"trials_dir": ".", "agent": harborAgentConfig(), "job_id": jobID,
		"environment": map[string]any{"type": nil},
	}
	result := map[string]any{
		"id": harborUUID(harborRunIdentity(processed)), "task_name": run.WorkflowName,
		"trial_name": name, "trial_uri": "./" + name, "task_id": map[string]any{"path": "gh-aw/evidence"},
		"task_checksum": "", "config": config,
		"agent_info":   agentInfo,
		"agent_result": nil, "verifier_result": nil, "exception_info": nil,
		"started_at": harborRecordedTime(run.StartedAt), "finished_at": nil,
	}
	if run.Status == "completed" {
		result["finished_at"] = harborRecordedTime(run.UpdatedAt)
	}
	graders := extractGradersData(evidence)
	rewards, mapping, err := harborRecordedRewards(evidence, graders)
	if err != nil {
		return nil, err
	}
	metadata := map[string]any{
		"format": "gh-aw-harbor-trial", "version": 1, "run": run,
		"context": processed.AwContext, "episode_contexts": processed.EpisodeContexts,
		"session_status": sessionStatus, "evidence_files": files,
		"graders": graders, "experiments": extractExperimentData(evidence),
		"experiment_assignment_provenance": "recorded_usage_summary_or_unavailable",
		"reward_mapping":                   nil, "verifier_status": "not-recorded",
		"task_identity":             "unknown; task.path references evidence, not a runnable Harbor task",
		"agent_identity":            "recorded GH-AW execution; import_path is an inert identity, not an installed agent",
		"agent_identity_provenance": agentProvenance,
		"trial_timestamps":          "started_at from Actions startedAt; finished_at from updatedAt only for completed runs",
	}
	if len(rewards) > 0 {
		result["verifier_result"] = map[string]any{"rewards": rewards}
		metadata["reward_mapping"] = mapping
		metadata["verifier_status"] = "recorded-gh-aw-grader-metrics"
		if err := os.MkdirAll(filepath.Join(dir, "verifier"), 0o700); err != nil {
			return nil, err
		}
		if err := harborWriteJSON(filepath.Join(dir, "verifier", "reward.json"), rewards); err != nil {
			return nil, err
		}
	}
	if state := findExperimentStatePath(evidence); state != "" {
		relative, err := filepath.Rel(dir, state)
		if err != nil {
			return nil, err
		}
		metadata["experiment_state"] = filepath.ToSlash(relative)
		raw, err := os.ReadFile(state)
		if err != nil {
			return nil, err
		}
		parsed := parseExperimentState(raw)
		if parsed != nil && len(parsed.Counts) > 0 {
			metadata["experiment_assignment_provenance"] = "legacy_max_count_heuristic"
			if len(parsed.Runs) > 0 && len(parsed.Runs[len(parsed.Runs)-1].Assignments) > 0 {
				record := parsed.Runs[len(parsed.Runs)-1]
				metadata["experiment_assignment_provenance"] = "recorded_ledger_latest_run"
				metadata["experiment_assignment_run_id"] = record.RunID
			}
		}
	}
	for path, value := range map[string]any{
		"config.json": config, "result.json": result, "gh-aw/metadata.json": metadata,
	} {
		if err := harborWriteJSON(filepath.Join(dir, filepath.FromSlash(path)), value); err != nil {
			return nil, err
		}
	}
	return result, ctx.Err()
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
			return fmt.Errorf("Harbor export path is a symbolic link: %s", current)
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
		return fmt.Errorf("Harbor export destination is not a directory: %s", destination)
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
