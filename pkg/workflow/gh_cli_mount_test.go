//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestGhCLIMountInAWFContainer tests that gh CLI binary is mounted in AWF container
func TestGhCLIMountInAWFContainer(t *testing.T) {
	t.Run("gh CLI is mounted when firewall is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that gh CLI binary mount is included in AWF command
		expectedMount := "--mount /usr/bin/gh:/usr/bin/gh:ro"
		if !strings.Contains(stepContent, expectedMount) {
			t.Errorf("Expected AWF command to contain gh CLI binary mount '%s', but it was not found", expectedMount)
		}

		// Verify mount is read-only
		if !strings.Contains(stepContent, "/usr/bin/gh:ro") {
			t.Error("Expected gh CLI mount to be read-only (:ro)")
		}
	})

	t.Run("gh CLI is NOT mounted when firewall is disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that AWF command is not used
		if strings.Contains(stepContent, "awf") {
			t.Error("Expected no AWF command when firewall is disabled")
		}

		// Check that gh CLI mount is not present
		if strings.Contains(stepContent, "/usr/bin/gh") {
			t.Error("Expected no gh CLI mount when firewall is disabled")
		}
	})

	t.Run("gh CLI mount is positioned after workspace mounts", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Find positions of mounts in the command
		tmpMountPos := strings.Index(stepContent, "--mount /tmp:/tmp:rw")
		workspaceMountPos := strings.Index(stepContent, "--mount \"${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw\"")
		ghMountPos := strings.Index(stepContent, "--mount /usr/bin/gh:/usr/bin/gh:ro")

		if tmpMountPos == -1 || workspaceMountPos == -1 || ghMountPos == -1 {
			t.Fatal("Not all expected mounts were found in the command")
		}

		// Verify order: /tmp < workspace < gh
		if tmpMountPos >= workspaceMountPos || workspaceMountPos >= ghMountPos {
			t.Error("Expected mount order: /tmp, workspace, gh CLI")
		}
	})

	t.Run("gh CLI mount works with custom firewall args", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Args:    []string{"--custom-flag", "value"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Verify both gh mount and custom args are present
		if !strings.Contains(stepContent, "--mount /usr/bin/gh:/usr/bin/gh:ro") {
			t.Error("Expected gh CLI mount to be present with custom firewall args")
		}

		if !strings.Contains(stepContent, "--custom-flag") {
			t.Error("Expected custom firewall args to be present with gh CLI mount")
		}
	})

	t.Run("gh CLI mount works with SRT disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			// Explicitly ensure SRT is not enabled
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Verify gh CLI mount is present
		if !strings.Contains(stepContent, "--mount /usr/bin/gh:/usr/bin/gh:ro") {
			t.Error("Expected gh CLI mount to be present when using AWF (not SRT)")
		}

		// Verify AWF is being used
		if !strings.Contains(stepContent, "awf") {
			t.Error("Expected AWF to be used when firewall is enabled")
		}
	})
}

// TestUtilityBinaryMountsInAWFContainer tests that utility binaries are mounted in AWF container
func TestUtilityBinaryMountsInAWFContainer(t *testing.T) {
	// Define all expected utility mounts (sorted alphabetically within their path)
	expectedEssentialMounts := []string{
		"--mount /usr/bin/cat:/usr/bin/cat:ro",
		"--mount /usr/bin/curl:/usr/bin/curl:ro",
		"--mount /usr/bin/date:/usr/bin/date:ro",
		"--mount /usr/bin/find:/usr/bin/find:ro",
		"--mount /usr/bin/gh:/usr/bin/gh:ro",
		"--mount /usr/bin/grep:/usr/bin/grep:ro",
		"--mount /usr/bin/jq:/usr/bin/jq:ro",
		"--mount /usr/bin/yq:/usr/bin/yq:ro",
	}

	expectedCommonMounts := []string{
		"--mount /usr/bin/cp:/usr/bin/cp:ro",
		"--mount /usr/bin/cut:/usr/bin/cut:ro",
		"--mount /usr/bin/diff:/usr/bin/diff:ro",
		"--mount /usr/bin/head:/usr/bin/head:ro",
		"--mount /usr/bin/ls:/usr/bin/ls:ro",
		"--mount /usr/bin/mkdir:/usr/bin/mkdir:ro",
		"--mount /usr/bin/rm:/usr/bin/rm:ro",
		"--mount /usr/bin/sed:/usr/bin/sed:ro",
		"--mount /usr/bin/sort:/usr/bin/sort:ro",
		"--mount /usr/bin/tail:/usr/bin/tail:ro",
		"--mount /usr/bin/wc:/usr/bin/wc:ro",
		"--mount /usr/bin/which:/usr/bin/which:ro",
	}

	t.Run("essential utilities are mounted when firewall is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		for _, mount := range expectedEssentialMounts {
			if !strings.Contains(stepContent, mount) {
				t.Errorf("Expected AWF command to contain essential utility mount '%s', but it was not found", mount)
			}
		}
	})

	t.Run("common utilities are mounted when firewall is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		for _, mount := range expectedCommonMounts {
			if !strings.Contains(stepContent, mount) {
				t.Errorf("Expected AWF command to contain common utility mount '%s', but it was not found", mount)
			}
		}
	})

	t.Run("utility mounts are NOT present when firewall is disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that AWF command is not used
		if strings.Contains(stepContent, "awf") {
			t.Error("Expected no AWF command when firewall is disabled")
		}

		// Sample check: jq mount should not be present
		if strings.Contains(stepContent, "/usr/bin/jq") {
			t.Error("Expected no utility mounts when firewall is disabled")
		}
	})

	t.Run("all utility mounts are read-only", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Verify all utility mounts are present and read-only
		allMounts := append(expectedEssentialMounts, expectedCommonMounts...)
		for _, mount := range allMounts {
			// Verify the mount string ends with :ro (read-only)
			if !strings.HasSuffix(mount, ":ro") {
				t.Errorf("Test data error: expected mount '%s' should end with ':ro'", mount)
				continue
			}
			// Verify the mount is present in the step content
			if !strings.Contains(stepContent, mount) {
				t.Errorf("Expected utility mount '%s' to be present in AWF command", mount)
			}
		}
	})
}
