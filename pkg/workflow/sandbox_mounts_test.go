//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestValidateMountsSyntax tests the mount syntax validation function
func TestValidateMountsSyntax(t *testing.T) {
	tests := []struct {
		name    string
		mounts  []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid read-only mount",
			mounts:  []string{"/host/path:/container/path:ro"},
			wantErr: false,
		},
		{
			name:    "valid read-write mount",
			mounts:  []string{"/host/path:/container/path:rw"},
			wantErr: false,
		},
		{
			name: "multiple valid mounts",
			mounts: []string{
				"/host/data:/data:ro",
				"/usr/local/bin/tool:/usr/local/bin/tool:ro",
				"/tmp/cache:/cache:rw",
			},
			wantErr: false,
		},
		{
			name:    "empty mounts list",
			mounts:  []string{},
			wantErr: false,
		},
		{
			name:    "invalid mount - too few parts",
			mounts:  []string{"/host/path:/container/path"},
			wantErr: true,
			errMsg:  "mount syntax must follow 'source:destination:mode' format",
		},
		{
			name:    "invalid mount - too many parts",
			mounts:  []string{"/host/path:/container/path:ro:extra"},
			wantErr: true,
			errMsg:  "mount syntax must follow 'source:destination:mode' format",
		},
		{
			name:    "invalid mount - empty source",
			mounts:  []string{":/container/path:ro"},
			wantErr: true,
			errMsg:  "source path cannot be empty",
		},
		{
			name:    "invalid mount - empty destination",
			mounts:  []string{"/host/path::ro"},
			wantErr: true,
			errMsg:  "destination path cannot be empty",
		},
		{
			name:    "invalid mount - invalid mode",
			mounts:  []string{"/host/path:/container/path:xyz"},
			wantErr: true,
			errMsg:  "mode must be 'ro' (read-only) or 'rw' (read-write)",
		},
		{
			name:    "invalid mount - uppercase mode",
			mounts:  []string{"/host/path:/container/path:RO"},
			wantErr: true,
			errMsg:  "mode must be 'ro' (read-only) or 'rw' (read-write)",
		},
		{
			name: "mixed valid and invalid mounts",
			mounts: []string{
				"/host/data:/data:ro",
				"/invalid:mount",
			},
			wantErr: true,
			errMsg:  "mount syntax must follow 'source:destination:mode' format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMountsSyntax(tt.mounts)

			if tt.wantErr && err == nil {
				t.Errorf("validateMountsSyntax() expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateMountsSyntax() unexpected error: %v", err)
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateMountsSyntax() error message = %v, want to contain %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestSandboxConfigWithMounts tests that sandbox configuration with mounts is validated
func TestSandboxConfigWithMounts(t *testing.T) {
	tests := []struct {
		name    string
		data    *WorkflowData
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid mounts in agent config",
			data: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID: "awf",
						Mounts: []string{
							"/host/data:/data:ro",
							"/usr/local/bin/tool:/usr/local/bin/tool:ro",
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{}, // Add MCP server to satisfy validation
				},
			},
			wantErr: false,
		},
		{
			name: "no mounts in agent config",
			data: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID: "awf",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{}, // Add MCP server to satisfy validation
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mount syntax in agent config",
			data: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID: "awf",
						Mounts: []string{
							"/host/data:/data:ro",
							"/invalid:mount", // Invalid - only 2 parts
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
			},
			wantErr: true,
			errMsg:  "mount syntax must follow 'source:destination:mode' format",
		},
		{
			name: "invalid mode in mount",
			data: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID: "awf",
						Mounts: []string{
							"/host/data:/data:invalid",
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
			},
			wantErr: true,
			errMsg:  "mode must be 'ro' (read-only) or 'rw' (read-write)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxConfig(tt.data)

			if tt.wantErr && err == nil {
				t.Errorf("validateSandboxConfig() expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateSandboxConfig() unexpected error: %v", err)
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateSandboxConfig() error message = %v, want to contain %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestCopilotEngineWithCustomMounts tests that custom mounts are included in AWF command
func TestCopilotEngineWithCustomMounts(t *testing.T) {
	t.Run("custom mounts are included in AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
					Mounts: []string{
						"/host/data:/data:ro",
						"/usr/local/bin/custom-tool:/usr/local/bin/custom-tool:ro",
					},
				},
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

		// Check that custom mounts are included
		if !strings.Contains(stepContent, "--mount /host/data:/data:ro") {
			t.Error("Expected command to contain custom mount '--mount /host/data:/data:ro'")
		}

		if !strings.Contains(stepContent, "--mount /usr/local/bin/custom-tool:/usr/local/bin/custom-tool:ro") {
			t.Error("Expected command to contain custom mount '--mount /usr/local/bin/custom-tool:/usr/local/bin/custom-tool:ro'")
		}

		// Verify --enable-chroot is present (replaces standard mounts)
		if !strings.Contains(stepContent, "--enable-chroot") {
			t.Error("Expected command to contain '--enable-chroot' for transparent host access")
		}
	})

	t.Run("no custom mounts when not specified", func(t *testing.T) {
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
			t.Error("Expected command to contain '--enable-chroot' for transparent host access")
		}

		// Custom mount should not be present
		if strings.Contains(stepContent, "--mount /host/data:/data:ro") {
			t.Error("Did not expect custom mount in output when not configured")
		}
	})

	t.Run("custom mounts are sorted alphabetically", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
					Mounts: []string{
						"/var/log:/logs:ro",
						"/data:/data:rw",
						"/usr/bin/tool:/usr/bin/tool:ro",
						"/etc/config:/etc/config:ro",
					},
				},
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

		// Find the positions of each mount in the output
		dataPos := strings.Index(stepContent, "--mount /data:/data:rw")
		etcPos := strings.Index(stepContent, "--mount /etc/config:/etc/config:ro")
		usrPos := strings.Index(stepContent, "--mount /usr/bin/tool:/usr/bin/tool:ro")
		varPos := strings.Index(stepContent, "--mount /var/log:/logs:ro")

		// Verify all mounts are present
		if dataPos == -1 {
			t.Error("Expected to find mount '/data:/data:rw'")
		}
		if etcPos == -1 {
			t.Error("Expected to find mount '/etc/config:/etc/config:ro'")
		}
		if usrPos == -1 {
			t.Error("Expected to find mount '/usr/bin/tool:/usr/bin/tool:ro'")
		}
		if varPos == -1 {
			t.Error("Expected to find mount '/var/log:/logs:ro'")
		}

		// Verify mounts are in alphabetical order
		// Expected order: /data, /etc, /usr, /var
		if dataPos != -1 && etcPos != -1 && dataPos >= etcPos {
			t.Error("Expected '/data:/data:rw' to appear before '/etc/config:/etc/config:ro'")
		}
		if etcPos != -1 && usrPos != -1 && etcPos >= usrPos {
			t.Error("Expected '/etc/config:/etc/config:ro' to appear before '/usr/bin/tool:/usr/bin/tool:ro'")
		}
		if usrPos != -1 && varPos != -1 && usrPos >= varPos {
			t.Error("Expected '/usr/bin/tool:/usr/bin/tool:ro' to appear before '/var/log:/logs:ro'")
		}
	})
}
