//go:build !integration

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow/aw"
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
		"# Skip diff fetch",
	}, "Shared PR prefetch should refresh review comments before checking cached diff data")
	assert.NotContains(t, text, `-f /tmp/gh-aw/agent/pr-review-comments.json ]; then`, "Review comments should not control diff cache hits")
	assert.Contains(t, text, "if ! gh api --paginate", "Shared PR prefetch should reject GitHub API failures")
	assert.Contains(t, text, "Failed to fetch files for PR #${PR_NUMBER}", "Shared PR prefetch should report GitHub API failures")
	assert.Contains(t, text, ".headRefOid == $sha and .number == $number and (.files | type == \"array\")", "Shared PR prefetch should validate cached PR identity and file metadata")
	assert.Contains(t, text, "Failed to refresh metadata for PR #${PR_NUMBER}", "Shared PR prefetch should reject mismatched fresh metadata")
}

func TestPRDataPrefetchRejectsAPIFailuresBeforeSavingCacheData(t *testing.T) {
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
		assert.Contains(t, text, "if ! gh api --paginate", "%s should reject GitHub API failures", workflow)
		assert.Contains(t, text, "Failed to fetch files for PR #${PR_NUMBER}", "%s should report GitHub API failures", workflow)
		assertSubstringsInOrder(t, text, []string{
			"if ! gh api --paginate",
			`echo "::error::Failed to fetch files for PR #${PR_NUMBER}: $(cat /tmp/gh-aw/agent/pr-diff.err)" >&2`,
			"exit 1",
			`head -n "${PR_DIFF_MAX_LINES}" /tmp/gh-aw/agent/pr-diff.full > /tmp/gh-aw/agent/pr-diff.patch`,
			"pr-data-head-sha.txt",
		}, "%s should reject API failures before saving a valid cache head marker", workflow)
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
		assert.Contains(t, text, "key: pr-prefetch-v2-${{ github.event.pull_request.number || github.event.issue.number", "%s should isolate cache data by PR number", workflow)
		assert.Contains(t, text, "${{ github.event.pull_request.head.sha || 'unknown' }}-${{ github.run_id }}-${{ github.run_attempt }}", "%s should use revisioned head-aware keys so refreshed data can be saved", workflow)
		assert.Contains(t, text, "item_type == 'pull_request'", "%s should only use aw_context item_number for pull requests", workflow)
		assert.Contains(t, text, "pr-prefetch-v2-${{ github.event.pull_request.number || github.event.issue.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request'", "%s should restore cache data with a centralized slash-command fallback", workflow)
		assert.Contains(t, text, "${{ github.event.pull_request.head.sha && format('{0}-', github.event.pull_request.head.sha) || '' }}", "%s should restore the current head prefix, or a PR-scoped prefix for slash commands", workflow)
	}

	mattWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "mattpocock-skills-reviewer.md")
	mattContent, err := os.ReadFile(mattWorkflowPath)
	require.NoError(t, err, "Should read mattpocock-skills-reviewer workflow")
	assert.Contains(t, string(mattContent), `${GITHUB_WORKSPACE}/.github/skills`, "Matt reviewer should discover skills from the restored workspace directory")

	sentinelWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "test-quality-sentinel.md")
	sentinelContent, err := os.ReadFile(sentinelWorkflowPath)
	require.NoError(t, err, "Should read test-quality-sentinel workflow")
	text := string(sentinelContent)
	assert.Contains(t, text, "key: pr-test-prefetch-v2-${{ github.event.pull_request.number || github.event.issue.number ||", "Test Quality Sentinel should isolate cache data by PR")
	assert.Contains(t, text, "${{ github.event.pull_request.head.sha || 'unknown' }}-${{ github.run_id }}-${{ github.run_attempt }}", "Test Quality Sentinel should save refreshed cache revisions")
	assert.Contains(t, text, "item_type == 'pull_request'", "Test Quality Sentinel should resolve centralized slash-command PR context")
	assert.Contains(t, text, "pre-agent-steps:", "Test Quality Sentinel should validate evidence after cache restoration")
	assert.Contains(t, text, "test-data-head-sha.txt", "Test Quality Sentinel should persist cache head SHA marker")
	assert.Contains(t, text, "set -uo pipefail", "Test Quality Sentinel prefetch should not hard-fail under errexit before the agent can report_incomplete")
	assert.Contains(t, text, "test-prefetch-unavailable.txt", "Test Quality Sentinel should write a fallback marker when prefetch data is unavailable")
	assert.Contains(t, text, `{"report_incomplete": {"reason": "Test Quality Sentinel could not review tests because pre-fetch PR data was unavailable: <reason>"}}`, "Test Quality Sentinel prompt should instruct the agent to report_incomplete with the fallback reason")
	assert.Contains(t, text, "immediately call `report_incomplete` (never `noop`)", "Test Quality Sentinel prompt should forbid noop when prefetch data is unavailable")
}

func TestSentinelPrefetchDiffEvidence(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "test-quality-sentinel.md"))
	require.NoError(t, err)
	document, err := aw.Parse(string(content))
	require.NoError(t, err)
	step := document.Frontmatter["pre-agent-steps"].([]any)[0].(map[string]any)
	script := step["run"].(string)
	diff := "diff --git a/src/main.go b/src/main.go\n--- a/src/main.go\n+++ b/src/main.go\n@@ -1 +1 @@\n-old\n+non-test\n" +
		"diff --git a/pkg/example_test.go b/pkg/example_test.go\n--- a/pkg/example_test.go\n+++ b/pkg/example_test.go\n@@ -1 +1 @@\n-old\n+go behavior\n" +
		"diff --git a/scripts/a test.test.cjs b/scripts/a test.test.cjs\n--- a/scripts/a test.test.cjs\n+++ b/scripts/a test.test.cjs\n@@ -1 +1 @@\n-old\n+js behavior\n" +
		"diff --git a/docs/readme.md b/docs/readme.md\n--- a/docs/readme.md\n+++ b/docs/readme.md\n@@ -1 +1 @@\n-old\n+documentation\n"
	const section = "diff --git a/pkg/example_test.go b/pkg/example_test.go\n"
	for _, tc := range []struct {
		name, mode, diff, unavailable string
		cache                         bool
	}{
		{name: "Go and JS sections with spaces", diff: diff},
		{name: "quoted diff paths", diff: strings.Replace(diff, "a/scripts/a test.test.cjs b/scripts/a test.test.cjs", `"a/scripts/a test.test.cjs" "b/scripts/a test.test.cjs"`, 1)},
		{name: "exact byte limit", diff: section + strings.Repeat("x", 40000-len(section)-1) + "\n"},
		{name: "fetch error", mode: "fail-diff", diff: diff, unavailable: "unable to fetch test file diff"},
		{name: "empty test diff", diff: "diff --git a/main.go b/main.go\n+code\n", unavailable: "test file diff is missing"},
		{name: "over byte limit", diff: section + strings.Repeat("x", 40001-len(section)), unavailable: "exceeds the 40 KB"},
		{name: "stale metadata", mode: "stale-metadata", diff: diff, unavailable: "PR metadata does not match"},
		{name: "head changes during fetch", mode: "changed-head", diff: diff, unavailable: "PR head changed"},
		{name: "local diff unavailable", mode: "fail-git", diff: diff, unavailable: "unable to compute PR diff statistics"},
		{name: "slash-command head and base resolution", mode: "slash-command", diff: diff},
		{name: "no tests clears restored evidence", mode: "no-tests", cache: true},
		{name: "wrong PR cache is refreshed", diff: diff, cache: true},
		{name: "fallback cache is refreshed", mode: "fallback-cache", diff: diff, cache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			agent := filepath.Join(dir, "agent")
			bin := filepath.Join(dir, "bin")
			require.NoError(t, os.MkdirAll(agent, 0o755))
			require.NoError(t, os.MkdirAll(bin, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "diff"), []byte(tc.diff), 0o600))
			gh := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE/calls"
if [ "$1 $2" = "pr view" ]; then
  if [ "$*" = "pr view 42 --json headRefOid --jq .headRefOid" ]; then
    if [ "$MODE" = changed-head ]; then echo new-head; else echo head-42; fi
  elif [ "$*" = "pr view 42 --json number,files,additions,deletions,baseRefName,baseRefOid,headRefName,headRefOid" ]; then
    head=head-42
    if [ "$MODE" = stale-metadata ]; then head=old-head; fi
    printf '{"number":42,"headRefOid":"%s","baseRefOid":"base-42","files":[]}\n' "$head"
  else
    echo "Unexpected metadata arguments: $*" >&2; exit 1
  fi
elif [ "$*" = "pr diff 42 --name-only" ]; then
  if [ "$MODE" != no-tests ]; then
    printf '%s\n' pkg/example_test.go
    if [ "$TEST_COUNT" = 2 ]; then printf '%s\n' 'scripts/a test.test.cjs'; fi
  fi
elif [ "$*" = "pr diff 42 --color=never" ]; then
  if [ "$MODE" = fail-diff ]; then
    printf 'diff --git a/pkg/example_test.go b/pkg/example_test.go\n+partial\n'
    echo 'gh: fixture HTTP 503' >&2; exit 1
  fi
  cat "$FIXTURE/diff"
else
  echo "Unexpected gh arguments: $*" >&2; exit 1
fi
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(`#!/bin/bash
if [ "$MODE" = fail-git ]; then echo 'fatal: fixture missing base history' >&2; exit 1; fi
case "$*" in
  "diff base-42...HEAD --numstat"|"diff base-42...HEAD --diff-filter=A --name-only") exit 0 ;;
  *) echo "Unexpected git arguments: $*" >&2; exit 1 ;;
esac
`), 0o700))
			if tc.cache {
				for _, file := range []string{"test-diff.txt", "test-files.txt", "diff-numstat.txt", "go-new-test-funcs.txt"} {
					require.NoError(t, os.WriteFile(filepath.Join(agent, file), []byte("stale evidence\n"), 0o600))
				}
				require.NoError(t, os.WriteFile(filepath.Join(agent, "test-data-head-sha.txt"), []byte("head-42\n"), 0o600))
				number := 99
				if tc.mode == "fallback-cache" {
					number = 42
					require.NoError(t, os.WriteFile(filepath.Join(agent, "test-prefetch-unavailable.txt"), []byte("previous failure"), 0o600))
				}
				require.NoError(t, os.WriteFile(filepath.Join(agent, "pr-meta.json"),
					[]byte(fmt.Sprintf(`{"number":%d,"headRefOid":"head-42","files":[]}`, number)), 0o600))
			}
			command := exec.Command("bash", "-c", strings.ReplaceAll(script, "/tmp/gh-aw/agent", agent))
			command.Env = []string{
				"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"FIXTURE=" + dir, "MODE=" + tc.mode, "PR_NUMBER=42", "PR_HEAD_SHA=head-42",
				"EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA=base-42", "TMPDIR=" + dir,
			}
			if strings.Contains(tc.diff, "+js behavior") {
				command.Env = append(command.Env, "TEST_COUNT=2")
			} else {
				command.Env = append(command.Env, "TEST_COUNT=1")
			}
			if tc.mode == "slash-command" {
				command.Env = append(command.Env, "PR_HEAD_SHA=", "EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA=")
			}
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			read := func(file string) string {
				t.Helper()
				data, readErr := os.ReadFile(filepath.Join(agent, file))
				require.NoError(t, readErr)
				return string(data)
			}
			if tc.unavailable != "" {
				assert.Contains(t, read("test-prefetch-unavailable.txt"), tc.unavailable)
				assert.Empty(t, read("test-diff.txt"))
				assert.NoFileExists(t, filepath.Join(agent, "test-data-head-sha.txt"))
				if tc.mode == "fail-diff" {
					assert.Contains(t, string(output), "gh: fixture HTTP 503", "Fetch stderr must remain visible")
				}
				return
			}
			assert.NoFileExists(t, filepath.Join(agent, "test-prefetch-unavailable.txt"))
			assert.Equal(t, "head-42\n", read("test-data-head-sha.txt"))
			actual := read("test-diff.txt")
			if tc.mode == "no-tests" {
				assert.Empty(t, actual)
				assert.Empty(t, read("go-new-test-funcs.txt"))
			} else if tc.name == "exact byte limit" {
				assert.Len(t, actual, 40000)
			} else {
				assert.Contains(t, actual, "+go behavior")
				assert.Contains(t, actual, "+js behavior")
				assert.NotContains(t, actual, "non-test")
				assert.NotContains(t, actual, "documentation")
				assert.NotContains(t, actual, "stale evidence")
				assert.Contains(t, string(output), "Pre-fetched 2 test files")
			}
		})
	}
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
	assert.Contains(t, text, "pbakaus/impeccable/.agents/skills/impeccable@924bbd194d02e5e15a227e75b5901fb0d7a4ecad", "Impeccable reviewer should install the pinned skill")
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
