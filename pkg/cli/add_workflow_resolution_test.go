//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateResolveWorkflowsInput(t *testing.T) {
	require.ErrorContains(t, validateResolveWorkflowsInput(nil), "at least one workflow name")
	require.ErrorContains(t, validateResolveWorkflowsInput([]string{"workflow", ""}), "cannot be empty")
	require.NoError(t, validateResolveWorkflowsInput([]string{"workflow"}))
}

func TestExpandWorkflowSpecsIfNeededWithoutWildcard(t *testing.T) {
	specs := []*WorkflowSpec{{WorkflowPath: "workflows/review.md"}}
	expanded, hasWildcard, err := expandWorkflowSpecsIfNeeded(specs, false)
	require.NoError(t, err)
	require.False(t, hasWildcard)
	require.Equal(t, specs, expanded)
}

func TestResolveRepositoryPackageFallbackRejectsInvalidSpec(t *testing.T) {
	_, _, _, err := resolveRepositoryPackageFallback(t.Context(), "not a repository")
	require.ErrorContains(t, err, "invalid specification")
}

func TestResolveLocalPackageWorkflowSpecWithoutManifest(t *testing.T) {
	_, _, _, err := resolveLocalPackageWorkflowSpec(filepath.Join(t.TempDir(), "workflow.md"))
	require.ErrorIs(t, err, errNotHandled)
}

func TestExpandWorkflowSpecsIfNeededWithLocalWildcard(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "review.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte("---\non: push\n---\n"), 0600))

	specs, hasWildcard, err := expandWorkflowSpecsIfNeeded([]*WorkflowSpec{{
		WorkflowPath: filepath.Join(dir, "*.md"),
		IsWildcard:   true,
	}}, false)
	require.NoError(t, err)
	require.True(t, hasWildcard)
	require.Len(t, specs, 1)
	require.Equal(t, workflowPath, specs[0].WorkflowPath)
}

func TestParseSingleWorkflowSpecForResolutionRejectsInvalidSpec(t *testing.T) {
	_, _, _, err := parseSingleWorkflowSpecForResolution(context.Background(), "not a repository")
	require.ErrorContains(t, err, "invalid specification")
}
