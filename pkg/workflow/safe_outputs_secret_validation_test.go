//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSafeOutputSecretRequirements(t *testing.T) {
	for _, data := range []*WorkflowData{
		nil,
		{},
		{SafeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}},
		{SafeOutputs: &SafeOutputsConfig{Env: map[string]string{
			"JIRA_USER_EMAIL":      "${{ secrets.CUSTOM_JIRA_EMAIL }}",
			"JIRA_API_TOKEN":       "${{ secrets.CUSTOM_JIRA_TOKEN }}",
			"GH_AW_LINEAR_TOKEN":   "${{ secrets.CUSTOM_LINEAR_TOKEN }}",
			"AZURE_DEVOPS_EXT_PAT": "${{ secrets.CUSTOM_ADO_PAT }}",
		}}},
		{Environment: "production", SafeOutputs: &SafeOutputsConfig{}},
		{SafeOutputs: &SafeOutputsConfig{Environment: "production"}},
	} {
		assert.Empty(t, getSafeOutputSecretRequirements(data), "native provider secret checks must not be reintroduced")
		assert.Empty(t, buildSafeOutputSecretValidationSteps(data))
		assert.False(t, hasSafeOutputSecretValidationSteps(data))
	}
}

func TestBuildSafeOutputSecretValidationStepsUsesDistinctIDs(t *testing.T) {
	assert.Equal(t, "validate-safe-output-secret-1", safeOutputSecretValidationStepID(0))
	assert.Equal(t, "validate-safe-output-secret-2", safeOutputSecretValidationStepID(1))
}
