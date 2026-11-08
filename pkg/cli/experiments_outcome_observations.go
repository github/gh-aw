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
// Category counts are per-run flags, so multiple outputs in one category count once.
type OutcomeMetricCoverage struct {
	Immature     int `json:"immature"`
	StalePending int `json:"stale_pending"`
	Unavailable  int `json:"unavailable"`
	NoOp         int `json:"no_op"`
}

// OutcomeWindowInfo records the effective maturity window and how it was selected.
type OutcomeWindowInfo struct {
	Window     string `json:"window"`
	Source     string `json:"source"`
	SampleSize int    `json:"sample_size"`
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
			window, err := experimentOutcomeWindow(name, cfg.OutcomeWindow)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if cfg.OutcomeWindow == "" || cfg.OutcomeWindow == "auto" {
				automatic[name] = struct{}{}
			}
			windows[name] = window
		}
		for _, ref := range cfg.SecondaryMetrics {
			kind, recognized := workflow.ParseExperimentMetricOutcomeReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, nil, nil, nil, fmt.Errorf("experiments.%s: unknown outcome metric %q. Expected outcome:accepted or outcome:zero_touch. Example: metric: outcome:accepted", name, ref)
			}
			window, err := experimentOutcomeWindow(name, cfg.OutcomeWindow)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			windows[name] = window
			if cfg.OutcomeWindow == "" || cfg.OutcomeWindow == "auto" {
				automatic[name] = struct{}{}
			}
		}
	}
	return primary, guardrails, windows, automatic, nil
}

func experimentOutcomeWindow(name, configured string) (time.Duration, error) {
	if configured == "" || configured == "auto" {
		return defaultExperimentOutcomeWindow, nil
	}
	window, err := time.ParseDuration(configured)
	if err != nil || window <= 0 {
		return 0, fmt.Errorf("experiments.%s.outcome_window: invalid value %q. Expected \"auto\" or a positive Go duration. Example: outcome_window: 72h", name, configured)
	}
	return window, nil
}

func resolveOutcomeSecondaryMetricReferences(configs map[string]*workflow.ExperimentConfig) (map[string]map[string]string, error) {
	refs := make(map[string]map[string]string)
	for name, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, ref := range cfg.SecondaryMetrics {
			kind, recognized := workflow.ParseExperimentMetricOutcomeReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, fmt.Errorf("experiments.%s: unknown outcome metric %q. Expected outcome:accepted or outcome:zero_touch. Example: metric: outcome:accepted", name, ref)
			}
			if refs[name] == nil {
				refs[name] = make(map[string]string)
			}
			refs[name][ref] = kind
		}
	}
	return refs, nil
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

func loadOutcomeObservationSetsForAnalysis(details *ExperimentDetails, frontmatter experimentFrontmatterResult, repoOverride string) (map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, func(), error) {
	primary, guardrails, windows, automatic, err := resolveOutcomeMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil || len(windows) == 0 {
		return nil, nil, nil, func() {}, err
	}
	secondary, err := resolveOutcomeSecondaryMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil {
		return nil, nil, nil, func() {}, err
	}
	cacheDir, err := os.MkdirTemp("", "gh-aw-experiment-outcomes-")
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("failed to prepare outcome artifact cache: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(cacheDir) }
	now := time.Now()
	source := &githubOutcomeRunSource{artifacts: newGitHubGraderRunArtifactSource(cacheDir, repoOverride)}
	data := loadOutcomeRunData(context.Background(), details.Runs, windows, automatic, now, source)
	windowInfo := make(map[string]*OutcomeWindowInfo, len(windows))
	for name := range automatic {
		info := estimateOutcomeWindowInfo(details.Runs, map[string]struct{}{name: {}}, data, now)
		windowInfo[name] = &info
		windows[name] = info.duration()
	}
	for name := range windows {
		if _, auto := automatic[name]; auto {
			continue
		}
		windowInfo[name] = &OutcomeWindowInfo{Window: windows[name].String(), Source: "configured"}
	}
	sets := buildOutcomeMetricObservationSets(details.Experiments, details.Runs, primary, windows, now, data)
	secondarySets := make(map[string]map[string]*graderMetricObservationSet, len(secondary))
	for name, metrics := range secondary {
		secondarySets[name] = make(map[string]*graderMetricObservationSet, len(metrics))
		for ref, kind := range metrics {
			metricSets := buildOutcomeMetricObservationSets(details.Experiments, details.Runs, map[string]string{name: kind}, windows, now, data)
			secondarySets[name][ref] = metricSets[name]
		}
	}
	guardrailSets := make(map[string]map[string]*graderMetricObservationSet)
	for name, metrics := range guardrails {
		guardrailSets[name] = make(map[string]*graderMetricObservationSet)
		for ref, kind := range metrics {
			guardrailSets[name][ref] = buildOutcomeMetricObservationSets(details.Experiments, details.Runs, map[string]string{name: kind}, windows, now, data)[name]
		}
	}
	for name, set := range sets {
		set.OutcomeWindow = windowInfo[name]
	}
	for name, metrics := range secondarySets {
		for _, set := range metrics {
			set.OutcomeWindow = windowInfo[name]
		}
	}
	for name, metrics := range guardrailSets {
		for _, set := range metrics {
			set.OutcomeWindow = windowInfo[name]
		}
	}
	return sets, secondarySets, guardrailSets, cleanup, nil
}

type outcomeWindowObservation struct {
	duration time.Duration
	decided  bool
}

func estimateOutcomeWindowInfo(runs []ExperimentRunRecord, automatic map[string]struct{}, data map[string]outcomeRunData, now time.Time) OutcomeWindowInfo {
	observations, decided := collectOutcomeWindowObservations(runs, automatic, data, now)
	if decided < minExperimentOutcomeDecisions {
		return OutcomeWindowInfo{Window: defaultExperimentOutcomeWindow.String(), Source: "default", SampleSize: len(observations)}
	}
	return OutcomeWindowInfo{
		Window: kaplanMeierOutcomeWindowP95(observations).String(), Source: "learned", SampleSize: len(observations),
	}
}

func (i OutcomeWindowInfo) duration() time.Duration {
	window, err := time.ParseDuration(i.Window)
	if err != nil || window <= 0 {
		return defaultExperimentOutcomeWindow
	}
	return window
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
		// Repeated products can round an exact 5% survival boundary upward.
		if *survival <= 0.05+1e-12 {
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

// Coverage flags are per run; each category counts once, regardless of item count.
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
