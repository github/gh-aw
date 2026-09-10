package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

type cachedLogsRuns map[int64]RunData

const cachedLogsJSONLSchemaVersion = 2

const (
	cachedLogsJSONLKindRun          = "run"
	cachedLogsJSONLKindWorkflowRuns = "workflow_runs"
	cachedLogsJSONLKindRateLimit    = "github_api_rate_limit"
)

type cachedWorkflowRunsRequest struct {
	Host       string   `json:"host"`
	Repository string   `json:"repository"`
	Args       []string `json:"args"`
}

type cachedLogsJSONLRecord struct {
	SchemaVersion int                        `json:"schema_version"`
	Kind          string                     `json:"kind,omitempty"`
	Run           *RunData                   `json:"run,omitempty"`
	Request       *cachedWorkflowRunsRequest `json:"request,omitempty"`
	Payload       json.RawMessage            `json:"payload,omitempty"`
	RateLimit     *GitHubAPIRateLimitReport  `json:"rate_limit,omitempty"`
}

type cachedLogsJSONLCache struct {
	runs             cachedLogsRuns
	workflowRunLists map[string]json.RawMessage
}

func loadCachedLogsJSONL(path string) (*cachedLogsJSONLCache, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cached logs JSONL file not found: "+path))
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read cached logs JSONL: %w", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found cached logs JSONL file: "+path))
	lines := bytes.Split(data, []byte{'\n'})
	cache := &cachedLogsJSONLCache{
		runs:             make(cachedLogsRuns, len(lines)),
		workflowRunLists: make(map[string]json.RawMessage),
	}
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record cachedLogsJSONLRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if index == len(lines)-1 {
				logsCacheLog.Printf("Ignoring incomplete final cached logs JSONL record: %v", err)
				break
			}
			return nil, fmt.Errorf("failed to parse cached logs JSONL record %d: %w", index+1, err)
		}
		if err := cache.addRecord(record, index+1); err != nil {
			return nil, err
		}
	}
	logsCacheLog.Printf("Loaded %d run records and %d workflow run lists from cached logs JSONL", len(cache.runs), len(cache.workflowRunLists))
	return cache, nil
}

func (cache *cachedLogsJSONLCache) addRecord(record cachedLogsJSONLRecord, recordNumber int) error {
	if record.SchemaVersion != cachedLogsJSONLSchemaVersion {
		logsCacheLog.Printf("Ignoring incompatible cached logs JSONL record: record=%d, schema_version=%d", recordNumber, record.SchemaVersion)
		return nil
	}
	switch record.Kind {
	case cachedLogsJSONLKindWorkflowRuns:
		if record.Request == nil || len(record.Payload) == 0 {
			return nil
		}
		// Validate the cached payload shape while retaining its complete raw JSON.
		var runs []WorkflowRun
		if err := json.Unmarshal(record.Payload, &runs); err != nil {
			return fmt.Errorf("failed to parse cached workflow runs payload in record %d: %w", recordNumber, err)
		}
		if runs == nil {
			return fmt.Errorf("failed to parse cached workflow runs payload in record %d: expected an array", recordNumber)
		}
		key, err := record.Request.key()
		if err != nil {
			return fmt.Errorf("failed to parse cached workflow runs request in record %d: %w", recordNumber, err)
		}
		cache.workflowRunLists[key] = append(json.RawMessage(nil), record.Payload...)
		return nil
	case cachedLogsJSONLKindRateLimit:
		return nil
	case cachedLogsJSONLKindRun:
	default:
		return nil
	}
	if record.Run == nil {
		return nil
	}
	run := *record.Run
	if err := normalizeCachedLogRun(&run); err != nil {
		return err
	}
	if run.RunID != 0 {
		cache.runs[run.RunID] = run
	}
	return nil
}

type cachedLogsJSONLWriter struct {
	path string
	mu   sync.Mutex
}

func newCachedLogsJSONLWriter(path string) *cachedLogsJSONLWriter {
	if path == "" {
		return nil
	}
	return &cachedLogsJSONLWriter{path: path}
}

func (w *cachedLogsJSONLWriter) Append(run ProcessedRun) error {
	if w == nil {
		return nil
	}
	logsData := buildLogsData([]ProcessedRun{run}, "", nil)
	if len(logsData.Runs) != 1 {
		return errors.New("failed to build cached logs JSONL record")
	}
	runData := logsData.Runs[0]
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRun,
		Run:           &runData,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached logs JSONL record: %w", err)
	}
	return w.appendRecord(record)
}

func (w *cachedLogsJSONLWriter) AppendWorkflowRuns(request cachedWorkflowRunsRequest, payload []byte) error {
	if w == nil {
		return nil
	}
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindWorkflowRuns,
		Request:       &request,
		Payload:       append(json.RawMessage(nil), payload...),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached workflow runs JSONL record: %w", err)
	}
	return w.appendRecord(record)
}

func (w *cachedLogsJSONLWriter) AppendRateLimit(report GitHubAPIRateLimitReport) error {
	if w == nil || (report.Start == nil && report.End == nil) {
		return nil
	}
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRateLimit,
		RateLimit:     &report,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached GitHub API rate limit JSONL record: %w", err)
	}
	return w.appendRecord(record)
}

func (w *cachedLogsJSONLWriter) appendRecord(record []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, record); err != nil {
		return fmt.Errorf("failed to encode cached logs JSONL record: %w", err)
	}
	record = append(compact.Bytes(), '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create cached logs JSONL directory: %w", err)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, constants.FilePermSensitive)
	if err != nil {
		return fmt.Errorf("failed to open cached logs JSONL: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(record); err != nil {
		return fmt.Errorf("failed to append cached logs JSONL: %w", err)
	}
	return nil
}

func (request cachedWorkflowRunsRequest) key() (string, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to build cached workflow runs request key: %w", err)
	}
	return string(data), nil
}

func (cache *cachedLogsJSONLCache) lookupWorkflowRuns(request cachedWorkflowRunsRequest) (json.RawMessage, bool) {
	if cache == nil {
		return nil, false
	}
	key, err := request.key()
	if err != nil {
		return nil, false
	}
	payload, ok := cache.workflowRunLists[key]
	return payload, ok
}

func (runs cachedLogsRuns) lookup(run WorkflowRun, filters runFilterOpts) (RunData, bool) {
	cached, ok := runs[run.DatabaseID]
	if !ok {
		return RunData{}, false
	}
	if cached.Status != "completed" || run.Status != "completed" || cached.Conclusion != run.Conclusion {
		return RunData{}, false
	}
	if cached.RunAttempt == "" || run.Attempt <= 0 || cached.RunAttempt != strconv.Itoa(run.Attempt) {
		return RunData{}, false
	}
	if cached.UpdatedAt.IsZero() || run.UpdatedAt.IsZero() || !cached.UpdatedAt.Equal(run.UpdatedAt) {
		return RunData{}, false
	}
	if cached.Repository == "" || run.Repository == "" || !strings.EqualFold(cached.Repository, run.Repository) {
		return RunData{}, false
	}
	if filters.engine != "" &&
		!strings.EqualFold(filters.engine, cached.EngineID) &&
		!strings.EqualFold(filters.engine, cached.Engine) &&
		!strings.EqualFold(filters.engine, cached.Agent) {
		return RunData{}, false
	}
	// Compact logs JSON does not retain enough per-run evidence to safely
	// re-evaluate these artifact-dependent filters.
	if filters.runtime != "" || filters.noStaged || filters.firewallOnly || filters.noFirewall ||
		filters.safeOutputType != "" || filters.filteredIntegrity || filters.evalsOnly || filters.gradersOnly {
		return RunData{}, false
	}
	return cached, true
}

func normalizeCachedLogRun(run *RunData) error {
	if run.RunAttempt == "" {
		return nil
	}
	attempt, err := strconv.Atoi(run.RunAttempt)
	if err != nil || attempt <= 0 {
		return fmt.Errorf("failed to parse cached logs JSONL: invalid run_attempt for run %d", run.RunID)
	}
	run.RunAttempt = strconv.Itoa(attempt)
	return nil
}

func cachedJSONLCanSatisfy(artifactFilter []string, parse, audit, train, toolGraph bool) bool {
	return isUsageOnlyArtifactFilter(artifactFilter) && !parse && !audit && !train && !toolGraph
}

func processedRunFromCachedData(data RunData) ProcessedRun {
	return ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:       data.RunID,
			Number:           data.Number,
			URL:              data.URL,
			Status:           data.Status,
			Conclusion:       data.Conclusion,
			WorkflowName:     data.WorkflowName,
			WorkflowPath:     data.WorkflowPath,
			CreatedAt:        data.CreatedAt,
			StartedAt:        data.StartedAt,
			UpdatedAt:        data.UpdatedAt,
			Event:            data.Event,
			HeadBranch:       data.Branch,
			HeadSha:          data.HeadSHA,
			DisplayTitle:     data.DisplayTitle,
			Repository:       data.Repository,
			Actor:            data.Actor,
			Duration:         parseDurationString(data.Duration),
			ActionMinutes:    data.ActionMinutes,
			TokenUsage:       data.TokenUsage,
			Turns:            data.Turns,
			ErrorCount:       data.ErrorCount,
			WarningCount:     data.WarningCount,
			MissingToolCount: data.MissingToolCount,
			MissingDataCount: data.MissingDataCount,
			SafeItemsCount:   data.SafeItemsCount,
		},
		AwContext:           data.AwContext,
		TaskDomain:          data.TaskDomain,
		BehaviorFingerprint: data.BehaviorFingerprint,
		AgenticAssessments:  data.AgenticAssessments,
		TokenUsage:          data.TokenUsageSummary,
		WorkingSet:          data.WorkingSet,
		cachedData:          &data,
	}
}
