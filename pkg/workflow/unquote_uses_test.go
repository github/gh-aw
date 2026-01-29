//go:build !integration

package workflow

import (
	"testing"
)

func TestUnquoteUsesWithComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic quoted uses with version comment",
			input:    `  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"`,
			expected: `  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6`,
		},
		{
			name:     "quoted uses with version comment and indentation",
			input:    `        uses: "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6"`,
			expected: `        uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6`,
		},
		{
			name: "multiple quoted uses on different lines",
			input: `  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"
  with:
    ref: main
  uses: "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6"`,
			expected: `  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6
  with:
    ref: main
  uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6`,
		},
		{
			name:     "unquoted uses should not be modified",
			input:    `  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6`,
			expected: `  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6`,
		},
		{
			name:     "quoted uses without version comment should not be modified",
			input:    `  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd"`,
			expected: `  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd"`,
		},
		{
			name:     "empty string",
			input:    ``,
			expected: ``,
		},
		{
			name: "no uses lines",
			input: `name: Test
run: echo "hello"
with:
  ref: main`,
			expected: `name: Test
run: echo "hello"
with:
  ref: main`,
		},
		{
			name: "complete step with quoted uses",
			input: `- name: Checkout repository
  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"
  with:
    persist-credentials: false`,
			expected: `- name: Checkout repository
  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6
  with:
    persist-credentials: false`,
		},
		{
			name:     "step with content after closing quote",
			input:    `  uses: "actions/checkout@sha # v6"  # trailing comment`,
			expected: `  uses: actions/checkout@sha # v6  # trailing comment`,
		},
		{
			name: "multiple steps in YAML array format",
			input: `steps:
- name: Checkout
  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"
- name: Setup Node
  uses: "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6"`,
			expected: `steps:
- name: Checkout
  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6
- name: Setup Node
  uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6`,
		},
		{
			name:     "handles version tags with special characters",
			input:    `  uses: "actions/cache@ab5e6d0c87105b4c9c2047343972218f562e4319 # v4.0.1"`,
			expected: `  uses: actions/cache@ab5e6d0c87105b4c9c2047343972218f562e4319 # v4.0.1`,
		},
		{
			name: "preserves empty lines",
			input: `  uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"

  uses: "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6"`,
			expected: `  uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6

  uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unquoteUsesWithComments(tt.input)
			if result != tt.expected {
				t.Errorf("unquoteUsesWithComments() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestUnquoteUsesWithCommentsEdgeCases tests edge cases and potential bugs
func TestUnquoteUsesWithCommentsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "line with only opening quote (malformed)",
			input:    `  uses: "actions/checkout@sha`,
			expected: `  uses: "actions/checkout@sha`,
		},
		{
			name:     "line with hash but no closing quote (malformed)",
			input:    `  uses: "actions/checkout@sha # v6`,
			expected: `  uses: "actions/checkout@sha # v6`,
		},
		{
			name:     "hash in action name not version comment",
			input:    `  uses: "some/action#with-hash@sha"`,
			expected: `  uses: "some/action#with-hash@sha"`,
		},
		{
			name:     "multiple quotes on same line (should handle first occurrence)",
			input:    `  uses: "actions/checkout@sha # v6" and uses: "other/action@sha # v1"`,
			expected: `  uses: actions/checkout@sha # v6 and uses: "other/action@sha # v1"`,
		},
		{
			name:     "no space before hash",
			input:    `  uses: "actions/checkout@sha#v5"`,
			expected: `  uses: "actions/checkout@sha#v5"`,
		},
		{
			name:     "hash in the middle without space (not a comment)",
			input:    `  uses: "actions/checkout@sha#abc # v6"`,
			expected: `  uses: actions/checkout@sha#abc # v6`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unquoteUsesWithComments(tt.input)
			if result != tt.expected {
				t.Errorf("unquoteUsesWithComments() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestUnquoteUsesWithCommentsRealWorldExamples tests with actual workflow YAML
func TestUnquoteUsesWithCommentsRealWorldExamples(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "real workflow from unbloat-docs",
			input: `steps:
  - name: Checkout repository
    uses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6"
    with:
      persist-credentials: false
  - name: Setup Node.js
    uses: "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6"
    with:
      cache: npm
      cache-dependency-path: docs/package-lock.json
      node-version: "24"`,
			expected: `steps:
  - name: Checkout repository
    uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v6
    with:
      persist-credentials: false
  - name: Setup Node.js
    uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6
    with:
      cache: npm
      cache-dependency-path: docs/package-lock.json
      node-version: "24"`,
		},
		{
			name: "post-steps with quoted uses",
			input: `post-steps:
  - if: always()
    name: Upload Test Results
    uses: "actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f # v6"
    with:
      if-no-files-found: ignore
      name: post-steps-test-results
      path: /tmp/gh-aw/
      retention-days: 1`,
			expected: `post-steps:
  - if: always()
    name: Upload Test Results
    uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f # v6
    with:
      if-no-files-found: ignore
      name: post-steps-test-results
      path: /tmp/gh-aw/
      retention-days: 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unquoteUsesWithComments(tt.input)
			if result != tt.expected {
				t.Errorf("unquoteUsesWithComments() failed for %s\nGot:\n%s\n\nWant:\n%s", tt.name, result, tt.expected)
			}
		})
	}
}
