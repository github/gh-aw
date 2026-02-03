//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/types"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestExtractSecretName(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := workflow.ExtractSecretName(tt.value)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractSecretsFromConfig(t *testing.T) {
	tests := []struct {
		name            string
		config          parser.MCPServerConfig
		expectedSecrets []string
	}{
		{
			name: "HTTP headers with secrets",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				Headers: map[string]string{
					"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
					"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
					"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
				}}, Name: "datadog",
			},
			expectedSecrets: []string{"DD_API_KEY", "DD_APPLICATION_KEY", "DD_SITE"},
		},
		{
			name: "env vars with secrets",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"API_KEY": "${{ secrets.API_KEY }}",
					"TOKEN":   "${{ secrets.TOKEN }}",
				}}, Name: "test-server",
			},
			expectedSecrets: []string{"API_KEY", "TOKEN"},
		},
		{
			name: "mixed headers and env",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				Headers: map[string]string{
					"Authorization": "Bearer ${{ secrets.AUTH_TOKEN }}",
				},
				Env: map[string]string{
					"API_KEY": "${{ secrets.API_KEY }}",
				}}, Name: "mixed-server",
			},
			expectedSecrets: []string{"AUTH_TOKEN", "API_KEY"},
		},
		{
			name: "no secrets",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"SIMPLE_VAR": "plain value",
				}}, Name: "simple-server",
			},
			expectedSecrets: []string{},
		},
		{
			name: "duplicate secrets (should only appear once)",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				Headers: map[string]string{
					"Header1": "${{ secrets.API_KEY }}",
					"Header2": "${{ secrets.API_KEY }}",
				}}, Name: "duplicate-server",
			},
			expectedSecrets: []string{"API_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := extractSecretsFromConfig(tt.config)

			if len(secrets) != len(tt.expectedSecrets) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expectedSecrets), len(secrets))
			}

			// Create a map of expected secrets for easier lookup
			expectedMap := make(map[string]bool)
			for _, name := range tt.expectedSecrets {
				expectedMap[name] = true
			}

			// Check that all extracted secrets are expected
			for _, secret := range secrets {
				if !expectedMap[secret.Name] {
					t.Errorf("Unexpected secret: %s", secret.Name)
				}
			}

			// Check that all expected secrets were extracted
			actualMap := make(map[string]bool)
			for _, secret := range secrets {
				actualMap[secret.Name] = true
			}
			for _, expected := range tt.expectedSecrets {
				if !actualMap[expected] {
					t.Errorf("Missing expected secret: %s", expected)
				}
			}
		})
	}
}

func TestCheckSecretsAvailability(t *testing.T) {
	tests := []struct {
		name         string
		secrets      []SecretInfo
		envVars      map[string]string
		useActions   bool
		expectSource map[string]string // Map of secret name to expected source
	}{
		{
			name: "secret in environment",
			secrets: []SecretInfo{
				{Name: "TEST_SECRET", EnvKey: "TEST_SECRET"},
			},
			envVars: map[string]string{
				"TEST_SECRET": "test-value",
			},
			useActions: false,
			expectSource: map[string]string{
				"TEST_SECRET": "env",
			},
		},
		{
			name: "secret not found",
			secrets: []SecretInfo{
				{Name: "MISSING_SECRET", EnvKey: "MISSING_SECRET"},
			},
			envVars:    map[string]string{},
			useActions: false,
			expectSource: map[string]string{
				"MISSING_SECRET": "",
			},
		},
		{
			name: "multiple secrets mixed availability",
			secrets: []SecretInfo{
				{Name: "AVAILABLE_SECRET", EnvKey: "AVAILABLE_SECRET"},
				{Name: "MISSING_SECRET", EnvKey: "MISSING_SECRET"},
			},
			envVars: map[string]string{
				"AVAILABLE_SECRET": "value",
			},
			useActions: false,
			expectSource: map[string]string{
				"AVAILABLE_SECRET": "env",
				"MISSING_SECRET":   "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			result := checkSecretsAvailability(tt.secrets, tt.useActions)

			for _, secret := range result {
				expectedSource, exists := tt.expectSource[secret.Name]
				if !exists {
					t.Errorf("Unexpected secret in result: %s", secret.Name)
					continue
				}

				if secret.Source != expectedSource {
					t.Errorf("Secret %s: expected source %q, got %q", secret.Name, expectedSource, secret.Source)
				}

				if expectedSource != "" && !secret.Available {
					t.Errorf("Secret %s should be available but is marked as not available", secret.Name)
				}
				if expectedSource == "" && secret.Available {
					t.Errorf("Secret %s should not be available but is marked as available", secret.Name)
				}
			}
		})
	}
}
