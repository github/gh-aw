//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestAgentVersionInAwInfo(t *testing.T) {
	tests := []struct {
		name                 string
		engineID             string
		explicitVersion      string
		expectedAgentVersion string
		description          string
	}{
		{
			name:                 "Copilot with explicit version",
			engineID:             "copilot",
			explicitVersion:      "1.2.3",
			expectedAgentVersion: "1.2.3",
			description:          "Should use explicit version when provided",
		},
		{
			name:                 "Copilot with default version",
			engineID:             "copilot",
			explicitVersion:      "",
			expectedAgentVersion: string(constants.DefaultCopilotVersion),
			description:          "Should use default version when not provided",
		},
		{
			name:                 "Claude with explicit version",
			engineID:             "claude",
			explicitVersion:      "2.5.0",
			expectedAgentVersion: "2.5.0",
			description:          "Should use explicit version when provided",
		},
		{
			name:                 "Claude with default version",
			engineID:             "claude",
			explicitVersion:      "",
			expectedAgentVersion: string(constants.DefaultClaudeCodeVersion),
			description:          "Should use default version when not provided",
		},
		{
			name:                 "Codex with explicit version",
			engineID:             "codex",
			explicitVersion:      "0.60.0",
			expectedAgentVersion: "0.60.0",
			description:          "Should use explicit version when provided",
		},
		{
			name:                 "Codex with default version",
			engineID:             "codex",
			explicitVersion:      "",
			expectedAgentVersion: string(constants.DefaultCodexVersion),
			description:          "Should use default version when not provided",
		},
		{
			name:                 "Custom engine with explicit version",
			engineID:             "custom",
			explicitVersion:      "1.0.0",
			expectedAgentVersion: "1.0.0",
			description:          "Should use explicit version when provided",
		},
		{
			name:                 "Custom engine without version",
			engineID:             "custom",
			explicitVersion:      "",
			expectedAgentVersion: "",
			description:          "Should use empty string for custom engines without version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get %s engine: %v", tt.engineID, err)
			}

			// Create workflow data
			workflowData := &WorkflowData{
				Name: "Test Workflow",
			}

			// Set explicit version if provided
			if tt.explicitVersion != "" {
				workflowData.EngineConfig = &EngineConfig{
					ID:      tt.engineID,
					Version: tt.explicitVersion,
				}
			}

			// Generate aw-info
			var yaml strings.Builder
			compiler.generateCreateAwInfo(&yaml, workflowData, engine)
			output := yaml.String()

			// Check that agent_version is set correctly
			expectedLine := `agent_version: "` + tt.expectedAgentVersion + `"`
			if !strings.Contains(output, expectedLine) {
				t.Errorf("%s: Expected output to contain '%s', got:\n%s",
					tt.description, expectedLine, output)
			}

			// Also verify that the version field matches (for non-custom engines with defaults)
			if tt.explicitVersion != "" {
				expectedVersionLine := `version: "` + tt.explicitVersion + `"`
				if !strings.Contains(output, expectedVersionLine) {
					t.Errorf("Expected output to contain version '%s'", expectedVersionLine)
				}
			}
		})
	}
}

func TestGetInstallationVersion(t *testing.T) {
	tests := []struct {
		name            string
		engineID        string
		explicitVersion string
		expectedVersion string
	}{
		{
			name:            "Copilot with explicit version",
			engineID:        "copilot",
			explicitVersion: "1.2.3",
			expectedVersion: "1.2.3",
		},
		{
			name:            "Copilot without explicit version",
			engineID:        "copilot",
			explicitVersion: "",
			expectedVersion: string(constants.DefaultCopilotVersion),
		},
		{
			name:            "Claude with explicit version",
			engineID:        "claude",
			explicitVersion: "2.5.0",
			expectedVersion: "2.5.0",
		},
		{
			name:            "Claude without explicit version",
			engineID:        "claude",
			explicitVersion: "",
			expectedVersion: string(constants.DefaultClaudeCodeVersion),
		},
		{
			name:            "Codex with explicit version",
			engineID:        "codex",
			explicitVersion: "0.60.0",
			expectedVersion: "0.60.0",
		},
		{
			name:            "Codex without explicit version",
			engineID:        "codex",
			explicitVersion: "",
			expectedVersion: string(constants.DefaultCodexVersion),
		},
		{
			name:            "Custom engine without version",
			engineID:        "custom",
			explicitVersion: "",
			expectedVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get %s engine: %v", tt.engineID, err)
			}

			workflowData := &WorkflowData{}
			if tt.explicitVersion != "" {
				workflowData.EngineConfig = &EngineConfig{
					Version: tt.explicitVersion,
				}
			}

			version := getInstallationVersion(workflowData, engine)
			if version != tt.expectedVersion {
				t.Errorf("Expected version '%s', got '%s'", tt.expectedVersion, version)
			}
		})
	}
}
