//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSafeOutputSecretRequirements(t *testing.T) {
	t.Run("collects required secrets for enabled external safe outputs", func(t *testing.T) {
		requirements := getSafeOutputSecretRequirements(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				JiraCreateIssue:   &JiraSafeOutputConfig{},
				LinearCreateIssue: &LinearCreateIssueConfig{},
				CreateWorkItems:   &CreateWorkItemConfig{},
			},
		})

		assert.Equal(t, []safeOutputSecretRequirement{
			{secretNames: []string{"JIRA_USER_EMAIL"}, name: "Jira safe outputs", docsAnchor: "#jira-safe-outputs"},
			{secretNames: []string{"JIRA_API_TOKEN"}, name: "Jira safe outputs", docsAnchor: "#jira-safe-outputs"},
			{secretNames: []string{"LINEAR_API_KEY"}, name: "Linear safe outputs", docsAnchor: "#linear-safe-outputs"},
			{secretNames: []string{"AZURE_DEVOPS_EXT_PAT"}, name: "Azure DevOps safe outputs", docsAnchor: "#azure-devops-work-items"},
		}, requirements)
	})

	t.Run("omits checks when credentials are provided in safe outputs frontmatter", func(t *testing.T) {
		requirements := getSafeOutputSecretRequirements(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				JiraAddComment:   &JiraSafeOutputConfig{},
				LinearAddComment: &LinearTargetConfig{},
				UpdateWorkItems:  &UpdateWorkItemConfig{},
				LinearToken:      "${{ secrets.CUSTOM_LINEAR_TOKEN }}",
				Env: map[string]string{
					"JIRA_USER_EMAIL":      "${{ secrets.CUSTOM_JIRA_EMAIL }}",
					"JIRA_API_TOKEN":       "${{ secrets.CUSTOM_JIRA_TOKEN }}",
					"AZURE_DEVOPS_EXT_PAT": "${{ secrets.CUSTOM_ADO_PAT }}",
				},
			},
		})

		assert.Empty(t, requirements)
	})

	t.Run("omits checks when safe outputs use an environment", func(t *testing.T) {
		requirements := getSafeOutputSecretRequirements(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				JiraCreateIssue: &JiraSafeOutputConfig{},
				Environment:     "production",
			},
		})

		assert.Empty(t, requirements)
	})

	t.Run("accepts Azure Pipelines system access token override", func(t *testing.T) {
		requirements := getSafeOutputSecretRequirements(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				CommentOnWorkItems: &CommentOnWorkItemConfig{},
				Env: map[string]string{
					"SYSTEM_ACCESSTOKEN": "${{ secrets.CUSTOM_SYSTEM_TOKEN }}",
				},
			},
		})

		assert.Empty(t, requirements)
	})
}

func TestBuildSafeOutputSecretValidationStepsUsesDistinctIDs(t *testing.T) {
	steps := buildSafeOutputSecretValidationSteps(&WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			JiraCreateIssue: &JiraSafeOutputConfig{},
		},
	})

	assert.Len(t, steps, 2)
	assert.Contains(t, steps[0], "        id: validate-safe-output-secret-1")
	assert.Contains(t, steps[1], "        id: validate-safe-output-secret-2")
}
