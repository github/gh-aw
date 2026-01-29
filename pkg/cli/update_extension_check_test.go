//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "GH_TOKEN environment variable error",
			output:   "gh: To use GitHub CLI in a GitHub Actions workflow, set the GH_TOKEN environment variable",
			expected: true,
		},
		{
			name:     "authentication required",
			output:   "error: authentication required",
			expected: true,
		},
		{
			name:     "invalid token",
			output:   "error: invalid token provided",
			expected: true,
		},
		{
			name:     "not authenticated",
			output:   "You are not authenticated to GitHub",
			expected: true,
		},
		{
			name:     "permission denied",
			output:   "error: permission denied",
			expected: true,
		},
		{
			name:     "successful check",
			output:   "✓ Successfully checked extension upgrades",
			expected: false,
		},
		{
			name:     "upgrade available",
			output:   "[agentics]: would have upgraded from v0.14.0 to v0.18.1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthenticationError(tt.output)
			assert.Equal(t, tt.expected, result, "isAuthenticationError() should return %v for: %s", tt.expected, tt.output)
		})
	}
}
