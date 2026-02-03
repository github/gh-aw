//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCLIVersionInAwInfo(t *testing.T) {
	tests := []struct {
		name          string
		cliVersion    string
		engineID      string
		description   string
		shouldInclude bool
		isRelease     bool // Whether to mark as release build
	}{
		{
			name:          "Released CLI version is stored in aw_info.json",
			cliVersion:    "1.2.3",
			engineID:      "copilot",
			description:   "Should include cli_version field with correct value for released builds",
			shouldInclude: true,
			isRelease:     true,
		},
		{
			name:          "CLI version with semver prerelease",
			cliVersion:    "1.2.3-beta.1",
			engineID:      "claude",
			description:   "Should handle prerelease versions",
			shouldInclude: true,
			isRelease:     true,
		},
		{
			name:          "Development CLI version is excluded",
			cliVersion:    "dev",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for development builds",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Dirty CLI version is excluded",
			cliVersion:    "1.2.3-dirty",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for dirty builds",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Test CLI version is excluded",
			cliVersion:    "1.0.0-test",
			engineID:      "claude",
			description:   "Should NOT include cli_version field for test builds",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Git hash with dirty suffix is excluded",
			cliVersion:    "708d3ee-dirty",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for git hash with dirty suffix",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Git commit hash is excluded",
			cliVersion:    "e63fd5a",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for git commit hash",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Short git hash is excluded",
			cliVersion:    "abc123",
			engineID:      "claude",
			description:   "Should NOT include cli_version field for short git hash",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Version starting with v is excluded",
			cliVersion:    "v1.2.3",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for version with v prefix",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Version with only major number is excluded",
			cliVersion:    "1",
			engineID:      "copilot",
			description:   "Should NOT include cli_version field for version with only major number",
			shouldInclude: false,
			isRelease:     false,
		},
		{
			name:          "Version with only major.minor is included",
			cliVersion:    "1.2",
			engineID:      "copilot",
			description:   "Should include cli_version field for version with major.minor",
			shouldInclude: true,
			isRelease:     true,
		},
		{
			name:          "Version with build metadata is included",
			cliVersion:    "1.2.3+build.456",
			engineID:      "claude",
			description:   "Should include cli_version field for version with build metadata",
			shouldInclude: true,
			isRelease:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original state
			originalIsRelease := isReleaseBuild
			defer func() { isReleaseBuild = originalIsRelease }()

			// Set the release flag for this test
			SetIsRelease(tt.isRelease)

			compiler := NewCompilerWithVersion(tt.cliVersion)
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get %s engine: %v", tt.engineID, err)
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
			}

			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			expectedLine := `cli_version: "` + tt.cliVersion + `"`
			containsVersion := strings.Contains(output, expectedLine)

			if tt.shouldInclude {
				if !containsVersion {
					t.Errorf("%s: Expected output to contain '%s', got:\n%s",
						tt.description, expectedLine, output)
				}
			} else {
				// For dev builds, cli_version should not appear at all
				if strings.Contains(output, "cli_version:") {
					t.Errorf("%s: Expected output to NOT contain 'cli_version:' field, got:\n%s",
						tt.description, output)
				}
			}
		})
	}
}

func TestAwfVersionInAwInfo(t *testing.T) {
	tests := []struct {
		name               string
		firewallEnabled    bool
		firewallVersion    string
		expectedAwfVersion string
		description        string
	}{
		{
			name:               "Firewall enabled with explicit version",
			firewallEnabled:    true,
			firewallVersion:    "v1.0.0",
			expectedAwfVersion: "v1.0.0",
			description:        "Should use explicit firewall version",
		},
		{
			name:               "Firewall enabled with default version",
			firewallEnabled:    true,
			firewallVersion:    "",
			expectedAwfVersion: string(constants.DefaultFirewallVersion),
			description:        "Should use default firewall version when not specified",
		},
		{
			name:               "Firewall disabled",
			firewallEnabled:    false,
			firewallVersion:    "",
			expectedAwfVersion: "",
			description:        "Should have empty awf_version when firewall is disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompilerWithVersion("1.0.0")
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine("copilot")
			if err != nil {
				t.Fatalf("Failed to get copilot engine: %v", err)
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
			}

			if tt.firewallEnabled {
				workflowData.NetworkPermissions = &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: tt.firewallVersion,
					},
				}
			}

			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			expectedLine := `awf_version: "` + tt.expectedAwfVersion + `"`
			if !strings.Contains(output, expectedLine) {
				t.Errorf("%s: Expected output to contain '%s', got:\n%s",
					tt.description, expectedLine, output)
			}
		})
	}
}

func TestBothVersionsInAwInfo(t *testing.T) {
	// Save and restore original state
	originalIsRelease := isReleaseBuild
	defer func() { isReleaseBuild = originalIsRelease }()

	// Set as release build to include CLI version
	SetIsRelease(true)

	// Test that both CLI version and AWF version are present simultaneously
	compiler := NewCompilerWithVersion("2.0.0-beta.5")
	registry := GetGlobalEngineRegistry()
	engine, err := registry.GetEngine("copilot")
	if err != nil {
		t.Fatalf("Failed to get copilot engine: %v", err)
	}

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
				Version: "v0.5.0",
			},
		},
	}

	var yaml strings.Builder
	compiler.generateCreateAwInfo(&yaml, workflowData, engine)
	output := yaml.String()

	// Check for cli_version
	expectedCLILine := `cli_version: "2.0.0-beta.5"`
	if !strings.Contains(output, expectedCLILine) {
		t.Errorf("Expected output to contain cli_version '%s', got:\n%s", expectedCLILine, output)
	}

	// Check for awf_version
	expectedAwfLine := `awf_version: "v0.5.0"`
	if !strings.Contains(output, expectedAwfLine) {
		t.Errorf("Expected output to contain awf_version '%s', got:\n%s", expectedAwfLine, output)
	}
}

func TestAwmgVersionInAwInfo(t *testing.T) {
	tests := []struct {
		name                string
		mcpGatewayVersion   string
		expectedAwmgVersion string
		description         string
	}{
		{
			name:                "MCP Gateway with explicit version",
			mcpGatewayVersion:   "v0.0.10",
			expectedAwmgVersion: "v0.0.10",
			description:         "Should use explicit MCP gateway version",
		},
		{
			name:                "MCP Gateway with default version",
			mcpGatewayVersion:   string(constants.DefaultMCPGatewayVersion),
			expectedAwmgVersion: string(constants.DefaultMCPGatewayVersion),
			description:         "Should use default MCP gateway version",
		},
		{
			name:                "No MCP Gateway configured",
			mcpGatewayVersion:   "",
			expectedAwmgVersion: "",
			description:         "Should have empty awmg_version when MCP gateway is not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompilerWithVersion("1.0.0")
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine("copilot")
			if err != nil {
				t.Fatalf("Failed to get copilot engine: %v", err)
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
			}

			if tt.mcpGatewayVersion != "" {
				workflowData.SandboxConfig = &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Version: tt.mcpGatewayVersion,
					},
				}
			}

			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			expectedLine := `awmg_version: "` + tt.expectedAwmgVersion + `"`
			if !strings.Contains(output, expectedLine) {
				t.Errorf("%s: Expected output to contain '%s', got:\n%s",
					tt.description, expectedLine, output)
			}
		})
	}
}

func TestAllVersionsInAwInfo(t *testing.T) {
	// Save and restore original state
	originalIsRelease := isReleaseBuild
	defer func() { isReleaseBuild = originalIsRelease }()

	// Set as release build to include CLI version
	SetIsRelease(true)

	// Test that CLI version, AWF version, and AWMG version are present simultaneously
	compiler := NewCompilerWithVersion("2.0.0-beta.5")
	registry := GetGlobalEngineRegistry()
	engine, err := registry.GetEngine("copilot")
	if err != nil {
		t.Fatalf("Failed to get copilot engine: %v", err)
	}

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
				Version: "v0.5.0",
			},
		},
		SandboxConfig: &SandboxConfig{
			MCP: &MCPGatewayRuntimeConfig{
				Version: "v0.0.12",
			},
		},
	}

	var yaml strings.Builder
	compiler.generateCreateAwInfo(&yaml, workflowData, engine)
	output := yaml.String()

	// Check for cli_version
	expectedCLILine := `cli_version: "2.0.0-beta.5"`
	if !strings.Contains(output, expectedCLILine) {
		t.Errorf("Expected output to contain cli_version '%s', got:\n%s", expectedCLILine, output)
	}

	// Check for awf_version
	expectedAwfLine := `awf_version: "v0.5.0"`
	if !strings.Contains(output, expectedAwfLine) {
		t.Errorf("Expected output to contain awf_version '%s', got:\n%s", expectedAwfLine, output)
	}

	// Check for awmg_version
	expectedAwmgLine := `awmg_version: "v0.0.12"`
	if !strings.Contains(output, expectedAwmgLine) {
		t.Errorf("Expected output to contain awmg_version '%s', got:\n%s", expectedAwmgLine, output)
	}
}
