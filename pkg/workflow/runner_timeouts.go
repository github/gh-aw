package workflow

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

const ubuntuSlimMaxTimeoutMinutes = 15

// resolveJobRunnerTimeouts runs after job augmentation so it checks the effective
// timeout, not just the default supplied by each job builder.
func (c *Compiler) resolveJobRunnerTimeouts(data *WorkflowData) error {
	for _, name := range c.jobManager.jobOrder {
		job := c.jobManager.jobs[name]
		minutes := job.TimeoutMinutes
		if job.TimeoutMinutesExpression != "" {
			var err error
			minutes, err = strconv.Atoi(strings.TrimSpace(job.TimeoutMinutesExpression))
			if err != nil {
				continue
			}
		}
		if minutes <= ubuntuSlimMaxTimeoutMinutes || job.Uses != "" || strings.Contains(job.RunsOn, "${{") {
			continue
		}

		var runner map[string]any
		if err := yaml.Unmarshal([]byte(job.RunsOn), &runner); err != nil {
			return fmt.Errorf("job '%s' runs-on could not be parsed: %w", name, err)
		}
		if !usesUbuntuSlim(runner["runs-on"]) {
			continue
		}

		if usesDefaultFrameworkRunner(data, name) {
			job.RunsOn = "runs-on: ubuntu-latest"
			jobTimeoutsLog.Printf("Using ubuntu-latest for job %s because timeout-minutes %d exceeds ubuntu-slim's %d-minute limit", name, minutes, ubuntuSlimMaxTimeoutMinutes)
			continue
		}
		return fmt.Errorf("job '%s' sets timeout-minutes to %d, but ubuntu-slim supports at most %d minutes; use runs-on: ubuntu-latest or reduce timeout-minutes to %d", name, minutes, ubuntuSlimMaxTimeoutMinutes, ubuntuSlimMaxTimeoutMinutes)
	}
	return nil
}

func usesDefaultFrameworkRunner(data *WorkflowData, jobName string) bool {
	if !isBuiltinJobName(jobName) || jobName == string(constants.AgentJobName) || jobName == string(constants.DetectionJobName) {
		return false
	}
	return data != nil && data.RunsOnSlim == "" && (data.SafeOutputs == nil || data.SafeOutputs.RunsOn == "")
}

func usesUbuntuSlim(runner any) bool {
	switch value := runner.(type) {
	case string:
		return value == "ubuntu-slim"
	case []any:
		return slices.ContainsFunc(value, usesUbuntuSlim)
	case map[string]any:
		return usesUbuntuSlim(value["labels"])
	}
	return false
}
