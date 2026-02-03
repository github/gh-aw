//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestThreatDetectionIsolation(t *testing.T) {
	compiler := NewCompiler()

	// Create a temporary directory for the test workflow
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-isolation.md")

	workflowContent := `---
on: push
safe-outputs:
  create-issue:
tools:
  github:
    allowed: ["*"]
---
Test workflow`

	// Write the workflow file
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	yamlStr := string(result)

	// Test 1: Detection job should have NO --allow-tool arguments
	// Extract the detection job section
	detectionJobStart := strings.Index(yamlStr, "detection:")
	if detectionJobStart == -1 {
		t.Fatal("Detection job not found in compiled workflow")
	}

	// Find the next job to get the detection job boundary
	nextJobStart := strings.Index(yamlStr[detectionJobStart+10:], "\n  ")
	var detectionSection string
	if nextJobStart == -1 {
		detectionSection = yamlStr[detectionJobStart:]
	} else {
		detectionSection = yamlStr[detectionJobStart : detectionJobStart+10+nextJobStart]
	}

	// Check that detection job has NO --allow-tool arguments
	if strings.Contains(detectionSection, "--allow-tool") {
		t.Error("Detection job should NOT have any --allow-tool arguments (all tools should be denied)")
	}

	// Test 2: Detection job should NOT have MCP setup
	if strings.Contains(detectionSection, "Start MCP gateway") {
		t.Error("Detection job should NOT have MCP setup step")
	}

	// Test 3: Main agent job should have --allow-tool arguments (for comparison)
	agentJobStart := strings.Index(yamlStr, "agent:")
	if agentJobStart == -1 {
		t.Fatal("Agent job not found in compiled workflow")
	}

	agentSection := yamlStr[agentJobStart:detectionJobStart]
	// Accept either --allow-tool or --allow-all-tools
	if !strings.Contains(agentSection, "--allow-tool") && !strings.Contains(agentSection, "--allow-all-tools") {
		t.Error("Main agent job should have --allow-tool or --allow-all-tools arguments")
	}

	// Test 4: Main agent job should have MCP setup (for comparison)
	if !strings.Contains(agentSection, "Start MCP gateway") {
		t.Error("Main agent job should have MCP setup step")
	}
}
