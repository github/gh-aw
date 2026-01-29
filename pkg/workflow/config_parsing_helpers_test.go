//go:build !integration

package workflow

import (
	"testing"
)

func TestExtractStringFromMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		expected string
	}{
		{
			name: "valid string value",
			input: map[string]any{
				"my-key": "my-value",
			},
			key:      "my-key",
			expected: "my-value",
		},
		{
			name: "empty string value",
			input: map[string]any{
				"my-key": "",
			},
			key:      "my-key",
			expected: "",
		},
		{
			name:     "missing key",
			input:    map[string]any{},
			key:      "my-key",
			expected: "",
		},
		{
			name: "non-string type",
			input: map[string]any{
				"my-key": 123,
			},
			key:      "my-key",
			expected: "",
		},
		{
			name: "string with special characters",
			input: map[string]any{
				"my-key": "[Special] 🎯 Value",
			},
			key:      "my-key",
			expected: "[Special] 🎯 Value",
		},
		{
			name: "different key returns different value",
			input: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
			key:      "key2",
			expected: "value2",
		},
		{
			name: "non-string value returns empty",
			input: map[string]any{
				"my-key": []string{"array", "value"},
			},
			key:      "my-key",
			expected: "",
		},
		{
			name: "nil value returns empty",
			input: map[string]any{
				"my-key": nil,
			},
			key:      "my-key",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractStringFromMap(tt.input, tt.key, nil)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParseLabelsFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected []string
	}{
		{
			name: "valid labels array",
			input: map[string]any{
				"labels": []any{"bug", "enhancement", "documentation"},
			},
			expected: []string{"bug", "enhancement", "documentation"},
		},
		{
			name: "empty labels array",
			input: map[string]any{
				"labels": []any{},
			},
			expected: []string{},
		},
		{
			name:     "missing labels field",
			input:    map[string]any{},
			expected: nil,
		},
		{
			name: "labels with mixed types (filters non-strings)",
			input: map[string]any{
				"labels": []any{"bug", 123, "enhancement", true, "documentation"},
			},
			expected: []string{"bug", "enhancement", "documentation"},
		},
		{
			name: "labels as non-array type",
			input: map[string]any{
				"labels": "not-an-array",
			},
			expected: nil,
		},
		{
			name: "labels with only non-string types",
			input: map[string]any{
				"labels": []any{123, true, 456},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabelsFromConfig(tt.input)

			// Handle nil vs empty slice comparison
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("at index %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestParseTitlePrefixFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected string
	}{
		{
			name: "valid title-prefix",
			input: map[string]any{
				"title-prefix": "[bot] ",
			},
			expected: "[bot] ",
		},
		{
			name: "empty title-prefix",
			input: map[string]any{
				"title-prefix": "",
			},
			expected: "",
		},
		{
			name:     "missing title-prefix field",
			input:    map[string]any{},
			expected: "",
		},
		{
			name: "title-prefix as non-string type",
			input: map[string]any{
				"title-prefix": 123,
			},
			expected: "",
		},
		{
			name: "title-prefix with special characters",
			input: map[string]any{
				"title-prefix": "[AI-Generated] 🤖 ",
			},
			expected: "[AI-Generated] 🤖 ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTitlePrefixFromConfig(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParseTargetRepoFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected string
	}{
		{
			name: "valid target-repo",
			input: map[string]any{
				"target-repo": "owner/repo",
			},
			expected: "owner/repo",
		},
		{
			name: "wildcard target-repo (returns * for caller to validate)",
			input: map[string]any{
				"target-repo": "*",
			},
			expected: "*",
		},
		{
			name:     "missing target-repo field",
			input:    map[string]any{},
			expected: "",
		},
		{
			name: "target-repo as non-string type",
			input: map[string]any{
				"target-repo": 123,
			},
			expected: "",
		},
		{
			name: "target-repo with organization and repo",
			input: map[string]any{
				"target-repo": "github/docs",
			},
			expected: "github/docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTargetRepoFromConfig(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// Integration tests to verify the helpers work correctly in the parser functions

func TestParseIssuesConfigWithHelpers(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-issue": map[string]any{
			"title-prefix": "[bot] ",
			"labels":       []any{"automation", "ai-generated"},
			"target-repo":  "owner/repo",
		},
	}

	result := compiler.parseIssuesConfig(outputMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TitlePrefix != "[bot] " {
		t.Errorf("expected title-prefix '[bot] ', got %q", result.TitlePrefix)
	}

	if len(result.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(result.Labels))
	}

	if result.TargetRepoSlug != "owner/repo" {
		t.Errorf("expected target-repo 'owner/repo', got %q", result.TargetRepoSlug)
	}
}

func TestParsePullRequestsConfigWithHelpers(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-pull-request": map[string]any{
			"title-prefix": "[auto] ",
			"labels":       []any{"automated", "needs-review"},
			"target-repo":  "org/project",
		},
	}

	result := compiler.parsePullRequestsConfig(outputMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TitlePrefix != "[auto] " {
		t.Errorf("expected title-prefix '[auto] ', got %q", result.TitlePrefix)
	}

	if len(result.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(result.Labels))
	}

	if result.TargetRepoSlug != "org/project" {
		t.Errorf("expected target-repo 'org/project', got %q", result.TargetRepoSlug)
	}
}

func TestParseDiscussionsConfigWithHelpers(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-discussion": map[string]any{
			"title-prefix": "[analysis] ",
			"target-repo":  "team/discussions",
		},
	}

	result := compiler.parseDiscussionsConfig(outputMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TitlePrefix != "[analysis] " {
		t.Errorf("expected title-prefix '[analysis] ', got %q", result.TitlePrefix)
	}

	if result.TargetRepoSlug != "team/discussions" {
		t.Errorf("expected target-repo 'team/discussions', got %q", result.TargetRepoSlug)
	}
}

func TestParseCommentsConfigWithHelpers(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"add-comment": map[string]any{
			"target-repo": "upstream/project",
		},
	}

	result := compiler.parseCommentsConfig(outputMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TargetRepoSlug != "upstream/project" {
		t.Errorf("expected target-repo 'upstream/project', got %q", result.TargetRepoSlug)
	}
}

func TestParsePRReviewCommentsConfigWithHelpers(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-pull-request-review-comment": map[string]any{
			"target-repo": "company/codebase",
		},
	}

	result := compiler.parsePullRequestReviewCommentsConfig(outputMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TargetRepoSlug != "company/codebase" {
		t.Errorf("expected target-repo 'company/codebase', got %q", result.TargetRepoSlug)
	}
}

// Test wildcard validation (should return nil for invalid config)

func TestParseIssuesConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-issue": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parseIssuesConfig(outputMap)
	if result != nil {
		t.Errorf("expected nil for wildcard target-repo, got %+v", result)
	}
}

func TestParsePullRequestsConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-pull-request": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parsePullRequestsConfig(outputMap)
	if result != nil {
		t.Errorf("expected nil for wildcard target-repo, got %+v", result)
	}
}

func TestParseDiscussionsConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-discussion": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parseDiscussionsConfig(outputMap)
	if result != nil {
		t.Errorf("expected nil for wildcard target-repo, got %+v", result)
	}
}

func TestParseCommentsConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"add-comment": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parseCommentsConfig(outputMap)
	if result != nil {
		t.Errorf("expected nil for wildcard target-repo, got %+v", result)
	}
}

func TestParsePRReviewCommentsConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"create-pull-request-review-comment": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parsePullRequestReviewCommentsConfig(outputMap)
	if result != nil {
		t.Errorf("expected nil for wildcard target-repo, got %+v", result)
	}
}

func TestParseTargetRepoWithValidation(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]any
		expectedSlug  string
		expectedError bool
	}{
		{
			name: "valid target-repo",
			input: map[string]any{
				"target-repo": "owner/repo",
			},
			expectedSlug:  "owner/repo",
			expectedError: false,
		},
		{
			name: "empty target-repo",
			input: map[string]any{
				"target-repo": "",
			},
			expectedSlug:  "",
			expectedError: false,
		},
		{
			name:          "missing target-repo",
			input:         map[string]any{},
			expectedSlug:  "",
			expectedError: false,
		},
		{
			name: "wildcard target-repo (invalid)",
			input: map[string]any{
				"target-repo": "*",
			},
			expectedSlug:  "",
			expectedError: true,
		},
		{
			name: "target-repo with special characters",
			input: map[string]any{
				"target-repo": "github-next/gh-aw",
			},
			expectedSlug:  "github-next/gh-aw",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, isInvalid := parseTargetRepoWithValidation(tt.input)
			if slug != tt.expectedSlug {
				t.Errorf("expected slug %q, got %q", tt.expectedSlug, slug)
			}
			if isInvalid != tt.expectedError {
				t.Errorf("expected error %v, got %v", tt.expectedError, isInvalid)
			}
		})
	}
}

func TestParseParticipantsFromConfig(t *testing.T) {
	tests := []struct {
		name           string
		input          map[string]any
		participantKey string
		expected       []string
	}{
		{
			name: "single assignee as string",
			input: map[string]any{
				"assignees": "user1",
			},
			participantKey: "assignees",
			expected:       []string{"user1"},
		},
		{
			name: "multiple assignees as array",
			input: map[string]any{
				"assignees": []any{"user1", "user2", "user3"},
			},
			participantKey: "assignees",
			expected:       []string{"user1", "user2", "user3"},
		},
		{
			name: "empty assignees array",
			input: map[string]any{
				"assignees": []any{},
			},
			participantKey: "assignees",
			expected:       []string{}, // Empty array returns empty slice (consistent with parseLabelsFromConfig)
		},
		{
			name:           "assignees key not present",
			input:          map[string]any{},
			participantKey: "assignees",
			expected:       nil,
		},
		{
			name: "assignees with non-string values (filtered out)",
			input: map[string]any{
				"assignees": []any{"user1", 123, "user2", nil, "user3"},
			},
			participantKey: "assignees",
			expected:       []string{"user1", "user2", "user3"},
		},
		{
			name: "assignees array with only non-string values",
			input: map[string]any{
				"assignees": []any{123, true, nil},
			},
			participantKey: "assignees",
			expected:       []string{}, // All filtered out returns empty slice
		},
		{
			name: "single reviewer as string",
			input: map[string]any{
				"reviewers": "reviewer1",
			},
			participantKey: "reviewers",
			expected:       []string{"reviewer1"},
		},
		{
			name: "multiple reviewers as array",
			input: map[string]any{
				"reviewers": []any{"reviewer1", "reviewer2"},
			},
			participantKey: "reviewers",
			expected:       []string{"reviewer1", "reviewer2"},
		},
		{
			name:           "reviewers key not present",
			input:          map[string]any{},
			participantKey: "reviewers",
			expected:       nil,
		},
		{
			name: "assignees is not string or array",
			input: map[string]any{
				"assignees": 123,
			},
			participantKey: "assignees",
			expected:       nil,
		},
		{
			name: "empty string assignee",
			input: map[string]any{
				"assignees": "",
			},
			participantKey: "assignees",
			expected:       []string{""},
		},
		{
			name: "mixed valid and empty strings in array",
			input: map[string]any{
				"assignees": []any{"user1", "", "user2"},
			},
			participantKey: "assignees",
			expected:       []string{"user1", "", "user2"},
		},
		{
			name: "assignees with special characters",
			input: map[string]any{
				"assignees": []any{"user-1", "user_2", "user.3"},
			},
			participantKey: "assignees",
			expected:       []string{"user-1", "user_2", "user.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseParticipantsFromConfig(tt.input, tt.participantKey)

			// Check nil vs empty array
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d participants, got %d", len(tt.expected), len(result))
				return
			}

			for i, expectedParticipant := range tt.expected {
				if result[i] != expectedParticipant {
					t.Errorf("participant %d: expected %q, got %q", i, expectedParticipant, result[i])
				}
			}
		})
	}
}

// TestParseParticipantsFromConfigConsistency verifies that assignees and reviewers
// are parsed with identical logic when using the same input data structure
func TestParseParticipantsFromConfigConsistency(t *testing.T) {
	testCases := []struct {
		name  string
		input map[string]any
	}{
		{
			name: "single participant string",
			input: map[string]any{
				"assignees": "user1",
				"reviewers": "user1",
			},
		},
		{
			name: "multiple participants array",
			input: map[string]any{
				"assignees": []any{"user1", "user2"},
				"reviewers": []any{"user1", "user2"},
			},
		},
		{
			name: "empty participants array",
			input: map[string]any{
				"assignees": []any{},
				"reviewers": []any{},
			},
		},
		{
			name: "participants with filtered non-strings",
			input: map[string]any{
				"assignees": []any{"user1", 123, "user2"},
				"reviewers": []any{"user1", 123, "user2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assignees := parseParticipantsFromConfig(tc.input, "assignees")
			reviewers := parseParticipantsFromConfig(tc.input, "reviewers")

			// Both should return the same results
			if (assignees == nil) != (reviewers == nil) {
				t.Errorf("nil mismatch: assignees=%v, reviewers=%v", assignees, reviewers)
				return
			}

			if assignees == nil {
				return
			}

			if len(assignees) != len(reviewers) {
				t.Errorf("length mismatch: assignees=%d, reviewers=%d", len(assignees), len(reviewers))
				return
			}

			for i := range assignees {
				if assignees[i] != reviewers[i] {
					t.Errorf("value mismatch at index %d: assignees=%q, reviewers=%q", i, assignees[i], reviewers[i])
				}
			}
		})
	}
}

func TestParseIntFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		expected int
	}{
		{
			name: "valid int value",
			input: map[string]any{
				"my-key": 42,
			},
			key:      "my-key",
			expected: 42,
		},
		{
			name: "valid int64 value",
			input: map[string]any{
				"my-key": int64(100),
			},
			key:      "my-key",
			expected: 100,
		},
		{
			name: "valid float64 value",
			input: map[string]any{
				"my-key": float64(75.5),
			},
			key:      "my-key",
			expected: 75,
		},
		{
			name: "valid uint64 value",
			input: map[string]any{
				"my-key": uint64(200),
			},
			key:      "my-key",
			expected: 200,
		},
		{
			name: "uint64 overflow - should return 0",
			input: map[string]any{
				"my-key": ^uint64(0), // Max uint64 value
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "zero value",
			input: map[string]any{
				"my-key": 0,
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "negative value",
			input: map[string]any{
				"my-key": -10,
			},
			key:      "my-key",
			expected: -10,
		},
		{
			name:     "missing key",
			input:    map[string]any{},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "non-numeric type (string)",
			input: map[string]any{
				"my-key": "123",
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "non-numeric type (bool)",
			input: map[string]any{
				"my-key": true,
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "non-numeric type (array)",
			input: map[string]any{
				"my-key": []int{1, 2, 3},
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "nil value",
			input: map[string]any{
				"my-key": nil,
			},
			key:      "my-key",
			expected: 0,
		},
		{
			name: "different keys with different values",
			input: map[string]any{
				"key1": 10,
				"key2": 20,
			},
			key:      "key2",
			expected: 20,
		},
		{
			name: "large int value",
			input: map[string]any{
				"my-key": 999999,
			},
			key:      "my-key",
			expected: 999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseIntFromConfig(tt.input, tt.key, nil)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestParseBoolFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		expected bool
	}{
		{
			name: "true value",
			input: map[string]any{
				"my-key": true,
			},
			key:      "my-key",
			expected: true,
		},
		{
			name: "false value",
			input: map[string]any{
				"my-key": false,
			},
			key:      "my-key",
			expected: false,
		},
		{
			name:     "missing key",
			input:    map[string]any{},
			key:      "my-key",
			expected: false,
		},
		{
			name: "non-bool type (string)",
			input: map[string]any{
				"my-key": "true",
			},
			key:      "my-key",
			expected: false,
		},
		{
			name: "non-bool type (int)",
			input: map[string]any{
				"my-key": 1,
			},
			key:      "my-key",
			expected: false,
		},
		{
			name: "non-bool type (int 0)",
			input: map[string]any{
				"my-key": 0,
			},
			key:      "my-key",
			expected: false,
		},
		{
			name: "non-bool type (array)",
			input: map[string]any{
				"my-key": []bool{true, false},
			},
			key:      "my-key",
			expected: false,
		},
		{
			name: "nil value",
			input: map[string]any{
				"my-key": nil,
			},
			key:      "my-key",
			expected: false,
		},
		{
			name: "different keys with different values",
			input: map[string]any{
				"key1": true,
				"key2": false,
			},
			key:      "key1",
			expected: true,
		},
		{
			name: "explicit false value should be preserved",
			input: map[string]any{
				"my-key": false,
			},
			key:      "my-key",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseBoolFromConfig(tt.input, tt.key, nil)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestUnmarshalConfig(t *testing.T) {
	tests := []struct {
		name        string
		inputMap    map[string]any
		key         string
		expectError bool
		validate    func(*testing.T, *CreateIssuesConfig)
	}{
		{
			name: "valid config with all fields",
			inputMap: map[string]any{
				"create-issue": map[string]any{
					"title-prefix":   "[bot] ",
					"labels":         []any{"bug", "enhancement"},
					"allowed-labels": []any{"bug", "feature"},
					"assignees":      []any{"user1", "user2"},
					"target-repo":    "owner/repo",
					"allowed-repos":  []any{"owner/repo1", "owner/repo2"},
					"expires":        7,
					"max":            5,
					"github-token":   "${{ secrets.TOKEN }}",
				},
			},
			key:         "create-issue",
			expectError: false,
			validate: func(t *testing.T, config *CreateIssuesConfig) {
				if config.TitlePrefix != "[bot] " {
					t.Errorf("expected title-prefix '[bot] ', got %q", config.TitlePrefix)
				}
				if len(config.Labels) != 2 || config.Labels[0] != "bug" || config.Labels[1] != "enhancement" {
					t.Errorf("expected labels [bug, enhancement], got %v", config.Labels)
				}
				if len(config.AllowedLabels) != 2 {
					t.Errorf("expected 2 allowed-labels, got %d", len(config.AllowedLabels))
				}
				if len(config.Assignees) != 2 {
					t.Errorf("expected 2 assignees, got %d", len(config.Assignees))
				}
				if config.TargetRepoSlug != "owner/repo" {
					t.Errorf("expected target-repo 'owner/repo', got %q", config.TargetRepoSlug)
				}
				if len(config.AllowedRepos) != 2 {
					t.Errorf("expected 2 allowed-repos, got %d", len(config.AllowedRepos))
				}
				if config.Expires != 7 {
					t.Errorf("expected expires 7, got %d", config.Expires)
				}
				if config.Max != 5 {
					t.Errorf("expected max 5, got %d", config.Max)
				}
				if config.GitHubToken != "${{ secrets.TOKEN }}" {
					t.Errorf("expected github-token, got %q", config.GitHubToken)
				}
			},
		},
		{
			name: "empty config (nil value)",
			inputMap: map[string]any{
				"create-issue": nil,
			},
			key:         "create-issue",
			expectError: false,
			validate: func(t *testing.T, config *CreateIssuesConfig) {
				// All fields should be zero values
				if config.TitlePrefix != "" {
					t.Errorf("expected empty title-prefix, got %q", config.TitlePrefix)
				}
				if len(config.Labels) != 0 {
					t.Errorf("expected no labels, got %v", config.Labels)
				}
			},
		},
		{
			name: "partial config",
			inputMap: map[string]any{
				"create-issue": map[string]any{
					"title-prefix": "[auto] ",
					"max":          3,
				},
			},
			key:         "create-issue",
			expectError: false,
			validate: func(t *testing.T, config *CreateIssuesConfig) {
				if config.TitlePrefix != "[auto] " {
					t.Errorf("expected title-prefix '[auto] ', got %q", config.TitlePrefix)
				}
				if config.Max != 3 {
					t.Errorf("expected max 3, got %d", config.Max)
				}
				// Other fields should be zero values
				if len(config.Labels) != 0 {
					t.Errorf("expected no labels, got %v", config.Labels)
				}
			},
		},
		{
			name: "missing key",
			inputMap: map[string]any{
				"other-key": map[string]any{},
			},
			key:         "create-issue",
			expectError: true,
		},
		{
			name: "empty map",
			inputMap: map[string]any{
				"create-issue": map[string]any{},
			},
			key:         "create-issue",
			expectError: false,
			validate: func(t *testing.T, config *CreateIssuesConfig) {
				// All fields should be zero values
				if config.Max != 0 {
					t.Errorf("expected max 0, got %d", config.Max)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config CreateIssuesConfig
			err := unmarshalConfig(tt.inputMap, tt.key, &config, nil)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, &config)
			}
		})
	}
}
