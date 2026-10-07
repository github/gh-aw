//go:build integration

package workflow

import (
	"os"
	"path/filepath"
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
	adapter := outputs["claim-adapters"].(map[string]any)["persist_eslint_memory"].(map[string]any)
	require.Equal(t, "script", adapter["mode"])
	require.Equal(t, "git_tree", adapter["effect-type"])
	require.Equal(t, "github/gh-aw", adapter["target-repo"])
	require.Equal(t, "46b68a61c366a01d86dc319b8e689d09a48bad04", adapter["git-tree"].(map[string]any)["base-revision"])
	directory := testutil.TempDir(t, "work-queue-eslint-memory-")
	shared, err := filepath.Abs("../../.github/workflows/shared")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(shared, filepath.Join(directory, "shared")))
	filename := filepath.Join(directory, "eslint-refiner.md")
	require.NoError(t, os.WriteFile(filename, source, 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(filename))
	compiled, err := os.ReadFile(filepath.Join(directory, "eslint-refiner.lock.yml"))
	require.NoError(t, err)
	result := string(compiled)
	require.Contains(t, result, `GH_AW_WORK_QUEUE_ROLE: "worker"`)
	require.Contains(t, result, "work_queue_prepare_persist_eslint_memory_0:")
	require.Contains(t, result, "work_queue_prepare_persist_eslint_memory_15:")
	require.Contains(t, result, "work_queue_prepare_claim_script.cjs")
	require.Contains(t, result, "eslint-refiner.json")
	require.Contains(t, result, "memory/eslint-refiner-runs")
	require.Contains(t, result, "create_discussion")
	require.Contains(t, result, "create_issue")
	require.Contains(t, result, "work_queue_restore_memory.cjs")
	require.Contains(t, result, "Restore legacy and immutable Claim memory read-only")
	require.NotContains(t, result, "push_repo_memory.cjs")
}
