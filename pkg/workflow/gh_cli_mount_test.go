//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestEnableChrootInAWFContainer tests that --enable-chroot is present in AWF container
// With --enable-chroot, individual binary mounts are no longer needed as the container
// can access host binaries transparently via chroot /host
func TestEnableChrootInAWFContainer(t *testing.T) {
	t.Run("enable-chroot is present when firewall is enabled", func(t *testing.T) {
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

		// Check that --enable-chroot is included in AWF command
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected AWF command to contain --enable-chroot flag")
		}
	})

	t.Run("individual binary mounts are NOT present when firewall is enabled", func(t *testing.T) {
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

		// Verify individual binary mounts are NOT present (--enable-chroot provides transparent access)
		deprecatedMounts := []string{
			"--mount /usr/bin/cat:/usr/bin/cat:ro",
			"--mount /usr/bin/curl:/usr/bin/curl:ro",
			"--mount /usr/bin/gh:/usr/bin/gh:ro",
			"--mount /usr/bin/jq:/usr/bin/jq:ro",
			"--mount /usr/local/bin/copilot:/usr/local/bin/copilot:ro",
			"--mount /opt/hostedtoolcache:/opt/hostedtoolcache:ro",
			"--mount /opt/gh-aw:/opt/gh-aw:ro",
		}

		for _, mount := range deprecatedMounts {
			if strings.Contains(stepContent, mount) {
				t.Errorf("Expected AWF command to NOT contain deprecated mount '%s' (--enable-chroot provides transparent access)", mount)
			}
		}
	})

	t.Run("essential mounts are still present when firewall is enabled", func(t *testing.T) {
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

		// These essential mounts are still required
		requiredMounts := []string{
			"--mount /tmp:/tmp:rw",
			"--mount \"${HOME}/.cache:${HOME}/.cache:rw\"",
			"--mount \"${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw\"",
			"--mount /home/runner/.copilot:/home/runner/.copilot:rw",
		}

		for _, mount := range requiredMounts {
			if !strings.Contains(stepContent, mount) {
				t.Errorf("Expected AWF command to contain required mount '%s'", mount)
			}
		}
	})

	t.Run("AWF is NOT present when firewall is disabled", func(t *testing.T) {
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

	t.Run("enable-chroot works with custom firewall args", func(t *testing.T) {
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
			t.Error("Expected custom firewall args to be present")
		}
	})
}

// TestClaudeEngineChrootMounts tests that Claude engine uses --enable-chroot correctly
func TestClaudeEngineChrootMounts(t *testing.T) {
	t.Run("Claude engine has enable-chroot and minimal mounts", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --enable-chroot is included in AWF command
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected Claude AWF command to contain --enable-chroot flag")
		}

		// Verify deprecated mounts are NOT present
		deprecatedMounts := []string{
			"--mount /opt/hostedtoolcache:/opt/hostedtoolcache:ro",
			"--mount /opt/gh-aw:/opt/gh-aw:ro",
		}

		for _, mount := range deprecatedMounts {
			if strings.Contains(stepContent, mount) {
				t.Errorf("Expected Claude AWF command to NOT contain deprecated mount '%s'", mount)
			}
		}
	})
}

// TestCodexEngineChrootMounts tests that Codex engine uses --enable-chroot correctly
func TestCodexEngineChrootMounts(t *testing.T) {
	t.Run("Codex engine has enable-chroot and minimal mounts", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --enable-chroot is included in AWF command
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected Codex AWF command to contain --enable-chroot flag")
		}

		// Verify deprecated mounts are NOT present
		deprecatedMounts := []string{
			"--mount /opt/hostedtoolcache:/opt/hostedtoolcache:ro",
			"--mount /opt/gh-aw:/opt/gh-aw:ro",
		}

		for _, mount := range deprecatedMounts {
			if strings.Contains(stepContent, mount) {
				t.Errorf("Expected Codex AWF command to NOT contain deprecated mount '%s'", mount)
			}
		}
	})
}
