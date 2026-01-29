//go:build !integration

package cli

import (
	"testing"
)

func TestIsRunningInCI(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "not running in CI - no env vars set",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name: "running in CI - CI env var set",
			envVars: map[string]string{
				"CI": "true",
			},
			expected: true,
		},
		{
			name: "running in CI - CONTINUOUS_INTEGRATION env var set",
			envVars: map[string]string{
				"CONTINUOUS_INTEGRATION": "true",
			},
			expected: true,
		},
		{
			name: "running in CI - GITHUB_ACTIONS env var set",
			envVars: map[string]string{
				"GITHUB_ACTIONS": "true",
			},
			expected: true,
		},
		{
			name: "running in CI - multiple env vars set",
			envVars: map[string]string{
				"CI":                     "true",
				"CONTINUOUS_INTEGRATION": "true",
				"GITHUB_ACTIONS":         "true",
			},
			expected: true,
		},
		{
			name: "running in CI - CI env var set to empty string is still truthy",
			envVars: map[string]string{
				"CI": "",
			},
			expected: false,
		},
		{
			name: "running in CI - other env vars don't affect result",
			envVars: map[string]string{
				"SOME_OTHER_VAR": "value",
				"PATH":           "/usr/bin",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all CI-related env vars first
			ciVars := []string{"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS"}
			for _, v := range ciVars {
				t.Setenv(v, "")
			}

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Run test
			result := IsRunningInCI()
			if result != tt.expected {
				t.Errorf("IsRunningInCI() = %v, want %v", result, tt.expected)
			}
		})
	}
}
