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

// resolveJobRunnerTimeouts runs after job augmentation so it can distinguish
// author-configured timeouts from compiler defaults on the effective jobs.
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
		if job.Uses != "" || strings.Contains(job.RunsOn, "${{") {
			continue
		}

		if name == string(constants.SafeOutputsJobName) &&
			data != nil &&
			usesPatchesAndCheckouts(data.SafeOutputs) &&
			usesDefaultFrameworkRunner(data, name) {
			job.RunsOn = "runs-on: ubuntu-latest # Safe-output code publication checks out repository files; ubuntu-slim is limited to 15 minutes."
			jobTimeoutsLog.Printf("Using ubuntu-latest for job %s because it checks out and publishes repository code", name)
			continue
		}

		if minutes <= ubuntuSlimMaxTimeoutMinutes {
			continue
		}

		runsOnSlim, err := jobUsesUbuntuSlim(job)
		if err != nil {
			return fmt.Errorf("job '%s' runs-on could not be parsed: %w", name, err)
		}
		if !runsOnSlim {
			continue
		}

		if !hasExplicitJobTimeout(data, name, job) {
			job.TimeoutMinutes = ubuntuSlimMaxTimeoutMinutes
			job.TimeoutMinutesExpression = ""
			jobTimeoutsLog.Printf("Capping default timeout for job %s at ubuntu-slim's %d-minute limit", name, ubuntuSlimMaxTimeoutMinutes)
			continue
		}

		if usesDefaultFrameworkRunner(data, name) {
			job.RunsOn = "runs-on: ubuntu-latest # ubuntu-slim supports job timeouts of at most 15 minutes."
			jobTimeoutsLog.Printf("Using ubuntu-latest for job %s because timeout-minutes %d exceeds ubuntu-slim's %d-minute limit", name, minutes, ubuntuSlimMaxTimeoutMinutes)
			continue
		}
		return fmt.Errorf("job '%s' sets timeout-minutes to %d, but ubuntu-slim supports at most %d minutes; set %s to ubuntu-latest or reduce timeout-minutes to %d", name, minutes, ubuntuSlimMaxTimeoutMinutes, runnerConfigurationField(data, name), ubuntuSlimMaxTimeoutMinutes)
	}
	return nil
}

func hasExplicitJobTimeout(data *WorkflowData, jobName string, job *Job) bool {
	if data == nil {
		return job.TimeoutMinutes > 0
	}
	for configuredName, rawConfig := range data.Jobs {
		if normalizeBuiltinJobAlias(configuredName) != jobName {
			continue
		}
		config, ok := rawConfig.(map[string]any)
		if ok {
			_, configured := config["timeout-minutes"]
			if configured {
				return true
			}
		}
	}
	if jobName == string(constants.SafeOutputsJobName) && data.SafeOutputs != nil {
		return data.SafeOutputs.TimeoutMinutes > 0
	}
	return false
}

func jobUsesUbuntuSlim(job *Job) (bool, error) {
	var runner map[string]any
	if err := yaml.Unmarshal([]byte(job.RunsOn), &runner); err != nil {
		return false, err
	}
	return usesUbuntuSlim(runner["runs-on"]), nil
}

func runnerConfigurationField(data *WorkflowData, jobName string) string {
	switch jobName {
	case string(constants.AgentJobName):
		return "runs-on"
	case string(constants.DetectionJobName):
		return "safe-outputs.threat-detection.runs-on"
	case string(constants.SafeOutputsJobName):
		if data != nil && data.SafeOutputs != nil && data.SafeOutputs.RunsOn != "" {
			return "safe-outputs.runs-on"
		}
		return "runs-on-slim"
	default:
		if isBuiltinJobName(jobName) {
			return "runs-on-slim"
		}
		return "jobs." + jobName + ".runs-on"
	}
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
