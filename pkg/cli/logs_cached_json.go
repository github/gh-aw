package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

type cachedLogsRuns map[int64]RunData

func loadCachedLogsJSON(path string) (cachedLogsRuns, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cached logs JSON file not found: "+path))
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read cached logs JSON: %w", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found cached logs JSON file: "+path))
	var logsData LogsData
	if err := json.Unmarshal(data, &logsData); err != nil {
		return nil, fmt.Errorf("failed to parse cached logs JSON: %w", err)
	}
	if logsData.Runs == nil {
		return nil, errors.New("failed to parse cached logs JSON: missing runs array")
	}
	runs := make(cachedLogsRuns, len(logsData.Runs))
	for _, run := range logsData.Runs {
		if err := normalizeCachedLogRun(&run); err != nil {
			return nil, err
		}
		if run.RunID != 0 {
			runs[run.RunID] = run
		}
	}
	logsCacheLog.Printf("Loaded %d run records from cached logs JSON", len(runs))
	return runs, nil
}

func writeCachedLogsJSON(path string, data LogsData, verbose bool) error {
	if path == "" {
		return nil
	}
	var output bytes.Buffer
	if err := renderLogsJSONToWriter(&output, data, verbose); err != nil {
		return fmt.Errorf("failed to render updated cached logs JSON: %w", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Writing cached logs JSON file: "+path))
	if err := os.WriteFile(path, output.Bytes(), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to update cached logs JSON: %w", err)
	}
	logsCacheLog.Printf("Updated cached logs JSON: path=%s", path)
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
		return fmt.Errorf("failed to parse cached logs JSON: invalid run_attempt for run %d", run.RunID)
	}
	run.RunAttempt = strconv.Itoa(attempt)
	return nil
}

func cachedJSONCanSatisfy(artifactFilter []string, parse, audit, train, toolGraph bool) bool {
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
