package workflow

import (
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// Dry-run execution keeps its own credit limits, but conclusion still records
// metadata from the admission step, which remains in the dry-run graph.
func admissionBudgetOutputsEnabled(data *WorkflowData) bool {
	return data != nil && data.AdmissionEnabled && !data.DryRun
}

func (c *Compiler) isAdmissionEnabled(frontmatter map[string]any) bool {
	repoConfig, err := c.loadRepoConfig()
	if err == nil && repoConfig != nil && repoConfig.Admission != nil {
		return *repoConfig.Admission
	}
	if on, ok := frontmatter["on"].(map[string]any); ok {
		if enabled, ok := on["admission"].(bool); ok {
			return enabled
		}
	}
	return true
}

func (c *Compiler) buildConclusionAdmissionStep(data *WorkflowData) []string {
	return []string{
		"      - name: Report admission conclusion\n",
		"        if: always() && vars.GH_AW_DEFAULT_ADMISSION_URL != ''\n",
		"        continue-on-error: true\n",
		"        timeout-minutes: 2\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionURL, compilerenv.DefaultAdmissionURL),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionAudience, compilerenv.DefaultAdmissionAudience),
		fmt.Sprintf("          %s: ${{ vars.%s }}\n", compilerenv.DefaultAdmissionOnOutage, compilerenv.DefaultAdmissionOnOutage),
		fmt.Sprintf("          GH_AW_ADMISSION_WORKFLOW_HASH: %q\n", data.FrontmatterHash+":"+data.BodyHash),
		"          GH_AW_INFO_WORKFLOW_ID: ${{ job.workflow_ref }}\n",
		"          GITHUB_ACTOR: ${{ github.actor }}\n",
		"          GITHUB_TRIGGERING_ACTOR: ${{ github.triggering_actor }}\n",
		"          GH_AW_USAGE_ARTIFACT_ID: ${{ steps.upload-usage-artifact.outputs.artifact-id }}\n",
		"          GH_AW_USAGE_ARTIFACT_RETRY_ID: ${{ steps.upload-usage-artifact-retry.outputs.artifact-id }}\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'setup_globals.cjs'));\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            await require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'conclusion_admission.cjs')).main(core);\n",
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

func (c *Compiler) addActivationAdmissionStep(ctx *activationJobBuildContext) error {
	if !ctx.data.AdmissionEnabled {
		return nil
	}
	data := ctx.data
	if err := validateAdmissionWorkflowHashes(data); err != nil {
		return err
	}
	for _, name := range []string{"result", "max-ai-credits", "max-ai-credits-threat-detection", "max-ai-credits-evals"} {
		ctx.outputs["admission-"+name] = "${{ steps.admission.outputs." + name + " }}"
	}
	ctx.steps = append(ctx.steps,
		"      - name: Check workflow admission\n",
		"        id: admission\n",
		fmt.Sprintf("        if: vars.%s != ''\n", compilerenv.DefaultAdmissionURL),
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
	return nil
}
