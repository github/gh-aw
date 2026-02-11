//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSafeOutputsPrompt_IncludesProjectsCategory(t *testing.T) {
	// Test that projects category is included when update-project is enabled
	config := &SafeOutputsConfig{
		UpdateProjects: &UpdateProjectConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "<safe-outputs>", "Prompt should have safe-outputs tag")
	assert.Contains(t, prompt, "Available tool categories:", "Prompt should list available categories")
	assert.Contains(t, prompt, "**Projects**: Manage GitHub Projects v2 boards", "Prompt should mention Projects category")
	assert.Contains(t, prompt, "add/update items", "Prompt should mention adding/updating project items")
	assert.Contains(t, prompt, "post status updates", "Prompt should mention project status updates")
}

func TestBuildSafeOutputsPrompt_IncludesProjectsCategoryWithCreate(t *testing.T) {
	// Test that projects category is included when create-project is enabled
	config := &SafeOutputsConfig{
		CreateProjects: &CreateProjectsConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "**Projects**: Manage GitHub Projects v2 boards", "Prompt should mention Projects category")
	assert.Contains(t, prompt, "create projects", "Prompt should mention creating projects")
}

func TestBuildSafeOutputsPrompt_IncludesProjectsCategoryWithStatusUpdates(t *testing.T) {
	// Test that projects category is included when create-project-status-update is enabled
	config := &SafeOutputsConfig{
		CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "**Projects**: Manage GitHub Projects v2 boards", "Prompt should mention Projects category")
	assert.Contains(t, prompt, "post status updates", "Prompt should mention project status updates")
}

func TestBuildSafeOutputsPrompt_ExcludesProjectsWhenNotEnabled(t *testing.T) {
	// Test that projects category is not included when no project safe outputs are enabled
	config := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
		AddComments:  &AddCommentsConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.NotContains(t, prompt, "**Projects**", "Prompt should NOT mention Projects category when not enabled")
	assert.Contains(t, prompt, "**Issues**", "Prompt should mention Issues category")
	assert.Contains(t, prompt, "**Comments**", "Prompt should mention Comments category")
}

func TestBuildSafeOutputsPrompt_AlwaysIncludesUtility(t *testing.T) {
	// Test that utility category is always included
	config := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "**Utility**: Report missing data, missing tools, or no-op completion status", "Prompt should always include Utility category")
}

func TestBuildSafeOutputsPrompt_IncludesMultipleCategories(t *testing.T) {
	// Test that multiple categories are included when multiple safe outputs are enabled
	config := &SafeOutputsConfig{
		CreateIssues:               &CreateIssuesConfig{},
		CreatePullRequests:         &CreatePullRequestsConfig{},
		AddComments:                &AddCommentsConfig{},
		UpdateProjects:             &UpdateProjectConfig{},
		CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{},
		AddLabels:                  &AddLabelsConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "**Issues**", "Prompt should mention Issues category")
	assert.Contains(t, prompt, "**Pull Requests**", "Prompt should mention Pull Requests category")
	assert.Contains(t, prompt, "**Comments**", "Prompt should mention Comments category")
	assert.Contains(t, prompt, "**Projects**", "Prompt should mention Projects category")
	assert.Contains(t, prompt, "**Labels**", "Prompt should mention Labels category")
	assert.Contains(t, prompt, "**Utility**", "Prompt should mention Utility category")
}

func TestBuildSafeOutputsPrompt_IncludesInstructions(t *testing.T) {
	// Test that key instructions are always included
	config := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.Contains(t, prompt, "gh CLI is NOT authenticated", "Prompt should warn about gh CLI")
	assert.Contains(t, prompt, "you MUST call the appropriate safe output tool", "Prompt should emphasize tool calls")
	assert.Contains(t, prompt, "Discover available tools from the safeoutputs MCP server", "Prompt should mention MCP server")
	assert.Contains(t, prompt, "Tool calls write structured data", "Prompt should explain importance of tool calls")
	assert.Contains(t, prompt, "noop", "Prompt should mention noop tool")
}

func TestBuildSafeOutputsPrompt_NilConfigReturnsEmpty(t *testing.T) {
	// Test that nil config returns empty string
	prompt := buildSafeOutputsPrompt(nil)
	assert.Empty(t, prompt, "Nil config should return empty prompt")
}

func TestBuildSafeOutputsPrompt_FormattedAsXML(t *testing.T) {
	// Test that prompt is properly formatted with XML-like tags
	config := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
	}

	prompt := buildSafeOutputsPrompt(config)

	assert.True(t, strings.HasPrefix(prompt, "<safe-outputs>"), "Prompt should start with <safe-outputs>")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(prompt), "</safe-outputs>"), "Prompt should end with </safe-outputs>")
	assert.Contains(t, prompt, "<description>", "Prompt should have description tag")
	assert.Contains(t, prompt, "<important>", "Prompt should have important tag")
	assert.Contains(t, prompt, "<instructions>", "Prompt should have instructions tag")
}
