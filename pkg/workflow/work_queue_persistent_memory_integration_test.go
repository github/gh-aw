//go:build integration

package workflow

import (
	"encoding/json"
	"fmt"
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
	require.NoError(t, os.Symlink(shared, filepath.Join(directory, "shared")))
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
	require.NotContains(t, result, "push_repo_memory.cjs")
}

func TestWorkQueueFactoryDocumentationExcerptsCompile(t *testing.T) {
	page, err := os.ReadFile("../../docs/src/content/docs/patterns/linter-factory.md")
	require.NoError(t, err)
	blocks := regexp.MustCompile("(?s)```aw[^\\n]*\\n(.*?)\\n```").FindAllStringSubmatch(string(page), -1)
	require.Len(t, blocks, 3)
	source, err := os.ReadFile("../../.github/workflows/eslint-refiner.md")
	require.NoError(t, err)
	var refiner map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(string(source), "---", 3)[1]), &refiner))
	for index, block := range blocks {
		t.Run(fmt.Sprintf("excerpt-%d", index), func(t *testing.T) {
			parts := strings.SplitN(block[1], "---", 3)
			require.Len(t, parts, 3)
			var configuration map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &configuration))
			if _, hasTrigger := configuration["on"]; !hasTrigger {
				configuration["on"] = "workflow_dispatch"
			}
			configuration["engine"] = "claude"
			configuration["permissions"] = map[string]any{"contents": "read"}
			if memory := configuration["tools"].(map[string]any)["work-queue"]; index == 2 {
				require.Equal(t, refiner["tools"].(map[string]any)["work-queue"], memory)
			}
			frontmatter, err := yaml.Marshal(configuration)
			require.NoError(t, err)
			directory := filepath.Join(testutil.TempDir(t, "work-queue-factory-docs-"), ".github", "workflows")
			require.NoError(t, os.MkdirAll(directory, 0o700))
			for _, worker := range []string{"eslint-miner", "eslint-refiner", "eslint-monster"} {
				for _, extension := range []string{".md", ".lock.yml"} {
					target, err := filepath.Abs("../../.github/workflows/" + worker + extension)
					require.NoError(t, err)
					require.NoError(t, os.Symlink(target, filepath.Join(directory, worker+extension)))
				}
			}
			filename := filepath.Join(directory, "example.md")
			require.NoError(t, os.WriteFile(filename, []byte("---\n"+string(frontmatter)+"---\n"+parts[2]), 0o600))
			compiler := NewCompiler(WithVersion("integration"))
			compiler.SetWorkflowIdentifier("example.md")
			compiler.SetApprove(true)
			require.NoError(t, compiler.CompileWorkflow(filename))
		})
	}
}
