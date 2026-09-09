package cli

import (
	"context"
	"fmt"
	"maps"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var experimentsLog = logger.New("cli:experiments_command")

type ExperimentsListConfig struct {
	RepoOverride string
	JSONOutput   bool
}

// ExperimentsAnalyzeConfig holds configuration for the experiments analyze subcommand.
type ExperimentsAnalyzeConfig struct {
	ExperimentName string
	RepoOverride   string
	JSONOutput     bool
}

// NewExperimentsCommand creates the experiments command with its subcommands.
func NewExperimentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "List and analyze experiment workflow branches in the repository",
		Long: `List and analyze experiment workflow branches in the repository.

Experiments are tracked via git branches with the "experiments/" prefix (e.g.,
experiments/my-workflow). Each branch stores a state.jsonl or state.json file
written by the workflow's pick_experiment step, containing variant counts and
run history.

Available subcommands:
  - list    - List all experiment workflow branches (default)
  - analyze - Analyze a specific experiment workflow in detail`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` experiments                        # List all experiments (default)
  ` + string(constants.CLIExtensionPrefix) + ` experiments list                   # List all experiments
  ` + string(constants.CLIExtensionPrefix) + ` experiments list --json            # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` experiments analyze my-workflow    # Analyze experiments/my-workflow
  ` + string(constants.CLIExtensionPrefix) + ` experiments analyze my-workflow --json  # Analyze in JSON format`,
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")
			return RunExperimentsList(ExperimentsListConfig{
				RepoOverride: repoOverride,
				JSONOutput:   jsonOutput,
			})
		},
	}

	addJSONFlag(cmd)
	addRepoFlag(cmd)

	cmd.AddCommand(NewExperimentsListSubcommand())
	cmd.AddCommand(NewExperimentsAnalyzeSubcommand())

	return cmd
}

// NewExperimentsListSubcommand creates the experiments list subcommand.
func NewExperimentsListSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all experiment workflow branches",
		Long: `List all experiment workflow branches in the repository.

Reads the state.jsonl/state.json file from each experiments/* branch and shows a summary
of each workflow's A/B experiments: number of experiments defined, total runs,
and timestamp of the most recent run.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` experiments list                             # List all experiments
  ` + string(constants.CLIExtensionPrefix) + ` experiments list --json                      # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` experiments list --repo owner/repo           # List from a specific repository`,
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")
			return RunExperimentsList(ExperimentsListConfig{
				RepoOverride: repoOverride,
				JSONOutput:   jsonOutput,
			})
		},
	}

	addJSONFlag(cmd)
	addRepoFlag(cmd)

	return cmd
}

// NewExperimentsAnalyzeSubcommand creates the experiments analyze subcommand.
func NewExperimentsAnalyzeSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <experiment>",
		Short: "Analyze a specific experiment workflow in detail",
		Long: `Analyze a specific experiment workflow in detail.

The experiment argument is the workflow ID (branch name without the "experiments/"
prefix, e.g., "my-workflow" for the "experiments/my-workflow" branch).

Reads the state.jsonl/state.json file from the branch and shows per-variant counts, total
runs, and the most recent run assignments.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` experiments analyze my-workflow              # Analyze experiments/my-workflow
  ` + string(constants.CLIExtensionPrefix) + ` experiments analyze my-workflow --json       # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` experiments analyze my-workflow --repo owner/repo  # Analyze in a specific repository`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")
			return RunExperimentsAnalyze(ExperimentsAnalyzeConfig{
				ExperimentName: args[0],
				RepoOverride:   repoOverride,
				JSONOutput:     jsonOutput,
			})
		},
	}

	addJSONFlag(cmd)
	addRepoFlag(cmd)

	return cmd
}

// RunExperimentsList lists all experiment branches.
func RunExperimentsList(config ExperimentsListConfig) error {
	experimentsLog.Printf("Listing experiments: repo=%s, json=%v", config.RepoOverride, config.JSONOutput)

	var experiments []ExperimentInfo
	var err error

	if config.RepoOverride != "" {
		experiments, err = fetchRemoteExperiments(config.RepoOverride)
	} else {
		experiments, err = fetchLocalExperiments()
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	if config.JSONOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(experiments, "experiments list")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	if len(experiments) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No experiment workflow branches found (branches matching experiments/* pattern)."))
		return nil
	}

	count := len(experiments)
	if count == 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Found 1 experiment workflow"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d experiment workflows", count)))
	}
	fmt.Fprint(os.Stderr, console.RenderStruct(experiments))

	return nil
}

// RunExperimentsAnalyze analyzes a specific experiment branch.
func RunExperimentsAnalyze(config ExperimentsAnalyzeConfig) error {
	experimentsLog.Printf("Analyzing experiment: name=%s, repo=%s, json=%v",
		config.ExperimentName, config.RepoOverride, config.JSONOutput)

	// Load experiment configs and evals from the workflow frontmatter to enrich the statistical
	// output with hypothesis text, analysis_type, min_samples, guardrail thresholds, and resolved
	// eval metric questions.
	// Config loading is best-effort: failures are silently ignored and analysis falls back to
	// defaults (min_samples=20, equal expected proportions, no hypothesis displayed).
	// This ensures the command remains functional even when the workflow .md file is absent
	// (e.g., when analysing experiments from a remote repository without the workflow checked out).
	frontmatterResult, details, metricEvalResults, err := loadExperimentAnalysisInputs(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	metricObservationSets, cleanup, err := loadMetricObservationSetsForAnalysis(
		details,
		frontmatterResult,
		config.RepoOverride,
	)
	if err != nil {
		return err
	}
	defer cleanup()

	// Compute statistical analyses for each named experiment.
	details.Analyses = computeExperimentAnalysesWithObservationBundle(
		details.Experiments,
		frontmatterResult.ExperimentConfigs,
		frontmatterResult.Evals,
		metricEvalResults,
		metricObservationSets,
	)

	if config.JSONOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(details, "experiment details")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	printExperimentDetails(details)
	return nil
}

func loadExperimentAnalysisInputs(
	config ExperimentsAnalyzeConfig,
) (experimentFrontmatterResult, *ExperimentDetails, map[string]MetricEvalResults, error) {
	branchName := experimentsBranchPrefix + config.ExperimentName
	var frontmatter experimentFrontmatterResult
	var details *ExperimentDetails
	var err error
	if config.RepoOverride != "" {
		frontmatter = loadRemoteExperimentConfigs(config.RepoOverride, config.ExperimentName)
		details, err = fetchRemoteExperimentDetails(config.RepoOverride, branchName, config.ExperimentName)
	} else {
		frontmatter = loadLocalExperimentConfigs(config.ExperimentName)
		details, err = fetchLocalExperimentDetails(branchName, config.ExperimentName)
	}
	if err != nil {
		return frontmatter, nil, nil, err
	}
	experimentsLog.Printf("Loaded %d experiment config(s) for %s", len(frontmatter.ExperimentConfigs), config.ExperimentName)
	reconcileExperimentDetailsWithConfigs(details, frontmatter.ExperimentConfigs)
	if config.RepoOverride != "" {
		return frontmatter, details, loadRemoteMetricEvalResults(config.RepoOverride, details.WorkflowID), nil
	}
	return frontmatter, details, loadLocalMetricEvalResults(details.WorkflowID), nil
}

// reconcileExperimentDetailsWithConfigs filters each experiment's persisted variant counts
// down to the variant labels currently declared in the workflow's frontmatter (variants:
// list). Variant keys are never removed from state.Counts once written, so a workflow that
// renames or removes variants otherwise leaves stale labels (e.g. from an earlier model
// name) mixed in with current ones forever; those stale counts would skew the chi-square
// balance test and the max-count "last selected variant" heuristic. When an experiment has
// no matching config (or the config declares no variants), its counts are left untouched.
func reconcileExperimentDetailsWithConfigs(details *ExperimentDetails, configs map[string]*workflow.ExperimentConfig) {
	if details == nil || len(configs) == 0 {
		return
	}
	for i, exp := range details.Experiments {
		cfg := configs[exp.Name]
		if cfg == nil || len(cfg.Variants) == 0 {
			continue
		}
		filtered := filterDeclaredVariantCounts(exp.Variants, cfg.Variants)
		if stale := staleVariantNames(exp.Variants, filtered); len(stale) > 0 {
			experimentsLog.Printf("Experiment %q: dropping %d stale variant(s) %v (not in declared variants %v)", exp.Name, len(stale), stale, cfg.Variants)
		}
		details.Experiments[i].Variants = filtered
		details.Experiments[i].Total = sumVariantCounts(filtered)
	}
}

// loadMetricObservationSetsForAnalysis resolves grader-backed and eval-backed experiment
// metrics and attributes their per-run measurements to variants using the persisted
// assignment history, returning one merged map of observation sets and a single cleanup
// closure. An experiment's metric references exactly one of a grader or an eval, so the
// two resolved maps never collide.
type experimentMetricObservationSets struct {
	Primary    map[string]*graderMetricObservationSet
	Guardrails map[string]map[string]*graderMetricObservationSet
}

func loadMetricObservationSetsForAnalysis(
	details *ExperimentDetails,
	frontmatter experimentFrontmatterResult,
	repoOverride string,
) (*experimentMetricObservationSets, func(), error) {
	graderSets, graderGuardrails, cleanup, err := loadGraderObservationSetsForAnalysis(details, frontmatter, repoOverride)
	if err != nil {
		return nil, func() {}, err
	}

	evalSets, evalGuardrails, err := loadEvalObservationSetsForAnalysis(details, frontmatter, repoOverride)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	if graderSets == nil {
		graderSets = make(map[string]*graderMetricObservationSet, len(evalSets))
	}
	maps.Copy(graderSets, evalSets)
	if graderGuardrails == nil {
		graderGuardrails = make(map[string]map[string]*graderMetricObservationSet, len(evalGuardrails))
	}
	mergeGuardrailObservationSets(graderGuardrails, evalGuardrails)
	return &experimentMetricObservationSets{Primary: graderSets, Guardrails: graderGuardrails}, cleanup, nil
}

func loadGraderObservationSetsForAnalysis(
	details *ExperimentDetails,
	frontmatter experimentFrontmatterResult,
	repoOverride string,
) (map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, func(), error) {
	refs, err := resolveGraderMetricReferences(frontmatter.ExperimentConfigs, frontmatter.Graders)
	if err != nil {
		return nil, nil, func() {}, err
	}
	guardrailRefs, err := resolveGraderGuardrailMetricReferences(frontmatter.ExperimentConfigs, frontmatter.Graders)
	if err != nil {
		return nil, nil, func() {}, err
	}
	runRefs := observationRunReferences(refs, guardrailRefs)
	if len(runRefs) == 0 {
		return nil, nil, func() {}, nil
	}
	tempDir, err := os.MkdirTemp("", "gh-aw-experiment-graders-*")
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("failed to prepare grader artifact cache: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}
	source := newGitHubGraderRunArtifactSource(tempDir, repoOverride)
	runData := loadGraderRunData(context.Background(), details.Runs, runRefs, source)
	sets := buildGraderMetricObservationSets(details.Experiments, details.Runs, refs, runData, frontmatter.Graders)
	guardrailSets := buildGraderGuardrailObservationSets(details, guardrailRefs, runData, frontmatter.Graders)
	return sets, guardrailSets, cleanup, nil
}

// loadEvalObservationSetsForAnalysis resolves eval-backed experiment metrics and attributes
// their per-run YES/NO answers to variants using the persisted assignment history, mirroring
// the grader observation pipeline so eval-backed metrics get the same statistical comparisons.
func loadEvalObservationSetsForAnalysis(
	details *ExperimentDetails,
	frontmatter experimentFrontmatterResult,
	repoOverride string,
) (map[string]*graderMetricObservationSet, map[string]map[string]*graderMetricObservationSet, error) {
	refs, err := resolveEvalMetricReferences(frontmatter.ExperimentConfigs, frontmatter.Evals)
	if err != nil {
		return nil, nil, err
	}
	guardrailRefs, err := resolveEvalGuardrailMetricReferences(frontmatter.ExperimentConfigs, frontmatter.Evals)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 && len(guardrailRefs) == 0 {
		return nil, nil, nil
	}
	var evalRecords []evalResultRecord
	if repoOverride != "" {
		evalRecords = loadRemoteEvalResultRecords(repoOverride, details.WorkflowID)
	} else {
		evalRecords = loadLocalEvalResultRecords(details.WorkflowID)
	}
	sets := buildEvalMetricObservationSets(details.Experiments, details.Runs, refs, evalRecords)
	guardrailSets := buildEvalGuardrailObservationSets(details, guardrailRefs, evalRecords)
	return sets, guardrailSets, nil
}

func computeExperimentAnalysesWithObservations(
	experiments []ExperimentVariantStats,
	configs map[string]*workflow.ExperimentConfig,
	evals *workflow.EvalsConfig,
	metricEvalResults map[string]MetricEvalResults,
	graderObservationSets map[string]*graderMetricObservationSet,
) []ExperimentAnalysis {
	return computeExperimentAnalysesWithObservationBundle(experiments, configs, evals, metricEvalResults, &experimentMetricObservationSets{
		Primary: graderObservationSets,
	})
}

func computeExperimentAnalysesWithObservationBundle(
	experiments []ExperimentVariantStats,
	configs map[string]*workflow.ExperimentConfig,
	evals *workflow.EvalsConfig,
	metricEvalResults map[string]MetricEvalResults,
	observationSets *experimentMetricObservationSets,
) []ExperimentAnalysis {
	if len(experiments) == 0 {
		return nil
	}
	if observationSets == nil {
		observationSets = &experimentMetricObservationSets{}
	}
	analyses := make([]ExperimentAnalysis, 0, len(experiments))
	for _, exp := range experiments {
		var cfg *workflow.ExperimentConfig
		if configs != nil {
			cfg = configs[exp.Name]
		}
		analyses = append(analyses, computeExperimentAnalysisWithObservationBundle(
			exp,
			cfg,
			evals,
			metricEvalResults,
			observationSets.Primary[exp.Name],
			observationSets.Guardrails[exp.Name],
		))
	}
	return analyses
}

func mergeGuardrailObservationSets(
	target map[string]map[string]*graderMetricObservationSet,
	source map[string]map[string]*graderMetricObservationSet,
) {
	for experimentName, sourceSets := range source {
		if target[experimentName] == nil {
			target[experimentName] = make(map[string]*graderMetricObservationSet)
		}
		maps.Copy(target[experimentName], sourceSets)
	}
}

func observationRunReferences(
	primary map[string]string,
	guardrails map[string]map[string]string,
) map[string]struct{} {
	experimentNames := make(map[string]struct{}, len(primary)+len(guardrails))
	for experimentName := range primary {
		experimentNames[experimentName] = struct{}{}
	}
	for experimentName := range guardrails {
		experimentNames[experimentName] = struct{}{}
	}
	return experimentNames
}

func buildGraderGuardrailObservationSets(
	details *ExperimentDetails,
	refs map[string]map[string]string,
	runData map[string]graderRunData,
	graders *workflow.GradersConfig,
) map[string]map[string]*graderMetricObservationSet {
	result := make(map[string]map[string]*graderMetricObservationSet, len(refs))
	for experimentName, metrics := range refs {
		result[experimentName] = make(map[string]*graderMetricObservationSet, len(metrics))
		for metricName, graderID := range metrics {
			sets := buildGraderMetricObservationSets(
				details.Experiments, details.Runs, map[string]string{experimentName: graderID}, runData, graders,
			)
			result[experimentName][metricName] = sets[experimentName]
		}
	}
	return result
}

func buildEvalGuardrailObservationSets(
	details *ExperimentDetails,
	refs map[string]map[string]string,
	evalRecords []evalResultRecord,
) map[string]map[string]*graderMetricObservationSet {
	result := make(map[string]map[string]*graderMetricObservationSet, len(refs))
	for experimentName, metrics := range refs {
		result[experimentName] = make(map[string]*graderMetricObservationSet, len(metrics))
		for metricName, evalID := range metrics {
			sets := buildEvalMetricObservationSets(
				details.Experiments, details.Runs, map[string]string{experimentName: evalID}, evalRecords,
			)
			result[experimentName][metricName] = sets[experimentName]
		}
	}
	return result
}
