//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inferPermissionsFromShellScriptsForTest(t *testing.T, scripts []string) map[PermissionScope]PermissionLevel {
	t.Helper()
	perms, err := inferPermissionsFromShellScripts(scripts)
	if err != nil {
		t.Fatalf("inferPermissionsFromShellScripts() error = %v", err)
	}
	return perms
}

func detectWriteCommandsInShellScriptsForTest(t *testing.T, scripts []string) []string {
	t.Helper()
	cmds, err := detectWriteCommandsInShellScripts(scripts)
	if err != nil {
		t.Fatalf("detectWriteCommandsInShellScripts() error = %v", err)
	}
	return cmds
}

func TestInferGitHubAppTokenPermissionsFromShellScripts(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		wantField    string
		wantInferred bool
	}{
		{name: "known read command", script: "gh pr view 123", wantField: "permission-pull-requests", wantInferred: true},
		{name: "known read API", script: "gh api /repos/owner/repo/issues", wantField: "permission-issues", wantInferred: true},
		{name: "explicit GET API", script: "gh api --method GET /repos/owner/repo/issues", wantField: "permission-issues", wantInferred: true},
		{name: "actions secrets API", script: "gh api /repos/owner/repo/actions/secrets", wantField: "permission-secrets", wantInferred: true},
		{name: "write command", script: "gh pr create --title test", wantInferred: false},
		{name: "unknown command", script: "gh pr custom", wantInferred: false},
		{name: "write API", script: "gh api --method POST /repos/owner/repo/issues", wantInferred: false},
		{name: "combined write API method", script: "gh api -XPOST /repos/owner/repo/issues", wantInferred: false},
		{name: "quoted destructive method after endpoint", script: `gh api /repos/owner/repo/actions/runs/123 "--method=DELETE"`, wantInferred: false},
		{name: "unknown actions subpath", script: "gh api /repos/owner/repo/actions/unknown", wantInferred: false},
		{name: "API body fields", script: "gh api /repos/owner/repo/issues -f title=test", wantInferred: false},
		{name: "API inline body fields", script: "gh api /repos/owner/repo/issues --field=title=test", wantInferred: false},
		{name: "no GitHub CLI", script: "echo hello", wantInferred: false},
		{name: "mixed token client", script: "gh pr view 123\ncurl -X POST https://api.github.com/repos/owner/repo/issues", wantInferred: false},
		{name: "compound token use", script: "gh pr view 123 && curl https://api.github.com/repos/owner/repo/issues", wantInferred: false},
		{name: "command substitution", script: "gh pr view 123 $(curl -X POST https://api.github.com/repos/owner/repo/issues)", wantInferred: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, inferred := InferGitHubAppTokenPermissionsFromShellScripts([]string{tt.script})
			assert.Equal(t, tt.wantInferred, inferred)
			if inferred {
				assert.Equal(t, "read", fields[tt.wantField])
			} else {
				assert.Empty(t, fields)
			}
		})
	}
}

func TestInferGitHubAppTokenPermissionsIgnoresCommentOnlyCommands(t *testing.T) {
	t.Parallel()

	fields, inferred := InferGitHubAppTokenPermissionsFromShellScripts([]string{
		"gh pr view 123\n# gh api /orgs/acme/hooks",
	})
	require.True(t, inferred)
	assert.Equal(t, "read", fields["permission-pull-requests"])
	assert.NotContains(t, fields, "permission-organization-hooks")
}

// TestInferPermissionsFromShellScripts_GhPrDiff verifies that `gh pr diff` in a
// shell script is recognized as requiring pull-requests: read.
func TestInferPermissionsFromShellScripts_GhPrDiff(t *testing.T) {
	scripts := []string{
		`gh pr diff "$PR_NUMBER" --name-only | awk '/\.md$/' > /tmp/changed.txt`,
	}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests], "gh pr diff should require pull-requests: read")
}

// TestInferPermissionsFromShellScripts_GhPrView verifies pull-requests: read for gh pr view.
func TestInferPermissionsFromShellScripts_GhPrView(t *testing.T) {
	scripts := []string{`gh pr view 123 --json title`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests])
}

// TestInferPermissionsFromShellScripts_GhIssueList verifies issues: read for gh issue list.
func TestInferPermissionsFromShellScripts_GhIssueList(t *testing.T) {
	scripts := []string{`gh issue list --label bug --json number`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues])
}

// TestInferPermissionsFromShellScripts_GhWorkflowList verifies actions: read for gh workflow list.
func TestInferPermissionsFromShellScripts_GhWorkflowList(t *testing.T) {
	scripts := []string{`gh workflow list`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionActions])
}

// TestInferPermissionsFromShellScripts_GhRunView verifies actions: read for gh run view.
func TestInferPermissionsFromShellScripts_GhRunView(t *testing.T) {
	scripts := []string{`gh run view $RUN_ID --json conclusion`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionActions])
}

// TestInferPermissionsFromShellScripts_GhAPI verifies pull-requests: read for gh api pulls endpoint.
func TestInferPermissionsFromShellScripts_GhAPI(t *testing.T) {
	scripts := []string{`gh api /repos/owner/repo/pulls/1 --jq .title`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests], "gh api /repos/.../pulls should require pull-requests: read")
}

// TestInferPermissionsFromShellScripts_GhAPIWithHeaderFlag verifies that flags before the
// endpoint are skipped, e.g. gh api -H 'Accept: ...' /repos/owner/repo/pulls.
func TestInferPermissionsFromShellScripts_GhAPIWithHeaderFlag(t *testing.T) {
	scripts := []string{`gh api -H 'Accept: application/vnd.github+json' /repos/owner/repo/pulls`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests],
		"gh api with -H flag before endpoint should still infer pull-requests: read")
}

// TestInferPermissionsFromShellScripts_GhAPIWithMethodFlag verifies that --method GET
// before the endpoint is skipped properly.
func TestInferPermissionsFromShellScripts_GhAPIWithMethodFlag(t *testing.T) {
	scripts := []string{`gh api --method GET /repos/owner/repo/issues`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues],
		"gh api with --method GET before endpoint should still infer issues: read")
}

// TestInferPermissionsFromShellScripts_GhAPIQuotedEndpoint verifies that a quoted endpoint
// is correctly extracted, e.g. gh api "/repos/owner/repo/pulls".
func TestInferPermissionsFromShellScripts_GhAPIQuotedEndpoint(t *testing.T) {
	scripts := []string{`gh api "/repos/owner/repo/pulls"`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests],
		`gh api with quoted endpoint should infer pull-requests: read`)
}

// TestInferPermissionsFromShellScripts_GhAPIIssues verifies issues: read for gh api issues endpoint.
func TestInferPermissionsFromShellScripts_GhAPIIssues(t *testing.T) {
	scripts := []string{`gh api /repos/owner/repo/issues --jq '.[].number'`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues], "gh api /repos/.../issues should require issues: read")
}

// TestInferPermissionsFromShellScripts_NoGhCommand verifies no permissions are inferred when
// there are no gh CLI calls in the script.
func TestInferPermissionsFromShellScripts_NoGhCommand(t *testing.T) {
	scripts := []string{`echo "hello" && ls /tmp`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Empty(t, perms, "no gh commands should produce no permission requirements")
}

// TestInferPermissionsFromShellScripts_MultiLine verifies multi-line shell scripts work correctly.
func TestInferPermissionsFromShellScripts_MultiLine(t *testing.T) {
	scripts := []string{
		`gh pr diff "$PR_NUMBER" --name-only \
  | awk '/\.md$/' \
  > /tmp/gh-aw/docs-review-data/changed-md.txt`,
	}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests], "multi-line gh pr diff should require pull-requests: read")
}

// TestInferPermissionsFromShellScripts_MultipleCommands verifies multiple gh commands are aggregated.
func TestInferPermissionsFromShellScripts_MultipleCommands(t *testing.T) {
	scripts := []string{
		`gh pr diff "$PR_NUMBER" --name-only > /tmp/changed.txt
gh issue view $ISSUE_NUMBER --json body > /tmp/issue.json`,
	}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests], "should infer pull-requests: read")
	assert.Equal(t, PermissionRead, perms[PermissionIssues], "should infer issues: read")
}

// TestActivationJobPermissionsWithGhPrDiffPreStep is an integration test that verifies
// the compiler adds pull-requests: read to the activation job when a pre-step calls
// `gh pr diff`. This reproduces the issue reported for the gh-aw-docs-review workflow.
func TestActivationJobPermissionsWithGhPrDiffPreStep(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-gh-pr-diff")
	testFile := filepath.Join(tmpDir, "docs-review.md")
	testContent := `---
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: read
engine: copilot
jobs:
  activation:
    pre-steps:
      - name: Get changed markdown files
        run: |
          gh pr diff "$PR_NUMBER" --name-only \
            | awk '/\.md$/' \
            > /tmp/gh-aw/docs-review-data/changed-md.txt
---

# Docs review workflow with Vale pre-step
`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "pull-requests: read",
		"activation job should include pull-requests: read when pre-step calls gh pr diff")
}

// TestActivationJobPermissionsWithGhIssuePreStep verifies issues: read is added when
// an activation pre-step calls `gh issue view`.
func TestActivationJobPermissionsWithGhIssuePreStep(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-gh-issue")
	testFile := filepath.Join(tmpDir, "issue-workflow.md")
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
jobs:
  activation:
    pre-steps:
      - name: Fetch issue data
        run: |
          gh issue view "$ISSUE_NUMBER" --json body > /tmp/issue.json
---

# Issue workflow with gh issue pre-step
`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "issues: read",
		"activation job should include issues: read when pre-step calls gh issue view")
}

// TestActivationJobPermissionsNoPreStepChanges verifies that the activation job permissions
// are unchanged when there are no pre-steps with gh commands.  Even if the workflow-level
// frontmatter declares pull-requests: read, the activation job should NOT receive that
// permission unless its own steps actually need it (the activation job computes its permissions
// independently of the main job's filtered permissions).
func TestActivationJobPermissionsNoPreStepChanges(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-no-gh")
	testFile := filepath.Join(tmpDir, "basic-workflow.md")
	testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
engine: copilot
---

# Basic workflow without activation pre-steps
`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "contents: read",
		"activation job should always include contents: read")
	assert.NotContains(t, activationJobSection, "pull-requests",
		"activation job should NOT include pull-requests when no pre-step requires it")
}

// TestDetectWriteCommandsInShellScripts_GhPrCreate verifies that `gh pr create` is detected as a write command.
func TestDetectWriteCommandsInShellScripts_GhPrCreate(t *testing.T) {
	scripts := []string{`gh pr create --title "Fix bug" --body "details"`}
	cmds := detectWriteCommandsInShellScriptsForTest(t, scripts)
	require.Len(t, cmds, 1)
	assert.Equal(t, "gh pr create", cmds[0])
}

// TestDetectWriteCommandsInShellScripts_GhIssueClose verifies that `gh issue close` is detected.
func TestDetectWriteCommandsInShellScripts_GhIssueClose(t *testing.T) {
	scripts := []string{`gh issue close $ISSUE_NUMBER`}
	cmds := detectWriteCommandsInShellScriptsForTest(t, scripts)
	require.Len(t, cmds, 1)
	assert.Equal(t, "gh issue close", cmds[0])
}

// TestDetectWriteCommandsInShellScripts_ReadCommandNotDetected verifies that a read command
// (e.g. `gh pr diff`) is NOT flagged as a write command.
func TestDetectWriteCommandsInShellScripts_ReadCommandNotDetected(t *testing.T) {
	scripts := []string{`gh pr diff "$PR_NUMBER" --name-only`}
	cmds := detectWriteCommandsInShellScriptsForTest(t, scripts)
	assert.Empty(t, cmds, "gh pr diff is a read command and should not be detected as write")
}

// TestDetectWriteCommandsInShellScripts_Deduplicated verifies that duplicate write commands
// are reported only once.
func TestDetectWriteCommandsInShellScripts_Deduplicated(t *testing.T) {
	scripts := []string{
		`gh pr create --title "Fix 1"
gh pr create --title "Fix 2"`,
	}
	cmds := detectWriteCommandsInShellScriptsForTest(t, scripts)
	assert.Len(t, cmds, 1, "duplicate write commands should be deduplicated")
	assert.Equal(t, "gh pr create", cmds[0])
}

// TestDetectWriteCommandsInShellScripts_MultipleWriteCommands verifies detection of
// multiple distinct write commands.
func TestDetectWriteCommandsInShellScripts_MultipleWriteCommands(t *testing.T) {
	scripts := []string{
		`gh pr merge $PR_NUMBER --squash
gh issue comment $ISSUE_NUMBER --body "done"`,
	}
	cmds := detectWriteCommandsInShellScriptsForTest(t, scripts)
	assert.Len(t, cmds, 2)
	assert.Contains(t, cmds, "gh pr merge")
	assert.Contains(t, cmds, "gh issue comment")
}

// TestInferPermissionsFromShellScripts_GhCacheList verifies actions: read for gh cache list.
func TestInferPermissionsFromShellScripts_GhCacheList(t *testing.T) {
	scripts := []string{`gh cache list --json key`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionActions], "gh cache list should require actions: read")
}

// TestInferPermissionsFromShellScripts_GhRepoView verifies contents: read for gh repo view.
func TestInferPermissionsFromShellScripts_GhRepoView(t *testing.T) {
	scripts := []string{`gh repo view owner/repo --json description`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionContents], "gh repo view should require contents: read")
}

// TestInferPermissionsFromShellScripts_GhLabelList verifies issues: read for gh label list.
func TestInferPermissionsFromShellScripts_GhLabelList(t *testing.T) {
	scripts := []string{`gh label list --json name`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues], "gh label list should require issues: read")
}

// TestInferPermissionsFromShellScripts_GhIssueComment verifies that `gh issue comment`
// (a write command) still causes issues: read to be inferred so the permission is present
// in the activation job — the write-command check is separate.
func TestInferPermissionsFromShellScripts_GhIssueComment(t *testing.T) {
	scripts := []string{`gh issue comment $ISSUE_NUMBER --body "hello"`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues], "write commands still require at minimum read-level permission for the scope")
}

// TestInferPermissionsFromShellScripts_GhAPIReleases verifies contents: read for gh api releases.
func TestInferPermissionsFromShellScripts_GhAPIReleases(t *testing.T) {
	scripts := []string{`gh api /repos/owner/repo/releases --jq '.[0].tag_name'`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionContents], "gh api /repos/.../releases should require contents: read")
}

// TestInferPermissionsFromShellScripts_GhAPILabels verifies issues: read for gh api labels endpoint.
func TestInferPermissionsFromShellScripts_GhAPILabels(t *testing.T) {
	scripts := []string{`gh api /repos/owner/repo/labels`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionIssues], "gh api /repos/.../labels should require issues: read")
}

// TestActivationJobWriteCommandInPreStepReturnsError verifies that the compiler returns
// an error when an activation pre-step calls a write gh command.
func TestActivationJobWriteCommandInPreStepReturnsError(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-write-cmd-error")
	testFile := filepath.Join(tmpDir, "bad-workflow.md")
	testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  pull-requests: read
engine: copilot
jobs:
  activation:
    pre-steps:
      - name: Create PR comment
        run: |
          gh pr comment "$PR_NUMBER" --body "Starting review..."
---

# Workflow whose activation pre-step illegally calls a write gh command
`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.Error(t, err, "compiler should reject write gh commands in activation pre-steps")
	require.ErrorContains(t, err, "gh pr comment", "error should mention the offending command")
	require.ErrorContains(t, err, "write", "error should explain the write-permission restriction")
}

// TestActivationJobPermissionsWithGhCachePreStep verifies actions: read is added when
// an activation pre-step calls `gh cache list`.
func TestActivationJobPermissionsWithGhCachePreStep(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-perms-gh-cache")
	testFile := filepath.Join(tmpDir, "cache-workflow.md")
	testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  actions: read
engine: copilot
jobs:
  activation:
    pre-steps:
      - name: List caches
        run: |
          gh cache list --json key > /tmp/caches.json
---

# Workflow that lists caches in activation pre-step
`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err, "failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read generated lock file")

	activationJobSection := extractJobSection(string(lockContent), string(constants.ActivationJobName))
	assert.Contains(t, activationJobSection, "actions: read",
		"activation job should include actions: read when pre-step calls gh cache list")
}

// TestInferPermissionsFromShellScripts_GhCodespaceList verifies that `gh codespace list`
// returns the GitHub App-only codespaces: read permission (no GITHUB_TOKEN equivalent).
func TestInferPermissionsFromShellScripts_GhCodespaceList(t *testing.T) {
	scripts := []string{`gh codespace list --json name`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionCodespaces],
		"gh codespace list should require codespaces: read (GitHub App-only)")
}

// TestInferPermissionsFromShellScripts_GhAPIOrgsMembers verifies that `gh api /orgs/.../members`
// returns the GitHub App-only members: read permission.
func TestInferPermissionsFromShellScripts_GhAPIOrgsMembers(t *testing.T) {
	scripts := []string{`gh api /orgs/myorg/members --jq '.[].login'`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionMembers],
		"gh api /orgs/.../members should require members: read (GitHub App-only)")
}

// TestInferPermissionsFromShellScripts_AppAndActionsPermissions verifies that a script
// combining standard and App-only gh commands returns both sets of permissions.
func TestInferPermissionsFromShellScripts_AppAndActionsPermissions(t *testing.T) {
	scripts := []string{
		`gh pr diff "$PR_NUMBER" --name-only
gh codespace list --json name`,
	}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionPullRequests],
		"gh pr diff should require pull-requests: read")
	assert.Equal(t, PermissionRead, perms[PermissionCodespaces],
		"gh codespace list should require codespaces: read (GitHub App-only)")
}

// TestInferPermissionsFromShellScripts_GhRepoWriteHasAppAdminPerm verifies that `gh repo archive`
// (a write command) is still inferred to need administration: read (GitHub App-only) at minimum.
func TestInferPermissionsFromShellScripts_GhRepoWriteHasAppAdminPerm(t *testing.T) {
	scripts := []string{`gh repo archive owner/repo`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionAdministration],
		"gh repo archive (write) should infer administration: read for GitHub App")
}

// TestInferPermissionsFromShellScripts_GhAPIRepoEnvironments verifies environments: read
// (GitHub App-only) for the environments REST API path.
func TestInferPermissionsFromShellScripts_GhAPIRepoEnvironments(t *testing.T) {
	scripts := []string{`gh api /repos/owner/repo/environments --jq '.[].name'`}
	perms := inferPermissionsFromShellScriptsForTest(t, scripts)
	assert.Equal(t, PermissionRead, perms[PermissionEnvironments],
		"gh api /repos/.../environments should require environments: read (GitHub App-only)")
}

// --- mergeInferredIntoPermissionsYAML ---

// TestMergeInferredIntoPermissionsYAML_AddsNewScope verifies that a new inferred scope
// is added when the existing permissions block does not include it.
func TestMergeInferredIntoPermissionsYAML_AddsNewScope(t *testing.T) {
	existing := "permissions:\n  contents: read"
	inferred := map[PermissionScope]PermissionLevel{
		PermissionPullRequests: PermissionRead,
	}
	result := mergeInferredIntoPermissionsYAML(existing, inferred)
	assert.Contains(t, result, "pull-requests: read")
	assert.Contains(t, result, "contents: read")
}

// TestMergeInferredIntoPermissionsYAML_DoesNotOverride verifies that an explicitly
// declared scope is never overridden by inferred values.
func TestMergeInferredIntoPermissionsYAML_DoesNotOverride(t *testing.T) {
	existing := "permissions:\n  pull-requests: read"
	inferred := map[PermissionScope]PermissionLevel{
		PermissionPullRequests: PermissionWrite,
	}
	result := mergeInferredIntoPermissionsYAML(existing, inferred)
	assert.Contains(t, result, "pull-requests: read")
	assert.NotContains(t, result, "pull-requests: write")
}

// TestMergeInferredIntoPermissionsYAML_EmptyInputUnchanged verifies that an empty
// permissions string is returned unchanged (no implicit block is created).
func TestMergeInferredIntoPermissionsYAML_EmptyInputUnchanged(t *testing.T) {
	inferred := map[PermissionScope]PermissionLevel{
		PermissionPullRequests: PermissionRead,
	}
	result := mergeInferredIntoPermissionsYAML("", inferred)
	assert.Empty(t, result)
}

// TestMergeInferredIntoPermissionsYAML_SkipsAppOnlyScopes verifies that GitHub
// App-only scopes are not added to the job-level permissions block.
func TestMergeInferredIntoPermissionsYAML_SkipsAppOnlyScopes(t *testing.T) {
	existing := "permissions:\n  contents: read"
	inferred := map[PermissionScope]PermissionLevel{
		PermissionCodespaces: PermissionRead, // GitHub App-only
	}
	result := mergeInferredIntoPermissionsYAML(existing, inferred)
	assert.NotContains(t, result, "codespaces")
}

// --- Agent job integration tests ---

// TestAgentJobPreStepsWriteCommandErrors verifies that when an agent job pre-step
// contains a write gh command, the compiler returns an error.
func TestAgentJobPreStepsWriteCommandErrors(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-prestep-write-error")

	content := `---
on: push
permissions:
  contents: read
pre-steps:
  - name: Comment on PR
    run: gh pr comment "$PR_NUMBER" --body "processing"
engine: claude
strict: false
---

Test agent pre-steps write command triggers error.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "compiler should error when agent pre-step uses a write gh command")
	require.ErrorContains(t, err, "agent job uses write gh command(s)")
	require.ErrorContains(t, err, "gh pr comment")
	require.ErrorContains(t, err, "safe-outputs")
}

// TestAgentJobPreStepsInferReadPermission verifies that when an agent job pre-step
// contains `gh pr diff` (a read command) and the user has explicit permissions,
// the compiler automatically adds pull-requests: read.
func TestAgentJobPreStepsInferReadPermission(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-prestep-read-perm")

	content := `---
on: push
permissions:
  contents: read
pre-steps:
  - name: Get changed files
    run: gh pr diff "$PR_NUMBER" --name-only > /tmp/changed.txt
engine: claude
strict: false
---

Test agent pre-steps read permission inference.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	lockContent := string(raw)

	agentJob := extractJobSection(lockContent, string(constants.AgentJobName))
	assert.Contains(t, agentJob, "pull-requests: read",
		"agent job should have pull-requests: read inferred from pre-step gh pr diff")
}

// TestAgentJobPreStepsInferWithDefaultPermissions verifies that when the user has not
// declared an explicit permissions block, the default permissions (contents: read) are
// still augmented with inferred scopes required by pre-step gh commands.
func TestAgentJobPreStepsInferWithDefaultPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-prestep-default-perms")

	content := `---
on: push
pre-steps:
  - name: Get changed files
    run: gh pr diff "$PR_NUMBER" --name-only > /tmp/changed.txt
engine: claude
strict: false
---

Test: inference applies even without explicit permissions declaration.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	lockContent := string(raw)

	agentJob := extractJobSection(lockContent, string(constants.AgentJobName))
	assert.Contains(t, agentJob, "pull-requests: read",
		"agent job should have pull-requests: read inferred from pre-step gh pr diff even without explicit permissions block")
}

// TestAgentJobPreStepsNoInferForEmptyPermissions verifies that when the user explicitly
// sets permissions: {} (empty/no permissions), pre-step inference is skipped because
// the user intentionally opted out of all permissions.
func TestAgentJobPreStepsNoInferForEmptyPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-prestep-empty-perms")

	content := `---
on: push
permissions: {}
pre-steps:
  - name: Get changed files
    run: gh pr diff "$PR_NUMBER" --name-only > /tmp/changed.txt
engine: claude
strict: false
---

Test: no inference when user explicitly opts out of permissions.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	lockContent := string(raw)

	agentJob := extractJobSection(lockContent, string(constants.AgentJobName))
	assert.NotContains(t, agentJob, "pull-requests:",
		"agent job should NOT have pull-requests when user explicitly set permissions: {}")
}

// --- extractRunScriptsFromSectionYAML ---

// TestExtractRunScriptsFromSectionYAML_Steps verifies extraction from a `steps:` section.
func TestExtractRunScriptsFromSectionYAML_Steps(t *testing.T) {
	yamlStr := `steps:
  - name: List issues
    run: gh issue list --json number
  - uses: some-org/action@0000000000000000000000000000000000000000
`
	scripts := extractRunScriptsFromSectionYAML(yamlStr, "steps")
	assert.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "gh issue list")
}

// TestExtractRunScriptsFromSectionYAML_PostSteps verifies extraction from a `post-steps:` section.
func TestExtractRunScriptsFromSectionYAML_PostSteps(t *testing.T) {
	yamlStr := `post-steps:
  - name: Clean up
    run: gh run list --json databaseId
`
	scripts := extractRunScriptsFromSectionYAML(yamlStr, "post-steps")
	assert.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "gh run list")
}

// TestExtractRunScriptsFromSectionYAML_PreAgentSteps verifies extraction from a `pre-agent-steps:` section.
func TestExtractRunScriptsFromSectionYAML_PreAgentSteps(t *testing.T) {
	yamlStr := `pre-agent-steps:
  - name: Fetch release info
    run: gh release view --json tagName
`
	scripts := extractRunScriptsFromSectionYAML(yamlStr, "pre-agent-steps")
	assert.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "gh release view")
}

// TestExtractRunScriptsFromSectionYAML_WrongKey verifies nil is returned when the YAML key
// does not match the requested section name.
func TestExtractRunScriptsFromSectionYAML_WrongKey(t *testing.T) {
	yamlStr := `pre-steps:
  - name: A step
    run: gh pr diff
`
	scripts := extractRunScriptsFromSectionYAML(yamlStr, "post-steps")
	assert.Nil(t, scripts)
}

// --- extractRunScriptsFromJobSection ---

// TestExtractRunScriptsFromJobSection_Steps verifies extraction from jobs.<name>.steps.
func TestExtractRunScriptsFromJobSection_Steps(t *testing.T) {
	jobs := map[string]any{
		"activation": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "List workflows",
					"run":  `gh workflow list --json name`,
				},
			},
		},
	}
	scripts := extractRunScriptsFromJobSection(jobs, "activation", "steps")
	require.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "gh workflow list")
}

// TestExtractRunScriptsFromJobSection_PostSteps verifies extraction from jobs.<name>.post-steps.
func TestExtractRunScriptsFromJobSection_PostSteps(t *testing.T) {
	jobs := map[string]any{
		"agent": map[string]any{
			"post-steps": []any{
				map[string]any{
					"name": "Summary",
					"run":  `gh pr view "$PR_NUMBER" --json state`,
				},
			},
		},
	}
	scripts := extractRunScriptsFromJobSection(jobs, "agent", "post-steps")
	require.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "gh pr view")
}

// TestExtractRunScriptsFromJobSection_MissingSectionReturnsNil verifies nil when section absent.
func TestExtractRunScriptsFromJobSection_MissingSectionReturnsNil(t *testing.T) {
	jobs := map[string]any{
		"activation": map[string]any{
			"pre-steps": []any{},
		},
	}
	assert.Nil(t, extractRunScriptsFromJobSection(jobs, "activation", "post-steps"))
}

// --- Agent job integration tests for steps / post-steps / pre-agent-steps ---

// TestAgentJobStepsInferReadPermission verifies that `gh issue list` in a top-level `steps:`
// block causes the compiler to add issues: read to the agent job.
func TestAgentJobStepsInferReadPermission(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-steps-read-perm")

	content := `---
on: push
permissions:
  contents: read
steps:
  - name: List issues
    run: gh issue list --json number > /tmp/issues.json
engine: claude
strict: false
---

Test agent steps read permission inference.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	agentJob := extractJobSection(string(raw), string(constants.AgentJobName))
	assert.Contains(t, agentJob, "issues: read",
		"agent job should have issues: read inferred from steps gh issue list")
}

// TestAgentJobPostStepsInferReadPermission verifies that `gh run list` in a top-level `post-steps:`
// block causes the compiler to add actions: read to the agent job.
func TestAgentJobPostStepsInferReadPermission(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-poststeps-read-perm")

	content := `---
on: push
permissions:
  contents: read
post-steps:
  - name: Show runs
    run: gh run list --json databaseId > /tmp/runs.json
engine: claude
strict: false
---

Test agent post-steps read permission inference.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	agentJob := extractJobSection(string(raw), string(constants.AgentJobName))
	assert.Contains(t, agentJob, "actions: read",
		"agent job should have actions: read inferred from post-steps gh run list")
}

// TestAgentJobPreAgentStepsInferReadPermission verifies that `gh pr diff` in a top-level
// `pre-agent-steps:` block causes the compiler to add pull-requests: read to the agent job.
func TestAgentJobPreAgentStepsInferReadPermission(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-preagent-read-perm")

	content := `---
on: push
permissions:
  contents: read
pre-agent-steps:
  - name: Get diff
    run: gh pr diff "$PR_NUMBER" --name-only > /tmp/diff.txt
engine: claude
strict: false
---

Test agent pre-agent-steps read permission inference.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	raw, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	agentJob := extractJobSection(string(raw), string(constants.AgentJobName))
	assert.Contains(t, agentJob, "pull-requests: read",
		"agent job should have pull-requests: read inferred from pre-agent-steps gh pr diff")
}

// TestAgentJobWriteCommandInStepsErrors verifies that a write gh command in a top-level
// `steps:` block triggers a compile error.
func TestAgentJobWriteCommandInStepsErrors(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-steps-write-error")

	content := `---
on: push
permissions:
  contents: read
steps:
  - name: Create issue
    run: gh issue create --title "bug" --body "found a bug"
engine: claude
strict: false
---

Test agent steps write command triggers error.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "compiler should error when agent steps use a write gh command")
	require.ErrorContains(t, err, "agent job uses write gh command(s)")
	require.ErrorContains(t, err, "gh issue create")
}

// TestAgentJobWriteCommandInPostStepsErrors verifies that a write gh command in a top-level
// `post-steps:` block triggers a compile error.
func TestAgentJobWriteCommandInPostStepsErrors(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-poststeps-write-error")

	content := `---
on: push
permissions:
  contents: read
post-steps:
  - name: Close PR
    run: gh pr close "$PR_NUMBER"
engine: claude
strict: false
---

Test agent post-steps write command triggers error.
`
	testFile := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "compiler should error when agent post-steps use a write gh command")
	require.ErrorContains(t, err, "agent job uses write gh command(s)")
	require.ErrorContains(t, err, "gh pr close")
}

// TestActivationJobStepsScanned verifies that jobs.activation.steps is scanned for
// gh CLI calls because activation steps are injected into the built-in activation job.
func TestActivationJobStepsScanned(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-steps-scanned")
	testFile := filepath.Join(tmpDir, "workflow.md")
	testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
engine: copilot
jobs:
  activation:
    steps:
      - name: Create label
        run: |
          gh label create "reviewed" --color "#0075ca"
---

# Workflow whose activation steps are scanned for permissions
`
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err)
	require.ErrorContains(t, err, "gh label create")
}
