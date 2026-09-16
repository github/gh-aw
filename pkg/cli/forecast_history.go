package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type forecastWorkflowTarget struct {
	repository string
	name       string
	path       string
}

type forecastJSONLHistory struct {
	files      []string
	runs       map[int64]cachedLogsJSONLRunData
	duplicates int
	earliest   time.Time
	latest     time.Time
}

func loadForecastJSONLHistory(inputs []string) (*forecastJSONLHistory, error) {
	files, err := resolveForecastJSONLInputs(inputs)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no JSONL history files matched --logs-jsonl")
	}

	history := &forecastJSONLHistory{files: files, runs: make(map[int64]cachedLogsJSONLRunData)}
	for _, path := range files {
		_, err := visitCachedLogsJSONLRecords(path, func(record cachedLogsJSONLRecord, _ int) error {
			if record.SchemaVersion != cachedLogsJSONLSchemaVersion ||
				record.Kind != cachedLogsJSONLKindRun || record.Run == nil {
				return nil
			}
			run := *record.Run
			if err := normalizeCachedLogRun(&run.RunData); err != nil {
				return fmt.Errorf("invalid run record in %s: %w", path, err)
			}
			if run.RunID == 0 {
				return nil
			}
			if _, exists := history.runs[run.RunID]; exists {
				history.duplicates++
			}
			history.runs[run.RunID] = run
			observedAt := forecastJSONLRunTime(run.RunData)
			if !observedAt.IsZero() {
				if history.earliest.IsZero() || observedAt.Before(history.earliest) {
					history.earliest = observedAt
				}
				if history.latest.IsZero() || observedAt.After(history.latest) {
					history.latest = observedAt
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(history.runs) == 0 {
		return nil, fmt.Errorf("no compatible schema version %d run records found in JSONL history", cachedLogsJSONLSchemaVersion)
	}
	return history, nil
}

func resolveForecastJSONLInputs(inputs []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string
	add := func(path string) error {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return fmt.Errorf("failed to resolve JSONL history input %q: %w", path, err)
		}
		if info.IsDir() {
			return filepath.WalkDir(absolute, func(candidate string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && strings.EqualFold(filepath.Ext(candidate), ".jsonl") {
					if _, exists := seen[candidate]; !exists {
						seen[candidate] = struct{}{}
						files = append(files, candidate)
					}
				}
				return nil
			})
		}
		if _, exists := seen[absolute]; !exists {
			seen[absolute] = struct{}{}
			files = append(files, absolute)
		}
		return nil
	}

	for _, input := range inputs {
		if strings.ContainsAny(input, "*?[") {
			matches, err := filepath.Glob(input)
			if err != nil {
				return nil, fmt.Errorf("invalid JSONL history glob %q: %w", input, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("JSONL history glob %q matched no files", input)
			}
			for _, match := range matches {
				if err := add(match); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := add(input); err != nil {
			return nil, err
		}
	}
	slices.Sort(files)
	return files, nil
}

func (history *forecastJSONLHistory) targets(ids []string, repo string) ([]forecastWorkflowTarget, error) {
	byKey := make(map[string]forecastWorkflowTarget)
	for _, run := range history.runs {
		if repo != "" && !strings.EqualFold(run.Repository, repo) {
			continue
		}
		if run.WorkflowName == "" {
			continue
		}
		target := forecastWorkflowTarget{repository: run.Repository, name: run.WorkflowName, path: run.WorkflowPath}
		byKey[forecastTargetKey(target)] = target
	}

	targets := make([]forecastWorkflowTarget, 0, len(byKey))
	for _, target := range byKey {
		if len(ids) == 0 || slices.ContainsFunc(ids, func(id string) bool { return forecastTargetMatches(target, id) }) {
			targets = append(targets, target)
		}
	}
	if len(ids) > 0 {
		for _, id := range ids {
			if !slices.ContainsFunc(targets, func(target forecastWorkflowTarget) bool { return forecastTargetMatches(target, id) }) {
				return nil, fmt.Errorf("workflow %q not found in JSONL history", id)
			}
		}
	}
	slices.SortFunc(targets, func(a, b forecastWorkflowTarget) int {
		if result := strings.Compare(a.repository, b.repository); result != 0 {
			return result
		}
		return strings.Compare(a.name, b.name)
	})
	return targets, nil
}

func forecastTargetMatches(target forecastWorkflowTarget, id string) bool {
	if strings.EqualFold(target.name, id) || strings.EqualFold(extractWorkflowIDFromName(target.name), id) {
		return true
	}
	base := filepath.Base(target.path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSuffix(base, ".lock")
	return strings.EqualFold(base, id)
}

func (history *forecastJSONLHistory) runsFor(target forecastWorkflowTarget, start, end time.Time, limit int) ([]WorkflowRun, map[int64]float64, bool, int) {
	type observation struct {
		run WorkflowRun
		aic float64
	}
	observations := make([]observation, 0)
	for _, cached := range history.runs {
		if !forecastRunMatchesTarget(cached.RunData, target) {
			continue
		}
		observedAt := forecastJSONLRunTime(cached.RunData)
		if observedAt.IsZero() || observedAt.Before(start) || (!end.IsZero() && !observedAt.Before(end)) {
			continue
		}
		duration, _ := time.ParseDuration(cached.Duration)
		startedAt := forecastJSONLRunTime(cached.RunData)
		observations = append(observations, observation{
			run: WorkflowRun{
				DatabaseID:   cached.RunID,
				URL:          cached.URL,
				Status:       cached.Status,
				Conclusion:   cached.Conclusion,
				WorkflowName: cached.WorkflowName,
				WorkflowPath: cached.WorkflowPath,
				Repository:   cached.Repository,
				CreatedAt:    cached.CreatedAt,
				StartedAt:    startedAt,
				UpdatedAt:    cached.UpdatedAt,
				Event:        cached.Event,
				HeadBranch:   cached.Branch,
				HeadSha:      cached.HeadSHA,
				DisplayTitle: cached.DisplayTitle,
				Duration:     duration,
			},
			aic: cached.AIC,
		})
	}
	slices.SortFunc(observations, func(a, b observation) int {
		return b.run.StartedAt.Compare(a.run.StartedAt)
	})
	truncated := limit > 0 && len(observations) > limit
	observedCount := len(observations)
	if truncated {
		observations = observations[:limit]
	}
	runs := make([]WorkflowRun, 0, len(observations))
	aic := make(map[int64]float64, len(observations))
	for _, observation := range observations {
		runs = append(runs, observation.run)
		aic[observation.run.DatabaseID] = observation.aic
	}
	return filterForecastSampleRuns(runs, start.Format("2006-01-02"), 0), aic, truncated, observedCount
}

func forecastTargetKey(target forecastWorkflowTarget) string {
	return strings.ToLower(target.repository) + "\x00" + strings.ToLower(target.name) + "\x00" + strings.ToLower(target.path)
}

func forecastRunMatchesTarget(run RunData, target forecastWorkflowTarget) bool {
	if !strings.EqualFold(run.Repository, target.repository) || !strings.EqualFold(run.WorkflowName, target.name) {
		return false
	}
	if run.WorkflowPath == "" || target.path == "" {
		return true
	}
	return strings.EqualFold(run.WorkflowPath, target.path)
}

func forecastJSONLRunTime(run RunData) time.Time {
	if !run.StartedAt.IsZero() {
		return run.StartedAt
	}
	return run.CreatedAt
}

func (history *forecastJSONLHistory) provenance(start time.Time, sampleTruncated bool) ForecastHistoryProvenance {
	provenance := ForecastHistoryProvenance{
		Source:            "jsonl",
		JSONLFiles:        append([]string(nil), history.files...),
		ObservedRuns:      len(history.runs),
		DuplicateRuns:     history.duplicates,
		LowerBound:        true,
		PossiblyTruncated: sampleTruncated || history.earliest.IsZero() || history.earliest.After(start),
	}
	if !history.earliest.IsZero() {
		provenance.EarliestObservation = history.earliest.UTC().Format(time.RFC3339)
	}
	if !history.latest.IsZero() {
		provenance.LatestObservation = history.latest.UTC().Format(time.RFC3339)
	}
	switch {
	case sampleTruncated:
		provenance.TruncationReason = "the per-workflow sample limit excluded matching observations"
	case history.earliest.IsZero():
		provenance.TruncationReason = "observation timestamps are unavailable"
	case history.earliest.After(start):
		provenance.TruncationReason = "the earliest observation is newer than the requested history window"
	}
	return provenance
}

func forecastWorkflowFromJSONL(ctx context.Context, target forecastWorkflowTarget, start, end time.Time, config ForecastConfig, periodDays int) (ForecastWorkflowResult, bool, error) {
	result := ForecastWorkflowResult{
		WorkflowID:   extractWorkflowIDFromName(target.name),
		Repository:   target.repository,
		WorkflowPath: target.path,
		Period:       config.Period,
		HistoryDays:  config.Days,
	}
	meta := loadWorkflowMeta(target.name, config.Verbose)
	result.ActiveTriggers = meta.activeTriggers
	result.ConcurrencyLimit = meta.concurrencyLimit
	result.ExperimentVariants = meta.variants
	result.Engines = meta.engines

	runs, aicMap, truncated, observedRunCount := config.history.runsFor(target, start, end, config.SampleSize)
	populateWorkflowRunAPIForecast(&result, observedRunCount, config.Days, periodDays)
	stats := collectForecastRunStats(runs, aicMap, target.name)
	result.RunSamples = stats.samples
	result.SampledRuns = len(stats.aicObservations)
	if result.SampledRuns == 0 {
		return result, truncated, nil
	}
	populateForecastProjection(&result, stats, config.Days, periodDays)
	result.ExperimentVariants = computeVariantFractions(result.ExperimentVariants, runs)
	if config.EvalMode {
		result.Evaluation = evaluateForecastFromJSONL(ctx, target, result, end, time.Now(), config)
	}
	return result, truncated, nil
}

func evaluateForecastFromJSONL(_ context.Context, target forecastWorkflowTarget, forecast ForecastWorkflowResult, start, end time.Time, config ForecastConfig) *ForecastEvaluation {
	evaluation := &ForecastEvaluation{
		TrainingStartDate: start.AddDate(0, 0, -config.Days).Format("2006-01-02"),
		TrainingEndDate:   start.Format("2006-01-02"),
		ValidationEndDate: end.Format("2006-01-02"),
	}
	runs, aic, _, _ := config.history.runsFor(target, start, end.Add(time.Nanosecond), 0)
	for _, run := range runs {
		if !isCompletedDispatchedRun(run) {
			continue
		}
		evaluation.ActualRuns++
		evaluation.ActualAIC += aic[run.DatabaseID]
	}
	applyForecastEvaluationMetrics(evaluation, forecast)
	return evaluation
}
