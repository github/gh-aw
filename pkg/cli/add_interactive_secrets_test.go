//go:build !integration

package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddInteractiveConfig_getSecretInfo(t *testing.T) {
	tests := []struct {
		name            string
		engineOverride  string
		existingSecrets map[string]bool
		envVars         map[string]string
		wantName        string
		wantValueEmpty  bool
		wantErr         bool
	}{
		{
			name:           "copilot with token in env",
			engineOverride: "copilot",
			envVars: map[string]string{
				"COPILOT_GITHUB_TOKEN": "test-token-123",
			},
			wantName:       "COPILOT_GITHUB_TOKEN",
			wantValueEmpty: false,
			wantErr:        false,
		},
		{
			name:           "copilot secret already exists",
			engineOverride: "copilot",
			existingSecrets: map[string]bool{
				"COPILOT_GITHUB_TOKEN": true,
			},
			wantName:       "COPILOT_GITHUB_TOKEN",
			wantValueEmpty: true,
			wantErr:        false,
		},
		{
			name:           "claude with token in env",
			engineOverride: "claude",
			envVars: map[string]string{
				"ANTHROPIC_API_KEY": "test-api-key-456",
			},
			wantName:       "ANTHROPIC_API_KEY",
			wantValueEmpty: false,
			wantErr:        false,
		},
		{
			name:           "unknown engine",
			engineOverride: "unknown-engine",
			wantErr:        true,
		},
		{
			name:           "copilot with no token",
			engineOverride: "copilot",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for key, val := range tt.envVars {
				os.Setenv(key, val)
				defer os.Unsetenv(key)
			}

			config := &AddInteractiveConfig{
				EngineOverride:  tt.engineOverride,
				existingSecrets: tt.existingSecrets,
			}

			if config.existingSecrets == nil {
				config.existingSecrets = make(map[string]bool)
			}

			name, value, err := config.getSecretInfo()

			if tt.wantErr {
				assert.Error(t, err, "Expected error but got none")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tt.wantName, name, "Secret name should match")
				if tt.wantValueEmpty {
					assert.Empty(t, value, "Value should be empty when secret exists")
				} else {
					assert.NotEmpty(t, value, "Value should not be empty")
				}
			}
		})
	}
}

func TestAddInteractiveConfig_checkExistingSecrets(t *testing.T) {
	config := &AddInteractiveConfig{
		RepoOverride: "test-owner/test-repo",
	}

	// This test requires GitHub CLI access, so we just verify it doesn't panic
	// and initializes the existingSecrets map
	require.NotPanics(t, func() {
		_ = config.checkExistingSecrets()
	}, "checkExistingSecrets should not panic")

	assert.NotNil(t, config.existingSecrets, "existingSecrets map should be initialized")
}
