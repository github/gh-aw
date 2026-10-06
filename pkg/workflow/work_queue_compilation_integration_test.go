//go:build integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueCompilationPhases(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-compilation-")
	workflowPath := filepath.Join(dir, "work-queue-worker.md")
	workflow := `---
on: workflow_dispatch
name: Work Queue Worker Integration
engine: claude
tools:
  work-queue:
    storage: git
    require-assignment: true
    worker: true
safe-outputs:
  create-issue:
    max: 1
---

Compile each work-queue workflow phase.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))

	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(dir, "work-queue-worker.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)
	require.Contains(t, compiled, "work_queue_assignment:")
	require.Contains(t, compiled, workQueueWorkerRunName)
	require.NotContains(t, compiled, "work_queue_claim:")
	require.Contains(t, compiled, "aw_context:")

	activation := extractJobSection(compiled, string(constants.ActivationJobName))
	require.Contains(t, activation, "Snapshot work queue state")
	require.Contains(t, activation, constants.WorkQueueSnapshotPath)

	agent := extractJobSection(compiled, string(constants.AgentJobName))
	require.Contains(t, agent, `"work-queue"`)
	require.Contains(t, agent, constants.WorkQueueFinishIntentMount)
	require.Contains(t, agent, constants.WorkQueueFinishIntentPath)
	require.Contains(t, agent, "collect_work_queue_intents.cjs")
	require.Contains(t, agent, "Collect work queue intents")
	require.Less(t, strings.Index(agent, "Collect work queue intents"), strings.Index(agent, "Redact secrets in logs"))
	require.Contains(t, agent, "steps.redact_secrets.outcome == 'success'")

	safeOutputs := extractJobSection(compiled, string(constants.SafeOutputsJobName))
	require.Contains(t, safeOutputs, "contents: write")
	require.Contains(t, safeOutputs, "actions: write")
	require.Contains(t, safeOutputs, "work_queue_controls")
	require.Contains(t, safeOutputs, constants.WorkQueueFinishIntentPath)
	require.Contains(t, safeOutputs, constants.WorkQueueIntentPath)
	require.Contains(t, safeOutputs, "Download activation artifact for work queue")
	require.Contains(t, safeOutputs, "Reconcile work queue claim")
	require.Contains(t, safeOutputs, "requireAssignment: true")
	gate := "steps.work_queue_claim_reconciliation.outputs.authorized == 'true'"
	require.NotContains(t, safeOutputs, gate)
	require.Contains(t, safeOutputs, "GH_AW_WORK_QUEUE_ENABLED")
	require.Contains(t, compiled, "work_queue_scoped")
	require.Contains(t, safeOutputs, "id: process_safe_outputs")
	require.Less(t,
		strings.Index(safeOutputs, "Reconcile work queue claim"),
		strings.Index(safeOutputs, "id: process_safe_outputs"),
		"claim reconciliation must precede ordinary safe-output handlers",
	)
	handlerID := strings.Index(safeOutputs, "id: process_safe_outputs")
	handlerStart := strings.LastIndex(safeOutputs[:handlerID], "      - name:")
	require.GreaterOrEqual(t, handlerStart, 0)
	handlerEnd := strings.Index(safeOutputs[handlerStart+len("      - name:"):], "      - name:")
	if handlerEnd < 0 {
		handlerEnd = len(safeOutputs) - handlerStart
	} else {
		handlerEnd += len("      - name:")
	}

	require.NotContains(t, safeOutputs[handlerStart:handlerStart+handlerEnd], gate)

	conclusion := extractJobSection(compiled, "conclusion")
	require.Contains(t, conclusion, "contents: read")
	require.Contains(t, conclusion, "Download activation artifact for work queue summary")
	require.Contains(t, conclusion, "Summarize work queue activity\n        if: always()")
	require.Contains(t, conclusion, "work_queue_summary.cjs")
	require.Contains(t, conclusion, "await main({ core, githubClient: github, context });")
}

func TestIssueWorkQueueCompilationPhases(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-issues-")
	workflowPath := filepath.Join(dir, "issue-worker.md")
	workflow := `---
on: workflow_dispatch
name: Issue Work Queue Worker
engine: claude
tools:
  work-queue:
    storage: issues
    worker: true
---

Read and finish assigned work.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))
	issueCompiler := NewCompiler(WithVersion("integration"))
	issueCompiler.SetApprove(true)
	require.Error(t, issueCompiler.CompileWorkflow(workflowPath))
	_, err := os.Stat(filepath.Join(dir, "issue-worker.lock.yml"))
	require.True(t, os.IsNotExist(err), "unsupported Issues backend must not produce a lock file")
}

func TestWorkQueueCustomAndStandaloneAdapterCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "claim-adapter-compilation-")
	workflowPath := filepath.Join(dir, "claim-worker.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: claude
tools:
  work-queue:
    worker: true
safe-outputs:
  upload-asset:
    branch: assets/trusted
  upload-code-coverage:
    target-ref: refs/heads/approved
  create-code-scanning-alert:
    target-ref: refs/heads/approved
  claim-adapters:
    code:
      mode: prepared
      effect-type: git_tree
      target-repo: owner/repo
      field-map:
        files: prepared_files
        title: title
      git-tree:
        base-revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        branch-prefix: automation/code
        pull-request: true
        base-branch: main
    checks:
      mode: prepared
      effect-type: github_rest
      target-repo: owner/repo
      field-map:
        name: check_name
        head_sha: revision
      expected:
        status: completed
        conclusion: success
      request:
        method: POST
        route: /repos/{owner}/{repo}/check-runs
        permission: checks
      verifier:
        route: /repos/{owner}/{repo}/check-runs/{receipt_id}
        resource-kind: check_run
        fields:
          name: name
          head_sha: head_sha
          status: status
          conclusion: conclusion
    discussion:
      mode: prepared
      effect-type: github_graphql
      target-repo: owner/repo
      field-map:
        title: title
        body: body
        categoryId: category
      graphql:
        mutation: createDiscussion
        input-type: CreateDiscussionInput
        response-field: discussion
        resource-type: Discussion
        resource-kind: discussion
        repository-input: repositoryId
        repository-field: repository.nameWithOwner
        number-field: number
        permission: discussions
        fields:
          title: title
          body: body
          categoryId: category.id
    prepared:
      mode: prepared
      effect-type: update_issue
      target-repo: owner/repo
      field-map:
        body: content
        item_number: number
    scripted:
      mode: script
      effect-type: add_comment
      target-repo: owner/repo
      field-map:
        body: content
        item_number: number
    raw_steps:
      mode: prepared
      effect-type: update_issue
      target-repo: owner/repo
      field-map:
        body: content
        item_number: number
  jobs:
    discussion:
      steps:
        - run: node prepare-discussion.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"
    code:
      steps:
        - run: node prepare-code.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"
    checks:
      steps:
        - run: node prepare-check.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"
    prepared:
      env:
        PREPARER_MODE: data-only
      steps:
        - run: node prepare.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"
  scripts:
    scripted:
      script: |
        return { content: item.content, number: item.number };
  steps:
    - run: node raw.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"
---
Process original immutable Claims.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	content, err := os.ReadFile(filepath.Join(dir, "claim-worker.lock.yml"))
	require.NoError(t, err)
	compiled := string(content)
	require.NotContains(t, compiled, "\n  upload_assets:")
	require.NotContains(t, compiled, "\n  upload_code_coverage:")
	require.NotContains(t, compiled, "\n  upload_code_scanning_sarif:")
	require.NotContains(t, compiled, "\n  prepared:")
	for _, name := range []string{"code", "checks", "discussion", "prepared", "scripted", "raw_steps"} {
		job := extractJobSection(compiled, "work_queue_prepare_"+name+"_0")
		require.NotContains(t, job, "matrix:")
		require.Contains(t, job, "contents: read")
		require.NotContains(t, job, "contents: write")
		require.NotContains(t, job, "issues: write")
		require.Contains(t, job, "path: /tmp/gh-aw/claims/")
		require.Contains(t, job, "steps.redact_secrets.outcome == 'success'")
	}
	require.Contains(t, extractJobSection(compiled, "work_queue_prepare_prepared_0"), "PREPARER_MODE")
	require.Contains(t, extractJobSection(compiled, "work_queue_prepare_scripted_0"), "work_queue_prepare_claim_script.cjs")
	trusted := extractJobSection(compiled, "safe_outputs")
	require.Contains(t, trusted, "checks: write")
	require.Contains(t, trusted, "contents: write")
	require.Contains(t, trusted, "pull-requests: write")
	require.Contains(t, trusted, "git_tree")
	require.Contains(t, trusted, "artifact-ids: ${{ needs.work_queue_prepare_code_15.outputs.artifact_id }}")
	require.NotContains(t, trusted, "node prepare-code.cjs")
	require.Contains(t, trusted, "github_rest")
	require.Contains(t, trusted, "github_graphql")
	require.Contains(t, trusted, "discussions: write")
	require.Contains(t, trusted, "artifact-ids: ${{ needs.work_queue_prepare_discussion_15.outputs.artifact_id }}")
	require.NotContains(t, trusted, "node prepare-discussion.cjs")
	require.Contains(t, trusted, "work_queue_control_adapter.cjs")
	require.Less(t, strings.Index(trusted, "finish_work_queue_claim.cjs"), strings.Index(trusted, "work_queue_control_adapter.cjs"))
	require.Contains(t, trusted, "receipt_id")
	require.Contains(t, trusted, "artifact-ids: ${{ needs.work_queue_prepare_checks_15.outputs.artifact_id }}")
	require.NotContains(t, trusted, "node raw.cjs")
	require.NotContains(t, trusted, "node prepare.cjs")
	require.Contains(t, trusted, "Download Claim-scoped staged assets")
	require.Contains(t, trusted, "Download Claim-scoped coverage reports")
	require.Contains(t, trusted, "code-quality: write")
	require.NotContains(t, trusted, "Configure Safe Output Scripts")
	require.Contains(t, trusted, "artifact-ids: ${{ needs.work_queue_prepare_prepared_0.outputs.artifact_id }}")
	require.Contains(t, trusted, "claim_adapters")
	require.Contains(t, trusted, "always()")
}

func TestWorkQueueDispatchCompilerConfiguration(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-dispatch-")
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
	workerPath := filepath.Join(workflowsDir, "worker.md")
	require.NoError(t, os.WriteFile(workerPath, []byte(`---
on:
  workflow_dispatch:
tools:
  work-queue:
    storage: git
    worker: true
---
Process the assigned work.
`), 0o600))
	dispatcherPath := filepath.Join(workflowsDir, "dispatcher.md")
	require.NoError(t, os.WriteFile(dispatcherPath, []byte(`---
on: workflow_dispatch
tools:
  work-queue:
    storage: git
safe-outputs:
  dispatch-workflow:
    workflows: [worker]
---
Read the queue and dispatch an available Work identity.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(dispatcherPath))
	compiled, err := os.ReadFile(filepath.Join(workflowsDir, "dispatcher.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compiled), `work_queue_enabled`)
	require.Contains(t, string(compiled), `work_queue_workflows`)
	require.Contains(t, string(compiled), `work_queue`)
	require.NotContains(t, string(compiled), "WORK_QUEUE_HMAC_SECRET")
	require.NotContains(t, string(compiled), "GH_AW_WORK_QUEUE_POLICY:")
	inputs, err := extractWorkflowDispatchInputs(filepath.Join(workflowsDir, "dispatcher.lock.yml"))
	require.NoError(t, err)
	require.NotContains(t, inputs, WorkQueueClaimInputName)
}

func TestWorkQueueSmokeVerification(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-work-queue.md")
	require.NoError(t, err)
	dir := testutil.TempDir(t, "work-queue-smoke-")
	workflowPath := filepath.Join(dir, "smoke-work-queue.md")
	require.NoError(t, os.WriteFile(workflowPath, source, 0o600))
	require.NoError(t, NewCompiler(WithVersion("integration")).CompileWorkflow(workflowPath))

	lock, err := os.ReadFile(filepath.Join(dir, "smoke-work-queue.lock.yml"))
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]struct {
			Needs any
			Steps []struct {
				Name string
				Run  string
			}
		}
	}
	require.NoError(t, yaml.Unmarshal(lock, &compiled))
	job, ok := compiled.Jobs["verify_smoke_result"]
	require.True(t, ok)
	require.Contains(t, job.Needs, "safe_outputs", "failure issues must be processed before failing the smoke run")
	var script string
	for _, step := range job.Steps {
		if step.Name == "Verify smoke result" {
			script = step.Run
		}
	}
	require.NotEmpty(t, script)

	for _, tc := range []struct {
		name    string
		outputs string
		intent  string
		success bool
	}{
		{"completed", "{\"type\":\"noop\"}\n", "{\"outcome\":\"completed\"}\n", true},
		{"reported failure", "{\"type\":\"create_issue\"}\n", "", false},
		{"failure with success artifacts", "{\"type\":\"noop\"}\n{\"type\":\"create_issue\"}\n", "{\"outcome\":\"completed\"}\n", false},
		{"missing finish intent", "{\"type\":\"noop\"}\n", "", false},
		{"cancelled", "{\"type\":\"noop\"}\n", "{\"outcome\":\"cancelled\"}\n", false},
		{"missing noop", "", "{\"outcome\":\"completed\"}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(evidence, "safeoutputs.jsonl"), []byte(tc.outputs), 0o600))
			if tc.intent != "" {
				require.NoError(t, os.WriteFile(filepath.Join(evidence, "work-queue.finish.jsonl"), []byte(tc.intent), 0o600))
			}
			output, err := exec.Command("bash", "-e", "-c", strings.ReplaceAll(script, "/tmp/gh-aw/", evidence+"/")).CombinedOutput()
			if tc.success {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "%s", output)
			}
		})
	}
}
