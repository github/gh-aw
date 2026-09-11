package workflow

import (
	"fmt"
	"strings"
)

const safeOutputsDocsURL = "https://github.github.com/gh-aw/reference/safe-outputs/"

type safeOutputSecretRequirement struct {
	secretNames []string
	name        string
	docsAnchor  string
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
	if hasAnyJiraSafeOutputEnabled(config) {
		for _, secretName := range []string{"JIRA_USER_EMAIL", "JIRA_API_TOKEN"} {
			if strings.TrimSpace(config.Env[secretName]) == "" {
				requirements = append(requirements, safeOutputSecretRequirement{
					secretNames: []string{secretName},
					name:        "Jira safe outputs",
					docsAnchor:  "#jira-safe-outputs",
				})
			}
		}
	}
	if hasLinearSafeOutputs(config) &&
		strings.TrimSpace(config.LinearToken) == "" &&
		strings.TrimSpace(config.Env["GH_AW_LINEAR_TOKEN"]) == "" {
		requirements = append(requirements, safeOutputSecretRequirement{
			secretNames: []string{"LINEAR_API_KEY"},
			name:        "Linear safe outputs",
			docsAnchor:  "#linear-safe-outputs",
		})
	}
	if hasAzureDevOpsSafeOutputs(config) &&
		strings.TrimSpace(config.Env["SYSTEM_ACCESSTOKEN"]) == "" &&
		strings.TrimSpace(config.Env["AZURE_DEVOPS_EXT_PAT"]) == "" {
		requirements = append(requirements, safeOutputSecretRequirement{
			secretNames: []string{"AZURE_DEVOPS_EXT_PAT"},
			name:        "Azure DevOps safe outputs",
			docsAnchor:  "#azure-devops-work-items",
		})
	}
	return requirements
}

func hasAzureDevOpsSafeOutputs(config *SafeOutputsConfig) bool {
	return config != nil &&
		(config.CreateWorkItems != nil ||
			config.UpdateWorkItems != nil ||
			config.CommentOnWorkItems != nil ||
			config.AssignWorkItems != nil ||
			config.LinkWorkItems != nil ||
			config.UploadWorkItemAttachments != nil)
}

func buildSafeOutputSecretValidationSteps(data *WorkflowData) []GitHubActionStep {
	requirements := getSafeOutputSecretRequirements(data)
	steps := make([]GitHubActionStep, 0, len(requirements))
	for i, requirement := range requirements {
		steps = append(steps, GenerateMultiSecretValidationStepWithID(
			requirement.secretNames,
			requirement.name,
			safeOutputsDocsURL+requirement.docsAnchor,
			nil,
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
