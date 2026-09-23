package workflow

import (
	"fmt"
	"strings"
)

// generateDailyAICRepoMemoryLedgerSteps appends the current run's AIC usage to
// the repo-memory ledger from the trusted push_repo_memory job. It runs after
// downstream usage artifacts are available and after deleting any agent-supplied
// ledger files from the downloaded repo-memory artifact.
func (c *Compiler) generateDailyAICRepoMemoryLedgerSteps(data *WorkflowData, hasEvals bool) []string {
	entry, ok := dailyAICRepoMemoryEntry(data)
	if !ok {
		return nil
	}
	var steps []string
	prefix := artifactPrefixExprForDownstreamJob(data)
	steps = append(steps, buildAgentOutputDownloadSteps(prefix, c.getActionPin)...)
	if IsDetectionJobEnabled(data.SafeOutputs) {
		steps = append(steps, buildDetectionArtifactDownloadSteps(prefix, c.getActionPin)...)
	}
	steps = append(steps, buildUsageArtifactInputDownloadSteps(prefix, hasEvals, c.getActionPin)...)
	ledgerDir := dailyAICRepoMemoryDir(entry)
	hydrationDir := dailyAICLedgerHydrationDir(entry)
	steps = append(steps, buildDailyAICRepoMemoryHydrationSteps(entry, ledgerDir, hydrationDir)...)
	steps = append(steps, buildDailyAICRepoMemoryAppendStep(data, ledgerDir))
	return steps
}

func buildDailyAICRepoMemoryHydrationSteps(entry RepoMemoryEntry, ledgerDir, hydrationDir string) []string {
	return []string{
		"      - name: Collect daily AIC repo-memory usage files\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        run: bash \"%s/collect_usage_artifact_files.sh\"\n", SetupActionDestinationShell),
		"      - name: Clone daily AIC repo-memory ledger for hydration\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		"        env:\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          BRANCH_NAME: %s\n", entry.BranchName),
		fmt.Sprintf("          TARGET_REPO: %s\n", dailyAICRepoMemoryTargetRepo(entry)),
		fmt.Sprintf("          MEMORY_DIR: %s\n", hydrationDir),
		"          CREATE_ORPHAN: false\n",
		fmt.Sprintf("        run: bash \"%s/clone_repo_memory_branch.sh\"\n", SetupActionDestinationShell),
		"      - name: Reset untrusted daily AIC repo-memory ledger artifact\n",
		"        if: always()\n",
		"        env:\n",
		fmt.Sprintf("          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", ledgerDir),
		"        run: rm -rf \"$GH_AW_DAILY_AIC_REPO_MEMORY_DIR/daily-aic-ledger\"\n",
		"      - name: Hydrate daily AIC repo-memory ledger from trusted branch\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		"        env:\n",
		fmt.Sprintf("          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", ledgerDir),
		fmt.Sprintf("          GH_AW_DAILY_AIC_LEDGER_SOURCE_DIR: %s\n", hydrationDir),
		"        run: |\n" +
			"          mkdir -p \"$GH_AW_DAILY_AIC_REPO_MEMORY_DIR/daily-aic-ledger\"\n" +
			"          if [ -d \"$GH_AW_DAILY_AIC_LEDGER_SOURCE_DIR/daily-aic-ledger\" ]; then\n" +
			"            cp -a \"$GH_AW_DAILY_AIC_LEDGER_SOURCE_DIR/daily-aic-ledger/.\" \"$GH_AW_DAILY_AIC_REPO_MEMORY_DIR/daily-aic-ledger/\"\n" +
			"          fi\n",
	}
}

func buildDailyAICRepoMemoryAppendStep(data *WorkflowData, ledgerDir string) string {
	var builder strings.Builder
	builder.WriteString("      - name: Append daily AIC repo-memory ledger\n")
	builder.WriteString("        if: always()\n")
	builder.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(&builder, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	builder.WriteString("        env:\n")
	for _, line := range buildTemplatableIntEnvVar(maxDailyAICreditsEnvVar, data.MaxDailyAICredits) {
		builder.WriteString(line)
	}
	fmt.Fprintf(&builder, "          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID)
	fmt.Fprintf(&builder, "          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", ledgerDir)
	builder.WriteString("          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { appendCurrentRunLedgerEntry } = require('" + SetupActionDestination + "/daily_aic_repo_memory_ledger.cjs');\n")
	builder.WriteString("            appendCurrentRunLedgerEntry();\n")
	return builder.String()
}
