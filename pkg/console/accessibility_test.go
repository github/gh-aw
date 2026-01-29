//go:build !integration

package console

import (
	"os"
	"testing"
)

func TestIsAccessibleMode(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "ACCESSIBLE set",
			envVars:  map[string]string{"ACCESSIBLE": "1"},
			expected: true,
		},
		{
			name:     "TERM=dumb",
			envVars:  map[string]string{"TERM": "dumb"},
			expected: true,
		},
		{
			name:     "NO_COLOR set",
			envVars:  map[string]string{"NO_COLOR": "1"},
			expected: true,
		},
		{
			name:     "no accessibility indicators",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name:     "TERM not dumb",
			envVars:  map[string]string{"TERM": "xterm-256color"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			origAccessible := os.Getenv("ACCESSIBLE")
			origTerm := os.Getenv("TERM")
			origNoColor := os.Getenv("NO_COLOR")

			// Clean up after test
			defer func() {
				if origAccessible != "" {
					os.Setenv("ACCESSIBLE", origAccessible)
				} else {
					os.Unsetenv("ACCESSIBLE")
				}
				if origTerm != "" {
					os.Setenv("TERM", origTerm)
				} else {
					os.Unsetenv("TERM")
				}
				if origNoColor != "" {
					os.Setenv("NO_COLOR", origNoColor)
				} else {
					os.Unsetenv("NO_COLOR")
				}
			}()

			// Clear all relevant env vars first
			for _, key := range []string{"ACCESSIBLE", "TERM", "NO_COLOR"} {
				os.Unsetenv(key)
			}

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			result := IsAccessibleMode()
			if result != tt.expected {
				t.Errorf("IsAccessibleMode() = %v, want %v", result, tt.expected)
			}
		})
	}
}
