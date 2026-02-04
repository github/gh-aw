//go:build !integration

package cli

import (
	"strings"
	"testing"
)

// TestRepoSlugErrorMessages verifies that repository slug format errors include examples
func TestRepoSlugErrorMessages(t *testing.T) {
	tests := []struct {
		name            string
		repoSlug        string
		expectInMessage []string
	}{
		{
			name:     "invalid format - no slash",
			repoSlug: "invalidrepo",
			expectInMessage: []string{
				"invalid repository",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
		{
			name:     "invalid format - too many slashes",
			repoSlug: "owner/repo/extra",
			expectInMessage: []string{
				"invalid repository",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
		{
			name:     "invalid format - empty owner",
			repoSlug: "/repo",
			expectInMessage: []string{
				"invalid repository",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
		{
			name:     "invalid format - empty repo",
			repoSlug: "owner/",
			expectInMessage: []string{
				"invalid repository",
				"Expected format: owner/repo",
				"Example: github/gh-aw",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test ensureTrialRepository function
			err := ensureTrialRepository(tt.repoSlug, "", false, false, false)

			if err == nil {
				t.Errorf("expected error for repo slug '%s', got nil", tt.repoSlug)
				return
			}

			errMsg := err.Error()
			for _, expectedText := range tt.expectInMessage {
				if !strings.Contains(errMsg, expectedText) {
					t.Errorf("error message missing expected text.\nGot: %s\nExpected to contain: %s", errMsg, expectedText)
				}
			}
		})
	}
}

// TestParseRepoSpecErrorMessages verifies that parseRepoSpec errors include examples
func TestParseRepoSpecErrorMessages(t *testing.T) {
	tests := []struct {
		name            string
		repoSpec        string
		expectInMessage []string
	}{
		{
			name:     "invalid format - single name",
			repoSpec: "justname",
			expectInMessage: []string{
				"must be in format",
				"Example: github/gh-aw",
			},
		},
		{
			name:     "invalid format - too many parts",
			repoSpec: "owner/repo/extra",
			expectInMessage: []string{
				"must be in format",
				"Example: github/gh-aw",
			},
		},
		{
			name:     "invalid GitHub URL",
			repoSpec: "https://github.com/owner",
			expectInMessage: []string{
				"invalid GitHub URL",
				"Example: https://github.com/github/gh-aw",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRepoSpec(tt.repoSpec)

			if err == nil {
				t.Errorf("expected error for repo spec '%s', got nil", tt.repoSpec)
				return
			}

			errMsg := err.Error()
			for _, expectedText := range tt.expectInMessage {
				if !strings.Contains(errMsg, expectedText) {
					t.Errorf("error message missing expected text.\nGot: %s\nExpected to contain: %s", errMsg, expectedText)
				}
			}
		})
	}
}
