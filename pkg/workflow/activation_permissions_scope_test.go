//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivationPermissionsIssueOnlyReactionAndStatusComment(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-issues-only")
	testFile := filepath.Join(tmpDir, "issue-only.md")
	testContent := `---
on:
  reaction: eyes
  status-comment: true
  issues:
    types: [opened]
engine: copilot
safe-outputs:
  add-comment:
---

# Issue-only activation permissions
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write for issue trigger reactions/comments")
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write for issue-only triggers")
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions: write for issue-only triggers")
}

func TestActivationPermissionsPRReviewReactionOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-pr-review")
	testFile := filepath.Join(tmpDir, "pr-review-reaction.md")
	testContent := `---
on:
  reaction: eyes
  status-comment: false
  pull_request_review_comment:
    types: [created]
engine: copilot
---

# PR review reaction permissions
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "pull-requests: write", "activation job should include pull-requests: write for PR review comment reactions")
	assert.NotContains(t, activationJobSection, "issues: write", "activation job should not include issues: write for PR review comment reactions without status comments")
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions: write for PR review comment reactions")
}

func TestActivationPermissionsPullRequestReactionRequiresPullRequestsWrite(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-pull-request-reaction")
	testFile := filepath.Join(tmpDir, "pull-request-reaction.md")
	testContent := `---
on:
  reaction: eyes
  status-comment: false
  pull_request:
    types: [opened]
engine: copilot
---

# Pull request reaction permissions
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write for pull_request reactions")
	assert.Contains(t, activationJobSection, "pull-requests: write", "activation job should include pull-requests: write for pull_request reactions")
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions: write for pull_request reactions")
}

func TestActivationPermissionsReactionPullRequestsDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-reaction-pr-disabled")
	testFile := filepath.Join(tmpDir, "reaction-pr-disabled.md")
	testContent := `---
on:
  reaction:
    type: eyes
    pull-requests: false
  status-comment: false
  pull_request:
    types: [opened]
engine: copilot
---

# Reaction pull_request target disabled
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	reactionStep := extractWorkflowStepByName(t, activationJobSection, "Add eyes reaction for immediate feedback")
	assert.Contains(t, reactionStep, "github.event_name == 'issues'", "reaction condition should still include issues when reaction.issues is enabled")
	assert.Contains(t, reactionStep, "github.event_name == 'discussion'", "reaction condition should still include discussions when reaction.discussions is enabled")
	assert.NotContains(t, reactionStep, "github.event_name == 'pull_request'", "reaction condition should exclude pull_request when reaction.pull-requests is false")
	assert.NotContains(t, reactionStep, "github.event_name == 'pull_request_review_comment'", "reaction condition should exclude pull_request_review_comment when reaction.pull-requests is false")
	assert.NotContains(t, activationJobSection, "issues: write", "activation job should not include issues: write when pull request reactions are disabled")
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write when pull request reactions are disabled")
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions: write when no discussion triggers are configured")
}

func TestActivationPermissionsStatusCommentDiscussionsDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-status-comment-discussions-disabled")
	testFile := filepath.Join(tmpDir, "status-comment-discussions-disabled.md")
	testContent := `---
on:
  reaction: none
  status-comment:
    discussions: false
  discussion:
    types: [created]
engine: copilot
---

# Status comment discussions disabled
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions: write when status-comment.discussions is false")
	assert.Contains(t, activationJobSection, "Add comment with workflow run link", "activation job should still include status comment step when enabled")
	assert.Contains(t, activationJobSection, "github.event_name == 'issues'", "status comment condition should still include issue events")
	assert.Contains(t, activationJobSection, "github.event_name == 'issue_comment'", "status comment condition should still include issue_comment events")
	assert.NotContains(t, activationJobSection, "github.event_name == 'discussion'", "status comment condition should not include discussion events when status-comment.discussions is false")
	assert.NotContains(t, activationJobSection, "github.event_name == 'discussion_comment'", "status comment condition should not include discussion_comment events when status-comment.discussions is false")
}

func TestAddActivationInteractionPermissionsMapFallsBackOnInvalidOnYAML(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         "on: [",
		hasReaction:                       true,
		reactionIncludesIssues:            true,
		reactionIncludesPullRequests:      true,
		reactionIncludesDiscussions:       true,
		hasStatusComment:                  true,
		statusCommentIncludesIssues:       true,
		statusCommentIncludesPullRequests: true,
		statusCommentIncludesDiscussions:  true,
	})

	assert.Equal(t, PermissionWrite, permsMap[PermissionIssues], "fallback should include issues:write")
	assert.Equal(t, PermissionWrite, permsMap[PermissionPullRequests], "fallback should include pull-requests:write")
	assert.Equal(t, PermissionWrite, permsMap[PermissionDiscussions], "fallback should include discussions:write when enabled")
}

func TestAddActivationInteractionPermissionsMapFallbackRespectsStatusCommentDiscussionsToggle(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         "name: no-on-key",
		hasReaction:                       false,
		reactionIncludesIssues:            true,
		reactionIncludesPullRequests:      true,
		reactionIncludesDiscussions:       true,
		hasStatusComment:                  true,
		statusCommentIncludesIssues:       true,
		statusCommentIncludesPullRequests: true,
		statusCommentIncludesDiscussions:  false,
	})

	assert.Equal(t, PermissionWrite, permsMap[PermissionIssues], "fallback should include issues:write for status comments")
	_, hasPullRequests := permsMap[PermissionPullRequests]
	assert.False(t, hasPullRequests, "fallback should omit pull-requests:write when only status comments are enabled")
	_, hasDiscussions := permsMap[PermissionDiscussions]
	assert.False(t, hasDiscussions, "fallback should omit discussions:write when status-comment.discussions is false and reactions are disabled")
}

func TestAddActivationInteractionPermissionsMapFallbackIgnoresStatusCommentDefaultsWhenDisabled(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         "on: [",
		hasReaction:                       true,
		reactionIncludesIssues:            false,
		reactionIncludesPullRequests:      false,
		reactionIncludesDiscussions:       true,
		hasStatusComment:                  false,
		statusCommentIncludesIssues:       true,
		statusCommentIncludesPullRequests: true,
		statusCommentIncludesDiscussions:  true,
	})

	_, hasIssues := permsMap[PermissionIssues]
	assert.False(t, hasIssues, "fallback should not include issues:write when status-comment is disabled")
	_, hasPullRequests := permsMap[PermissionPullRequests]
	assert.False(t, hasPullRequests, "fallback should not include pull-requests:write for discussions-only reactions")
	assert.Equal(t, PermissionWrite, permsMap[PermissionDiscussions], "fallback should include discussions:write for discussions reactions")
}

func TestActivationPermissionsStatusCommentIssuesDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-status-comment-issues-disabled")
	testFile := filepath.Join(tmpDir, "status-comment-issues-disabled.md")
	testContent := `---
on:
  reaction: none
  status-comment:
    issues: false
  issues:
    types: [opened]
  discussion:
    types: [created]
engine: copilot
---

# Status comment issues disabled
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "discussions: write", "activation job should include discussions: write for discussion status comments")
	assert.NotContains(t, activationJobSection, "issues: write", "activation job should not include issues: write when status-comment.issues is false and reactions are disabled")
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write when status-comment.issues is false and reactions are disabled")
	assert.Contains(t, activationJobSection, "github.event_name == 'discussion'", "status comment condition should include discussion events when status-comment.issues is false")
	assert.NotContains(t, activationJobSection, "github.event_name == 'issues'", "status comment condition should not include issue events when status-comment.issues is false")
	assert.NotContains(t, activationJobSection, "github.event_name == 'issue_comment'", "status comment condition should not include issue_comment events when status-comment.issues is false")
}

func TestAddActivationInteractionPermissionsMapFallbackRespectsStatusCommentIssuesToggle(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         "name: no-on-key",
		hasReaction:                       false,
		reactionIncludesIssues:            true,
		reactionIncludesPullRequests:      true,
		reactionIncludesDiscussions:       true,
		hasStatusComment:                  true,
		statusCommentIncludesIssues:       false,
		statusCommentIncludesPullRequests: false,
		statusCommentIncludesDiscussions:  true,
	})

	_, hasIssues := permsMap[PermissionIssues]
	_, hasPullRequests := permsMap[PermissionPullRequests]
	assert.False(t, hasIssues, "fallback should omit issues:write when status-comment.issues is false and reactions are disabled")
	assert.False(t, hasPullRequests, "fallback should omit pull-requests:write when status-comment.issues is false and reactions are disabled")
	assert.Equal(t, PermissionWrite, permsMap[PermissionDiscussions], "fallback should include discussions:write when status-comment.discussions is true")
}

func TestStatusCommentObjectRejectsAllTargetsDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "status-comment-object-all-disabled")
	testFile := filepath.Join(tmpDir, "status-comment-object-all-disabled.md")
	testContent := `---
on:
  status-comment:
    issues: false
    pull-requests: false
    discussions: false
  issues:
    types: [opened]
engine: copilot
---

# Invalid status comment object
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.Error(t, err, "compilation should fail when status-comment object disables all targets")
	require.ErrorContains(t, err, "status-comment object requires at least one target to be enabled", "error should explain invalid status-comment object configuration")
}

func TestActivationPermissionsStatusCommentPullRequestsDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-status-comment-pull-requests-disabled")
	testFile := filepath.Join(tmpDir, "status-comment-pull-requests-disabled.md")
	testContent := `---
on:
  reaction: none
  status-comment:
    pull-requests: false
  issues:
    types: [opened]
  pull_request:
    types: [opened]
engine: copilot
---

# Status comment pull-requests disabled
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write for issue status comments")
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write when reactions are disabled")
	statusCommentStep := extractActivationStepByName(t, activationJobSection, "Add comment with workflow run link")
	assert.Contains(t, statusCommentStep, "github.event_name == 'issues'", "status comment condition should include issue events")
	assert.Contains(t, statusCommentStep, "github.event_name == 'issue_comment'", "status comment condition should include issue_comment events")
	assert.NotContains(t, statusCommentStep, "github.event_name == 'pull_request_review_comment'", "status comment condition should not include pull_request_review_comment when status-comment.pull-requests is false")
}

func extractActivationStepByName(t *testing.T, activationJobSection, stepName string) string {
	t.Helper()

	start := strings.Index(activationJobSection, "- name: "+stepName)
	require.NotEqualf(t, -1, start, "activation job should include step %q", stepName)

	step := activationJobSection[start:]
	if next := strings.Index(step[len("- name: "+stepName):], "\n      - name: "); next != -1 {
		step = step[:len("- name: "+stepName)+next]
	}
	return step
}

func TestAddActivationInteractionPermissionsMapFallbackRespectsStatusCommentPullRequestsToggle(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         "name: no-on-key",
		hasReaction:                       false,
		reactionIncludesIssues:            true,
		reactionIncludesPullRequests:      true,
		reactionIncludesDiscussions:       true,
		hasStatusComment:                  true,
		statusCommentIncludesIssues:       false,
		statusCommentIncludesPullRequests: false,
		statusCommentIncludesDiscussions:  true,
	})

	_, hasIssues := permsMap[PermissionIssues]
	_, hasPullRequests := permsMap[PermissionPullRequests]
	assert.False(t, hasIssues, "fallback should omit issues:write when status-comment.issues and status-comment.pull-requests are false and reactions are disabled")
	assert.False(t, hasPullRequests, "fallback should omit pull-requests:write when reactions are disabled")
	assert.Equal(t, PermissionWrite, permsMap[PermissionDiscussions], "fallback should include discussions:write when status-comment.discussions is true")
}

// TestActivationPermissionsIssueCommentReactionRequiresPullRequestsWrite verifies that
// issue_comment triggers with PR reactions grant pull-requests:write. This covers the
// slash_command case (events:[pull_request_comment] compiles to issue_comment) where
// reactions on PR comments require pull-requests:write even though the API uses /issues/comments.
func TestActivationPermissionsIssueCommentReactionRequiresPullRequestsWrite(t *testing.T) {
	permsMap := map[PermissionScope]PermissionLevel{}

	onSection := "on:\n  issue_comment:\n    types: [created]\n"
	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         onSection,
		hasReaction:                       true,
		reactionIncludesIssues:            true,
		reactionIncludesPullRequests:      true,
		reactionIncludesDiscussions:       false,
		hasStatusComment:                  false,
		statusCommentIncludesIssues:       false,
		statusCommentIncludesPullRequests: false,
		statusCommentIncludesDiscussions:  false,
	})

	assert.Equal(t, PermissionWrite, permsMap[PermissionIssues], "issue_comment reaction should include issues:write")
	assert.Equal(t, PermissionWrite, permsMap[PermissionPullRequests], "issue_comment reaction should include pull-requests:write because PR comments use issue_comment event")
	_, hasDiscussions := permsMap[PermissionDiscussions]
	assert.False(t, hasDiscussions, "issue_comment reaction should not include discussions:write")
}

// TestActivationPermissionsCentralizedSlashCommandIssueCommentReaction verifies that a
// centralized slash_command workflow with issue_comment events and a reaction gets
// issues:write and pull-requests:write permissions on the activation job.
// Centralized workflows compile to workflow_dispatch, so the compiler must derive permissions
// from the declared command events rather than the "on" trigger section.
func TestActivationPermissionsCentralizedSlashCommandIssueCommentReaction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-centralized-slash-issue-comment")
	testFile := filepath.Join(tmpDir, "centralized-issue-comment.md")
	testContent := `---
on:
  slash_command:
    name: plan
    strategy: centralized
    events: [issue_comment, discussion_comment]
  reaction: eyes
engine: copilot
---

# Plan
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write",
		"centralized slash_command with issue_comment events should include issues:write for reactions")
	assert.Contains(t, activationJobSection, "pull-requests: write",
		"centralized slash_command with issue_comment events should include pull-requests:write (issue_comment fires for PR comments)")
	assert.Contains(t, activationJobSection, "discussions: write",
		"centralized slash_command with discussion_comment events should include discussions:write for reactions")
}

// TestActivationPermissionsCentralizedSlashCommandDiscussionOnlyReaction verifies that a
// centralized slash_command workflow with only discussion events gets discussions:write but
// not issues:write or pull-requests:write.
func TestActivationPermissionsCentralizedSlashCommandDiscussionOnlyReaction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-centralized-slash-discussion-only")
	testFile := filepath.Join(tmpDir, "centralized-discussion-only.md")
	testContent := `---
on:
  slash_command:
    name: discuss
    strategy: centralized
    events: [discussion_comment]
  reaction: eyes
engine: copilot
---

# Discuss
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "discussions: write",
		"centralized slash_command with discussion_comment events should include discussions:write for reactions")
	assert.NotContains(t, activationJobSection, "issues: write",
		"centralized slash_command with only discussion events should not include issues:write")
	assert.NotContains(t, activationJobSection, "pull-requests: write",
		"centralized slash_command with only discussion events should not include pull-requests:write")
}

// TestActivationPermissionsCentralizedSlashCommandNoReactionNoPermissions verifies that a
// centralized slash_command workflow without reactions or status comments does not gain
// unexpected write permissions.
func TestActivationPermissionsCentralizedSlashCommandNoReactionNoPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-centralized-slash-no-reaction")
	testFile := filepath.Join(tmpDir, "centralized-no-reaction.md")
	testContent := `---
on:
  slash_command:
    name: deploy
    strategy: centralized
    events: [issue_comment, discussion_comment]
  reaction: none
  status-comment: false
engine: copilot
---

# Deploy
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.NotContains(t, activationJobSection, "issues: write",
		"centralized slash_command without reactions or status-comments should not include issues:write")
	assert.NotContains(t, activationJobSection, "discussions: write",
		"centralized slash_command without reactions or status-comments should not include discussions:write")
}

// TestActivationPermissionsSlashCommandPRCommentReactionRequiresPullRequestsWrite verifies
// end-to-end that a slash_command workflow with events:[pull_request_comment] produces an
// activation job with pull-requests:write. slash_command compiles to issue_comment, and
// GitHub requires pull-requests:write to react to PR comments (#26720 follow-up).
func TestActivationPermissionsSlashCommandPRCommentReactionRequiresPullRequestsWrite(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-slash-command-pr-comment")
	testFile := filepath.Join(tmpDir, "slash-command-pr-comment.md")
	testContent := `---
on:
  slash_command:
    name: review
    events: [pull_request_comment]
  reaction: eyes
  status-comment: false
engine: copilot
---

# Slash command PR comment reaction permissions
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues:write for PR comment reactions via issue_comment event")
	assert.Contains(t, activationJobSection, "pull-requests: write", "activation job should include pull-requests:write for slash_command PR comment reactions")
	assert.NotContains(t, activationJobSection, "discussions: write", "activation job should not include discussions:write for slash_command PR comment reactions")
}

func TestActivationPermissionsCentralizedSlashCommandDiscussionStatusCommentWithGitHubApp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-centralized-slash-discussion-status-comment-app")
	testFile := filepath.Join(tmpDir, "centralized-discussion-status-comment-app.md")
	testContent := `---
on:
  slash_command:
    name: discuss
    strategy: centralized
    events: [discussion_comment]
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
  reaction: none
  status-comment: true
engine: copilot
---

# Centralized slash command status-comment with activation GitHub App
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "permission-discussions: write", "activation app token should include permission-discussions: write for centralized slash_command discussion status comments")
	assert.NotContains(t, activationJobSection, "permission-issues: write", "activation app token should not include permission-issues: write for discussions-only centralized status comments")
}

// Tests for workflow_call trigger + reaction/status-comment permissions (issue #39372).
// When a workflow declares workflow_call as its trigger it acts as a reusable workflow and can
// be called from ANY caller event.  The compiler cannot know the caller's event type at compile
// time, so it must grant the full set of permissions that the configured reactions / status-comments
// could require.

func TestActivationPermissionsWorkflowCallReaction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-reaction")
	testFile := filepath.Join(tmpDir, "workflow-call-reaction.md")
	testContent := `---
on:
  workflow_call:
  reaction: eyes
engine: copilot
---

# Reusable workflow with reaction
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write when workflow_call + reaction are configured")
	assert.Contains(t, activationJobSection, "pull-requests: write", "activation job should include pull-requests: write when workflow_call + reaction are configured")
	assert.Contains(t, activationJobSection, "discussions: write", "activation job should include discussions: write when workflow_call + reaction are configured")
}

func TestActivationPermissionsWorkflowCallReactionDiscussionsOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-reaction-discussions-only")
	testFile := filepath.Join(tmpDir, "workflow-call-reaction-discussions-only.md")
	testContent := `---
on:
  workflow_call:
  reaction:
    type: eyes
    issues: false
    pull-requests: false
    discussions: true
engine: copilot
---

# Reusable workflow with discussions-only reaction
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.NotContains(t, activationJobSection, "issues: write", "activation job should not include issues: write when workflow_call + discussions-only reaction are configured")
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write when workflow_call + discussions-only reaction are configured")
	assert.Contains(t, activationJobSection, "discussions: write", "activation job should include discussions: write when workflow_call + discussions-only reaction are configured")
}

func TestActivationPermissionsWorkflowCallStatusComment(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-status-comment")
	testFile := filepath.Join(tmpDir, "workflow-call-status-comment.md")
	testContent := `---
on:
  workflow_call:
  reaction: none
  status-comment: true
engine: copilot
---

# Reusable workflow with status-comment only
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write when workflow_call + status-comment are configured")
	// pull-requests:write is only needed for PR reactions (addBroadActivationInteractionPermissions only sets it for
	// hasReaction && reactionIncludesPullRequests); PR status-comments post via the issues API so issues:write suffices.
	assert.NotContains(t, activationJobSection, "pull-requests: write", "activation job should not include pull-requests: write for status-comment-only (PR status-comments use the issues API scope, not pull-requests)")
	// discussions:write is included because status-comment defaults include discussions (statusCommentIncludesDiscussions=true by default)
	assert.Contains(t, activationJobSection, "discussions: write", "activation job should include discussions: write when workflow_call + status-comment are configured (discussions enabled by default)")
}

func TestActivationPermissionsWorkflowCallAndIssuesTriggerReaction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-and-issues-reaction")
	testFile := filepath.Join(tmpDir, "workflow-call-and-issues-reaction.md")
	testContent := `---
on:
  workflow_call:
  issues:
    types: [labeled]
  reaction: eyes
engine: copilot
---

# Reusable workflow with workflow_call and issues trigger
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: write", "activation job should include issues: write when workflow_call+issues triggers + reaction are configured")
	assert.Contains(t, activationJobSection, "pull-requests: write", "activation job should include pull-requests: write when workflow_call is present (broad permissions)")
	assert.Contains(t, activationJobSection, "discussions: write", "activation job should include discussions: write when workflow_call is present (broad permissions)")
}

func TestActivationPermissionsWorkflowCallReactionWithGitHubApp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-reaction-app")
	testFile := filepath.Join(tmpDir, "workflow-call-reaction-app.md")
	testContent := `---
on:
  workflow_call:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
  reaction: eyes
engine: copilot
---

# Reusable workflow with reaction and activation GitHub App
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "permission-issues: write", "activation app token should include permission-issues: write when workflow_call + reaction are configured")
	assert.Contains(t, activationJobSection, "permission-pull-requests: write", "activation app token should include permission-pull-requests: write when workflow_call + reaction are configured")
	assert.Contains(t, activationJobSection, "permission-discussions: write", "activation app token should include permission-discussions: write when workflow_call + reaction are configured")
}

func TestActivationPermissionsWorkflowCallReactionDiscussionsOnlyWithGitHubApp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-workflow-call-reaction-discussions-only-app")
	testFile := filepath.Join(tmpDir, "workflow-call-reaction-discussions-only-app.md")
	testContent := `---
on:
  workflow_call:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
  reaction:
    type: eyes
    issues: false
    pull-requests: false
    discussions: true
engine: copilot
---

# Reusable workflow with discussions-only reaction and activation GitHub App
`

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.NotContains(t, activationJobSection, "permission-issues: write", "activation app token should not include permission-issues: write for workflow_call + discussions-only reaction")
	assert.NotContains(t, activationJobSection, "permission-pull-requests: write", "activation app token should not include permission-pull-requests: write for workflow_call + discussions-only reaction")
	assert.Contains(t, activationJobSection, "permission-discussions: write", "activation app token should include permission-discussions: write for workflow_call + discussions-only reaction")
}
