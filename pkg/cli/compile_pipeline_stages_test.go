//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompilePipelineStageRecordsFiles(t *testing.T) {
	lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, []byte("name: test\n"), 0o644))
	data := &workflow.WorkflowData{
		WorkflowID: "test",
		MCPScripts: &workflow.MCPScriptsConfig{Tools: map[string]*workflow.MCPScriptToolConfig{
			"inspect": {Run: "echo test"},
		}},
	}
	results := []ValidationResult{{Workflow: "previous"}}
	stats := &CompilationStats{}
	stage := compilePipelineStage{
		config: CompileConfig{
			Actionlint: true, Zizmor: true, Poutine: true, Syft: true,
			Grype: true, Grant: true, Yamllint: true, Shellcheck: true,
		},
		stats: stats, results: &results,
	}
	stage.recordFile("test.md", compileWorkflowFileResult{success: true, workflowData: data, lockFile: lockFile})

	assert.Equal(t, 1, stats.Succeeded)
	assert.Equal(t, 1, stage.successes)
	assert.Equal(t, []*workflow.WorkflowData{data}, stage.workflows)
	assert.Equal(t, []int{1}, stage.resultIndexes)
	assert.Equal(t, []string{lockFile}, stage.compiledLockFiles)
	for _, files := range [][]string{
		stage.batch.lockFilesForActionlint, stage.batch.lockFilesForZizmor,
		stage.batch.lockFilesForDirTools, stage.batch.lockFilesForSyft,
		stage.batch.lockFilesForGrype, stage.batch.lockFilesForGrant,
		stage.batch.lockFilesForYamllint, stage.batch.lockFilesForShellcheck,
	} {
		assert.Equal(t, []string{lockFile}, files)
	}
	assert.Equal(t, []workflow.ShellScriptResource{{Name: "mcp-scripts.inspect", Script: "echo test", Shell: "bash", Source: "test"}}, stage.batch.shellcheckResources)

	stage.recordFile("broken.md", compileWorkflowFileResult{})
	assert.Equal(t, 1, stage.failures)
	assert.Equal(t, 1, stats.Errors)
	require.Len(t, stats.FailureDetails, 1)
	assert.Equal(t, []string{fallbackCompilationErrorMessage}, stats.FailureDetails[0].ErrorMessages)
}

func TestCompilePipelineStageSkipsUnemittedLocks(t *testing.T) {
	lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, nil, 0o644))
	for _, tc := range []struct {
		name   string
		noEmit bool
		lock   string
	}{
		{name: "no emit", noEmit: true, lock: lockFile},
		{name: "empty path"},
		{name: "missing file", lock: lockFile + ".missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results := []ValidationResult{}
			stage := compilePipelineStage{
				config: CompileConfig{NoEmit: tc.noEmit, Actionlint: true},
				stats:  &CompilationStats{}, results: &results,
			}
			stage.recordFile("test.md", compileWorkflowFileResult{success: true, lockFile: tc.lock})
			assert.Empty(t, stage.compiledLockFiles)
			assert.Empty(t, stage.batch.lockFilesForActionlint)
			assert.Equal(t, 1, stage.successes)
		})
	}
}

func TestCompilePipelineStageCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := []ValidationResult{}
	stats := &CompilationStats{}
	stage := compilePipelineStage{stats: stats, results: &results}
	require.ErrorIs(t, stage.compile(ctx, []string{"test.md"}, true), context.Canceled)
	assert.Zero(t, stats.Total)
	assert.Empty(t, results)
}

func TestCompilationResultErrorPrecedence(t *testing.T) {
	grantErr := errors.New("grant")
	batchErr := errors.New("batch")
	developmentErr := errors.New("development")
	require.ErrorIs(t, compilationResultError(1, grantErr, batchErr, developmentErr), grantErr)
	require.ErrorIs(t, compilationResultError(1, nil, batchErr, developmentErr), batchErr)
	require.EqualError(t, compilationResultError(1, nil, nil, developmentErr), "compilation failed")
	require.ErrorIs(t, compilationResultError(0, nil, nil, developmentErr), developmentErr)
	require.NoError(t, compilationResultError(0, nil, nil, nil))
}
