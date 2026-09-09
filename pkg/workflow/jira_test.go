package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraSafeOutputConfigParsing(t *testing.T) {
	compiler := &Compiler{}
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"jira-create-issue": map[string]any{"max": 2, "staged": true},
			"jira-update-issue": nil,
			"jira-add-comment":  map[string]any{"max": "${{ inputs.max }}"},
			"jira-add-label":    map[string]any{},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)
	require.NotNil(t, config)
	require.NotNil(t, config.JiraCreateIssue)
	require.NotNil(t, config.JiraUpdateIssue)
	require.NotNil(t, config.JiraAddComment)
	require.NotNil(t, config.JiraAddLabel)
	assert.Equal(t, "2", *config.JiraCreateIssue.Max)
	assert.True(t, templatableBoolIsTrue(config.JiraCreateIssue.Staged))
	assert.Equal(t, "1", *config.JiraUpdateIssue.Max)
	assert.Equal(t, "${{ inputs.max }}", *config.JiraAddComment.Max)
	assert.Equal(t, "1", *config.JiraAddLabel.Max)
}

func TestJiraHandlerConfigContainsOnlyCommonControls(t *testing.T) {
	staged := TemplatableBool("true")
	config := &JiraSafeOutputConfig{
		BaseSafeOutputConfig: BaseSafeOutputConfig{
			Max:    defaultIntStr(3),
			Staged: &staged,
		},
	}

	assert.Equal(t, map[string]any{"max": 3, "staged": true}, buildJiraHandlerConfig(config))
	assert.Nil(t, buildJiraHandlerConfig(nil))
}

func TestJiraSafeOutputsRequireNoGitHubWritePermissions(t *testing.T) {
	config := &SafeOutputsConfig{
		JiraCreateIssue: &JiraSafeOutputConfig{},
		JiraUpdateIssue: &JiraSafeOutputConfig{},
		JiraAddComment:  &JiraSafeOutputConfig{},
		JiraAddLabel:    &JiraSafeOutputConfig{},
	}

	permissions := ComputePermissionsForSafeOutputs(config)
	require.NotNil(t, permissions)
	assert.Equal(t, "permissions: {}", permissions.RenderToYAML())
}

func TestJiraSafeOutputsCountAsNonBuiltin(t *testing.T) {
	assert.True(t, hasAnySafeOutputEnabled(&SafeOutputsConfig{JiraAddComment: &JiraSafeOutputConfig{}}))
	assert.True(t, hasNonBuiltinSafeOutputsEnabled(&SafeOutputsConfig{JiraAddComment: &JiraSafeOutputConfig{}}))
}

func TestJiraCredentialsAreAddedOnlyToProcessorStep(t *testing.T) {
	config := &SafeOutputsConfig{
		JiraCreateIssue: &JiraSafeOutputConfig{},
		Env: map[string]string{
			"JIRA_BASE_URL": "https://example.atlassian.net",
			"OTHER":         "value",
		},
	}

	steps := make([]string, 3, 8)
	copy(steps, []string{
		"      - name: Process Safe Outputs\n",
		"        env:\n",
		"        with:\n",
	})

	injectedSteps := injectJiraCredentialsIntoProcessorStep(steps, config)
	rendered := strings.Join(injectedSteps, "")
	assert.Contains(t, rendered, "JIRA_BASE_URL: https://example.atlassian.net")
	assert.Contains(t, rendered, "JIRA_USER_EMAIL: ${{ secrets.JIRA_USER_EMAIL }}")
	assert.Contains(t, rendered, "JIRA_API_TOKEN: ${{ secrets.JIRA_API_TOKEN }}")
	assert.Equal(t, "        with:\n", injectedSteps[len(injectedSteps)-1])

	customSteps := []string{}
	NewCompiler().addCustomSafeOutputEnvVars(&customSteps, &WorkflowData{SafeOutputs: config})
	assert.Equal(t, "          OTHER: value\n", strings.Join(customSteps, ""))
}

func TestJiraCredentialsUseDefaultBaseURL(t *testing.T) {
	config := &SafeOutputsConfig{JiraCreateIssue: &JiraSafeOutputConfig{}}
	steps := []string{
		"      - name: Process Safe Outputs\n",
		"        env:\n",
		"        with:\n",
	}

	rendered := strings.Join(injectJiraCredentialsIntoProcessorStep(steps, config), "")
	assert.Contains(t, rendered, "JIRA_BASE_URL: "+constants.JiraBaseURLExpr)
}
