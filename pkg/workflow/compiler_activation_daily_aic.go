package workflow

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// compiler_activation_daily_aic contains daily AIC guardrail token and step builders.

const dailyAICAppTokenStepID = "daily-aic-app-token"
const dailyAICDefaultRepoMemoryID = "default"

// buildDailyAICAppTokenMintStep generates a GitHub App token mint step dedicated
// to the daily AIC guardrail. The minted token is used only for the guardrail API
// calls, avoiding depletion of credentials held by the main activation app or
// GITHUB_TOKEN.
//
// The step is gated on maxDailyAICreditsConfiguredIfExpr so it is skipped when
// the guardrail is not active at runtime.
func (c *Compiler) buildDailyAICAppTokenMintStep(app *GitHubAppConfig) []string {
	var steps []string
	steps = append(steps, "      - name: Generate GitHub App token for daily AIC guardrail\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", dailyAICAppTokenStepID))
	if app.shouldIgnoreMissingKey() {
		guard := buildIgnoreIfMissingCondition(app)
		steps = appendStepEnvAssignments(steps, guard.EnvAssignments)
		if condition := combineGitHubIfExpressions(maxDailyAICreditsConfiguredIfExpr, guard.Condition); condition != "" {
			steps = append(steps, fmt.Sprintf("        if: %s\n", condition))
		} else {
			steps = append(steps, fmt.Sprintf("        if: %s\n", maxDailyAICreditsConfiguredIfExpr))
		}
	} else {
		steps = append(steps, fmt.Sprintf("        if: %s\n", maxDailyAICreditsConfiguredIfExpr))
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/create-github-app-token")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          client-id: %s\n", app.AppID))
	steps = append(steps, fmt.Sprintf("          private-key: %s\n", app.PrivateKey))
	owner := app.Owner
	if owner == "" {
		owner = "${{ github.repository_owner }}"
	}
	steps = append(steps, fmt.Sprintf("          owner: %s\n", owner))
	steps = appendDailyAICAppRepositories(steps, app.Repositories)
	steps = append(steps, "          github-api-url: ${{ github.api_url }}\n")
	// Build permission fields: baseline is actions: read (required for guardrail script to read
	// workflow run data). Merge any user-configured app.Permissions on top so callers can extend
	// or override the scope without changing the compiler. Sort keys for deterministic output.
	basePerms := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionActions: PermissionRead,
	})
	permissionFields := convertPermissionsToAppTokenFields(basePerms)
	for key, val := range app.Permissions {
		scope := convertStringToPermissionScope(key)
		if scope == "" {
			safeOutputsAppLog.Printf("Skipping unknown permission scope %q in max-daily-ai-credits github-app.permissions", key)
			continue
		}
		level := strings.ToLower(strings.TrimSpace(val))
		tempPerms := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{scope: PermissionLevel(level)})
		maps.Copy(permissionFields, convertPermissionsToAppTokenFields(tempPerms))
	}
	for _, key := range sliceutil.SortedKeys(permissionFields) {
		steps = append(steps, fmt.Sprintf("          %s: %s\n", key, permissionFields[key]))
	}
	return steps
}

func appendDailyAICAppRepositories(steps []string, repositories []string) []string {
	if len(repositories) == 1 {
		for _, repository := range repositories {
			if repository != "*" {
				steps = append(steps, fmt.Sprintf("          repositories: %s\n", repository))
			}
		}
	} else if len(repositories) > 1 {
		steps = append(steps, "          repositories: |-\n")
		for _, repo := range repositories {
			steps = append(steps, fmt.Sprintf("            %s\n", repo))
		}
	} else {
		steps = append(steps, "          repositories: ${{ github.event.repository.name }}\n")
	}
	return steps
}

// resolveDailyAICToken returns the GitHub token to use for daily AIC guardrail steps.
// When a dedicated MaxDailyAICreditsGitHubApp is configured, it references the
// minted token from that step. Otherwise it falls back to the activation token.
func (c *Compiler) resolveDailyAICToken(data *WorkflowData) string {
	if data.MaxDailyAICreditsGitHubApp != nil {
		if data.MaxDailyAICreditsGitHubApp.shouldIgnoreMissingKey() {
			return combineTokenExpressions(
				fmt.Sprintf("${{ steps.%s.outputs.token }}", dailyAICAppTokenStepID),
				c.resolveActivationToken(data),
			)
		}
		return fmt.Sprintf("${{ steps.%s.outputs.token }}", dailyAICAppTokenStepID)
	}
	return c.resolveActivationToken(data)
}

func (c *Compiler) buildActivationDailyAICGuardrailStep(data *WorkflowData) []string {
	compilerActivationJobLog.Printf("Building daily AIC guardrail step: dedicated_app=%t, cache_enabled=%t", data.MaxDailyAICreditsGitHubApp != nil, data.WorkflowID != "")
	var steps []string
	// When a dedicated GitHub App is configured for the daily AIC guardrail, mint
	// its token first so the subsequent steps can reference it.
	if data.MaxDailyAICreditsGitHubApp != nil {
		compilerActivationJobLog.Print("Prepending dedicated daily-AIC app-token mint step")
		steps = append(steps, c.buildDailyAICAppTokenMintStep(data.MaxDailyAICreditsGitHubApp)...)
	}
	if entry, ok := dailyAICRepoMemoryEntry(data); ok {
		steps = append(steps, buildDailyAICRepoMemoryCloneStep(entry)...)
	}
	// Only restore observations from a verified workflow-run artifact. Actions
	// cache restore-key matches do not establish producer provenance or freshness.
	if data.WorkflowID != "" && data.MaxDailyAICBackend != maxDailyAICBackendRepoMemory {
		steps = append(steps, c.buildDailyAICScanObservationRestoreStep(data)...)
	}
	steps = append(steps, "      - name: Check daily workflow token guardrail\n")
	steps = append(steps, "        id: daily-ai-credits-workflow-guardrail\n")
	steps = append(steps, fmt.Sprintf("        if: %s\n", maxDailyAICreditsConfiguredIfExpr))
	steps = appendDailyAICContinueOnError(steps, data)
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID))
	steps = append(steps, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	steps = append(steps, "          GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT: ${{ github.event.inputs.aw_context || '' }}\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_HAS_SLASH_COMMAND: %q\n", strconv.FormatBool(len(data.Command) > 0)))
	steps = append(steps, fmt.Sprintf("          GH_AW_HAS_LABEL_COMMAND: %q\n", strconv.FormatBool(len(data.LabelCommand) > 0)))
	steps = append(steps, fmt.Sprintf("          GH_AW_GITHUB_TOKEN: %s\n", c.resolveDailyAICToken(data)))
	steps = append(steps, buildTemplatableIntEnvVar(maxDailyAICreditsEnvVar, data.MaxDailyAICredits)...)
	if data.MaxDailyAICBackend != "" {
		steps = append(steps, fmt.Sprintf("          %s: %q\n", maxDailyAICBackendEnvVar, data.MaxDailyAICBackend))
	}
	if data.MaxDailyAICBackend == maxDailyAICBackendRepoMemory {
		steps = append(steps, "          GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC: ${{ vars.GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC || 'false' }}\n")
	}
	if entry, ok := dailyAICRepoMemoryEntry(data); ok {
		steps = append(steps, fmt.Sprintf("          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", dailyAICRepoMemoryDir(entry)))
	}
	steps = append(steps, fmt.Sprintf("          GH_AW_MAX_AI_CREDITS: %s\n", dailyAICMaxCreditsEnvValue(data)))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          github-token: %s\n", c.resolveDailyAICToken(data)))
	steps = append(steps, "          script: |\n")
	steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
	steps = append(steps, "            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	steps = append(steps, "            const { main } = require('"+SetupActionDestination+"/check_daily_aic_workflow_guardrail.cjs');\n")
	steps = append(steps, "            await main();\n")
	if data.WorkflowID != "" && data.MaxDailyAICBackend != maxDailyAICBackendRepoMemory {
		steps = append(steps, "      - name: Publish daily AIC scan observations\n")
		steps = append(steps, "        if: always() && env.GH_AW_MAX_DAILY_AI_CREDITS != ''\n")
		steps = append(steps, "        continue-on-error: true\n")
		steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/upload-artifact", data)))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          name: aic-usage-scan-v2\n")
		steps = append(steps, "          path: /tmp/gh-aw/agentic-workflow-usage-scan-v2.jsonl\n")
		steps = append(steps, "          overwrite: true\n")
		steps = append(steps, "          if-no-files-found: ignore\n")
		steps = append(steps, "          retention-days: 3\n")
	}
	return steps
}

func (c *Compiler) buildDailyAICScanObservationRestoreStep(data *WorkflowData) []string {
	return []string{
		"      - name: Restore daily AIC scan observations\n",
		"        id: restore-daily-aic-cache-fallback\n",
		fmt.Sprintf("        if: %s\n", maxDailyAICreditsConfiguredIfExpr),
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_HAS_SLASH_COMMAND: %q\n", strconv.FormatBool(len(data.Command) > 0)),
		fmt.Sprintf("          GH_AW_HAS_LABEL_COMMAND: %q\n", strconv.FormatBool(len(data.LabelCommand) > 0)),
		"        with:\n",
		fmt.Sprintf("          github-token: %s\n", c.resolveDailyAICToken(data)),
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('" + SetupActionDestination + "/restore_aic_scan_cache.cjs');\n",
		"            await main();\n",
	}
}

// dailyAICRepoMemoryEntry selects the repo-memory ledger for the repo-memory
// daily AIC backend. It prefers the memory with id "default" and otherwise
// falls back to the first configured memory.
func dailyAICRepoMemoryEntry(data *WorkflowData) (RepoMemoryEntry, bool) {
	if data == nil || data.MaxDailyAICBackend != maxDailyAICBackendRepoMemory || data.RepoMemoryConfig == nil {
		return RepoMemoryEntry{}, false
	}
	for _, memory := range data.RepoMemoryConfig.Memories {
		if memory.ID == dailyAICDefaultRepoMemoryID {
			return memory, true
		}
	}
	return firstRepoMemoryEntry(data.RepoMemoryConfig.Memories)
}

func firstRepoMemoryEntry(memories []RepoMemoryEntry) (RepoMemoryEntry, bool) {
	if len(memories) == 0 {
		return RepoMemoryEntry{}, false
	}
	return memories[0], true //nolint:uncheckedsliceindex // len(memories) is checked above.
}

func dailyAICRepoMemoryDir(memory RepoMemoryEntry) string {
	return constants.TmpRepoMemoryDir + memory.ID
}

// dailyAICLedgerHydrationDir returns a directory, distinct from the
// agent-writable repo-memory artifact directory, into which the push_repo_memory
// job clones a read-only snapshot of the committed ledger branch. This trusted
// snapshot is used to hydrate the ledger before appending the current run's
// entry, so accumulated history from other runs is never discarded.
func dailyAICLedgerHydrationDir(memory RepoMemoryEntry) string {
	return constants.TmpGhAwDir + "/daily-aic-ledger-source/" + memory.ID
}

// dailyAICRepoMemoryTargetRepo resolves the target repository reference for the
// repo-memory ledger, matching the resolution used for the primary repo-memory
// clone/push steps (defaulting to the current repository and appending ".wiki"
// for wiki-backed memories).
func dailyAICRepoMemoryTargetRepo(memory RepoMemoryEntry) string {
	targetRepo := memory.TargetRepo
	if targetRepo == "" {
		targetRepo = "${{ github.repository }}"
	}
	if memory.Wiki {
		targetRepo += ".wiki"
	}
	return targetRepo
}

func buildDailyAICRepoMemoryCloneStep(memory RepoMemoryEntry) []string {
	targetRepo := dailyAICRepoMemoryTargetRepo(memory)
	memoryLabel := "repo-memory"
	if memory.Wiki {
		memoryLabel = "wiki-memory"
	}
	memoryDir := dailyAICRepoMemoryDir(memory)
	return []string{
		fmt.Sprintf("      - name: Clone daily AIC %s ledger (%s)\n", memoryLabel, memory.ID),
		fmt.Sprintf("        if: %s\n", maxDailyAICreditsConfiguredIfExpr),
		"        env:\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          BRANCH_NAME: %s\n", memory.BranchName),
		fmt.Sprintf("          TARGET_REPO: %s\n", targetRepo),
		fmt.Sprintf("          MEMORY_DIR: %s\n", memoryDir),
		fmt.Sprintf("          CREATE_ORPHAN: %t\n", memory.CreateOrphan),
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/clone_repo_memory_branch.sh\"\n",
	}
}

func appendDailyAICContinueOnError(steps []string, data *WorkflowData) []string {
	if data.MaxDailyAICContinueOnError {
		return append(steps, "        continue-on-error: true\n")
	}
	return steps
}

func dailyAICMaxCreditsEnvValue(data *WorkflowData) string {
	if data.EngineConfig != nil && data.EngineConfig.MaxAICredits != 0 {
		return strconv.Quote(strconv.FormatInt(data.EngineConfig.MaxAICredits, 10))
	}
	return compilerenv.BuildDefaultMaxAICreditsExpression(strconv.FormatInt(constants.DefaultMaxAICredits, 10))
}

func buildDailyAICActivationJobEnv(data *WorkflowData) map[string]string {
	if !hasMaxDailyAICGuardrail(data) || !hasMaxDailyAICFrontmatterConfig(data) {
		return nil
	}
	value := strings.TrimSpace(*data.MaxDailyAICredits)
	if value == "" {
		compilerActivationJobLog.Print("Daily AIC guardrail configured but max-daily-ai-credits value is empty; omitting activation job env")
		return nil
	}
	if isExpression(value) {
		return map[string]string{maxDailyAICreditsEnvVar: value}
	}
	return map[string]string{maxDailyAICreditsEnvVar: strconv.Quote(value)}
}
