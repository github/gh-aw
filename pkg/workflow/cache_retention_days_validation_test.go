//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestCacheMemoryRetentionDaysValidationObject tests retention-days boundary validation with object notation
func TestCacheMemoryRetentionDaysValidationObject(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int
		wantError     bool
		errorText     string
	}{
		{
			name:          "valid minimum (1 day)",
			retentionDays: 1,
			wantError:     false,
		},
		{
			name:          "valid middle value (30 days)",
			retentionDays: 30,
			wantError:     false,
		},
		{
			name:          "valid maximum (90 days)",
			retentionDays: 90,
			wantError:     false,
		},
		{
			name:          "invalid zero",
			retentionDays: 0,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got 0",
		},
		{
			name:          "invalid negative",
			retentionDays: -1,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got -1",
		},
		{
			name:          "invalid exceeds maximum (91 days)",
			retentionDays: 91,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got 91",
		},
		{
			name:          "invalid large value (365 days)",
			retentionDays: 365,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got 365",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsMap := map[string]any{
				"cache-memory": map[string]any{
					"retention-days": tt.retentionDays,
				},
			}

			toolsConfig, err := ParseToolsConfig(toolsMap)
			if err != nil {
				t.Fatalf("Failed to parse tools config: %v", err)
			}

			compiler := NewCompiler()
			config, err := compiler.extractCacheMemoryConfig(toolsConfig)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorText) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorText, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if config == nil {
					t.Fatal("Expected non-nil config")
				}
				if len(config.Caches) != 1 {
					t.Fatalf("Expected 1 cache, got %d", len(config.Caches))
				}
				cache := config.Caches[0]
				if cache.RetentionDays == nil {
					t.Error("Expected RetentionDays to be set")
				} else if *cache.RetentionDays != tt.retentionDays {
					t.Errorf("Expected retention-days %d, got %d", tt.retentionDays, *cache.RetentionDays)
				}
			}
		})
	}
}

// TestCacheMemoryRetentionDaysValidationArray tests retention-days boundary validation with array notation
func TestCacheMemoryRetentionDaysValidationArray(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int
		wantError     bool
		errorText     string
	}{
		{
			name:          "valid minimum in array (1 day)",
			retentionDays: 1,
			wantError:     false,
		},
		{
			name:          "valid maximum in array (90 days)",
			retentionDays: 90,
			wantError:     false,
		},
		{
			name:          "invalid zero in array",
			retentionDays: 0,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got 0",
		},
		{
			name:          "invalid exceeds maximum in array (100 days)",
			retentionDays: 100,
			wantError:     true,
			errorText:     "retention-days must be between 1 and 90, got 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsMap := map[string]any{
				"cache-memory": []any{
					map[string]any{
						"id":             "test-cache",
						"retention-days": tt.retentionDays,
					},
				},
			}

			toolsConfig, err := ParseToolsConfig(toolsMap)
			if err != nil {
				t.Fatalf("Failed to parse tools config: %v", err)
			}

			compiler := NewCompiler()
			config, err := compiler.extractCacheMemoryConfig(toolsConfig)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorText) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorText, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if config == nil {
					t.Fatal("Expected non-nil config")
				}
				if len(config.Caches) != 1 {
					t.Fatalf("Expected 1 cache, got %d", len(config.Caches))
				}
				cache := config.Caches[0]
				if cache.RetentionDays == nil {
					t.Error("Expected RetentionDays to be set")
				} else if *cache.RetentionDays != tt.retentionDays {
					t.Errorf("Expected retention-days %d, got %d", tt.retentionDays, *cache.RetentionDays)
				}
			}
		})
	}
}

// TestCacheMemoryRetentionDaysNoValue tests that omitting retention-days does not cause an error
func TestCacheMemoryRetentionDaysNoValue(t *testing.T) {
	toolsMap := map[string]any{
		"cache-memory": map[string]any{
			"key": "my-cache-key",
		},
	}

	toolsConfig, err := ParseToolsConfig(toolsMap)
	if err != nil {
		t.Fatalf("Failed to parse tools config: %v", err)
	}

	compiler := NewCompiler()
	config, err := compiler.extractCacheMemoryConfig(toolsConfig)

	if err != nil {
		t.Errorf("Expected no error when retention-days is omitted, got: %v", err)
	}

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if len(config.Caches) != 1 {
		t.Fatalf("Expected 1 cache, got %d", len(config.Caches))
	}

	cache := config.Caches[0]
	if cache.RetentionDays != nil {
		t.Errorf("Expected RetentionDays to be nil when not specified, got %d", *cache.RetentionDays)
	}
}
