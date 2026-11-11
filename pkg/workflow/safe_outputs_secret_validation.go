package workflow

import (
	"fmt"
	"strings"
)

const safeOutputsDocsURL = "https://github.github.com/gh-aw/reference/safe-outputs/"

type safeOutputSecretRequirement struct {
	secretNames  []string
	name         string
	docsAnchor   string
	envOverrides map[string]string
}

func getSafeOutputSecretRequirements(data *WorkflowData) []safeOutputSecretRequirement {
	if data == nil || data.SafeOutputs == nil {
		return nil
	}
	config := data.SafeOutputs
	if strings.TrimSpace(data.Environment) != "" || strings.TrimSpace(config.Environment) != "" {
		return nil
	}

	var requirements []safeOutputSecretRequirement
	return requirements
}

func buildSafeOutputSecretValidationSteps(data *WorkflowData) []GitHubActionStep {
	requirements := getSafeOutputSecretRequirements(data)
	steps := make([]GitHubActionStep, 0, len(requirements))
	for i, requirement := range requirements {
		steps = append(steps, GenerateMultiSecretValidationStepWithID(
			requirement.secretNames,
			requirement.name,
			safeOutputsDocsURL+requirement.docsAnchor,
			requirement.envOverrides,
			safeOutputSecretValidationStepID(i),
		))
	}
	return steps
}

func safeOutputSecretValidationStepID(index int) string {
	return fmt.Sprintf("validate-safe-output-secret-%d", index+1)
}

func hasSafeOutputSecretValidationSteps(data *WorkflowData) bool {
	return len(getSafeOutputSecretRequirements(data)) > 0
}

func hasSecretValidationStep(engine CodingAgentEngine, data *WorkflowData) bool {
	return EngineHasValidateSecretStep(engine, data) || hasSafeOutputSecretValidationSteps(data)
}
