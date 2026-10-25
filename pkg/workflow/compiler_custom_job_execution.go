// Custom-job execution configuration handles reusable workflows and step rendering.
package workflow

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

func (c *Compiler) configureCustomJobExecution(job *Job, jobName string, configMap map[string]any, data *WorkflowData) error {
	uses, hasUses := configMap["uses"]
	if hasUses {
		if usesStr, ok := uses.(string); ok {
			return configureCustomReusableWorkflow(job, jobName, usesStr, configMap)
		}
	}

	return c.configureCustomJobSteps(job, jobName, configMap, data)
}

func configureCustomReusableWorkflow(job *Job, jobName string, usesStr string, configMap map[string]any) error {
	compilerJobsLog.Printf("Custom job '%s' is a reusable workflow call: %s", jobName, usesStr)

	// restore-memory cannot inject steps into reusable-workflow call jobs (no steps block).
	if rm, ok := configMap["restore-memory"]; ok && rm != nil && rm != false {
		return fmt.Errorf("jobs.%s.restore-memory: not supported for reusable workflow call jobs (uses: %s)", jobName, usesStr)
	}

	job.Uses = usesStr

	// Extract with parameters for reusable workflow
	if with, hasWith := configMap["with"]; hasWith {
		if withMap, ok := with.(map[string]any); ok {
			job.With = withMap
		}
	}

	// Extract secrets for reusable workflow
	if secrets, hasSecrets := configMap["secrets"]; hasSecrets {
		switch sv := secrets.(type) {
		case string:
			if sv == "inherit" {
				job.SecretsInherit = true
			}
		case map[string]any:
			job.Secrets = make(map[string]string)
			for key, val := range sv {
				if valStr, ok := val.(string); ok {
					// Validate that the secret value is a proper GitHub Actions expression
					// Note: We don't pass the key to validateSecretsExpression to prevent
					// CodeQL from detecting sensitive data flow to error messages/logs
					if err := validateSecretsExpression(valStr); err != nil {
						return err
					}
					job.Secrets[key] = valStr
				}
			}
		}
	}

	return nil
}

func (c *Compiler) configureCustomJobSteps(job *Job, jobName string, configMap map[string]any, data *WorkflowData) error {
	if job.RunsOn == "" {
		job.RunsOn = c.indentYAMLLines(data.RunsOn, "    ")
		if job.RunsOn == "" {
			job.RunsOn = "runs-on: ubuntu-latest"
		}
	}

	// Add basic steps if specified (only for non-reusable workflow jobs).
	// `setup-steps` and `pre-steps` stay distinct so setup-steps can remain the
	// first injected steps in the job, followed by compiler scaffolding,
	// `pre-steps`, and the regular `steps` list.
	_, hasSetupStepsField := configMap["setup-steps"]
	_, hasPreStepsField := configMap["pre-steps"]
	_, hasStepsField := configMap["steps"]

	setupSteps, preSteps, regularSteps, err := c.extractCustomJobStepGroups(jobName, configMap, data)
	if err != nil {
		return err
	}

	// Parse restore-memory configuration.
	// restore-memory injects read-only memory restore steps into the custom job.
	// No write-back or commit steps are ever emitted for memory in custom jobs.
	restoreMemCfg, err := extractRestoreMemoryConfig(configMap, jobName, data)
	if err != nil {
		return err
	}

	hasRestoreMemory := restoreMemCfg != nil

	// When cache-memory restore is requested, inject GH_AW_WORKFLOW_ID_SANITIZED so that
	// restore keys match those used by the agent job.  Only set it when the user has not
	// already provided the variable in their job's env: block.
	configureCustomJobMemoryEnv(job, restoreMemCfg, data)

	if hasSetupStepsField || hasPreStepsField || hasStepsField || hasRestoreMemory {
		return c.appendCustomJobSteps(job, jobName, data, restoreMemCfg, setupSteps, preSteps, regularSteps)
	}

	return nil
}

func (c *Compiler) extractCustomJobStepGroups(jobName string, configMap map[string]any, data *WorkflowData) ([]string, []string, []string, error) {
	setupSteps, err := c.extractCustomJobStepField("setup-steps", jobName, configMap, data)
	if err != nil {
		return nil, nil, nil, err
	}
	preSteps, err := c.extractCustomJobStepField("pre-steps", jobName, configMap, data)
	if err != nil {
		return nil, nil, nil, err
	}
	regularSteps, err := c.extractCustomJobStepField("steps", jobName, configMap, data)
	if err != nil {
		return nil, nil, nil, err
	}
	return setupSteps, preSteps, regularSteps, nil
}

func (c *Compiler) extractCustomJobStepField(field, jobName string, configMap map[string]any, data *WorkflowData) ([]string, error) {
	if _, exists := configMap[field]; !exists {
		return nil, nil
	}
	steps, err := c.extractPinnedJobSteps(field, jobName, configMap, data)
	if err != nil {
		return nil, fmt.Errorf("%s for job '%s' could not be processed: %w. Check that %s is an array of valid step objects", field, jobName, err, field)
	}
	return steps, nil
}

func configureCustomJobMemoryEnv(job *Job, restoreMemCfg *restoreMemoryConfig, data *WorkflowData) {
	if restoreMemCfg != nil && restoreMemCfg.CacheMemory && data.WorkflowID != "" {
		sanitized := SanitizeWorkflowIDForCacheKey(data.WorkflowID)
		if job.Env == nil {
			job.Env = make(map[string]string)
		}
		if _, alreadySet := job.Env["GH_AW_WORKFLOW_ID_SANITIZED"]; !alreadySet {
			job.Env["GH_AW_WORKFLOW_ID_SANITIZED"] = sanitized
		}
	}

}

func (c *Compiler) appendCustomJobSteps(job *Job, jobName string, data *WorkflowData, restoreMemCfg *restoreMemoryConfig, setupSteps, preSteps, regularSteps []string) error {
	job.Steps = append(job.Steps, setupSteps...)
	// Prepend GH_HOST configuration step for GHES/GHEC compatibility.
	// Custom frontmatter jobs run as independent GitHub Actions jobs that
	// don't inherit GITHUB_ENV from the agent job, so the gh CLI won't
	// know which host to target without this step.
	job.Steps = append(job.Steps, generateGHESHostConfigurationStep())

	// Inject gh-aw setup + memory restore steps when restore-memory is requested.
	// Setup lines come first (they install scripts needed by repo/comment memory).
	// Memory lines follow immediately after (restore/clone/prepare steps).
	if restoreMemCfg != nil {
		memorySetupLines, memoryRestoreLines, memErr := c.buildRestoreMemorySteps(restoreMemCfg, jobName, data)
		if memErr != nil {
			return memErr
		}
		job.Steps = append(job.Steps, memorySetupLines...)
		job.Steps = append(job.Steps, memoryRestoreLines...)
	}

	job.Steps = append(job.Steps, preSteps...)
	job.Steps = append(job.Steps, regularSteps...)
	return nil
}

func formatIndentedYAMLField(fieldName string, value any, trimTrailingNewline bool) (string, error) {
	// IndentSequence ensures list items (e.g. strategy.matrix arrays, runs-on
	// lists, services ports) are indented under their parent key rather than
	// aligned with it, satisfying yamllint's default indentation rule.
	marshalOptions := append(append([]yaml.EncodeOption{}, DefaultMarshalOptions...), yaml.IndentSequence(true))
	yamlBytes, err := yaml.MarshalWithOptions(map[string]any{fieldName: value}, marshalOptions...)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSuffix(string(yamlBytes), "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("    ")
		}
		b.WriteString(line + "\n")
	}

	formatted := b.String()
	if trimTrailingNewline {
		return strings.TrimSuffix(formatted, "\n"), nil
	}
	return formatted, nil
}
