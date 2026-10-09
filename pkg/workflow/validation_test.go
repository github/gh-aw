//go:build integration

package workflow

import (
	"os/exec"
	"strings"
	"testing"
)

const validationAlpineImage = "mirror.gcr.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6"

func TestValidateContainerImages(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		expectError    bool
		errorContains  string
		skipIfNoDocker bool
	}{
		{
			name: "no tools",
			workflowData: &WorkflowData{
				Tools: nil,
			},
			expectError: false,
		},
		{
			name: "tools without container",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"command": "npx",
						"args":    []any{"@github/github-mcp-server"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid container image",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"test-tool": map[string]any{
						"container": validationAlpineImage,
					},
				},
			},
			expectError:    false,
			skipIfNoDocker: true,
		},
		{
			name: "invalid container image",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"test-tool": map[string]any{
						"container": "mirror.gcr.io/library/alpine",
						"version":   "nonexistent-image-that-should-not-exist-12345",
					},
				},
			},
			expectError:    true,
			errorContains:  "manifest unknown",
			skipIfNoDocker: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip test if docker daemon is not running
			if tt.skipIfNoDocker {
				if _, err := exec.LookPath("docker"); err != nil {
					t.Skip("docker not available, skipping test")
				}
				// Also check if Docker daemon is running (not just if binary exists)
				if !isDockerDaemonRunning() {
					t.Skip("docker daemon not running, skipping test")
				}
			}

			compiler := NewCompiler()
			err := compiler.validateContainerImages(tt.workflowData)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if err != nil && tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestValidateDockerImage(t *testing.T) {
	// Skip if docker is not available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping test")
	}
	// Also check if Docker daemon is running (not just if binary exists)
	if !isDockerDaemonRunning() {
		t.Skip("docker daemon not running, skipping test")
	}

	tests := []struct {
		name          string
		image         string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid image - alpine",
			image:       validationAlpineImage,
			expectError: false,
		},
		{
			name:          "invalid image",
			image:         "mirror.gcr.io/library/alpine:nonexistent-image-that-should-not-exist-12345",
			expectError:   true,
			errorContains: "manifest unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDockerImage(tt.image, false, false)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if err != nil && tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestExtractNpxPackages(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     []string
	}{
		{
			name: "no npx packages",
			workflowData: &WorkflowData{
				CustomSteps: "echo hello",
			},
			expected: []string{},
		},
		{
			name: "npx in custom steps",
			workflowData: &WorkflowData{
				CustomSteps: "npx @playwright/mcp@latest",
			},
			expected: []string{"@playwright/mcp@latest"},
		},
		{
			name: "npx in MCP config",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"playwright": map[string]any{
						"command": "npx",
						"args":    []any{"@playwright/mcp@latest"},
					},
				},
			},
			expected: []string{"@playwright/mcp@latest"},
		},
		{
			name: "multiple npx packages",
			workflowData: &WorkflowData{
				CustomSteps: "npx package1 && npx package2",
				Tools: map[string]any{
					"tool1": map[string]any{
						"command": "npx",
						"args":    []any{"package3"},
					},
				},
			},
			expected: []string{"package1", "package2", "package3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := extractNpxPackages(tt.workflowData)

			if len(packages) != len(tt.expected) {
				t.Errorf("expected %d packages, got %d: %v", len(tt.expected), len(packages), packages)
				return
			}

			// Check that all expected packages are present (order doesn't matter)
			expectedMap := make(map[string]bool)
			for _, pkg := range tt.expected {
				expectedMap[pkg] = true
			}

			for _, pkg := range packages {
				if !expectedMap[pkg] {
					t.Errorf("unexpected package: %s", pkg)
				}
			}
		})
	}
}

func TestExtractNpxFromCommands(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		expected []string
	}{
		{
			name:     "no npx",
			commands: "echo hello",
			expected: []string{},
		},
		{
			name:     "single npx",
			commands: "npx package-name",
			expected: []string{"package-name"},
		},
		{
			name:     "multiple npx with operators",
			commands: "npx pkg1 && npx pkg2 | npx pkg3",
			expected: []string{"pkg1", "pkg2", "pkg3"},
		},
		{
			name:     "npx with version specifier",
			commands: "npx @scope/package@1.0.0",
			expected: []string{"@scope/package@1.0.0"},
		},
		{
			name:     "npx with -y flag",
			commands: "npx -y @github/copilot@0.0.350",
			expected: []string{"@github/copilot@0.0.350"},
		},
		{
			name:     "npx with --yes flag",
			commands: "npx --yes package-name",
			expected: []string{"package-name"},
		},
		{
			name:     "npx with multiple flags",
			commands: "npx -y --quiet @scope/package",
			expected: []string{"@scope/package"},
		},
		{
			name:     "npx with flags and shell operators",
			commands: "npx -y pkg1 && npx --yes pkg2",
			expected: []string{"pkg1", "pkg2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := extractNpxFromCommands(tt.commands)

			if len(packages) != len(tt.expected) {
				t.Errorf("expected %d packages, got %d: %v", len(tt.expected), len(packages), packages)
				return
			}

			for i, pkg := range packages {
				if pkg != tt.expected[i] {
					t.Errorf("expected package[%d] = %s, got %s", i, tt.expected[i], pkg)
				}
			}
		})
	}
}

func TestValidateRuntimePackages(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expectError  bool
		skipReason   string
	}{
		{
			name: "no runtime packages",
			workflowData: &WorkflowData{
				CustomSteps: "echo hello",
			},
			expectError: false,
		},
		{
			name: "invalid mcp-scripts pip dependency name",
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"fetch-url": {
							Name:         "fetch-url",
							Description:  "Fetch URL",
							Py:           "print('ok')",
							Dependencies: []string{"re quests"},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "floating mcp-scripts pip dependency",
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"fetch-url": {
							Name:         "fetch-url",
							Description:  "Fetch URL",
							Py:           "print('ok')",
							Dependencies: []string{"requests"},
						},
					},
				},
			},
			expectError: true,
		},
		// Note: These tests would fail if npm/uv/pip are available, so we skip them
		// The actual validation logic is tested by the extraction tests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateRuntimePackages(tt.workflowData)

			// If we expect an error and got one, or don't expect one and didn't get one, test passes
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectError && err != nil && tt.name == "invalid mcp-scripts pip dependency name" {
				if !strings.Contains(err.Error(), `invalid dependency name "re quests" for tool "fetch-url"`) {
					t.Fatalf("expected invalid dependency error message, got: %v", err)
				}
			}
			if tt.expectError && err != nil && tt.name == "floating mcp-scripts pip dependency" {
				if !strings.Contains(err.Error(), `dependency "requests" for tool "fetch-url" is not pinned to a release tag`) {
					t.Fatalf("expected pinned dependency error message, got: %v", err)
				}
			}
		})
	}
}

func TestCollectPackagesFromWorkflow(t *testing.T) {
	// Mock extractor function that returns packages from commands
	mockExtractor := func(commands string) []string {
		if commands == "command1" {
			return []string{"pkg1", "pkg2"}
		}
		if commands == "command2" {
			return []string{"pkg2", "pkg3"}
		}
		return []string{}
	}

	tests := []struct {
		name         string
		workflowData *WorkflowData
		toolCommand  string
		expected     []string
	}{
		{
			name: "extract from custom steps only",
			workflowData: &WorkflowData{
				CustomSteps: "command1",
			},
			toolCommand: "",
			expected:    []string{"pkg1", "pkg2"},
		},
		{
			name: "extract from MCP tools when toolCommand provided",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"test-tool": map[string]any{
						"command": "test-cmd",
						"args":    []any{"pkg4"},
					},
				},
			},
			toolCommand: "test-cmd",
			expected:    []string{"pkg4"},
		},
		{
			name: "skip MCP tools when toolCommand is empty",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"test-tool": map[string]any{
						"command": "test-cmd",
						"args":    []any{"pkg4"},
					},
				},
			},
			toolCommand: "",
			expected:    []string{},
		},
		{
			name: "deduplicate across custom steps and MCP tools",
			workflowData: &WorkflowData{
				CustomSteps: "command1",
				Tools: map[string]any{
					"test-tool": map[string]any{
						"command": "test-cmd",
						"args":    []any{"pkg1"}, // Duplicate from custom steps
					},
				},
			},
			toolCommand: "test-cmd",
			expected:    []string{"pkg1", "pkg2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := collectPackagesFromWorkflow(tt.workflowData, mockExtractor, tt.toolCommand)

			if len(packages) != len(tt.expected) {
				t.Errorf("expected %d packages, got %d: %v", len(tt.expected), len(packages), packages)
				return
			}

			// Check that all expected packages are present (order doesn't matter)
			expectedMap := make(map[string]bool)
			for _, pkg := range tt.expected {
				expectedMap[pkg] = true
			}

			for _, pkg := range packages {
				if !expectedMap[pkg] {
					t.Errorf("unexpected package: %s", pkg)
				}
			}
		})
	}
}
