//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestCompilePipelineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	compiler := workflow.NewCompiler()
	stats := &CompilationStats{}
	var results []ValidationResult

	_, err := compileSpecificFiles(ctx, compiler, CompileConfig{MarkdownFiles: []string{"workflow"}}, stats, &results)
	require.ErrorIs(t, err, context.Canceled)

	repoDir := t.TempDir()
	require.NoError(t, initTestGitRepo(repoDir))
	workflowsDir := filepath.Join(repoDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "workflow.md"), []byte("---\nname: Workflow\n---\n"), 0o600))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	_, err = compileAllFilesInDirectory(ctx, compiler, CompileConfig{}, ".github/workflows", stats, &results)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCompileSpecificFilesResolutionFailure(t *testing.T) {
	var results []ValidationResult
	_, err := compileSpecificFiles(
		context.Background(),
		workflow.NewCompiler(),
		CompileConfig{MarkdownFiles: []string{"missing-workflow"}, NoEmit: true},
		&CompilationStats{},
		&results,
	)

	require.ErrorContains(t, err, "compilation failed")
	require.Len(t, results, 1)
	require.False(t, results[0].Valid)
	require.Equal(t, "resolution_error", results[0].Errors[0].Type)
}

func TestRunPostProcessingPaths(t *testing.T) {
	repoDir := t.TempDir()
	require.NoError(t, initTestGitRepo(repoDir))
	workflowsDir := filepath.Join(repoDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	lockFile := filepath.Join(workflowsDir, "workflow.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, []byte("name: Workflow\n"), 0o600))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	compiler := workflow.NewCompiler()
	require.NoError(t, runPostProcessing(context.Background(), compiler, nil, []string{lockFile}, CompileConfig{Dependabot: true}, 0))
	require.NoError(t, runPostProcessingForDirectory(
		context.Background(),
		compiler,
		nil,
		CompileConfig{},
		workflowsDir,
		repoDir,
		0,
		0,
	))
}

func TestOutputResultsIncludesModelWarnings(t *testing.T) {
	stats := &CompilationStats{}
	var results []ValidationResult
	config := CompileConfig{
		JSONOutput:              true,
		modelValidationWarnings: []string{"model inventory is unavailable"},
	}

	require.NoError(t, outputResults(stats, &results, config))

	require.Equal(t, 1, stats.Warnings)
	require.Len(t, results, 1)
	require.Equal(t, "models", results[0].Workflow)
	require.Len(t, results[0].Warnings, 1)
}
