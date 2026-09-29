package workflow

import (
	"fmt"
	"strings"
)

// Ledger mutations are audited, not requested. The trusted ledger MCP server appends
// one redacted `ledger_mutation` entry to the safe-output transaction log after each
// durable agent append. Declaring the type here lets the ingestion step accept those
// entries and lets the log-only handler report them in the safe-outputs job, so
// ledger writes are reviewable next to every other safe output.
const (
	// ledgerMutationHandlerKey is the safe-output type and handler key for ledger audit entries.
	ledgerMutationHandlerKey = "ledger_mutation"
	// LedgerMutationDefaultMax bounds how many ledger audit entries one run may report.
	// Appends are already bounded by the ledger record, segment, patch, and shard limits;
	// this bound only keeps the transaction log and step summary readable.
	LedgerMutationDefaultMax = 1000
)

// repoMemoryLedgerEnabled reports whether any repo-memory entry enables the ledger.
func repoMemoryLedgerEnabled(repoMemoryConfig *RepoMemoryConfig) bool {
	if repoMemoryConfig == nil {
		return false
	}
	for _, memory := range repoMemoryConfig.Memories {
		if memory.Ledger != nil {
			return true
		}
	}
	return false
}

// buildLedgerMutationHandlerConfig returns the log-only handler configuration for
// ledger audit entries, or nil when no repo-memory ledger is configured.
func buildLedgerMutationHandlerConfig(repoMemoryConfig *RepoMemoryConfig) map[string]any {
	if !repoMemoryLedgerEnabled(repoMemoryConfig) {
		return nil
	}
	return map[string]any{"max": LedgerMutationDefaultMax}
}

// generateLedgerAuditMergeStep merges the redacted ledger transaction log written by the
// trusted ledger MCP server into the safe-output file, so audit entries are ingested,
// validated, bounded, and reported by the log-only `ledger_mutation` handler.
// The merge runs on the runner after the agent step and never fails the workflow.
func (c *Compiler) generateLedgerAuditMergeStep(yaml *strings.Builder, data *WorkflowData) {
	if data == nil || data.SafeOutputs == nil || !repoMemoryLedgerEnabled(data.RepoMemoryConfig) {
		return
	}
	yaml.WriteString("      - name: Merge ledger audit entries\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/merge_ledger_transactions.cjs');\n")
	yaml.WriteString("            await main();\n")
}
