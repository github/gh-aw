//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixPullRequestTargetAutomaticMigrations(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "dry run", true: "write"}[write], func(t *testing.T) {
			t.Parallel()
			content := "---\non:\n  pull_request_target:\nroles: [admin, read]\n---\n\n# Review metadata\n"
			file := filepath.Join(t.TempDir(), "workflow.md")
			require.NoError(t, os.WriteFile(file, []byte(content), 0600))
			fixed, fixes, err := processWorkflowFileWithInfo(file, GetAllCodemods(), write, false)
			require.NoError(t, err)
			assert.True(t, fixed)
			assert.Contains(t, fixes, getRolesToOnRolesCodemod().Name)
			assert.Contains(t, fixes, getPullRequestTargetRolesCodemod().Name)
			assert.Contains(t, fixes, getPullRequestTargetCheckoutFalseCodemod().Name)
			written, err := os.ReadFile(file)
			require.NoError(t, err)
			if !write {
				assert.Equal(t, content, string(written))
				return
			}
			document, err := aw.Parse(string(written))
			require.NoError(t, err)
			assert.Equal(t, false, document.Frontmatter["checkout"])
			on := document.Frontmatter["on"].(map[string]any)
			assert.Equal(t, []any{"admin"}, on["roles"])
			assert.NotContains(t, string(written), "acknowledge-risk")
			assert.NotContains(t, string(written), "strict: false")
			fixed, fixes, err = processWorkflowFileWithInfo(file, GetAllCodemods(), true, false)
			require.NoError(t, err)
			assert.False(t, fixed)
			assert.Empty(t, fixes)
		})
	}
}

func TestFixPullRequestTargetGuidedFailureIsAtomic(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "dry run", true: "write"}[write], func(t *testing.T) {
			t.Parallel()
			content := "---\non: {pull_request_target: null, roles: read}\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}\n---\n\n# Prompt\n"
			file := filepath.Join(t.TempDir(), "workflow.md")
			require.NoError(t, os.WriteFile(file, []byte(content), 0600))
			fixed, fixes, err := processWorkflowFileWithInfo(file, GetAllCodemods(), write, false)
			var guided *GuidedError
			require.ErrorAs(t, err, &guided)
			require.ErrorContains(t, err, "Manual fix required")
			require.ErrorContains(t, err, "checkout: false")
			assert.False(t, fixed)
			assert.Empty(t, fixes)
			written, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, content, string(written))
		})
	}
}

func TestFixPullRequestTargetDisabledRoleMigration(t *testing.T) {
	t.Parallel()
	mods, err := GetCodemods([]string{"pull-request-target-restrict-roles"})
	require.NoError(t, err)
	content := "---\non: {pull_request_target: null, roles: read}\ncheckout: false\n---\n"
	file := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(file, []byte(content), 0600))
	_, _, err = processWorkflowFileWithInfo(file, mods, true, false)
	var guided *GuidedError
	require.ErrorAs(t, err, &guided)
	require.ErrorContains(t, err, "on.roles: [admin, maintainer, write]")
	written, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, content, string(written))

	mods, err = GetCodemods([]string{"pull-request-target-restrict-roles", "pull-request-target-guided-error"})
	require.NoError(t, err)
	fixed, _, err := processWorkflowFileWithInfo(file, mods, true, false)
	require.NoError(t, err)
	assert.False(t, fixed)
}

func TestRunFixPullRequestTargetGuidedExitCode(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "dry run", true: "write"}[write], func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			content := "---\non: pull_request_target\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}\n---\n"
			file := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(file, []byte(content), 0600))
			err := RunFix(FixConfig{Write: write, WorkflowDir: dir, SkipInstructionFileUpdates: true})
			var exitErr *ExitCodeError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 2, exitErr.Code)
			written, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, content, string(written))
		})
	}
}
