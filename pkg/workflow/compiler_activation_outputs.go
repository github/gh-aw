package workflow

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

// compiler_activation_outputs contains activation outputs, artifact upload wiring, and environment assembly.

// addActivationFeedbackAndValidationSteps appends token minting, reactions, secret validation, and guidance.
func (c *Compiler) addActivationFeedbackAndValidationSteps(ctx *activationJobBuildContext) error {
	data := ctx.data
	compilerActivationJobLog.Printf("Adding activation feedback/validation steps: reaction=%t, status_comment=%t, remove_label=%t, app_token_for_access=%t",
		ctx.hasReaction, ctx.hasStatusComment, ctx.shouldRemoveLabel, ctx.needsAppTokenForAccess)
	c.maybeAddActivationAppTokenMintStep(ctx)
	if hasMaxDailyAICGuardrail(data) {
		ctx.steps = append(ctx.steps, c.buildActivationDailyAICGuardrailStep(data)...)
		ctx.outputs["daily_ai_credits_exceeded"] = "${{ steps.daily-effective-workflow-guardrail.outputs.daily_ai_credits_exceeded == 'true' }}"
		ctx.outputs["daily_ai_credits_guardrail_status"] = "${{ steps.daily-effective-workflow-guardrail.outputs.daily_ai_credits_guardrail_status || '' }}"
		ctx.outputs["daily_ai_credits_total_effective_tokens"] = "${{ steps.daily-effective-workflow-guardrail.outputs.daily_ai_credits_total_effective_tokens || '' }}"
		ctx.outputs["daily_ai_credits_threshold"] = "${{ steps.daily-effective-workflow-guardrail.outputs.daily_ai_credits_threshold || '' }}"
	}
	c.addActivationReactionStep(ctx)
	c.addActivationSecretValidationStep(ctx)
	c.addActivationDockerSbxSecretsCheckStep(ctx)
	c.addActivationOAuthTokenCheckStep(ctx)
	c.addActivationCrossRepoGuidanceStep(ctx)
	return nil
}

// addActivationCommandAndLabelOutputs appends slash-command and label-command output steps.
func (c *Compiler) addActivationCommandAndLabelOutputs(ctx *activationJobBuildContext) error {
	data := ctx.data

	if len(data.Command) > 0 {
		if ctx.preActivationJob {
			ctx.outputs["slash_command"] = fmt.Sprintf("${{ needs.%s.outputs.%s }}", string(constants.PreActivationJobName), constants.MatchedCommandOutput)
		} else {
			ctx.outputs["slash_command"] = fmt.Sprintf("${{ steps.%s.outputs.%s }}", constants.CheckCommandPositionStepID, constants.MatchedCommandOutput)
		}
	}

	if ctx.shouldRemoveLabel {
		compilerActivationJobLog.Print("Adding remove-trigger-label step for label-command workflow")
		ctx.steps = append(ctx.steps, "      - name: Remove trigger label\n")
		ctx.steps = append(ctx.steps, fmt.Sprintf("        id: %s\n", constants.RemoveTriggerLabelStepID))
		ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
		ctx.steps = append(ctx.steps, "        env:\n")
		labelNamesJSON, err := json.Marshal(data.LabelCommand)
		if err != nil {
			return fmt.Errorf("failed to marshal label-command names: %w", err)
		}
		ctx.steps = append(ctx.steps, formatYAMLEnv("          ", "GH_AW_LABEL_NAMES", string(labelNamesJSON)))
		ctx.steps = append(ctx.steps, "        with:\n")
		labelToken := c.resolveActivationToken(data)
		if labelToken != "${{ secrets.GITHUB_TOKEN }}" {
			ctx.steps = append(ctx.steps, fmt.Sprintf("          github-token: %s\n", labelToken))
		}
		ctx.steps = append(ctx.steps, "          script: |\n")
		ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("remove_trigger_label.cjs"))
		ctx.outputs["label_command"] = fmt.Sprintf("${{ steps.%s.outputs.label_name }}", constants.RemoveTriggerLabelStepID)
	} else if ctx.hasLabelCommand {
		compilerActivationJobLog.Print("Adding get-trigger-label step for label-command workflow")
		ctx.steps = append(ctx.steps, "      - name: Get trigger label name\n")
		ctx.steps = append(ctx.steps, fmt.Sprintf("        id: %s\n", constants.GetTriggerLabelStepID))
		ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
		if len(data.Command) > 0 {
			ctx.steps = append(ctx.steps, "        env:\n")
			if ctx.preActivationJob {
				ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_MATCHED_COMMAND: ${{ needs.%s.outputs.%s }}\n", string(constants.PreActivationJobName), constants.MatchedCommandOutput))
			} else {
				ctx.steps = append(ctx.steps, fmt.Sprintf("          GH_AW_MATCHED_COMMAND: ${{ steps.%s.outputs.%s }}\n", constants.CheckCommandPositionStepID, constants.MatchedCommandOutput))
			}
		}
		ctx.steps = append(ctx.steps, "        with:\n")
		ctx.steps = append(ctx.steps, "          script: |\n")
		ctx.steps = append(ctx.steps, generateGitHubScriptWithRequire("get_trigger_label.cjs"))
		ctx.outputs["label_command"] = fmt.Sprintf("${{ steps.%s.outputs.label_name }}", constants.GetTriggerLabelStepID)
		ctx.outputs["command_name"] = fmt.Sprintf("${{ steps.%s.outputs.command_name }}", constants.GetTriggerLabelStepID)
	}

	return nil
}

// configureActivationNeedsAndCondition computes and sets activation dependencies and final job condition.
// This helper mutates the context but only derives values from workflow data and has no error paths.
func (c *Compiler) configureActivationNeedsAndCondition(ctx *activationJobBuildContext) {
	data := ctx.data
	compilerActivationJobLog.Printf("Configuring activation needs and condition: pre_activation=%t, has_if=%t", ctx.preActivationJob, data.If != "")
	customJobsBeforeActivation := c.getCustomJobsDependingOnPreActivation(data.Jobs)
	for _, jobName := range data.OnNeeds {
		if !slices.Contains(customJobsBeforeActivation, jobName) {
			customJobsBeforeActivation = append(customJobsBeforeActivation, jobName)
		}
	}
	promptReferencedJobs := c.getCustomJobsReferencedInPromptWithNoActivationDep(data)
	for _, jobName := range promptReferencedJobs {
		if !slices.Contains(customJobsBeforeActivation, jobName) {
			customJobsBeforeActivation = append(customJobsBeforeActivation, jobName)
			compilerActivationJobLog.Printf("Added '%s' to activation dependencies: referenced in markdown body and has no explicit needs", jobName)
		}
	}

	// Also scan engine.env values for needs.<job>.outputs.* expressions.
	// This ensures custom jobs referenced in engine.env (e.g. to override COPILOT_GITHUB_TOKEN)
	// are added as activation job dependencies so their outputs are available at activation time.
	// Only jobs with NO explicit needs are added here; jobs with explicit needs that depend on
	// pre_activation are already captured by getCustomJobsDependingOnPreActivation above.
	engineEnvJobs := c.getEngineEnvReferencedCustomJobsWithNoExplicitNeeds(data)
	for _, jobName := range engineEnvJobs {
		if !slices.Contains(customJobsBeforeActivation, jobName) {
			customJobsBeforeActivation = append(customJobsBeforeActivation, jobName)
			compilerActivationJobLog.Printf("Added '%s' to activation dependencies: referenced in engine.env", jobName)
		}
	}
	ctx.customJobsBeforeActivation = customJobsBeforeActivation

	if ctx.preActivationJob {
		ctx.activationNeeds = []string{string(constants.PreActivationJobName)}
		ctx.activationNeeds = append(ctx.activationNeeds, customJobsBeforeActivation...)
		activatedExpr := BuildEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.%s", string(constants.PreActivationJobName), constants.ActivatedOutput)),
			BuildStringLiteral("true"),
		)
		if data.If != "" && c.referencesCustomJobOutputs(data.If, data.Jobs) && len(customJobsBeforeActivation) > 0 {
			unwrappedIf := stripExpressionWrapper(data.If)
			ifExpr := &ExpressionNode{Expression: unwrappedIf}
			ctx.activationCondition = RenderCondition(BuildAnd(activatedExpr, ifExpr))
		} else if data.If != "" && !c.referencesCustomJobOutputs(data.If, data.Jobs) {
			unwrappedIf := stripExpressionWrapper(data.If)
			ifExpr := &ExpressionNode{Expression: unwrappedIf}
			ctx.activationCondition = RenderCondition(BuildAnd(activatedExpr, ifExpr))
		} else {
			ctx.activationCondition = RenderCondition(activatedExpr)
		}
	} else {
		ctx.activationNeeds = append(ctx.activationNeeds, customJobsBeforeActivation...)
		if data.If != "" && c.referencesCustomJobOutputs(data.If, data.Jobs) && len(customJobsBeforeActivation) > 0 {
			ctx.activationCondition = data.If
		} else if !c.referencesCustomJobOutputs(data.If, data.Jobs) {
			ctx.activationCondition = data.If
		}
	}

	if ctx.workflowRunRepoSafety != "" {
		ctx.activationCondition = c.combineJobIfConditions(ctx.activationCondition, ctx.workflowRunRepoSafety)
	}
}

// addActivationArtifactUploadStep appends the activation artifact upload step for downstream jobs.
func (c *Compiler) addActivationArtifactUploadStep(ctx *activationJobBuildContext) {
	compilerActivationJobLog.Print("Adding activation artifact upload step")
	c.addActivationInfoArtifactUploadStep(ctx)
	activationArtifactName := artifactPrefixExprForActivationJob(ctx.data) + constants.ActivationArtifactName.String()
	ctx.steps = append(ctx.steps, generateStageAmbientFoldersStep(ctx.data)...)
	ctx.steps = append(ctx.steps,
		"      - name: Stage prompt files for artifact upload\n",
		"        run: |\n",
		"          mkdir -p /tmp/gh-aw/aw-prompts\n",
		"          cp -a \"${RUNNER_TEMP}/gh-aw/aw-prompts/.\" /tmp/gh-aw/aw-prompts/\n",
	)
	ctx.steps = append(ctx.steps, "      - name: "+constants.ActivationUploadArtifactStepName+"\n")
	ctx.steps = append(ctx.steps, "        if: success() || failure()\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")))
	ctx.steps = append(ctx.steps, "        with:\n")
	ctx.steps = append(ctx.steps, fmt.Sprintf("          name: %s\n", activationArtifactName))
	ctx.steps = append(ctx.steps, "          include-hidden-files: true\n")
	ctx.steps = append(ctx.steps, "          path: |\n")
	// Keep aw_info.json in activation for fallback downloads from workflows created
	// before the compact info artifact was available.
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/aw_info.json\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/models.json\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/aw-prompts/prompt.txt\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/aw-prompts/prompt-template.txt\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/aw-prompts/prompt-import-tree.json\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/"+constants.GithubRateLimitsFilename.String()+"\n")
	ctx.steps = append(ctx.steps, "            /tmp/gh-aw/base\n")
	if len(ctx.data.AmbientFolders) > 0 {
		ctx.steps = append(ctx.steps, "            /tmp/gh-aw/ambient-folders\n")
	}
	engineID := resolveActivationEngineID(ctx.data)
	// Include the engine-specific sub-agent staging directory only when inline agents are enabled.
	if isFeatureEnabled(constants.FeatureFlag("inline-agents"), ctx.data) {
		subAgentDir := path.Join(engineConfigBaseDirForRegistry(c.engineRegistry, engineID), "agents")
		ctx.steps = append(ctx.steps, fmt.Sprintf("            /tmp/gh-aw/%s\n", subAgentDir))
	}
	// Always include the engine-specific skill directory when either inline skills are enabled
	// or frontmatter skills are configured (via Skills or SkillReferences).
	if isFeatureEnabled(constants.FeatureFlag("inline-agents"), ctx.data) || len(ctx.data.Skills) > 0 || len(ctx.data.SkillReferences) > 0 {
		skillDir := path.Join(engineConfigBaseDirForRegistry(c.engineRegistry, engineID), "skills")
		ctx.steps = append(ctx.steps, fmt.Sprintf("            /tmp/gh-aw/%s\n", skillDir))
	}
	ctx.steps = append(ctx.steps, "          if-no-files-found: ignore\n")
	ctx.steps = append(ctx.steps, "          retention-days: 1\n")
}

// addActivationInfoArtifactUploadStep appends an archived upload of aw_info.json.
func (c *Compiler) addActivationInfoArtifactUploadStep(ctx *activationJobBuildContext) {
	compilerActivationJobLog.Print("Adding info artifact upload step")
	infoArtifactName := artifactPrefixExprForActivationJob(ctx.data) + constants.InfoArtifactName.String()
	ctx.steps = append(ctx.steps,
		"      - name: Upload info artifact\n",
		"        if: success() || failure()\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %s\n", infoArtifactName),
		"          path: /tmp/gh-aw/aw_info.json\n",
		"          if-no-files-found: ignore\n",
	)
}

// buildActivationEnvironment returns manual-approval environment YAML, with ANSI removed.
func (c *Compiler) buildActivationEnvironment(ctx *activationJobBuildContext) string {
	if ctx.data.ManualApproval == "" {
		return ""
	}
	compilerActivationJobLog.Print("Activation job uses manual-approval environment gate")
	return "environment: " + stringutil.StripANSI(ctx.data.ManualApproval)
}
