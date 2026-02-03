//go:build integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

func TestCopilotGitCommandsIntegrationWithCreatePullRequest(t *testing.T) {
	// Create a simple workflow with create-pull-request enabled
	workflowContent := `---
on: push
name: Test Git Commands Integration
tools:
  edit:
safe-outputs:
  create-pull-request:
    max: 1
---

This is a test workflow that should automatically get Git commands when create-pull-request is enabled.
`

	compiler := NewCompiler()

	// Parse the workflow content and get both result and allowed tools arguments for Copilot
	_, allowedToolArgs, err := compiler.parseCopilotWorkflowMarkdownContentWithToolArgs(workflowContent)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify that Git commands are present in the tool arguments
	expectedGitCommands := []string{"shell(git checkout:*)", "shell(git add:*)", "shell(git commit:*)", "shell(git branch:*)", "shell(git switch:*)", "shell(git rm:*)", "shell(git merge:*)"}

	for _, expectedCmd := range expectedGitCommands {
		found := false
		for i := 0; i < len(allowedToolArgs); i += 2 {
			if i+1 < len(allowedToolArgs) && allowedToolArgs[i] == "--allow-tool" && allowedToolArgs[i+1] == expectedCmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected allowed tool args to contain %s, got: %v", expectedCmd, allowedToolArgs)
		}
	}

	// Verify that write tool is also present (required for edit functionality)
	found := false
	for i := 0; i < len(allowedToolArgs); i += 2 {
		if i+1 < len(allowedToolArgs) && allowedToolArgs[i] == "--allow-tool" && allowedToolArgs[i+1] == "write" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected allowed tool args to contain write tool, got: %v", allowedToolArgs)
	}
}

func TestCopilotGitCommandsNotAddedWithoutPullRequestOutput(t *testing.T) {
	// Create a workflow with only create-issue (no PR-related outputs)
	workflowContent := `---
on: push
name: Test No Git Commands
tools:
  edit:
safe-outputs:
  create-issue:
    max: 1
---

This workflow should NOT get Git commands since it doesn't use create-pull-request or push-to-pull-request-branch.
`

	compiler := NewCompiler()

	// Parse the workflow content and get allowed tool arguments
	_, allowedToolArgs, err := compiler.parseCopilotWorkflowMarkdownContentWithToolArgs(workflowContent)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify allowed tool args do not include Git commands
	gitCommands := []string{"shell(git checkout:*)", "shell(git add:*)", "shell(git commit:*)", "shell(git branch:*)", "shell(git switch:*)", "shell(git rm:*)", "shell(git merge:*)"}
	for _, gitCmd := range gitCommands {
		for i := 0; i < len(allowedToolArgs); i += 2 {
			if i+1 < len(allowedToolArgs) && allowedToolArgs[i] == "--allow-tool" && allowedToolArgs[i+1] == gitCmd {
				t.Errorf("Did not expect allowed tool args to contain Git command %s, got: %v", gitCmd, allowedToolArgs)
			}
		}
	}

	// Verify write tool is still present (required for edit functionality)
	found := false
	for i := 0; i < len(allowedToolArgs); i += 2 {
		if i+1 < len(allowedToolArgs) && allowedToolArgs[i] == "--allow-tool" && allowedToolArgs[i+1] == "write" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected allowed tool args to contain write tool, got: %v", allowedToolArgs)
	}
}

// Helper function to parse workflow content and return both WorkflowData and allowed tool arguments for Copilot
func (c *Compiler) parseCopilotWorkflowMarkdownContentWithToolArgs(content string) (*WorkflowData, []string, error) {
	// Extract frontmatter
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return nil, nil, err
	}
	engine := NewCopilotEngine()

	// Extract SafeOutputs early
	safeOutputs := c.extractSafeOutputsConfig(result.Frontmatter)

	// Extract and process tools
	topTools := extractToolsFromFrontmatter(result.Frontmatter)
	topTools = c.applyDefaultTools(topTools, safeOutputs, nil, nil)

	// Build basic workflow data for testing
	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		Tools:       topTools,
		SafeOutputs: safeOutputs,
		AI:          "copilot",
	}
	allowedToolArgs := engine.computeCopilotToolArguments(topTools, safeOutputs, nil, workflowData)

	return workflowData, allowedToolArgs, nil
}
