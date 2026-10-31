package cli

import (
	"bytes"
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

func regradeValidateSessionIdentity(events []json.RawMessage, source string, expected WorkflowRun) error {
	for _, raw := range events {
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &event); err != nil || event.Type != "workflow.info" {
			continue
		}
		if err := regradeValidateRecordedIdentity(expected, event.Data); err != nil {
			return fmt.Errorf("workflow.info conflicts with selected run identity: %w", err)
		}
	}
	for _, name := range []string{"aw_info.json", "runtime/aw_info.json", "activation/aw_info.json", "usage/aw_info.json", "agent/aw_info.json"} {
		path := filepath.Join(source, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		data, err := regradeReadEvidence(path, maxGraderPayloadBytes)
		if err != nil {
			return err
		}
		if err := regradeValidateRecordedIdentity(expected, data); err != nil {
			return fmt.Errorf("%s conflicts with selected run identity: %w", name, err)
		}
	}
	return nil
}

func regradeValidateRecordedIdentity(expected WorkflowRun, raw json.RawMessage) error {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		if err == nil {
			err = errors.New("expected a JSON object")
		}
		return fmt.Errorf("invalid identity metadata: %w", err)
	}
	runID, err := regradeIdentityInt64(metadata, "run_id", "runId", "databaseId", "id")
	if err != nil {
		return err
	}
	attempt, err := regradeIdentityInt64(metadata, "run_attempt", "runAttempt", "attempt")
	if err != nil {
		return err
	}
	if int64(int(attempt)) != attempt {
		return errors.New("recorded run attempt is outside the supported range")
	}
	repository, err := regradeIdentityString(metadata, "repository", "repo")
	if err != nil {
		return err
	}
	runURL, err := regradeIdentityString(metadata, "url", "html_url")
	if err != nil {
		return err
	}
	host, err := regradeIdentityString(metadata, "server_url", "serverUrl", "host")
	if err != nil {
		return err
	}
	actual := WorkflowRun{
		DatabaseID: runID,
		Attempt:    int(attempt),
		Repository: repository,
		URL:        runURL,
	}
	if err := validateOfflineRunIdentity(expected, actual); err != nil {
		return err
	}
	expectedHost, _, err := offlineRunIdentityParts(expected)
	if err != nil {
		return err
	}
	if host != "" {
		parsed, err := url.Parse(host)
		if err != nil {
			return fmt.Errorf("invalid recorded server host %q", host)
		}
		recordedHost := parsed.Host
		if recordedHost == "" {
			recordedHost = parsed.Path
		}
		recordedHost = strings.TrimSuffix(strings.ToLower(recordedHost), "/")
		if expectedHost != "" && !strings.EqualFold(expectedHost, recordedHost) {
			return fmt.Errorf("host metadata %q does not match selected host %q", recordedHost, expectedHost)
		}
	}
	return nil
}

func regradeIdentityString(metadata map[string]json.RawMessage, names ...string) (string, error) {
	for _, name := range names {
		raw, ok := metadata[name]
		if !ok || string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			if value = strings.TrimSpace(value); value != "" {
				return value, nil
			}
			continue
		}
		return "", fmt.Errorf("recorded identity field %s is not a string", name)
	}
	return "", nil
}

func regradeIdentityInt64(metadata map[string]json.RawMessage, names ...string) (int64, error) {
	for _, name := range names {
		raw, ok := metadata[name]
		if !ok || string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
		} else {
			var number json.Number
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&number); err != nil {
				return 0, fmt.Errorf("recorded identity field %s is not a number", name)
			}
			value = number.String()
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("recorded identity field %s must be a positive integer", name)
		}
		return parsed, nil
	}
	return 0, nil
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
