package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkQueueLogicalContractIgnoresImplementationNotAuthority(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "worker.md")
	source := "---\non:\n  workflow_dispatch:\npermissions:\n  contents: read\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  create-issue:\n    max: 1\n    github-token: ${{ secrets.ISSUE_TOKEN }}\nengine: copilot\n---\nRun the original implementation.\n"
	write := func(source string) string {
		t.Helper()
		require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
		contract, err := workQueueLogicalContract(file)
		require.NoError(t, err)
		require.Len(t, contract, 64)
		return contract
	}
	before := write(source)
	require.Equal(t, before, write(strings.ReplaceAll(strings.ReplaceAll(source, "engine: copilot", "engine: claude"), "original implementation", "compatible replacement")))
	require.Equal(t, before, write(strings.ReplaceAll(source, "secrets.ISSUE_TOKEN", "secrets.ROTATED_TOKEN")))
	require.NotEqual(t, before, write(strings.ReplaceAll(source, "max: 1", "max: 2")))
	require.NotEqual(t, before, write(strings.ReplaceAll(source, "contents: read", "contents: write")))
	require.NotEqual(t, before, write(strings.ReplaceAll(source, "workflow_dispatch:", "workflow_dispatch:\n    inputs:\n      mode:\n        type: string\n        default: analysis")))
}

func TestWorkQueueLogicalContractCoversImportedAuthority(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "worker.md")
	shared := filepath.Join(dir, "shared.md")
	require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\nimports:\n  - shared.md\n---\nRun assigned Work.\n"), 0o600))
	write := func(source string) string {
		t.Helper()
		require.NoError(t, os.WriteFile(shared, []byte(source), 0o600))
		contract, err := workQueueLogicalContract(file)
		require.NoError(t, err)
		return contract
	}
	before := write("---\nsafe-outputs:\n  create-issue:\n    max: 1\n---\nOriginal helper instructions.\n")
	require.Equal(t, before, write("---\nsafe-outputs:\n  create-issue:\n    max: 1\n---\nNew compatible helper instructions.\n"))
	require.NotEqual(t, before, write("---\nsafe-outputs:\n  create-issue:\n    max: 2\n---\nNew helper instructions.\n"))
}

func TestWorkQueueLogicalContractPreservesInputTypes(t *testing.T) {
	stringValue := map[string]any{"inputs": map[string]any{"default": "3"}}
	numberValue := map[string]any{"inputs": map[string]any{"default": 3}}
	require.NotEqual(t, normalizeWorkQueueContract(stringValue), normalizeWorkQueueContract(numberValue))
	require.Equal(t, map[string]any{"github-token": map[string]any{"type": "string"}}, normalizeWorkQueueContract(map[string]any{"github-token": map[string]any{"type": "string"}}))
}

func TestWorkQueueLogicalContractScriptImplementationCanEvolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.md")
	source := "---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\nmcp-scripts:\n  analysis:\n    description: Analyze assigned data\n    run: echo original\n    inputs:\n      mode:\n        type: string\n---\nProcess Claims.\n"
	contract, err := workQueueLogicalContractFromContent(path, source)
	require.NoError(t, err)
	replacement, err := workQueueLogicalContractFromContent(path, strings.ReplaceAll(source, "echo original", "echo replacement"))
	require.NoError(t, err)
	require.Equal(t, contract, replacement)
	expanded, err := workQueueLogicalContractFromContent(path, strings.ReplaceAll(source, "type: string", "type: number"))
	require.NoError(t, err)
	require.NotEqual(t, contract, expanded)
}

func TestWorkQueueLogicalContractFromVirtualWorker(t *testing.T) {
	c := NewCompiler()
	c.gitRoot = t.TempDir()
	data := &WorkflowData{
		Tools:       map[string]any{"work-queue": map[string]any{"worker": true}},
		RawMarkdown: "---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\n---\nProcess original Claims.\n",
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.NoError(t, c.configureAWWorkQueue(data, filepath.Join(c.gitRoot, "virtual-worker.md")))
	contract := data.WorkQueuePolicy.WorkerContract
	require.Len(t, contract, 64)
	require.Equal(t, contract, data.WorkQueuePolicy.Policy.Pools["default"].Profiles["virtual-worker"].LogicalContract)
	require.Contains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_CONTRACT: \""+contract+"\"")
}
