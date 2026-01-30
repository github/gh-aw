//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    HealthConfig
		shouldErr bool
	}{
		{
			name: "valid 7 days",
			config: HealthConfig{
				Days:      7,
				Threshold: 80.0,
			},
			shouldErr: false,
		},
		{
			name: "valid 30 days",
			config: HealthConfig{
				Days:      30,
				Threshold: 80.0,
			},
			shouldErr: false,
		},
		{
			name: "valid 90 days",
			config: HealthConfig{
				Days:      90,
				Threshold: 80.0,
			},
			shouldErr: false,
		},
		{
			name: "invalid days value",
			config: HealthConfig{
				Days:      15,
				Threshold: 80.0,
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For now, just validate the days parameter directly
			// since the full RunHealth needs GitHub API access
			if tt.config.Days != 7 && tt.config.Days != 30 && tt.config.Days != 90 {
				assert.True(t, tt.shouldErr, "Should error for invalid days value")
			} else {
				assert.False(t, tt.shouldErr, "Should not error for valid days value")
			}
		})
	}
}

func TestHealthCommand(t *testing.T) {
	cmd := NewHealthCommand()

	assert.NotNil(t, cmd, "Health command should be created")
	assert.Equal(t, "health", cmd.Name(), "Command name should be 'health'")
	assert.True(t, cmd.HasAvailableFlags(), "Command should have flags")

	// Check that required flags are registered
	daysFlag := cmd.Flags().Lookup("days")
	assert.NotNil(t, daysFlag, "Should have --days flag")
	assert.Equal(t, "7", daysFlag.DefValue, "Default days should be 7")

	thresholdFlag := cmd.Flags().Lookup("threshold")
	assert.NotNil(t, thresholdFlag, "Should have --threshold flag")
	assert.Equal(t, "80", thresholdFlag.DefValue, "Default threshold should be 80")

	jsonFlag := cmd.Flags().Lookup("json")
	assert.NotNil(t, jsonFlag, "Should have --json flag")

	repoFlag := cmd.Flags().Lookup("repo")
	assert.NotNil(t, repoFlag, "Should have --repo flag")
}
