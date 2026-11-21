package workflow

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

const brokerReportSystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func usesExperimentsBroker(data *WorkflowData) bool {
	return data != nil && len(data.Experiments) > 0 && data.ExperimentsStorage == ExperimentsStorageBroker && !data.DryRun
}

func experimentBrokerEnv(data *WorkflowData, names []string, invocationID string) []string {
	spec := buildExperimentSpecJSON(data.Experiments, data.ExperimentConfigs, names)
	if invocationID == "" {
		invocationID = "default"
	}
	return []string{
		"        env:\n",
		fmt.Sprintf("          GH_AW_EXPERIMENT_SPEC: '%s'\n", strings.ReplaceAll(spec, "'", "''")),
		fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID),
		"          GH_AW_WORKFLOW_REF: ${{ job.workflow_ref }}\n",
		fmt.Sprintf("          GH_AW_EXPERIMENT_INVOCATION_ID: %s\n", invocationID),
		fmt.Sprintf("          GH_AW_EXPERIMENT_STATE_DIR: %s\n", experimentsCacheDir),
		"          GH_AW_EXPERIMENTS_BROKER_URL: ${{ secrets.GH_AW_EXPERIMENTS_BROKER_URL }}\n",
	}
}

func (c *Compiler) generateBrokerExperimentSteps(data *WorkflowData, names []string) []string {
	steps := []string{
		initializeBrokerExperimentStateStep(),
		"      - name: Pick experiment variants\n",
		"        id: pick-experiment\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
	}
	steps = append(steps, experimentBrokerEnv(data, names, artifactPrefixExprForActivationJob(data))...)
	steps = append(steps,
		"        with:\n",
		"          script: |\n",
		"            const { assign } = require('"+SetupActionDestination+"/experiments_broker.cjs');\n",
		"            await assign({core});\n",
	)
	return append(steps, c.generateExperimentArtifactUploadStep(data, SanitizeWorkflowIDForCacheKey(data.WorkflowID))...)
}

func initializeBrokerExperimentStateStep() string {
	return "      - name: Initialize ephemeral experiment files\n" +
		"        run: |\n" +
		"          mkdir -p " + experimentsCacheDir + "\n" +
		"          rm -f " + experimentsCacheDir + "/assignments.json " + experimentsCacheDir + "/state.json " + experimentStateFile + "\n"
}

// Reporting runs after usage collection, including when the agent fails, but never
// grants the agent or custom grader code access to the broker's OIDC credentials.
func (c *Compiler) addReportExperimentsJob(data *WorkflowData) error {
	if !usesExperimentsBroker(data) {
		return nil
	}
	if _, exists := c.jobManager.GetJob(string(constants.ConclusionJobName)); !exists {
		return errors.New("experiments.storage: broker requires a conclusion job publishing usage artifacts")
	}
	if _, exists := c.jobManager.GetJob(string(constants.AgentJobName)); !exists {
		return errors.New("experiments.storage: broker requires an agent job whose result can be reported; status-only workflows are not supported")
	}
	env, err := brokerReportJobEnv(data)
	if err != nil {
		return err
	}
	needs := []string{string(constants.ActivationJobName), string(constants.AgentJobName), string(constants.ConclusionJobName)}
	if _, exists := c.jobManager.GetJob(string(constants.GradersJobName)); exists {
		needs = append(needs, string(constants.GradersJobName))
	}
	perms := NewPermissionsContentsRead()
	perms.Set(PermissionIdToken, PermissionWrite)
	job := &Job{
		Name:           string(constants.ReportExperimentsJobName),
		RunsOn:         "runs-on: ubuntu-latest",
		Needs:          needs,
		If:             "always() && !cancelled() && needs.activation.outputs.experiments != '' && needs.activation.outputs.experiments != '{}'",
		Permissions:    perms.RenderToYAML(),
		Env:            env,
		TimeoutMinutes: 5,
		Steps:          c.buildReportExperimentsSteps(data),
	}
	return c.jobManager.AddJob(job)
}

func (c *Compiler) buildReportExperimentsSteps(data *WorkflowData) []string {
	steps := c.generateCheckoutActionsFolder(data)
	setupRef := c.resolveActionReference("./actions/setup", data)
	// Keep authored authentication, setup steps, and OTLP configuration out of this job.
	steps = append(steps, c.generateSetupStepForJob(string(constants.ReportExperimentsJobName), &WorkflowData{Name: data.Name}, setupRef, SetupActionDestination, false, "", "", "")...)
	steps = append(steps, initializeBrokerExperimentStateStep(),
		"      - name: Initialize experiment usage files\n",
		"        run: rm -f /tmp/gh-aw/usage/activity/summary.json /tmp/gh-aw/usage/aw_info.json\n",
	)
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:    experimentArtifactDownloadName(data),
		DownloadPath:    experimentsCacheDir,
		StepName:        "Download experiment assignments",
		ContinueOnError: new(false),
	}, c.getActionPin)...)
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:    artifactPrefixExprForDownstreamJob(data) + constants.UsageArtifactName.String(),
		DownloadPath:    path.Join(constants.TmpGhAwDir, constants.UsageArtifactName.String()),
		StepName:        "Download experiment usage results",
		ContinueOnError: new(false),
	}, c.getActionPin)...)
	return append(steps, c.buildReportExperimentsScriptStep(data)...)
}

func (c *Compiler) buildReportExperimentsScriptStep(data *WorkflowData) []string {
	steps := []string{
		"      - name: Report experiment results\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	}
	steps = append(steps, experimentBrokerEnv(data, sortedExperimentNames(data.Experiments), artifactPrefixExprForDownstreamJob(data))...)
	return append(steps,
		"          GH_AW_EXPERIMENT_RESULTS_FILE: /tmp/gh-aw/usage/activity/summary.json\n",
		"          GH_AW_EXPERIMENT_METADATA_FILE: /tmp/gh-aw/usage/aw_info.json\n",
		"          GH_AW_EXPERIMENT_AGENT_RESULT: ${{ needs.agent.result }}\n",
		"        with:\n",
		"          script: |\n",
		"            const { report } = require('"+SetupActionDestination+"/experiments_broker.cjs');\n",
		"            await report({core});\n",
	)
}

// Job-level overrides take effect before any action or interpreter starts. A
// minimal setup config alone cannot isolate BASH_ENV/NODE_OPTIONS inherited from
// authored or imported workflow-level env.
func brokerReportJobEnv(data *WorkflowData) (map[string]string, error) {
	var workflow struct {
		Env map[string]any `yaml:"env"`
	}
	if err := yaml.Unmarshal([]byte(data.Env), &workflow); err != nil {
		return nil, fmt.Errorf("cannot isolate experiment reporting environment: %w", err)
	}
	if len(workflow.Env) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(workflow.Env))
	for name := range workflow.Env {
		env[name] = `""`
	}
	for _, name := range []string{"HOME", "TMPDIR", "TMP", "TEMP"} {
		if _, exists := env[name]; exists {
			env[name] = "${{ runner.temp }}"
		}
	}
	if _, exists := env["PATH"]; exists {
		env["PATH"] = brokerReportSystemPath
	}
	return env, nil
}
