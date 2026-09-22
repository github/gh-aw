//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatePullRequestCITriggerToken verifies the GH_AW_CI_TRIGGER_TOKEN env var
// is correctly generated in the safe_outputs job for different configurations:
// - unset/empty: uses secrets.GH_AW_CI_TRIGGER_TOKEN
// - "app": uses steps.safe-outputs-app-token.outputs.token
// - explicit token: uses the specified token value
func TestCreatePullRequestCITriggerToken(t *testing.T) {
	tests := []struct {
		name             string
		tokenConfig      string // value for github-token-for-extra-empty-commit
		expectedContains string // expected substring in GH_AW_CI_TRIGGER_TOKEN env var
		notExpected      string // should NOT contain this string
	}{
		{
			name:             "unset config uses secrets.GH_AW_CI_TRIGGER_TOKEN",
			tokenConfig:      "",
			expectedContains: "${{ secrets.GH_AW_CI_TRIGGER_TOKEN }}",
			notExpected:      "safe-outputs-app-token",
		},
		{
			name:             "app config uses app token step output",
			tokenConfig:      "app",
			expectedContains: "${{ steps.safe-outputs-app-token.outputs.token || '' }}",
			notExpected:      "secrets.GH_AW_CI_TRIGGER_TOKEN",
		},
		{
			name:             "explicit token uses provided value",
			tokenConfig:      "${{ secrets.MY_CUSTOM_PAT }}",
			expectedContains: "${{ secrets.MY_CUSTOM_PAT }}",
			notExpected:      "safe-outputs-app-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "ci-trigger-token-test")

			// Build the workflow content with or without the token config
			var safeOutputsConfig string
			if tt.tokenConfig == "" {
				safeOutputsConfig = `safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
    labels: [test]`
			} else {
				safeOutputsConfig = `safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
    labels: [test]
    github-token-for-extra-empty-commit: ` + tt.tokenConfig
			}

			testContent := `---
on: push
permissions:
  contents: read
  pull-requests: read
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
` + safeOutputsConfig + `
---

# Test CI Trigger Token Configuration

This workflow tests the GH_AW_CI_TRIGGER_TOKEN env var generation.
`

			testFile := filepath.Join(tmpDir, "test-ci-trigger-token.md")
			err := os.WriteFile(testFile, []byte(testContent), 0644)
			require.NoError(t, err, "Failed to write test file")

			compiler := NewCompiler()

			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Should compile workflow without error")

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Should read generated lock file")

			lockContentStr := string(lockContent)

			// Verify the expected token configuration is present
			assert.Contains(t, lockContentStr, "GH_AW_CI_TRIGGER_TOKEN:",
				"Generated workflow should contain GH_AW_CI_TRIGGER_TOKEN env var")

			assert.Contains(t, lockContentStr, tt.expectedContains,
				"GH_AW_CI_TRIGGER_TOKEN should have expected value")

			if tt.notExpected != "" {
				// Find the GH_AW_CI_TRIGGER_TOKEN line and verify it doesn't contain the unexpected value
				for line := range strings.SplitSeq(lockContentStr, "\n") {
					if strings.Contains(line, "GH_AW_CI_TRIGGER_TOKEN:") {
						assert.NotContains(t, line, tt.notExpected,
							"GH_AW_CI_TRIGGER_TOKEN should not contain %q", tt.notExpected)
					}
				}
			}
		})
	}
}

// TestPushToPullRequestBranchCITriggerToken verifies the GH_AW_CI_TRIGGER_TOKEN env var
// is correctly generated for push-to-pull-request-branch safe output configuration.
func TestPushToPullRequestBranchCITriggerToken(t *testing.T) {
	tests := []struct {
		name             string
		tokenConfig      string
		expectedContains string
	}{
		{
			name:             "unset config uses secrets.GH_AW_CI_TRIGGER_TOKEN",
			tokenConfig:      "",
			expectedContains: "${{ secrets.GH_AW_CI_TRIGGER_TOKEN }}",
		},
		{
			name:             "app config uses app token step output",
			tokenConfig:      "app",
			expectedContains: "${{ steps.safe-outputs-app-token.outputs.token || '' }}",
		},
		{
			name:             "explicit token uses provided value",
			tokenConfig:      "${{ secrets.CUSTOM_PUSH_TOKEN }}",
			expectedContains: "${{ secrets.CUSTOM_PUSH_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "push-pr-branch-ci-trigger-test")

			var safeOutputsConfig string
			if tt.tokenConfig == "" {
				safeOutputsConfig = `safe-outputs:
  push-to-pull-request-branch:
    labels: [test]`
			} else {
				safeOutputsConfig = `safe-outputs:
  push-to-pull-request-branch:
    labels: [test]
    github-token-for-extra-empty-commit: ` + tt.tokenConfig
			}

			testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
` + safeOutputsConfig + `
---

# Test Push to PR Branch CI Trigger Token

This workflow tests push-to-pull-request-branch token configuration.
`

			testFile := filepath.Join(tmpDir, "test-push-pr-branch-token.md")
			err := os.WriteFile(testFile, []byte(testContent), 0644)
			require.NoError(t, err, "Failed to write test file")

			compiler := NewCompiler()

			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Should compile workflow without error")

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Should read generated lock file")

			lockContentStr := string(lockContent)

			assert.Contains(t, lockContentStr, "GH_AW_CI_TRIGGER_TOKEN:",
				"Generated workflow should contain GH_AW_CI_TRIGGER_TOKEN env var")

			assert.Contains(t, lockContentStr, tt.expectedContains,
				"GH_AW_CI_TRIGGER_TOKEN should have expected value")
		})
	}
}

// TestCITriggerTokenNoneOmitsSecret verifies that setting
// github-token-for-extra-empty-commit to "none" removes GH_AW_CI_TRIGGER_TOKEN
// from the compiled workflow entirely, including the gh-aw-manifest secrets list.
// This lets repositories that do not configure the magic secret keep it out of
// their lock files.
func TestCITriggerTokenNoneOmitsSecret(t *testing.T) {
	tests := []struct {
		name              string
		safeOutputsConfig string
	}{
		{
			name: "create-pull-request with none",
			safeOutputsConfig: `safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
    github-token-for-extra-empty-commit: none`,
		},
		{
			name: "create-pull-request with None is case-insensitive",
			safeOutputsConfig: `safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
    github-token-for-extra-empty-commit: None`,
		},
		{
			name: "create-pull-request with none overrides custom environment token",
			safeOutputsConfig: `safe-outputs:
  env:
    GH_AW_CI_TRIGGER_TOKEN: ${{ secrets.CUSTOM_CI_TRIGGER_TOKEN }}
  create-pull-request:
    title-prefix: "[test] "
    github-token-for-extra-empty-commit: none`,
		},
		{
			name: "push-to-pull-request-branch with none",
			safeOutputsConfig: `safe-outputs:
  push-to-pull-request-branch:
    labels: [test]
    github-token-for-extra-empty-commit: none`,
		},
		{
			name: "push-to-pull-request-branch with none overrides custom environment token",
			safeOutputsConfig: `safe-outputs:
  env:
    GH_AW_CI_TRIGGER_TOKEN: ${{ secrets.CUSTOM_CI_TRIGGER_TOKEN }}
  push-to-pull-request-branch:
    labels: [test]
    github-token-for-extra-empty-commit: none`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "ci-trigger-token-none-test")

			testContent := `---
on: push
permissions:
  contents: read
  pull-requests: read
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
` + tt.safeOutputsConfig + `
---

# Test CI Trigger Token Disabled

This workflow tests that GH_AW_CI_TRIGGER_TOKEN is omitted when disabled.
`

			testFile := filepath.Join(tmpDir, "test-ci-trigger-token-none.md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "Failed to write test file")

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(testFile), "Should compile workflow without error")

			lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
			require.NoError(t, err, "Should read generated lock file")
			lockContentStr := string(lockContent)

			assert.NotContains(t, lockContentStr, "GH_AW_CI_TRIGGER_TOKEN",
				"GH_AW_CI_TRIGGER_TOKEN should not appear anywhere in the lock file when disabled")

			manifest, err := ExtractGHAWManifestFromLockFile(lockContentStr)
			require.NoError(t, err)
			require.NotNil(t, manifest)
			assert.NotContains(t, manifest.Secrets, "secrets.GH_AW_CI_TRIGGER_TOKEN",
				"gh-aw-manifest secrets should not include GH_AW_CI_TRIGGER_TOKEN when disabled")
			assert.NotContains(t, manifest.Secrets, "GH_AW_CI_TRIGGER_TOKEN",
				"gh-aw-manifest secrets should not include GH_AW_CI_TRIGGER_TOKEN when disabled")
		})
	}
}

func TestCITriggerTokenCreatePullRequestPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		create   string
		push     string
		expected string
		disabled bool
	}{
		{
			name:     "create-pull-request none wins over push token",
			create:   "none",
			push:     "${{ secrets.PUSH_TOKEN }}",
			expected: "none",
			disabled: true,
		},
		{
			name:     "create-pull-request token wins over push none",
			create:   "${{ secrets.CREATE_TOKEN }}",
			push:     "none",
			expected: "${{ secrets.CREATE_TOKEN }}",
			disabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safeOutputs := &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					GithubTokenForExtraEmptyCommit: tt.create,
				},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					GithubTokenForExtraEmptyCommit: tt.push,
				},
			}

			assert.Equal(t, tt.expected, getCITriggerTokenConfig(safeOutputs))
			assert.Equal(t, tt.disabled, isCITriggerTokenDisabled(safeOutputs))
		})
	}
}
