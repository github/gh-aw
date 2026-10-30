package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func offlineGradingRawRun(directory string) (WorkflowRun, error) {
	path := filepath.Join(directory, runAPIResponseFileName)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return WorkflowRun{}, nil
	}
	data, err := regradeReadEvidence(path, maxGraderPayloadBytes)
	if err != nil {
		return WorkflowRun{}, err
	}
	run, err := parseWorkflowRunAPIResponse(data)
	if err != nil {
		return WorkflowRun{}, fmt.Errorf("invalid cached Actions metadata %s: %w", path, err)
	}
	if run.DatabaseID <= 0 {
		return WorkflowRun{}, fmt.Errorf("cached Actions metadata %s has no positive run identity", path)
	}
	return run, nil
}

func offlineGradingRunDataMetadata(entry RunData) (WorkflowRun, error) {
	attempt := 0
	if entry.RunAttempt != "" {
		var err error
		attempt, err = strconv.Atoi(entry.RunAttempt)
		if err != nil || attempt <= 0 {
			return WorkflowRun{}, fmt.Errorf("run %d has invalid recorded attempt %q", entry.RunID, entry.RunAttempt)
		}
	}
	return WorkflowRun{
		DatabaseID: entry.RunID, Number: entry.Number, URL: entry.URL,
		Repository: entry.Repository, Attempt: attempt, WorkflowName: entry.WorkflowName,
		WorkflowPath: entry.WorkflowPath, Status: entry.Status, Conclusion: entry.Conclusion,
		CreatedAt: entry.CreatedAt, StartedAt: entry.StartedAt, UpdatedAt: entry.UpdatedAt,
		Event: entry.Event, HeadBranch: entry.Branch, HeadSha: entry.HeadSHA,
		DisplayTitle: entry.DisplayTitle, Actor: entry.Actor, TokenUsage: entry.TokenUsage,
		Turns: entry.Turns, ErrorCount: entry.ErrorCount, WarningCount: entry.WarningCount,
		ActionMinutes: entry.ActionMinutes,
	}, nil
}

// Identity remains compatible with Harbor's key, but its legacy attempt default
// is not evidence that an unrecorded attempt was actually attempt one.
func regradeIdentityMissing(processed ProcessedRun) []string {
	missing := make([]string, 0)
	run := processed.Run
	parsed, err := url.Parse(run.URL)
	if err != nil {
		parsed = nil
	}
	repository := run.Repository
	if repository == "" && processed.AwContext != nil {
		repository = processed.AwContext.Repo
	}
	if parsed == nil || parsed.Host == "" {
		missing = append(missing, "host")
	} else if repository == "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 {
			repository = strings.Join(parts[:2], "/")
		}
	}
	if repository == "" {
		missing = append(missing, "repository")
	}
	if run.DatabaseID <= 0 {
		missing = append(missing, "run_id")
	}
	if run.Attempt <= 0 {
		missing = append(missing, "attempt")
	}
	return missing
}

func regradeNativeHarborBundle(directory string) (bool, error) {
	configPath, resultPath := filepath.Join(directory, "config.json"), filepath.Join(directory, "result.json")
	for _, path := range []string{configPath, resultPath} {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	config, err := regradeNativeHarborMarker(configPath)
	if err != nil {
		return false, err
	}
	result, err := regradeNativeHarborMarker(resultPath)
	if err != nil {
		return false, err
	}
	trial := config["task"] != nil && config["agent"] != nil && result["trial_name"] != nil &&
		(result["task_name"] != nil || result["verifier_result"] != nil)
	job := config["agents"] != nil && (config["tasks"] != nil || config["datasets"] != nil) &&
		result["stats"] != nil && (result["n_total_trials"] != nil || result["trial_results"] != nil)
	return trial || job, nil
}

func regradeNativeHarborMarker(path string) (map[string]json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("regrading bundle metadata must be a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	var marker map[string]json.RawMessage
	if len(data) > 1024*1024 {
		return nil, fmt.Errorf("bundle metadata %s exceeds the 1 MiB source-validation limit", path)
	}
	if json.Unmarshal(data, &marker) != nil {
		return nil, nil
	}
	return marker, nil
}
