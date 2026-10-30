package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) appendDecisionArtifactStep(data *WorkflowData, steps []string) []string {
	name := constants.DecisionGateArtifactName.String()
	if hasWorkflowCallTrigger(data.On) {
		name = "${{ steps.setup.outputs.span-id }}-" + name
	}
	return append(steps,
		"      - name: Upload decision gate receipt\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/upload-artifact", data)),
		"        with:\n",
		"          name: "+name+"\n",
		"          path: ${{ runner.temp }}/gh-aw/decision-gate.json\n",
		"          if-no-files-found: ignore\n",
	)
}
