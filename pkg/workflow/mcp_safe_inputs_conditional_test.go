//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestMCPGatewaySafeInputsEnvVarsConditional tests that Safe Inputs environment variables
// are only included in the MCP gateway step when Safe Inputs is actually configured
func TestMCPGatewaySafeInputsEnvVarsConditional(t *testing.T) {
	tests := []struct {
		name                    string
		workflowContent         string
		expectSafeInputsEnvVars bool
		description             string
	}{
		{
			name: "No Safe Inputs - Should Not Include Env Vars",
			workflowContent: `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    toolsets: [default]
---

Test workflow without safe inputs
`,
			expectSafeInputsEnvVars: false,
			description:             "When safe-inputs is not configured, the MCP gateway should not reference Safe Inputs env vars",
		},
		{
			name: "With Safe Inputs - Should Include Env Vars",
			workflowContent: `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    toolsets: [default]
safe-inputs:
  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

Test workflow with safe inputs
`,
			expectSafeInputsEnvVars: true,
			description:             "When safe-inputs is configured, the MCP gateway should reference Safe Inputs env vars",
		},
		{
			name: "Claude Engine Without Safe Inputs",
			workflowContent: `---
on: workflow_dispatch
engine: claude
tools:
  github:
    toolsets: [default]
---

Test Claude workflow without safe inputs
`,
			expectSafeInputsEnvVars: false,
			description:             "Claude engine without safe-inputs should not include Safe Inputs env vars",
		},
		{
			name: "Codex Engine With Safe Inputs",
			workflowContent: `---
on: workflow_dispatch
engine: codex
safe-inputs:
  my-tool:
    description: My custom tool
    script: |
      return { status: "ok" };
---

Test Codex workflow with safe inputs
`,
			expectSafeInputsEnvVars: true,
			description:             "Codex engine with safe-inputs should include Safe Inputs env vars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			workflowPath := filepath.Join(tempDir, "test-workflow.md")

			err := os.WriteFile(workflowPath, []byte(tt.workflowContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			yamlStr := string(lockContent)

			// Check for "Start MCP gateway" step
			if !strings.Contains(yamlStr, "Start MCP gateway") {
				t.Skip("No MCP gateway step generated (sandbox might be disabled)")
			}

			// Extract the MCP gateway step section
			startIdx := strings.Index(yamlStr, "Start MCP gateway")
			if startIdx == -1 {
				t.Fatal("Start MCP gateway step not found")
			}

			// Find the next step or end of steps section
			nextStepIdx := strings.Index(yamlStr[startIdx+1:], "- name:")
			var mcpGatewaySection string
			if nextStepIdx != -1 {
				mcpGatewaySection = yamlStr[startIdx : startIdx+1+nextStepIdx]
			} else {
				mcpGatewaySection = yamlStr[startIdx:]
			}

			// Check for Safe Inputs env vars in the env section
			hasEnvVarsInEnvSection := strings.Contains(mcpGatewaySection, "GH_AW_SAFE_INPUTS_PORT:") &&
				strings.Contains(mcpGatewaySection, "GH_AW_SAFE_INPUTS_API_KEY:")

			// Check for Safe Inputs env vars in the Docker command
			hasEnvVarsInDockerCmd := strings.Contains(mcpGatewaySection, "-e GH_AW_SAFE_INPUTS_PORT") &&
				strings.Contains(mcpGatewaySection, "-e GH_AW_SAFE_INPUTS_API_KEY")

			if tt.expectSafeInputsEnvVars {
				if !hasEnvVarsInEnvSection {
					t.Errorf("%s: Expected GH_AW_SAFE_INPUTS_PORT and GH_AW_SAFE_INPUTS_API_KEY in env section but not found", tt.description)
				}
				if !hasEnvVarsInDockerCmd {
					t.Errorf("%s: Expected GH_AW_SAFE_INPUTS_PORT and GH_AW_SAFE_INPUTS_API_KEY in Docker command but not found", tt.description)
				}
			} else {
				if hasEnvVarsInEnvSection {
					t.Errorf("%s: Did not expect GH_AW_SAFE_INPUTS_PORT or GH_AW_SAFE_INPUTS_API_KEY in env section but found them", tt.description)
				}
				if hasEnvVarsInDockerCmd {
					t.Errorf("%s: Did not expect GH_AW_SAFE_INPUTS_PORT or GH_AW_SAFE_INPUTS_API_KEY in Docker command but found them", tt.description)
				}
			}
		})
	}
}

// TestMCPGatewaySafeInputsValidation tests that the workflow fails validation
// if Safe Inputs env vars are referenced but the safe-inputs-start step doesn't exist
func TestMCPGatewaySafeInputsValidation(t *testing.T) {
	t.Skip("This test is for future validation logic - not implemented yet")
	// This would test that if someone manually adds Safe Inputs to MCP tools list
	// without actually configuring safe-inputs, validation catches the error
}
