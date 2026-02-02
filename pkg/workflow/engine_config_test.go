//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractEngineConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                  string
		frontmatter           map[string]any
		expectedEngineSetting string
		expectedConfig        *EngineConfig
	}{
		{
			name:                  "no engine specified",
			frontmatter:           map[string]any{},
			expectedEngineSetting: "",
			expectedConfig:        nil,
		},
		{
			name:                  "string format - claude",
			frontmatter:           map[string]any{"engine": "claude"},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude"},
		},
		{
			name:                  "string format - codex",
			frontmatter:           map[string]any{"engine": "codex"},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex"},
		},
		{
			name:                  "string format - custom",
			frontmatter:           map[string]any{"engine": "custom"},
			expectedEngineSetting: "custom",
			expectedConfig:        &EngineConfig{ID: "custom"},
		},
		{
			name: "object format - minimal (id only)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude"},
		},
		{
			name: "object format - with version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "claude",
					"version": "beta",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta"},
		},
		{
			name: "object format - with integer version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": 20,
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", Version: "20"},
		},
		{
			name: "object format - with float version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "claude",
					"version": 3.11,
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Version: "3.11"},
		},
		{
			name: "object format - with model",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":    "codex",
					"model": "gpt-4o",
				},
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", Model: "gpt-4o"},
		},
		{
			name: "object format - complete",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "claude",
					"version": "beta",
					"model":   "claude-3-5-sonnet-20241022",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta", Model: "claude-3-5-sonnet-20241022"},
		},
		{
			name: "object format - with max-turns",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":        "claude",
					"max-turns": 5,
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", MaxTurns: "5"},
		},
		{
			name: "object format - complete with max-turns",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":        "claude",
					"version":   "beta",
					"model":     "claude-3-5-sonnet-20241022",
					"max-turns": 10,
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta", Model: "claude-3-5-sonnet-20241022", MaxTurns: "10"},
		},
		{
			name: "object format - with env vars",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"env": map[string]any{
						"CUSTOM_VAR":  "value1",
						"ANOTHER_VAR": "${{ secrets.SECRET_VAR }}",
					},
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Env: map[string]string{"CUSTOM_VAR": "value1", "ANOTHER_VAR": "${{ secrets.SECRET_VAR }}"}},
		},
		{
			name: "object format - complete with env vars",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":        "claude",
					"version":   "beta",
					"model":     "claude-3-5-sonnet-20241022",
					"max-turns": 5,
					"env": map[string]any{
						"AWS_REGION":   "us-west-2",
						"API_ENDPOINT": "https://api.example.com",
					},
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta", Model: "claude-3-5-sonnet-20241022", MaxTurns: "5", Env: map[string]string{"AWS_REGION": "us-west-2", "API_ENDPOINT": "https://api.example.com"}},
		},
		{
			name: "custom engine with steps",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "custom",
					"steps": []any{
						map[string]any{
							"name": "Setup Node.js",
							"uses": "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
							"with": map[string]any{
								"node-version": "18",
							},
						},
						map[string]any{
							"name": "Run tests",
							"run":  "npm test",
						},
					},
				},
			},
			expectedEngineSetting: "custom",
			expectedConfig: &EngineConfig{
				ID: "custom",
				Steps: []map[string]any{
					{
						"name": "Setup Node.js",
						"uses": "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
						"with": map[string]any{
							"node-version": "18",
						},
					},
					{
						"name": "Run tests",
						"run":  "npm test",
					},
				},
			},
		},
		{
			name: "object format - missing id",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"version": "beta",
					"model":   "gpt-4o",
				},
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{Version: "beta", Model: "gpt-4o"},
		},
		{
			name: "object format - with user-agent (hyphen)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":         "codex",
					"user-agent": "my-custom-agent-hyphen",
				},
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", UserAgent: "my-custom-agent-hyphen"},
		},
		{
			name: "object format - complete with user-agent",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":         "codex",
					"version":    "beta",
					"model":      "gpt-4o",
					"max-turns":  3,
					"user-agent": "complete-custom-agent",
					"env": map[string]any{
						"CUSTOM_VAR": "value1",
					},
				},
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", Version: "beta", Model: "gpt-4o", MaxTurns: "3", UserAgent: "complete-custom-agent", Env: map[string]string{"CUSTOM_VAR": "value1"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engineSetting, config := compiler.ExtractEngineConfig(test.frontmatter)

			if engineSetting != test.expectedEngineSetting {
				t.Errorf("Expected engineSetting '%s', got '%s'", test.expectedEngineSetting, engineSetting)
			}

			if test.expectedConfig == nil {
				if config != nil {
					t.Errorf("Expected nil config, got %+v", config)
				}
			} else {
				if config == nil {
					t.Errorf("Expected config %+v, got nil", test.expectedConfig)
					return
				}

				if config.ID != test.expectedConfig.ID {
					t.Errorf("Expected config.ID '%s', got '%s'", test.expectedConfig.ID, config.ID)
				}

				if config.Version != test.expectedConfig.Version {
					t.Errorf("Expected config.Version '%s', got '%s'", test.expectedConfig.Version, config.Version)
				}

				if config.Model != test.expectedConfig.Model {
					t.Errorf("Expected config.Model '%s', got '%s'", test.expectedConfig.Model, config.Model)
				}

				if config.MaxTurns != test.expectedConfig.MaxTurns {
					t.Errorf("Expected config.MaxTurns '%s', got '%s'", test.expectedConfig.MaxTurns, config.MaxTurns)
				}

				if config.UserAgent != test.expectedConfig.UserAgent {
					t.Errorf("Expected config.UserAgent '%s', got '%s'", test.expectedConfig.UserAgent, config.UserAgent)
				}

				if len(config.Env) != len(test.expectedConfig.Env) {
					t.Errorf("Expected config.Env length %d, got %d", len(test.expectedConfig.Env), len(config.Env))
				} else {
					for key, expectedValue := range test.expectedConfig.Env {
						if actualValue, exists := config.Env[key]; !exists {
							t.Errorf("Expected config.Env to contain key '%s'", key)
						} else if actualValue != expectedValue {
							t.Errorf("Expected config.Env['%s'] = '%s', got '%s'", key, expectedValue, actualValue)
						}
					}
				}

				if len(config.Steps) != len(test.expectedConfig.Steps) {
					t.Errorf("Expected config.Steps length %d, got %d", len(test.expectedConfig.Steps), len(config.Steps))
				} else {
					for i, expectedStep := range test.expectedConfig.Steps {
						if i >= len(config.Steps) {
							t.Errorf("Expected step at index %d", i)
							continue
						}
						actualStep := config.Steps[i]
						for key, expectedValue := range expectedStep {
							if actualValue, exists := actualStep[key]; !exists {
								t.Errorf("Expected step[%d] to contain key '%s'", i, key)
							} else {
								// For nested maps, do a simple string comparison for now
								if fmt.Sprintf("%v", actualValue) != fmt.Sprintf("%v", expectedValue) {
									t.Errorf("Expected step[%d]['%s'] = '%v', got '%v'", i, key, expectedValue, actualValue)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestCompileWorkflowWithExtendedEngine(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "extended-engine-test")

	tests := []struct {
		name           string
		content        string
		expectedAI     string
		expectedConfig *EngineConfig
	}{
		{
			name: "string engine format",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

This is a test workflow.`,
			expectedAI:     "claude",
			expectedConfig: &EngineConfig{ID: "claude"},
		},
		{
			name: "object engine format - complete",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
engine:
  id: claude
  version: beta
  model: claude-3-5-sonnet-20241022
---

# Test Workflow

This is a test workflow.`,
			expectedAI:     "claude",
			expectedConfig: &EngineConfig{ID: "claude", Version: "beta", Model: "claude-3-5-sonnet-20241022"},
		},
		{
			name: "object engine format - codex with model",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
engine:
  id: codex
  model: gpt-4o
---

# Test Workflow

This is a test workflow.`,
			expectedAI:     "codex",
			expectedConfig: &EngineConfig{ID: "codex", Model: "gpt-4o"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(test.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Check AI field (backwards compatibility)
			if workflowData.AI != test.expectedAI {
				t.Errorf("Expected AI '%s', got '%s'", test.expectedAI, workflowData.AI)
			}

			// Check EngineConfig
			if test.expectedConfig == nil {
				if workflowData.EngineConfig != nil {
					t.Errorf("Expected nil EngineConfig, got %+v", workflowData.EngineConfig)
				}
			} else {
				if workflowData.EngineConfig == nil {
					t.Errorf("Expected EngineConfig %+v, got nil", test.expectedConfig)
					return
				}

				if workflowData.EngineConfig.ID != test.expectedConfig.ID {
					t.Errorf("Expected EngineConfig.ID '%s', got '%s'", test.expectedConfig.ID, workflowData.EngineConfig.ID)
				}

				if workflowData.EngineConfig.Version != test.expectedConfig.Version {
					t.Errorf("Expected EngineConfig.Version '%s', got '%s'", test.expectedConfig.Version, workflowData.EngineConfig.Version)
				}

				if workflowData.EngineConfig.Model != test.expectedConfig.Model {
					t.Errorf("Expected EngineConfig.Model '%s', got '%s'", test.expectedConfig.Model, workflowData.EngineConfig.Model)
				}
			}
		})
	}
}

func TestEngineConfigurationWithModel(t *testing.T) {
	tests := []struct {
		name           string
		engine         CodingAgentEngine
		engineConfig   *EngineConfig
		expectedModel  string
		expectedAPIKey string
	}{
		{
			name:   "Claude with model",
			engine: NewClaudeEngine(),
			engineConfig: &EngineConfig{
				ID:    "claude",
				Model: "claude-3-5-sonnet-20241022",
			},
			expectedModel:  "claude-3-5-sonnet-20241022",
			expectedAPIKey: "",
		},
		{
			name:   "Codex with model",
			engine: NewCodexEngine(),
			engineConfig: &EngineConfig{
				ID:    "codex",
				Model: "gpt-4o",
			},
			expectedModel:  "gpt-4o",
			expectedAPIKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: tt.engineConfig,
			}
			steps := tt.engine.GetExecutionSteps(workflowData, "test-log")

			if len(steps) == 0 {
				t.Fatalf("Expected at least one step, got none")
			}

			// Convert first step to YAML string for testing
			stepContent := strings.Join([]string(steps[0]), "\n")

			switch tt.engine.GetID() {
			case "claude":
				if tt.expectedModel != "" {
					expectedModelLine := "--model " + tt.expectedModel
					if !strings.Contains(stepContent, expectedModelLine) {
						t.Errorf("Expected step to contain model %s, got step content:\n%s", tt.expectedModel, stepContent)
					}
				}

			case "codex":
				if tt.expectedModel != "" {
					expectedModelArg := "model=" + tt.expectedModel
					if !strings.Contains(stepContent, expectedModelArg) {
						t.Errorf("Expected command to contain %s, got step content:\n%s", expectedModelArg, stepContent)
					}
				}
			}
		})
	}
}

func TestEngineConfigurationWithCustomEnvVars(t *testing.T) {
	tests := []struct {
		name         string
		engine       CodingAgentEngine
		engineConfig *EngineConfig
		hasOutput    bool
	}{
		{
			name:   "Claude with custom env vars",
			engine: NewClaudeEngine(),
			engineConfig: &EngineConfig{
				ID:  "claude",
				Env: map[string]string{"AWS_REGION": "us-west-2", "CUSTOM_VAR": "${{ secrets.MY_SECRET }}"},
			},
			hasOutput: false,
		},
		{
			name:   "Claude with custom env vars and output",
			engine: NewClaudeEngine(),
			engineConfig: &EngineConfig{
				ID:  "claude",
				Env: map[string]string{"API_ENDPOINT": "https://api.example.com", "DEBUG_MODE": "true"},
			},
			hasOutput: true,
		},
		{
			name:   "Codex with custom env vars",
			engine: NewCodexEngine(),
			engineConfig: &EngineConfig{
				ID:  "codex",
				Env: map[string]string{"CUSTOM_API_KEY": "test123", "PROXY_URL": "http://proxy.example.com"},
			},
			hasOutput: false,
		},
		{
			name:   "Codex with custom env vars and output",
			engine: NewCodexEngine(),
			engineConfig: &EngineConfig{
				ID:  "codex",
				Env: map[string]string{"ENVIRONMENT": "production", "LOG_LEVEL": "debug"},
			},
			hasOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: tt.engineConfig,
			}
			if tt.hasOutput {
				workflowData.SafeOutputs = &SafeOutputsConfig{}
			}
			steps := tt.engine.GetExecutionSteps(workflowData, "test-log")

			if len(steps) == 0 {
				t.Fatalf("Expected at least one step, got none")
			}

			// Convert first step to YAML string for testing
			stepContent := strings.Join([]string(steps[0]), "\n")

			switch tt.engine.GetID() {
			case "claude":
				// For Claude, custom env vars should be in claude_env input
				if tt.engineConfig != nil && len(tt.engineConfig.Env) > 0 {
					foundEnvVar := false
					for key, value := range tt.engineConfig.Env {
						if strings.Contains(stepContent, key+":") && strings.Contains(stepContent, value) {
							foundEnvVar = true
							break
						}
					}
					if !foundEnvVar {
						t.Errorf("Expected step to contain custom environment variables, got step content:\n%s", stepContent)
					}
				}

			case "codex":
				// For Codex, custom env vars should be in the step's env section
				if tt.engineConfig != nil && len(tt.engineConfig.Env) > 0 {
					foundEnvVar := false
					for key, expectedValue := range tt.engineConfig.Env {
						envLine := key + ": " + expectedValue
						if strings.Contains(stepContent, envLine) {
							foundEnvVar = true
							break
						}
					}
					if !foundEnvVar {
						t.Errorf("Expected step to contain custom environment variables, got step content:\n%s", stepContent)
					}
				}
			}
		})
	}
}

func TestNilEngineConfig(t *testing.T) {
	engines := []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
		NewCustomEngine(),
	}

	for _, engine := range engines {
		t.Run(engine.GetID(), func(t *testing.T) {
			// Should not panic when engineConfig is nil
			workflowData := &WorkflowData{
				Name: "test-workflow",
			}
			steps := engine.GetExecutionSteps(workflowData, "test-log")

			// Custom engine returns one log step even when no custom steps are configured
			if engine.GetID() == "custom" {
				if len(steps) != 1 {
					t.Errorf("Expected 1 step (log step) for custom engine when no custom steps configured, got %d", len(steps))
				}
			} else {
				// Other engines should return at least one step
				if len(steps) == 0 {
					t.Errorf("Expected at least one step for engine %s, got none", engine.GetID())
				}

				// Check that the first step has some content
				if len(steps) > 0 && len(steps[0]) == 0 {
					t.Errorf("Expected non-empty step content for engine %s", engine.GetID())
				}
			}
		})
	}
}
