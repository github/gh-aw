//go:build !integration

package workflow

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGitHubCLIProxyVersion(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{"mode": "gh-proxy"},
		},
	}
	require.NoError(t, validateGitHubCLIProxyVersion(workflowData))

	workflowData.NetworkPermissions = &NetworkPermissions{
		Firewall: &FirewallConfig{Enabled: true},
	}
	require.NoError(t, validateGitHubCLIProxyVersion(workflowData))

	workflowData.NetworkPermissions.Firewall.Version = "v0.28.12"
	err := validateGitHubCLIProxyVersion(workflowData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tools.github.mode: gh-proxy requires AWF v0.28.13 or newer")
	assert.Contains(t, err.Error(), "gh issue list or gh pr list")

	workflowData.NetworkPermissions.Firewall.Version = "v0.28.13"
	require.NoError(t, validateGitHubCLIProxyVersion(workflowData))
}

// TestValidateExpressions tests expression safety and runtime-import validation.
func TestValidateExpressions(t *testing.T) {
	tests := []struct {
		name          string
		markdown      string
		shouldError   bool
		errorContains string
	}{
		{
			name:        "no expressions",
			markdown:    "# Hello\n\nNo expressions here.",
			shouldError: false,
		},
		{
			name:        "safe expression",
			markdown:    "# Hello\n\n${{ github.event.issue.number }}",
			shouldError: false,
		},
		{
			name:          "unsafe expression in markdown",
			markdown:      "# Hello\n\n${{ github.event.issue.body }}",
			shouldError:   true,
			errorContains: "unauthorized expressions found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "expr-test")
			markdownPath := filepath.Join(tmpDir, "test.md")

			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:            "Test",
				MarkdownContent: tt.markdown,
				AI:              "copilot",
			}

			err := compiler.validateExpressions(workflowData, markdownPath)
			if tt.shouldError {
				require.Error(t, err, "Expected validateExpressions to return an error")
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "validateExpressions should not return an error")
			}
		})
	}
}

// TestValidateFeatureConfig tests feature flag and action-mode validation.
func TestValidateFeatureConfig(t *testing.T) {
	tests := []struct {
		name          string
		features      map[string]any
		shouldError   bool
		errorContains string
	}{
		{
			name:        "no features",
			features:    nil,
			shouldError: false,
		},
		{
			name: "valid action-mode dev",
			features: map[string]any{
				"action-mode": "dev",
			},
			shouldError: false,
		},
		{
			name: "valid action-mode release",
			features: map[string]any{
				"action-mode": "release",
			},
			shouldError: false,
		},
		{
			name: "invalid action-mode",
			features: map[string]any{
				"action-mode": "invalid-mode",
			},
			shouldError:   true,
			errorContains: "invalid action-mode feature flag",
		},
		{
			name: "empty action-mode is ignored",
			features: map[string]any{
				"action-mode": "",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "feature-test")
			markdownPath := filepath.Join(tmpDir, "test.md")

			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Features:        tt.features,
			}

			err := compiler.validateFeatureConfig(workflowData, markdownPath)
			if tt.shouldError {
				require.Error(t, err, "Expected validateFeatureConfig to return an error")
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "validateFeatureConfig should not return an error")
			}
		})
	}
}

func TestEmitExperimentalFeatureWarningsGHAWDetection(t *testing.T) {
	t.Setenv("GH_AW_FEATURES", "")
	tests := []struct {
		name          string
		features      map[string]any
		batchMode     bool
		expectWarning bool
		expectedUsage int
	}{
		{
			name: "gh-aw-detection enabled produces experimental warning",
			features: map[string]any{
				"gh-aw-detection": true,
			},
			expectWarning: true,
		},
		{
			name: "batch mode aggregates gh-aw-detection usage",
			features: map[string]any{
				"gh-aw-detection": true,
			},
			batchMode:     true,
			expectWarning: false,
			expectedUsage: 1,
		},
		{
			name: "gh-aw-detection disabled does not produce experimental warning",
			features: map[string]any{
				"gh-aw-detection": false,
			},
			expectWarning: false,
		},
		{
			name:          "no gh-aw-detection does not produce experimental warning",
			features:      nil,
			expectWarning: false,
		},
	}

	expectedMessage := "Using experimental feature: gh-aw-detection"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetBatchMode(tt.batchMode)
			workflowData := &WorkflowData{
				Features: tt.features,
			}

			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = w
			t.Cleanup(func() {
				os.Stderr = oldStderr
				_ = r.Close()
			})

			compiler.emitExperimentalFeatureWarnings(workflowData)

			require.NoError(t, w.Close())
			os.Stderr = oldStderr
			var buf bytes.Buffer
			_, err = io.Copy(&buf, r)
			require.NoError(t, err)
			stderrOutput := buf.String()

			if tt.expectWarning {
				assert.Contains(t, stderrOutput, expectedMessage)
				assert.Positive(t, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, stderrOutput, expectedMessage)
				if tt.expectedUsage == 0 {
					assert.Zero(t, compiler.GetWarningCount())
				}
			}
			assert.Equal(t, tt.expectedUsage, compiler.GetExperimentalFeatureUsage()[expectedMessage])
		})
	}
}

func TestEmitGeneralToolWarningsCloudHypervisorReviewTrigger(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Runtime: AgentRuntimeCloudHypervisor,
			},
		},
	}

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = r.Close()
	})

	compiler.emitGeneralToolWarnings(workflowData, "test.md")

	require.NoError(t, w.Close())
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	stderrOutput := buf.String()

	assert.Contains(t, stderrOutput, "sandbox.agent.runtime: cloud-hypervisor uses a privileged KVM preview path")
	assert.Contains(t, stderrOutput, "Require a human security review before merge or rollout")
	assert.Equal(t, 1, compiler.GetWarningCount())
}

func TestEmitGeneralToolWarningsDeprecatedSandboxRuntimes(t *testing.T) {
	tests := []struct {
		name            string
		agent           *AgentSandboxConfig
		expectedMessage string
	}{
		{
			name:            "gvisor runtime",
			agent:           &AgentSandboxConfig{Runtime: AgentRuntimeGVisor},
			expectedMessage: "sandbox.agent.runtime: gvisor is deprecated and will be removed in a future release",
		},
		{
			name:            "docker-sbx runtime",
			agent:           &AgentSandboxConfig{Runtime: AgentRuntimeDockerSbx},
			expectedMessage: "sandbox.agent.runtime: docker-sbx is deprecated and will be removed in a future release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				SandboxConfig: &SandboxConfig{Agent: tt.agent},
			}

			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = w
			t.Cleanup(func() {
				os.Stderr = oldStderr
				_ = r.Close()
			})

			compiler.emitGeneralToolWarnings(workflowData, "test.md")

			require.NoError(t, w.Close())
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, err = io.Copy(&buf, r)
			require.NoError(t, err)
			assert.Contains(t, buf.String(), tt.expectedMessage)
			assert.Equal(t, 1, compiler.GetWarningCount())
		})
	}
}

func TestEmitGeneralToolWarningsIgnoredFilesystemAllowWrite(t *testing.T) {
	tests := []struct {
		name          string
		runtime       AgentRuntime
		expectWarning bool
	}{
		{name: "docker runtime warns", runtime: AgentRuntimeDocker, expectWarning: true},
		{name: "gvisor runtime warns", runtime: AgentRuntimeGVisor, expectWarning: true},
		{name: "default runtime warns", runtime: "", expectWarning: true},
		{name: "cloud-hypervisor runtime does not warn", runtime: AgentRuntimeCloudHypervisor, expectWarning: false},
	}

	const expectedMessage = "sandbox.agent.config.filesystem.allowWrite is ignored for this runtime"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Runtime: tt.runtime,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{AllowWrite: []string{"/tmp/gh-aw/agent"}},
						},
					},
				},
			}

			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = w
			t.Cleanup(func() {
				os.Stderr = oldStderr
				_ = r.Close()
			})

			compiler.emitGeneralToolWarnings(workflowData, "test.md")

			require.NoError(t, w.Close())
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, err = io.Copy(&buf, r)
			require.NoError(t, err)
			stderrOutput := buf.String()

			if tt.expectWarning {
				assert.Contains(t, stderrOutput, expectedMessage)
			} else {
				assert.NotContains(t, stderrOutput, expectedMessage)
			}
		})
	}
}

// TestValidatePermissions tests permission parsing and MCP tool constraint validation.
func TestValidatePermissions(t *testing.T) {
	tests := []struct {
		name            string
		workflowData    *WorkflowData
		strictMode      bool
		shouldError     bool
		errorContains   string
		wantPermissions bool // whether the returned *Permissions should be non-nil
	}{
		{
			name: "no permissions returns empty Permissions",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "",
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "valid permissions parses successfully",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n",
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "engine auth github-oidc requires id-token write",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n",
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Auth: &EngineAuthConfig{
						Type: "github-oidc",
					},
				},
			},
			shouldError:     true,
			errorContains:   "engine.auth.type: github-oidc requires permissions.id-token: write",
			wantPermissions: false,
		},
		{
			name: "engine auth github-oidc with id-token write succeeds",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n  id-token: write\n",
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Auth: &EngineAuthConfig{
						Type: "github-oidc",
					},
				},
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "observability otlp github-app requires id-token write",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n",
				RawFrontmatter: map[string]any{
					"observability": map[string]any{
						"otlp": map[string]any{
							"github-app": map[string]any{},
						},
					},
				},
			},
			shouldError:     true,
			errorContains:   "observability.otlp.github-app requires permissions.id-token: write",
			wantPermissions: false,
		},
		{
			name: "observability otlp github-app with id-token write succeeds",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n  id-token: write\n",
				RawFrontmatter: map[string]any{
					"observability": map[string]any{
						"otlp": map[string]any{
							"github-app": map[string]any{},
						},
					},
				},
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "http mcp github-oidc requires id-token write",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n",
				Tools: map[string]any{
					"oidc-server": map[string]any{
						"type": "http",
						"url":  "https://my-server.example.com/mcp",
						"auth": map[string]any{
							"type": "github-oidc",
						},
					},
				},
			},
			shouldError:     true,
			errorContains:   "mcp-servers.<name>.auth.type: github-oidc requires permissions.id-token: write",
			wantPermissions: false,
		},
		{
			name: "http mcp github-oidc with id-token write succeeds",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n  id-token: write\n",
				Tools: map[string]any{
					"oidc-server": map[string]any{
						"type": "http",
						"url":  "https://my-server.example.com/mcp",
						"auth": map[string]any{
							"type":     "github-oidc",
							"audience": "https://my-server.example.com",
						},
					},
				},
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "http mcp github-oidc with legacy AWF version is rejected",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n  id-token: write\n",
				Tools: map[string]any{
					"oidc-server": map[string]any{
						"type": "http",
						"url":  "https://my-server.example.com/mcp",
						"auth": map[string]any{
							"type": "github-oidc",
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.25.2", // older than AWFExcludeEnvMinVersion (v0.25.3)
					},
				},
			},
			shouldError:     true,
			errorContains:   "mcp-servers.<name>.auth.type: github-oidc requires AWF v0.25.3 or newer",
			wantPermissions: false,
		},
		{
			name: "http mcp github-oidc with minimum required AWF version succeeds",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n  id-token: write\n",
				Tools: map[string]any{
					"oidc-server": map[string]any{
						"type": "http",
						"url":  "https://my-server.example.com/mcp",
						"auth": map[string]any{
							"type": "github-oidc",
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.25.3", // exactly AWFExcludeEnvMinVersion
					},
				},
			},
			shouldError:     false,
			wantPermissions: true,
		},
		{
			name: "observability otlp GitHub App credentials do not require id-token write",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Permissions:     "permissions:\n  contents: read\n",
				RawFrontmatter: map[string]any{
					"observability": map[string]any{
						"otlp": map[string]any{
							"github-app": map[string]any{
								"app-id":      "${{ vars.APP_ID }}",
								"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
							},
						},
					},
				},
			},
			shouldError:     false,
			wantPermissions: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "perms-test")
			markdownPath := filepath.Join(tmpDir, "test.md")

			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			perms, err := compiler.validatePermissions(tt.workflowData, markdownPath)
			if tt.shouldError {
				require.Error(t, err, "Expected validatePermissions to return an error")
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "Error should contain expected message")
				}
			} else {
				require.NoError(t, err, "validatePermissions should not return an error")
				if tt.wantPermissions {
					assert.NotNil(t, perms, "validatePermissions should return a non-nil *Permissions")
				}
			}
		})
	}
}

// TestValidateToolConfiguration tests safe-outputs, GitHub tools, and dispatch validation.
func TestValidateToolConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		permissions   string // raw permissions YAML to parse
		shouldError   bool
		errorContains string
	}{
		{
			name: "minimal workflow passes",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
			},
			permissions: "",
			shouldError: false,
		},
		{
			name: "agentic-workflows tool requires actions read permission",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Tools: map[string]any{
					"agentic-workflows": map[string]any{},
				},
				Permissions: "",
			},
			permissions:   "",
			shouldError:   true,
			errorContains: "Missing required permission for agentic-workflows tool",
		},
		{
			name: "agentic-workflows tool with actions read succeeds",
			workflowData: &WorkflowData{
				Name:            "Test",
				MarkdownContent: "# Test",
				AI:              "copilot",
				Tools: map[string]any{
					"agentic-workflows": map[string]any{},
				},
				Permissions: "permissions:\n  actions: read\n",
			},
			permissions: "permissions:\n  actions: read\n",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "tool-test")
			markdownPath := filepath.Join(tmpDir, "test.md")

			compiler := NewCompiler()
			parsedPermissions := NewPermissionsParser(tt.permissions).ToPermissions()

			err := compiler.validateToolConfiguration(tt.workflowData, markdownPath, parsedPermissions)
			if tt.shouldError {
				require.Error(t, err, "Expected validateToolConfiguration to return an error")
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "validateToolConfiguration should not return an error")
			}
		})
	}
}

func TestValidatePermissions_UsesCachedPermissionScopeValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "perms-cache-test")
	markdownPath := filepath.Join(tmpDir, "test.md")

	cachedErr := errors.New("cached permission scope validation failure")
	workflowData := &WorkflowData{
		Name:                          "Test",
		MarkdownContent:               "# Test",
		AI:                            "copilot",
		Permissions:                   "permissions:\n  contents: read\n",
		CachedPermissionScopeNamesSet: true,
		CachedPermissionScopeNamesErr: cachedErr,
	}

	compiler := NewCompiler()
	_, err := compiler.validatePermissions(workflowData, markdownPath)
	require.Error(t, err)
	require.ErrorContains(t, err, cachedErr.Error())
}

func TestValidatePermissions_EmitsCopilotRequestsTipOncePerMarkdownPath(t *testing.T) {
	tmpDir := testutil.TempDir(t, "perms-tip-once-test")
	markdownPath := filepath.Join(tmpDir, "test.md")

	workflowData := &WorkflowData{
		Name:        "Test",
		AI:          "copilot",
		Permissions: "permissions:\n  contents: read\n",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}

	compiler := NewCompiler()

	stderr := testutil.CaptureStderr(t, func() {
		_, err := compiler.validatePermissions(workflowData, markdownPath)
		require.NoError(t, err)
		_, err = compiler.validatePermissions(workflowData, markdownPath)
		require.NoError(t, err)
	})

	const tipText = "Tip: set permissions.copilot-requests: write to use GitHub Actions token-based inference"
	assert.Equal(t, 1, strings.Count(stderr, tipText), "copilot-requests tip should be emitted only once per markdown path")
	assert.Contains(t, stderr, "To suppress this tip when using a PAT, set permissions.copilot-requests: none")
}

func TestValidatePermissions_QuietSuppressesCopilotRequestsTip(t *testing.T) {
	workflowData := &WorkflowData{
		Name:        "Test",
		AI:          "copilot",
		Permissions: "permissions:\n  contents: read\n",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}
	compiler := NewCompiler()
	compiler.SetQuiet(true)

	stderr := testutil.CaptureStderr(t, func() {
		_, err := compiler.validatePermissions(workflowData, "test.md")
		require.NoError(t, err)
	})

	assert.NotContains(t, stderr, "Tip: set permissions.copilot-requests: write")
}

func TestEmitGeneralToolWarnings_PiThreatDetectionAuthWarning(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		wantWarning bool
	}{
		{
			name: "warns without Copilot authentication",
			data: &WorkflowData{
				AI:           "pi",
				EngineConfig: &EngineConfig{ID: "pi"},
				Permissions:  "permissions:\n  contents: read\n",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			wantWarning: true,
		},
		{
			name: "does not warn with copilot requests permission",
			data: &WorkflowData{
				AI:           "pi",
				EngineConfig: &EngineConfig{ID: "pi"},
				Permissions:  "permissions:\n  contents: read\n  copilot-requests: write\n",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
		},
		{
			name: "does not warn with explicit Copilot token",
			data: &WorkflowData{
				AI: "pi",
				EngineConfig: &EngineConfig{
					ID: "pi",
					Env: map[string]string{
						"COPILOT_GITHUB_TOKEN": "${{ secrets.DETECTION_COPILOT_TOKEN }}",
					},
				},
				Permissions: "permissions:\n  contents: read\n",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
		},
		{
			name: "does not warn with Copilot BYOK credentials",
			data: &WorkflowData{
				AI: "pi",
				EngineConfig: &EngineConfig{
					ID: "pi",
				},
				Permissions: "permissions:\n  contents: read\n",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							Env: map[string]string{
								constants.CopilotProviderBaseURL:     "https://provider.example.com/v1",
								constants.CopilotProviderAPIKey:      "${{ secrets.PROVIDER_API_KEY }}",
								constants.CopilotProviderBearerToken: "",
							},
						},
					},
				},
			},
		},
		{
			name: "does not warn when threat detection is disabled",
			data: &WorkflowData{
				AI:           "pi",
				EngineConfig: &EngineConfig{ID: "pi"},
				Permissions:  "permissions:\n  contents: read\n",
			},
		},
		{
			name: "does not warn for a non-Copilot detection override",
			data: &WorkflowData{
				AI:           "pi",
				EngineConfig: &EngineConfig{ID: "pi"},
				Permissions:  "permissions:\n  contents: read\n",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{ID: "codex"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			stderr := testutil.CaptureStderr(t, func() {
				compiler.emitGeneralToolWarnings(tt.data, "test.md")
			})

			const warning = "Threat detection for engine: pi runs on the GitHub Copilot CLI."
			if tt.wantWarning {
				assert.Contains(t, stderr, warning)
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, stderr, warning)
				assert.Zero(t, compiler.GetWarningCount())
			}
		})
	}
}

func TestShouldEmitCopilotRequestsEnableTip(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		perms    *Permissions
		expected bool
	}{
		{
			name:     "nil workflow data",
			data:     nil,
			perms:    NewPermissions(),
			expected: false,
		},
		{
			name: "non copilot engine",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "claude"},
			},
			perms:    NewPermissions(),
			expected: false,
		},
		{
			name: "copilot engine without permission emits tip",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			perms:    NewPermissionsContentsRead(),
			expected: true,
		},
		{
			name: "copilot engine with permission suppresses tip",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			perms: func() *Permissions {
				p := NewPermissionsContentsRead()
				p.Set(PermissionCopilotRequests, PermissionWrite)
				return p
			}(),
			expected: false,
		},
		{
			name: "explicit none suppresses tip",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			perms: func() *Permissions {
				p := NewPermissionsContentsRead()
				p.Set(PermissionCopilotRequests, PermissionNone)
				return p
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEmitCopilotRequestsEnableTip(tt.data, tt.perms)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestValidateToolConfiguration_DoesNotWarnForDisabledSandbox(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tool-warning-test")
	markdownPath := filepath.Join(tmpDir, "test.md")

	compiler := NewCompiler()
	compiler.SetStrictMode(false)

	workflowData := &WorkflowData{
		Name: "Test",
		Features: map[string]any{
			"dangerously-disable-sandbox-agent": true,
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Disabled: true},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	initialWarnings := compiler.GetWarningCount()
	var validateErr error
	stderr := testutil.CaptureStderr(t, func() {
		validateErr = compiler.validateToolConfiguration(workflowData, markdownPath, &Permissions{})
	})

	require.Error(t, validateErr)
	require.ErrorContains(t, validateErr, "threat detection requires sandbox.agent")
	assert.NotContains(t, stderr, "sandbox.agent: false")
	assert.Equal(t, initialWarnings, compiler.GetWarningCount())
}

// TestWarnPromptTmpPaths tests the /tmp path heuristic used by the compiler.
func TestWarnPromptTmpPaths(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name:          "no tmp reference",
			content:       "# Workflow\n\nDo some work.",
			expectWarning: false,
		},
		{
			name:          "correct path /tmp/gh-aw/agent/ only",
			content:       "Store output in /tmp/gh-aw/agent/result.txt",
			expectWarning: false,
		},
		{
			name:          "correct path /tmp/gh-aw/agent/ subdirectory",
			content:       "Write to /tmp/gh-aw/agent/logs/output.log for later inspection.",
			expectWarning: false,
		},
		{
			name:          "bare /tmp/ reference",
			content:       "Save the file to /tmp/myfile.txt",
			expectWarning: true,
		},
		{
			name:          "/tmp/gh-aw/ subpath is safe",
			content:       "Write your output to /tmp/gh-aw/result.json",
			expectWarning: false,
		},
		{
			name:          "/tmp/ root only",
			content:       "Use /tmp/ for temporary storage.",
			expectWarning: true,
		},
		{
			name:          "mix of correct and raw /tmp/ reference",
			content:       "Prefer /tmp/gh-aw/agent/ but avoid plain /tmp/foo paths.",
			expectWarning: true,
		},
		{
			name:          "cache-memory path is safe",
			content:       "Read state from /tmp/gh-aw/cache-memory/my-workflow/state.json",
			expectWarning: false,
		},
		{
			name:          "named cache-memory path is safe",
			content:       "Use /tmp/gh-aw/cache-memory-focus-areas/ for storage.",
			expectWarning: false,
		},
		{
			name:          "repo-memory path is safe",
			content:       "Access shared memory at /tmp/gh-aw/repo-memory/default/metrics.json",
			expectWarning: false,
		},
		{
			name:          "comment-memory path is safe",
			content:       "Append haiku to /tmp/gh-aw/comment-memory/notes.md",
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := warnPromptTmpPaths(tt.content)
			if tt.expectWarning {
				assert.NotEmpty(t, msg, "expected a warning message")
				assert.Contains(t, msg, "/tmp/gh-aw/agent/", "warning should suggest /tmp/gh-aw/agent/")
			} else {
				assert.Empty(t, msg, "expected no warning message")
			}
		})
	}
}

// TestValidatePromptTmpPaths tests that validatePromptTmpPaths increments the
// warning counter and emits a message when the markdown body has problematic /tmp paths.
func TestValidatePromptTmpPaths(t *testing.T) {
	tests := []struct {
		name       string
		markdown   string
		expectWarn bool
	}{
		{
			name:       "no tmp reference — no warning",
			markdown:   "# Hello\n\nDo some work.",
			expectWarn: false,
		},
		{
			name:       "correct /tmp/gh-aw/agent/ — no warning",
			markdown:   "Store results in /tmp/gh-aw/agent/output.txt",
			expectWarn: false,
		},
		{
			name:       "bare /tmp/ reference — warning",
			markdown:   "Save artifacts to /tmp/data.json",
			expectWarn: true,
		},
		{
			name:       "/tmp/gh-aw/ subpath — no warning",
			markdown:   "Write summary to /tmp/gh-aw/summary.txt",
			expectWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "tmp-path-test")
			markdownPath := filepath.Join(tmpDir, "workflow.md")

			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:            "Test",
				MarkdownContent: tt.markdown,
				AI:              "copilot",
			}

			before := compiler.GetWarningCount()
			compiler.validatePromptTmpPaths(workflowData, markdownPath)
			after := compiler.GetWarningCount()

			if tt.expectWarn {
				assert.Greater(t, after, before, "warning count should have increased")
			} else {
				assert.Equal(t, before, after, "warning count should not have changed")
			}
		})
	}
}
