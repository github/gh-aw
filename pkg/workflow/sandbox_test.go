//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSRTConfigJSON(t *testing.T) {
	tests := []struct {
		name                string
		workflowData        *WorkflowData
		expectError         bool
		expectAllowedDomain string // Check if a specific domain is in allowedDomains
		expectFilesystemSet bool   // Check if filesystem config is set
	}{
		{
			name:         "nil workflow data returns error",
			workflowData: nil,
			expectError:  true,
		},
		{
			name: "nil sandbox config returns error",
			workflowData: &WorkflowData{
				SandboxConfig: nil,
			},
			expectError: true,
		},
		{
			name: "basic sandbox config with default network",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeSRT,
					},
				},
			},
			expectError:         false,
			expectAllowedDomain: "api.enterprise.githubcopilot.com", // Default Copilot domain
			expectFilesystemSet: true,
		},
		{
			name: "sandbox config uses top-level network permissions",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeSRT,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Allowed: []string{"example.com"},
				},
			},
			expectError:         false,
			expectAllowedDomain: "example.com",
			expectFilesystemSet: true,
		},
		{
			name: "custom filesystem config is applied",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeSRT,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{
								AllowWrite: []string{"/custom/path"},
								DenyRead:   []string{"/secret"},
							},
						},
					},
				},
			},
			expectError:         false,
			expectFilesystemSet: true,
		},
		{
			name: "legacy sandbox config format",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Type: SandboxTypeRuntime,
					Config: &SandboxRuntimeConfig{
						Filesystem: &SRTFilesystemConfig{
							AllowWrite: []string{"/legacy/path"},
						},
					},
				},
			},
			expectError:         false,
			expectFilesystemSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generateSRTConfigJSON(tt.workflowData)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, result)

			// Parse the JSON to verify structure
			var config SandboxRuntimeConfig
			err = json.Unmarshal([]byte(result), &config)
			require.NoError(t, err, "Generated JSON should be valid")

			if tt.expectAllowedDomain != "" {
				require.NotNil(t, config.Network, "Network config should be set")
				assert.Contains(t, config.Network.AllowedDomains, tt.expectAllowedDomain)
			}

			if tt.expectFilesystemSet {
				require.NotNil(t, config.Filesystem, "Filesystem config should be set")
			}
		})
	}
}

func TestSandboxNetworkConfigFromTopLevelField(t *testing.T) {
	// This test verifies that network config comes from top-level network field,
	// NOT from sandbox.agent.config.network
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeSRT,
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Allowed: []string{"custom-domain.example.com"},
		},
	}

	result, err := generateSRTConfigJSON(workflowData)
	require.NoError(t, err)

	var config SandboxRuntimeConfig
	err = json.Unmarshal([]byte(result), &config)
	require.NoError(t, err)

	// Network config should include the custom domain from top-level network field
	require.NotNil(t, config.Network)
	assert.Contains(t, config.Network.AllowedDomains, "custom-domain.example.com")
}

func TestIsSRTEnabled(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil workflow data",
			data:     nil,
			expected: false,
		},
		{
			name: "nil sandbox config",
			data: &WorkflowData{
				SandboxConfig: nil,
			},
			expected: false,
		},
		{
			name: "agent type AWF",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeAWF},
				},
			},
			expected: false,
		},
		{
			name: "agent type SRT",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeSRT},
				},
			},
			expected: true,
		},
		{
			name: "agent type sandbox-runtime (legacy)",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeRuntime},
				},
			},
			expected: true,
		},
		{
			name: "legacy type sandbox-runtime",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Type: SandboxTypeRuntime,
				},
			},
			expected: true,
		},
		{
			name: "legacy type srt",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Type: SandboxTypeSRT,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSRTEnabled(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateSandboxConfig(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil workflow data is valid",
			data:        nil,
			expectError: false,
		},
		{
			name: "nil sandbox config is valid",
			data: &WorkflowData{
				SandboxConfig: nil,
			},
			expectError: false,
		},
		{
			name: "SRT without feature flag fails",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeSRT},
				},
				EngineConfig: &EngineConfig{ID: "copilot"},
				Features:     map[string]any{},
			},
			expectError: true,
			errorMsg:    "sandbox-runtime feature is experimental",
		},
		{
			name: "SRT with feature flag succeeds",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeSRT},
				},
				EngineConfig: &EngineConfig{ID: "copilot"},
				Features:     map[string]any{"sandbox-runtime": true},
				Tools: map[string]any{
					"github": map[string]any{}, // Add MCP server to satisfy validation
				},
			},
			expectError: false,
		},
		{
			name: "SRT with non-copilot engine fails",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeSRT},
				},
				EngineConfig: &EngineConfig{ID: "claude"},
				Features:     map[string]any{"sandbox-runtime": true},
			},
			expectError: true,
			errorMsg:    "sandbox-runtime is only supported with Copilot engine",
		},
		{
			name: "SRT with AWF firewall fails",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeSRT},
				},
				EngineConfig: &EngineConfig{ID: "copilot"},
				Features:     map[string]any{"sandbox-runtime": true},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
			expectError: true,
			errorMsg:    "sandbox-runtime and AWF firewall cannot be used together",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxConfig(tt.data)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSandboxCompilationWithFilesystemConfig(t *testing.T) {
	content := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    type: srt
    config:
      filesystem:
        allowWrite:
          - "."
          - "/tmp"
          - "/custom/path"
        denyRead:
          - "/etc/passwd"
      enableWeakerNestedSandbox: true
features:
  sandbox-runtime: true
permissions:
  contents: read
---

# Test Workflow
`

	tmpDir := testutil.TempDir(t, "sandbox-filesystem-test")

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err)

	// Verify the lock file was created
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	_, err = os.Stat(lockFile)
	require.NoError(t, err, "Lock file should be created")
}

func TestSandboxCompilationWithNetworkViaTopLevel(t *testing.T) {
	// This test verifies that network config comes from top-level network field
	// Note: SRT is incompatible with AWF firewall, so we use sandbox: awf
	// and network.firewall: true to test network permissions separately
	content := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent: awf
network:
  allowed:
    - "api.example.com"
    - python
  firewall: true
permissions:
  contents: read
---

# Test Workflow with network from top-level field
`

	tmpDir := testutil.TempDir(t, "sandbox-network-toplevel-test")

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err)

	// Verify the lock file was created
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	_, err = os.Stat(lockFile)
	require.NoError(t, err, "Lock file should be created")
}
