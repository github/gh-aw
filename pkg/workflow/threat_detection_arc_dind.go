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

func buildCollectArcDindDetectionResultsStep(data *WorkflowData) []string {
	if !isArcDindTopology(data) {
		return nil
	}
	return []string{
		"      - name: Collect ARC/DinD threat detection results\n",
		"        if: always() && steps.detection_agentic_execution.outcome != 'skipped'\n",
		"        continue-on-error: true\n",
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh\" collect\n",
	}
}
