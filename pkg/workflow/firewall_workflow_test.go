//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFirewallWorkflowNetworkConfiguration verifies that the firewall workflow
// is properly configured to block access to example.com
func TestFirewallWorkflowNetworkConfiguration(t *testing.T) {
	// Create workflow data with network defaults, firewall enabled, and web-fetch tool
	workflowData := &WorkflowData{
		Name:  "firewall",
		Model: "claude-3-5-sonnet-20241022",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		NetworkPermissions: &NetworkPermissions{
			Allowed:  []string{"claude"},
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{
			"web-fetch": nil,
		},
	}

	t.Run("example.com is not in default allowed domains", func(t *testing.T) {
		allowedDomains := GetAllowedDomains(workflowData.NetworkPermissions)
		for _, domain := range allowedDomains {
			if domain == "example.com" {
				t.Error("example.com should not be in the default allowed domains list")
			}
		}
	})

	t.Run("AWF is installed with firewall enabled", func(t *testing.T) {
		engine := NewClaudeEngine()
		steps := engine.GetInstallationSteps(workflowData)

		// With AWF enabled: Node.js setup, AWF install, Claude install = 3 steps
		// (secret validation is now in the activation job)
		if len(steps) != 3 {
			t.Errorf("Expected 3 installation steps with firewall enabled (Node.js setup + AWF install + Claude install), got %d", len(steps))
		}

		// Check AWF installation step (2nd step, index 1)
		awfStepStr := strings.Join(steps[1], "\n")
		if !strings.Contains(awfStepStr, "Install AWF binary") {
			t.Error("Second step should install AWF binary")
		}
	})

	t.Run("execution step includes AWF wrapper", func(t *testing.T) {
		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test-log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF wrapper is present (required for network sandboxing)
		if !strings.Contains(stepYAML, "awf ") {
			t.Error("AWF wrapper should be present with firewall enabled")
		}

		// Verify --tty flag is present (required for Claude)
		if !strings.Contains(stepYAML, "--tty") {
			t.Error("--tty flag should be present for Claude with AWF")
		}

		// Verify domains are in the AWF config JSON (not as --allow-domains CLI flag)
		if !strings.Contains(stepYAML, "allowDomains") {
			t.Error("allowDomains should be present in AWF config JSON")
		}
	})
}

// TestFirewallWorkflowCompilation verifies the firewall workflow compiles correctly
func TestFirewallWorkflowCompilation(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
		"engine":  "claude",
		"network": "defaults",
		"tools": map[string]any{
			"web-fetch": nil,
		},
		"timeout_minutes": 5,
	}

	// Create compiler
	c := NewCompiler(WithVersion("firewall"))
	c.SetSkipValidation(true)

	// Extract and verify tools
	tools := extractToolsMapFromFrontmatter(frontmatter)
	if _, exists := tools["web-fetch"]; !exists {
		t.Error("web-fetch tool should be present in firewall workflow")
	}

	// Verify network permissions
	networkPerms := c.extractNetworkPermissions(frontmatter)
	if networkPerms == nil {
		t.Fatal("Network permissions should be configured")
	}

	// Verify it's using defaults ecosystem
	if len(networkPerms.Allowed) != 1 || networkPerms.Allowed[0] != "defaults" {
		t.Errorf("Expected network allowed to be ['defaults'], got %v", networkPerms.Allowed)
	}

	// Get the actual allowed domains using the GetAllowedDomains function
	allowedDomains := GetAllowedDomains(networkPerms)
	if len(allowedDomains) == 0 {
		t.Error("Default network permissions should have allowed domains")
	}

	// Verify example.com is not in the allowed list
	for _, domain := range allowedDomains {
		if domain == "example.com" {
			t.Error("example.com should not be in the allowed domains")
		}
	}
}

// TestFirewallWorkflowBlocksExampleCom tests that the network hook would block example.com
func TestFirewallWorkflowBlocksExampleCom(t *testing.T) {
	networkPerms := &NetworkPermissions{
		Allowed: []string{"defaults"},
	}
	allowedDomains := GetAllowedDomains(networkPerms)

	// Create a simple function to check if domain would be allowed
	isDomainAllowed := func(domain string, allowedList []string) bool {
		for _, allowed := range allowedList {
			if allowed == domain {
				return true
			}
			// Check wildcard patterns
			if strings.HasPrefix(allowed, "*.") {
				suffix := allowed[2:]
				if strings.HasSuffix(domain, suffix) {
					return true
				}
			}
		}
		return false
	}

	// Test that example.com is blocked
	if isDomainAllowed("example.com", allowedDomains) {
		t.Error("example.com should be blocked by default network permissions")
	}

	// Test that some infrastructure domains are allowed
	infrastructureDomains := []string{
		"json-schema.org",
		"archive.ubuntu.com",
		"ocsp.digicert.com",
	}

	for _, domain := range infrastructureDomains {
		if !isDomainAllowed(domain, allowedDomains) {
			t.Errorf("Infrastructure domain '%s' should be allowed by default network permissions", domain)
		}
	}
}

// TestCompileWorkflow_LegacySecurityFromFrontmatter validates that
// runtime: docker-sudo-iptables passes schema validation and produces the expected AWF
// command with sudo and --legacy-security.
func TestCompileWorkflow_LegacySecurityFromFrontmatter(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    id: awf
    runtime: docker-sudo-iptables
network:
  allowed:
    - defaults
tools:
  web-fetch:
---

# Test
Test workflow with legacy security.`

	tmpDir := testutil.TempDir(t, "legacy-security-compile-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Compilation with runtime: docker-sudo-iptables should succeed")

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Should read compiled lock file")

	lockStr := string(lockContent)

	// Legacy mode should preserve the runner PATH across sudo's secure_path.
	assert.Contains(t, lockStr, `sudo -E /usr/bin/env PATH="$PATH" /usr/local/bin/awf`, "Legacy security mode should preserve PATH across sudo")

	// Should emit --legacy-security flag
	assert.Contains(t, lockStr, "--legacy-security", "Legacy security mode should emit --legacy-security flag")

	// Should emit --enable-host-access
	assert.Contains(t, lockStr, "--enable-host-access", "Legacy security mode should emit --enable-host-access flag")
}

// TestCompileWorkflow_StrictSecurityDefault validates that omitting legacy-security
// produces the strict mode AWF command without sudo.
func TestCompileWorkflow_StrictSecurityDefault(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    id: awf
network:
  allowed:
    - defaults
tools:
  web-fetch:
---

# Test
Test workflow with strict security (default).`

	tmpDir := testutil.TempDir(t, "strict-security-compile-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Compilation with strict security (default) should succeed")

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Should read compiled lock file")

	lockStr := string(lockContent)

	// Strict mode should NOT use sudo
	assert.NotContains(t, lockStr, "sudo -E ", "Strict security mode should NOT use sudo")

	// Should NOT emit --legacy-security flag
	assert.NotContains(t, lockStr, "--legacy-security", "Strict security mode should NOT emit --legacy-security")

	// Should NOT emit --enable-host-access
	assert.NotContains(t, lockStr, "--enable-host-access", "Strict security mode should NOT emit --enable-host-access")

	// Should still have awf command
	assert.Contains(t, lockStr, "awf ", "Strict security mode should still use 'awf' command")
}
