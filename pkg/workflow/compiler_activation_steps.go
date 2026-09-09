package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// compiler_activation_steps contains activation job step generation helpers.

func (c *Compiler) addActivationReactionStep(ctx *activationJobBuildContext) {
	if !ctx.hasReaction {
		return
	}
	reactionCondition := BuildReactionConditionForTargets(
		ctx.reactionIssues,
		ctx.reactionPullRequests,
		ctx.reactionDiscussions,
		ctx.data.CommandCentralized,
	)
	ctx.steps = append(ctx.steps, fmt.Sprintf("      - name: Add %s reaction for immediate feedback\n", ctx.data.AIReaction))
	ctx.steps = append(ctx.steps, "        id: react\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        if: %s\n", RenderCondition(reactionCondition)))
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        env:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_REACTION: %q\n", ctx.data.AIReaction))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          github-token: %s\n", c.resolveActivationToken(ctx.data)))
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("add_reaction.cjs"))
}

func (c *Compiler) addActivationSecretValidationStep(ctx *activationJobBuildContext) {
	secretValidationStep := ctx.engine.GetSecretValidationStep(ctx.data)
	if len(secretValidationStep) == 0 {
		compilerActivationJobLog.Printf("Skipped validate-secret step (engine does not require secret validation)")
		return
	}
	for _, line := range secretValidationStep {
		ctx.steps = append(ctx.steps, line+"\n")
	}
	ctx.outputs["secret_verification_result"] = "${{ steps.validate-secret.outputs.verification_result }}"
	compilerActivationJobLog.Printf("Added validate-secret step to activation job")
}

func (c *Compiler) addActivationDockerSbxSecretsCheckStep(ctx *activationJobBuildContext) {
	if !isDockerSbxRuntime(ctx.data) {
		return
	}
	for _, line := range generateDockerSbxActivationSecretsCheckStep() {
		ctx.steps = append(ctx.steps, line+"\n")
	}
	ctx.outputs["docker_sbx_secrets_result"] = "${{ steps.docker-sbx-secrets.outputs.verification_result }}"
	compilerActivationJobLog.Printf("Added docker-sbx Docker Hub secrets check to activation job")
}

// addActivationOAuthTokenCheckStep adds a step to the activation job that checks
// COPILOT_GITHUB_TOKEN, GH_AW_GITHUB_TOKEN, and GH_AW_GITHUB_MCP_SERVER_TOKEN are not
// OAuth tokens. OAuth tokens (gho_...) are not suitable for automation as they are
// typically over-provisioned.
func (c *Compiler) addActivationOAuthTokenCheckStep(ctx *activationJobBuildContext) {
	var envLines []string

	// Skip COPILOT_GITHUB_TOKEN when permissions.copilot-requests is write: in that
	// mode the Copilot engine authenticates via ${{ github.token }} (org-billed,
	// PAT-free) and does not reference secrets.COPILOT_GITHUB_TOKEN anywhere else in
	// the compiled workflow, so checking it here would needlessly re-introduce the
	// secret reference into the lock file and its gh-aw-manifest.
	if !hasCopilotRequestsWritePermission(ctx.data) {
		// Resolve COPILOT_GITHUB_TOKEN expression, respecting engine.env overrides.
		copilotTokenExpr := fmt.Sprintf("${{ secrets.%s }}", constants.CopilotGitHubToken)
		if overrides := getEngineEnvOverrides(ctx.data); overrides != nil {
			if override, ok := overrides[constants.CopilotGitHubToken]; ok {
				copilotTokenExpr = override
			}
		}
		envLines = appendEnvVarLine(envLines, constants.CopilotGitHubToken, copilotTokenExpr)
	}
	envLines = appendEnvVarLine(envLines, constants.EnvVarGitHubToken, fmt.Sprintf("${{ secrets.%s }}", constants.EnvVarGitHubToken))
	envLines = appendEnvVarLine(envLines, constants.EnvVarGitHubMCPServerToken, fmt.Sprintf("${{ secrets.%s }}", constants.EnvVarGitHubMCPServerToken))

	if len(envLines) == 0 {
		compilerActivationJobLog.Print("Skipped OAuth token check step (no secrets to check)")
		return
	}

	compilerActivationJobLog.Print("Adding OAuth token check step to activation job")

	ctx.steps = append(ctx.steps, "      - name: Check for OAuth tokens\n")
	ctx.steps = append(ctx.steps, "        id: check-oauth-tokens\n")
	ctx.steps = append(ctx.steps, "        run: bash \"${RUNNER_TEMP}/gh-aw/actions/check_oauth_tokens.sh\"\n")
	ctx.steps = append(ctx.steps, "        env:\n")
	for _, envLine := range envLines {
		ctx.steps = append(ctx.steps, envLine+"\n")
	}
}

func (c *Compiler) addActivationCrossRepoGuidanceStep(ctx *activationJobBuildContext) {
	if !hasWorkflowCallTrigger(ctx.data.On) || ctx.data.InlinedImports {
		return
	}
	compilerActivationJobLog.Print("Adding cross-repo setup guidance step for workflow_call trigger")
	ctx.steps = append(ctx.steps, "      - name: Print cross-repo setup guidance\n")
	ctx.steps = append(ctx.steps, "        if: failure() && steps.resolve-host-repo.outputs.target_repo != github.repository\n")
	ctx.steps = append(ctx.steps, "        run: |\n")
	ctx.steps = append(ctx.steps, "          echo \"::error::COPILOT_GITHUB_TOKEN must be configured in the CALLER repository's secrets.\"\n")
	ctx.steps = append(ctx.steps, "          echo \"::error::For cross-repo workflow_call, secrets must be set in the repository that triggers the workflow.\"\n")
	ctx.steps = append(ctx.steps, "          echo \"::error::See: https://github.github.com/gh-aw/patterns/central-repo-ops/#cross-repo-setup\"\n")
}

func buildRuntimeFeaturesSummaryStep() []string {
	return []string{
		"      - name: Log runtime features\n",
		"        if: ${{ contains(toJSON(vars), '\"GH_AW_RUNTIME_FEATURES\":') }}\n",
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/log_runtime_features_summary.sh\"\n",
	}
}

func buildPolicyStrictEnforcementStep() []string {
	return []string{
		"      - name: Enforce strict mode policy\n",
		fmt.Sprintf("        if: ${{ vars.%s == 'true' }}\n", compilerenv.PolicyStrict),
		"        run: |\n",
		fmt.Sprintf("          echo \"::error::%s=true but this workflow was not compiled in strict mode. Recompile with --strict or strict: true.\"\n", compilerenv.PolicyStrict),
		"          exit 1\n",
	}
}

// addActivationRepositoryAndOutputSteps appends checkout, validation, sanitization, comment, and lock steps.
func (c *Compiler) addActivationRepositoryAndOutputSteps(ctx *activationJobBuildContext) error {
	data := ctx.data
	compilerActivationJobLog.Printf("Adding activation repository/output steps: stale_check_disabled=%t, needs_text_output=%t, lock_for_agent=%t",
		data.StaleCheckDisabled, data.NeedsTextOutput, data.LockForAgent)
	c.addActivationCheckoutAndBaseRestoreStep(ctx)
	c.addActivationSteeringIssueStep(ctx)
	c.addActivationLockFileStep(ctx)
	c.addActivationVersionCheckStep(ctx)
	if err := c.addActivationTextOutputStep(ctx); err != nil {
		return err
	}
	if err := c.addActivationStatusCommentStep(ctx); err != nil {
		return err
	}
	c.addActivationIssueLockStep(ctx)
	ensureActivationCommentOutputs(ctx)
	return nil
}

func (c *Compiler) addActivationCheckoutAndBaseRestoreStep(ctx *activationJobBuildContext) {
	data := ctx.data
	checkoutSteps := c.generateCheckoutGitHubFolderForActivation(data)
	ctx.steps = append(ctx.steps, checkoutSteps...)
	if len(checkoutSteps) > 0 {
		compilerActivationJobLog.Print("Adding step to save agent config folders for base branch restoration")
		folders, files := resolveAgentManifestPaths(c.engineRegistry, data)
		ctx.steps = append(ctx.steps, generateSaveBaseGitHubFoldersStep(
			folders,
			files,
		)...)
	}
}

func (c *Compiler) addActivationLockFileStep(ctx *activationJobBuildContext) {
	if ctx.data.StaleCheckDisabled {
		return
	}
	ctx.steps = append(ctx.steps, "      - name: Check workflow lock file\n")
	ctx.steps = append(ctx.steps, "        id: check-lock-file\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        env:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_WORKFLOW_FILE: \"%s\"\n", ctx.lockFilename))
	ctx.steps = append(ctx.steps, "          GH_AW_CONTEXT_WORKFLOW_REF: \"${{ github.workflow_ref }}\"\n")
	if ctx.data.StaleCheckFull {
		ctx.steps = append(ctx.steps, "          GH_AW_STALE_CHECK_FULL: \"true\"\n")
	}
	ctx.steps = append(ctx.steps, "        with:\n")
	hashToken := c.resolveActivationToken(ctx.data)
	if hashToken != "${{ secrets.GITHUB_TOKEN }}" {
		ctx.steps = append(ctx.steps, fmt.Sprintf("          github-token: %s\n", hashToken))
	}
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("check_workflow_timestamp_api.cjs"))
}

func (c *Compiler) addActivationVersionCheckStep(ctx *activationJobBuildContext) {
	if ctx.data.UpdateCheckDisabled || !IsReleasedVersion(c.version) {
		return
	}
	ctx.steps = append(ctx.steps, "      - name: Check compile-agentic version\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        env:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_COMPILED_VERSION: \"%s\"\n", c.version))
	ctx.steps = append(ctx.steps, buildTemplatableBoolEnvVar("GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE", activationBlockedVersionReportAsIssueValue(ctx))...)
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", ctx.data.Name))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("check_version_updates.cjs"))
}

// frontmatterSkillStepName builds a human-readable step name for installing a
// frontmatter skill, including the skill identifier (with any trailing
// "@<sha>" pin stripped) so a reader doesn't have to cross-reference the
// workflow's skills: frontmatter list to map index -> skill. Falls back to the
// numbered form when the skill identifier is empty.
func frontmatterSkillStepName(skill string, stepNumber int) string {
	identifier := strings.TrimSpace(skill)
	if identifier == "" {
		return fmt.Sprintf("Install frontmatter skill %d", stepNumber)
	}
	if at := strings.LastIndex(identifier, "@"); at > 0 {
		identifier = identifier[:at]
	}
	return fmt.Sprintf("%q", "Install frontmatter skill: "+identifier)
}

func (c *Compiler) addActivationSkillInstallSteps(ctx *activationJobBuildContext) {
	skillRefs := activationSkillReferences(ctx.data)
	if len(skillRefs) == 0 {
		return
	}

	engineID := resolveActivationEngineID(ctx.data)
	skillDir := path.Join(engineConfigBaseDirForRegistry(c.engineRegistry, engineID), "skills")
	skillInstallAgentName := ""
	if engine, err := c.engineRegistry.GetEngine(strings.ToLower(engineID)); err == nil {
		skillInstallAgentName = engine.GetGHSkillAgentName()
	}

	c.addActivationSkillUpgradeStep(ctx)

	for i, skillRef := range skillRefs {
		c.addActivationSingleSkillInstallStep(ctx, skillRef, i+1, engineID, skillInstallAgentName, skillDir)
	}

	c.addActivationSkillFailureCollectionStep(ctx)
}

func activationSkillReferences(data *WorkflowData) []SkillReference {
	skillRefs := append([]SkillReference(nil), data.SkillReferences...)
	if len(skillRefs) > 0 || len(data.Skills) == 0 {
		return skillRefs
	}
	skillRefs = make([]SkillReference, 0, len(data.Skills))
	for _, skill := range data.Skills {
		if strings.TrimSpace(skill) == "" {
			continue
		}
		skillRefs = append(skillRefs, SkillReference{Skill: skill})
	}
	return skillRefs
}

func (c *Compiler) addActivationSkillUpgradeStep(ctx *activationJobBuildContext) {
	ctx.steps = append(ctx.steps, "      - name: Upgrade gh CLI for frontmatter skills\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/ensure_gh_cli_min_version.sh\" \"%s\"\n", constants.GhSkillsMinVersion))
}

func (c *Compiler) addActivationSingleSkillInstallStep(ctx *activationJobBuildContext, skillRef SkillReference, stepNumber int, engineID, skillInstallAgentName, skillDir string) {
	tokenExpr := c.resolveFrontmatterSkillToken(ctx, skillRef, stepNumber)
	ctx.steps = append(ctx.steps, fmt.Sprintf("      - name: %s\n", frontmatterSkillStepName(skillRef.Skill, stepNumber)))
	ctx.steps = append(ctx.steps, "        env:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_TOKEN: %s\n", tokenExpr))
	ctx.steps = append(ctx.steps, formatYAMLEnv("          ", "GH_AW_INFO_ENGINE_ID", engineID))
	ctx.steps = append(ctx.steps, formatYAMLEnv("          ", "GH_AW_GH_SKILL_AGENT_NAME", skillInstallAgentName))
	ctx.steps = append(ctx.steps, formatYAMLEnv("          ", "GH_AW_SKILL_DIR", skillDir))
	ctx.steps = append(ctx.steps, formatYAMLEnv("          ", "GH_AW_FRONTMATTER_SKILLS", skillRef.Skill))
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("install_frontmatter_skills.cjs"))
}

func (c *Compiler) resolveFrontmatterSkillToken(ctx *activationJobBuildContext, skillRef SkillReference, stepNumber int) string {
	tokenExpr := c.resolveActivationToken(ctx.data)
	if skillRef.GitHubToken != "" {
		tokenExpr = skillRef.GitHubToken
	}
	if skillRef.GitHubApp == nil {
		return tokenExpr
	}
	stepID := fmt.Sprintf("frontmatter-skill-app-token-%d", stepNumber)
	ctx.steps = append(ctx.steps, c.buildGitHubAppTokenMintStepWithMeta(
		skillRef.GitHubApp,
		nil,
		"",
		"",
		fmt.Sprintf("Generate GitHub App token for frontmatter skill %d", stepNumber),
		stepID,
	)...)
	stepTokenExpr := fmt.Sprintf("${{ steps.%s.outputs.token }}", stepID)
	if skillRef.GitHubApp.shouldIgnoreMissingKey() {
		return combineTokenExpressions(stepTokenExpr, c.resolveActivationToken(ctx.data))
	}
	return stepTokenExpr
}

func (c *Compiler) addActivationSkillFailureCollectionStep(ctx *activationJobBuildContext) {
	// Collect skill install failures written by each install step into a shared file.
	// Runs with if: always() so failures are captured even if a prior step was unexpectedly hard-failed.
	ctx.steps = append(ctx.steps, "      - name: Collect skill install failures\n")
	ctx.steps = append(ctx.steps, "        id: collect-skill-install-failures\n")
	ctx.steps = append(ctx.steps, "        if: always()\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("collect_skill_install_failures.cjs"))

	ctx.outputs["skill_install_failure_count"] = "${{ steps.collect-skill-install-failures.outputs.failure_count || '0' }}"
	ctx.outputs["skill_install_errors"] = "${{ steps.collect-skill-install-failures.outputs.errors || '' }}"
}

func (c *Compiler) addActivationTextOutputStep(ctx *activationJobBuildContext) error {
	if !ctx.data.NeedsTextOutput {
		return nil
	}
	ctx.steps = append(ctx.steps, "      - name: Compute current body text\n")
	ctx.steps = append(ctx.steps, "        id: sanitized\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	domainsStr, err := c.computeActivationSanitizationDomains(ctx.data)
	if err != nil {
		return err
	}
	envLines := buildActivationTextOutputEnvLines(ctx.data, domainsStr)
	if len(envLines) > 0 {
		ctx.steps = append(ctx.steps, "        env:\n")
		ctx.steps = append(ctx.steps, envLines...)
	}
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("compute_text.cjs"))
	ctx.outputs["text"] = "${{ steps.sanitized.outputs.text }}"
	ctx.outputs["title"] = "${{ steps.sanitized.outputs.title }}"
	ctx.outputs["body"] = "${{ steps.sanitized.outputs.body }}"
	return nil
}

func (c *Compiler) computeActivationSanitizationDomains(data *WorkflowData) (string, error) {
	if data.SafeOutputs != nil && len(data.SafeOutputs.AllowedDomains) > 0 {
		return c.computeExpandedAllowedDomainsForSanitization(data)
	}
	return c.computeAllowedDomainsForSanitization(data)
}

func buildActivationTextOutputEnvLines(data *WorkflowData, domainsStr string) []string {
	var envLines []string
	if len(data.Bots) > 0 {
		envLines = append(envLines, formatYAMLEnv("          ", "GH_AW_ALLOWED_BOTS", strings.Join(data.Bots, ",")))
	}
	if domainsStr != "" {
		envLines = append(envLines, formatYAMLEnv("          ", "GH_AW_ALLOWED_DOMAINS", domainsStr))
	}
	return envLines
}

func (c *Compiler) addActivationStatusCommentStep(ctx *activationJobBuildContext) error {
	if ctx.data.StatusComment == nil || !*ctx.data.StatusComment {
		return nil
	}
	statusCommentCondition := BuildStatusCommentCondition(
		ctx.statusCommentIssues,
		ctx.statusCommentPRs,
		ctx.statusCommentDiscussions,
		ctx.data.CommandCentralized,
	)
	ctx.steps = append(ctx.steps, "      - name: Add comment with workflow run link\n")
	ctx.steps = append(ctx.steps, "        id: add-comment\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        if: %s\n", RenderCondition(statusCommentCondition)))
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        env:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", ctx.data.Name))
	if ctx.data.FrontmatterEmoji != "" {
		ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_WORKFLOW_EMOJI: %q\n", ctx.data.FrontmatterEmoji))
	}
	if ctx.data.TrackerID != "" {
		ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_TRACKER_ID: %q\n", ctx.data.TrackerID))
	}
	if ctx.data.LockForAgent {
		ctx.steps = append(ctx.steps, "          GH_AW_LOCK_FOR_AGENT: \"true\"\n")
	}
	addActivationSafeOutputMessagesEnv(ctx)
	ctx.steps = append(ctx.steps, "        with:\n")
	commentToken := c.resolveActivationToken(ctx.data)
	if commentToken != "${{ secrets.GITHUB_TOKEN }}" {
		ctx.steps = append(ctx.steps, fmt.Sprintf("          github-token: %s\n", commentToken))
	}
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("add_workflow_run_comment.cjs"))
	ctx.outputs["comment_id"] = "${{ steps.add-comment.outputs.comment-id }}"
	ctx.outputs["comment_url"] = "${{ steps.add-comment.outputs.comment-url }}"
	ctx.outputs["comment_repo"] = "${{ steps.add-comment.outputs.comment-repo }}"
	return nil
}

func addActivationSafeOutputMessagesEnv(ctx *activationJobBuildContext) {
	if ctx.data.SafeOutputs == nil || ctx.data.SafeOutputs.Messages == nil {
		return
	}
	// serializeMessagesConfig uses json.Marshal on a struct containing only strings and bools,
	// so it cannot fail in practice; the error is intentionally ignored here.
	messagesJSON, _ := serializeMessagesConfig(ctx.data.SafeOutputs.Messages)
	if messagesJSON != "" {
		ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
	}
}

func (c *Compiler) addActivationIssueLockStep(ctx *activationJobBuildContext) {
	if !ctx.data.LockForAgent {
		return
	}
	lockCondition := BuildOr(
		BuildEventTypeEquals("issues"),
		BuildEventTypeEquals("issue_comment"),
	)
	ctx.steps = append(ctx.steps, "      - name: Lock issue for agentic workflow\n")
	ctx.steps = append(ctx.steps, "        id: lock-issue\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        if: %s\n", RenderCondition(lockCondition)))
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, "          script: |\n")
	ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("lock-issue.cjs"))
	ctx.outputs["issue_locked"] = "${{ steps.lock-issue.outputs.locked }}"
	if ctx.data.AIReaction != "" && ctx.data.AIReaction != "none" {
		compilerActivationJobLog.Print("Adding lock notification to reaction message")
	}
}

func ensureActivationCommentOutputs(ctx *activationJobBuildContext) {
	if _, exists := ctx.outputs["comment_id"]; !exists {
		ctx.outputs["comment_id"] = `""`
	}
	if _, exists := ctx.outputs["comment_repo"]; !exists {
		ctx.outputs["comment_repo"] = `""`
	}
}
