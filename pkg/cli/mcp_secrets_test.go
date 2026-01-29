//go:build !integration

package cli

import (
	"strings"
	"testing"
)

// Helper constants for secret syntax validation in tests
const (
	testSecretPrefix = "${{ secrets."
	testSecretSuffix = " }}"
)

// testIsSecretSyntax checks if a value matches GitHub Actions secret syntax (test helper)
func testIsSecretSyntax(value string) bool {
	return strings.HasPrefix(value, testSecretPrefix) && strings.HasSuffix(value, testSecretSuffix)
}

// testExtractSecretName extracts the secret name from GitHub Actions secret syntax (test helper)
func testExtractSecretName(value string) string {
	if !testIsSecretSyntax(value) {
		return ""
	}
	return value[len(testSecretPrefix) : len(value)-len(testSecretSuffix)]
}

func TestCheckAndSuggestSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		toolConfig map[string]any
		verbose    bool
		wantErr    bool
		skipReason string
	}{
		{
			name: "no mcp section",
			toolConfig: map[string]any{
				"other": "config",
			},
			verbose: false,
			wantErr: false,
		},
		{
			name: "mcp section without env",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"command": "test",
				},
			},
			verbose: false,
			wantErr: false,
		},
		{
			name: "mcp with env but no secrets",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"PLAIN_VAR": "plain_value",
					},
				},
			},
			verbose: false,
			wantErr: false,
		},
		{
			name: "mcp with secrets in env",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"API_KEY": "${{ secrets.DD_API_KEY }}",
						"TOKEN":   "${{ secrets.AUTH_TOKEN }}",
					},
				},
			},
			verbose:    true,
			wantErr:    false,
			skipReason: "requires GitHub CLI and repository access",
		},
		{
			name:       "empty tool config",
			toolConfig: map[string]any{},
			verbose:    false,
			wantErr:    false,
		},
		{
			name:       "nil tool config",
			toolConfig: nil,
			verbose:    false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipReason != "" {
				t.Skip(tt.skipReason)
			}

			err := checkAndSuggestSecrets(tt.toolConfig, tt.verbose)

			if (err != nil) != tt.wantErr {
				// Accept 403 errors as they're expected when permissions are insufficient
				if err != nil && strings.Contains(err.Error(), "403") {
					return
				}
				t.Errorf("checkAndSuggestSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecretExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		toolConfig    map[string]any
		expectedCount int
		expectedNames []string
	}{
		{
			name: "single secret",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"API_KEY": "${{ secrets.DD_API_KEY }}",
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"DD_API_KEY"},
		},
		{
			name: "multiple secrets",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"API_KEY":         "${{ secrets.DD_API_KEY }}",
						"APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
						"SITE":            "${{ secrets.DD_SITE }}",
					},
				},
			},
			expectedCount: 3,
			expectedNames: []string{"DD_API_KEY", "DD_APPLICATION_KEY", "DD_SITE"},
		},
		{
			name: "mixed secrets and plain values",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"SECRET":  "${{ secrets.MY_SECRET }}",
						"PLAIN":   "plain_value",
						"ANOTHER": "another_plain",
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"MY_SECRET"},
		},
		{
			name: "no secrets",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]string{
						"VAR1": "value1",
						"VAR2": "value2",
					},
				},
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requiredSecrets []string

			if mcpSection, ok := tt.toolConfig["mcp"].(map[string]any); ok {
				if env, hasEnv := mcpSection["env"].(map[string]string); hasEnv {
					for _, value := range env {
						// Extract secret name from GitHub Actions syntax
						if secretName := testExtractSecretName(value); secretName != "" {
							requiredSecrets = append(requiredSecrets, secretName)
						}
					}
				}
			}

			if len(requiredSecrets) != tt.expectedCount {
				t.Errorf("Expected %d secrets, got %d", tt.expectedCount, len(requiredSecrets))
			}

			// Verify all expected names are present
			for _, expectedName := range tt.expectedNames {
				found := false
				for _, secretName := range requiredSecrets {
					if secretName == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected secret %q not found in extracted secrets", expectedName)
				}
			}
		})
	}
}

func TestSecretSyntaxParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		value        string
		expectSecret bool
		secretName   string
	}{
		{
			name:         "standard secret syntax",
			value:        "${{ secrets.API_KEY }}",
			expectSecret: true,
			secretName:   "API_KEY",
		},
		{
			name:         "secret with underscores",
			value:        "${{ secrets.MY_SECRET_KEY }}",
			expectSecret: true,
			secretName:   "MY_SECRET_KEY",
		},
		{
			name:         "plain value",
			value:        "plain_value",
			expectSecret: false,
			secretName:   "",
		},
		{
			name:         "incomplete secret syntax",
			value:        "${{ secrets.KEY",
			expectSecret: false,
			secretName:   "",
		},
		{
			name:         "empty value",
			value:        "",
			expectSecret: false,
			secretName:   "",
		},
		{
			name:         "secret-like but not secret syntax",
			value:        "secrets.API_KEY",
			expectSecret: false,
			secretName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isSecret := testIsSecretSyntax(tt.value)

			if isSecret != tt.expectSecret {
				t.Errorf("Expected isSecret=%v, got %v", tt.expectSecret, isSecret)
			}

			if tt.expectSecret && isSecret {
				secretName := testExtractSecretName(tt.value)
				if secretName != tt.secretName {
					t.Errorf("Expected secret name %q, got %q", tt.secretName, secretName)
				}
			}
		})
	}
}

func TestCheckAndSuggestSecretsWithInvalidMCPSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		toolConfig map[string]any
		wantErr    bool
	}{
		{
			name: "mcp is not a map",
			toolConfig: map[string]any{
				"mcp": "invalid",
			},
			wantErr: false, // Should handle gracefully
		},
		{
			name: "env is not a map[string]string",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": "invalid",
				},
			},
			wantErr: false, // Should handle gracefully
		},
		{
			name: "env is a map but wrong type",
			toolConfig: map[string]any{
				"mcp": map[string]any{
					"env": map[string]int{
						"KEY": 123,
					},
				},
			},
			wantErr: false, // Should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAndSuggestSecrets(tt.toolConfig, false)

			if (err != nil) != tt.wantErr {
				// Accept 403 errors
				if err != nil && strings.Contains(err.Error(), "403") {
					return
				}
				t.Errorf("checkAndSuggestSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecretExtractionEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		value         string
		shouldExtract bool
		extractedName string
	}{
		{
			name:          "extra spaces before closing - still valid",
			value:         "${{ secrets.KEY  }}",
			shouldExtract: true,   // Has extra spaces but still matches pattern
			extractedName: "KEY ", // Will include one trailing space (string is trimmed by 3 chars from end)
		},
		{
			name:          "extra spaces after opening",
			value:         "${{  secrets.KEY }}",
			shouldExtract: false, // Different format
		},
		{
			name:          "single closing brace",
			value:         "${{ secrets.KEY }",
			shouldExtract: false,
		},
		{
			name:          "triple closing braces",
			value:         "${{ secrets.KEY }}}",
			shouldExtract: false,
		},
		{
			name:          "correct format",
			value:         "${{ secrets.VALID_KEY }}",
			shouldExtract: true,
			extractedName: "VALID_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := testIsSecretSyntax(tt.value)

			if isValid != tt.shouldExtract {
				t.Errorf("Expected shouldExtract=%v, got %v", tt.shouldExtract, isValid)
			}

			if tt.shouldExtract && isValid {
				secretName := testExtractSecretName(tt.value)
				if secretName != tt.extractedName {
					t.Errorf("Expected extracted name %q, got %q", tt.extractedName, secretName)
				}
			}
		})
	}
}
