package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

const admissionJobResultCondition = "(needs.admission.result == 'success' || needs.admission.result == 'skipped')"

// Dry-run execution keeps its own credit limits, but conclusion still records
// metadata from the admission job, which remains in the dry-run graph.
func admissionBudgetOutputsEnabled(data *WorkflowData) bool {
	return data != nil && data.AdmissionEnabled && !data.DryRun
}

func (c *Compiler) isAdmissionEnabled(frontmatter map[string]any) bool {
	if on, ok := frontmatter["on"].(map[string]any); ok {
		if enabled, ok := on["admission"].(bool); ok && enabled {
			return true
		}
	}
	repoConfig, err := c.loadRepoConfig()
	return err == nil && repoConfig != nil && repoConfig.Admission
}

func (c *Compiler) buildConclusionAdmissionStep(data *WorkflowData) []string {
	return []string{
		"      - name: Report admission conclusion\n",
		"        if: always() && vars.GH_AW_DEFAULT_ADMISSION_URL != ''\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionURL, compilerenv.DefaultAdmissionURL),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionAudience, compilerenv.DefaultAdmissionAudience),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionOnOutage, compilerenv.DefaultAdmissionOnOutage),
		fmt.Sprintf("          GH_AW_ADMISSION_WORKFLOW_HASH: %q\n", data.FrontmatterHash+":"+data.BodyHash),
		"          GH_AW_INFO_WORKFLOW_ID: ${{ job.workflow_ref }}\n",
		"          GITHUB_ACTOR: ${{ github.actor }}\n",
		"          GITHUB_TRIGGERING_ACTOR: ${{ github.triggering_actor }}\n",
		"          GH_AW_ADMISSION_COMPACT_SESSION: ${{ needs.conclusion.outputs.admission_compact_session }}\n",
		"          GH_AW_USAGE_ARTIFACT_ID: ${{ needs.conclusion.outputs.usage_artifact_id }}\n",
		"          GH_AW_USAGE_ARTIFACT_RETRY_ID: ${{ needs.conclusion.outputs.usage_artifact_retry_id }}\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'setup_globals.cjs'));\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            await require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'conclusion_admission.cjs')).main(core);\n",
	}
}

func (c *Compiler) buildConclusionAdmissionMetadataStep(data *WorkflowData) []string {
	return []string{
		"      - name: Prepare admission conclusion metadata\n",
		"        id: admission-conclusion-metadata\n",
		"        if: always() && vars.GH_AW_DEFAULT_ADMISSION_URL != ''\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        with:\n",
		"          script: |\n",
		"            const path = require('node:path');\n",
		"            const { readCompactUnifiedSession } = require(path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'compact_unified_session.cjs'));\n",
		"            const compact = readCompactUnifiedSession('/tmp/gh-aw/usage/aw_session.jsonl');\n",
		"            core.setOutput('compact_session', JSON.stringify({ version: compact.version, session: compact.session, included_events: compact.includedEvents, omitted_events: compact.omittedEvents, oversized_lines: compact.oversizedLines, scan_truncated: compact.scanTruncated, truncated: compact.truncated }));\n",
	}
}

func validateAdmissionWorkflowHashes(data *WorkflowData) error {
	if data == nil || !isSHA256Hex(data.FrontmatterHash) || !isSHA256Hex(data.BodyHash) {
		return errors.New("admission requires 64-character lowercase SHA-256 workflow hashes")
	}
	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (c *Compiler) buildConclusionAdmissionJob(data *WorkflowData) (*Job, error) {
	if err := validateAdmissionWorkflowHashes(data); err != nil {
		return nil, err
	}
	setupRef := c.resolveActionReference("./actions/setup", data)
	if setupRef == "" {
		return nil, errors.New("admission conclusion setup action reference could not be resolved")
	}
	steps := c.generateCheckoutActionsFolder(data)
	steps = append(steps, c.generateSetupStepForJob(
		string(constants.ConclusionAdmissionJobName),
		&WorkflowData{Name: data.Name},
		setupRef,
		SetupActionDestination,
		false,
		"",
		"",
		"",
	)...)
	steps = append(steps, c.buildConclusionAdmissionStep(data)...)
	return &Job{
		Name:   string(constants.ConclusionAdmissionJobName),
		If:     "always() && needs.conclusion.result != 'skipped' && vars.GH_AW_DEFAULT_ADMISSION_URL != ''",
		RunsOn: c.formatFrameworkJobRunsOn(data),
		Permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
			PermissionContents: PermissionRead,
			PermissionIdToken:  PermissionWrite,
		}).RenderToYAML(),
		TimeoutMinutes: 5,
		Needs:          []string{string(constants.ConclusionJobName)},
		Steps:          steps,
	}, nil
}

func addActivationAdmissionOutputs(ctx *activationJobBuildContext) {
	if !ctx.data.AdmissionEnabled {
		return
	}
	for _, name := range []string{"result", "max-ai-credits", "max-ai-credits-threat-detection", "max-ai-credits-evals"} {
		ctx.outputs["admission-"+name] = "${{ needs.admission.outputs." + name + " }}"
	}
}

func (c *Compiler) buildAdmissionJob(data *WorkflowData, activation *Job) (*Job, error) {
	if err := validateAdmissionWorkflowHashes(data); err != nil {
		return nil, err
	}
	setupRef := c.resolveActionReference("./actions/setup", data)
	if setupRef == "" {
		return nil, errors.New("admission setup action reference could not be resolved")
	}
	steps := c.generateCheckoutActionsFolder(data)
	traceID, parentSpanID := buildActivationSetupParentSpans(slices.Contains(activation.Needs, string(constants.PreActivationJobName)))
	// Admission never runs workflow hooks or mints workflow/telemetry App credentials.
	steps = append(steps, c.generateSetupStepForJob("admission", &WorkflowData{Name: data.Name}, setupRef, SetupActionDestination, false, traceID, parentSpanID, "")...)
	steps = append(steps,
		"      - name: Check workflow admission\n",
		"        id: admission\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionURL, compilerenv.DefaultAdmissionURL),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionAudience, compilerenv.DefaultAdmissionAudience),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionOnOutage, compilerenv.DefaultAdmissionOnOutage),
		fmt.Sprintf("          GH_AW_ADMISSION_WORKFLOW_HASH: %q\n", data.FrontmatterHash+":"+data.BodyHash),
		"          GH_AW_INFO_WORKFLOW_ID: ${{ job.workflow_ref }}\n",
		"          GITHUB_ACTOR: ${{ github.actor }}\n",
		"          GITHUB_TRIGGERING_ACTOR: ${{ github.triggering_actor }}\n",
		"        with:\n",
		"          script: |\n",
		"            await require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'activation_admission.cjs')).main(core, github, context, exec, io, getOctokit);\n",
	)
	outputs := map[string]string{}
	for _, name := range []string{"result", "max-ai-credits", "max-ai-credits-threat-detection", "max-ai-credits-evals", "trace-id"} {
		outputs[name] = "${{ steps.admission.outputs." + name + " }}"
	}
	return &Job{
		Name:                       string(constants.AdmissionJobName),
		If:                         c.combineJobIfConditions(activation.If, fmt.Sprintf("vars.%s != ''", compilerenv.DefaultAdmissionURL)),
		HasWorkflowRunSafetyChecks: activation.HasWorkflowRunSafetyChecks,
		RunsOn:                     c.formatFrameworkJobRunsOn(data),
		Permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
			PermissionContents: PermissionRead,
			PermissionIdToken:  PermissionWrite,
		}).RenderToYAML(),
		TimeoutMinutes: 5,
		Needs:          append([]string(nil), activation.Needs...),
		Steps:          steps,
		Outputs:        outputs,
	}, nil
}

func (c *Compiler) connectActivationAdmission(activation *Job) {
	conditions := []string{"(!cancelled())", admissionJobResultCondition}
	// An explicit status function disables GitHub's implicit success gate.
	// Preserve the result requirements of every existing prerequisite.
	if !ifExpressionContainsStatusFunc(stripExpressionWrapper(activation.If)) {
		for _, need := range activation.Needs {
			conditions = append(conditions, fmt.Sprintf("needs.%s.result == 'success'", need))
		}
	}
	activation.If = c.combineJobIfConditions(strings.Join(conditions, " && "), activation.If)
	activation.Needs = append(activation.Needs, string(constants.AdmissionJobName))
}

// GitHub's implicit success check includes skipped ancestors, not just direct needs.
// Preserve each job's direct dependency requirements without inheriting admission's skip.
func (c *Compiler) ensureAdmissionSkipSafeConditions() {
	jobs := c.jobManager.GetAllJobs()
	if _, exists := jobs[string(constants.AdmissionJobName)]; !exists {
		return
	}
	descendants := map[string]struct{}{string(constants.AdmissionJobName): {}}
	for changed := true; changed; {
		changed = false
		for name, job := range jobs {
			if _, exists := descendants[name]; exists {
				continue
			}
			for _, need := range job.Needs {
				if _, exists := descendants[need]; exists {
					descendants[name] = struct{}{}
					changed = true
					break
				}
			}
		}
	}
	for name, job := range jobs {
		bare := stripExpressionWrapper(job.If)
		_, downstream := descendants[name]
		if name == string(constants.AdmissionJobName) || !downstream || bare == "false" || ifExpressionContainsStatusFunc(bare) {
			continue
		}
		conditions := []string{"(!cancelled())"}
		for _, need := range job.Needs {
			guard := fmt.Sprintf("needs.%s.result == 'success'", need)
			if need == string(constants.AdmissionJobName) {
				guard = admissionJobResultCondition
			}
			conditions = append(conditions, guard)
		}
		job.If = c.combineJobIfConditions(strings.Join(conditions, " && "), job.If)
	}
}

func (c *Compiler) buildActivationAndAdmissionJobs(data *WorkflowData, preActivation bool, workflowRunSafety, lockFilename string) error {
	activation, err := c.buildActivationJob(data, preActivation, workflowRunSafety, lockFilename)
	if err != nil {
		return fmt.Errorf("activation job could not be built: %w. Check the workflow triggers and activation-related frontmatter fields", err)
	}
	if data.AdmissionEnabled {
		admission, err := c.buildAdmissionJob(data, activation)
		if err != nil {
			return fmt.Errorf("admission job could not be built: %w", err)
		}
		if err := c.jobManager.AddJob(admission); err != nil {
			return fmt.Errorf("admission job could not be added: %w", err)
		}
		c.connectActivationAdmission(activation)
	}
	if err := c.jobManager.AddJob(activation); err != nil {
		return fmt.Errorf("activation job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}
	return nil
}
