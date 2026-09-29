package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) buildPushLedgerChangesJob(data *WorkflowData, threatDetectionEnabled bool) *Job {
	needs := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}
	if IsDetectionJobEnabled(data.SafeOutputs) && threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
	}
	steps := append([]string{}, c.generateCheckoutActionsFolder(data)...)
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
		"          GH_AW_LEDGER_BRANCH_PREFIX: ledgers/\n",
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
