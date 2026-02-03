//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestChrootModeInAWFContainer tests that AWF uses --enable-chroot mode for transparent host access
func TestChrootModeInAWFContainer(t *testing.T) {
	t.Run("chroot mode is enabled when firewall is enabled", func(t *testing.T) {
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

		// Check that --enable-chroot is used instead of individual binary mounts
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected AWF command to contain '--enable-chroot' for transparent host access")
		}
	})

	t.Run("AWF command is NOT used when firewall is disabled", func(t *testing.T) {
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

		// Check that --enable-chroot is not present
		if strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected no --enable-chroot when firewall is disabled")
		}
	})

	t.Run("chroot mode replaces individual binary mounts", func(t *testing.T) {
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

		// Verify --enable-chroot is present
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected --enable-chroot to be present")
		}

		// Verify individual binary mounts are NOT present (replaced by chroot)
		individualMounts := []string{
			"--mount /usr/bin/gh:/usr/bin/gh:ro",
			"--mount /usr/bin/cat:/usr/bin/cat:ro",
			"--mount /usr/bin/jq:/usr/bin/jq:ro",
			"--mount /tmp:/tmp:rw",
			"--mount /opt/hostedtoolcache:/opt/hostedtoolcache:ro",
		}

		for _, mount := range individualMounts {
			if strings.Contains(stepContent, mount) {
				t.Errorf("Individual mount '%s' should be replaced by --enable-chroot mode", mount)
			}
		}
	})

	t.Run("chroot mode works with custom firewall args", func(t *testing.T) {
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

		// Verify both --enable-chroot and custom args are present
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected --enable-chroot to be present with custom firewall args")
		}

		if !strings.Contains(stepContent, "--custom-flag") {
			t.Error("Expected custom firewall args to be present with chroot mode")
		}
	})

	t.Run("chroot mode works with AWF sandbox type", func(t *testing.T) {
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
			// Explicitly set AWF sandbox type
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

		// Verify --enable-chroot is present
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected --enable-chroot to be present when using AWF")
		}

		// Verify AWF is being used
		if !strings.Contains(stepContent, "awf") {
			t.Error("Expected AWF to be used when firewall is enabled")
		}
	})
}

// TestChrootModeEnvFlags tests that --env-all is used with chroot mode to pass env vars to AWF
func TestChrootModeEnvFlags(t *testing.T) {
	t.Run("env-all is required for AWF to receive host env vars", func(t *testing.T) {
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

		// Verify --enable-chroot is present (provides transparent host access)
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected --enable-chroot to be present")
		}

		// Verify --env-all IS used (required for AWF to receive host environment variables)
		if !strings.Contains(stepContent, "--env-all") {
			t.Error("--env-all is required for AWF to receive host environment variables")
		}
	})
}
