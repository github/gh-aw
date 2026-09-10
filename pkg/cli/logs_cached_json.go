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

const cachedLogsJSONLSchemaVersion = 1

type cachedLogsJSONLRecord struct {
	SchemaVersion int     `json:"schema_version"`
	Run           RunData `json:"run"`
}

func loadCachedLogsJSONL(path string) (cachedLogsRuns, error) {
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
	runs := make(cachedLogsRuns, len(lines))
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
		if record.SchemaVersion != cachedLogsJSONLSchemaVersion {
			logsCacheLog.Printf("Ignoring incompatible cached logs JSONL record: record=%d, schema_version=%d", index+1, record.SchemaVersion)
			continue
		}
		run := record.Run
		if err := normalizeCachedLogRun(&run); err != nil {
			return nil, err
		}
		if run.RunID != 0 {
			runs[run.RunID] = run
		}
	}
	logsCacheLog.Printf("Loaded %d run records from cached logs JSONL", len(runs))
	return runs, nil
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
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Run:           logsData.Runs[0],
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached logs JSONL record: %w", err)
	}
	record = append(record, '\n')

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
