//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheMemorySyntaxVariations(t *testing.T) {
	tests := []struct {
		name        string
		cacheValue  any
		shouldWork  bool
		description string
	}{
		{
			name:        "cache-memory with nil (no value)",
			cacheValue:  nil,
			shouldWork:  true,
			description: "Should enable cache-memory when field is present without value",
		},
		{
			name:        "cache-memory with true",
			cacheValue:  true,
			shouldWork:  true,
			description: "Should enable cache-memory with boolean true",
		},
		{
			name:        "cache-memory with false",
			cacheValue:  false,
			shouldWork:  true, // Still valid, just disabled
			description: "Should disable cache-memory with boolean false",
		},
		{
			name: "cache-memory with object",
			cacheValue: map[string]any{
				"key": "custom-key",
			},
			shouldWork:  true,
			description: "Should enable cache-memory with custom configuration",
		},
		{
			name:        "cache-memory with empty object",
			cacheValue:  map[string]any{},
			shouldWork:  true,
			description: "Should enable cache-memory with empty object using defaults",
		},
		{
			name: "cache-memory with array - single cache",
			cacheValue: []any{
				map[string]any{
					"id":  "default",
					"key": "custom-key",
				},
			},
			shouldWork:  true,
			description: "Should enable cache-memory with array notation (single cache)",
		},
		{
			name: "cache-memory with array - multiple caches",
			cacheValue: []any{
				map[string]any{
					"id":  "default",
					"key": "memory-default",
				},
				map[string]any{
					"id":  "session",
					"key": "memory-session",
					"validation": map[string]any{
						"json-schemas": []any{
							map[string]any{
								"file":   "state.json",
								"format": "json",
								"schema": map[string]any{"type": "object"},
							},
						},
					},
				},
			},
			shouldWork:  true,
			description: "Should enable cache-memory with array notation (multiple caches)",
		},
		{
			name: "cache-memory with array - no explicit ID",
			cacheValue: []any{
				map[string]any{
					"key": "custom-key",
				},
			},
			shouldWork:  true,
			description: "Should use 'default' as ID when not specified in array",
		},
		{
			name: "cache-memory with array - duplicate IDs",
			cacheValue: []any{
				map[string]any{
					"id":  "session",
					"key": "key1",
				},
				map[string]any{
					"id":  "session",
					"key": "key2",
				},
			},
			shouldWork:  false,
			description: "Should fail with duplicate cache IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			tools := map[string]any{
				"cache-memory": tt.cacheValue,
			}

			config, err := compiler.extractCacheMemoryConfigFromMap(tools)

			// Check if error matches expectation
			if !tt.shouldWork {
				if err == nil {
					t.Errorf("Expected error for %s but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tt.description, err)
			}

			if tt.cacheValue == nil || tt.cacheValue == true {
				// Should create a single default cache
				if config == nil {
					t.Errorf("Expected non-nil config for %s", tt.description)
					return
				}
				if len(config.Caches) != 1 {
					t.Errorf("Expected 1 cache, got %d for %s", len(config.Caches), tt.description)
					return
				}
				if config.Caches[0].ID != "default" {
					t.Errorf("Expected cache ID 'default', got '%s' for %s", config.Caches[0].ID, tt.description)
				}
				if config.Caches[0].Key == "" {
					t.Errorf("Expected default Key to be set for %s", tt.description)
				}
			} else if tt.cacheValue == false {
				// Should create empty config (disabled)
				if config == nil {
					t.Errorf("Expected non-nil config for %s", tt.description)
					return
				}
				if len(config.Caches) != 0 {
					t.Errorf("Expected 0 caches (disabled), got %d for %s", len(config.Caches), tt.description)
				}
			} else if cacheArray, ok := tt.cacheValue.([]any); ok {
				if config == nil {
					t.Errorf("Expected non-nil config for %s", tt.description)
					return
				}
				if len(config.Caches) != len(cacheArray) {
					t.Errorf("Expected %d caches, got %d for %s", len(cacheArray), len(config.Caches), tt.description)
				}
				for i, cache := range config.Caches {
					if cache.ID == "" {
						t.Errorf("Expected cache ID to be set for cache %d in %s", i, tt.description)
					}
					if cache.Key == "" {
						t.Errorf("Expected cache Key to be set for cache %d in %s", i, tt.description)
					}
				}
			} else if configMap, ok := tt.cacheValue.(map[string]any); ok {
				// Object config should create a single default cache
				if config == nil {
					t.Errorf("Expected non-nil config for %s", tt.description)
					return
				}
				if len(config.Caches) != 1 {
					t.Errorf("Expected 1 cache, got %d for object config: %s", len(config.Caches), tt.description)
					return
				}
				if config.Caches[0].ID != "default" {
					t.Errorf("Expected cache ID 'default', got '%s' for %s", config.Caches[0].ID, tt.description)
				}
				if customKey, hasKey := configMap["key"]; hasKey {
					expectedKey := customKey.(string) + "-${{ github.run_id }}"
					if config.Caches[0].Key != expectedKey {
						t.Errorf("Expected Key=%s, got %s", expectedKey, config.Caches[0].Key)
					}
				}
			}
		})
	}
}

func TestCacheMemoryValidationConfigAndGeneratedSteps(t *testing.T) {
	compiler := NewCompiler()
	config, err := compiler.extractCacheMemoryConfigFromMap(map[string]any{
		"cache-memory": []any{
			map[string]any{
				"id":  "default",
				"key": "memory-default",
				"validation": map[string]any{
					"script":          "if (!fs.existsSync(path.join(memoryRoot, 'index.json'))) throw new Error('missing index');",
					"timeout-minutes": 1,
				},
			},
			map[string]any{
				"id":  "session",
				"key": "memory-session",
				"validation": map[string]any{
					"json-schemas": []any{
						map[string]any{
							"file":   "state.json",
							"format": "json",
							"schema": map[string]any{"type": "object"},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Caches, 2)
	require.NotNil(t, config.Caches[0].Validation)
	assert.Equal(t, 1, config.Caches[0].Validation.TimeoutMinutes)
	require.NotNil(t, config.Caches[1].Validation)
	assert.Empty(t, config.Caches[1].Validation.Script)

	data := &WorkflowData{
		CacheMemoryConfig: config,
		SafeOutputs:       &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}

	var validation strings.Builder
	generateCacheMemoryValidation(&validation, data)
	validationYAML := validation.String()
	assert.Contains(t, validationYAML, "Validate cache-memory file types and domain content")
	assert.Contains(t, validationYAML, "VALIDATION_SCRIPT_B64:")
	assert.Contains(t, validationYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'")
	assert.Contains(t, validationYAML, "validate_memory_step.cjs")
	assert.Contains(t, validationYAML, "id: "+cacheMemoryValidationStepID("default"))
	assert.Contains(t, validationYAML, "id: "+cacheMemoryValidationStepID("session"))

	var upload strings.Builder
	generateCacheMemoryArtifactUpload(&upload, data, getActionPin)
	uploadYAML := upload.String()
	assert.Contains(t, uploadYAML, "steps."+cacheMemoryValidationStepID("default")+".outcome == 'success'")
	assert.Contains(t, uploadYAML, "steps."+cacheMemoryValidationStepID("session")+".outcome == 'success'")

	job, err := compiler.buildUpdateCacheMemoryJob(data, true)
	require.NoError(t, err)
	require.NotNil(t, job)
	updateYAML := strings.Join(job.Steps, "\n")
	assert.Contains(t, updateYAML, "Validate cache-memory before save (default)")
	assert.Contains(t, updateYAML, "VALIDATION_TIMEOUT_SECONDS: 60")
	assert.Contains(t, updateYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'")
	assert.Contains(t, updateYAML, "validate_memory_step.cjs")
	assert.Contains(t, updateYAML, "steps."+cacheMemoryValidationStepID("default")+".outcome == 'success'")
	assert.Contains(t, updateYAML, "steps."+cacheMemoryValidationStepID("session")+".outcome == 'success'")
}

func TestCacheMemoryValidationStepIDsDoNotCollide(t *testing.T) {
	hyphenID := cacheMemoryValidationStepID("my-cache")
	underscoreID := cacheMemoryValidationStepID("my_cache")

	assert.NotEqual(t, hyphenID, underscoreID)
	assert.Equal(t, "validate_cache_memory_6d792d6361636865", hyphenID)
	assert.Equal(t, "validate_cache_memory_6d795f6361636865", underscoreID)
}
