package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const parallelExperimentsJobName = "parallel_experiments"
const parallelExperimentDirectory = "${{ runner.temp }}/gh-aw/parallel-experiment"

type parallelExperimentEntry struct {
	ID             string `json:"id,omitempty" yaml:"id"`
	Experiment     string `json:"experiment" yaml:"experiment"`
	Variant        string `json:"variant" yaml:"variant"`
	Engine         string `json:"engine,omitempty" yaml:"engine"`
	Model          string `json:"model,omitempty" yaml:"model"`
	Harness        string `json:"harness,omitempty" yaml:"harness"`
	MaxAICredits   int    `json:"max_ai_credits,omitempty" yaml:"max_ai_credits"`
	TimeoutMinutes int    `json:"timeout_minutes,omitempty" yaml:"timeout_minutes"`
	Admitted       bool   `json:"admitted" yaml:"admitted"`
	SkipReason     string `json:"skip_reason,omitempty" yaml:"skip_reason,omitempty"`
}

type parallelExperimentsConfig struct {
	Include           []parallelExperimentEntry `json:"include"`
	MaxJobs           int                       `json:"max-jobs"`
	MaxParallel       int                       `json:"max-parallel"`
	TimeoutMinutes    int                       `json:"timeout-minutes"`
	MaxAICredits      int                       `json:"max-ai-credits"`
	MaxTotalAICredits int                       `json:"max-total-ai-credits"`
}

func parseParallelExperiments(data *WorkflowData) (*parallelExperimentsConfig, error) { //nolint:largefunc // Admission and reference validation remain adjacent to the bounded configuration decode.
	raw, exists := data.RawFrontmatter["parallel-experiments"]
	if !exists || raw == false {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	config := parallelExperimentsConfig{MaxJobs: 4, MaxParallel: 2, TimeoutMinutes: 10, MaxAICredits: 100, MaxTotalAICredits: 400}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("parallel-experiments: expected an include list and bounded execution controls: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("parallel-experiments: expected exactly one configuration object")
	}
	for _, bound := range []struct {
		name       string
		value, max int
	}{
		{"max-jobs", config.MaxJobs, 16}, {"max-parallel", config.MaxParallel, 4},
		{"timeout-minutes", config.TimeoutMinutes, 30}, {"max-ai-credits", config.MaxAICredits, 1000},
		{"max-total-ai-credits", config.MaxTotalAICredits, 4000},
	} {
		if bound.value < 1 || bound.value > bound.max {
			return nil, fmt.Errorf("parallel-experiments.%s: expected an integer from 1 to %d", bound.name, bound.max)
		}
	}
	if len(config.Include) == 0 || len(config.Include) > 16 {
		return nil, errors.New("parallel-experiments.include: provide between 1 and 16 entries")
	}
	admitted := min(config.MaxJobs, config.MaxParallel, config.MaxTotalAICredits/config.MaxAICredits)
	if admitted == 0 {
		return nil, errors.New("parallel-experiments: max-total-ai-credits must admit at least one per-job budget")
	}
	for i := range config.Include {
		entry := &config.Include[i]
		if !slices.Contains(data.Experiments[entry.Experiment], entry.Variant) {
			return nil, fmt.Errorf("parallel-experiments.include[%d]: variant %q must belong to the existing experiments.%s definition", i, entry.Variant, entry.Experiment)
		}
		if strings.ContainsAny(entry.Variant, "\"'\r\n") || containsExpression(entry.Variant) {
			return nil, errors.New("parallel-experiments: variants must not contain quotes, line breaks or expression delimiters")
		}
		if definition := data.ExperimentConfigs[entry.Experiment]; definition != nil &&
			(definition.StartDate != "" || definition.EndDate != "" || definition.Continual != nil) {
			return nil, errors.New("parallel-experiments: scheduled or continual experiment definitions are not supported")
		}
		if entry.Engine == "" {
			entry.Engine = data.AI
		}
		if entry.Engine != "copilot" {
			return nil, errors.New("parallel-experiments: only the sandboxed copilot engine is currently supported")
		}
		if entry.Harness == "" {
			entry.Harness = copilotEngineHarness("copilot", data)
		}
		if entry.Harness != "copilot-cli" && entry.Harness != "copilot-sdk" {
			return nil, errors.New("parallel-experiments: harness must be copilot-cli or copilot-sdk; custom harness code is not isolated")
		}
		if containsExpression(entry.Model) || strings.ContainsAny(entry.Model, "\r\n") {
			return nil, errors.New("parallel-experiments: model overrides must be literal model names, not expressions")
		}
		if entry.Model == "" {
			entry.Model = parallelExperimentReferences(data.Model, *entry, data.Experiments)
		}
		entry.ID = fmt.Sprintf("entry-%d", i+1)
		entry.MaxAICredits = config.MaxAICredits
		entry.TimeoutMinutes = config.TimeoutMinutes
		entry.Admitted = i < admitted
		if !entry.Admitted {
			entry.SkipReason = "admission-limit"
		}
	}
	return &config, nil
}

// The normal document is already finalized. This pass only appends a job; it
// cannot add dependencies, outputs, detection, or result consumers to production.
func (c *Compiler) appendParallelExperiments(production string, data *WorkflowData) (string, error) { //nolint:largefunc // The additive pass keeps production preservation, admission and final serialization in one ordered sequence.
	config, err := parseParallelExperiments(data)
	if err != nil || config == nil {
		return production, err
	}
	if err := c.validateParallelExperimentIsolation(data); err != nil {
		return "", err
	}
	var productionDocument yaml.Node
	if err := yaml.Unmarshal([]byte(production), &productionDocument); err != nil {
		return "", err
	}
	if len(productionDocument.Content) == 0 {
		return "", errors.New("parallel-experiments: production workflow document is empty")
	}
	productionJob := artifactMappingValue(artifactMappingValue(productionDocument.Content[0], "jobs"), "agent") //nolint:uncheckedsliceindex // Empty documents are rejected immediately above.
	if productionJob == nil {
		return "", errors.New("parallel-experiments: production agent job is required")
	}
	if _, exists := c.jobManager.GetJob(parallelExperimentsJobName); exists {
		return "", errors.New("parallel-experiments: job id parallel_experiments is reserved")
	}
	var branches []*yaml.Node
	var experimentJob *yaml.Node
	var admittedEntries []parallelExperimentEntry
	for _, entry := range config.Include {
		if !entry.Admitted {
			continue
		}
		admittedEntries = append(admittedEntries, entry)
		job, err := c.buildParallelExperimentJob(data, entry)
		if err != nil {
			return "", fmt.Errorf("parallel-experiments.%s: %w", entry.ID, err)
		}
		node, err := parallelJobNode(job)
		if err != nil {
			return "", err
		}
		if experimentJob == nil {
			experimentJob = node
		}
		steps := artifactMappingValue(node, "steps")
		env := artifactMappingValue(node, "env")
		if err := scopeParallelExperimentSteps(steps, env, entry); err != nil {
			return "", err
		}
		branches = append(branches, steps.Content...)
	}
	setParallelNode(experimentJob, "steps", &yaml.Node{Kind: yaml.SequenceNode, Content: branches})
	setParallelNode(experimentJob, "name", parallelScalar("Parallel experiment (${{ matrix.id }})"))
	if condition := artifactMappingValue(productionJob, "if"); condition != nil {
		setParallelNode(experimentJob, "if", condition)
	}
	if needs := artifactMappingValue(productionJob, "needs"); needs != nil {
		setParallelNode(experimentJob, "needs", needs)
	}
	setParallelNode(experimentJob, "continue-on-error", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	setParallelNode(experimentJob, "timeout-minutes", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(config.TimeoutMinutes)})
	var strategy yaml.Node
	if err := strategy.Encode(map[string]any{"fail-fast": false, "max-parallel": config.MaxParallel, "matrix": map[string]any{"include": admittedEntries}}); err != nil {
		return "", err
	}
	setParallelNode(experimentJob, "strategy", &strategy)
	var env yaml.Node
	admission, err := json.Marshal(config.Include)
	if err != nil {
		return "", err
	}
	if err := env.Encode(map[string]string{
		"GH_AW_PARALLEL_EXPERIMENT": "${{ toJSON(matrix) }}",
		"GH_AW_PARALLEL_ADMISSION":  inertJSON(admission),
	}); err != nil {
		return "", err
	}
	setParallelNode(experimentJob, "env", &env)
	removeParallelNode(experimentJob, "outputs", "concurrency")
	appended := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{parallelScalar(parallelExperimentsJobName), experimentJob}}
	var rendered strings.Builder
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	if err := encoder.Encode(appended); err != nil {
		return "", err
	}
	var suffix strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(rendered.String(), "\n"), "\n") {
		suffix.WriteString("  " + line + "\n")
	}
	finalized, err := finalizeRunnerTempSafety(suffix.String())
	if err != nil {
		return "", err
	}
	return production + finalized, nil
}

func (c *Compiler) buildParallelExperimentJob(data *WorkflowData, entry parallelExperimentEntry) (*Job, error) { //nolint:largefunc // Isolated generation and instrumentation are ordered to preserve compiler security boundaries.
	compiler := *c
	compiler.dryRun = true
	compiler.inlinePrompt = true
	compiler.stepOrderTracker = NewStepOrderTracker()
	compiler.jobManager = NewJobManager()
	isolated := compiler.dryRunWorkflowData(data)
	isolated.OTLPEndpoint = data.OTLPEndpoint
	isolated.OTLPHeaders = data.OTLPHeaders
	isolated.OTLPEndpoints = data.OTLPEndpoints
	isolated.OTLPUsesEnterpriseDefaults = data.OTLPUsesEnterpriseDefaults
	if observability, ok := data.RawFrontmatter["observability"]; ok {
		isolated.RawFrontmatter["observability"] = observability
	}
	if isolated.ParsedFrontmatter != nil {
		isolated.ParsedFrontmatter.Observability = data.ParsedFrontmatter.Observability
	}
	// The staged copy has already removed compiler-managed mutation settings.
	// Retain normal host telemetry; cache writes are prohibited independently.
	isolated.DryRun = false
	isolated.AdmissionEnabled = false
	isolated.generatedActionRefs = make(map[string]struct{})
	isolated.ActionResolutionFailures = slices.Clone(data.ActionResolutionFailures)
	isolated.EngineConfig = new(EngineConfig)
	*isolated.EngineConfig = *data.EngineConfig
	isolated.EngineConfig.Env = maps.Clone(data.EngineConfig.Env)
	isolated.EngineConfig.CopilotSDK = entry.Harness == "copilot-sdk"
	isolated.EngineConfig.MaxAICredits = int64(entry.MaxAICredits)
	isolated.EngineConfig.ModelRouting = nil
	isolated.EngineConfig.ModelRoutingDisabled = true
	isolated.EngineConfig.Bare = true
	dynamic := false
	isolated.EngineConfig.DynamicWorkflows = &dynamic
	isolated.Model = entry.Model
	isolated.TimeoutMinutes = strconv.Itoa(entry.TimeoutMinutes)
	isolated.Permissions = "permissions:\n  contents: read\n"
	isolated.CachedPermissions = nil
	isolated.DeferredPromptExpressions = nil
	isolated.CompiledPromptMarkdown = nil
	isolated.Tools = maps.Clone(isolated.Tools)
	isolated.Tools["github"] = false
	if isolated.ParsedTools != nil {
		tools := *isolated.ParsedTools
		tools.GitHub = nil
		isolated.ParsedTools = &tools
	}
	job, err := compiler.buildMainJob(isolated, true)
	if err != nil {
		return nil, err
	}
	job.CacheMode = "read"
	var preparation strings.Builder
	preparation.WriteString(parallelExperimentRuntimeStep("prepare", ""))
	if err := compiler.generatePrompt(&preparation, isolated, false, nil); err != nil {
		return nil, err
	}
	var collection strings.Builder
	collection.WriteString(parallelExperimentRuntimeStep("finalize", "always()"))
	compiler.generateTrackedSecretRedactionStep(&collection, isolated.Env+strings.Join(job.Steps, ""), isolated, true)
	collectionSteps := strings.NewReplacer(
		"id: redact_secrets", "id: redact_parallel_experiment",
		"id: validate_recovery_envelopes", "id: validate_parallel_recovery_envelopes",
		"await main();", "await main({ additionalDirectories: [process.env.GH_AW_PARALLEL_EXPERIMENT_DIR] });",
	).Replace(collection.String())
	job.Name = parallelExperimentsJobName
	node, err := parallelJobNode(job)
	if err != nil {
		return nil, err
	}
	steps := artifactMappingValue(node, "steps")
	var injected []*yaml.Node
	prepared, collected := false, false
	for _, step := range steps.Content {
		name := artifactMappingValue(step, "name")
		if name != nil && name.Value == "Upload agent artifacts" {
			additional, err := parallelStepNodes(collectionSteps)
			if err != nil {
				return nil, err
			}
			injected = append(injected, additional...)
			collected = true
			condition := artifactMappingValue(step, "if")
			condition.Value = strings.Join([]string{condition.Value, " && steps.redact_parallel_experiment.outcome == 'success'"}, "")
		}
		injected = append(injected, step)
		if name != nil && name.Value == "Download activation artifact" {
			additional, err := parallelStepNodes(preparation.String())
			if err != nil {
				return nil, err
			}
			injected = append(injected, additional...)
			prepared = true
		}
	}
	if !prepared || !collected {
		return nil, errors.New("generated agent lacks required activation restore or guarded artifact boundary")
	}
	steps.Content = injected
	var serialized strings.Builder
	encoder := yaml.NewEncoder(&serialized)
	encoder.SetIndent(2)
	if err := encoder.Encode(steps); err != nil {
		return nil, err
	}
	var stepsText strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(serialized.String(), "\n"), "\n") {
		stepsText.WriteString("      " + line + "\n")
	}
	job.Steps = []string{stepsText.String()}
	for i, step := range job.Steps {
		step = strings.NewReplacer(
			resolveGitHubToken(""), "${{ github.token }}",
			resolveSafeOutputGitHubToken(""), "${{ github.token }}",
			"${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}", "${{ github.token }}",
			"${{ secrets.GH_AW_GITHUB_TOKEN }}", "${{ github.token }}",
		).Replace(step)
		job.Steps[i] = parallelExperimentReferences(step, entry, data.Experiments)
	}

	return job, nil
}

func parallelStepNodes(text string) ([]*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte("steps:\n"+text), &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return nil, errors.New("parallel-experiments: generated step document is empty")
	}
	return artifactMappingValue(document.Content[0], "steps").Content, nil //nolint:uncheckedsliceindex // Empty documents are rejected immediately above.
}

var parallelExperimentReferencePattern = regexp.MustCompile(`\b(?:needs\.activation\.outputs\.|steps\.pick-experiment\.outputs\.|experiments\.)([A-Za-z_][A-Za-z0-9_]*)\b`)

func parallelExperimentReferences(text string, entry parallelExperimentEntry, experiments map[string][]string) string {
	return parallelExperimentReferencePattern.ReplaceAllStringFunc(text, func(reference string) string {
		name := reference[strings.LastIndexByte(reference, '.')+1:]
		if _, declared := experiments[name]; !declared {
			return reference
		}
		if name == entry.Experiment {
			return "'" + strings.ReplaceAll(entry.Variant, "'", "''") + "'"
		}
		return "needs.activation.outputs." + name
	})
}

func parallelExperimentRuntimeStep(mode, condition string) string {
	var step strings.Builder
	step.WriteString("      - name: Parallel experiment " + mode + "\n")
	if condition != "" {
		step.WriteString("        if: " + condition + "\n")
	}
	step.WriteString("        env:\n          GH_AW_PARALLEL_EXPERIMENT_DIR: " + parallelExperimentDirectory + "\n")
	if mode == "finalize" {
		step.WriteString("          GH_AW_PARALLEL_STEPS: ${{ toJSON(steps) }}\n")
	}
	step.WriteString("        run: node \"${RUNNER_TEMP}/gh-aw/actions/parallel_experiment.cjs\" " + mode + "\n")
	return step.String()
}
