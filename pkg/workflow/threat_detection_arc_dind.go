package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/constants"
)

const threatDetectionRWMount = constants.ThreatDetectionDir + ":" + constants.ThreatDetectionDir + ":rw"

// The external detector adds this mount to its synthetic workflow data. Keep
// its ARC mount out of shellJoinArgs: RUNNER_TEMP must expand on the runner,
// rather than becoming a literal directory name in a single-quoted argument.
func usesArcDindDetectionMount(data *WorkflowData) bool {
	if !isArcDindTopology(data) || !data.IsDetectionRun {
		return false
	}
	agent := getAgentConfig(data)
	return agent != nil && slices.Contains(agent.Mounts, threatDetectionRWMount)
}

func buildResetDetectionResultsStep(data *WorkflowData) []string {
	steps := []string{
		"      - name: Reset threat detection staging\n",
		"        id: detection_staging_reset\n",
		"        if: always()\n",
		"        run: |\n",
		"          rm -rf /tmp/gh-aw/threat-detection /tmp/gh-aw/threat-detection-inputs\n",
	}
	if isArcDindTopology(data) {
		steps = append(steps, "          bash \"${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh\" reset\n")
	}
	return steps
}

func buildCollectArcDindDetectionResultsStep(data *WorkflowData) []string {
	if !isArcDindTopology(data) {
		return nil
	}
	return []string{
		"      - name: Collect ARC/DinD threat detection results\n",
		"        id: detection_collect\n",
		"        if: always() && steps.detection_staging_reset.outcome == 'success' && (steps.detection_agentic_execution.outcome == 'success' || steps.detection_agentic_execution.outcome == 'failure')\n",
		"        continue-on-error: true\n",
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh\" collect\n",
	}
}
