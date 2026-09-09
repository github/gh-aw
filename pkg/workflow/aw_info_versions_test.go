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

			compiler := NewCompiler(WithVersion(tt.cliVersion))
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

			expectedLine := `GH_AW_INFO_CLI_VERSION: "` + tt.cliVersion + `"`
			containsVersion := strings.Contains(output, expectedLine)

			if tt.shouldInclude {
				if !containsVersion {
					t.Errorf("%s: Expected output to contain '%s', got:\n%s",
						tt.description, expectedLine, output)
				}
			} else {
				// For dev builds, cli_version should not appear at all
				if strings.Contains(output, "GH_AW_INFO_CLI_VERSION:") {
					t.Errorf("%s: Expected output to NOT contain 'GH_AW_INFO_CLI_VERSION:' field, got:\n%s",
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
		agentVersion       string
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
			agentVersion:       "",
			expectedAwfVersion: "",
			description:        "Should have empty awf_version when firewall is disabled",
		},
		{
			name:               "sandbox.agent.version overrides firewall version",
			firewallEnabled:    true,
			firewallVersion:    "",
			agentVersion:       "v0.30.1",
			expectedAwfVersion: "v0.30.1",
			description:        "Should prefer sandbox.agent.version override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion("1.0.0"))
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
			if tt.agentVersion != "" {
				workflowData.SandboxConfig = &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type:    SandboxTypeAWF,
						Version: tt.agentVersion,
					},
				}
			}

			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			expectedLine := `GH_AW_INFO_AWF_VERSION: "` + tt.expectedAwfVersion + `"`
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
	compiler := NewCompiler(WithVersion("2.0.0-beta.5"))
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
	expectedCLILine := `GH_AW_INFO_CLI_VERSION: "2.0.0-beta.5"`
	if !strings.Contains(output, expectedCLILine) {
		t.Errorf("Expected output to contain cli_version '%s', got:\n%s", expectedCLILine, output)
	}

	// Check for awf_version
	expectedAwfLine := `GH_AW_INFO_AWF_VERSION: "v0.5.0"`
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
		{
			name:                "Dynamic enclave without explicit MCP Gateway version",
			mcpGatewayVersion:   "",
			expectedAwmgVersion: string(constants.DefaultMCPGatewayVersion),
			description:         "Should use the default MCP gateway version when it satisfies the dynamic delegation minimum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion("1.0.0"))
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine("copilot")
			if err != nil {
				t.Fatalf("Failed to get copilot engine: %v", err)
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
			}
			if tt.name == "Dynamic enclave without explicit MCP Gateway version" {
				workflowData = dynamicEnclaveWorkflowData()
				workflowData.SandboxConfig.MCP.Version = ""
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

			expectedLine := `GH_AW_INFO_AWMG_VERSION: "` + tt.expectedAwmgVersion + `"`
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
	compiler := NewCompiler(WithVersion("2.0.0-beta.5"))
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
	expectedCLILine := `GH_AW_INFO_CLI_VERSION: "2.0.0-beta.5"`
	if !strings.Contains(output, expectedCLILine) {
		t.Errorf("Expected output to contain cli_version '%s', got:\n%s", expectedCLILine, output)
	}

	// Check for awf_version
	expectedAwfLine := `GH_AW_INFO_AWF_VERSION: "v0.5.0"`
	if !strings.Contains(output, expectedAwfLine) {
		t.Errorf("Expected output to contain awf_version '%s', got:\n%s", expectedAwfLine, output)
	}

	// Check for awmg_version
	expectedAwmgLine := `GH_AW_INFO_AWMG_VERSION: "v0.0.12"`
	if !strings.Contains(output, expectedAwmgLine) {
		t.Errorf("Expected output to contain awmg_version '%s', got:\n%s", expectedAwmgLine, output)
	}
}

func TestFeaturesInAwInfo(t *testing.T) {
	tests := []struct {
		name          string
		features      map[string]any
		shouldInclude bool
	}{
		{
			name:          "features included when configured",
			features:      map[string]any{"gh-aw-detection": true, "mode": "strict"},
			shouldInclude: true,
		},
		{
			name:          "features omitted when empty",
			features:      map[string]any{},
			shouldInclude: false,
		},
		{
			name:          "features omitted when nil",
			features:      nil,
			shouldInclude: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion("1.0.0"))
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine("copilot")
			if err != nil {
				t.Fatalf("Failed to get copilot engine: %v", err)
			}

			workflowData := &WorkflowData{
				Name:     "Test Workflow",
				Features: tt.features,
			}

			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			if tt.shouldInclude {
				if !strings.Contains(output, "GH_AW_INFO_FEATURES:") {
					t.Fatalf("Expected output to contain GH_AW_INFO_FEATURES, got:\n%s", output)
				}
				if !strings.Contains(output, `"gh-aw-detection":true`) {
					t.Errorf("Expected output to include boolean feature value, got:\n%s", output)
				}
				if !strings.Contains(output, `"mode":"strict"`) {
					t.Errorf("Expected output to include string feature value, got:\n%s", output)
				}
			} else if strings.Contains(output, "GH_AW_INFO_FEATURES:") {
				t.Errorf("Expected output to omit GH_AW_INFO_FEATURES, got:\n%s", output)
			}
		})
	}
}
