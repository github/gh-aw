package workflow

import (
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var jobTimeoutsLog = logger.New("workflow:job_timeouts")

func safeOutputsJobTimeoutMinutes(data *WorkflowData) int {
	if data.SafeOutputs.TimeoutMinutes > 0 {
		return data.SafeOutputs.TimeoutMinutes
	}
	return 45
}

// resolveAgentJobTimeoutValue returns the timeout-minutes value emitted on the
// generated agent job. The agent job bounds every step of the job (setup,
// agentic execution and teardown), so it is resolved independently from the
// agentic execution step timeout:
//  1. jobs.agent.timeout-minutes
//  2. constants.DefaultAgentJobTimeout (60 minutes)
//
// When top-level timeout-minutes explicitly requests a longer agentic step than
// the built-in job default, the built-in default is raised to that value so an
// explicit step budget is never truncated by the implicit job budget.
func resolveAgentJobTimeoutValue(data *WorkflowData) string {
	if override, ok := builtinJobTimeoutOverride(data, string(constants.AgentJobName)); ok {
		return override
	}
	builtinDefault := int(constants.DefaultAgentJobTimeout / time.Minute)
	if stepMinutes, ok := literalStepTimeoutMinutes(data); ok && stepMinutes > builtinDefault {
		jobTimeoutsLog.Printf("Raising agent job default timeout to %d minutes to cover the configured step timeout", stepMinutes)
		builtinDefault = stepMinutes
	}
	return strconv.Itoa(builtinDefault)
}

// resolveDetectionJobTimeoutValue returns the timeout-minutes value emitted on
// the generated detection job and on its agentic execution step:
//  1. jobs.detection.timeout-minutes
//  2. constants.DefaultDetectionJobTimeout (10 minutes)
func resolveDetectionJobTimeoutValue(data *WorkflowData) string {
	if override, ok := builtinJobTimeoutOverride(data, string(constants.DetectionJobName)); ok {
		return override
	}
	return strconv.Itoa(int(constants.DefaultDetectionJobTimeout / time.Minute))
}

// builtinJobTimeoutOverride returns the jobs.<name>.timeout-minutes value
// configured in frontmatter, if any.
func builtinJobTimeoutOverride(data *WorkflowData, jobName string) (string, bool) {
	if data == nil {
		return "", false
	}
	jobConfig, ok := data.Jobs[jobName].(map[string]any)
	if !ok {
		return "", false
	}
	job := &Job{}
	if err := extractCustomJobTimeoutMinutes(job, jobName, jobConfig); err != nil {
		// Invalid values are reported by the jobs.<name> validation pass.
		jobTimeoutsLog.Printf("Ignoring invalid jobs.%s.timeout-minutes: %v", jobName, err)
		return "", false
	}
	if job.TimeoutMinutes > 0 {
		return strconv.Itoa(job.TimeoutMinutes), true
	}
	return "", false
}

// literalStepTimeoutMinutes returns the agentic execution step timeout when it
// resolves to a compile-time integer. Expressions cannot be compared at compile
// time and report false.
func literalStepTimeoutMinutes(data *WorkflowData) (int, bool) {
	raw := strings.TrimSpace(resolveStepTimeoutValue(data))
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes <= 0 {
		return 0, false
	}
	return minutes, true
}
