package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) buildPushLedgerChangesJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	ledgerConfig, err := encodeLedgerConfigBase64(data.LedgerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ledger persistence configuration: %w", err)
	}
	needs := []string{string(constants.AgentJobName), string(constants.ActivationJobName), "safe_outputs"}
	if IsDetectionJobEnabled(data.SafeOutputs) && threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
	}
	steps := append([]string{}, c.generateCheckoutActionsFolder(data)...)
	steps = append(steps, c.generateSetupStepForJob("push_ledger_changes", data, c.resolveActionReference("./actions/setup", data), SetupActionDestination, false, "", "", "")...)
	if data.LedgerConfig.hasCommentStorage() {
		lines, _ := c.generateCommentMemoryEarlyConfigLines(data)
		steps = append(steps, lines...)
	}
	steps = append(steps,
		"      - name: Download validated ledger transactions\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %s\n", ledgerTransactionsArtifactName),
		fmt.Sprintf("          path: %s\n", ledgerTransactionsArtifactDir),
	)
	steps = append(steps,
		"      - name: Checkout repository\n",
		fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")),
		"        with:\n",
		"          persist-credentials: false\n",
		"      - name: Reconcile and push ledger changes\n",
		"        id: push_ledger_changes\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		fmt.Sprintf("          GH_AW_LEDGER_TRANSACTIONS: %s\n", ledgerTransactionsArtifactPath),
		fmt.Sprintf("          GH_AW_LEDGER_TRANSACTIONS_ARTIFACT_NAME: %s\n", ledgerTransactionsArtifactName),
		fmt.Sprintf("          GH_AW_LEDGER_CONFIG_BASE64: %s\n", ledgerConfig),
	)
	steps = append(steps, ledgerCommentMemoryCredentialEnv(data)...)
	steps = append(steps, c.ledgerPersistenceStagedEnv(data)...)
	steps = append(steps, "        with:\n")
	steps = append(steps,
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('"+SetupActionDestination+"/push_ledger_changes.cjs');\n",
		"            await main();\n",
	)
	permissions, concurrency := ledgerPersistencePolicy(data)
	return &Job{
		Name:        pushLedgerChangesJobName,
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		If:          "always() && needs.safe_outputs.result == 'success'",
		Permissions: permissions,
		Concurrency: concurrency,
		Needs:       needs,
		Steps:       steps,
	}, nil
}

func ledgerCommentMemoryCredentialEnv(data *WorkflowData) []string {
	if !data.LedgerConfig.hasCommentStorage() {
		return nil
	}
	token := data.CommentMemoryConfig.GitHubToken
	return []string{
		fmt.Sprintf("          GH_AW_COMMENT_MEMORY_TOKEN: %s\n", resolveSafeOutputGitHubToken(token)),
		fmt.Sprintf("          GH_AW_COMMENT_MEMORY_TOKEN_SOURCE: %s\n", safeOutputGitHubTokenSourceForToken(token)),
	}
}

func (c *Compiler) ledgerPersistenceStagedEnv(data *WorkflowData) []string {
	if !data.LedgerConfig.hasCommentStorage() {
		return nil
	}
	var staged *TemplatableBool
	if data.SafeOutputs != nil {
		staged = data.SafeOutputs.Staged
	}
	if value := resolveSafeOutputsStagedValue(c.trialMode, staged); value != nil {
		return []string{fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_STAGED: %q\n", *value)}
	}
	return nil
}

func ledgerPersistencePolicy(data *WorkflowData) (string, string) {
	permissions := NewPermissionsContentsWrite()
	concurrency := ""
	if data.LedgerConfig.hasCommentStorage() {
		permissions.Set(PermissionContents, PermissionRead)
		permissions.Set(PermissionIssues, PermissionWrite)
		permissions.Set(PermissionPullRequests, PermissionWrite)
		for _, ledger := range data.LedgerConfig.Ledgers {
			if ledger.Storage != "comment-memory" {
				permissions.Set(PermissionContents, PermissionWrite)
				break
			}
		}
		target := data.CommentMemoryConfig.Target
		targetExpression := "(github.event.issue != null && github.event.issue.number) || (github.event.pull_request != null && github.event.pull_request.number)"
		if target != "" && target != "triggering" && target != "*" {
			targetExpression = "'" + strings.ReplaceAll(target, "'", "''") + "'"
		}
		repoExpression := "github.repository"
		if repo := data.CommentMemoryConfig.TargetRepoSlug; repo != "" {
			repoExpression = "'" + strings.ReplaceAll(strings.ToLower(repo), "'", "''") + "'"
		}
		concurrency = fmt.Sprintf("concurrency:\n      group: ${{ format('gh-aw-comment-ledger-{0}-{1}', %s, %s) }}\n      cancel-in-progress: false", repoExpression, targetExpression)
		if isGroupConcurrencyQueueEnabled(data) {
			concurrency += "\n      queue: max"
		}
	}
	return permissions.RenderToYAML(), concurrency
}
