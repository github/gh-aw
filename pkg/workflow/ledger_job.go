package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) buildPushLedgerChangesJob(data *WorkflowData, threatDetectionEnabled bool) *Job {
	needs := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}
	if IsDetectionJobEnabled(data.SafeOutputs) && threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
	}
	steps := append([]string{}, buildAgentOutputDownloadSteps(artifactPrefixExprForAgentDownstreamJob(data), c.getActionPin)...)
	steps = append(steps, c.generateCheckoutActionsFolder(data)...)
	steps = append(steps, c.generateSetupStep(data, c.resolveActionReference("./actions/setup", data), SetupActionDestination, false, "", "")...)
	steps = append(steps,
		"      - name: Checkout repository\n",
		fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")),
		"        with:\n",
		"          persist-credentials: false\n",
		"      - name: Reconcile and push ledger changes\n",
		"        id: push_ledger_changes\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		fmt.Sprintf("          GH_AW_LEDGER_TRANSACTIONS: %s%s\n", constants.TmpGhAwDirSlash, constants.SafeOutputsFilename),
		"          GH_AW_LEDGER_TRANSACTION_ID: ${{ github.run_id }}-${{ github.run_attempt }}\n",
		fmt.Sprintf("          GH_AW_LEDGER_CONFIG_B64: %s\n", encodeLedgerJobConfig(data.LedgerConfig)),
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('"+SetupActionDestination+"/push_ledger_changes.cjs');\n",
		"            await main();\n",
	)
	return &Job{
		Name:        pushLedgerChangesJobName,
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		If:          "always()",
		Permissions: "permissions:\n      contents: write",
		Needs:       needs,
		Steps:       steps,
	}
}

func encodeLedgerJobConfig(config *LedgerToolConfig) string {
	if config == nil {
		return ""
	}
	ledgers := make([]map[string]any, 0, len(config.Ledgers))
	for _, ledger := range config.Ledgers {
		ledgers = append(ledgers, map[string]any{
			"name":         ledger.Name,
			"schema":       ledger.Schema,
			"schemaPath":   ledger.SchemaPath,
			"maxRecordKB":  ledger.MaxRecordKB,
			"maxSegmentKB": ledger.MaxSegmentKB,
			"maxPatchKB":   ledger.MaxPatchKB,
			"branchName":   ledger.BranchName,
		})
	}
	encoded, err := json.Marshal(ledgers)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(encoded)
}
