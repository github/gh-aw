//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueESLintRefinerPersistentSnapshotCompilation(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/eslint-refiner.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotContains(t, frontmatter["tools"], "repo-memory")
	require.NotContains(t, frontmatter["tools"], "cli-proxy")
	outputs := frontmatter["safe-outputs"].(map[string]any)
	require.NotContains(t, outputs, "scripts")
	require.NotContains(t, outputs, "claim-adapters")
	memory := frontmatter["tools"].(map[string]any)["work-queue"].(map[string]any)["memory"].(map[string]any)
	require.Equal(t, "persist_eslint_memory", memory["name"])
	require.Equal(t, "github/gh-aw", memory["target-repo"])
	require.Equal(t, "46b68a61c366a01d86dc319b8e689d09a48bad04", memory["base-revision"])
	directory := testutil.TempDir(t, "work-queue-eslint-memory-")
	shared, err := filepath.Abs("../../.github/workflows/shared")
	require.NoError(t, err)
	require.NoError(t, os.CopyFS(filepath.Join(directory, "shared"), os.DirFS(shared)))
	filename := filepath.Join(directory, "eslint-refiner.md")
	require.NoError(t, os.WriteFile(filename, source, 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetWorkflowIdentifier("example.md")
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(filename))
	compiled, err := os.ReadFile(filepath.Join(directory, "eslint-refiner.lock.yml"))
	require.NoError(t, err)
	result := string(compiled)
	require.Contains(t, result, `GH_AW_WORK_QUEUE_ROLE: "worker"`)
	require.Contains(t, result, "work_queue_prepare_persist_eslint_memory_0:")
	require.Contains(t, result, "work_queue_prepare_persist_eslint_memory_15:")
	require.Contains(t, result, "work_queue_prepare_claim_script.cjs")
	require.Contains(t, result, "work_queue_memory.cjs")
	var compiledWorkflow map[string]any
	require.NoError(t, yaml.Unmarshal(compiled, &compiledWorkflow))
	job := compiledWorkflow["jobs"].(map[string]any)["work_queue_prepare_persist_eslint_memory_0"].(map[string]any)
	for _, rawStep := range job["steps"].([]any) {
		step := rawStep.(map[string]any)
		if step["id"] != "claim_adapter_context" {
			continue
		}
		var adapter map[string]any
		require.NoError(t, json.Unmarshal([]byte(step["env"].(map[string]any)["GH_AW_CLAIM_ADAPTER_CONFIG"].(string)), &adapter))
		require.Equal(t, "git_tree", adapter["effect-type"])
		require.Equal(t, "script", adapter["mode"])
		require.Equal(t, "github/gh-aw", adapter["target-repo"])
	}
	require.Contains(t, result, "eslint-refiner.json")
	require.Contains(t, result, "memory/eslint-refiner-runs")
	require.Contains(t, result, "create_discussion")
	require.Contains(t, result, "create_issue")
	require.Contains(t, result, "work_queue_restore_memory.cjs")
	require.Contains(t, result, "Restore legacy and immutable Claim memory read-only")
	require.Contains(t, result, `const directory = \"/tmp/gh-aw/agent/eslint-refiner-memory\";`)
	require.Contains(t, result, "cat /tmp/gh-aw/agent/eslint-refiner-memory/history.json")
	require.Contains(t, result, "mcp__work-queue")
	require.Contains(t, result, "mcp__safeoutputs")
	require.NotContains(t, result, "Bash(work-queue:*)")
	require.NotContains(t, result, "cat /tmp/gh-aw/eslint-refiner-memory/history.json")
	require.NotContains(t, result, "push_repo_memory.cjs")
}

func TestWorkQueueFactoryDocumentationLinksWorkflows(t *testing.T) {
	page, err := os.ReadFile("../../docs/src/content/docs/gallery/linter-factory.md")
	require.NoError(t, err)
	links := regexp.MustCompile(`https://github\.com/github/gh-aw/blob/main/\.github/workflows/(eslint-[a-z-]+\.md)`).FindAllStringSubmatch(string(page), -1)
	var workflows []string
	for _, link := range links {
		workflows = append(workflows, link[1])
	}
	require.ElementsMatch(t, []string{"eslint-factory-dispatcher.md", "eslint-miner.md", "eslint-refiner.md", "eslint-monster.md"}, workflows)
}
