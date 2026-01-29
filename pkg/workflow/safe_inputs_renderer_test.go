//go:build !integration

package workflow

import (
	"testing"
)

func TestGetSafeInputsEnvVars(t *testing.T) {
	tests := []struct {
		name        string
		config      *SafeInputsConfig
		expectedLen int
		contains    []string
	}{
		{
			name:        "nil config",
			config:      nil,
			expectedLen: 0,
		},
		{
			name: "tool with env",
			config: &SafeInputsConfig{
				Tools: map[string]*SafeInputToolConfig{
					"test": {
						Name: "test",
						Env: map[string]string{
							"API_KEY": "${{ secrets.API_KEY }}",
							"TOKEN":   "${{ secrets.TOKEN }}",
						},
					},
				},
			},
			expectedLen: 2,
			contains:    []string{"API_KEY", "TOKEN"},
		},
		{
			name: "multiple tools with shared env",
			config: &SafeInputsConfig{
				Tools: map[string]*SafeInputToolConfig{
					"tool1": {
						Name: "tool1",
						Env:  map[string]string{"API_KEY": "key1"},
					},
					"tool2": {
						Name: "tool2",
						Env:  map[string]string{"API_KEY": "key2"},
					},
				},
			},
			expectedLen: 1, // Should deduplicate
			contains:    []string{"API_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSafeInputsEnvVars(tt.config)

			if len(result) != tt.expectedLen {
				t.Errorf("Expected %d env vars, got %d: %v", tt.expectedLen, len(result), result)
			}

			for _, expected := range tt.contains {
				found := false
				for _, v := range result {
					if v == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to contain %s, got %v", expected, result)
				}
			}
		})
	}
}

func TestCollectSafeInputsSecrets(t *testing.T) {
	tests := []struct {
		name        string
		config      *SafeInputsConfig
		expectedLen int
	}{
		{
			name:        "nil config",
			config:      nil,
			expectedLen: 0,
		},
		{
			name: "tool with secrets",
			config: &SafeInputsConfig{
				Tools: map[string]*SafeInputToolConfig{
					"test": {
						Name: "test",
						Env: map[string]string{
							"API_KEY": "${{ secrets.API_KEY }}",
						},
					},
				},
			},
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectSafeInputsSecrets(tt.config)

			if len(result) != tt.expectedLen {
				t.Errorf("Expected %d secrets, got %d", tt.expectedLen, len(result))
			}
		})
	}
}

func TestCollectSafeInputsSecretsStability(t *testing.T) {
	config := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"zebra-tool": {
				Name: "zebra-tool",
				Env: map[string]string{
					"ZEBRA_SECRET": "${{ secrets.ZEBRA }}",
					"ALPHA_SECRET": "${{ secrets.ALPHA }}",
				},
			},
			"alpha-tool": {
				Name: "alpha-tool",
				Env: map[string]string{
					"BETA_SECRET": "${{ secrets.BETA }}",
				},
			},
		},
	}

	// Test collectSafeInputsSecrets stability
	iterations := 10
	secretResults := make([]map[string]string, iterations)
	for i := 0; i < iterations; i++ {
		secretResults[i] = collectSafeInputsSecrets(config)
	}

	// All iterations should produce same key set
	for i := 1; i < iterations; i++ {
		if len(secretResults[i]) != len(secretResults[0]) {
			t.Errorf("collectSafeInputsSecrets produced different number of secrets on iteration %d", i+1)
		}
		for key, val := range secretResults[0] {
			if secretResults[i][key] != val {
				t.Errorf("collectSafeInputsSecrets produced different value for key %s on iteration %d", key, i+1)
			}
		}
	}
}

func TestGetSafeInputsEnvVarsStability(t *testing.T) {
	config := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"zebra-tool": {
				Name: "zebra-tool",
				Env: map[string]string{
					"ZEBRA_SECRET": "${{ secrets.ZEBRA }}",
					"ALPHA_SECRET": "${{ secrets.ALPHA }}",
				},
			},
			"alpha-tool": {
				Name: "alpha-tool",
				Env: map[string]string{
					"BETA_SECRET": "${{ secrets.BETA }}",
				},
			},
		},
	}

	// Test getSafeInputsEnvVars stability
	iterations := 10
	envResults := make([][]string, iterations)
	for i := 0; i < iterations; i++ {
		envResults[i] = getSafeInputsEnvVars(config)
	}

	for i := 1; i < iterations; i++ {
		if len(envResults[i]) != len(envResults[0]) {
			t.Errorf("getSafeInputsEnvVars produced different number of env vars on iteration %d", i+1)
		}
		for j := range envResults[0] {
			if envResults[i][j] != envResults[0][j] {
				t.Errorf("getSafeInputsEnvVars produced different value at position %d on iteration %d: expected %s, got %s",
					j, i+1, envResults[0][j], envResults[i][j])
			}
		}
	}
}
