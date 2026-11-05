package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/sourcegraph/conc/pool"
)

const defaultExperimentOutcomeWindow = 72 * time.Hour

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
	if len(report.Items) == 0 {
		items, _, err := readLogsOutcomeItems(run)
		if err != nil {
			return outcomeRunData{ExclusionReason: exclusionArtifactUnavailable}
		}
		for _, item := range items {
			if item.Type != "noop" {
				return outcomeRunData{ExclusionReason: "unavailable"}
			}
		}
	}
	return outcomeRunData{Report: report}
}

func resolveOutcomeMetricReferences(configs map[string]*workflow.ExperimentConfig) (map[string]string, map[string]map[string]string, map[string]time.Duration, error) {
	primary := make(map[string]string)
	guardrails := make(map[string]map[string]string)
	windows := make(map[string]time.Duration)
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
				return nil, nil, nil, fmt.Errorf("experiments.%s: unknown outcome metric %q. Expected outcome:accepted or outcome:zero_touch. Example: metric: outcome:accepted", name, ref)
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
			if cfg.OutcomeWindow != "" {
				var err error
				window, err = time.ParseDuration(cfg.OutcomeWindow)
				if err != nil || window <= 0 {
					return nil, nil, nil, fmt.Errorf("experiments.%s.outcome_window: invalid duration %q. Expected a positive duration. Example: outcome_window: 72h", name, cfg.OutcomeWindow)
				}
			}
			windows[name] = window
		}
	}
	return primary, guardrails, windows, nil
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

func loadOutcomeRunData(ctx context.Context, runs []ExperimentRunRecord, windows map[string]time.Duration, now time.Time, source outcomeRunSource) map[string]outcomeRunData {
	ctx = withOutcomeAPICache(ctx)
	runIDs := make(map[string]struct{})
	for _, run := range runs {
		for name, window := range windows {
			if _, assigned := run.Assignments[name]; !assigned || outcomeRunMaturity(run, window, now) != "" {
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
	primary, guardrails, windows, err := resolveOutcomeMetricReferences(frontmatter.ExperimentConfigs)
	if err != nil || len(windows) == 0 {
		return nil, nil, func() {}, err
	}
	cacheDir, err := os.MkdirTemp(".", ".gh-aw-experiment-outcomes-")
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("failed to prepare outcome artifact cache: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(cacheDir) }
	now := time.Now()
	source := &githubOutcomeRunSource{artifacts: newGitHubGraderRunArtifactSource(cacheDir, repoOverride)}
	data := loadOutcomeRunData(context.Background(), details.Runs, windows, now, source)
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
			if kind == "zero_touch" && !item.ZeroTouchEvidenceComplete {
				coverage.Unavailable = 1
				continue
			}
			denominator++
			if kind == "accepted" || item.ZeroTouch {
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
