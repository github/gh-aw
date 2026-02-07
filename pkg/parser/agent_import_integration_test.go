//go:build integration

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAgentImportWithToolsArray verifies that importing custom agent files
// with tools as an array (GitHub Copilot format) works correctly in the full import flow
func TestAgentImportWithToolsArray(t *testing.T) {
	tempDir := t.TempDir()

	// Create .github/agents directory
	agentsDir := filepath.Join(tempDir, ".github", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a custom agent file with tools as an array
	agentFile := filepath.Join(agentsDir, "feature-flag-remover.agent.md")
	agentContent := `---
description: "Removes feature flags from codebase"
tools:
  [
    "edit",
    "search",
    "execute/getTerminalOutput",
    "execute/runInTerminal",
    "read/terminalLastCommand",
    "read/terminalSelection",
    "execute/createAndRunTask",
    "execute/getTaskOutput",
    "execute/runTask",
    "read/problems",
    "search/changes",
    "agent",
    "runTasks",
    "problems",
    "changes",
    "runSubagent",
  ]
---

# Feature Flag Remover Agent

This agent removes feature flags from the codebase.`

	if err := os.WriteFile(agentFile, []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to write agent file: %v", err)
	}

	// Create a main workflow that imports the agent file
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	workflowContent := `---
on: issues
imports:
  - ../agents/feature-flag-remover.agent.md
---

# Test Workflow

This workflow imports a custom agent with array-format tools.`

	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Process imports from the workflow frontmatter
	frontmatter := map[string]any{
		"on": "issues",
		"imports": []string{
			"../agents/feature-flag-remover.agent.md",
		},
	}

	result, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, workflowsDir, nil)
	if err != nil {
		t.Fatalf("ProcessImportsFromFrontmatterWithManifest() error = %v, want nil", err)
	}

	// Verify that the agent file was detected and stored
	if result.AgentFile == "" {
		t.Errorf("Expected AgentFile to be set, got empty string")
	}

	expectedAgentPath := ".github/agents/feature-flag-remover.agent.md"
	if result.AgentFile != expectedAgentPath {
		t.Errorf("AgentFile = %q, want %q", result.AgentFile, expectedAgentPath)
	}

	// Verify that the import path was added for runtime-import macro (new behavior)
	// Agent imports without inputs should go into ImportPaths, not MergedMarkdown
	if len(result.ImportPaths) == 0 {
		t.Errorf("Expected ImportPaths to contain agent import path")
	}

	expectedImportPath := ".github/agents/feature-flag-remover.agent.md"
	found := false
	for _, path := range result.ImportPaths {
		if path == expectedImportPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ImportPaths = %v, want to contain %q", result.ImportPaths, expectedImportPath)
	}

	// MergedMarkdown should be empty for imports without inputs (runtime-import behavior)
	if result.MergedMarkdown != "" {
		t.Errorf("Expected MergedMarkdown to be empty for agent import without inputs, got: %q", result.MergedMarkdown)
	}

	// Verify that tools were NOT merged from the agent file (they're in array format)
	// Agent tools should be ignored during import processing
	if result.MergedTools != "" && result.MergedTools != "{}\n" {
		t.Errorf("MergedTools should be empty for agent files, got: %q", result.MergedTools)
	}
}

// TestMultipleAgentImportsError verifies that importing multiple agent files
// results in an error as only one agent file is allowed per workflow
func TestMultipleAgentImportsError(t *testing.T) {
	tempDir := t.TempDir()

	// Create .github/agents directory
	agentsDir := filepath.Join(tempDir, ".github", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create first agent file
	agent1File := filepath.Join(agentsDir, "agent1.md")
	if err := os.WriteFile(agent1File, []byte("---\ndescription: Agent 1\n---\n# Agent 1"), 0644); err != nil {
		t.Fatalf("Failed to write agent1 file: %v", err)
	}

	// Create second agent file
	agent2File := filepath.Join(agentsDir, "agent2.md")
	if err := os.WriteFile(agent2File, []byte("---\ndescription: Agent 2\n---\n# Agent 2"), 0644); err != nil {
		t.Fatalf("Failed to write agent2 file: %v", err)
	}

	// Process imports with multiple agent files - should error
	frontmatter := map[string]any{
		"on": "issues",
		"imports": []string{
			"../agents/agent1.md",
			"../agents/agent2.md",
		},
	}

	_, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, workflowsDir, nil)
	if err == nil {
		t.Errorf("Expected error when importing multiple agent files, got nil")
	}

	// Verify error message mentions multiple agent files
	if err != nil && err.Error() == "" {
		t.Errorf("Error message is empty")
	}
}
