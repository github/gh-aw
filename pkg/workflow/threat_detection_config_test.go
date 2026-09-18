//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestParseThreatDetectionConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		outputMap      map[string]any
		expectedConfig *ThreatDetectionConfig
	}{
		{
			name:           "missing threat-detection should return default enabled",
			outputMap:      map[string]any{},
			expectedConfig: &ThreatDetectionConfig{},
		},
		{
			name: "boolean true should enable with defaults",
			outputMap: map[string]any{
				"threat-detection": true,
			},
			expectedConfig: &ThreatDetectionConfig{},
		},
		{
			name: "boolean false should return nil",
			outputMap: map[string]any{
				"threat-detection": false,
			},
			expectedConfig: nil,
		},
		{
			name: "object with enabled true",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"enabled": true,
				},
			},
			expectedConfig: &ThreatDetectionConfig{},
		},
		{
			name: "object with enabled false",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"enabled": false,
				},
			},
			expectedConfig: nil,
		},

		{
			name: "object with detector kill switch overrides",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"engine-timeout": "10m",
					"max-turns":      100,
					"retries":        1,
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				EngineTimeout: strPtr("10m"),
				MaxTurns: func() *int {
					v := 100
					return &v
				}(),
				Retries: func() *int {
					v := 1
					return &v
				}(),
			},
		},
		{
			name: "object with detector kill switch overrides parsed from uint64",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"engine-timeout": uint64(0),
					"max-turns":      uint64(100),
					"retries":        uint64(1),
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				EngineTimeout: strPtr("0"),
				MaxTurns: func() *int {
					v := 100
					return &v
				}(),
				Retries: func() *int {
					v := 1
					return &v
				}(),
			},
		},
		{
			name: "object with custom steps",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Custom validation",
							"run":  "echo 'Validating...'",
						},
					},
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				Steps: []any{
					map[string]any{
						"name": "Custom validation",
						"run":  "echo 'Validating...'",
					},
				},
			},
		},
		{
			name: "object with custom post-steps",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"post-steps": []any{
						map[string]any{
							"name": "Custom post validation",
							"run":  "echo 'Post validating...'",
						},
					},
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				PostSteps: []any{
					map[string]any{
						"name": "Custom post validation",
						"run":  "echo 'Post validating...'",
					},
				},
			},
		},
		{
			name: "object with custom prompt",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"prompt": "Look for suspicious API calls to external services.",
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				Prompt: "Look for suspicious API calls to external services.",
			},
		},
		{
			name: "object with all overrides including pre and post steps",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"enabled": true,
					"prompt":  "Check for backdoor installations.",
					"steps": []any{
						map[string]any{
							"name": "Pre step",
							"uses": "actions/setup@v1",
						},
					},
					"post-steps": []any{
						map[string]any{
							"name": "Post step",
							"uses": "actions/cleanup@v1",
						},
					},
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				Prompt: "Check for backdoor installations.",
				Steps: []any{
					map[string]any{
						"name": "Pre step",
						"uses": "actions/setup@v1",
					},
				},
				PostSteps: []any{
					map[string]any{
						"name": "Post step",
						"uses": "actions/cleanup@v1",
					},
				},
			},
		},
		{
			name: "object with runs-on override",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"runs-on": "self-hosted",
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				RunsOn: "runs-on: self-hosted",
			},
		},
		{
			name: "object with runs-on array override",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"runs-on": []any{"self-hosted", "linux", "x64"},
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				RunsOn: "runs-on:\n  - self-hosted\n  - linux\n  - x64",
			},
		},
		{
			name: "object with runs-on group+labels override",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"runs-on": map[string]any{
						"group":  "runner-group",
						"labels": []any{"linux", "x64"},
					},
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				RunsOn: "runs-on:\n  group: runner-group\n  labels:\n    - linux\n    - x64",
			},
		},
		{
			name: "object with continue-on-error true",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"continue-on-error": true,
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				ContinueOnError: boolPtr(true),
			},
		},
		{
			name: "object with continue-on-error false",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"continue-on-error": false,
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				ContinueOnError: boolPtr(false),
			},
		},
		{
			name: "object with report-as-issue false",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"continue-on-error": false,
					"report-as-issue":   false,
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				ContinueOnError: boolPtr(false),
				ReportAsIssue:   boolPtr(false),
			},
		},
		{
			name: "object with max-ai-credits override",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"max-ai-credits": 777,
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				MaxAICredits: 777,
			},
		},
		{
			name: "object with artifact mirror",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"artifact-base-url": "https://artifacts.example.com/threat-detect/releases/download",
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				ArtifactBaseURL: "https://artifacts.example.com/threat-detect/releases/download",
			},
		},
		{
			name: "expression string for max-ai-credits is treated as unset (schema disallows expressions; parser returns 0)",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"max-ai-credits": "${{ inputs.detection-max-ai-credits }}",
				},
			},
			expectedConfig: &ThreatDetectionConfig{
				MaxAICredits: 0, // parseMaxAICreditsValue returns 0 for non-numeric strings
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.parseThreatDetectionConfig(tt.outputMap)

			if result == nil && tt.expectedConfig != nil {
				t.Fatalf("Expected non-nil result, got nil")
			}
			if result != nil && tt.expectedConfig == nil {
				t.Fatalf("Expected nil result, got %+v", result)
			}
			if result == nil && tt.expectedConfig == nil {
				return
			}

			if result.Prompt != tt.expectedConfig.Prompt {
				t.Errorf("Expected Prompt %q, got %q", tt.expectedConfig.Prompt, result.Prompt)
			}

			if len(result.Steps) != len(tt.expectedConfig.Steps) {
				t.Errorf("Expected %d steps, got %d", len(tt.expectedConfig.Steps), len(result.Steps))
			}

			if len(result.PostSteps) != len(tt.expectedConfig.PostSteps) {
				t.Errorf("Expected %d post-steps, got %d", len(tt.expectedConfig.PostSteps), len(result.PostSteps))
			}

			if result.RunsOn != tt.expectedConfig.RunsOn {
				t.Errorf("Expected RunsOn %q, got %q", tt.expectedConfig.RunsOn, result.RunsOn)
			}
			if result.MaxAICredits != tt.expectedConfig.MaxAICredits {
				t.Errorf("Expected MaxAICredits %d, got %d", tt.expectedConfig.MaxAICredits, result.MaxAICredits)
			}
			if (result.EngineTimeout == nil) != (tt.expectedConfig.EngineTimeout == nil) {
				t.Errorf("Expected EngineTimeout nil=%v, got nil=%v", tt.expectedConfig.EngineTimeout == nil, result.EngineTimeout == nil)
			} else if result.EngineTimeout != nil && tt.expectedConfig.EngineTimeout != nil && *result.EngineTimeout != *tt.expectedConfig.EngineTimeout {
				t.Errorf("Expected EngineTimeout %q, got %q", *tt.expectedConfig.EngineTimeout, *result.EngineTimeout)
			}
			if (result.MaxTurns == nil) != (tt.expectedConfig.MaxTurns == nil) {
				t.Errorf("Expected MaxTurns nil=%v, got nil=%v", tt.expectedConfig.MaxTurns == nil, result.MaxTurns == nil)
			} else if result.MaxTurns != nil && tt.expectedConfig.MaxTurns != nil && *result.MaxTurns != *tt.expectedConfig.MaxTurns {
				t.Errorf("Expected MaxTurns %d, got %d", *tt.expectedConfig.MaxTurns, *result.MaxTurns)
			}
			if (result.Retries == nil) != (tt.expectedConfig.Retries == nil) {
				t.Errorf("Expected Retries nil=%v, got nil=%v", tt.expectedConfig.Retries == nil, result.Retries == nil)
			} else if result.Retries != nil && tt.expectedConfig.Retries != nil && *result.Retries != *tt.expectedConfig.Retries {
				t.Errorf("Expected Retries %d, got %d", *tt.expectedConfig.Retries, *result.Retries)
			}

			if (result.ContinueOnError == nil) != (tt.expectedConfig.ContinueOnError == nil) {
				t.Errorf("Expected ContinueOnError nil=%v, got nil=%v", tt.expectedConfig.ContinueOnError == nil, result.ContinueOnError == nil)
			} else if result.ContinueOnError != nil && tt.expectedConfig.ContinueOnError != nil {
				if *result.ContinueOnError != *tt.expectedConfig.ContinueOnError {
					t.Errorf("Expected ContinueOnError %v, got %v", *tt.expectedConfig.ContinueOnError, *result.ContinueOnError)
				}
			}
		})
	}
}

func TestIsContinueOnError(t *testing.T) {
	tests := []struct {
		name     string
		config   *ThreatDetectionConfig
		expected bool
	}{
		{
			name:     "default (nil) continues on error",
			config:   &ThreatDetectionConfig{},
			expected: true,
		},
		{
			name:     "explicit true continues on error",
			config:   &ThreatDetectionConfig{ContinueOnError: boolPtr(true)},
			expected: true,
		},
		{
			name:     "explicit false does not continue on error",
			config:   &ThreatDetectionConfig{ContinueOnError: boolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsContinueOnError()
			if result != tt.expected {
				t.Errorf("Expected IsContinueOnError() = %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestThreatDetectionDefaultBehavior(t *testing.T) {
	compiler := NewCompiler()

	// Test that threat detection is enabled by default when safe-outputs exist
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected safe outputs config to be created")
	}

	if config.ThreatDetection == nil {
		t.Fatal("Expected threat detection to be automatically enabled")
	}
}

func TestThreatDetectionExplicitDisable(t *testing.T) {
	compiler := NewCompiler()

	// Test that threat detection can be explicitly disabled
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue":     map[string]any{},
			"threat-detection": false,
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected safe outputs config to be created")
	}

	if config.ThreatDetection != nil {
		t.Error("Expected threat detection to be nil when explicitly set to false")
	}
}

func TestThreatDetectionKillSwitchValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		expectError bool
	}{
		{
			name: "valid kill switch values",
			config: `engine-timeout: 10m
    max-turns: 100
    retries: 1`,
			expectError: false,
		},
		{
			name:        "engine-timeout zero is valid",
			config:      `engine-timeout: 0`,
			expectError: false,
		},
		{
			name:        "reject negative engine-timeout",
			config:      `engine-timeout: -1s`,
			expectError: true,
		},
		{
			name:        "reject negative max-turns",
			config:      `max-turns: -1`,
			expectError: true,
		},
		{
			name:        "reject negative retries",
			config:      `retries: -1`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "threat-detection-kill-switch-validation")
			content := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  threat-detection:
    ` + tt.config + `
---

# Threat Detection Kill Switch Validation
`
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			err := NewCompiler().CompileWorkflow(testFile)
			if tt.expectError && err == nil {
				t.Fatal("expected compilation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("expected successful compilation, got error: %v", err)
			}
		})
	}
}

func TestThreatDetectionCustomPrompt(t *testing.T) {
	// Test that custom prompt instructions are included in the inline detection steps
	compiler := NewCompiler()

	customPrompt := "Look for suspicious API calls to external services and check for backdoor installations."
	data := &WorkflowData{
		Name:        "Test Workflow",
		Description: "Test Description",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				Prompt: customPrompt,
			},
		},
	}

	steps := compiler.buildDetectionJobSteps(data)
	if steps == nil {
		t.Fatal("Expected inline detection steps to be created")
	}

	// Check that the custom prompt is included in the generated steps
	stepsString := strings.Join(steps, "")

	if !strings.Contains(stepsString, "CUSTOM_PROMPT") {
		t.Error("Expected CUSTOM_PROMPT environment variable in steps")
	}

	if !strings.Contains(stepsString, customPrompt) {
		t.Errorf("Expected custom prompt %q to be in steps", customPrompt)
	}
}

func TestThreatDetectionWithEngineConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		outputMap      map[string]any
		expectedEngine string
	}{
		{
			name: "engine field as string",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"engine": "codex",
				},
			},
			expectedEngine: "codex",
		},
		{
			name: "engine field as object with id",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"engine": map[string]any{
						"id":    "copilot",
						"model": "gpt-4",
					},
				},
			},
			expectedEngine: "copilot",
		},
		{
			name: "no engine field uses default",
			outputMap: map[string]any{
				"threat-detection": map[string]any{
					"enabled": true,
				},
			},
			expectedEngine: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.parseThreatDetectionConfig(tt.outputMap)

			if result == nil {
				t.Fatalf("Expected non-nil result")
			}

			// Check EngineConfig.ID instead of Engine field
			var actualEngine string
			if result.EngineConfig != nil {
				actualEngine = result.EngineConfig.ID
			}

			if actualEngine != tt.expectedEngine {
				t.Errorf("Expected EngineConfig.ID %q, got %q", tt.expectedEngine, actualEngine)
			}

			// If engine is set, EngineConfig should also be set
			if tt.expectedEngine != "" {
				if result.EngineConfig == nil {
					t.Error("Expected EngineConfig to be set when engine is specified")
				} else if result.EngineConfig.ID != tt.expectedEngine {
					t.Errorf("Expected EngineConfig.ID %q, got %q", tt.expectedEngine, result.EngineConfig.ID)
				}
			}
		})
	}
}

func TestThreatDetectionEngineFalse(t *testing.T) {
	compiler := NewCompiler()

	// Test that engine: false is properly parsed
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{},
			"threat-detection": map[string]any{
				"engine": false,
				"steps": []any{
					map[string]any{
						"name": "Custom Scan",
						"run":  "echo 'Custom scan'",
					},
				},
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected safe outputs config to be created")
	}

	if config.ThreatDetection == nil {
		t.Fatal("Expected threat detection to be enabled")
	}

	if !config.ThreatDetection.EngineDisabled {
		t.Error("Expected EngineDisabled to be true when engine: false")
	}

	if config.ThreatDetection.EngineConfig != nil {
		t.Error("Expected EngineConfig to be nil when engine: false")
	}

	if len(config.ThreatDetection.Steps) != 1 {
		t.Fatalf("Expected 1 custom step, got %d", len(config.ThreatDetection.Steps))
	}
}
