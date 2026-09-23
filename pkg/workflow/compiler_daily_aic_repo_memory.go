package workflow

import (
	"fmt"
	"strings"
)

func (c *Compiler) generateDailyAICRepoMemoryLedgerStep(builder *strings.Builder, data *WorkflowData) {
	entry, ok := dailyAICRepoMemoryEntry(data)
	if !ok {
		return
	}
	builder.WriteString("      - name: Append daily AIC repo-memory ledger\n")
	builder.WriteString("        if: always()\n")
	builder.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(builder, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID)
	fmt.Fprintf(builder, "          GH_AW_DAILY_AIC_REPO_MEMORY_DIR: %s\n", dailyAICRepoMemoryDir(entry))
	builder.WriteString("          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { appendCurrentRunLedgerEntry } = require('" + SetupActionDestination + "/daily_aic_repo_memory_ledger.cjs');\n")
	builder.WriteString("            appendCurrentRunLedgerEntry();\n")
}
