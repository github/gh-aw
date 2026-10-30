package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/atif"
)

func harborExportTrial(ctx context.Context, staging, name, jobID, source string, processed ProcessedRun) (map[string]any, error) {
	dir := filepath.Join(staging, name)
	evidence := filepath.Join(dir, "gh-aw", "evidence")
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		return nil, err
	}
	files, err := harborSnapshot(ctx, source, evidence)
	if err != nil {
		return nil, err
	}
	sessionStatus, agentInfo, agentProvenance, err := harborExportTrajectory(ctx, dir, evidence)
	if err != nil {
		return nil, err
	}
	config, result := harborTrialRecords(name, jobID, processed, agentInfo)
	graders := extractGradersData(evidence)
	rewards, mapping, err := harborRecordedRewards(evidence, graders)
	if err != nil {
		return nil, err
	}
	metadata := harborTrialMetadata(processed, sessionStatus, files, graders, agentProvenance, evidence)
	if err := harborExperimentProvenance(dir, evidence, metadata); err != nil {
		return nil, err
	}
	if err := harborPublishRewards(dir, result, metadata, rewards, mapping); err != nil {
		return nil, err
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

func harborTrialRecords(name, jobID string, processed ProcessedRun, agentInfo map[string]any) (map[string]any, map[string]any) {
	run := processed.Run
	config := map[string]any{
		"task": map[string]any{"path": "gh-aw/evidence"}, "trial_name": name,
		"trials_dir": ".", "agent": harborAgentConfig(), "job_id": jobID,
		"environment": map[string]any{"type": nil},
	}
	result := map[string]any{
		"id": harborUUID(harborRunIdentity(processed)), "task_name": run.WorkflowName,
		"trial_name": name, "trial_uri": "./" + name, "task_id": map[string]any{"path": "gh-aw/evidence"},
		"task_checksum": "", "config": config, "agent_info": agentInfo,
		"agent_result": nil, "verifier_result": nil, "exception_info": nil,
		"started_at": harborRecordedTime(run.StartedAt), "finished_at": nil,
	}
	if run.Status == "completed" {
		result["finished_at"] = harborRecordedTime(run.UpdatedAt)
	}
	return config, result
}

func harborTrialMetadata(processed ProcessedRun, sessionStatus string, files []string, graders *GradersData, agentProvenance, evidence string) map[string]any {
	run := processed.Run
	run.LogsPath = "gh-aw/evidence"
	return map[string]any{
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
}

func harborExportTrajectory(ctx context.Context, dir, evidence string) (string, map[string]any, string, error) {
	agentInfo, provenance := harborRecordedAgent(evidence)
	sessionPath, err := findUnifiedSessionPath(evidence)
	if err != nil || sessionPath == "" {
		return "missing", agentInfo, provenance, err
	}
	content, err := readATIFSessionFile(sessionPath)
	if err != nil {
		return "", nil, "", err
	}
	content, err = normalizeATIFSession(ctx, content, sessionPath, evidence)
	if err != nil {
		return "", nil, "", err
	}
	trajectory, err := convertSessionATIF(ctx, content)
	if err != nil {
		return "", nil, "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "agent"), 0o700); err != nil {
		return "", nil, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "agent", "trajectory.json"), trajectory, 0o600); err != nil {
		return "", nil, "", err
	}
	if err := harborMergeAgentInfo(agentInfo, trajectory); err != nil {
		return "", nil, "", err
	}
	return "recorded", agentInfo, "agent/trajectory.json#/agent via existing ATIF converter; missing identity fields fall back to " + provenance, nil
}

func harborMergeAgentInfo(agentInfo map[string]any, trajectory []byte) error {
	var identity struct {
		Agent atif.Agent `json:"agent"`
	}
	if err := json.Unmarshal(trajectory, &identity); err != nil {
		return err
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
	return nil
}

func harborPublishRewards(dir string, result, metadata map[string]any, rewards map[string]json.Number, mapping map[string]any) error {
	if len(rewards) == 0 {
		return nil
	}
	result["verifier_result"] = map[string]any{"rewards": rewards}
	metadata["reward_mapping"] = mapping
	metadata["verifier_status"] = "recorded-gh-aw-grader-metrics"
	if err := os.MkdirAll(filepath.Join(dir, "verifier"), 0o700); err != nil {
		return err
	}
	return harborWriteJSON(filepath.Join(dir, "verifier", "reward.json"), rewards)
}

func harborExperimentProvenance(dir, evidence string, metadata map[string]any) error {
	state := findExperimentStatePath(evidence)
	if state == "" {
		return nil
	}
	relative, err := filepath.Rel(dir, state)
	if err != nil {
		return err
	}
	metadata["experiment_state"] = filepath.ToSlash(relative)
	raw, err := os.ReadFile(state)
	if err != nil {
		return err
	}
	parsed := parseExperimentState(raw)
	if parsed != nil && len(parsed.Counts) > 0 {
		metadata["experiment_assignment_provenance"] = "legacy_max_count_heuristic"
		last := len(parsed.Runs) - 1
		if last >= 0 && last < len(parsed.Runs) {
			record := parsed.Runs[last]
			if len(record.Assignments) > 0 {
				metadata["experiment_assignment_provenance"] = "recorded_ledger_latest_run"
				metadata["experiment_assignment_run_id"] = record.RunID
			}
		}
	}
	return nil
}
