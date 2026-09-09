package cli

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var experimentsStatsLog = logger.New("cli:experiments_statistics")

// defaultMinSamples is the minimum samples per variant before analysis is reliable (§11.4 / R-STAT-007).
const defaultMinSamples = 20

// balanceSignificanceThreshold is the p-value threshold for the chi-square balance test.
const balanceSignificanceThreshold = 0.05

// ExperimentReadiness reports whether normal analysis requirements have enough usable data.
type ExperimentReadiness string

const (
	ExperimentReadinessCollecting ExperimentReadiness = "COLLECTING"
	ExperimentReadinessReady      ExperimentReadiness = "READY"
)

// ExperimentAnalysis holds statistical analysis results for one named A/B experiment.
// The analysis is computed from state.jsonl/state.json counts and optional experiment configuration.
type ExperimentAnalysis struct {
	ExperimentDecisionResult

	// ExperimentName is the name of the A/B experiment (key in state.counts).
	ExperimentName string `json:"experiment_name"`

	// Hypothesis is the null/alternative hypothesis text (from experiment config).
	Hypothesis string `json:"hypothesis,omitempty"`

	// AnalysisType is the statistical test declared in the experiment config
	// (t_test, mann_whitney, proportion_test, bayesian_ab).
	AnalysisType string `json:"analysis_type,omitempty"`

	// Metric is the primary metric string declared in the experiment config
	// (e.g. "effective_tokens" or "evals.builds").
	Metric string `json:"metric,omitempty"`

	// MetricQuestion is the resolved eval question text when Metric references a declared
	// eval question ID (e.g. "evals.builds" resolves to "Does the generated code compile?").
	// Empty when Metric is absent or does not reference an eval.
	MetricQuestion string `json:"metric_question,omitempty"`

	// MetricEvalResults summarizes observed eval outcomes when Metric references an eval ID
	// and evals.jsonl data is available.
	MetricEvalResults *MetricEvalResults `json:"metric_eval_results,omitempty"`

	// MetricGraderID is the normalized grader identifier for a grader-backed metric.
	MetricGraderID string `json:"metric_grader_id,omitempty"`

	// MetricType is "continuous" or "binary" when grader observations are available.
	MetricType string `json:"metric_type,omitempty"`

	// MinSamples is the minimum runs per variant required before analysis is reliable.
	// Defaults to 20 when not declared in the experiment config (R-STAT-007).
	MinSamples int `json:"min_samples"`

	// TotalRuns is the total number of observed runs across all variants.
	TotalRuns int `json:"total_runs"`

	// Variants holds per-variant statistics in alphabetical order.
	Variants []VariantAnalysis `json:"variants"`

	// Balance test (chi-square goodness-of-fit against expected allocation, §11.1).
	ChiSquare        float64 `json:"chi_square"`
	DegreesOfFreedom int     `json:"degrees_of_freedom"`
	PValue           float64 `json:"p_value"`
	IsBalanced       bool    `json:"is_balanced"`

	// BonferroniAlpha is the Bonferroni-corrected significance threshold for experiments
	// with K ≥ 3 variants (§11.3: α_adjusted = 0.05 / (K − 1)).
	// Zero when fewer than 3 variants are declared.
	BonferroniAlpha float64 `json:"bonferroni_alpha,omitempty"`

	// Guardrails lists the declared metric thresholds.
	// Pass/fail evaluation requires per-run outcome data not stored in state.jsonl/state.json (R-STAT-009).
	Guardrails []GuardrailStatus `json:"guardrails,omitempty"`

	// Comparisons contains control-versus-variant outcome analyses for grader observations.
	Comparisons []MetricComparison `json:"comparisons,omitempty"`

	// MetricDirection is the normalized primary metric direction: max or min.
	MetricDirection string `json:"metric_direction,omitempty"`

	// DecisionPolicy is the normalized deterministic decision policy.
	DecisionPolicy ExperimentDecisionPolicy `json:"-"`

	// VariantOrder preserves configured control/candidate ordering for the decision layer.
	VariantOrder []string `json:"-"`

	// UsesMetricObservations distinguishes usable outcome counts from assignment counts.
	UsesMetricObservations bool `json:"-"`

	// Readiness is COLLECTING until every variant reaches min_samples, then READY.
	Readiness ExperimentReadiness `json:"readiness"`

	// Recommendation is the analysis recommendation: EXTEND or READY_FOR_ANALYSIS.
	// EXTEND is issued when any variant is below min_samples (R-STAT-007).
	Recommendation string `json:"recommendation"`

	// Rationale is a one-sentence explanation of the recommendation.
	Rationale string `json:"rationale"`
}

// VariantAnalysis holds per-variant statistics for one experiment.
type VariantAnalysis struct {
	// Name is the variant identifier (e.g., "concise", "detailed").
	Name string `json:"name"`

	// Count is the number of times this variant was selected (from state.counts).
	Count int `json:"count"`

	// ObservedPct is the observed percentage share of total runs (0–100).
	ObservedPct float64 `json:"observed_pct"`

	// ExpectedPct is the expected percentage share based on declared weights or equal split (0–100).
	ExpectedPct float64 `json:"expected_pct"`

	// MinSamples is the minimum required count for this variant.
	MinSamples int `json:"min_samples"`

	// BelowMinSamples is true when Count < MinSamples.
	BelowMinSamples bool `json:"below_min_samples"`

	// ObservationCount is the number of usable grader measurements for this variant.
	ObservationCount int `json:"observation_count,omitempty"`

	// Mean is the arithmetic mean of usable grader measurements.
	Mean *float64 `json:"mean,omitempty"`

	// Observations lists the usable grader/eval measurements for this variant, preserving
	// run and grader/eval provenance (run ID, metric ID, status, and value) for each
	// included measurement.
	Observations []GraderMetricObservation `json:"observations,omitempty"`

	// Excluded summarizes assigned runs that did not yield a usable grader measurement.
	Excluded []ExcludedObservationSummary `json:"excluded,omitempty"`
}

// GuardrailStatusCode is the stable machine-readable outcome of a guardrail evaluation.
type GuardrailStatusCode string

const (
	GuardrailStatusInsufficientObservations GuardrailStatusCode = "insufficient_observations"
	GuardrailStatusMissing                  GuardrailStatusCode = "missing"
	GuardrailStatusUnsupported              GuardrailStatusCode = "unsupported"
	GuardrailStatusFail                     GuardrailStatusCode = "fail"
	GuardrailStatusPass                     GuardrailStatusCode = "pass"
)

// GuardrailStatus represents a declared guardrail metric threshold (R-STAT-009).
// The Threshold field records the declared expression (e.g. ">=0.95").
// Status, Passed, and Variants are populated after metric observations are resolved.
type GuardrailStatus struct {
	Name      string                   `json:"name"`
	Threshold string                   `json:"threshold"`
	Direction string                   `json:"direction,omitempty"`
	Status    GuardrailStatusCode      `json:"status"`
	Passed    *bool                    `json:"passed,omitempty"`
	Variants  []GuardrailVariantStatus `json:"variants,omitempty"`
}

// GuardrailVariantStatus reports one variant's aggregate guardrail outcome.
type GuardrailVariantStatus struct {
	Variant          string   `json:"variant"`
	ObservationCount int      `json:"observation_count"`
	Mean             *float64 `json:"mean,omitempty"`
	Passed           *bool    `json:"passed,omitempty"`
}

// MetricEvalResults summarizes observed YES/NO/UNKNOWN outcomes for an eval-backed metric.
type MetricEvalResults struct {
	Yes          int    `json:"yes"`
	No           int    `json:"no"`
	Unknown      int    `json:"unknown"`
	Total        int    `json:"total"`
	LatestAnswer string `json:"latest_answer,omitempty"`
	LatestRunID  string `json:"latest_run_id,omitempty"`
}

func computeExperimentAnalysisWithObservationBundle(
	exp ExperimentVariantStats,
	cfg *workflow.ExperimentConfig,
	evals *workflow.EvalsConfig,
	metricEvalResults map[string]MetricEvalResults,
	graderObservations *graderMetricObservationSet,
	guardrailObservations map[string]*graderMetricObservationSet,
) ExperimentAnalysis {
	experimentsStatsLog.Printf("Computing analysis for experiment %q: %d variant(s), %d total runs", exp.Name, len(exp.Variants), exp.Total)
	a := newExperimentAnalysis(exp, cfg, evals, metricEvalResults)
	if len(exp.Variants) < 2 && graderObservations == nil {
		experimentsStatsLog.Printf("Experiment %q has fewer than 2 variants; skipping analysis", exp.Name)
		a.IsBalanced = true
		a.Recommendation = "EXTEND"
		a.Rationale = "experiment has fewer than 2 variants; cannot perform statistical analysis"
		a.ExperimentDecisionResult = DecideExperiment(a)
		return a
	}

	variantCounts := experimentVariantCounts(exp, cfg, graderObservations != nil)
	variantNames := sliceutil.SortedKeys(variantCounts)
	expectedPcts := expectedProportions(variantNames, cfg)
	// When cfg declares a variant list, variantCounts has already been reconciled
	// against it (stale variant keys dropped); recompute the total from the
	// reconciled counts so it stays consistent with the chi-square inputs below.
	total := exp.Total
	if cfg != nil && len(cfg.Variants) > 0 {
		total = sumVariantCounts(variantCounts)
	}
	a.Variants = buildVariantAnalyses(total, variantCounts, variantNames, expectedPcts, a.MinSamples, graderObservations)
	if graderObservations != nil {
		a.UsesMetricObservations = true
		a.MetricType = graderObservationMetricType(cfg, graderObservations)
		a.MetricDirection = normalizeMetricDirection(graderObservations.Direction)
		a.Comparisons = computeGraderMetricComparisons(cfg, graderObservations, variantNames, a.MetricType)
	}
	applyExperimentBalance(&a, exp.Name, total, cfg, variantCounts, variantNames, expectedPcts)
	applyExperimentReadiness(&a, graderObservations != nil)
	applyExperimentGuardrails(&a, guardrailObservations)
	a.ExperimentDecisionResult = DecideExperiment(a)
	return a
}

func newExperimentAnalysis(
	exp ExperimentVariantStats,
	cfg *workflow.ExperimentConfig,
	evals *workflow.EvalsConfig,
	metricEvalResults map[string]MetricEvalResults,
) ExperimentAnalysis {
	a := ExperimentAnalysis{
		ExperimentName: exp.Name,
		TotalRuns:      exp.Total,
		MinSamples:     defaultMinSamples,
		Readiness:      ExperimentReadinessCollecting,
		DecisionPolicy: ExperimentDecisionPolicy{
			Confidence: defaultDecisionConfidence,
		},
	}
	if cfg == nil {
		return a
	}
	a.Hypothesis = cfg.Hypothesis
	a.AnalysisType = cfg.AnalysisType
	a.VariantOrder = append([]string(nil), cfg.Variants...)
	if cfg.MinSamples > 0 {
		a.MinSamples = cfg.MinSamples
	}
	for _, guardrail := range cfg.GuardrailMetrics {
		a.Guardrails = append(a.Guardrails, GuardrailStatus{
			Name: guardrail.Name, Threshold: guardrail.Threshold, Direction: guardrail.Direction,
			Status: GuardrailStatusUnsupported,
		})
	}
	if cfg.Decision != nil {
		a.DecisionPolicy.MinimumEffect = cfg.Decision.MinimumEffect
		a.DecisionPolicy.RegressionTolerance = cfg.Decision.MinimumEffect
		if cfg.Decision.RegressionTolerance != nil {
			a.DecisionPolicy.RegressionTolerance = *cfg.Decision.RegressionTolerance
		}
		if cfg.Decision.Confidence > 0 && cfg.Decision.Confidence < 1 {
			a.DecisionPolicy.Confidence = cfg.Decision.Confidence
		}
	}
	a.Metric = cfg.Metric
	evalID, isEval := workflow.ParseExperimentMetricEvalReference(cfg.Metric)
	if isEval && evalID != "" {
		a.MetricQuestion = findEvalQuestion(evals, evalID)
		if summary, ok := metricEvalResults[evalID]; ok {
			a.MetricEvalResults = &summary
		}
	}
	if graderID, isGrader := workflow.ParseExperimentMetricGraderReference(cfg.Metric); isGrader && graderID != "" {
		a.MetricGraderID = graderID
	}
	return a
}

func normalizeMetricDirection(direction string) string {
	if direction == "lower_is_better" || direction == "min" {
		return "min"
	}
	return "max"
}

func applyExperimentGuardrails(
	analysis *ExperimentAnalysis,
	observationSets map[string]*graderMetricObservationSet,
) {
	for index := range analysis.Guardrails {
		guardrail := &analysis.Guardrails[index]
		set := observationSets[guardrail.Name]
		if set == nil {
			continue
		}
		if guardrail.Direction == "" {
			guardrail.Direction = normalizeMetricDirection(set.Direction)
		}
		hasInsufficient := false
		hasUnsupported := false
		hasFailure := false
		for _, variant := range analysis.Variants {
			observations := set.ByVariant[variant.Name]
			variantStatus := GuardrailVariantStatus{
				Variant:          variant.Name,
				ObservationCount: len(observations),
			}
			if len(observations) > 0 {
				mean := meanGraderObservations(observations)
				variantStatus.Mean = &mean
			}
			if len(observations) < analysis.MinSamples {
				hasInsufficient = true
				guardrail.Variants = append(guardrail.Variants, variantStatus)
				continue
			}
			passed, ok := evaluateGuardrailThreshold(*variantStatus.Mean, guardrail.Threshold, guardrail.Direction)
			if !ok {
				hasUnsupported = true
				guardrail.Variants = append(guardrail.Variants, variantStatus)
				continue
			}
			variantStatus.Passed = &passed
			if !passed {
				hasFailure = true
			}
			guardrail.Variants = append(guardrail.Variants, variantStatus)
		}
		switch {
		case hasInsufficient:
			guardrail.Status = GuardrailStatusInsufficientObservations
		case hasUnsupported:
			guardrail.Status = GuardrailStatusUnsupported
		case hasFailure:
			guardrail.Status = GuardrailStatusFail
			passed := false
			guardrail.Passed = &passed
		default:
			guardrail.Status = GuardrailStatusPass
			passed := true
			guardrail.Passed = &passed
		}
	}
}

func evaluateGuardrailThreshold(value float64, threshold, direction string) (bool, bool) {
	operator := ""
	number := strings.TrimSpace(threshold)
	for _, candidate := range []string{">=", "<=", "==", ">", "<"} {
		if strings.HasPrefix(number, candidate) {
			operator = candidate
			number = strings.TrimSpace(strings.TrimPrefix(number, candidate))
			break
		}
	}
	if operator == "" {
		switch direction {
		case "min":
			operator = "<="
		case "max":
			operator = ">="
		default:
			return false, false
		}
	}
	limit, err := strconv.ParseFloat(number, 64)
	if err != nil || math.IsNaN(limit) || math.IsInf(limit, 0) {
		return false, false
	}
	switch operator {
	case ">=":
		return value >= limit, true
	case "<=":
		return value <= limit, true
	case "==":
		return value == limit, true
	case ">":
		return value > limit, true
	case "<":
		return value < limit, true
	default:
		return false, false
	}
}

func findEvalQuestion(evals *workflow.EvalsConfig, evalID string) string {
	if evals == nil {
		return ""
	}
	for _, question := range evals.Questions {
		if question.ID == evalID {
			return question.Question
		}
	}
	return ""
}

// experimentVariantCounts returns the observed run counts for a named experiment,
// reconciled against the workflow's currently-declared variants: list. Persisted
// state.Counts entries are keyed by whatever variant label a run recorded, and never
// reconciled against the workflow's current variant list; when a workflow renames or
// removes variants, stale labels from earlier runs stay in the branch's state forever
// and would otherwise skew balance/selection statistics. When cfg declares a variant
// list, counts for variants no longer present in it are dropped. When includeDeclared
// is true, declared variants with no observed runs are included with a zero count.
// When cfg is nil or declares no variants (e.g. the workflow's frontmatter could not be
// loaded), the raw, unreconciled counts are returned as-is. Note: reconcileExperimentDetailsWithConfigs
// (pkg/cli/experiments_command.go) applies the same stale-key filtering upstream in
// RunExperimentsAnalyze, so by the time exp.Variants reaches here it is typically already
// reconciled; this function re-applies the filter defensively for callers that build
// ExperimentVariantStats directly (e.g. tests, or future call sites).
func experimentVariantCounts(exp ExperimentVariantStats, cfg *workflow.ExperimentConfig, includeDeclared bool) map[string]int {
	if cfg == nil || len(cfg.Variants) == 0 {
		return exp.Variants
	}
	counts := filterDeclaredVariantCounts(exp.Variants, cfg.Variants)
	if includeDeclared {
		for _, name := range cfg.Variants {
			if _, ok := counts[name]; !ok {
				counts[name] = 0
			}
		}
	}
	return counts
}

// filterDeclaredVariantCounts returns a copy of counts containing only the keys present in
// declaredVariants, dropping any stale variant labels left over from a renamed or removed
// variant. Shared by experimentVariantCounts and reconcileExperimentDetailsWithConfigs so
// the two callers apply identical reconciliation semantics.
func filterDeclaredVariantCounts(counts map[string]int, declaredVariants []string) map[string]int {
	declared := make(map[string]bool, len(declaredVariants))
	for _, name := range declaredVariants {
		declared[name] = true
	}
	filtered := make(map[string]int, typeutil.SafeAllocationCapacity(len(counts), len(declaredVariants)))
	for name, count := range counts {
		if declared[name] {
			filtered[name] = count
		}
	}
	return filtered
}

// staleVariantNames returns the sorted names present in original but absent from filtered,
// used to log which stale variant labels a reconciliation step dropped.
func staleVariantNames(original, filtered map[string]int) []string {
	var stale []string
	for name := range original {
		if _, ok := filtered[name]; !ok {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	return stale
}

// sumVariantCounts returns the sum of all values in a variant→count map.
func sumVariantCounts(counts map[string]int) int {
	total := 0
	for _, c := range counts {
		total += c
	}
	return total
}

func buildVariantAnalyses(
	total int,
	counts map[string]int,
	names []string,
	expected []float64,
	minSamples int,
	graderObservations *graderMetricObservationSet,
) []VariantAnalysis {
	variants := make([]VariantAnalysis, 0, len(names))
	for i, name := range names {
		count := counts[name]
		variant := VariantAnalysis{
			Name: name, Count: count, ObservedPct: safePercent(count, total),
			ExpectedPct: expected[i] * 100, MinSamples: minSamples, BelowMinSamples: count < minSamples,
		}
		if graderObservations != nil {
			observations := graderObservations.ByVariant[name]
			variant.ObservationCount = len(observations)
			variant.BelowMinSamples = len(observations) < minSamples
			variant.Excluded = graderObservations.Exclusions[name]
			if len(observations) > 0 {
				mean := meanGraderObservations(observations)
				variant.Mean = &mean
				variant.Observations = observations
			}
		}
		variants = append(variants, variant)
	}
	return variants
}

func applyExperimentBalance(
	a *ExperimentAnalysis,
	experimentName string,
	total int,
	cfg *workflow.ExperimentConfig,
	counts map[string]int,
	names []string,
	expectedPcts []float64,
) {
	k := len(names)
	if cfg != nil && cfg.Continual != nil {
		a.IsBalanced = true
	} else if total > 0 && k >= 2 {
		for i, name := range names {
			expected := float64(total) * expectedPcts[i]
			if expected > 0 {
				diff := float64(counts[name]) - expected
				a.ChiSquare += (diff * diff) / expected
			}
		}
		a.DegreesOfFreedom = k - 1
		a.PValue = chiSquarePValue(a.ChiSquare, a.DegreesOfFreedom)
		a.IsBalanced = a.PValue >= balanceSignificanceThreshold
		experimentsStatsLog.Printf("Chi-square balance test for %q: χ²=%.3f df=%d p=%.3f balanced=%v",
			experimentName, a.ChiSquare, a.DegreesOfFreedom, a.PValue, a.IsBalanced)
	} else {
		a.IsBalanced = true
	}
	if k >= 3 {
		a.BonferroniAlpha = 0.05 / float64(k-1)
	}
}

func applyExperimentReadiness(a *ExperimentAnalysis, usesObservations bool) {
	k := len(a.Variants)
	belowCount := 0
	minObserved := math.MaxInt
	for _, variant := range a.Variants {
		if variant.BelowMinSamples {
			belowCount++
		}
		count := variant.Count
		if usesObservations {
			count = variant.ObservationCount
		}
		if count < minObserved {
			minObserved = count
		}
	}
	if belowCount > 0 {
		a.Readiness = ExperimentReadinessCollecting
		a.Recommendation = "EXTEND"
		a.Rationale = fmt.Sprintf("%d of %d variant(s) below min_samples threshold (min observed: %d / %d)",
			belowCount, k, minObserved, a.MinSamples)
		experimentsStatsLog.Printf("Experiment %q recommendation: EXTEND (%d/%d variants below min_samples=%d)",
			a.ExperimentName, belowCount, k, a.MinSamples)
	} else {
		a.Readiness = ExperimentReadinessReady
		a.Recommendation = "READY_FOR_ANALYSIS"
		a.Rationale = fmt.Sprintf("all %d variants have reached min_samples (%d); outcome metric analysis is available",
			k, a.MinSamples)
		experimentsStatsLog.Printf("Experiment %q recommendation: READY_FOR_ANALYSIS (all %d variants above min_samples=%d)",
			a.ExperimentName, k, a.MinSamples)
	}
}

// expectedProportions returns the expected fraction (0.0–1.0) for each variant, in the
// same order as sortedVariantNames. When cfg provides valid per-variant weights that cover
// every name in sortedVariantNames, uses weighted proportions; otherwise returns an equal split.
func expectedProportions(sortedVariantNames []string, cfg *workflow.ExperimentConfig) []float64 {
	k := len(sortedVariantNames)
	if k == 0 {
		return nil
	}

	// Use weights from config when they are well-formed and cover the observed variants.
	if cfg != nil && len(cfg.Weight) == len(cfg.Variants) && len(cfg.Weight) > 0 {
		nameToWeight := make(map[string]float64, len(cfg.Variants))
		totalWeight := 0.0
		for i, name := range cfg.Variants {
			w := float64(cfg.Weight[i])
			nameToWeight[name] = w
			totalWeight += w
		}
		if totalWeight > 0 {
			// Only use weights when every observed variant name has a declared weight.
			result := make([]float64, k)
			allFound := true
			for i, name := range sortedVariantNames {
				w, ok := nameToWeight[name]
				if !ok {
					allFound = false
					break
				}
				result[i] = w / totalWeight
			}
			if allFound {
				return result
			}
		}
	}

	// Default: equal proportions.
	result := make([]float64, k)
	for i := range result {
		result[i] = 1.0 / float64(k)
	}
	return result
}

// chiSquarePValue computes the right-tail p-value P(X ≥ chi2) where X ~ Chi²(df).
// Uses the Wilson-Hilferty normal approximation via math.Erfc.
// Accuracy: good for df ≥ 1 and chi2 in roughly the range [0, 100]; the approximation
// degrades for very large chi2 values (>100) or very small df (df=1 with extreme chi2),
// but is adequate for the balance-testing use case (variants rarely exceed 10, and
// chi2 values outside the critical region are truncated by the significance gate).
// Returns 1.0 for degenerate inputs (chi2 ≤ 0 or df ≤ 0).
func chiSquarePValue(chi2 float64, df int) float64 {
	if chi2 <= 0 || df <= 0 {
		return 1.0
	}

	dfF := float64(df)
	// Wilson-Hilferty approximation: transform chi²/df to a standard normal deviate.
	h := 2.0 / (9.0 * dfF)
	x := math.Pow(chi2/dfF, 1.0/3.0)
	z := (x - (1.0 - h)) / math.Sqrt(h)

	// Right-tail: P(Z > z) = erfc(z / √2) / 2
	return math.Erfc(z/math.Sqrt2) / 2.0
}

// printExperimentAnalyses renders the statistical analyses to stderr.
func printExperimentAnalyses(analyses []ExperimentAnalysis) {
	if len(analyses) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "\nStatistical Analysis:")
	for _, a := range analyses {
		printOneExperimentAnalysis(a)
	}
}

// printOneExperimentAnalysis renders a single experiment analysis to stderr.
func printOneExperimentAnalysis(a ExperimentAnalysis) {
	fmt.Fprintf(os.Stderr, "\n  [%s]\n", a.ExperimentName)
	printExperimentMetadata(a)
	printVariantProgress(a)
	printObservationExclusions(a)
	printBalanceAnalysis(a)
	printOutcomeComparisons(a.Comparisons)
	printGuardrails(a.Guardrails)
	fmt.Fprintf(os.Stderr, "  Readiness  : %s\n", a.Readiness)
	printExperimentRecommendation(a)
	printExperimentDecision(a.ExperimentDecisionResult)
}

func printExperimentMetadata(a ExperimentAnalysis) {
	if a.Hypothesis != "" {
		fmt.Fprintf(os.Stderr, "  Hypothesis : %s\n", a.Hypothesis)
	}
	if a.AnalysisType != "" {
		fmt.Fprintf(os.Stderr, "  Test type  : %s\n", a.AnalysisType)
	}
	if a.Metric != "" {
		if a.MetricQuestion != "" {
			fmt.Fprintf(os.Stderr, "  Metric     : %s — %s\n", a.Metric, a.MetricQuestion)
		} else {
			fmt.Fprintf(os.Stderr, "  Metric     : %s\n", a.Metric)
		}
		if a.MetricEvalResults != nil {
			fmt.Fprintf(
				os.Stderr,
				"  Eval       : YES=%d  NO=%d  UNKNOWN=%d  TOTAL=%d",
				a.MetricEvalResults.Yes,
				a.MetricEvalResults.No,
				a.MetricEvalResults.Unknown,
				a.MetricEvalResults.Total,
			)
			if a.MetricEvalResults.LatestAnswer != "" {
				fmt.Fprintf(os.Stderr, "  (latest: %s", a.MetricEvalResults.LatestAnswer)
				if a.MetricEvalResults.LatestRunID != "" {
					fmt.Fprintf(os.Stderr, " run %s", a.MetricEvalResults.LatestRunID)
				}
				fmt.Fprint(os.Stderr, ")")
			}
			fmt.Fprintln(os.Stderr)
		}
		if a.MetricGraderID != "" {
			fmt.Fprintf(os.Stderr, "  Grader     : %s", a.MetricGraderID)
			if a.MetricType != "" {
				fmt.Fprintf(os.Stderr, " (%s)", a.MetricType)
			}
			fmt.Fprintln(os.Stderr)
		}
	}
	fmt.Fprintf(os.Stderr, "  Min samples: %d per variant\n", a.MinSamples)
}

func printVariantProgress(a ExperimentAnalysis) {
	if a.MetricType != "" {
		fmt.Fprintf(os.Stderr, "\n  %-20s %8s  %8s  %10s  %s\n", "Variant", "Assigned", "Observed", "Mean", "Progress")
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Repeat("─", 66))
		for _, v := range a.Variants {
			progressStr := fmt.Sprintf("%d/%d", v.ObservationCount, v.MinSamples)
			if v.BelowMinSamples {
				progressStr += " ⚠"
			}
			mean := "-"
			if v.Mean != nil {
				mean = fmt.Sprintf("%.4g", *v.Mean)
			}
			fmt.Fprintf(os.Stderr, "  %-20s %8d  %8d  %10s  %s\n",
				v.Name, v.Count, v.ObservationCount, mean, progressStr)
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n  %-20s %6s  %7s  %7s  %s\n", "Variant", "Count", "Obs%", "Exp%", "Progress")
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Repeat("─", 62))
		for _, v := range a.Variants {
			progressStr := fmt.Sprintf("%d/%d", v.Count, v.MinSamples)
			if v.BelowMinSamples {
				progressStr += " ⚠"
			}
			fmt.Fprintf(os.Stderr, "  %-20s %6d  %6.1f%%  %6.1f%%  %s\n",
				v.Name, v.Count, v.ObservedPct, v.ExpectedPct, progressStr)
		}
	}
}

func printObservationExclusions(a ExperimentAnalysis) {
	if a.MetricType == "" {
		return
	}
	printedHeader := false
	for _, variant := range a.Variants {
		for _, excluded := range variant.Excluded {
			if !printedHeader {
				fmt.Fprintln(os.Stderr, "\n  Excluded observations:")
				printedHeader = true
			}
			runSuffix := ""
			if len(excluded.RunIDs) > 0 {
				runSuffix = " (runs " + strings.Join(excluded.RunIDs, ", ") + ")"
			}
			fmt.Fprintf(os.Stderr, "    %s: %d %s%s\n", variant.Name, excluded.Count, excluded.Reason, runSuffix)
		}
	}
}

func printBalanceAnalysis(a ExperimentAnalysis) {
	fmt.Fprintln(os.Stderr)
	if a.TotalRuns > 0 {
		balancedStr := "balanced ✓"
		if !a.IsBalanced {
			balancedStr = "unbalanced ✗"
		}
		fmt.Fprintf(os.Stderr, "  Balance    : χ² = %.3f  df = %d  p = %.3f  (%s)\n",
			a.ChiSquare, a.DegreesOfFreedom, a.PValue, balancedStr)
	} else {
		fmt.Fprintln(os.Stderr, "  Balance    : no data")
	}

	// Bonferroni correction.
	if a.BonferroniAlpha > 0 {
		fmt.Fprintf(os.Stderr, "  Bonferroni : α_adjusted = %.4f (for %d variants, K-1 comparisons)\n",
			a.BonferroniAlpha, len(a.Variants))
	}
}

func printOutcomeComparisons(comparisons []MetricComparison) {
	if len(comparisons) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  Outcome comparisons:")
	for _, comparison := range comparisons {
		fmt.Fprintf(os.Stderr, "    %s vs %s: %s, delta=%+.4g",
			comparison.Variant, comparison.ControlVariant, comparison.AnalysisType, comparison.Delta)
		if comparison.PValue != nil {
			fmt.Fprintf(os.Stderr, ", p=%.4g", *comparison.PValue)
		}
		if comparison.ProbabilitySuperiority != nil {
			fmt.Fprintf(os.Stderr, ", P(variant > control)=%.4g", *comparison.ProbabilitySuperiority)
		}
		if comparison.Error != "" {
			fmt.Fprintf(os.Stderr, " (%s)", comparison.Error)
		}
		fmt.Fprintln(os.Stderr)
	}
}

func printGuardrails(guardrails []GuardrailStatus) {
	if len(guardrails) == 0 {
		return
	}
	parts := make([]string, 0, len(guardrails))
	for _, guardrail := range guardrails {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", guardrail.Name, guardrail.Threshold, strings.ToUpper(string(guardrail.Status))))
	}
	fmt.Fprintf(os.Stderr, "  Guardrails : %s\n", strings.Join(parts, "  •  "))
}

func printExperimentRecommendation(a ExperimentAnalysis) {
	fmt.Fprintln(os.Stderr)
	switch a.Recommendation {
	case "EXTEND":
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("  EXTEND — "+a.Rationale))
	case "READY_FOR_ANALYSIS":
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("  READY FOR ANALYSIS — "+a.Rationale))
	default:
		fmt.Fprintf(os.Stderr, "  %s — %s\n", a.Recommendation, a.Rationale)
	}
}

func printExperimentDecision(result ExperimentDecisionResult) {
	fmt.Fprintln(os.Stderr)
	label := string(result.Decision)
	if result.Candidate != "" && (result.Decision == ExperimentDecisionPromote || result.Decision == ExperimentDecisionReject) {
		label += " " + result.Candidate
	}
	fmt.Fprintf(os.Stderr, "  Decision   : %s\n", label)
	fmt.Fprintf(os.Stderr, "  Reason     : %s (%s)\n", result.DecisionReason, result.ReasonCode)
}
