//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRepositoryFormatErrorMessages verifies that repository format errors include examples
func TestRepositoryFormatErrorMessages(t *testing.T) {
	tests := []struct {
		name            string
		repo            string
		expectError     bool
		expectInMessage []string
	}{
		{
			name:        "invalid format - no slash",
			repo:        "invalidrepo",
			expectError: true,
			expectInMessage: []string{
				"invalid repository format",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
		{
			name:        "invalid format - empty string",
			repo:        "",
			expectError: true,
			expectInMessage: []string{
				"invalid repository format",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
		{
			name:        "valid format",
			repo:        "github/gh-aw",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := checkRepositoryHasDiscussionsUncached(tt.repo)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for repo '%s', got nil", tt.repo)
					return
				}

				errMsg := err.Error()
				for _, expectedText := range tt.expectInMessage {
					if !strings.Contains(errMsg, expectedText) {
						t.Errorf("error message missing expected text.\nGot: %s\nExpected to contain: %s", errMsg, expectedText)
					}
				}
			} else {
				// For valid format, we might still get an error from the API call,
				// but it shouldn't be a format error
				if err != nil && strings.Contains(err.Error(), "invalid repository format") {
					t.Errorf("valid repo format '%s' incorrectly rejected: %v", tt.repo, err)
				}
			}
		})
	}
}
