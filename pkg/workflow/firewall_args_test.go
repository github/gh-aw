//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestFirewallArgsInCopilotEngine tests that custom firewall args are included in AWF command
func TestFirewallArgsInCopilotEngine(t *testing.T) {
	t.Run("no custom args uses only default flags", func(t *testing.T) {
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

		// Check that the command contains --enable-chroot for AWF v0.13.1+ chroot mode
		if !strings.Contains(stepContent, "awf --enable-chroot") {
			t.Error("Expected command to contain 'awf --enable-chroot'")
		}

		if !strings.Contains(stepContent, "--allow-domains") {
			t.Error("Expected command to contain '--allow-domains'")
		}

		if !strings.Contains(stepContent, "--log-level") {
			t.Error("Expected command to contain '--log-level'")
		}

		// Verify that --log-dir is included in copilot args for log collection
		if !strings.Contains(stepContent, "--log-dir /tmp/gh-aw/sandbox/agent/logs/") {
			t.Error("Expected copilot command to contain '--log-dir /tmp/gh-aw/sandbox/agent/logs/' for log collection in firewall mode")
		}
	})

	t.Run("custom args are included in AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Args:    []string{"--custom-arg", "value", "--another-flag"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that custom args are included
		if !strings.Contains(stepContent, "--custom-arg") {
			t.Error("Expected command to contain custom arg '--custom-arg'")
		}

		if !strings.Contains(stepContent, "value") {
			t.Error("Expected command to contain custom arg value 'value'")
		}

		if !strings.Contains(stepContent, "--another-flag") {
			t.Error("Expected command to contain custom arg '--another-flag'")
		}

		// Verify standard flags are still present
		if !strings.Contains(stepContent, "--allow-domains") {
			t.Error("Expected command to still contain '--allow-domains'")
		}
	})

	t.Run("custom args with spaces are properly escaped", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Args:    []string{"--message", "hello world", "--path", "/some/path with spaces"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that args with spaces are present (they should be escaped)
		if !strings.Contains(stepContent, "--message") {
			t.Error("Expected command to contain '--message' flag")
		}

		// The value might be escaped, so just check the flag exists
		if !strings.Contains(stepContent, "--path") {
			t.Error("Expected command to contain '--path' flag")
		}
	})

	t.Run("AWF uses chroot mode instead of individual binary mounts", func(t *testing.T) {
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

		// Check that --enable-chroot is used for transparent host access (AWF v0.13.1+)
		// This replaces the need for explicit binary mounts like --mount /usr/bin/gh:/usr/bin/gh:ro
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected AWF command to contain '--enable-chroot' for transparent host access")
		}

		// Verify that individual binary mounts are no longer used (replaced by chroot)
		if strings.Contains(stepContent, "--mount /usr/bin/gh:/usr/bin/gh:ro") {
			t.Error("Individual binary mounts should be replaced by --enable-chroot mode")
		}
	})

	t.Run("AWF command includes image-tag with default version", func(t *testing.T) {
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

		// Check that --image-tag is included with default version (without v prefix)
		expectedImageTag := "--image-tag " + strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if !strings.Contains(stepContent, expectedImageTag) {
			t.Errorf("Expected AWF command to contain '%s', got:\n%s", expectedImageTag, stepContent)
		}
	})

	t.Run("AWF command includes image-tag with custom version", func(t *testing.T) {
		customVersion := "v0.5.0"
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: customVersion,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --image-tag is included with custom version (without v prefix)
		expectedImageTag := "--image-tag " + strings.TrimPrefix(customVersion, "v")
		if !strings.Contains(stepContent, expectedImageTag) {
			t.Errorf("Expected AWF command to contain '%s', got:\n%s", expectedImageTag, stepContent)
		}

		// Ensure default version is not used when custom version is specified
		defaultImageTag := "--image-tag " + strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if strings.TrimPrefix(customVersion, "v") != strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v") && strings.Contains(stepContent, defaultImageTag) {
			t.Error("Should use custom version, not default version")
		}
	})

	t.Run("AWF command includes ssl-bump flag when enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					SSLBump: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --ssl-bump flag is included
		if !strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to contain '--ssl-bump' flag")
		}
	})

	t.Run("AWF command includes allow-urls with ssl-bump enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   true,
					AllowURLs: []string{"https://github.com/githubnext/*", "https://api.github.com/repos/*"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --ssl-bump flag is included
		if !strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to contain '--ssl-bump' flag")
		}

		// Check that --allow-urls is included with the comma-separated URLs
		if !strings.Contains(stepContent, "--allow-urls") {
			t.Error("Expected AWF command to contain '--allow-urls' flag")
		}

		if !strings.Contains(stepContent, "https://github.com/githubnext/*") {
			t.Error("Expected AWF command to contain URL pattern 'https://github.com/githubnext/*'")
		}
	})

	t.Run("AWF command does not include allow-urls without ssl-bump", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   false, // SSL Bump disabled
					AllowURLs: []string{"https://github.com/githubnext/*"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that --ssl-bump flag is NOT included
		if strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to NOT contain '--ssl-bump' flag when SSLBump is false")
		}

		// Check that --allow-urls is NOT included when ssl-bump is disabled
		if strings.Contains(stepContent, "--allow-urls") {
			t.Error("Expected AWF command to NOT contain '--allow-urls' flag when SSLBump is false")
		}
	})
}
