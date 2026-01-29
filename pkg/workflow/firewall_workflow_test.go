//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestFirewallWorkflowNetworkConfiguration verifies that the firewall workflow
// is properly configured to block access to example.com
func TestFirewallWorkflowNetworkConfiguration(t *testing.T) {
	// Create workflow data with network defaults, firewall enabled, and web-fetch tool
	workflowData := &WorkflowData{
		Name: "firewall",
		EngineConfig: &EngineConfig{
			ID:    "claude",
			Model: "claude-3-5-sonnet-20241022",
		},
		NetworkPermissions: &NetworkPermissions{
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

		// With AWF enabled (using parallel installation): secret validation, Node.js setup
		// AWF and Claude CLI installation are deferred to parallel installation step
		if len(steps) != 2 {
			t.Errorf("Expected 2 installation steps with firewall enabled (secret validation + Node.js setup), got %d", len(steps))
		}

		// Verify that AWF installation is skipped (will be handled by parallel installation)
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				t.Error("AWF installation should be deferred to parallel installation step")
			}
		}

		// Verify that parallel installation should be used
		if !ShouldUseParallelInstallation(workflowData, engine) {
			t.Error("Parallel installation should be enabled with firewall and Claude engine")
		}

		// Verify parallel installation config includes AWF
		config := GetParallelInstallConfig(workflowData, engine)
		if config.AWFVersion == "" {
			t.Error("Parallel installation should include AWF version")
		}
		if config.ClaudeVersion == "" {
			t.Error("Parallel installation should include Claude version")
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
		if !strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF wrapper should be present with firewall enabled")
		}

		// Verify --tty flag is present (required for Claude)
		if !strings.Contains(stepYAML, "--tty") {
			t.Error("--tty flag should be present for Claude with AWF")
		}

		// Verify --allow-domains is present
		if !strings.Contains(stepYAML, "--allow-domains") {
			t.Error("--allow-domains should be present with AWF")
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
	c := NewCompilerWithVersion("firewall")
	c.SetSkipValidation(true)

	// Extract and verify tools
	tools := extractToolsFromFrontmatter(frontmatter)
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
