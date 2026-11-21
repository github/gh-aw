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

func TestWriteMCPCompileProgressPublicationFailure(t *testing.T) {
	progressPath := t.TempDir()
	writeMCPCompileProgress(progressPath, []ValidationResult{{
		Workflow: "workflow.md",
		Valid:    true,
		Warnings: []ValidationIssue{{Type: "compile_warning", Message: "warning"}},
	}})
}

func TestRunPostProcessingDependabot(t *testing.T) {
	setupCompilePipelineRepo(t)

	err := runPostProcessing(context.Background(), workflow.NewCompiler(), nil, nil, CompileConfig{
		Dependabot: true,
	}, 0)
	require.NoError(t, err)
}

func TestRunPostProcessingForDirectoryCancellation(t *testing.T) {
	repoDir, workflowsDir := setupCompilePipelineRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, runPostProcessingForDirectory(
		ctx,
		workflow.NewCompiler(),
		nil,
		CompileConfig{},
		workflowsDir,
		repoDir,
		0,
		0,
	))
}

func TestRunPostProcessingForDirectoryGenerationFailure(t *testing.T) {
	repoDir, _ := setupCompilePipelineRepo(t)
	blockedDir := filepath.Join(repoDir, "blocked")
	require.NoError(t, os.WriteFile(blockedDir, nil, 0o600))
	workflowData := &workflow.WorkflowData{
		AI:           "claude",
		EngineConfig: &workflow.EngineConfig{ID: "claude", Version: "1.2.3"},
	}

	err := runPostProcessingForDirectory(
		context.Background(),
		workflow.NewCompiler(),
		[]*workflow.WorkflowData{workflowData},
		CompileConfig{Dependabot: true, Strict: true},
		blockedDir,
		repoDir,
		0,
		0,
	)
	require.ErrorContains(t, err, "failed to generate maintenance workflow")
}

func TestRunPostProcessingRepoConfigFailure(t *testing.T) {
	repoDir, workflowsDir := setupCompilePipelineRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte("{"), 0o600))
	lockFile := filepath.Join(workflowsDir, "workflow.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, []byte("name: Workflow\n"), 0o600))

	require.NoError(t, runPostProcessing(
		context.Background(),
		workflow.NewCompiler(),
		nil,
		[]string{lockFile},
		CompileConfig{},
		0,
	))
	err := runPostProcessingForDirectory(
		context.Background(),
		workflow.NewCompiler(),
		nil,
		CompileConfig{Strict: true},
		workflowsDir,
		repoDir,
		0,
		0,
	)
	require.ErrorContains(t, err, "failed to load repo config")
}

func TestOutputResultsTextStatsAndWarnings(t *testing.T) {
	markdownFile := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(markdownFile, []byte("---\nname: Workflow\n---\n# Workflow\n"), 0o600))
	stats := &CompilationStats{}
	var results []ValidationResult

	err := outputResults(stats, &results, CompileConfig{
		Actionlint:              true,
		Stats:                   true,
		MarkdownFiles:           []string{markdownFile},
		modelValidationWarnings: []string{"model inventory is unavailable"},
	})

	require.NoError(t, err)
	require.Equal(t, 1, stats.Warnings)
	require.Len(t, results, 1)
}

func setupCompilePipelineRepo(t *testing.T) (string, string) {
	t.Helper()
	repoDir := t.TempDir()
	require.NoError(t, initTestGitRepo(repoDir))
	workflowsDir := filepath.Join(repoDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	return repoDir, workflowsDir
}
