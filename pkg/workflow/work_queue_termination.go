package workflow

import "fmt"

// buildWorkQueueTerminationStep releases the run's Claims that are still open
// when the worker run ends without a finish commit, then projects Issues.
func (c *Compiler) buildWorkQueueTerminationStep(data *WorkflowData) []string {
	if !workQueueRequiresAssignment(data) {
		return nil
	}
	issues := workQueueIssuesConfig(data) != nil
	var steps []string
	if issues {
		steps = append(steps, c.workQueueProjectorTokenSteps(data, "conclusion")...)
	}
	steps = append(steps,
		"      - name: Terminate work queue worker\n",
		"        id: worker_termination\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	)
	steps = append(steps, workQueuePolicyEnvironment(data)...)
	var staged *TemplatableBool
	if data.SafeOutputs != nil {
		staged = data.SafeOutputs.Staged
	}
	if value := resolveSafeOutputsStagedValue(c.trialMode, staged); value != nil {
		steps = append(steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_STAGED: %q\n", *value))
	}
	steps = append(steps, "        with:\n")
	if issues {
		c.addWorkQueueProjectorToken(&steps, data, "conclusion")
	}
	return append(steps,
		"          script: |\n",
		"            const { main } = require('${{ runner.temp }}/gh-aw/actions/reconcile_work_queue_run.cjs');\n",
		"            await main({ core, githubClient: github, context });\n",
	)
}
