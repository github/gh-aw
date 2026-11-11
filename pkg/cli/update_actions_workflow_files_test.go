//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindWorkflowWithMergeConflictMarkers_DefaultDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	path := filepath.Join(".github", "workflows", "conflict.md")
	require.NoError(t, os.WriteFile(path, []byte("<<<<<<< local\n=======\n>>>>>>> upstream\n"), 0o600))

	found, err := findWorkflowWithMergeConflictMarkers("")
	require.NoError(t, err)
	require.Equal(t, path, found)
}

func TestFindWorkflowWithMergeConflictMarkers_ReadFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken.md")))

	_, err := findWorkflowWithMergeConflictMarkers(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUpdateActionsInWorkflowFiles_UpdatesUsesReferences(t *testing.T) {
	t.Parallel()
	// Stub latest release lookup so no network calls are made.
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		if repo == "ruby/setup-ruby" {
			return "v1.310.0", "newrubysha1234567890123456789012345678901", nil
		}
		return currentVersion, "", nil
	}

	// Create a temp workflows directory with a .md file that has an outdated uses: reference.
	workflowsDir := t.TempDir()
	mdContent := "steps:\n  - uses: ruby/setup-ruby@v1.309.0\n"
	mdPath := filepath.Join(workflowsDir, "my-workflow.md")
	if err := os.WriteFile(mdPath, []byte(mdContent), 0o644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	if err := updateActionsInWorkflowFiles(context.Background(), deps, updateActionsOptions{workflowsDir: workflowsDir, noCompile: true}); err != nil {
		t.Fatalf("UpdateActionsInWorkflowFiles() error = %v", err)
	}

	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read updated workflow file: %v", err)
	}

	if want := "steps:\n  - uses: ruby/setup-ruby@v1.310.0\n"; string(got) != want {
		t.Errorf("workflow file not updated:\ngot:  %s\nwant: %s", string(got), want)
	}
}
func TestUpdateActionsInWorkflowFiles_NeverDowngrades(t *testing.T) {
	t.Parallel()
	// Mirrors TestUpdateActions_NeverDowngrades: the release resolver returns a lower
	// version than what is already pinned in the .md file; the source file must not
	// be rewritten to the older tag.
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		if repo == "actions-ecosystem/action-add-labels" {
			return "v1.1.0", "oldsha1234567890123456789012345678901234a", nil
		}
		return currentVersion, "", nil
	}

	workflowsDir := t.TempDir()
	originalContent := "steps:\n  - uses: actions-ecosystem/action-add-labels@v1.1.3\n"
	mdPath := filepath.Join(workflowsDir, "my-workflow.md")
	if err := os.WriteFile(mdPath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	if err := updateActionsInWorkflowFiles(context.Background(), deps, updateActionsOptions{workflowsDir: workflowsDir, noCompile: true}); err != nil {
		t.Fatalf("UpdateActionsInWorkflowFiles() error = %v", err)
	}

	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read workflow file: %v", err)
	}

	if string(got) != originalContent {
		t.Errorf("workflow file was incorrectly modified (downgrade must not occur):\ngot:  %s\nwant: %s", string(got), originalContent)
	}
}

func TestUpdateActionsInWorkflowFiles_SkipsMergeConflicts(t *testing.T) {
	t.Parallel()
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		if repo == "ruby/setup-ruby" {
			return "v1.310.0", "newrubysha1234567890123456789012345678901", nil
		}
		return currentVersion, "", nil
	}

	workflowsDir := t.TempDir()
	originalContent := "steps:\n  - uses: ruby/setup-ruby@v1.309.0\n" +
		strings.Repeat("<", 7) + " current (local changes)\n" +
		"  - name: Local change\n" +
		strings.Repeat("=", 7) + "\n" +
		"  - name: Upstream change\n" +
		strings.Repeat(">", 7) + " new (upstream)\n"
	mdPath := filepath.Join(workflowsDir, "conflicted-workflow.md")
	if err := os.WriteFile(mdPath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	if err := updateActionsInWorkflowFiles(context.Background(), deps, updateActionsOptions{workflowsDir: workflowsDir, noCompile: true}); err != nil {
		t.Fatalf("UpdateActionsInWorkflowFiles() error = %v", err)
	}

	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read workflow file: %v", err)
	}
	if string(got) != originalContent {
		t.Errorf("conflicted workflow file was modified:\ngot:  %s\nwant: %s", string(got), originalContent)
	}
}
