package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/sourcegraph/conc/pool"
)

const defaultExperimentOutcomeWindow = 72 * time.Hour
const minExperimentOutcomeDecisions = 20
const minExperimentOutcomeWindow = 24 * time.Hour
const maxExperimentOutcomeWindow = 30 * 24 * time.Hour

// OutcomeMetricCoverage counts assigned runs, not outputs. A partially observed
// run can contribute both a measurement and an unavailable/stale-pending count.
type OutcomeMetricCoverage struct {
	Immature     int `json:"immature"`
	StalePending int `json:"stale_pending"`
	Unavailable  int `json:"unavailable"`
	NoOp         int `json:"no_op"`
}

type outcomeRunData struct {
	Report          OutcomesData
	ExclusionReason string
}

type outcomeRunSource interface {
	Load(context.Context, string) outcomeRunData
}

type githubOutcomeRunSource struct {
	artifacts *githubGraderRunArtifactSource
}

func (s *githubOutcomeRunSource) Load(ctx context.Context, runIDText string) outcomeRunData {
	runID, reason := s.artifacts.eligibleRunID(ctx, runIDText)
	if reason != "" {
		return outcomeRunData{ExclusionReason: reason}
	}
	runDir := filepath.Join(s.artifacts.baseDir, strconv.FormatInt(runID, 10))
	if err := os.MkdirAll(runDir, constants.DirPermPublic); err != nil {
		return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	names, err := listRunArtifactNames(ctx, runID, s.artifacts.owner, s.artifacts.repo, s.artifacts.hostname, false)
	if err != nil {
		return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	var selected []string
	for _, name := range names {
		if artifactMatchesFilter(name, artifactSetArtifacts[ArtifactSetOutcomes]) {
			selected = append(selected, name)
		}
	}
	if len(selected) == 0 {
		return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	opts := downloadArtifactsOptions{
		runID: runID, outputDir: runDir, owner: s.artifacts.owner,
		repo: s.artifacts.repo, hostname: s.artifacts.hostname, failOnDownloadError: true,
	}
	if err := downloadArtifactsByName(ctx, opts, selected); err != nil {
		return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	// Preserve invocation directories: collectRunOutcomes understands immutable
	// manifests and their matching compact enrichment artifacts.
	run := ProcessedRun{Run: WorkflowRun{
		DatabaseID: runID, Repository: buildRepoFlag(s.artifacts.owner, s.artifacts.repo, s.artifacts.hostname),
		LogsPath: runDir,
	}}
	report, err := collectRunOutcomes(ctx, run, nil, false)
	if err != nil || report.EvalError != "" {
		return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	return outcomeRunData{Report: report}
}

func resolveOutcomeMetricReferences(configs map[string]*workflow.ExperimentConfig) (map[string]string, map[string]map[string]string, map[string]time.Duration, map[string]struct{}, error) {
	primary := make(map[string]string)
	guardrails := make(map[string]map[string]string)
	windows := make(map[string]time.Duration)
	automatic := make(map[string]struct{})
	for name, cfg := range configs {
		if cfg == nil {
			continue
		}
		refs := []string{cfg.Metric}
		for _, guardrail := range cfg.GuardrailMetrics {
			refs = append(refs, guardrail.Name)
		}
		for index, ref := range refs {
			kind, recognized := workflow.ParseExperimentMetricOutcomeReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, nil, nil, nil, fmt.Errorf("experiments.%s: unknown outcome metric %q. Expected outcome:accepted or outcome:zero_touch. Example: metric: outcome:accepted", name, ref)
			}
			if index == 0 {
				primary[name] = kind
			} else {
				if guardrails[name] == nil {
					guardrails[name] = make(map[string]string)
				}
				guardrails[name][ref] = kind
			}
			window := defaultExperimentOutcomeWindow
			if cfg.OutcomeWindow == "" || cfg.OutcomeWindow == "auto" {
				automatic[name] = struct{}{}
			} else {
				var err error
				window, err = time.ParseDuration(cfg.OutcomeWindow)
				if err != nil || window <= 0 {
					return nil, nil, nil, nil, fmt.Errorf("experiments.%s.outcome_window: invalid value %q. Expected \"auto\" or a positive Go duration. Example: outcome_window: 72h", name, cfg.OutcomeWindow)
				}
			}
			windows[name] = window
		}
	}
	return primary, guardrails, windows, automatic, nil
}

func outcomeRunMaturity(run ExperimentRunRecord, window time.Duration, now time.Time) string {
	if window == 0 {
		window = defaultExperimentOutcomeWindow
	}
	timestamp, err := time.Parse(time.RFC3339, run.Timestamp)
	if err != nil {
		return "unavailable"
	}
	if now.Sub(timestamp) < window {
		return "immature"
	}
	return ""
}

func loadOutcomeRunData(ctx context.Context, runs []ExperimentRunRecord, windows map[string]time.Duration, automatic map[string]struct{}, now time.Time, source outcomeRunSource) map[string]outcomeRunData {
	ctx = withOutcomeAPICache(ctx)
	runIDs := make(map[string]struct{})
	for _, run := range runs {
		for name, window := range windows {
			_, auto := automatic[name]
			if _, assigned := run.Assignments[name]; !assigned || !auto && outcomeRunMaturity(run, window, now) != "" {
				continue
			}
			runIDs[run.RunID] = struct{}{}
			break
		}
	}
	result := make(map[string]outcomeRunData, len(runIDs))
	var mu sync.Mutex
	p := pool.New().WithContext(ctx).WithMaxGoroutines(getMaxConcurrentDownloads())
	for runID := range runIDs {
		p.Go(func(ctx context.Context) error {
			data := source.Load(ctx, runID)
			mu.Lock()
			result[runID] = data
			mu.Unlock()
			return nil
		})
	}
	_ = p.Wait()
	return result
}

func loadOutcomeObservationSetsForAnalysis(details *ExperimentDetails, frontmatter experimentFrontmatterResult, repoOverride string) (map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, func(), error) {
	primary, guardrails, windows, automatic, err := resolveOutcomeMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil || len(windows) == 0 {
		return nil, nil, func() {}, err
	}
	cacheDir, err := os.MkdirTemp("", "gh-aw-experiment-outcomes-")
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("failed to prepare outcome artifact cache: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(cacheDir) }
	now := time.Now()
	source := &githubOutcomeRunSource{artifacts: newGitHubGraderRunArtifactSource(cacheDir, repoOverride)}
	data := loadOutcomeRunData(context.Background(), details.Runs, windows, automatic, now, source)
	if len(automatic) > 0 {
		learnedWindow := estimateOutcomeWindow(details.Runs, automatic, data, now)
		for name := range automatic {
			windows[name] = learnedWindow
		}
	}
	sets := buildOutcomeMetricObservationSets(details.Experiments, details.Runs, primary, windows, now, data)
	guardrailSets := make(map[string]map[string]*graderMetricObservationSet)
	for name, metrics := range guardrails {
		guardrailSets[name] = make(map[string]*graderMetricObservationSet)
		for ref, kind := range metrics {
			guardrailSets[name][ref] = buildOutcomeMetricObservationSets(details.Experiments, details.Runs, map[string]string{name: kind}, windows, now, data)[name]
		}
	}
	return sets, guardrailSets, cleanup, nil
}

type outcomeWindowObservation struct {
	duration time.Duration
	decided  bool
}

// estimateOutcomeWindow pools variant outputs and treats pending outputs as
// right-censored at their current age. If p95 is not observable, the upper cap
// is used rather than underestimating the maturity period.
func estimateOutcomeWindow(runs []ExperimentRunRecord, automatic map[string]struct{}, data map[string]outcomeRunData, now time.Time) time.Duration {
	observations, decided := collectOutcomeWindowObservations(runs, automatic, data, now)
	if decided < minExperimentOutcomeDecisions {
		return defaultExperimentOutcomeWindow
	}
	return kaplanMeierOutcomeWindowP95(observations)
}

func collectOutcomeWindowObservations(runs []ExperimentRunRecord, automatic map[string]struct{}, data map[string]outcomeRunData, now time.Time) ([]outcomeWindowObservation, int) {
	observations := make([]outcomeWindowObservation, 0)
	seenRuns := make(map[string]struct{})
	decided := 0
	for _, run := range runs {
		if _, seen := seenRuns[run.RunID]; seen {
			continue
		}
		include := false
		for name := range automatic {
			if _, assigned := run.Assignments[name]; assigned {
				include = true
				break
			}
		}
		if !include {
			continue
		}
		seenRuns[run.RunID] = struct{}{}
		runData, found := data[run.RunID]
		if !found || runData.ExclusionReason != "" || runData.Report.EvalError != "" {
			continue
		}
		for _, item := range runData.Report.Items {
			switch item.OutcomeStatus {
			case OutcomeStatusAccepted, OutcomeStatusRejected:
				if item.TimeToOutcomeHours <= 0 || math.IsNaN(item.TimeToOutcomeHours) || math.IsInf(item.TimeToOutcomeHours, 0) {
					continue
				}
				duration := maxExperimentOutcomeWindow
				if item.TimeToOutcomeHours < maxExperimentOutcomeWindow.Hours() {
					duration = time.Duration(item.TimeToOutcomeHours * float64(time.Hour))
				}
				if duration <= 0 {
					continue
				}
				observations = append(observations, outcomeWindowObservation{duration: duration, decided: true})
				decided++
			case OutcomeStatusPending:
				createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
				if err != nil {
					continue
				}
				duration := now.Sub(createdAt)
				if duration >= 0 {
					observations = append(observations, outcomeWindowObservation{duration: duration})
				}
			}
		}
	}
	return observations, decided
}

func kaplanMeierOutcomeWindowP95(observations []outcomeWindowObservation) time.Duration {
	slices.SortFunc(observations, func(a, b outcomeWindowObservation) int {
		switch {
		case a.duration < b.duration:
			return -1
		case a.duration > b.duration:
			return 1
		default:
			return 0
		}
	})
	atRisk := len(observations)
	survival := 1.0
	var currentDuration time.Duration
	groupSize, events := 0, 0
	hasGroup := false
	for _, observation := range observations {
		if hasGroup && observation.duration != currentDuration {
			if window, reached := applyOutcomeWindowKMGroup(currentDuration, events, groupSize, &atRisk, &survival); reached {
				return window
			}
			groupSize, events = 0, 0
		}
		currentDuration = observation.duration
		hasGroup = true
		groupSize++
		if observation.decided {
			events++
		}
	}
	if hasGroup {
		if window, reached := applyOutcomeWindowKMGroup(currentDuration, events, groupSize, &atRisk, &survival); reached {
			return window
		}
	}
	return maxExperimentOutcomeWindow
}

func applyOutcomeWindowKMGroup(duration time.Duration, events, groupSize int, atRisk *int, survival *float64) (time.Duration, bool) {
	if events > 0 && *atRisk > 0 {
		*survival *= 1 - float64(events)/float64(*atRisk)
		if *survival <= 0.05 {
			return clampExperimentOutcomeWindow(duration), true
		}
	}
	*atRisk -= groupSize
	return 0, false
}

func clampExperimentOutcomeWindow(window time.Duration) time.Duration {
	return min(max(window, minExperimentOutcomeWindow), maxExperimentOutcomeWindow)
}

func buildOutcomeMetricObservationSets(experiments []ExperimentVariantStats, runs []ExperimentRunRecord, refs map[string]string, windows map[string]time.Duration, now time.Time, data map[string]outcomeRunData) map[string]*graderMetricObservationSet {
	state := initializeGraderObservationSets(experiments, refs, nil)
	for name, set := range state.sets {
		set.MetricID = "outcome:" + refs[name]
		set.Coverage = make(map[string]*OutcomeMetricCoverage)
		for variant := range state.assignedCounts[name] {
			set.Coverage[variant] = &OutcomeMetricCoverage{}
		}
	}
	for _, run := range runs {
		for name, kind := range refs {
			appendOutcomeMetricObservation(run, name, kind, windows[name], now, data, state)
		}
	}
	addMissingAssignmentHistory(state)
	for _, set := range state.sets {
		for variant, exclusions := range set.Exclusions {
			for _, exclusion := range exclusions {
				if exclusion.Reason == exclusionAssignmentHistoryUnavailable {
					set.Coverage[variant].Unavailable += exclusion.Count
				}
			}
		}
	}
	return state.sets
}

func appendOutcomeMetricObservation(run ExperimentRunRecord, name, kind string, window time.Duration, now time.Time, data map[string]outcomeRunData, state *graderObservationState) {
	variant, assigned := run.Assignments[name]
	if !assigned {
		return
	}
	set := state.sets[name]
	state.recordedCounts[name][variant]++
	if set.Coverage[variant] == nil {
		set.Coverage[variant] = &OutcomeMetricCoverage{}
	}
	coverage := set.Coverage[variant]
	if _, duplicate := state.seen[name][run.RunID]; duplicate {
		addObservationExclusion(set, variant, exclusionDuplicateAssignment, run.RunID, 1)
		return
	}
	state.seen[name][run.RunID] = struct{}{}
	reason := outcomeRunMaturity(run, window, now)
	switch reason {
	case "immature":
		coverage.Immature++
	case "unavailable":
		coverage.Unavailable++
	default:
		runData, found := data[run.RunID]
		if !found || runData.ExclusionReason != "" || runData.Report.EvalError != "" {
			coverage.Unavailable++
			reason = "unavailable"
		} else {
			value, measured, itemCoverage := outcomeMetricRunValue(kind, runData.Report.Items)
			coverage.StalePending += itemCoverage.StalePending
			coverage.Unavailable += itemCoverage.Unavailable
			coverage.NoOp += itemCoverage.NoOp
			if measured {
				set.ByVariant[variant] = append(set.ByVariant[variant], GraderMetricObservation{
					RunID: run.RunID, Variant: variant, GraderID: set.MetricID,
					GraderStatus: "measured", Value: value, Binary: value == 0 || value == 1,
				})
				return
			}
			if itemCoverage.NoOp > 0 {
				reason = "no_op"
			} else {
				reason = "unavailable"
			}
		}
	}
	addObservationExclusion(set, variant, reason, run.RunID, 1)
}

func outcomeMetricRunValue(kind string, items []OutcomeReport) (float64, bool, OutcomeMetricCoverage) {
	var coverage OutcomeMetricCoverage
	numerator, denominator := 0, 0
	for _, item := range items {
		switch item.OutcomeStatus {
		case OutcomeStatusAccepted:
			if kind == "zero_touch" && item.Type == "create_pull_request" && !item.ZeroTouchEvidenceComplete {
				coverage.Unavailable = 1
				continue
			}
			denominator++
			if kind == "accepted" || item.Type == "create_pull_request" && item.ZeroTouch {
				numerator++
			}
		case OutcomeStatusRejected:
			denominator++
		case OutcomeStatusPending:
			denominator++
			coverage.StalePending = 1
		case OutcomeStatusLifecycle, OutcomeStatusLifecycleClose, OutcomeStatusIgnored, OutcomeStatusSkipped:
		default:
			coverage.Unavailable = 1
		}
	}
	if denominator == 0 {
		if coverage.Unavailable == 0 {
			coverage.NoOp = 1
		}
		return 0, false, coverage
	}
	return float64(numerator) / float64(denominator), true, coverage
}

func printOutcomeMetricCoverage(label string, coverage *OutcomeMetricCoverage) {
	if coverage == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "  Outcome coverage %s: immature=%d stale_pending=%d unavailable=%d no_op=%d\n",
		label, coverage.Immature, coverage.StalePending, coverage.Unavailable, coverage.NoOp)
}

func outcomeVariantCoverage(set *graderMetricObservationSet, variant string) *OutcomeMetricCoverage {
	if set.Coverage == nil {
		return nil
	}
	if coverage := set.Coverage[variant]; coverage != nil {
		return coverage
	}
	return &OutcomeMetricCoverage{}
}
