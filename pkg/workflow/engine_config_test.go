//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func TestExtractEngineConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                  string
		frontmatter           map[string]any
		expectedEngineSetting string
		expectedConfig        *EngineConfig
		expectedModel         string
	}{
		{
			name:                  "no engine specified",
			frontmatter:           map[string]any{},
			expectedEngineSetting: "",
			expectedConfig:        nil,
		},
		{
			name: "top-level max-runs without engine",
			frontmatter: map[string]any{
				"max-runs": 25,
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxRuns: 25},
		},
		{
			name: "top-level max-turns takes precedence over deprecated max-runs",
			frontmatter: map[string]any{
				"max-runs":  25,
				"max-turns": 30,
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxTurns: "30", MaxRuns: 30},
		},
		{
			name: "top-level max-turns without engine",
			frontmatter: map[string]any{
				"max-turns": 25,
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxTurns: "25", MaxRuns: 25},
		},
		{
			name: "top-level max-turns expression without engine",
			frontmatter: map[string]any{
				"max-turns": "${{ inputs.max-turns }}",
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxTurns: "${{ inputs.max-turns }}"},
		},
		{
			name: "top-level max-tool-denials without engine",
			frontmatter: map[string]any{
				"max-tool-denials": 5,
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxToolDenials: "5"},
		},
		{
			name: "top-level max-tool-denials expression without engine",
			frontmatter: map[string]any{
				"max-tool-denials": "${{ inputs.max-tool-denials }}",
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxToolDenials: "${{ inputs.max-tool-denials }}"},
		},
		{
			name: "top-level max-turn-cache-misses without engine",
			frontmatter: map[string]any{
				"max-turn-cache-misses": 6,
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{MaxTurnCacheMisses: 6},
		},
		{
			name: "top-level max-turns zero is ignored",
			frontmatter: map[string]any{
				"max-turns": 0,
			},
			expectedEngineSetting: "",
			expectedConfig:        nil,
		},
		{
			name: "top-level max-turns negative is ignored",
			frontmatter: map[string]any{
				"max-turns": -1,
			},
			expectedEngineSetting: "",
			expectedConfig:        nil,
		},
		{
			// ExtractEngineConfig returns &EngineConfig{} (empty struct, not nil) when
			// only a top-level model: is present and no engine: key is specified.
			// Callers must nil-check config before reading fields in the engine-present path,
			// but for the model-only path they receive a non-nil empty config.
			name: "top-level model without engine",
			frontmatter: map[string]any{
				"model": "gpt-4",
			},
			expectedEngineSetting: "",
			expectedConfig:        &EngineConfig{},
			expectedModel:         "gpt-4",
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
			name: "string format - top-level model returned",
			frontmatter: map[string]any{
				"engine": "copilot",
				"model":  "claude-sonnet-4.5",
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot"},
			expectedModel:         "claude-sonnet-4.5",
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
			name: "object format - with expression version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": "${{ inputs.engine-version }}",
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", Version: "${{ inputs.engine-version }}"},
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
			expectedConfig:        &EngineConfig{ID: "codex"},
			expectedModel:         "gpt-4o",
		},
		{
			name: "object format - engine.model overrides top-level model",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":    "codex",
					"model": "gpt-4o",
				},
				"model": "gpt-5",
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex"},
			expectedModel:         "gpt-4o",
		},
		{
			// Empty top-level model: "" must NOT override engine.model.
			// The implementation guards with topLevelModel != "", so an empty
			// string leaves the engine.model value intact.
			name: "object format - empty top-level model does not override engine.model",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":    "codex",
					"model": "gpt-4o",
				},
				"model": "",
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex"},
			expectedModel:         "gpt-4o",
		},
		{
			name: "object format - top-level model alone (no engine.model)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
				},
				"model": "claude-sonnet-4.5",
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude"},
			expectedModel:         "claude-sonnet-4.5",
		},
		{
			name: "object format - with model-provider override",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":             "claude",
					"model-provider": "github",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", LLMProvider: LLMProviderGitHub},
		},
		{
			name: "object format - with provider override",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":       "claude",
					"provider": "openai",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", LLMProvider: LLMProviderOpenAI},
		},
		{
			name: "object format - provider override wins over model-provider",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":             "claude",
					"model-provider": "github",
					"provider":       "openai",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", LLMProvider: LLMProviderOpenAI},
		},
		{
			name: "object format - deprecated llm-provider ignored",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":           "claude",
					"llm-provider": "github",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude"},
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
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta"},
			expectedModel:         "claude-3-5-sonnet-20241022",
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
			name: "object format - with top-level max-runs",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
				},
				"max-runs": 12,
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", MaxRuns: 12},
		},
		{
			name: "object format - with top-level max-turns",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "codex",
				},
				"max-turns": 12,
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", MaxTurns: "12", MaxRuns: 12},
		},
		{
			name: "object format - with top-level max-tool-denials",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
				"max-tool-denials": 8,
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", MaxToolDenials: "8"},
		},
		{
			name: "object format - top-level max-turns overrides engine max-turns",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":        "codex",
					"max-turns": 3,
				},
				"max-turns": "${{ inputs.max-turns }}",
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", MaxTurns: "${{ inputs.max-turns }}"},
		},
		{
			name: "object format - with top-level max-runs as string",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
				},
				"max-runs": "12",
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", MaxRuns: 12},
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
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta", MaxTurns: "10"},
			expectedModel:         "claude-3-5-sonnet-20241022",
		},
		{
			// float64 is what json.Unmarshal produces for numbers when deserializing engine
			// config JSON from shared imports (JSON roundtrip: YAML int -> JSON -> Go float64)
			name: "object format - with max-turns as float64 (JSON roundtrip from shared import)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":        "claude",
					"max-turns": float64(100),
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", MaxTurns: "100"},
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
			name: "object format - with non-string scalar env vars",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"env": map[string]any{
						"STRING_VAR":      "value1",
						"INT_VAR":         1,
						"FLOAT_VAR":       float64(1000),
						"LARGE_FLOAT_VAR": float64(1000000),
						"BOOL_VAR":        true,
					},
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Env: map[string]string{"STRING_VAR": "value1", "INT_VAR": "1", "FLOAT_VAR": "1000", "LARGE_FLOAT_VAR": "1000000", "BOOL_VAR": "true"}},
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
			expectedConfig:        &EngineConfig{ID: "claude", Version: "beta", MaxTurns: "5", Env: map[string]string{"AWS_REGION": "us-west-2", "API_ENDPOINT": "https://api.example.com"}},
			expectedModel:         "claude-3-5-sonnet-20241022",
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
			expectedConfig:        &EngineConfig{Version: "beta"},
			expectedModel:         "gpt-4o",
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
			name: "object format - harness string short form (legacy)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"harness": "custom_copilot_harness.cjs",
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", HarnessScript: "custom_copilot_harness.cjs"},
		},
		{
			name: "object format - harness sub-object use-only (long form)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
					"harness": map[string]any{
						"use": "custom_copilot_harness.cjs",
					},
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", HarnessScript: "custom_copilot_harness.cjs"},
		},
		{
			name: "object format - cwd literal path",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":  "copilot",
					"cwd": "/custom/workspace",
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig:        &EngineConfig{ID: "copilot", Cwd: "/custom/workspace"},
		},
		{
			name: "object format - cwd github actions expression",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":  "claude",
					"cwd": "${{ github.workspace }}/subdir",
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig:        &EngineConfig{ID: "claude", Cwd: "${{ github.workspace }}/subdir"},
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
			expectedConfig:        &EngineConfig{ID: "codex", Version: "beta", MaxTurns: "3", UserAgent: "complete-custom-agent", Env: map[string]string{"CUSTOM_VAR": "value1"}},
			expectedModel:         "gpt-4o",
		},
		{
			name: "object format - harness sub-object retry policy fields (integer literals)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
					"harness": map[string]any{
						"max-retries":        6,
						"initial-delay-ms":   10000,
						"backoff-multiplier": 2,
						"max-delay-ms":       180000,
						"watchdog-timeout":   120,
					},
				},
			},
			expectedEngineSetting: "copilot",
			expectedConfig: &EngineConfig{
				ID:                       "copilot",
				HarnessMaxRetries:        "6",
				HarnessInitialDelayMs:    "10000",
				HarnessBackoffMultiplier: "2",
				HarnessMaxDelayMs:        "180000",
				HarnessWatchdogTimeoutMs: "120000",
			},
		},
		{
			name: "object format - harness sub-object retry policy fields (GitHub Actions expressions)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"harness": map[string]any{
						"max-retries":        "${{ vars.RETRY_COUNT }}",
						"initial-delay-ms":   "${{ vars.RETRY_DELAY }}",
						"backoff-multiplier": "${{ vars.BACKOFF }}",
						"max-delay-ms":       "${{ vars.MAX_DELAY }}",
						"watchdog-timeout":   "${{ vars.WATCHDOG_TIMEOUT_SEC }}",
					},
				},
			},
			expectedEngineSetting: "claude",
			expectedConfig: &EngineConfig{
				ID:                       "claude",
				HarnessMaxRetries:        "${{ vars.RETRY_COUNT }}",
				HarnessInitialDelayMs:    "${{ vars.RETRY_DELAY }}",
				HarnessBackoffMultiplier: "${{ vars.BACKOFF }}",
				HarnessMaxDelayMs:        "${{ vars.MAX_DELAY }}",
				HarnessWatchdogTimeoutMs: "${{ vars.WATCHDOG_TIMEOUT_SEC }}",
			},
		},
		{
			name: "object format - harness sub-object use and max-retries allows zero",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "codex",
					"harness": map[string]any{
						"use":         "custom_harness.cjs",
						"max-retries": 0,
					},
				},
			},
			expectedEngineSetting: "codex",
			expectedConfig:        &EngineConfig{ID: "codex", HarnessScript: "custom_harness.cjs", HarnessMaxRetries: "0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engineSetting, config, model := compiler.ExtractEngineConfig(test.frontmatter)

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

				if model != test.expectedModel {
					t.Errorf("Expected model '%s', got '%s'", test.expectedModel, model)
				}

				if config.MaxTurns != test.expectedConfig.MaxTurns {
					t.Errorf("Expected config.MaxTurns '%s', got '%s'", test.expectedConfig.MaxTurns, config.MaxTurns)
				}
				if config.MaxToolDenials != test.expectedConfig.MaxToolDenials {
					t.Errorf("Expected config.MaxToolDenials '%s', got '%s'", test.expectedConfig.MaxToolDenials, config.MaxToolDenials)
				}

				if config.MaxRuns != test.expectedConfig.MaxRuns {
					t.Errorf("Expected config.MaxRuns '%d', got '%d'", test.expectedConfig.MaxRuns, config.MaxRuns)
				}

				if config.UserAgent != test.expectedConfig.UserAgent {
					t.Errorf("Expected config.UserAgent '%s', got '%s'", test.expectedConfig.UserAgent, config.UserAgent)
				}

				if config.HarnessScript != test.expectedConfig.HarnessScript {
					t.Errorf("Expected config.HarnessScript '%s', got '%s'", test.expectedConfig.HarnessScript, config.HarnessScript)
				}

				if config.Driver != test.expectedConfig.Driver {
					t.Errorf("Expected config.Driver '%s', got '%s'", test.expectedConfig.Driver, config.Driver)
				}

				if config.CopilotSDK != test.expectedConfig.CopilotSDK {
					t.Errorf("Expected config.CopilotSDK '%v', got '%v'", test.expectedConfig.CopilotSDK, config.CopilotSDK)
				}

				if config.Cwd != test.expectedConfig.Cwd {
					t.Errorf("Expected config.Cwd '%s', got '%s'", test.expectedConfig.Cwd, config.Cwd)
				}

				if config.HarnessMaxRetries != test.expectedConfig.HarnessMaxRetries {
					t.Errorf("Expected config.HarnessMaxRetries '%s', got '%s'", test.expectedConfig.HarnessMaxRetries, config.HarnessMaxRetries)
				}
				if config.HarnessInitialDelayMs != test.expectedConfig.HarnessInitialDelayMs {
					t.Errorf("Expected config.HarnessInitialDelayMs '%s', got '%s'", test.expectedConfig.HarnessInitialDelayMs, config.HarnessInitialDelayMs)
				}
				if config.HarnessBackoffMultiplier != test.expectedConfig.HarnessBackoffMultiplier {
					t.Errorf("Expected config.HarnessBackoffMultiplier '%s', got '%s'", test.expectedConfig.HarnessBackoffMultiplier, config.HarnessBackoffMultiplier)
				}
				if config.HarnessMaxDelayMs != test.expectedConfig.HarnessMaxDelayMs {
					t.Errorf("Expected config.HarnessMaxDelayMs '%s', got '%s'", test.expectedConfig.HarnessMaxDelayMs, config.HarnessMaxDelayMs)
				}
				if config.HarnessWatchdogTimeoutMs != test.expectedConfig.HarnessWatchdogTimeoutMs {
					t.Errorf("Expected config.HarnessWatchdogTimeoutMs '%s', got '%s'", test.expectedConfig.HarnessWatchdogTimeoutMs, config.HarnessWatchdogTimeoutMs)
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

			}
		})
	}
}

func TestExtractEngineConfig_EngineAuthMapsToAWFEnv(t *testing.T) {
	compiler := NewCompiler()
	_, config, _ := compiler.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"auth": map[string]any{
				"type":            "github-oidc",
				"audience":        "https://cognitiveservices.azure.com",
				"azure-tenant-id": "tenant-id",
				"azure-client-id": "client-id",
				"azure-scope":     "https://cognitiveservices.azure.com/.default",
				"azure-cloud":     "public",
			},
		},
	})

	assert.NotNil(t, config)
	if assert.NotNil(t, config.Auth) {
		assert.Equal(t, "github-oidc", config.Auth.Type)
		assert.Equal(t, "https://cognitiveservices.azure.com", config.Auth.Audience)
		assert.Equal(t, "tenant-id", config.Auth.AzureTenantID)
		assert.Equal(t, "client-id", config.Auth.AzureClientID)
		assert.Equal(t, "https://cognitiveservices.azure.com/.default", config.Auth.AzureScope)
		assert.Equal(t, "public", config.Auth.AzureCloud)
	}

	assert.Equal(t, "github-oidc", config.Env["AWF_AUTH_TYPE"])
	assert.Equal(t, "https://cognitiveservices.azure.com", config.Env["AWF_AUTH_OIDC_AUDIENCE"])
	assert.Equal(t, "tenant-id", config.Env["AWF_AUTH_AZURE_TENANT_ID"])
	assert.Equal(t, "client-id", config.Env["AWF_AUTH_AZURE_CLIENT_ID"])
	assert.Equal(t, "https://cognitiveservices.azure.com/.default", config.Env["AWF_AUTH_AZURE_SCOPE"])
	assert.Equal(t, "public", config.Env["AWF_AUTH_AZURE_CLOUD"])
}

func TestExtractEngineConfig_EngineEnvTakesPrecedenceOverEngineAuth(t *testing.T) {
	compiler := NewCompiler()
	_, config, _ := compiler.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"env": map[string]any{
				"AWF_AUTH_TYPE":          "static",
				"AWF_AUTH_OIDC_AUDIENCE": "from-engine-env",
			},
			"auth": map[string]any{
				"type":     "github-oidc",
				"audience": "from-engine-auth",
			},
		},
	})

	assert.NotNil(t, config)
	assert.Equal(t, "static", config.Env["AWF_AUTH_TYPE"])
	assert.Equal(t, "from-engine-env", config.Env["AWF_AUTH_OIDC_AUDIENCE"])
}

func TestExtractEngineConfig_AnthropicWIFMapsToAWFEnv(t *testing.T) {
	compiler := NewCompiler()
	_, config, _ := compiler.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "claude",
			"auth": map[string]any{
				"type":               "github-oidc",
				"provider":           "anthropic",
				"federation-rule-id": "fr_01ABC",
				"organization-id":    "org_01XYZ",
				"service-account-id": "sa_01DEF",
				"workspace-id":       "ws_01GHI",
			},
		},
	})

	assert.NotNil(t, config)
	if assert.NotNil(t, config.Auth) {
		assert.Equal(t, "github-oidc", config.Auth.Type)
		assert.Equal(t, "anthropic", config.Auth.Provider)
		assert.Equal(t, "fr_01ABC", config.Auth.AnthropicFederationRuleID)
		assert.Equal(t, "org_01XYZ", config.Auth.AnthropicOrganizationID)
		assert.Equal(t, "sa_01DEF", config.Auth.AnthropicServiceAccountID)
		assert.Equal(t, "ws_01GHI", config.Auth.AnthropicWorkspaceID)
	}

	assert.Equal(t, "github-oidc", config.Env["AWF_AUTH_TYPE"])
	assert.Equal(t, "anthropic", config.Env["AWF_AUTH_PROVIDER"])
	assert.Equal(t, "fr_01ABC", config.Env["AWF_AUTH_ANTHROPIC_FEDERATION_RULE_ID"])
	assert.Equal(t, "org_01XYZ", config.Env["AWF_AUTH_ANTHROPIC_ORGANIZATION_ID"])
	assert.Equal(t, "sa_01DEF", config.Env["AWF_AUTH_ANTHROPIC_SERVICE_ACCOUNT_ID"])
	assert.Equal(t, "ws_01GHI", config.Env["AWF_AUTH_ANTHROPIC_WORKSPACE_ID"])
}

func TestCompileWorkflowWithExtendedEngine(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "extended-engine-test")

	tests := []struct {
		name           string
		content        string
		expectedAI     string
		expectedConfig *EngineConfig
		expectedModel  string
	}{
		{
			name: "string engine format",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
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
  issues: read
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
			expectedConfig: &EngineConfig{ID: "claude", Version: "beta"},
			expectedModel:  "claude-3-5-sonnet-20241022",
		},
		{
			name: "object engine format - codex with model",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
engine:
  id: codex
  model: gpt-4o
---

# Test Workflow

This is a test workflow.`,
			expectedAI:     "codex",
			expectedConfig: &EngineConfig{ID: "codex"},
			expectedModel:  "gpt-4o",
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

				if workflowData.Model != test.expectedModel {
					t.Errorf("Expected WorkflowData.Model '%s', got '%s'", test.expectedModel, workflowData.Model)
				}
			}
		})
	}
}

func TestParseWorkflowWithSplitEngineModels(t *testing.T) {
	workflowPath := filepath.Join(testutil.TempDir(t, "split-engine-model-test"), "workflow.md")
	workflowContent := `---
on: push
permissions:
  contents: read
strict: false
engine:
  id: codex
model: openai/gpt-4o-mini
safe-outputs:
  threat-detection:
    engine:
      id: copilot
      model: gpt-5-mini
---

# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	workflowData, err := NewCompiler().ParseWorkflowFile(workflowPath)
	if err != nil {
		t.Fatalf("ParseWorkflowFile failed: %v", err)
	}

	assert.Equal(t, "openai/gpt-4o-mini", workflowData.Model)
	if assert.NotNil(t, workflowData.SafeOutputs) && assert.NotNil(t, workflowData.SafeOutputs.ThreatDetection) {
		assert.Equal(t, "gpt-5-mini", workflowData.SafeOutputs.ThreatDetection.Model)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}
	lockFile, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	if err != nil {
		t.Fatalf("ReadFile lock file failed: %v", err)
	}
	metadata, legacy, err := ExtractMetadataFromLockFile(string(lockFile))
	if err != nil {
		t.Fatalf("ExtractMetadataFromLockFile failed: %v", err)
	}
	if metadata == nil {
		t.Fatal("Expected compiled lock file to contain metadata")
	}
	if legacy {
		t.Fatal("Expected compiled lock file to contain structured metadata")
	}
	assert.Equal(t, "openai/gpt-4o-mini", metadata.AgentModel)
	assert.Equal(t, "gpt-5-mini", metadata.DetectionAgentModel)
}

// TestCompileClaudeWithExperimentsModel ensures that ${{ experiments.<name> }} used as
// engine.model is rewritten to a valid GitHub Actions expression everywhere it lands in the
// compiled lock file, instead of the literal (invalid) `experiments` context reference.
// Regression test for https://github.com/github/gh-aw/issues/61584.
func TestCompileClaudeWithExperimentsModel(t *testing.T) {
	workflowPath := filepath.Join(testutil.TempDir(t, "claude-experiments-model-test"), "workflow.md")
	workflowContent := `---
on:
  workflow_dispatch:
engine:
  id: claude
  model: ${{ experiments.model }}
experiments:
  model: [sonnet, opus]
safe-outputs:
  noop:
---

# Test Workflow

Write a haiku and say Done.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}
	lockFile, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	if err != nil {
		t.Fatalf("ReadFile lock file failed: %v", err)
	}
	lock := string(lockFile)

	// The literal (invalid) "experiments" context must never appear in the compiled workflow.
	assert.NotContains(t, lock, "experiments.model }}", "the raw experiments.model expression must be rewritten")

	// Within the activation job (same job as the pick-experiment step), the info step must
	// read the step output directly since a job cannot reference its own outputs via needs.
	assert.Contains(t, lock, `GH_AW_INFO_MODEL: "${{ steps.pick-experiment.outputs.model }}"`)

	// The pick-experiment step must run before the info step, otherwise its output is not
	// yet available and the resolved model would be empty at run time.
	pickIdx := strings.Index(lock, "id: pick-experiment")
	infoIdx := strings.Index(lock, "id: generate_aw_info")
	assert.NotEqual(t, -1, pickIdx, "pick-experiment step should be present")
	assert.NotEqual(t, -1, infoIdx, "generate_aw_info step should be present")
	assert.Less(t, pickIdx, infoIdx, "experiment selection must precede the agentic run info step")
	// Everywhere else (agent job env, safe-outputs job env), the activation job's output
	// must be referenced via the needs context.
	assert.Contains(t, lock, "ANTHROPIC_MODEL: ${{ needs.activation.outputs.model }}")
	assert.Contains(t, lock, `GH_AW_ENGINE_MODEL: "${{ needs.activation.outputs.model }}"`)
}

// TestCompileClaudeWithExperimentsModelSplitField is like TestCompileClaudeWithExperimentsModel
// but uses the split top-level `model:` field (rather than `engine.model`) to confirm both
// paths funnel through the same rewrite before the compiled workflow is emitted.
func TestCompileClaudeWithExperimentsModelSplitField(t *testing.T) {
	workflowPath := filepath.Join(testutil.TempDir(t, "claude-experiments-model-split-test"), "workflow.md")
	workflowContent := `---
on:
  workflow_dispatch:
engine:
  id: claude
model: ${{ experiments.model }}
experiments:
  model: [sonnet, opus]
safe-outputs:
  noop:
---

# Test Workflow

Write a haiku and say Done.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}
	lockFile, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	if err != nil {
		t.Fatalf("ReadFile lock file failed: %v", err)
	}
	lock := string(lockFile)

	assert.NotContains(t, lock, "experiments.model }}", "the raw experiments.model expression must be rewritten")
	assert.Contains(t, lock, `GH_AW_INFO_MODEL: "${{ steps.pick-experiment.outputs.model }}"`)
	assert.Contains(t, lock, "ANTHROPIC_MODEL: ${{ needs.activation.outputs.model }}")
	assert.Contains(t, lock, `GH_AW_ENGINE_MODEL: "${{ needs.activation.outputs.model }}"`)
}

func TestCompileCodexWithCopilotModel(t *testing.T) {
	workflowPath := filepath.Join(testutil.TempDir(t, "codex-copilot-model-test"), "workflow.md")
	workflowContent := `---
on: push
permissions:
  contents: read
  copilot-requests: write
strict: false
engine: codex
model: copilot/auto
---

# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}
	lockFile, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	if err != nil {
		t.Fatalf("ReadFile lock file failed: %v", err)
	}
	lock := string(lockFile)
	assert.Contains(t, lock, "GH_AW_LLM_PROVIDER: github")
	assert.Contains(t, lock, "COPILOT_GITHUB_TOKEN: ${{ github.token }}")
	assert.Contains(t, lock, constants.CopilotBYOKDummyAPIKeyEnvVar+": "+constants.CopilotBYOKDummyAPIKey)
	assert.Contains(t, lock, `export CODEX_API_KEY="$`+constants.CopilotBYOKDummyAPIKeyEnvVar+`"`)
	assert.Contains(t, lock, "GH_AW_MODEL_AGENT_CODEX: auto")
	assert.NotContains(t, lock, "secrets.CODEX_API_KEY")
	assert.NotContains(t, lock, "secrets.OPENAI_API_KEY")
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
				ID: "claude",
			},
			expectedModel:  "claude-3-5-sonnet-20241022",
			expectedAPIKey: "",
		},
		{
			name:   "Codex with model",
			engine: NewCodexEngine(),
			engineConfig: &EngineConfig{
				ID: "codex",
			},
			expectedModel:  "gpt-4o",
			expectedAPIKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:         "test-workflow",
				Model:        tt.expectedModel,
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
					// Claude passes model via native ANTHROPIC_MODEL env var
					expectedEnvLine := "ANTHROPIC_MODEL: " + tt.expectedModel
					if !strings.Contains(stepContent, expectedEnvLine) {
						t.Errorf("Expected step to contain env var for model %s, got step content:\n%s", tt.expectedModel, stepContent)
					}
					// Should NOT embed --model in the shell command
					if strings.Contains(stepContent, "--model "+tt.expectedModel) {
						t.Errorf("Model should not be embedded as --model flag, got step content:\n%s", stepContent)
					}
				}

			case "codex":
				if tt.expectedModel != "" {
					// Codex passes model via GH_AW_MODEL_*_CODEX with shell expansion
					// The workflow has no SafeOutputs, so it uses the detection env var
					expectedEnvLine := constants.EnvVarModelDetectionCodex + ": " + tt.expectedModel
					if !strings.Contains(stepContent, expectedEnvLine) {
						t.Errorf("Expected step to contain env var for model %s, got step content:\n%s", tt.expectedModel, stepContent)
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
	}

	for _, engine := range engines {
		t.Run(engine.GetID(), func(t *testing.T) {
			// Should not panic when engineConfig is nil
			workflowData := &WorkflowData{
				Name: "test-workflow",
			}
			steps := engine.GetExecutionSteps(workflowData, "test-log")

			// Engines should return at least one step
			if len(steps) == 0 {
				t.Errorf("Expected at least one step for engine %s, got none", engine.GetID())
			}

			// Check that the first step has some content
			if len(steps) > 0 && len(steps[0]) == 0 {
				t.Errorf("Expected non-empty step content for engine %s", engine.GetID())
			}
		})
	}
}

func TestEngineBareFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		frontmatter  map[string]any
		expectedBare bool
	}{
		{
			name: "bare true",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":   "copilot",
					"bare": true,
				},
			},
			expectedBare: true,
		},
		{
			name: "bare false",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":   "copilot",
					"bare": false,
				},
			},
			expectedBare: false,
		},
		{
			name: "bare not set (default false)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			expectedBare: false,
		},
		{
			name: "bare true for claude",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":   "claude",
					"bare": true,
				},
			},
			expectedBare: true,
		},
		{
			name: "bare true for codex",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":   "codex",
					"bare": true,
				},
			},
			expectedBare: true,
		},
		{
			name: "bare true for gemini",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":   "gemini",
					"bare": true,
				},
			},
			expectedBare: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config, _ := compiler.ExtractEngineConfig(tt.frontmatter)
			if config == nil {
				t.Fatal("Expected config to be non-nil")
			}
			if config.Bare != tt.expectedBare {
				t.Errorf("Expected Bare=%v, got Bare=%v", tt.expectedBare, config.Bare)
			}
		})
	}
}

func TestEngineBareModeCopilotArgs(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:   "copilot",
			Bare: true,
		},
	}

	engine := NewCopilotEngine()
	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	var foundFlag bool
	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "--no-custom-instructions") {
				foundFlag = true
				break
			}
		}
	}
	if !foundFlag {
		t.Error("Expected --no-custom-instructions in copilot execution steps when bare=true")
	}
}

func TestEngineBareModeCopilotArgsNotPresent(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:   "copilot",
			Bare: false,
		},
	}

	engine := NewCopilotEngine()
	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "--no-custom-instructions") {
				t.Error("Expected --no-custom-instructions to be absent when bare=false")
				return
			}
		}
	}
}

func TestEngineBareModeClaude(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:   "claude",
			Bare: true,
		},
	}

	engine := NewClaudeEngine()
	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	var foundFlag bool
	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "--bare") {
				foundFlag = true
				break
			}
		}
	}
	if !foundFlag {
		t.Error("Expected --bare in claude execution steps when bare=true")
	}
}

func TestEngineBareModeClaude_NotPresent(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:   "claude",
			Bare: false,
		},
	}

	engine := NewClaudeEngine()
	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "--bare") {
				t.Error("Expected --bare to be absent in claude execution steps when bare=false")
				return
			}
		}
	}
}

func TestSupportsBareMode(t *testing.T) {
	tests := []struct {
		name     string
		engine   CodingAgentEngine
		expected bool
	}{
		{
			name:     "copilot supports bare mode",
			engine:   NewCopilotEngine(),
			expected: true,
		},
		{
			name:     "claude supports bare mode",
			engine:   NewClaudeEngine(),
			expected: true,
		},
		{
			name:     "pi supports bare mode",
			engine:   NewPiEngine(),
			expected: true,
		},
		{
			name:     "codex does not support bare mode",
			engine:   NewCodexEngine(),
			expected: false,
		},
		{
			name:     "gemini does not support bare mode",
			engine:   NewGeminiEngine(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.engine.GetCapabilities().BareMode,
				"BareMode capability should be %v for %s", tt.expected, tt.engine.GetID())
		})
	}
}

func TestSupportsContextWindow(t *testing.T) {
	tests := []struct {
		name     string
		engine   CodingAgentEngine
		expected bool
	}{
		{name: "Claude does not support context-window", engine: NewClaudeEngine()},
		{name: "Codex does not support context-window", engine: NewCodexEngine()},
		{name: "Copilot does not support context-window", engine: NewCopilotEngine()},
		{name: "Gemini does not support context-window", engine: NewGeminiEngine()},
		{name: "Pi supports context-window", engine: NewPiEngine(), expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.engine.GetCapabilities().ContextWindow)
		})
	}
}

// TestBareMode_UnsupportedEngineNoFlag verifies that engines not supporting bare mode
// do not inject any bare-mode flags in their execution steps.
func TestBareMode_UnsupportedEngineNoFlag(t *testing.T) {
	t.Run("codex does not inject --no-system-prompt", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:   "codex",
				Bare: true,
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

		for _, step := range steps {
			for _, line := range step {
				assert.NotContains(t, line, "--no-system-prompt",
					"Codex should not inject --no-system-prompt (bare mode unsupported)")
			}
		}
	})

	t.Run("gemini does not inject GEMINI_SYSTEM_MD=/dev/null", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:   "gemini",
				Bare: true,
			},
		}

		engine := NewGeminiEngine()
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

		for _, step := range steps {
			for _, line := range step {
				if strings.Contains(line, "GEMINI_SYSTEM_MD") && strings.Contains(line, "/dev/null") {
					t.Error("Gemini should not inject GEMINI_SYSTEM_MD=/dev/null (bare mode unsupported)")
					return
				}
			}
		}
	})
}

// TestEngineMCPSessionTimeoutExtraction tests extraction of engine.mcp.session-timeout.
func TestEngineMCPSessionTimeoutExtraction(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedTimeout string
	}{
		{
			name: "extracts session-timeout from engine.mcp",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
					"mcp": map[string]any{
						"session-timeout": "4h",
					},
				},
			},
			expectedTimeout: "4h",
		},
		{
			name: "no mcp section - empty session timeout",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			expectedTimeout: "",
		},
		{
			name: "mcp section without session-timeout - empty",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":  "copilot",
					"mcp": map[string]any{},
				},
			},
			expectedTimeout: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config, _ := compiler.ExtractEngineConfig(tt.frontmatter)
			if config == nil {
				t.Fatal("Expected non-nil config")
			}
			if config.MCPSessionTimeout != tt.expectedTimeout {
				t.Errorf("MCPSessionTimeout = %q, want %q", config.MCPSessionTimeout, tt.expectedTimeout)
			}
		})
	}
}

// TestEngineMCPToolTimeoutExtraction tests extraction of engine.mcp.tool-timeout.
func TestEngineMCPToolTimeoutExtraction(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedTimeout string
	}{
		{
			name: "extracts tool-timeout from engine.mcp",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
					"mcp": map[string]any{
						"tool-timeout": "2m",
					},
				},
			},
			expectedTimeout: "2m",
		},
		{
			name: "no mcp section - empty tool timeout",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			expectedTimeout: "",
		},
		{
			name: "mcp section without tool-timeout - empty",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":  "copilot",
					"mcp": map[string]any{},
				},
			},
			expectedTimeout: "",
		},
		{
			name: "mcp section with both session-timeout and tool-timeout",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
					"mcp": map[string]any{
						"session-timeout": "4h",
						"tool-timeout":    "5m",
					},
				},
			},
			expectedTimeout: "5m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config, _ := compiler.ExtractEngineConfig(tt.frontmatter)
			if config == nil {
				t.Fatal("Expected non-nil config")
			}
			if config.MCPToolTimeout != tt.expectedTimeout {
				t.Errorf("MCPToolTimeout = %q, want %q", config.MCPToolTimeout, tt.expectedTimeout)
			}
		})
	}
}
