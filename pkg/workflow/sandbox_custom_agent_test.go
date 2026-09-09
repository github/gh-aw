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

	t.Run("default docker profile passes --rootless to install script", func(t *testing.T) {
		agentConfig := &AgentSandboxConfig{
			ID: "awf",
		}

		step := generateAWFInstallationStep("", agentConfig)
		stepStr := strings.Join(step, "\n")

		if len(step) == 0 {
			t.Error("Expected installation step to be generated for the default docker profile")
		}

		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Should contain reference to install_awf_binary.sh script")
		}

		if !strings.Contains(stepStr, "--rootless") {
			t.Error("Should contain --rootless flag for the rootless docker profile")
		}
	})

	t.Run("cloud-hypervisor profile does not pass --rootless", func(t *testing.T) {
		agentConfig := &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
		}

		step := generateAWFInstallationStep("", agentConfig)
		stepStr := strings.Join(step, "\n")

		if strings.Contains(stepStr, "--rootless") {
			t.Error("Should not contain --rootless flag for a privileged runtime profile")
		}
	})

	t.Run("docker-sudo-iptables does not pass --rootless", func(t *testing.T) {
		// The docker-sudo-iptables profile runs AWF with sudo, which requires awf to be
		// in /usr/local/bin (non-rootless install path).
		agentConfig := &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeDockerSudoIptables,
		}

		step := generateAWFInstallationStep("", agentConfig)
		stepStr := strings.Join(step, "\n")

		if len(step) == 0 {
			t.Error("Expected installation step to be generated for the docker-sudo-iptables profile")
		}

		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Should contain reference to install_awf_binary.sh script")
		}

		if strings.Contains(stepStr, "--rootless") {
			t.Error("Should not contain --rootless flag for docker-sudo-iptables (awf must be in /usr/local/bin for sudo)")
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
				Type: SandboxTypeAWF,
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
				ID: "awf",
			},
			expected: "awf",
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
strict: false
network:
  allowed:
    - "example.com"
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

		compiler := NewCompiler()
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

		// Verify the custom command is used instead of privileged AWF.
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
		if strings.Contains(lockStr, "Install AWF binary") {
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
strict: false
network:
  allowed:
    - "example.com"
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

		compiler := NewCompiler()
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

		// Verify standard AWF command is used (rootless mode, no sudo).
		if strings.Contains(lockStr, "sudo -E ") {
			t.Error("Expected no sudo with legacy type field (network isolation is the default)")
		}
		if !strings.Contains(lockStr, "awf --config") {
			t.Error("Expected rootless AWF invocation with legacy type field")
		}

		// Verify installation step is present
		if !strings.Contains(lockStr, "Install AWF binary") {
			t.Error("Expected AWF installation step with legacy type field")
		}
	})

	t.Run("custom command and args for AWF", func(t *testing.T) {
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
strict: false
sandbox:
  agent:
    id: awf
    command: "custom-awf-wrapper"
    args:
      - "--custom-awf-arg"
      - "--debug"
    env:
      AWF_CUSTOM_VAR: "test_value"
      AWF_DEBUG: "true"
---

# Test Custom AWF
`

		testFile := filepath.Join(tmpDir, "test-workflow.md")
		err = os.WriteFile(testFile, []byte(markdown), 0644)
		if err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
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

		// Verify custom AWF command is used
		if !strings.Contains(lockStr, "custom-awf-wrapper") {
			t.Error("Expected custom AWF command 'custom-awf-wrapper' in compiled workflow")
		}

		// Verify custom args are included
		if !strings.Contains(lockStr, "--custom-awf-arg") {
			t.Error("Expected custom arg '--custom-awf-arg' in compiled workflow")
		}
		if !strings.Contains(lockStr, "--debug") {
			t.Error("Expected custom arg '--debug' in compiled workflow")
		}

		// Verify custom env is included
		if !strings.Contains(lockStr, "AWF_CUSTOM_VAR: test_value") {
			t.Error("Expected custom env 'AWF_CUSTOM_VAR: test_value' in compiled workflow")
		}
		if !strings.Contains(lockStr, "AWF_DEBUG: true") {
			t.Error("Expected custom env 'AWF_DEBUG: true' in compiled workflow")
		}

		// Verify installation steps were skipped
		if strings.Contains(lockStr, "Install AWF") {
			t.Error("Expected AWF installation step to be skipped when custom command is specified")
		}
	})
}
