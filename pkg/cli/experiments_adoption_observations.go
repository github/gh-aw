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

const defaultExperimentAdoptionWindow = 72 * time.Hour
const minExperimentAdoptionDecisions = 20
const minExperimentAdoptionWindow = 24 * time.Hour
const maxExperimentAdoptionWindow = 30 * 24 * time.Hour

// AdoptionMetricCoverage counts assigned runs, not outputs. A partially observed
// run can contribute both a measurement and an unavailable/stale-pending count.
// Category counts are per-run flags, so multiple outputs in one category count once.
type AdoptionMetricCoverage struct {
	Immature     int `json:"immature"`
	StalePending int `json:"stale_pending"`
	Unavailable  int `json:"unavailable"`
	NoOp         int `json:"no_op"`
}

// AdoptionWindowInfo records the effective maturity window and how it was selected.
type AdoptionWindowInfo struct {
	Window     string `json:"window"`
	Source     string `json:"source"`
	SampleSize int    `json:"sample_size"`
}

type adoptionRunData struct {
	Report          AdoptionsData
	ExclusionReason string
}

type adoptionRunSource interface {
	Load(context.Context, string) adoptionRunData
}

type githubAdoptionRunSource struct {
	artifacts *githubGraderRunArtifactSource
}

func (s *githubAdoptionRunSource) Load(ctx context.Context, runIDText string) adoptionRunData {
	runID, reason := s.artifacts.eligibleRunID(ctx, runIDText)
	if reason != "" {
		return adoptionRunData{ExclusionReason: reason}
	}
	runDir := filepath.Join(s.artifacts.baseDir, strconv.FormatInt(runID, 10))
	if err := os.MkdirAll(runDir, constants.DirPermPublic); err != nil {
		return adoptionRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	names, err := listRunArtifactNames(ctx, runID, s.artifacts.owner, s.artifacts.repo, s.artifacts.hostname, false)
	if err != nil {
		return adoptionRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	var selected []string
	for _, name := range names {
		if artifactMatchesFilter(name, artifactSetArtifacts[ArtifactSetAdoptions]) {
			selected = append(selected, name)
		}
	}
	if len(selected) == 0 {
		return adoptionRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	opts := downloadArtifactsOptions{
		runID: runID, outputDir: runDir, owner: s.artifacts.owner,
		repo: s.artifacts.repo, hostname: s.artifacts.hostname, failOnDownloadError: true,
	}
	if err := downloadArtifactsByName(ctx, opts, selected); err != nil {
		return adoptionRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	// Preserve invocation directories: collectRunAdoptions understands immutable
	// manifests and their matching compact enrichment artifacts.
	run := ProcessedRun{Run: WorkflowRun{
		DatabaseID: runID, Repository: buildRepoFlag(s.artifacts.owner, s.artifacts.repo, s.artifacts.hostname),
		LogsPath: runDir,
	}}
	report, err := collectRunAdoptions(ctx, run, nil, false)
	if err != nil || report.EvalError != "" {
		return adoptionRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	return adoptionRunData{Report: report}
}

func resolveAdoptionMetricReferences(configs map[string]*workflow.ExperimentConfig) (map[string]string, map[string]map[string]string, map[string]time.Duration, map[string]struct{}, error) {
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
			kind, recognized := workflow.ParseExperimentMetricAdoptionReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, nil, nil, nil, fmt.Errorf("experiments.%s: unknown adoption metric %q. Expected adoption:accepted or adoption:zero_touch. Example: metric: adoption:accepted", name, ref)
			}
			if index == 0 {
				primary[name] = kind
			} else {
				if guardrails[name] == nil {
					guardrails[name] = make(map[string]string)
				}
				guardrails[name][ref] = kind
			}
			window, err := experimentAdoptionWindow(name, cfg.AdoptionWindow)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if cfg.AdoptionWindow == "" || cfg.AdoptionWindow == "auto" {
				automatic[name] = struct{}{}
			}
			windows[name] = window
		}
		for _, ref := range cfg.SecondaryMetrics {
			kind, recognized := workflow.ParseExperimentMetricAdoptionReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, nil, nil, nil, fmt.Errorf("experiments.%s: unknown adoption metric %q. Expected adoption:accepted or adoption:zero_touch. Example: metric: adoption:accepted", name, ref)
			}
			window, err := experimentAdoptionWindow(name, cfg.AdoptionWindow)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			windows[name] = window
			if cfg.AdoptionWindow == "" || cfg.AdoptionWindow == "auto" {
				automatic[name] = struct{}{}
			}
		}
	}
	return primary, guardrails, windows, automatic, nil
}

func experimentAdoptionWindow(name, configured string) (time.Duration, error) {
	if configured == "" || configured == "auto" {
		return defaultExperimentAdoptionWindow, nil
	}
	window, err := time.ParseDuration(configured)
	if err != nil || window <= 0 {
		return 0, fmt.Errorf("experiments.%s.adoption_window: invalid value %q. Expected \"auto\" or a positive Go duration. Example: adoption_window: 72h", name, configured)
	}
	return window, nil
}

func resolveAdoptionSecondaryMetricReferences(configs map[string]*workflow.ExperimentConfig) (map[string]map[string]string, error) {
	refs := make(map[string]map[string]string)
	for name, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, ref := range cfg.SecondaryMetrics {
			kind, recognized := workflow.ParseExperimentMetricAdoptionReference(ref)
			if !recognized {
				continue
			}
			if kind == "" {
				return nil, fmt.Errorf("experiments.%s: unknown adoption metric %q. Expected adoption:accepted or adoption:zero_touch. Example: metric: adoption:accepted", name, ref)
			}
			if refs[name] == nil {
				refs[name] = make(map[string]string)
			}
			refs[name][ref] = kind
		}
	}
	return refs, nil
}

func adoptionRunMaturity(run ExperimentRunRecord, window time.Duration, now time.Time) string {
	if window == 0 {
		window = defaultExperimentAdoptionWindow
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

func loadAdoptionRunData(ctx context.Context, runs []ExperimentRunRecord, windows map[string]time.Duration, automatic map[string]struct{}, now time.Time, source adoptionRunSource) map[string]adoptionRunData {
	ctx = withAdoptionAPICache(ctx)
	runIDs := make(map[string]struct{})
	for _, run := range runs {
		for name, window := range windows {
			_, auto := automatic[name]
			if _, assigned := run.Assignments[name]; !assigned || !auto && adoptionRunMaturity(run, window, now) != "" {
				continue
			}
			runIDs[run.RunID] = struct{}{}
			break
		}
	}
	result := make(map[string]adoptionRunData, len(runIDs))
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

func loadAdoptionObservationSetsForAnalysis(
	ctx context.Context,
	details *ExperimentDetails,
	frontmatter experimentFrontmatterResult,
	repoOverride string,
	sourceFactory func(baseDir, repoOverride string) adoptionRunSource,
) (map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, func(), error) {
	primary, guardrails, windows, automatic, err := resolveAdoptionMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil || len(windows) == 0 {
		return nil, nil, nil, func() {}, err
	}
	secondary, err := resolveAdoptionSecondaryMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil {
		return nil, nil, nil, func() {}, err
	}
	cacheDir, err := os.MkdirTemp("", "gh-aw-experiment-adoptions-")
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("failed to prepare adoption artifact cache: %w", err)
	}
	cleanup := experimentCacheCleanup(cacheDir, "adoption")
	now := time.Now()
	source := sourceFactory(cacheDir, repoOverride)
	data := loadAdoptionRunData(ctx, details.Runs, windows, automatic, now, source)
	windowInfo := make(map[string]*AdoptionWindowInfo, len(windows))
	for name := range automatic {
		info := estimateAdoptionWindowInfo(details.Runs, map[string]struct{}{name: {}}, data, now)
		windowInfo[name] = &info
		windows[name] = info.duration()
	}
	for name := range windows {
		if _, auto := automatic[name]; auto {
			continue
		}
		windowInfo[name] = &AdoptionWindowInfo{Window: windows[name].String(), Source: "configured"}
	}
	sets := buildAdoptionMetricObservationSets(details.Experiments, details.Runs, primary, windows, now, data)
	secondarySets := make(map[string]map[string]*graderMetricObservationSet, len(secondary))
	for name, metrics := range secondary {
		secondarySets[name] = make(map[string]*graderMetricObservationSet, len(metrics))
		for ref, kind := range metrics {
			metricSets := buildAdoptionMetricObservationSets(details.Experiments, details.Runs, map[string]string{name: kind}, windows, now, data)
			secondarySets[name][ref] = metricSets[name]
		}
	}
	guardrailSets := make(map[string]map[string]*graderMetricObservationSet)
	for name, metrics := range guardrails {
		guardrailSets[name] = make(map[string]*graderMetricObservationSet)
		for ref, kind := range metrics {
			guardrailSets[name][ref] = buildAdoptionMetricObservationSets(details.Experiments, details.Runs, map[string]string{name: kind}, windows, now, data)[name]
		}
	}
	for name, set := range sets {
		set.AdoptionWindow = windowInfo[name]
	}
	for name, metrics := range secondarySets {
		for _, set := range metrics {
			set.AdoptionWindow = windowInfo[name]
		}
	}
	for name, metrics := range guardrailSets {
		for _, set := range metrics {
			set.AdoptionWindow = windowInfo[name]
		}
	}
	return sets, secondarySets, guardrailSets, cleanup, nil
}

type adoptionWindowObservation struct {
	duration time.Duration
	decided  bool
}

func estimateAdoptionWindowInfo(runs []ExperimentRunRecord, automatic map[string]struct{}, data map[string]adoptionRunData, now time.Time) AdoptionWindowInfo {
	observations, decided := collectAdoptionWindowObservations(runs, automatic, data, now)
	if decided < minExperimentAdoptionDecisions {
		return AdoptionWindowInfo{Window: defaultExperimentAdoptionWindow.String(), Source: "default", SampleSize: len(observations)}
	}
	return AdoptionWindowInfo{
		Window: kaplanMeierAdoptionWindowP95(observations).String(), Source: "learned", SampleSize: len(observations),
	}
}

func (i AdoptionWindowInfo) duration() time.Duration {
	window, err := time.ParseDuration(i.Window)
	if err != nil || window <= 0 {
		return defaultExperimentAdoptionWindow
	}
	return window
}

func collectAdoptionWindowObservations(runs []ExperimentRunRecord, automatic map[string]struct{}, data map[string]adoptionRunData, now time.Time) ([]adoptionWindowObservation, int) {
	observations := make([]adoptionWindowObservation, 0)
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
			switch item.AdoptionStatus {
			case AdoptionStatusAccepted, AdoptionStatusRejected:
				if item.TimeToAdoptionHours <= 0 || math.IsNaN(item.TimeToAdoptionHours) || math.IsInf(item.TimeToAdoptionHours, 0) {
					continue
				}
				duration := maxExperimentAdoptionWindow
				if item.TimeToAdoptionHours < maxExperimentAdoptionWindow.Hours() {
					duration = time.Duration(item.TimeToAdoptionHours * float64(time.Hour))
				}
				if duration <= 0 {
					continue
				}
				observations = append(observations, adoptionWindowObservation{duration: duration, decided: true})
				decided++
			case AdoptionStatusPending:
				createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
				if err != nil {
					continue
				}
				duration := now.Sub(createdAt)
				if duration >= 0 {
					observations = append(observations, adoptionWindowObservation{duration: duration})
				}
			}
		}
	}
	return observations, decided
}

func kaplanMeierAdoptionWindowP95(observations []adoptionWindowObservation) time.Duration {
	slices.SortFunc(observations, func(a, b adoptionWindowObservation) int {
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
			if window, reached := applyAdoptionWindowKMGroup(currentDuration, events, groupSize, &atRisk, &survival); reached {
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
		if window, reached := applyAdoptionWindowKMGroup(currentDuration, events, groupSize, &atRisk, &survival); reached {
			return window
		}
	}
	return maxExperimentAdoptionWindow
}

func applyAdoptionWindowKMGroup(duration time.Duration, events, groupSize int, atRisk *int, survival *float64) (time.Duration, bool) {
	if events > 0 && *atRisk > 0 {
		*survival *= 1 - float64(events)/float64(*atRisk)
		// Repeated products can round an exact 5% survival boundary upward.
		if *survival <= 0.05+1e-12 {
			return clampExperimentAdoptionWindow(duration), true
		}
	}
	*atRisk -= groupSize
	return 0, false
}

func clampExperimentAdoptionWindow(window time.Duration) time.Duration {
	return min(max(window, minExperimentAdoptionWindow), maxExperimentAdoptionWindow)
}

func buildAdoptionMetricObservationSets(experiments []ExperimentVariantStats, runs []ExperimentRunRecord, refs map[string]string, windows map[string]time.Duration, now time.Time, data map[string]adoptionRunData) map[string]*graderMetricObservationSet {
	state := initializeGraderObservationSets(experiments, refs, nil)
	for name, set := range state.sets {
		set.MetricID = "adoption:" + refs[name]
		set.Coverage = make(map[string]*AdoptionMetricCoverage)
		for variant := range state.assignedCounts[name] {
			set.Coverage[variant] = &AdoptionMetricCoverage{}
		}
	}
	for _, run := range runs {
		for name, kind := range refs {
			appendAdoptionMetricObservation(run, name, kind, windows[name], now, data, state)
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

func appendAdoptionMetricObservation(run ExperimentRunRecord, name, kind string, window time.Duration, now time.Time, data map[string]adoptionRunData, state *graderObservationState) {
	variant, assigned := run.Assignments[name]
	if !assigned {
		return
	}
	set := state.sets[name]
	state.recordedCounts[name][variant]++
	if set.Coverage[variant] == nil {
		set.Coverage[variant] = &AdoptionMetricCoverage{}
	}
	coverage := set.Coverage[variant]
	if _, duplicate := state.seen[name][run.RunID]; duplicate {
		addObservationExclusion(set, variant, exclusionDuplicateAssignment, run.RunID, 1)
		return
	}
	state.seen[name][run.RunID] = struct{}{}
	reason := adoptionRunMaturity(run, window, now)
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
			value, measured, itemCoverage := adoptionMetricRunValue(kind, runData.Report.Items)
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
func adoptionMetricRunValue(kind string, items []AdoptionReport) (float64, bool, AdoptionMetricCoverage) {
	var coverage AdoptionMetricCoverage
	numerator, denominator := 0, 0
	for _, item := range items {
		switch item.AdoptionStatus {
		case AdoptionStatusAccepted:
			if kind == "zero_touch" && item.Type == "create_pull_request" && !item.ZeroTouchEvidenceComplete {
				coverage.Unavailable = 1
				continue
			}
			denominator++
			if kind == "accepted" || item.Type == "create_pull_request" && item.ZeroTouch {
				numerator++
			}
		case AdoptionStatusRejected:
			denominator++
		case AdoptionStatusPending:
			denominator++
			coverage.StalePending = 1
		case AdoptionStatusLifecycle, AdoptionStatusNotApplicable:
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

func printAdoptionMetricCoverage(label string, coverage *AdoptionMetricCoverage) {
	if coverage == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "  Adoption coverage %s: immature=%d stale_pending=%d unavailable=%d no_op=%d\n",
		label, coverage.Immature, coverage.StalePending, coverage.Unavailable, coverage.NoOp)
}

func adoptionVariantCoverage(set *graderMetricObservationSet, variant string) *AdoptionMetricCoverage {
	if set.Coverage == nil {
		return nil
	}
	if coverage := set.Coverage[variant]; coverage != nil {
		return coverage
	}
	return &AdoptionMetricCoverage{}
}
