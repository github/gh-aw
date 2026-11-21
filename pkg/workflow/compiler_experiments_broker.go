package workflow

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/jsonutil"
	"github.com/goccy/go-yaml"
)

const brokerReportSystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func validateExperimentBrokerURL(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(value, " \t\r\n\\#$") {
		return errors.New("experiments.broker-url must be a literal HTTPS URL without credentials or expressions")
	}
	return nil
}

func mergeImportedExperimentBrokerSettings(frontmatter map[string]any, imported string) (map[string]any, error) {
	settings := map[string]any{}
	storageSeen := false
	for line := range strings.SplitSeq(imported, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var config map[string]any
		if err := jsonutil.UnmarshalString(line, &config); err != nil {
			return nil, fmt.Errorf("invalid imported experiments configuration: %w", err)
		}
		if storage, ok := config["storage"]; ok && !storageSeen {
			storageSeen = true
			if mode, ok := storage.(string); ok && mode == "broker" {
				settings["storage"] = "broker"
			}
		}
		if value, ok := config["broker-url"]; ok {
			if _, seen := settings["broker-url"]; !seen {
				settings["broker-url"] = value
			}
		}
	}
	if len(settings) == 0 {
		return frontmatter, nil
	}
	if local, ok := frontmatter["experiments"].(map[string]any); ok {
		maps.Copy(settings, local)
	}
	result := make(map[string]any, len(frontmatter))
	maps.Copy(result, frontmatter)
	result["experiments"] = settings
	return result, nil
}

func usesExperimentsBroker(data *WorkflowData) bool {
	return data != nil && len(data.Experiments) > 0 && data.ExperimentsStorage == ExperimentsStorageBroker && !data.DryRun
}

func experimentBrokerEnv(data *WorkflowData, names []string, invocationID string) []string {
	spec := buildExperimentSpecJSON(data.Experiments, data.ExperimentConfigs, names)
	if invocationID == "" {
		invocationID = "default"
	}
	brokerURL := "${{ secrets.GH_AW_EXPERIMENTS_BROKER_URL }}"
	if data.ExperimentsBrokerURL != "" {
		brokerURL = "'" + strings.ReplaceAll(data.ExperimentsBrokerURL, "'", "''") + "'"
	}
	return []string{
		"        env:\n",
		fmt.Sprintf("          GH_AW_EXPERIMENT_SPEC: '%s'\n", strings.ReplaceAll(spec, "'", "''")),
		fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID),
		"          GH_AW_WORKFLOW_REF: ${{ job.workflow_ref || github.workflow_ref }}\n",
		fmt.Sprintf("          GH_AW_EXPERIMENT_INVOCATION_ID: %s\n", invocationID),
		fmt.Sprintf("          GH_AW_EXPERIMENT_STATE_DIR: %s\n", experimentsCacheDir),
		fmt.Sprintf("          GH_AW_EXPERIMENT_STATE_FILE: %s\n", experimentStateFile),
		"          GH_AW_BROKER_EXPERIMENTS_URL: " + brokerURL + "\n",
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
		Name:                      string(constants.ReportExperimentsJobName),
		RunsOn:                    c.formatFrameworkJobRunsOn(data),
		Needs:                     needs,
		If:                        "always() && !cancelled() && needs.activation.outputs.experiments != '' && needs.activation.outputs.experiments != '{}'",
		Permissions:               perms.RenderToYAML(),
		Env:                       env,
		TimeoutMinutes:            5,
		Steps:                     c.buildReportExperimentsSteps(data),
		experimentsBrokerReporter: true,
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
		"        run: rm -f /tmp/gh-aw/usage/activity/experiment-results.json /tmp/gh-aw/usage/aw_info.json\n",
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
	steps = append(steps, c.buildReportExperimentsScriptStep(data)...)
	return isolateBrokerReportStepTemp(steps)
}

// Step contexts can use runner.temp; job-level env cannot.
func isolateBrokerReportStepTemp(steps []string) []string {
	lines := strings.SplitAfter(strings.Join(steps, ""), "\n")
	var result []string
	var step []string
	flush := func() {
		if len(step) == 0 {
			return
		}
		hasEnv := slices.Contains(step, "        env:\n")
		for i, line := range step {
			result = append(result, line)
			if i == 0 && !hasEnv {
				result = append(result, "        env:\n")
			} else if line != "        env:\n" {
				continue
			}
			for _, name := range []string{"HOME", "TMPDIR", "TMP", "TEMP"} {
				result = append(result, "          "+name+": ${{ runner.temp }}\n")
			}
		}
		step = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "      - ") {
			flush()
		}
		step = append(step, line)
	}
	flush()
	return result
}

func (c *Compiler) buildReportExperimentsScriptStep(data *WorkflowData) []string {
	steps := []string{
		"      - name: Report experiment results\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	}
	steps = append(steps, experimentBrokerEnv(data, sortedExperimentNames(data.Experiments), artifactPrefixExprForDownstreamJob(data))...)
	return append(steps,
		"          GH_AW_EXPERIMENT_RESULTS_FILE: /tmp/gh-aw/usage/activity/experiment-results.json\n",
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
			env[name] = "/tmp"
		}
	}
	if _, exists := env["PATH"]; exists {
		env["PATH"] = brokerReportSystemPath
	}
	return env, nil
}
