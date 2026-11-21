//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestCollectBatchToolInputs(t *testing.T) {
	config := CompileConfig{
		Actionlint:  true,
		Zizmor:      true,
		Poutine:     true,
		RunnerGuard: true,
		Syft:        true,
		Grype:       true,
		Grant:       true,
		Yamllint:    true,
		Shellcheck:  true,
	}
	fileResult := compileWorkflowFileResult{
		lockFile:     "workflow.lock.yml",
		workflowData: &workflow.WorkflowData{},
	}
	var opts batchToolsOptions

	collectBatchToolInputs(config, fileResult, &opts)

	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForActionlint)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForZizmor)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForDirTools)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForSyft)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForGrype)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForGrant)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForYamllint)
	require.Equal(t, []string{"workflow.lock.yml"}, opts.lockFilesForShellcheck)
}
