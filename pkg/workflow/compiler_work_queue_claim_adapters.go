package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
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
				fmt.Sprintf("          GH_AW_CLAIM_ADAPTER_INDEX: %q\n", fmt.Sprint(index)),
				"        with:\n",
				"          script: |\n",
				"            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n",
				"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
				"            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_prepare_claim_adapter.cjs');\n",
				"            await main({ core, github, context });\n",
			)
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
			if name == "raw_steps" {
				customSteps = data.SafeOutputs.Steps
			}
			for rawName, config := range data.SafeOutputs.Scripts {
				if stringutil.NormalizeSafeOutputIdentifier(rawName) != name {
					continue
				}
				scriptSteps, err := buildCustomScriptFilesStep(map[string]*SafeScriptConfig{rawName: config})
				if err != nil {
					return nil, err
				}
				steps = append(steps, scriptSteps...)
				customSteps = []any{map[string]any{
					"name": "Prepare Claim script payload without write credentials",
					"uses": c.getActionPin("actions/github-script"),
					"env":  map[string]any{"GH_AW_CLAIM_SCRIPT_FILENAME": safeOutputScriptFilename(name)},
					"with": map[string]any{"script": "const { main } = require(`${process.env.RUNNER_TEMP}/gh-aw/actions/work_queue_prepare_claim_script.cjs`);\nawait main();"},
				}}
			}
			if len(customSteps) == 0 {
				return nil, fmt.Errorf("work-queue: prepared adapter %q has no matching custom job/action/raw-step executable", name)
			}
			for _, value := range customSteps {
				step, ok := value.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("work-queue: prepared adapter %q contains a non-object step", name)
				}
				step = shallowCopyMap(step)
				condition := "steps.claim_adapter_context.outputs.active == 'true'"
				if existing, ok := step["if"].(string); ok && existing != "" {
					condition += " && (" + c.extractExpressionFromIfString(existing) + ")"
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
			job.Steps = steps
			if err := c.jobManager.AddJob(job); err != nil {
				return nil, err
			}
			jobs = append(jobs, jobName)
		}
	}
	return jobs, nil
}

func shallowCopyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, field := range value {
		result[key] = field
	}
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
