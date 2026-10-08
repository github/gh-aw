package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

func workQueuePreparedJobName(name string, index int) string {
	return fmt.Sprintf("work_queue_prepare_%s_%d", name, index)
}

func (c *Compiler) buildWorkQueuePreparedAdapterJobs(data *WorkflowData, threatDetectionEnabled bool) ([]string, error) {
	var jobs []string
	for _, name := range workQueuePreparedAdapterNames(data) {
		adapter := data.SafeOutputs.ClaimAdapters[name]
		configuration, err := json.Marshal(adapter)
		if err != nil {
			return nil, err
		}
		for index := range 16 {
			job, err := c.buildWorkQueuePreparedAdapterJob(data, name, index, configuration, threatDetectionEnabled)
			if err != nil {
				return nil, err
			}
			if err := c.jobManager.AddJob(job); err != nil {
				return nil, err
			}
			jobs = append(jobs, job.Name)
		}
	}
	return jobs, nil
}

func (c *Compiler) buildWorkQueuePreparedAdapterJob(data *WorkflowData, name string, index int, configuration []byte, threatDetectionEnabled bool) (*Job, error) {
	job := c.newWorkQueuePreparedAdapterJob(data, name, index, threatDetectionEnabled)
	steps := c.buildWorkQueuePreparedAdapterSetup(data, name, index, configuration)
	customSteps := workQueuePreparedCustomJobSteps(data, name, job)
	if actionSteps := workQueuePreparedCustomActionSteps(data, name); actionSteps != nil {
		customSteps = actionSteps
	}
	if name == "raw_steps" {
		customSteps = data.SafeOutputs.Steps
	}
	scriptSteps, scriptSetup, err := c.buildWorkQueuePreparedCustomScriptSteps(data, name)
	if err != nil {
		return nil, err
	}
	steps = append(steps, scriptSetup...)
	if scriptSteps != nil {
		customSteps = scriptSteps
	}
	if len(customSteps) == 0 {
		return nil, fmt.Errorf("work-queue: prepared adapter %q has no matching custom job/action/raw-step executable", name)
	}
	rendered, err := c.renderWorkQueuePreparedCustomSteps(data, name, customSteps)
	if err != nil {
		return nil, err
	}
	steps = append(steps, rendered...)
	job.Steps = c.finishWorkQueuePreparedAdapterSteps(data, name, index, steps)
	return job, nil
}

func (c *Compiler) newWorkQueuePreparedAdapterJob(data *WorkflowData, name string, index int, threatDetectionEnabled bool) *Job {
	jobName := workQueuePreparedJobName(name, index)
	job := &Job{
		Name: jobName, RunsOn: c.formatFrameworkJobRunsOn(data),
		Needs:          []string{string(constants.AgentJobName), string(constants.ActivationJobName)},
		If:             "always() && needs.agent.result == 'success' && needs.activation.result == 'success'",
		Permissions:    "permissions:\n      contents: read\n      actions: read",
		TimeoutMinutes: 10,
		Env: map[string]string{
			"GH_AW_WORK_QUEUE_ENABLED":  `"true"`,
			"GH_AW_WORK_QUEUE_SNAPSHOT": fmt.Sprintf("%q", constants.WorkQueueSnapshotPath),
		},
	}
	if threatDetectionEnabled {
		job.Needs = append(job.Needs, string(constants.DetectionJobName))
		job.If += " && (needs.detection.result == 'success' || needs.detection.result == 'skipped')"
	}
	for key := range parseEnvYAMLSection(data.Env) {
		if (!strings.HasPrefix(key, "GITHUB_") || key == "GITHUB_TOKEN") && key != "GH_AW_WORK_QUEUE_ENABLED" && key != "GH_AW_WORK_QUEUE_SNAPSHOT" {
			job.Env[key] = `""`
		}
	}
	job.Outputs = map[string]string{"artifact_id": "${{ steps.claim_adapter_artifact.outputs.artifact-id }}"}
	return job
}

func (c *Compiler) buildWorkQueuePreparedAdapterSetup(data *WorkflowData, name string, index int, configuration []byte) []string {
	preparationData := *data
	preparationData.RawFrontmatter = nil
	preparationData.ParsedFrontmatter = nil
	preparationData.OTLPEndpoint = ""
	preparationData.OTLPHeaders = ""
	preparationData.OTLPEndpoints = ""
	preparationData.OTLPUsesEnterpriseDefaults = false
	preparationData.Env = ""
	steps := c.buildSafeOutputsSetupSteps(&preparationData)
	steps = append(steps, buildAgentOutputDownloadSteps(artifactPrefixExprForAgentDownstreamJob(data), c.getActionPin)...)
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: artifactPrefixExprForActivationJob(data) + constants.ActivationArtifactName.String(),
		DownloadPath: constants.TmpGhAwDirSlash, StepName: "Download immutable Claim adapter snapshot",
	}, c.getActionPin)...)
	steps = append(steps,
		"      - name: Prepare isolated Claim adapter context\n",
		"        id: claim_adapter_context\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
		"        env:\n",
		fmt.Sprintf("          GH_AW_CLAIM_ADAPTER_TYPE: %q\n", name),
		fmt.Sprintf("          GH_AW_CLAIM_ADAPTER_CONFIG: %q\n", string(configuration)),
		fmt.Sprintf("          GH_AW_CLAIM_ADAPTER_INDEX: %q\n", strconv.Itoa(index)),
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_prepare_claim_adapter.cjs');\n",
		"            await main({ core, github, context });\n",
	)
	return steps
}

func workQueuePreparedCustomJobSteps(data *WorkflowData, name string, job *Job) []any {
	var customSteps []any
	for rawName, config := range data.SafeOutputs.Jobs {
		if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
			customSteps = config.Steps
			for key, value := range config.Env {
				job.Env[key] = fmt.Sprintf("%q", value)
			}
			if config.RunsOn != "" {
				job.RunsOn = config.RunsOn
			}
			break
		}
	}
	return customSteps
}

func workQueuePreparedCustomActionSteps(data *WorkflowData, name string) []any {
	var customSteps []any
	for rawName, config := range data.SafeOutputs.Actions {
		if stringutil.NormalizeSafeOutputIdentifier(rawName) != name {
			continue
		}
		uses := config.ResolvedRef
		if uses == "" {
			uses = config.Uses
		}
		custom := map[string]any{"uses": uses, "env": config.Env}
		inputs := make(map[string]any)
		for input, definition := range config.Inputs {
			if !isGitHubExpressionDefault(definition) {
				inputs[input] = fmt.Sprintf("${{ fromJSON(steps.claim_adapter_context.outputs.payload).%s }}", input)
			} else {
				inputs[input] = definition.Default
			}
		}
		if len(inputs) == 0 {
			inputs["payload"] = "${{ steps.claim_adapter_context.outputs.payload }}"
		}
		custom["with"] = inputs
		customSteps = []any{custom}
	}
	return customSteps
}

func (c *Compiler) buildWorkQueuePreparedCustomScriptSteps(data *WorkflowData, name string) ([]any, []string, error) {
	var customSteps []any
	var steps []string
	for rawName, config := range data.SafeOutputs.Scripts {
		if stringutil.NormalizeSafeOutputIdentifier(rawName) != name {
			continue
		}
		scriptSteps, err := buildCustomScriptFilesStep(map[string]*SafeScriptConfig{rawName: config})
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, scriptSteps...)
		customSteps = []any{map[string]any{
			"name": "Prepare Claim script payload without write credentials",
			"uses": c.getActionPin("actions/github-script"),
			"env":  map[string]any{"GH_AW_CLAIM_SCRIPT_FILENAME": safeOutputScriptFilename(name)},
			"with": map[string]any{"script": "const { main } = require(`${process.env.RUNNER_TEMP}/gh-aw/actions/work_queue_prepare_claim_script.cjs`);\nawait main();"},
		}}
	}
	return customSteps, steps, nil
}

func (c *Compiler) renderWorkQueuePreparedCustomSteps(data *WorkflowData, name string, customSteps []any) ([]string, error) {
	var steps []string
	for _, value := range customSteps {
		step, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("work-queue: prepared adapter %q contains a non-object step", name)
		}
		step = shallowCopyMap(step)
		condition := "steps.claim_adapter_context.outputs.active == 'true'"
		if existing, ok := step["if"].(string); ok && existing != "" {
			condition = fmt.Sprintf("%s && (%s)", condition, c.extractExpressionFromIfString(existing))
		}
		step["if"] = condition
		typed, err := MapToStep(step)
		if err != nil {
			return nil, err
		}
		pinned, err := applyActionPinToTypedStep(typed, data)
		if err != nil {
			return nil, err
		}
		rendered, err := ConvertStepToYAML(pinned.ToMap())
		if err != nil {
			return nil, err
		}
		steps = append(steps, rendered)
	}
	return steps, nil
}

func (c *Compiler) finishWorkQueuePreparedAdapterSteps(data *WorkflowData, name string, index int, steps []string) []string {
	var redaction strings.Builder
	c.generateTrackedSecretRedactionStep(&redaction, strings.Join(steps, ""), data)
	steps = append(steps, redaction.String())
	steps = append(steps,
		"      - name: Upload isolated Claim adapter preparation\n",
		"        id: claim_adapter_artifact\n",
		"        if: always() && steps.claim_adapter_context.outputs.active == 'true' && steps.redact_secrets.outcome == 'success'\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: work-queue-claim-adapter-%s-%d\n", name, index),
		fmt.Sprintf("          path: %s/claims/\n", constants.TmpGhAwDir),
		"          if-no-files-found: error\n",
	)
	return steps
}

func shallowCopyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	maps.Copy(result, value)
	return result
}

func addWorkQueuePreparedAdapterNeeds(job *Job, names []string) {
	if len(names) == 0 {
		return
	}
	job.Needs = slices.Concat(job.Needs, names)
	condition := strings.TrimSpace(job.If)
	condition = strings.TrimPrefix(strings.TrimSuffix(condition, "}}"), "${{")
	job.If = "always() && (" + strings.TrimSpace(condition) + ")"
}
