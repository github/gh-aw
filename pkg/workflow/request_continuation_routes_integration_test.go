//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type continuationCompiledStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type continuationCompiledJob struct {
	Permissions any                        `yaml:"permissions"`
	Outputs     map[string]string          `yaml:"outputs"`
	Steps       []continuationCompiledStep `yaml:"steps"`
}

type continuationCompiled struct {
	text string
	Jobs map[string]continuationCompiledJob `yaml:"jobs"`
}

func (c continuationCompiled) step(t *testing.T, job, id string) (int, continuationCompiledStep) {
	t.Helper()
	for index, step := range c.Jobs[job].Steps {
		if step.ID == id {
			return index, step
		}
	}
	return -1, continuationCompiledStep{}
}

func (c continuationCompiled) stepNamed(job, name string) int {
	for index, step := range c.Jobs[job].Steps {
		if step.Name == name {
			return index
		}
	}
	return -1
}

func (c continuationCompiled) permission(job, scope string) string {
	permissions, _ := c.Jobs[job].Permissions.(map[string]any)
	value, _ := permissions[scope].(string)
	return value
}

func continuationHandlerConfig(t *testing.T, step continuationCompiledStep) map[string]any {
	t.Helper()
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &config))
	return config
}

func newContinuationRepo(t *testing.T) string {
	t.Helper()
	root := testutil.TempDir(t, "continuation-routes-")
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	return dir
}

func compileContinuation(t *testing.T, dir, name, source string) (continuationCompiled, error) {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.gitRoot = filepath.Dir(filepath.Dir(dir))
	compiler.SetApprove(true)
	if err := compiler.CompileWorkflow(path); err != nil {
		return continuationCompiled{}, err
	}
	content, err := os.ReadFile(filepath.Join(dir, name+".lock.yml"))
	require.NoError(t, err)
	compiled := continuationCompiled{text: string(content)}
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	return compiled, nil
}

func mustCompileContinuation(t *testing.T, dir, name, source string) continuationCompiled {
	t.Helper()
	compiled, err := compileContinuation(t, dir, name, source)
	require.NoError(t, err)
	return compiled
}

const continuationReceiver = `---
on:
  workflow_dispatch:
    inputs:
      gh_aw_continuation:
        description: Continuation handoff
        type: string
        required: false
engine: claude
safe-outputs:
  noop:
---
Continue the summary.
`

func assertContinuationEvidenceBound(t *testing.T, compiled continuationCompiled) {
	t.Helper()
	index, step := compiled.step(t, "agent", continuationEvidenceStepID)
	require.GreaterOrEqual(t, index, 0)
	fallback := compiled.stepNamed("agent", "Upload agent output fallback artifact")
	require.Equal(t, fallback-1, index, "evidence binds the bytes immediately before the uploads")
	_, fallbackStep := compiled.stepNamed("agent", "Upload agent output fallback artifact"), compiled.Jobs["agent"].Steps[fallback]
	require.Equal(t, fallbackStep.If, step.If, "evidence uses the uploads' redaction and envelope guards")
	require.Contains(t, step.With["script"], "bind_continuation_evidence.cjs")
	require.Equal(t, "${{ steps.bind_continuation_evidence.outputs.continuation_evidence }}", compiled.Jobs["agent"].Outputs["continuation_evidence"])
	_, manager := compiled.step(t, "safe_outputs", "process_safe_outputs")
	require.Equal(t, "${{ needs.agent.outputs.continuation_evidence }}", manager.Env["GH_AW_CONTINUATION_EVIDENCE"])
	require.NotContains(t, compiled.permission("agent", "actions"), "write")
	require.NotContains(t, compiled.permission("agent", "issues"), "write")
}

func TestContinuationRoutesCompilation(t *testing.T) {
	t.Run("dispatch route is the only action", func(t *testing.T) {
		dir := newContinuationRepo(t)
		receiver := mustCompileContinuation(t, dir, "summarize", continuationReceiver)
		require.Contains(t, receiver.text, "gh_aw_continuation:")
		require.NotContains(t, receiver.text, continuationEvidenceStepID)

		compiled := mustCompileContinuation(t, dir, "source", `---
on: push
engine: claude
safe-outputs:
  report-incomplete:
    create-issue: false
  request-continuation:
    route: dispatch
    workflow: summarize
    max-depth: 2
---
Summarize page 1 and request a continuation when more pages remain.
`)
		assertContinuationEvidenceBound(t, compiled)
		require.Equal(t, "write", compiled.permission("safe_outputs", "actions"))
		require.Empty(t, compiled.permission("safe_outputs", "issues"))
		_, manager := compiled.step(t, "safe_outputs", "process_safe_outputs")
		config := continuationHandlerConfig(t, manager)
		require.Equal(t, map[string]any{"max": float64(1), "route": "dispatch", "max_depth": float64(2), "workflow": "summarize", "workflow_file": "summarize.lock.yml", "aw_context": true}, config["request_continuation"])
		require.NotContains(t, config, "create_issue", "a route-only source is not given a default create_issue")
		require.NotContains(t, config, "dispatch_workflow", "routing exposes no dispatch_workflow agent tool")
		require.NotContains(t, compiled.text, "Finish work queue claim")
		// The conclusion recorder still runs only the metadata handler.
		require.Contains(t, compiled.text, "record_request_continuation.cjs")
		require.Contains(t, compiled.text, "safe_outputs_request_continuation_dispatch.md")
	})

	t.Run("advisory request stays metadata only", func(t *testing.T) {
		dir := newContinuationRepo(t)
		compiled := mustCompileContinuation(t, dir, "advisory", `---
on: push
engine: claude
safe-outputs:
  request-continuation:
    max: 2
  add-comment:
---
Record a follow-up request.
`)
		require.NotContains(t, compiled.text, continuationEvidenceStepID)
		require.NotContains(t, compiled.text, "GH_AW_CONTINUATION_EVIDENCE")
		require.NotContains(t, compiled.text, "safe_outputs_request_continuation_")
		require.Empty(t, compiled.permission("safe_outputs", "actions"))
		_, manager := compiled.step(t, "safe_outputs", "process_safe_outputs")
		require.Equal(t, map[string]any{"max": float64(2)}, continuationHandlerConfig(t, manager)["request_continuation"])
	})

	t.Run("workqueue worker finishes with accepted receipts", func(t *testing.T) {
		dir := newContinuationRepo(t)
		compiled := mustCompileContinuation(t, dir, "worker", `---
on: workflow_dispatch
engine: claude
tools:
  work-queue:
    worker: true
safe-outputs:
  report-incomplete:
    create-issue: false
  request-continuation:
    route: workqueue
    max-depth: 3
---
Audit the assigned pages.
`)
		assertContinuationEvidenceBound(t, compiled)
		managerIndex, manager := compiled.step(t, "safe_outputs", "process_safe_outputs")
		finishIndex, finish := compiled.step(t, "safe_outputs", "work_queue_finish")
		require.Greater(t, finishIndex, managerIndex)
		require.Empty(t, manager.If, "the manager runs on every worker pass, including finish-only")
		require.NotContains(t, finish.If, "always()", "the finish runs only after successful Process Safe Outputs")
		require.Equal(t, "${{ steps.process_safe_outputs.outputs.work_queue_continuations }}", finish.Env["GH_AW_WORK_QUEUE_CONTINUATIONS"])
		require.Equal(t, "${{ needs.agent.outputs.continuation_evidence }}", finish.Env["GH_AW_CONTINUATION_EVIDENCE"])
		require.Equal(t, "${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}", finish.Env["GH_AW_AGENT_OUTPUT"])
		require.Equal(t, "workqueue", finish.Env["GH_AW_CONTINUATION_ROUTE"])
		require.Equal(t, "3", finish.Env["GH_AW_CONTINUATION_MAX_DEPTH"])
		require.Equal(t, manager.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"], finish.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"])
		require.Equal(t, map[string]any{"max": float64(1), "route": "workqueue", "max_depth": float64(3)}, continuationHandlerConfig(t, manager)["request_continuation"])
		require.Empty(t, compiled.permission("safe_outputs", "issues"))
		require.Contains(t, compiled.text, "safe_outputs_request_continuation_queue.md")
	})

	t.Run("issue worker and its dispatcher", func(t *testing.T) {
		dir := newContinuationRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "shared-continuation.md"), []byte("---\nsafe-outputs:\n  request-continuation:\n    route: issue\n---\n"), 0o600))
		worker := mustCompileContinuation(t, dir, "worker", `---
on: workflow_dispatch
engine: claude
imports:
  - shared-continuation.md
tools:
  work-queue:
    worker: true
safe-outputs:
  report-incomplete:
    create-issue: false
---
Audit the assigned pages.
`)
		assertContinuationEvidenceBound(t, worker)
		require.Equal(t, "write", worker.permission("safe_outputs", "issues"))
		_, finish := worker.step(t, "safe_outputs", "work_queue_finish")
		require.Equal(t, "issue", finish.Env["GH_AW_CONTINUATION_ROUTE"])

		dispatcher := mustCompileContinuation(t, dir, "dispatcher", `---
on: workflow_dispatch
engine: claude
tools:
  work-queue: true
safe-outputs:
  report-incomplete:
    create-issue: false
  dispatch-workflow:
    workflows: [worker]
---
Dispatch queue work.
`)
		require.Equal(t, "read", dispatcher.permission("safe_outputs", "issues"), "dispatchers read approval Issues")
		require.NotContains(t, dispatcher.permission("agent", "issues"), "write")
		require.NotContains(t, dispatcher.text, continuationEvidenceStepID)
	})

	t.Run("dispatcher without issue route gets no issue read", func(t *testing.T) {
		dir := newContinuationRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.md"), []byte("---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  request-continuation:\n    route: workqueue\n---\nwork\n"), 0o600))
		dispatcher := mustCompileContinuation(t, dir, "dispatcher", "---\non: workflow_dispatch\nengine: claude\ntools:\n  work-queue: true\nsafe-outputs:\n  report-incomplete:\n    create-issue: false\n  dispatch-workflow:\n    workflows: [worker]\n---\nDispatch.\n")
		require.Empty(t, dispatcher.permission("safe_outputs", "issues"))
	})

	t.Run("rejected contracts", func(t *testing.T) {
		dir := newContinuationRepo(t)
		mustCompileContinuation(t, dir, "summarize", continuationReceiver)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "no-input.yml"), []byte("on:\n  workflow_dispatch:\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "queue-worker.md"), []byte("---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\n---\nwork\n"), 0o600))
		// A receiver whose source dropped the input while its lock still declares it is stale.
		stale := strings.Replace(continuationReceiver, "Continue the summary.", "Stale.", 1)
		mustCompileContinuation(t, dir, "stale", stale)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.md"), []byte("---\non: workflow_dispatch\nengine: claude\n---\nStale.\n"), 0o600))

		for name, entry := range map[string]struct{ source, message string }{
			"missing input":            {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: no-input\n---\nx\n", "gh_aw_continuation"},
			"stale target":             {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: stale\n---\nx\n", "source stale.md"},
			"absent target":            {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: absent\n---\nx\n", "not found"},
			"queue target":             {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: queue-worker\n---\nx\n", "work-queue workflow"},
			"self target":              {"---\non: workflow_dispatch\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: self\n---\nx\n", "is this workflow"},
			"routed max":               {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: summarize\n    max: 2\n---\nx\n", "request-continuation"},
			"deep chain":               {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: summarize\n    max-depth: 17\n---\nx\n", "max-depth"},
			"queue route on ordinary":  {"---\non: push\nengine: claude\nsafe-outputs:\n  request-continuation:\n    route: workqueue\n---\nx\n", "requires an assignment-bound work-queue worker"},
			"dispatch route on worker": {"---\non: workflow_dispatch\nengine: claude\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  request-continuation:\n    route: dispatch\n    workflow: summarize\n---\nx\n", "route: dispatch requires an ordinary workflow"},
		} {
			fileName := strings.ReplaceAll(name, " ", "-")
			if name == "self target" {
				fileName = "self"
			}
			_, err := compileContinuation(t, dir, fileName, entry.source)
			require.ErrorContains(t, err, entry.message, name)
		}
	})
}
