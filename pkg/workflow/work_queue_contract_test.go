package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestDeepReportPreservesAdmittedDailyReportContract(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "deep-report.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	contract, err := workQueueLogicalContractFromContentWithCache(path, string(content), parser.NewImportCache(filepath.Dir(path)))
	require.NoError(t, err)
	// Pending daily reports retain this immutable admission contract across deployments.
	require.Equal(t, "5ba80ab1b9423f26578a2cb7b0832d7eaa2d9e92b19d7564f0224e5c244187b4", contract)

	scoped := strings.Replace(string(content), "    deduplicate-by-labels: [automation, improvement, quick-win, cookie, code-quality, task-mining]\n", "", 1)
	require.NotEqual(t, string(content), scoped)
	incompatible, err := workQueueLogicalContractFromContentWithCache(path, scoped, parser.NewImportCache(filepath.Dir(path)))
	require.NoError(t, err)
	require.Equal(t, "099f49b1f233e2c5ebfe89e8fb87697affeb622b76156d375591191c6b28872c", incompatible)
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
	contract, err := workQueueLogicalContractFromContentWithCache(path, source, parser.NewImportCache(filepath.Dir(path)))
	require.NoError(t, err)
	replacement, err := workQueueLogicalContractFromContentWithCache(path, strings.ReplaceAll(source, "echo original", "echo replacement"), parser.NewImportCache(filepath.Dir(path)))
	require.NoError(t, err)
	require.Equal(t, contract, replacement)
	expanded, err := workQueueLogicalContractFromContentWithCache(path, strings.ReplaceAll(source, "type: string", "type: number"), parser.NewImportCache(filepath.Dir(path)))
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
