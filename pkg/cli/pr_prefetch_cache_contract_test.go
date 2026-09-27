//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedPRDiffDataFetchValidatesHeadSHAForCacheHit(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "shared", "pr-diff-data-fetch.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "Should read shared pr-diff-data-fetch workflow")

	text := string(content)
	assert.Contains(t, text, "pr-data-head-sha.txt", "Shared PR prefetch should persist head SHA marker")
	assert.Contains(t, text, "--json number,title,body,headRefName,headRefOid,additions,deletions,changedFiles,files", "Shared PR prefetch should capture head SHA in metadata")
	assert.Contains(t, text, "Cache hit: using pre-fetched PR data for head", "Shared PR prefetch should verify cache by current head SHA")
	assert.Contains(t, text, "fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number", "Shared PR prefetch should resolve centralized slash-command PR numbers")
	assert.Contains(t, text, "item_type == 'pull_request'", "Shared PR prefetch should only use aw_context item_number for pull requests")
	assert.Contains(t, text, "::error::Unable to determine the pull request number from the event context.", "Shared PR prefetch should reject missing PR context")
	assertSubstringsInOrder(t, text, []string{
		`CACHE_HEAD_SHA=""`,
		"fetch_review_comments",
		"# Skip diff and metadata fetch",
	}, "Shared PR prefetch should refresh review comments before checking cached diff data")
	assert.NotContains(t, text, `-f /tmp/gh-aw/agent/pr-review-comments.json ]; then`, "Review comments should not control diff cache hits")
	assert.Contains(t, text, "DIFF_EXIT=$?", "Shared PR prefetch should capture gh pr diff failures")
	assert.Contains(t, text, `if [ "$DIFF_EXIT" -ne 0 ]; then`, "Shared PR prefetch should reject gh pr diff failures")
	assert.NotContains(t, text, "|| true; } | head", "Shared PR prefetch should not suppress gh pr diff failures")
}

func TestPRDataPrefetchRejectsDiffFailuresBeforeSavingCacheData(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	for _, workflow := range []string{
		filepath.Join(".github", "workflows", "shared", "pr-diff-data-fetch.md"),
		filepath.Join(".github", "workflows", "pr-data-prefetch.yml"),
	} {
		workflowPath := filepath.Join(repoRoot, workflow)
		content, readErr := os.ReadFile(workflowPath)
		require.NoError(t, readErr, "Should read %s", workflow)

		text := string(content)
		assert.Contains(t, text, "DIFF_EXIT=$?", "%s should capture gh pr diff failures", workflow)
		assert.Contains(t, text, `if [ "$DIFF_EXIT" -ne 0 ]; then`, "%s should reject gh pr diff failures", workflow)
		assert.NotContains(t, text, "|| true; } | head", "%s should not suppress gh pr diff failures", workflow)
		assertSubstringsInOrder(t, text, []string{
			`if [ "$DIFF_EXIT" -ne 0 ]; then`,
			"rm -f /tmp/gh-aw/agent/pr-diff.full /tmp/gh-aw/agent/pr-diff.err",
			"exit 1",
			"pr-data-head-sha.txt",
		}, "%s should validate the diff before writing a valid cache head marker", workflow)
	}
}

func TestTopReviewWorkflowsHaveHeadAwarePRDataCacheKeys(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	for _, workflow := range []string{
		"mattpocock-skills-reviewer.md",
		"ponytail-reviewer.md",
		"pr-code-quality-reviewer.md",
	} {
		workflowPath := filepath.Join(repoRoot, ".github", "workflows", workflow)
		content, readErr := os.ReadFile(workflowPath)
		require.NoError(t, readErr, "Should read %s", workflow)
		text := string(content)
		assert.Contains(t, text, "key: pr-prefetch-${{ github.event.pull_request.head.sha || format('{0}-{1}'", "%s should use the PR head SHA for pull_request runs and a unique slash-command key", workflow)
		assert.Contains(t, text, "github.run_id", "%s should make slash-command cache keys unique so refreshed data can be saved", workflow)
		assert.Contains(t, text, "item_type == 'pull_request'", "%s should only use aw_context item_number for pull requests", workflow)
		assert.Contains(t, text, "pr-prefetch-${{ github.event.pull_request.number || github.event.issue.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request'", "%s should restore cache data with a centralized slash-command fallback", workflow)
	}

	mattWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "mattpocock-skills-reviewer.md")
	mattContent, err := os.ReadFile(mattWorkflowPath)
	require.NoError(t, err, "Should read mattpocock-skills-reviewer workflow")
	assert.Contains(t, string(mattContent), `${GITHUB_WORKSPACE}/.github/skills`, "Matt reviewer should discover skills from the restored workspace directory")

	sentinelWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "test-quality-sentinel.md")
	sentinelContent, err := os.ReadFile(sentinelWorkflowPath)
	require.NoError(t, err, "Should read test-quality-sentinel workflow")
	text := string(sentinelContent)
	assert.Contains(t, text, "key: pr-test-prefetch-${{ github.event.pull_request.head.sha || github.event.issue.number }}", "Test Quality Sentinel should define a head-aware cache key")
	assert.Contains(t, text, "test-data-head-sha.txt", "Test Quality Sentinel should persist cache head SHA marker")
	assert.Contains(t, text, "set -uo pipefail", "Test Quality Sentinel prefetch should not hard-fail under errexit before the agent can noop")
	assert.Contains(t, text, "test-prefetch-unavailable.txt", "Test Quality Sentinel should write a fallback marker when prefetch data is unavailable")
	assert.Contains(t, text, "Test Quality Sentinel skipped because pre-fetch PR data was unavailable", "Test Quality Sentinel prompt should instruct the agent to noop with the fallback reason")
}

func TestImpeccableSkillsReviewerHasDeterministicSkillSelectionGuidance(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "impeccable-skills-reviewer.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "Should read impeccable-skills-reviewer workflow")

	text := string(content)
	assert.Contains(t, text, "pbakaus/impeccable/.agents/skills/impeccable@19786e7a225c3688e558f8694a7c8c6a8a25d840", "Impeccable reviewer should install the pinned skill")
	assert.Contains(t, text, "using the first matching row", "Impeccable reviewer should select modes deterministically")
	assert.Contains(t, text, "`tests_only`")
	assert.Contains(t, text, "`bug_fix`")
	assert.Contains(t, text, "`new_feature`")
	assert.Contains(t, text, "`refactor_cleanup`")
	assert.Contains(t, text, "`documentation`")
	assert.Contains(t, text, "`mixed_unclear`")
	assert.Contains(t, text, "Select 1–2 Impeccable modes", "Impeccable reviewer should limit selected modes")
	assert.Contains(t, text, "If the Impeccable skill cannot be found or read, do not abort", "Impeccable reviewer should continue when skill discovery fails")
}

func assertSubstringsInOrder(t *testing.T, text string, substrings []string, msgAndArgs ...any) {
	t.Helper()

	searchFrom := 0
	for _, substring := range substrings {
		relativeIndex := -1
		if searchFrom <= len(text) {
			relativeIndex = strings.Index(text[searchFrom:], substring)
		}
		require.NotEqual(t, -1, relativeIndex, msgAndArgs...)
		index := searchFrom + relativeIndex
		searchFrom = index + len(substring)
	}
}
