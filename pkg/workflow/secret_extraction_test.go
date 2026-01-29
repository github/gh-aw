//go:build !integration

package workflow

import (
	"testing"
)

// TestSharedExtractSecretName tests the shared ExtractSecretName utility function
func TestSharedExtractSecretName(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "simple secret",
			value:    "${{ secrets.DD_API_KEY }}",
			expected: "DD_API_KEY",
		},
		{
			name:     "secret with default value",
			value:    "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			expected: "DD_SITE",
		},
		{
			name:     "secret with spaces",
			value:    "${{  secrets.API_TOKEN  }}",
			expected: "API_TOKEN",
		},
		{
			name:     "bearer token",
			value:    "Bearer ${{ secrets.TAVILY_API_KEY }}",
			expected: "TAVILY_API_KEY",
		},
		{
			name:     "no secret",
			value:    "plain value",
			expected: "",
		},
		{
			name:     "empty value",
			value:    "",
			expected: "",
		},
		{
			name:     "secret with underscore",
			value:    "${{ secrets.MY_SECRET_KEY }}",
			expected: "MY_SECRET_KEY",
		},
		{
			name:     "secret with numbers",
			value:    "${{ secrets.API_KEY_123 }}",
			expected: "API_KEY_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretName(tt.value)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestSharedExtractSecretsFromValue tests the shared ExtractSecretsFromValue utility function
func TestSharedExtractSecretsFromValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected map[string]string
	}{
		{
			name:  "simple secret",
			value: "${{ secrets.DD_API_KEY }}",
			expected: map[string]string{
				"DD_API_KEY": "${{ secrets.DD_API_KEY }}",
			},
		},
		{
			name:  "secret with default value",
			value: "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			expected: map[string]string{
				"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
		},
		{
			name:  "bearer token",
			value: "Bearer ${{ secrets.TAVILY_API_KEY }}",
			expected: map[string]string{
				"TAVILY_API_KEY": "${{ secrets.TAVILY_API_KEY }}",
			},
		},
		{
			name:  "multiple secrets in one value",
			value: "${{ secrets.KEY1 }} and ${{ secrets.KEY2 }}",
			expected: map[string]string{
				"KEY1": "${{ secrets.KEY1 }}",
				"KEY2": "${{ secrets.KEY2 }}",
			},
		},
		{
			name:     "no secrets",
			value:    "plain value",
			expected: map[string]string{},
		},
		{
			name:     "empty value",
			value:    "",
			expected: map[string]string{},
		},
		{
			name:  "secret with complex default",
			value: "${{ secrets.CONFIG || 'default-config-value' }}",
			expected: map[string]string{
				"CONFIG": "${{ secrets.CONFIG || 'default-config-value' }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretsFromValue(tt.value)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
			}

			for varName, expr := range tt.expected {
				if result[varName] != expr {
					t.Errorf("Expected secret %q to have expression %q, got %q", varName, expr, result[varName])
				}
			}
		})
	}
}

// TestSharedExtractSecretsFromMap tests the shared ExtractSecretsFromMap utility function
func TestSharedExtractSecretsFromMap(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]string
		expected map[string]string
	}{
		{
			name: "HTTP headers with secrets",
			values: map[string]string{
				"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
				"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
				"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
			expected: map[string]string{
				"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
				"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
				"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
		},
		{
			name: "env vars with secrets",
			values: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
				"TOKEN":   "${{ secrets.TOKEN }}",
			},
			expected: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
				"TOKEN":   "${{ secrets.TOKEN }}",
			},
		},
		{
			name: "mixed secrets and plain values",
			values: map[string]string{
				"Authorization": "Bearer ${{ secrets.AUTH_TOKEN }}",
				"Content-Type":  "application/json",
				"API_KEY":       "${{ secrets.API_KEY }}",
			},
			expected: map[string]string{
				"AUTH_TOKEN": "${{ secrets.AUTH_TOKEN }}",
				"API_KEY":    "${{ secrets.API_KEY }}",
			},
		},
		{
			name: "no secrets",
			values: map[string]string{
				"SIMPLE_VAR": "plain value",
			},
			expected: map[string]string{},
		},
		{
			name: "duplicate secrets (same secret in multiple values)",
			values: map[string]string{
				"Header1": "${{ secrets.API_KEY }}",
				"Header2": "${{ secrets.API_KEY }}",
			},
			expected: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
			},
		},
		{
			name:     "empty map",
			values:   map[string]string{},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretsFromMap(tt.values)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
			}

			for varName, expr := range tt.expected {
				if result[varName] != expr {
					t.Errorf("Expected secret %q to have expression %q, got %q", varName, expr, result[varName])
				}
			}
		})
	}
}

// TestSharedReplaceSecretsWithEnvVars tests the shared ReplaceSecretsWithEnvVars utility function
func TestSharedReplaceSecretsWithEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		secrets  map[string]string
		expected string
	}{
		{
			name:  "simple replacement",
			value: "${{ secrets.DD_API_KEY }}",
			secrets: map[string]string{
				"DD_API_KEY": "${{ secrets.DD_API_KEY }}",
			},
			expected: "\\${DD_API_KEY}",
		},
		{
			name:  "replacement with default value",
			value: "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			secrets: map[string]string{
				"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
			expected: "\\${DD_SITE}",
		},
		{
			name:  "bearer token replacement",
			value: "Bearer ${{ secrets.TAVILY_API_KEY }}",
			secrets: map[string]string{
				"TAVILY_API_KEY": "${{ secrets.TAVILY_API_KEY }}",
			},
			expected: "Bearer \\${TAVILY_API_KEY}",
		},
		{
			name:  "multiple replacements",
			value: "${{ secrets.KEY1 }} and ${{ secrets.KEY2 }}",
			secrets: map[string]string{
				"KEY1": "${{ secrets.KEY1 }}",
				"KEY2": "${{ secrets.KEY2 }}",
			},
			expected: "\\${KEY1} and \\${KEY2}",
		},
		{
			name:     "no replacements",
			value:    "plain value",
			secrets:  map[string]string{},
			expected: "plain value",
		},
		{
			name:  "partial replacement",
			value: "${{ secrets.API_KEY }} and plain text",
			secrets: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
			},
			expected: "\\${API_KEY} and plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReplaceSecretsWithEnvVars(tt.value, tt.secrets)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestSharedExtractSecretsFromValueEdgeCases tests edge cases for the shared ExtractSecretsFromValue utility function
func TestSharedExtractSecretsFromValueEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected map[string]string
	}{
		{
			name:     "malformed expression - missing closing braces",
			value:    "${{ secrets.KEY",
			expected: map[string]string{},
		},
		{
			name:     "malformed expression - missing opening braces",
			value:    "secrets.KEY }}",
			expected: map[string]string{},
		},
		{
			name:     "incomplete expression",
			value:    "${{ secrets.",
			expected: map[string]string{},
		},
		{
			name:  "secret name with trailing space before pipe",
			value: "${{ secrets.KEY  || 'default' }}",
			expected: map[string]string{
				"KEY": "${{ secrets.KEY  || 'default' }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretsFromValue(tt.value)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
			}

			for varName, expr := range tt.expected {
				if result[varName] != expr {
					t.Errorf("Expected secret %q to have expression %q, got %q", varName, expr, result[varName])
				}
			}
		})
	}
}
