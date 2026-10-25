// Custom-job property extraction maps frontmatter fields onto Job values.
package workflow

import (
	"fmt"
	"math"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) extractCustomJobProperties(job *Job, jobName string, configMap map[string]any) error {
	if err := c.extractCustomJobCoreProperties(job, jobName, configMap); err != nil {
		return err
	}
	extractCustomJobOutputs(job, jobName, configMap)
	return nil
}

func (c *Compiler) extractCustomJobCoreProperties(job *Job, jobName string, configMap map[string]any) error {
	if _, hasInputs := configMap["inputs"]; hasInputs {
		return fmt.Errorf("jobs.%s.inputs: inputs are not supported on jobs; use 'env' to pass values to job steps", jobName)
	}

	if err := c.extractCustomJobRunsOn(job, jobName, configMap); err != nil {
		return err
	}

	if ifCond, hasIf := configMap["if"]; hasIf {
		if ifStr, ok := ifCond.(string); ok {
			job.If = c.extractExpressionFromIfString(ifStr)
		}
	}

	if permissions, hasPermissions := configMap["permissions"]; hasPermissions {
		formattedPerms := NewPermissionsParserFromValue(permissions).ToPermissions().RenderToYAML()
		if formattedPerms != "" {
			job.Permissions = formattedPerms
		}
	}

	if strategy, hasStrategy := configMap["strategy"]; hasStrategy {
		if strategyMap, ok := strategy.(map[string]any); ok {
			formattedStrategy, err := formatIndentedYAMLField("strategy", strategyMap, false)
			if err != nil {
				return fmt.Errorf("strategy field for job '%s' could not be converted to YAML: %w. Check that strategy is a valid object, for example: strategy:\n  matrix:\n    os: [ubuntu-latest]", jobName, err)
			}
			job.Strategy = formattedStrategy
		}
	}

	// Extract name (display name) for custom jobs
	if name, hasName := configMap["name"]; hasName {
		if nameStr, ok := name.(string); ok {
			job.DisplayName = nameStr
		}
	}

	if err := extractCustomJobTimeoutMinutes(job, jobName, configMap); err != nil {
		return err
	}

	if err := extractCustomJobConcurrency(job, jobName, configMap); err != nil {
		return err
	}

	return extractCustomJobExecutionProperties(job, jobName, configMap)
}

func extractCustomJobExecutionProperties(job *Job, jobName string, configMap map[string]any) error {
	extractCustomJobEnv(job, configMap)

	if err := extractCustomJobContainer(job, jobName, configMap); err != nil {
		return err
	}
	if err := extractCustomJobServices(job, jobName, configMap); err != nil {
		return err
	}
	extractCustomJobContinueOnError(job, configMap)

	if err := extractCustomJobEnvironment(job, jobName, configMap); err != nil {
		return err
	}

	return nil
}

func (c *Compiler) extractCustomJobRunsOn(job *Job, jobName string, configMap map[string]any) error {
	runsOn, hasRunsOn := configMap["runs-on"]
	if !hasRunsOn {
		return nil
	}
	if err := validateRunsOnValue(runsOn); err != nil {
		return fmt.Errorf("runs-on field for job '%s' is invalid: %w", jobName, err)
	}
	if runsOnStr, ok := runsOn.(string); ok {
		job.RunsOn = "runs-on: " + runsOnStr
		return nil
	}

	// Array or object form: marshal the value and build indented YAML snippet
	formattedRunsOn, err := formatIndentedYAMLField("runs-on", runsOn, true)
	if err != nil {
		return fmt.Errorf("runs-on field for job '%s' could not be converted to YAML: %w. Check that runs-on is a valid string, array, or object, for example: runs-on: ubuntu-latest", jobName, err)
	}
	job.RunsOn = formattedRunsOn
	return nil
}

func extractCustomJobTimeoutMinutes(job *Job, jobName string, configMap map[string]any) error {
	timeout, hasTimeout := configMap["timeout-minutes"]
	if !hasTimeout {
		return nil
	}

	if v, ok := timeout.(string); ok {
		return extractCustomJobTimeoutExpression(job, jobName, v)
	}
	return extractCustomJobNumericTimeout(job, jobName, timeout)
}

func extractCustomJobNumericTimeout(job *Job, jobName string, timeout any) error {
	const maxLosslessIntFloat64 = float64(1 << 53)
	timeoutMustBePositiveIntegerErr := func(got any) error {
		return fmt.Errorf("job '%s' timeout-minutes must be a positive integer, got %v", jobName, got)
	}

	switch v := timeout.(type) {
	case int:
		if v <= 0 {
			return timeoutMustBePositiveIntegerErr(v)
		}
		job.TimeoutMinutes = v
		job.TimeoutMinutesExpression = ""
	case int64:
		if v <= 0 || int64(int(v)) != v {
			return timeoutMustBePositiveIntegerErr(v)
		}
		job.TimeoutMinutes = int(v)
		job.TimeoutMinutesExpression = ""
	case uint64:
		if v == 0 || uint64(int(v)) != v {
			return timeoutMustBePositiveIntegerErr(v)
		}
		job.TimeoutMinutes = int(v)
		job.TimeoutMinutesExpression = ""
	case float64:
		if v <= 0 || math.Trunc(v) != v || v > maxLosslessIntFloat64 {
			return timeoutMustBePositiveIntegerErr(v)
		}
		minutes64 := int64(v)
		if minutes64 <= 0 || int64(int(minutes64)) != minutes64 {
			return timeoutMustBePositiveIntegerErr(v)
		}
		job.TimeoutMinutes = int(minutes64)
		job.TimeoutMinutesExpression = ""
	default:
		return timeoutMustBePositiveIntegerErr(v)
	}

	return nil
}

func extractCustomJobTimeoutExpression(job *Job, jobName string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("job '%s' timeout-minutes must be a positive integer, got %v", jobName, value)
	}
	if jobName == string(constants.AgentJobName) || jobName == string(constants.DetectionJobName) {
		return fmt.Errorf("job '%s' timeout-minutes must be a positive integer; expressions are not supported", jobName)
	}
	if isExpression(value) {
		job.TimeoutMinutes = 0
		job.TimeoutMinutesExpression = value
		return nil
	}
	return fmt.Errorf(
		"job '%s' timeout-minutes must be an integer or a GitHub Actions expression, got %q. Example: timeout-minutes: 30 or ${{ inputs.timeout }}",
		jobName,
		value,
	)
}

func extractCustomJobConcurrency(job *Job, jobName string, configMap map[string]any) error {
	concurrency, hasConcurrency := configMap["concurrency"]
	if !hasConcurrency {
		return nil
	}

	switch v := concurrency.(type) {
	case string:
		job.Concurrency = "concurrency: " + v
	case map[string]any:
		// Default cancel-in-progress to false for non-agent jobs if not explicitly set.
		// This prevents accidental cancellation of queued runs when multiple agents
		// are running the same workflow concurrently.
		if _, hasCancelInProgress := v["cancel-in-progress"]; !hasCancelInProgress {
			v["cancel-in-progress"] = false
		}

		formattedConcurrency, err := formatIndentedYAMLField("concurrency", v, false)
		if err != nil {
			return fmt.Errorf("concurrency field for job '%s' could not be converted to YAML: %w. Check that concurrency is a valid object, for example: concurrency:\n  group: my-group", jobName, err)
		}
		job.Concurrency = formattedConcurrency
	}

	return nil
}

func extractCustomJobEnv(job *Job, configMap map[string]any) {
	env, hasEnv := configMap["env"]
	if !hasEnv {
		return
	}
	envMap, ok := env.(map[string]any)
	if !ok {
		return
	}

	job.Env = make(map[string]string)
	for key, val := range envMap {
		if valStr, ok := val.(string); ok {
			job.Env[key] = valStr
		} else if val != nil {
			// Arrays and maps are serialized as JSON so that shell consumers
			// (e.g. jq --argjson) receive valid JSON.
			job.Env[key] = marshalEnvValue(val)
		}
	}
}

func extractCustomJobContainer(job *Job, jobName string, configMap map[string]any) error {
	container, hasContainer := configMap["container"]
	if !hasContainer {
		return nil
	}

	switch v := container.(type) {
	case string:
		job.Container = "container: " + v
	case map[string]any:
		formattedContainer, err := formatIndentedYAMLField("container", v, false)
		if err != nil {
			return fmt.Errorf("container field for job '%s' could not be converted to YAML: %w. Check that container is a valid object, for example: container:\n  image: node:20", jobName, err)
		}
		job.Container = formattedContainer
	}

	return nil
}

func extractCustomJobServices(job *Job, jobName string, configMap map[string]any) error {
	services, hasServices := configMap["services"]
	if !hasServices {
		return nil
	}
	servicesMap, ok := services.(map[string]any)
	if !ok {
		return nil
	}

	formattedServices, err := formatIndentedYAMLField("services", servicesMap, false)
	if err != nil {
		return fmt.Errorf("services field for job '%s' could not be converted to YAML: %w. Check that services is a valid object, for example: services:\n  redis:\n    image: redis", jobName, err)
	}
	job.Services = formattedServices
	return nil
}

func extractCustomJobContinueOnError(job *Job, configMap map[string]any) {
	continueOnError, hasCOE := configMap["continue-on-error"]
	if !hasCOE {
		return
	}
	if coeVal, ok := continueOnError.(bool); ok {
		job.ContinueOnError = &coeVal
	}
}

func extractCustomJobEnvironment(job *Job, jobName string, configMap map[string]any) error {
	environment, hasEnvironment := configMap["environment"]
	if !hasEnvironment {
		return nil
	}

	switch v := environment.(type) {
	case string:
		job.Environment = "environment: " + v
	case map[string]any:
		formattedEnvironment, err := formatIndentedYAMLField("environment", v, true)
		if err != nil {
			return fmt.Errorf("environment field for job '%s' could not be converted to YAML: %w. Check that environment is a valid object, for example: environment:\n  name: production", jobName, err)
		}
		job.Environment = formattedEnvironment
	}

	return nil
}

func extractCustomJobOutputs(job *Job, jobName string, configMap map[string]any) {
	outputs, hasOutputs := configMap["outputs"]
	if !hasOutputs {
		return
	}
	outputsMap, ok := outputs.(map[string]any)
	if !ok {
		return
	}

	job.Outputs = make(map[string]string)
	for key, val := range outputsMap {
		if valStr, ok := val.(string); ok {
			job.Outputs[key] = valStr
		} else {
			compilerJobsLog.Printf("Warning: output '%s' in job '%s' has non-string value (type: %T), ignoring", key, jobName, val)
		}
	}
}
