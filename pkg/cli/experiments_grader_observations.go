package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/sourcegraph/conc/pool"
)

const maxGraderResultsBytes = 10 * 1024 * 1024

const (
	exclusionArtifactTooLarge             = "artifact too large"
	exclusionArtifactUnavailable          = "artifact unavailable"
	exclusionAssignmentHistoryUnavailable = "assignment history unavailable"
	exclusionDuplicateAssignment          = "duplicate assignment"
	exclusionGraderFailed                 = "grader failed"
	exclusionGraderMissing                = "grader missing"
	exclusionGraderUnavailable            = "grader unavailable"
	exclusionInvalidValue                 = "value invalid"
	exclusionMalformedArtifact            = "artifact malformed"
	exclusionRunCancelled                 = "run cancelled"
	exclusionRunIncomplete                = "run incomplete"
	exclusionRunUnavailable               = "run unavailable"
)

// GraderMetricObservation records one grader-derived experiment outcome.
type GraderMetricObservation struct {
	RunID        string  `json:"run_id"`
	Variant      string  `json:"variant"`
	GraderID     string  `json:"grader_id"`
	GraderStatus string  `json:"grader_status"`
	Value        float64 `json:"value"`
	Binary       bool    `json:"binary,omitempty"`
}

// ExcludedObservationSummary groups assigned runs that could not produce a usable observation.
type ExcludedObservationSummary struct {
	Reason string   `json:"reason"`
	Count  int      `json:"count"`
	RunIDs []string `json:"run_ids,omitempty"`
}

type graderMetricObservationSet struct {
	// MetricID identifies the grader or eval question backing this observation set.
	MetricID string
	// Direction is the declared grader direction ("higher_is_better" or "lower_is_better").
	// Empty (treated as higher_is_better) for eval-backed metrics.
	Direction  string
	ByVariant  map[string][]GraderMetricObservation
	Exclusions map[string][]ExcludedObservationSummary
}

type graderResultsArtifact struct {
	Version int                    `json:"version"`
	Run     graderArtifactRun      `json:"run"`
	Results []graderArtifactResult `json:"results"`
}

type graderArtifactRun struct {
	ID      string `json:"id"`
	Attempt int    `json:"attempt"`
}

type graderArtifactResult struct {
	ID             string                       `json:"id"`
	Status         string                       `json:"status"`
	Value          json.RawMessage              `json:"value"`
	Implementation graderArtifactImplementation `json:"implementation"`
}

type graderArtifactImplementation struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Digest  string `json:"digest,omitempty"`
}

type graderRunData struct {
	Artifact        *graderResultsArtifact
	ExclusionReason string
}

type graderRunArtifactSource interface {
	Load(context.Context, string) graderRunData
}

type githubGraderRunArtifactSource struct {
	baseDir  string
	hostname string
	owner    string
	repo     string
}

func newGitHubGraderRunArtifactSource(baseDir, repoOverride string) *githubGraderRunArtifactSource {
	params := buildConcurrentDownloadParams(baseDir, false, repoOverride, nil, false, nil)
	return &githubGraderRunArtifactSource{
		baseDir:  baseDir,
		hostname: params.dlHost,
		owner:    params.dlOwner,
		repo:     params.dlRepo,
	}
}

func (s *githubGraderRunArtifactSource) Load(ctx context.Context, runIDText string) graderRunData {
	runID, reason := s.eligibleRunID(ctx, runIDText)
	if reason != "" {
		return graderRunData{ExclusionReason: reason}
	}
	return s.downloadGraderArtifact(ctx, runID, runIDText)
}

func (s *githubGraderRunArtifactSource) eligibleRunID(ctx context.Context, runIDText string) (int64, string) {
	runID, err := strconv.ParseInt(runIDText, 10, 64)
	if err != nil || runID <= 0 {
		return 0, exclusionRunUnavailable
	}
	run, err := fetchWorkflowRunMetadata(ctx, runID, s.owner, s.repo, s.hostname, false)
	if err != nil {
		return 0, exclusionRunUnavailable
	}
	if run.Status != "completed" {
		return 0, exclusionRunIncomplete
	}
	if run.Conclusion == "cancelled" || run.Conclusion == "skipped" {
		return 0, exclusionRunCancelled
	}
	return runID, ""
}

func (s *githubGraderRunArtifactSource) downloadGraderArtifact(ctx context.Context, runID int64, runIDText string) graderRunData {
	runDir := filepath.Join(s.baseDir, runIDText)
	if err := os.MkdirAll(runDir, constants.DirPermPublic); err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	names, err := listRunArtifactNames(ctx, runID, s.owner, s.repo, s.hostname, false)
	if err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	graderArtifacts := make([]string, 0, 2)
	for _, name := range names {
		if artifactMatchesFilter(name, []string{
			constants.AgentArtifactName.String(),
			constants.AgentOutputFallbackArtifactName.String(),
		}) {
			graderArtifacts = append(graderArtifacts, name)
		}
	}
	if len(graderArtifacts) == 0 {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}

	opts := downloadArtifactsOptions{
		runID:     runID,
		outputDir: runDir,
		owner:     s.owner,
		repo:      s.repo,
		hostname:  s.hostname,
	}
	if err := downloadArtifactsByName(ctx, opts, graderArtifacts); err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	if err := flattenUnifiedArtifact(runDir, false); err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	if err := flattenAgentOutputFallbackArtifact(runDir, false); err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	return readGraderResultsArtifact(runDir)
}

func readGraderResultsArtifact(runDir string) graderRunData {
	resultsPath := findGraderFile(runDir, constants.GraderResultsFilename.String())
	if resultsPath == "" {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	info, err := os.Stat(resultsPath)
	if err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	if info.Size() > maxGraderResultsBytes {
		return graderRunData{ExclusionReason: exclusionArtifactTooLarge}
	}
	data, err := os.ReadFile(resultsPath) // #nosec G304 -- path is beneath a tool-created temporary directory
	if err != nil {
		return graderRunData{ExclusionReason: exclusionArtifactUnavailable}
	}
	var artifact graderResultsArtifact
	if err := json.Unmarshal(data, &artifact); err != nil || artifact.Version <= 0 || artifact.Results == nil {
		return graderRunData{ExclusionReason: exclusionMalformedArtifact}
	}
	return graderRunData{Artifact: &artifact}
}

func resolveGraderMetricReferences(configs map[string]*workflow.ExperimentConfig, graders *workflow.GradersConfig) (map[string]string, error) {
	refs := make(map[string]string)
	for experimentName, cfg := range configs {
		if cfg == nil {
			continue
		}
		graderID, isGrader := workflow.ParseExperimentMetricGraderReference(cfg.Metric)
		if !isGrader {
			continue
		}
		if graderID == "" || !hasValidGraderMetricSuffix(cfg.Metric) {
			return nil, fmt.Errorf("experiments.%s.metric: expected grader reference format grader:<grader_id> or graders.<grader_id>.value", experimentName)
		}
		if graders == nil {
			return nil, fmt.Errorf("experiments.%s.metric: references grader %q but no graders are declared", experimentName, graderID)
		}
		def, ok := graders.Graders[graderID]
		if !ok || def == nil {
			return nil, fmt.Errorf("experiments.%s.metric: references unknown grader %q", experimentName, graderID)
		}
		if def.Enabled != nil && !*def.Enabled {
			return nil, fmt.Errorf("experiments.%s.metric: references disabled grader %q", experimentName, graderID)
		}
		refs[experimentName] = graderID
	}
	return refs, nil
}

func resolveGraderGuardrailMetricReferences(
	configs map[string]*workflow.ExperimentConfig,
	graders *workflow.GradersConfig,
) (map[string]map[string]string, error) {
	refs := make(map[string]map[string]string)
	for experimentName, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, guardrail := range cfg.GuardrailMetrics {
			graderID, isGrader := workflow.ParseExperimentMetricGraderReference(guardrail.Name)
			if !isGrader {
				continue
			}
			if graderID == "" || !hasValidGraderMetricSuffix(guardrail.Name) {
				return nil, fmt.Errorf(
					"experiments.%s.guardrail_metrics: expected grader reference format grader:<grader_id> or graders.<grader_id>.value",
					experimentName,
				)
			}
			if graders == nil {
				return nil, fmt.Errorf(
					"experiments.%s.guardrail_metrics: references grader %q but no graders are declared",
					experimentName, graderID,
				)
			}
			def, ok := graders.Graders[graderID]
			if !ok || def == nil {
				return nil, fmt.Errorf(
					"experiments.%s.guardrail_metrics: references unknown grader %q",
					experimentName, graderID,
				)
			}
			if def.Enabled != nil && !*def.Enabled {
				return nil, fmt.Errorf(
					"experiments.%s.guardrail_metrics: references disabled grader %q",
					experimentName, graderID,
				)
			}
			if refs[experimentName] == nil {
				refs[experimentName] = make(map[string]string)
			}
			refs[experimentName][guardrail.Name] = graderID
		}
	}
	return refs, nil
}

// hasValidGraderMetricSuffix rejects dotted grader references with a suffix other than
// "value" (e.g. "graders.score.passed" or a typo such as "graders.score.vaule"), since this
// implementation only supports primary .value metrics.
func hasValidGraderMetricSuffix(metric string) bool {
	trimmed := strings.TrimSpace(metric)
	rest, ok := strings.CutPrefix(trimmed, "graders.")
	if !ok {
		return true
	}
	parts := strings.SplitN(strings.TrimSpace(rest), ".", 2)
	return len(parts) < 2 || parts[1] == "value"
}

func loadGraderRunData(ctx context.Context, runs []ExperimentRunRecord, experimentNames map[string]struct{}, source graderRunArtifactSource) map[string]graderRunData {
	runIDs := make(map[string]struct{})
	for _, run := range runs {
		for experimentName := range experimentNames {
			if _, assigned := run.Assignments[experimentName]; assigned {
				runIDs[run.RunID] = struct{}{}
				break
			}
		}
	}

	result := make(map[string]graderRunData, len(runIDs))
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

func buildGraderMetricObservationSets(
	experiments []ExperimentVariantStats,
	runs []ExperimentRunRecord,
	refs map[string]string,
	runData map[string]graderRunData,
	graders *workflow.GradersConfig,
) map[string]*graderMetricObservationSet {
	state := initializeGraderObservationSets(experiments, refs, graders)
	for _, run := range runs {
		for experimentName, graderID := range refs {
			appendGraderObservation(run, experimentName, graderID, state, runData)
		}
	}
	addMissingAssignmentHistory(state)
	return state.sets
}

// graderObservationState bundles the mutable state accumulated while joining assignment
// history with grader (or eval) run data into per-variant observation sets.
type graderObservationState struct {
	assignedCounts map[string]map[string]int
	sets           map[string]*graderMetricObservationSet
	recordedCounts map[string]map[string]int
	seen           map[string]map[string]struct{}
}

func initializeGraderObservationSets(
	experiments []ExperimentVariantStats,
	refs map[string]string,
	graders *workflow.GradersConfig,
) *graderObservationState {
	state := &graderObservationState{
		assignedCounts: make(map[string]map[string]int, len(experiments)),
		sets:           make(map[string]*graderMetricObservationSet, len(refs)),
		recordedCounts: make(map[string]map[string]int, len(refs)),
		seen:           make(map[string]map[string]struct{}, len(refs)),
	}
	for _, exp := range experiments {
		state.assignedCounts[exp.Name] = exp.Variants
	}
	for experimentName, metricID := range refs {
		state.sets[experimentName] = &graderMetricObservationSet{
			MetricID:   metricID,
			Direction:  graderDirection(graders, metricID),
			ByVariant:  make(map[string][]GraderMetricObservation),
			Exclusions: make(map[string][]ExcludedObservationSummary),
		}
		state.recordedCounts[experimentName] = make(map[string]int)
		state.seen[experimentName] = make(map[string]struct{})
	}
	return state
}

// graderDirection returns the declared direction ("higher_is_better" or "lower_is_better")
// for a grader ID, or "" (treated as higher_is_better) when graders is nil or the grader
// is not declared (e.g. for eval-backed metric IDs, which have no direction).
func graderDirection(graders *workflow.GradersConfig, graderID string) string {
	if graders == nil {
		return ""
	}
	if def, ok := graders.Graders[graderID]; ok && def != nil {
		return def.Direction
	}
	return ""
}

func appendGraderObservation(
	run ExperimentRunRecord,
	experimentName string,
	graderID string,
	state *graderObservationState,
	runData map[string]graderRunData,
) {
	variant, assigned := run.Assignments[experimentName]
	if !assigned {
		return
	}
	state.recordedCounts[experimentName][variant]++
	set := state.sets[experimentName]
	if _, duplicate := state.seen[experimentName][run.RunID]; duplicate {
		addObservationExclusion(set, variant, exclusionDuplicateAssignment, run.RunID, 1)
		return
	}
	state.seen[experimentName][run.RunID] = struct{}{}
	data, ok := runData[run.RunID]
	if !ok || data.ExclusionReason != "" {
		reason := data.ExclusionReason
		if reason == "" {
			reason = exclusionArtifactUnavailable
		}
		addObservationExclusion(set, variant, reason, run.RunID, 1)
		return
	}
	observation, reason := extractGraderObservation(data.Artifact, run.RunID, variant, graderID)
	if reason != "" {
		addObservationExclusion(set, variant, reason, run.RunID, 1)
		return
	}
	set.ByVariant[variant] = append(set.ByVariant[variant], observation)
}

func addMissingAssignmentHistory(state *graderObservationState) {
	for experimentName, variants := range state.assignedCounts {
		set, ok := state.sets[experimentName]
		if !ok {
			continue
		}
		for variant, assigned := range variants {
			missingHistory := assigned - state.recordedCounts[experimentName][variant]
			if missingHistory > 0 {
				addObservationExclusion(set, variant, exclusionAssignmentHistoryUnavailable, "", missingHistory)
			}
		}
	}
}

func addObservationExclusion(set *graderMetricObservationSet, variant, reason, runID string, count int) {
	summaries := set.Exclusions[variant]
	for i := range summaries {
		if summaries[i].Reason == reason {
			summaries[i].Count += count
			if runID != "" {
				summaries[i].RunIDs = append(summaries[i].RunIDs, runID)
			}
			set.Exclusions[variant] = summaries
			return
		}
	}
	summary := ExcludedObservationSummary{Reason: reason, Count: count}
	if runID != "" {
		summary.RunIDs = []string{runID}
	}
	set.Exclusions[variant] = append(summaries, summary)
}

func extractGraderObservation(artifact *graderResultsArtifact, runID, variant, graderID string) (GraderMetricObservation, string) {
	if artifact == nil {
		return GraderMetricObservation{}, exclusionMalformedArtifact
	}
	var match *graderArtifactResult
	for i := range artifact.Results {
		if artifact.Results[i].ID != graderID {
			continue
		}
		if match != nil {
			return GraderMetricObservation{}, exclusionMalformedArtifact
		}
		match = &artifact.Results[i]
	}
	if match == nil {
		return GraderMetricObservation{}, exclusionGraderMissing
	}
	switch match.Status {
	case "error":
		return GraderMetricObservation{}, exclusionGraderFailed
	case "unavailable":
		return GraderMetricObservation{}, exclusionGraderUnavailable
	case "pass", "fail":
	default:
		return GraderMetricObservation{}, exclusionMalformedArtifact
	}

	var value float64
	var binary bool
	if string(match.Value) == "null" || len(match.Value) == 0 {
		return GraderMetricObservation{}, exclusionInvalidValue
	}
	if err := json.Unmarshal(match.Value, &value); err != nil {
		var boolValue bool
		if err := json.Unmarshal(match.Value, &boolValue); err != nil {
			return GraderMetricObservation{}, exclusionInvalidValue
		}
		binary = true
		if boolValue {
			value = 1
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return GraderMetricObservation{}, exclusionInvalidValue
	}
	return GraderMetricObservation{
		RunID:        runID,
		Variant:      variant,
		GraderID:     graderID,
		GraderStatus: match.Status,
		Value:        value,
		Binary:       binary,
	}, ""
}
