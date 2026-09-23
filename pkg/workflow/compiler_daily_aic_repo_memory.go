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
	steps = append(steps,
		"      - name: Collect daily AIC repo-memory usage files\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        run: bash \"%s/collect_usage_artifact_files.sh\"\n", SetupActionDestinationShell),
		"      - name: Reset untrusted daily AIC repo-memory ledger artifact\n",
		"        if: always()\n",
		"        env:\n",
		fmt.Sprintf("          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", dailyAICRepoMemoryDir(entry)),
		"        run: rm -rf \"$GH_AW_DAILY_AIC_REPO_MEMORY_DIR/daily-aic-ledger\"\n",
	)
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
	fmt.Fprintf(&builder, "          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", dailyAICRepoMemoryDir(entry))
	builder.WriteString("          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { appendCurrentRunLedgerEntry } = require('" + SetupActionDestination + "/daily_aic_repo_memory_ledger.cjs');\n")
	builder.WriteString("            appendCurrentRunLedgerEntry();\n")
	steps = append(steps, builder.String())
	return steps
}
