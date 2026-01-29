//go:build !integration

package cli

import (
	"testing"
)

func TestConvertToGitHubActionsEnv(t *testing.T) {
	tests := []struct {
		name        string
		input       any
		envMetadata []EnvironmentVariable
		expected    map[string]string
	}{
		{
			name: "shell syntax conversion with secret metadata",
			input: map[string]any{
				"API_TOKEN":    "${API_TOKEN}",
				"NOTION_TOKEN": "${NOTION_TOKEN}",
			},
			envMetadata: []EnvironmentVariable{
				{Name: "API_TOKEN", IsSecret: true},
				{Name: "NOTION_TOKEN", IsSecret: true},
			},
			expected: map[string]string{
				"API_TOKEN":    "${{ secrets.API_TOKEN }}",
				"NOTION_TOKEN": "${{ secrets.NOTION_TOKEN }}",
			},
		},
		{
			name: "shell syntax conversion with mixed secret and env metadata",
			input: map[string]any{
				"API_TOKEN": "${API_TOKEN}",
				"LOG_LEVEL": "${LOG_LEVEL}",
			},
			envMetadata: []EnvironmentVariable{
				{Name: "API_TOKEN", IsSecret: true},
				{Name: "LOG_LEVEL", IsSecret: false},
			},
			expected: map[string]string{
				"API_TOKEN": "${{ secrets.API_TOKEN }}",
				"LOG_LEVEL": "${{ env.LOG_LEVEL }}",
			},
		},
		{
			name: "shell syntax conversion without metadata defaults to secrets",
			input: map[string]any{
				"API_TOKEN":    "${API_TOKEN}",
				"NOTION_TOKEN": "${NOTION_TOKEN}",
			},
			envMetadata: []EnvironmentVariable{},
			expected: map[string]string{
				"API_TOKEN":    "${{ secrets.API_TOKEN }}",
				"NOTION_TOKEN": "${{ secrets.NOTION_TOKEN }}",
			},
		},
		{
			name: "mixed syntax",
			input: map[string]any{
				"API_TOKEN":  "${API_TOKEN}",
				"PLAIN_VAR":  "plain_value",
				"GITHUB_VAR": "${{ secrets.EXISTING }}",
			},
			envMetadata: []EnvironmentVariable{
				{Name: "API_TOKEN", IsSecret: true},
			},
			expected: map[string]string{
				"API_TOKEN":  "${{ secrets.API_TOKEN }}",
				"PLAIN_VAR":  "plain_value",
				"GITHUB_VAR": "${{ secrets.EXISTING }}",
			},
		},
		{
			name: "no shell syntax",
			input: map[string]any{
				"PLAIN_VAR": "plain_value",
				"NUMBER":    "123",
			},
			envMetadata: []EnvironmentVariable{},
			expected: map[string]string{
				"PLAIN_VAR": "plain_value",
				"NUMBER":    "123",
			},
		},
		{
			name:        "empty input",
			input:       map[string]any{},
			envMetadata: []EnvironmentVariable{},
			expected:    map[string]string{},
		},
		{
			name:        "nil input",
			input:       nil,
			envMetadata: []EnvironmentVariable{},
			expected:    map[string]string{},
		},
		{
			name: "non-string values ignored",
			input: map[string]any{
				"STRING_VAR": "${TOKEN}",
				"INT_VAR":    123,
				"BOOL_VAR":   true,
			},
			envMetadata: []EnvironmentVariable{
				{Name: "TOKEN", IsSecret: true},
			},
			expected: map[string]string{
				"STRING_VAR": "${{ secrets.TOKEN }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToGitHubActionsEnv(tt.input, tt.envMetadata)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d environment variables, got %d", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				if actualValue, exists := result[key]; !exists {
					t.Errorf("Expected key '%s' not found in result", key)
				} else if actualValue != expectedValue {
					t.Errorf("For key '%s', expected '%s', got '%s'", key, expectedValue, actualValue)
				}
			}
		})
	}
}
