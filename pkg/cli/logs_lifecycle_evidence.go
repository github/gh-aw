package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/github/gh-aw/pkg/repoutil"
)

var errLifecycleEvidenceIncomplete = errors.New("safe-output session collection is incomplete")

// Unlike optional aw_info metadata, a supplied execution identity cannot be
// empty, zero, or replaced by a conflicting occurrence of the same field.
type lifecycleSessionNumber int64

func (n *lifecycleSessionNumber) UnmarshalJSON(raw []byte) error {
	var value NumericID
	if err := value.UnmarshalJSON(raw); err != nil {
		return err
	}
	if value <= 0 {
		return errors.New("lifecycle session identity must be a positive integer")
	}
	return n.merge(lifecycleSessionNumber(value))
}

func (n *lifecycleSessionNumber) merge(alias lifecycleSessionNumber) error {
	if *n == 0 {
		*n = alias
	} else if alias != 0 && *n != alias {
		return errors.New("conflicting lifecycle session identity values")
	}
	return nil
}

func nameForLifecycleInvocation(dir, root string) string {
	if dir == root {
		return "safe-outputs"
	}
	return logsOutcomeArtifactName(dir)
}

func readLifecycleItems(ctx context.Context, run ProcessedRun) ([]CreatedItemReport, bool, error) {
	items, found, err := readLogsOutcomeItems(run)
	if err != nil {
		return nil, false, err
	}
	sessionItems, sessionFound, err := readSessionLifecycleItems(ctx, run.Run)
	if err != nil && !errors.Is(err, errLifecycleEvidenceIncomplete) {
		return nil, false, err
	}
	for _, item := range sessionItems {
		if !found || !strings.HasSuffix(item.InvocationID, "safe-output-items.jsonl") {
			if item.InvocationID == "safe-output-items.jsonl" {
				item.InvocationID = "safe-outputs"
			}
			items = append(items, item)
		}
	}
	found = found || sessionFound
	var delivered []CreatedItemReport
	for _, item := range items {
		if item.Status == "" || item.Status == "success" {
			delivered = append(delivered, item)
		}
	}
	return delivered, found, err
}

func readSessionLifecycleItems(ctx context.Context, run WorkflowRun) (items []CreatedItemReport, found bool, returnErr error) { //nolint:largefunc // One streaming state machine validates format, identity, coverage, and result records.
	path, err := findUnifiedSessionPath(run.LogsPath)
	if err != nil || path == "" {
		return nil, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxAuditSearchSessionBytes {
		return nil, false, errors.New("lifecycle session must be a regular file no larger than 64 MiB")
	}
	reader := bufio.NewReader(file)
	headerSeen, identitySeen, safeOutputsSkipped := false, false, false
	incomplete := false
	occurrences := make(map[string]map[[32]byte]int)
	for line := 1; ; line++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		raw, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, false, errors.New("oversized lifecycle session record")
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			var event auditSearchSessionEvent
			if err := json.Unmarshal(raw, &event); err != nil {
				return nil, false, fmt.Errorf("invalid lifecycle session record %d: %w", line, err)
			}
			if !headerSeen {
				header := unifiedDetectionEvent{Type: event.Type, Data: event.Data}
				header.Provenance.Component = event.Provenance.Component
				if err := validateUnifiedDetectionHeader(header); err != nil {
					return nil, false, err
				}
				headerSeen = true
			} else if event.Type == "session.format" {
				return nil, false, errors.New("lifecycle session contains repeated format headers")
			} else if event.Type == "session.collection_warning" {
				var warning struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(event.Data, &warning); err != nil {
					return nil, false, err
				}
				incomplete = incomplete || strings.HasPrefix(filepath.Base(warning.Path), "safe-output")
			} else if event.Type == "workflow.info" && event.Provenance.Component == "workflow" {
				var identity struct {
					RunID      lifecycleSessionNumber `json:"run_id"`
					RunIDCamel lifecycleSessionNumber `json:"runId"`
					RunAttempt lifecycleSessionNumber `json:"runAttempt"`
					Attempt    lifecycleSessionNumber `json:"run_attempt"`
					Repository string                 `json:"repository"`
					ServerURL  string                 `json:"serverUrl"`
				}
				if err := json.Unmarshal(event.Data, &identity); err != nil {
					return nil, false, fmt.Errorf("invalid lifecycle session identity on line %d: %w", line, err)
				}
				if err := identity.RunID.merge(identity.RunIDCamel); err != nil {
					return nil, false, fmt.Errorf("run ID: %w", err)
				}
				if identity.RunID == 0 || int64(identity.RunID) != run.DatabaseID {
					return nil, false, errors.New("lifecycle session run identity does not match selected run")
				}
				if err := identity.RunAttempt.merge(identity.Attempt); err != nil {
					return nil, false, fmt.Errorf("run attempt: %w", err)
				}
				if run.Attempt > 0 && int64(identity.RunAttempt) != int64(run.Attempt) {
					return nil, false, errors.New("lifecycle session attempt does not match selected run")
				}
				repository, _ := repoutil.NormalizeRepoForAPI(logsOutcomeRepository(run))
				if identity.Repository != "" && !strings.EqualFold(identity.Repository, repository) {
					return nil, false, errors.New("lifecycle session repository does not match selected run")
				}
				if identity.ServerURL != "" && !strings.EqualFold(identity.ServerURL, "https://"+outcomeAPIHostForRepo(logsOutcomeRepository(run))) {
					return nil, false, errors.New("lifecycle session host does not match selected run")
				}
				identitySeen = true
			} else if event.Type == "workflow.job_result" && event.Provenance.Component == "workflow" {
				var job struct {
					ID     string `json:"jobId"`
					Result string `json:"result"`
				}
				if err := json.Unmarshal(event.Data, &job); err != nil {
					return nil, false, err
				}
				safeOutputsSkipped = safeOutputsSkipped || job.ID == "safe_outputs" && job.Result == "skipped"
			} else if event.Type == "safe_output.result" && event.Provenance.Component == "safe_output" && event.Provenance.Phase == "safe_outputs" {
				var item CreatedItemReport
				if err := json.Unmarshal(event.Data, &item); err != nil {
					return nil, false, err
				}
				if item.Type == "" {
					return nil, false, errors.New("lifecycle result has no safe-output type")
				}
				if item.Timestamp == "" && len(event.Timestamp) > 0 {
					if err := json.Unmarshal(event.Timestamp, &item.Timestamp); err != nil {
						return nil, false, err
					}
				}
				item.InvocationID = event.Provenance.Path
				item.EvidenceSource = "unified-session"
				if occurrences[item.InvocationID] == nil {
					occurrences[item.InvocationID] = make(map[[32]byte]int)
				}
				item.EvidenceID = outcomeItemID(item, occurrences[item.InvocationID])
				items = append(items, item)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, false, readErr
			}
			break
		}
	}
	if !headerSeen || !identitySeen {
		return nil, false, errors.New("lifecycle session is missing its format or execution identity")
	}
	if incomplete {
		return items, len(items) > 0 || safeOutputsSkipped, errLifecycleEvidenceIncomplete
	}
	return items, len(items) > 0 || safeOutputsSkipped, nil
}

func lifecycleFromItem(run WorkflowRun, item CreatedItemReport, repository, host string) (lifecycle.Record, error) {
	at, err := time.Parse(time.RFC3339Nano, item.Timestamp)
	if err != nil {
		return lifecycle.Record{}, fmt.Errorf("safe-output %s has no valid delivery timestamp", item.Type)
	}
	targetRepository := resolveItemRepo(item, repository)
	if !sameOutcomeAPIHost(targetRepository, host) {
		return lifecycle.Record{}, errors.New("item repository host does not match workflow run host")
	}
	sourceRepository, _ := repoutil.NormalizeRepoForAPI(repository)
	targetRepository, _ = repoutil.NormalizeRepoForAPI(targetRepository)
	targetURL := item.URL
	if number := resolveItemNumber(item); number > 0 {
		kind := ""
		switch item.Type {
		case "create_issue":
			kind = "issues"
		case "create_pull_request":
			kind = "pull"
		}
		if kind != "" {
			expected := fmt.Sprintf("https://%s/%s/%s/%d", host, targetRepository, kind, number)
			if targetURL != "" && targetURL != expected {
				return lifecycle.Record{}, errors.New("delivered proposal URL conflicts with its repository or number")
			}
			targetURL = expected
		}
	}
	invocation := item.InvocationID
	if invocation == "" {
		invocation = "safe-outputs"
	}
	attempt := run.Attempt
	source := item.EvidenceSource
	if source == "" {
		source = "safe-output-manifest"
	}
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: strings.ToLower(host), Repository: sourceRepository, Workflow: run.WorkflowPath,
		RunID: run.DatabaseID, RunAttempt: attempt, InvocationID: invocation, EvidenceID: item.EvidenceID,
		Type: item.Type, TargetURL: targetURL, TargetRepository: targetRepository,
	}, at, time.Now(), source)
	if err != nil {
		return record, err
	}
	record.Execution = retainedLifecycleExecution(item)
	return record, nil
}

func lifecycleStorePath(runDir string) string {
	dir := filepath.Clean(runDir)
	if _, ok := logsRunIDFromPath(filepath.Dir(dir)); ok && strings.HasPrefix(filepath.Base(dir), "attempt-") {
		dir = filepath.Dir(dir)
	}
	if _, ok := logsRunIDFromPath(dir); ok {
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, "lifecycle.jsonl")
}
