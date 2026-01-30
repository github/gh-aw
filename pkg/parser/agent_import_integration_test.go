//go:build integration

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAgentImportWithToolsArray verifies that importing files from .github/agents/
// are now treated as regular imports (not special agent files).
// Agent identification is now done via engine.agent field instead.
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

	// Verify that AgentFile is NOT set (deprecated - agent files no longer automatically detected)
	if result.AgentFile != "" {
		t.Errorf("Expected AgentFile to be empty (deprecated), got %q", result.AgentFile)
	}

	// Verify that markdown was extracted from the imported file (treated as regular import now)
	if result.MergedMarkdown == "" {
		t.Errorf("Expected MergedMarkdown to contain markdown content from import")
	}
}

// TestMultipleAgentImportsNoError verifies that importing multiple files from .github/agents/
// no longer causes an error (they are treated as regular imports now).
// Use engine.agent field to specify the agent ID instead.
func TestMultipleAgentImportsNoError(t *testing.T) {
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

	// Process imports with multiple agent files - should NOT error (treated as regular imports)
	frontmatter := map[string]any{
		"on": "issues",
		"imports": []string{
			"../agents/agent1.md",
			"../agents/agent2.md",
		},
	}

	result, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, workflowsDir, nil)
	if err != nil {
		t.Errorf("Expected no error when importing multiple files from .github/agents/, got: %v", err)
	}

	// Verify that AgentFile is NOT set (deprecated)
	if result.AgentFile != "" {
		t.Errorf("Expected AgentFile to be empty (deprecated), got %q", result.AgentFile)
	}

	// Verify that markdown was extracted from both imported files
	if result.MergedMarkdown == "" {
		t.Errorf("Expected MergedMarkdown to contain markdown content from imports")
	}
}
