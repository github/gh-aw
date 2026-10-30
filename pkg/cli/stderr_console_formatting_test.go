//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderOrgActionSummaryUsesConsoleFormatListItemStderr(t *testing.T) {
	preview := orgRepoPreview{
		Repo:           "octo/repo",
		TotalWorkflows: 2,
		Workflows: []orgWorkflowPreview{
			{Name: "repo-assist", CurrentRef: "v1.0.0", LatestRef: "v1.1.0"},
		},
	}

	_, stderr := captureOutput(t, func() error {
		renderOrgActionSummary(preview, "update")
		return nil
	})

	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	require.Len(t, lines, 5)
	assert.Equal(t, console.FormatInfoMessageStderr("Ready to update"), lines[0])
	assert.Equal(t, console.FormatListItemStderr("Repository: octo/repo"), lines[1])
	assert.Equal(t, console.FormatListItemStderr("Workflows: 2"), lines[2])
	assert.Equal(t, console.FormatListItemStderr("Pending workflow updates:"), lines[3])
	assert.Equal(t, console.FormatListItemStderr("repo-assist: v1.0.0 -> v1.1.0"), lines[4])
}

func TestRenderOrgActionSummaryShowsUncompiledOnlyVersion(t *testing.T) {
	_, stderr := captureOutput(t, func() error {
		renderOrgActionSummary(orgRepoPreview{
			Repo: "octo/repo", TotalWorkflows: 1, MissingWorkflowLocks: 1,
		}, "upgrade")
		return nil
	})
	assert.Contains(t, stderr, "Compiler versions: uncompiled (1 workflow(s)) ->")
}

func TestFindWorkflowsWithMCPServerUsesConsoleFormatInfoMessageStderr(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "workflow1.md"), []byte(`---
tools:
  github:
    allowed: ["create_issue"]
---
# Workflow 1
`), 0o644))

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalDir)
	}()
	require.NoError(t, os.Chdir(tmpDir))

	_, stderr := captureOutput(t, func() error {
		return findWorkflowsWithMCPServer(workflowsDir, "github", false)
	})

	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, console.FormatInfoMessageStderr("Found MCP server 'github' in 1 workflow(s): workflow1"), lines[0])
	assert.Empty(t, lines[1])
	assert.Equal(t, console.FormatInfoMessageStderr("Run 'gh aw mcp list-tools <workflow-name> --server github' to list tools for a specific workflow"), lines[2])
}
