//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomAWFConfiguration(t *testing.T) {
	t.Run("custom command replaces AWF installation", func(t *testing.T) {
		agentConfig := &AgentSandboxConfig{
			ID:      "awf",
			Command: "docker run my-custom-awf",
		}

		step := generateAWFInstallationStep("", agentConfig)
		stepStr := strings.Join(step, "\n")

		// Step should be empty (installation skipped)
		if len(step) > 0 {
			t.Error("Expected installation step to be skipped when custom command is specified")
		}

		// Verify no curl commands
		if strings.Contains(stepStr, "curl") {
			t.Error("Should not contain curl command when custom command is specified")
		}
	})

	t.Run("nil agent config uses standard installation", func(t *testing.T) {
		step := generateAWFInstallationStep("", nil)
		stepStr := strings.Join(step, "\n")

		// Step should not be empty
		if len(step) == 0 {
			t.Error("Expected installation step to be generated when no agent config is provided")
		}

		// Should contain reference to installation script
		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Should contain reference to install_awf_binary.sh script for standard installation")
		}
	})

	t.Run("agent config without command uses standard installation", func(t *testing.T) {
		agentConfig := &AgentSandboxConfig{
			ID: "awf",
		}

		step := generateAWFInstallationStep("", agentConfig)
		stepStr := strings.Join(step, "\n")

		// Step should not be empty
		if len(step) == 0 {
			t.Error("Expected installation step to be generated when command is not specified")
		}

		// Should contain reference to installation script
		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Should contain reference to install_awf_binary.sh script for standard installation")
		}
	})
}

func TestGetAgentType(t *testing.T) {
	tests := []struct {
		name     string
		agent    *AgentSandboxConfig
		expected SandboxType
	}{
		{
			name:     "nil agent returns empty",
			agent:    nil,
			expected: "",
		},
		{
			name: "ID field takes precedence over Type",
			agent: &AgentSandboxConfig{
				ID:   "awf",
				Type: SandboxTypeSRT,
			},
			expected: "awf",
		},
		{
			name: "Type field used when ID is empty",
			agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
			expected: SandboxTypeAWF,
		},
		{
			name: "ID field only",
			agent: &AgentSandboxConfig{
				ID: "srt",
			},
			expected: "srt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAgentType(tt.agent)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCustomAWFCommandExecution(t *testing.T) {
	t.Run("custom command and args in workflow compilation", func(t *testing.T) {
		// Create temp directory for test
		tmpDir, err := os.MkdirTemp("", "custom-awf-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		markdown := `---
on:
  workflow_dispatch:
engine: copilot
network:
  allowed:
    - "example.com"
  firewall: true
sandbox:
  agent:
    id: awf
    command: "custom-awf-wrapper"
    args:
      - "--custom-arg1"
      - "--custom-arg2"
    env:
      CUSTOM_VAR: "custom_value"
---

# Test Custom AWF
`

		testFile := filepath.Join(tmpDir, "test-workflow.md")
		err = os.WriteFile(testFile, []byte(markdown), 0644)
		if err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler(
			WithVersion("test"),
			WithStrictMode(false),
		)
		err = compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		// Read the lock file
		lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatal(err)
		}
		lockStr := string(lockContent)

		// Verify custom command is used instead of sudo -E awf
		if !strings.Contains(lockStr, "custom-awf-wrapper") {
			t.Error("Expected custom command 'custom-awf-wrapper' in compiled workflow")
		}

		// Verify custom args are included
		if !strings.Contains(lockStr, "--custom-arg1") {
			t.Error("Expected custom arg '--custom-arg1' in compiled workflow")
		}
		if !strings.Contains(lockStr, "--custom-arg2") {
			t.Error("Expected custom arg '--custom-arg2' in compiled workflow")
		}

		// Verify custom env is included
		if !strings.Contains(lockStr, "CUSTOM_VAR: custom_value") {
			t.Error("Expected custom env 'CUSTOM_VAR: custom_value' in compiled workflow")
		}

		// Verify installation step was skipped (no curl command for AWF)
		if strings.Contains(lockStr, "Install awf binary") {
			t.Error("Expected AWF installation step to be skipped when custom command is specified")
		}
	})

	t.Run("legacy type field still works", func(t *testing.T) {
		// Create temp directory for test
		tmpDir, err := os.MkdirTemp("", "legacy-type-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		markdown := `---
on:
  workflow_dispatch:
engine: copilot
network:
  allowed:
    - "example.com"
  firewall: true
sandbox:
  agent:
    type: awf
---

# Test Legacy Type
`

		testFile := filepath.Join(tmpDir, "test-workflow.md")
		err = os.WriteFile(testFile, []byte(markdown), 0644)
		if err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler(
			WithVersion("test"),
			WithStrictMode(false),
		)
		err = compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		// Read the lock file
		lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatal(err)
		}
		lockStr := string(lockContent)

		// Verify standard AWF command is used
		if !strings.Contains(lockStr, "sudo -E awf") {
			t.Error("Expected standard AWF command 'sudo -E awf' with legacy type field")
		}

		// Verify installation step is present
		if !strings.Contains(lockStr, "Install awf binary") {
			t.Error("Expected AWF installation step with legacy type field")
		}
	})

	t.Run("custom command and args for SRT", func(t *testing.T) {
		// Create temp directory for test
		tmpDir, err := os.MkdirTemp("", "custom-srt-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		markdown := `---
on:
  workflow_dispatch:
engine: copilot
features:
  sandbox-runtime: true
sandbox:
  agent:
    id: srt
    command: "custom-srt-wrapper"
    args:
      - "--custom-srt-arg"
      - "--debug"
    env:
      SRT_CUSTOM_VAR: "test_value"
      SRT_DEBUG: "true"
---

# Test Custom SRT
`

		testFile := filepath.Join(tmpDir, "test-workflow.md")
		err = os.WriteFile(testFile, []byte(markdown), 0644)
		if err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler(
			WithVersion("test"),
			WithStrictMode(false),
		)
		err = compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		// Read the lock file
		lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatal(err)
		}
		lockStr := string(lockContent)

		// Verify custom SRT command is used
		if !strings.Contains(lockStr, "custom-srt-wrapper") {
			t.Error("Expected custom SRT command 'custom-srt-wrapper' in compiled workflow")
		}

		// Verify custom args are included
		if !strings.Contains(lockStr, "--custom-srt-arg") {
			t.Error("Expected custom arg '--custom-srt-arg' in compiled workflow")
		}
		if !strings.Contains(lockStr, "--debug") {
			t.Error("Expected custom arg '--debug' in compiled workflow")
		}

		// Verify custom env is included
		if !strings.Contains(lockStr, "SRT_CUSTOM_VAR: test_value") {
			t.Error("Expected custom env 'SRT_CUSTOM_VAR: test_value' in compiled workflow")
		}
		if !strings.Contains(lockStr, "SRT_DEBUG: true") {
			t.Error("Expected custom env 'SRT_DEBUG: true' in compiled workflow")
		}

		// Verify installation steps were skipped
		if strings.Contains(lockStr, "Install Sandbox Runtime") {
			t.Error("Expected SRT installation step to be skipped when custom command is specified")
		}
	})
}
