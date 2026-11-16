//go:build !integration

package cli

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCrossRepositoryDispatchCaptureToGoEvaluation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the dispatch capture regression")
	}
	scripts, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup", "js"))
	require.NoError(t, err)
	root := t.TempDir()
	command := exec.Command(node, "-e", `
const assert = require("node:assert/strict");
const path = require("node:path");
const [scripts, root] = process.argv.slice(1);
global.core = { info() {}, warning() {}, error() {} };
global.context = {
  repo: { owner: "caller-org", repo: "caller-repo" },
  ref: "refs/heads/caller-main",
  payload: { repository: { default_branch: "caller-main" } },
};
global.github = {
  rest: { actions: {
    createWorkflowDispatch: async parameters => {
      assert.deepEqual(parameters, {
        owner: "remote-org",
        repo: "remote-repo",
        workflow_id: "worker.lock.yml",
        ref: "refs/heads/remote-main",
        inputs: {},
        return_run_details: true,
      });
      return { data: { workflow_run_id: 987654 } };
    },
  } },
};
const { main } = require(path.join(scripts, "dispatch_workflow.cjs"));
const { extractCreatedItemFromResult, createManifestLogger } = require(path.join(scripts, "safe_output_manifest.cjs"));
(async () => {
  const handler = await main({
    "target-repo": "remote-org/remote-repo",
    "target-ref": "refs/heads/remote-main",
    allowed_repos: ["remote-org/remote-repo"],
    workflows: ["worker"],
    workflow_files: { worker: ".lock.yml" },
  });
  const result = await handler({ type: "dispatch_workflow", workflow_name: "worker", inputs: {} }, {});
  assert.equal(result.success, true, result.error);
  createManifestLogger(path.join(root, "safe-output-items.jsonl"))(
    extractCreatedItemFromResult("dispatch_workflow", result)
  );
})().catch(error => { console.error(error); process.exitCode = 1; });
`, scripts, root)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)

	items := extractCreatedItemsFromManifest(root)
	require.Len(t, items, 1)
	require.Equal(t, "remote-org/remote-repo", items[0].Repo)
	require.Equal(t, int64(987654), dispatchAdoptionRunID(items[0].Metadata))

	old := workflowAdoptionGHAPIGet
	t.Cleanup(func() { workflowAdoptionGHAPIGet = old })
	calls := 0
	workflowAdoptionGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
		calls++
		require.Equal(t, "actions/runs/987654", endpoint)
		require.Equal(t, "remote-org/remote-repo", repo)
		return map[string]any{"status": "completed", "conclusion": "success"}, nil
	}
	reports := EvaluateAdoptions(context.Background(), items, "caller-org/caller-repo", nil)
	require.Equal(t, 1, calls)
	require.Len(t, reports, 1)
	require.Equal(t, "remote-org/remote-repo", reports[0].Repo)
	require.Equal(t, AdoptionStatusAccepted, reports[0].AdoptionStatus)
	require.Equal(t, "workflow_success", reports[0].Signal)
	require.Empty(t, reports[0].EvalError)
}
