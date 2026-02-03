//go:build !integration

package cli

import (
	"testing"
)

func TestExtractIssueNumberFromURL(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "Valid GitHub issue URL",
			url:      "https://github.com/github/releases/issues/6818",
			expected: "6818",
		},
		{
			name:     "Another valid issue URL",
			url:      "https://github.com/github/gh-aw-trial/issues/123",
			expected: "123",
		},
		{
			name:     "Issue URL with single digit",
			url:      "https://github.com/user/repo/issues/5",
			expected: "5",
		},
		{
			name:     "Invalid URL - not GitHub",
			url:      "https://gitlab.com/user/repo/issues/123",
			expected: "",
		},
		{
			name:     "Invalid URL - not an issue",
			url:      "https://github.com/user/repo/pulls/123",
			expected: "",
		},
		{
			name:     "Invalid URL - missing issue number",
			url:      "https://github.com/user/repo/issues/",
			expected: "",
		},
		{
			name:     "Invalid URL - non-numeric issue number",
			url:      "https://github.com/user/repo/issues/abc",
			expected: "",
		},
		{
			name:     "Empty URL",
			url:      "",
			expected: "",
		},
		{
			name:     "URL with query parameters",
			url:      "https://github.com/user/repo/issues/456?tab=comments",
			expected: "456",
		},
		{
			name:     "URL with fragment",
			url:      "https://github.com/user/repo/issues/789#issuecomment-123456",
			expected: "789",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseIssueSpec(tc.url)
			if result != tc.expected {
				t.Errorf("parseIssueSpec(%q) = %q, expected %q", tc.url, result, tc.expected)
			}
		})
	}
}

func TestTrialWorkflowSpecParsing(t *testing.T) {
	// Test that workflow spec parsing still works with the new trial functionality
	testCases := []struct {
		name         string
		spec         string
		expectedRepo string
		expectedName string
		shouldError  bool
	}{
		{
			name:         "GitHub URL workflow spec",
			spec:         "github/gh-aw-trial/.github/workflows/release-issue-linker.md",
			expectedRepo: "github/gh-aw-trial",
			expectedName: "release-issue-linker",
			shouldError:  false,
		},
		{
			name:         "Simple workflow spec",
			spec:         "user/repo/workflow-name",
			expectedRepo: "user/repo",
			expectedName: "workflow-name",
			shouldError:  false,
		},
		{
			name:         "Invalid workflow spec",
			spec:         "invalid-spec",
			expectedRepo: "",
			expectedName: "",
			shouldError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := parseWorkflowSpec(tc.spec)

			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error for spec %q, but got none", tc.spec)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for spec %q: %v", tc.spec, err)
				return
			}

			if spec.RepoSlug != tc.expectedRepo {
				t.Errorf("Expected repo %q, got %q", tc.expectedRepo, spec.RepoSlug)
			}

			if spec.WorkflowName != tc.expectedName {
				t.Errorf("Expected workflow name %q, got %q", tc.expectedName, spec.WorkflowName)
			}
		})
	}
}
