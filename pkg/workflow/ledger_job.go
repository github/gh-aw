package workflow

import (
	"fmt"

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
	steps = append(steps,
		"      - name: Download validated ledger transactions\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %s\n", ledgerTransactionsArtifactName),
		"          path: ${{ runner.temp }}/gh-aw\n",
	)
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
		"          GH_AW_LEDGER_TRANSACTIONS: ${{ runner.temp }}/gh-aw/ledger-transactions.json\n",
		fmt.Sprintf("          GH_AW_LEDGER_CONFIG_BASE64: %s\n", ledgerConfig),
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
	}, nil
}
