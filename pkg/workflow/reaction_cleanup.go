package workflow

import "fmt"

func reactionCleanupEnabled(data *WorkflowData) bool {
	return data.ReactionRemoveOnComplete && data.AIReaction != "" && data.AIReaction != ReactionTypeNone && !data.DryRun
}

func addReactionCleanupPermissions(data *WorkflowData, permissions *Permissions) {
	ctx := newActivationBuildContext(data, false, "", "")
	ctx.hasStatusComment = false
	addActivationAppInteractionPermissions(permissions, ctx)
}

func addReactionCleanupJobPermissions(data *WorkflowData, permissions *Permissions) {
	if reactionCleanupEnabled(data) && activationTokenMayUseGitHubToken(data) {
		addReactionCleanupPermissions(data, permissions)
	}
}

func withReactionCleanupCondition(data *WorkflowData, condition ConditionNode) ConditionNode {
	if !reactionCleanupEnabled(data) {
		return condition
	}
	hasReaction := BuildNotEquals(BuildPropertyAccess("needs.activation.outputs.reaction_id"), BuildStringLiteral(""))
	return BuildOr(condition, BuildAnd(BuildFunctionCall("always"), hasReaction))
}

func (c *Compiler) buildReactionCleanupSteps(data *WorkflowData) []string {
	if !reactionCleanupEnabled(data) {
		return nil
	}
	var steps []string
	if data.ActivationGitHubApp != nil {
		permissions := NewPermissions()
		addReactionCleanupPermissions(data, permissions)
		steps = append(steps, c.buildGitHubAppTokenMintStepForJob(
			"conclusion", data.ActivationGitHubApp, permissions, "",
			"",
			"Generate GitHub App token for reaction cleanup", "activation-app-token",
			"always() && needs.activation.outputs.reaction_id != ''",
		)...)
	}
	steps = append(steps,
		"      - name: Remove trigger reaction on completion\n",
		"        id: remove-trigger-reaction\n",
		"        if: always() && needs.activation.outputs.reaction_id != ''\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_REACTION: %q\n", data.AIReaction),
		"          GH_AW_REMOVE_REACTION_ID: ${{ needs.activation.outputs.reaction_id }}\n",
		"        with:\n",
		fmt.Sprintf("          github-token: %s\n", c.resolveActivationToken(data)),
		"          script: |\n",
	)
	return append(steps, generateGitHubScriptWithRequire("add_reaction.cjs"))
}
