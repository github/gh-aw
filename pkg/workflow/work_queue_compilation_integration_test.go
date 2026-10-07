//go:build integration

package workflow

import (
	"encoding/json"
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

func TestWorkQueueDispatchCredentialActualCompilation(t *testing.T) {
	for _, entry := range []struct {
		name     string
		global   string
		dispatch string
		kind     string
		token    string
		app      bool
	}{
		{"default", "", "", "${{ secrets.GH_AW_GITHUB_TOKEN != '' && 'authenticated' || 'github_token' }}", "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", false},
		{"default-explicit", "", "    github-token: ${{ secrets.GITHUB_TOKEN }}\n", "github_token", "${{ secrets.GITHUB_TOKEN }}", false},
		{"pat", "  github-token: ${{ secrets.PUBLISHER_PAT }}\n", "    github-token: ${{ secrets.WORKER_PAT }}\n", "authenticated", "${{ secrets.WORKER_PAT }}", false},
		{"app", "", "    github-app:\n      app-id: ${{ vars.WORKER_APP_ID }}\n      private-key: ${{ secrets.WORKER_APP_KEY }}\n", "github_app", "${{ steps.work-queue-dispatch-app-token.outputs.token }}", true},
		{"global-app", "  github-app:\n    app-id: ${{ vars.WORKER_APP_ID }}\n    private-key: ${{ secrets.WORKER_APP_KEY }}\n", "", "github_app", "${{ steps.work-queue-dispatch-app-token.outputs.token }}", true},
	} {
		t.Run(entry.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "work-queue-dispatch-credential-")
			dir = filepath.Join(dir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.md"), []byte("---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\n---\nProcess queue work.\n"), 0o600))
			filename := filepath.Join(dir, "dispatcher.md")
			source := "---\non: workflow_dispatch\nengine: claude\ntools:\n  work-queue:\n    storage: git\nsafe-outputs:\n" +
				entry.global + "  dispatch-workflow:\n    workflows: [worker]\n" + entry.dispatch + "---\nProcess queue work.\n"
			require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
			compiler := NewCompiler(WithVersion("integration"))
			compiler.SetApprove(true)
			require.NoError(t, compiler.CompileWorkflow(filename))
			content, err := os.ReadFile(filepath.Join(dir, "dispatcher.lock.yml"))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]struct {
					Steps []struct {
						ID   string            `yaml:"id"`
						With map[string]string `yaml:"with"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			mint, controls, handlers := -1, -1, -1
			for index, step := range compiled.Jobs["safe_outputs"].Steps {
				switch step.ID {
				case workQueueDispatchAppTokenStepID:
					mint = index
					require.Equal(t, "write", step.With["permission-actions"])
					require.NotContains(t, step.With, "permission-contents")
				case "work_queue_controls":
					controls = index
					var config map[string]any
					require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"]), &config))
					require.Equal(t, entry.token, config["github-token"])
					credential := config["work_queue_dispatch_credential"].(map[string]any)
					require.Equal(t, entry.kind, credential["kind"])
					require.NotContains(t, credential, "principal")
					require.NotEmpty(t, step.With["github-token"])
					if entry.name == "pat" {
						require.Equal(t, "${{ secrets.PUBLISHER_PAT }}", step.With["github-token"])
					}
					assertWorkQueueScriptSyntax(t, step.With["script"])
				case "process_safe_outputs":
					handlers = index
					var config map[string]any
					require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &config))
					dispatch := config["dispatch_workflow"].(map[string]any)
					require.Equal(t, entry.token, dispatch["github-token"])
					require.Equal(t, entry.kind, dispatch["work_queue_dispatch_credential"].(map[string]any)["kind"])
				}
			}
			require.GreaterOrEqual(t, controls, 0)
			require.Greater(t, handlers, controls)
			if entry.app {
				require.GreaterOrEqual(t, mint, 0)
				require.Less(t, mint, controls)
			} else {
				require.Equal(t, -1, mint)
			}
			require.NotContains(t, string(content), "GH_AW_WORK_QUEUE_POLICY:")
		})
	}
}

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
steps:
  - name: User origin ordering marker
    run: echo prepared
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
	assertWorkQueueProtectedOriginTransport(t, lockContent)
	compiled := string(lockContent)
	require.Contains(t, compiled, `GH_AW_WORK_QUEUE_ROLE: "worker"`)
	require.NotContains(t, compiled, "GH_AW_WORK_QUEUE_POLICY:")
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

func TestWorkQueueNamedProfileCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-named-profile-")
	path := filepath.Join(dir, "worker.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
engine: claude
tools:
  work-queue:
    worker: true
work-queue-policy:
  producers:
    '777':
      pools: [default]
      priorities: [1, 2, 3, 4, 5]
      fairness-keys: ['']
  worker-profiles:
    approved:
      workflow: .github/workflows/worker.lock.yml
      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      principal: '888'
      trust-domain: trusted
      credential-scope: repository
      effect-scope: owner/repo
safe-outputs:
  noop:
---
Use the explicitly approved profile without an implicit fallback worker.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "worker.lock.yml"))
	require.NoError(t, err)
	compiled := string(content)
	require.Contains(t, compiled, `\"default_profile\":\"approved\"`)
	require.Contains(t, compiled, `\"principal\":\"888\"`)
	require.NotContains(t, compiled, `\"default_profile\":\"default\"`)
	require.NotContains(t, compiled, `\"profiles\":{\"default\":`)
}

func TestWorkQueueGlobalPreviewCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-global-preview-")
	path := filepath.Join(dir, "worker.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
engine: claude
tools:
  work-queue:
    worker: true
safe-outputs:
  staged: false
  noop:
---
Preview the original Claim assignment without publishing facts or effects.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	compiler.SetTrialMode(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "worker.lock.yml"))
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	want := map[string]bool{
		"work_queue_claim_reconciliation": false,
		"work_queue_controls":             false,
		"process_safe_outputs":            false,
	}
	for _, step := range compiled.Jobs[string(constants.SafeOutputsJobName)].Steps {
		if _, required := want[step.ID]; required {
			require.Equal(t, "true", step.Env["GH_AW_SAFE_OUTPUTS_STAGED"], step.ID)
			want[step.ID] = true
		}
	}
	for id, found := range want {
		require.True(t, found, "compiled preview is missing %s", id)
	}
}

func TestWorkQueueObserverCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "work-queue-observer-")
	workflowPath := filepath.Join(dir, "observer.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
tools:
  work-queue: true
safe-outputs:
  create-issue:
    max: 1
  noop:
---
Read the queue and publish an ordinary report.
`), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	content, err := os.ReadFile(filepath.Join(dir, "observer.lock.yml"))
	require.NoError(t, err)
	compiled := string(content)
	require.Contains(t, compiled, `GH_AW_WORK_QUEUE_ROLE: "observer"`)
	require.Contains(t, compiled, "work_queue_origin: ${{ steps.work_queue_snapshot.outputs.work_queue_origin }}")
	require.NotContains(t, compiled, "GH_AW_WORK_QUEUE_POLICY:")
	require.NotContains(t, compiled, "work_queue_claim_reconciliation")
	require.NotContains(t, compiled, "work_queue_controls")
	require.NotContains(t, compiled, "GH_AW_WORK_QUEUE_INTENT_ORIGIN:")
	require.Contains(t, compiled, `"tools": ["work_queue_read", "work_queue_explain"]`)
	require.NotContains(t, compiled, constants.WorkQueueFinishIntentMount)
	require.Contains(t, extractJobSection(compiled, "safe_outputs"), "Process Safe Outputs")
	require.Contains(t, extractJobSection(compiled, "safe_outputs"), "issues: write")
}

func TestWorkQueueRepositoryObserverCompilation(t *testing.T) {
	for _, name := range []string{"smoke-work-queue", "daily-work-queues-report"} {
		t.Run(name, func(t *testing.T) {
			source := filepath.Join("..", "..", ".github", "workflows", name+".md")
			lockPath := strings.TrimSuffix(source, ".md") + ".lock.yml"
			before, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			compiler := NewCompiler(WithVersion("integration"), WithNoEmit(true), WithWorkflowIdentifier(name))
			compiler.SetApprove(true)
			data, err := compiler.ParseWorkflowFile(source)
			require.NoError(t, err)
			require.Equal(t, "observer", workQueueRuntimeRole(data))
			require.NoError(t, compiler.CompileWorkflowData(data, source))
			require.False(t, isWorkQueueParticipant(data))
			require.False(t, workQueueRequiresAssignment(data))
			require.NotNil(t, data.SafeOutputs.CreateIssues)
			require.NotContains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_POLICY:")
			require.Empty(t, compiler.buildWorkQueueClaimReconciliationStep(data))
			controls, err := compiler.buildWorkQueueControlProcessingStep(data)
			require.NoError(t, err)
			require.Empty(t, controls)
			after, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			require.Equal(t, before, after, "no-emit must preserve the parent-owned repository lock")
		})
	}
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
	assertWorkQueueProtectedOriginTransport(t, compiled)
	require.Contains(t, string(compiled), `work_queue_enabled`)
	require.Contains(t, string(compiled), `work_queue_workflows`)
	require.Contains(t, string(compiled), `work_queue`)
	require.NotContains(t, string(compiled), "WORK_QUEUE_HMAC_SECRET")
	require.NotContains(t, string(compiled), "GH_AW_WORK_QUEUE_POLICY:")
	inputs, err := extractWorkflowDispatchInputs(filepath.Join(workflowsDir, "dispatcher.lock.yml"))
	require.NoError(t, err)
	require.NotContains(t, inputs, WorkQueueClaimInputName)
}

func assertWorkQueueProtectedOriginTransport(t *testing.T, content []byte) {
	t.Helper()
	var compiled struct {
		Jobs map[string]struct {
			Env     map[string]string `yaml:"env"`
			Outputs map[string]string `yaml:"outputs"`
			Steps   []struct {
				ID   string            `yaml:"id"`
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
				With map[string]any    `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	activation := compiled.Jobs[string(constants.ActivationJobName)]
	require.Equal(t, "${{ steps.work_queue_snapshot.outputs.work_queue_origin }}", activation.Outputs["work_queue_origin"])
	agent := compiled.Jobs[string(constants.AgentJobName)]
	require.Equal(t, "${{ steps.work_queue_intent_origin.outputs.work_queue_origin }}", agent.Outputs["work_queue_origin"])
	downloadIndex, originIndex, executionIndex := -1, -1, -1
	for index, step := range agent.Steps {
		if step.Name == "Download activation artifact" {
			downloadIndex = index
		}
		if step.ID == "work_queue_intent_origin" {
			originIndex = index
			script, ok := step.With["script"].(string)
			require.True(t, ok)
			require.Contains(t, script, "capture_work_queue_intent_origin.cjs")
			require.Equal(t, 1, strings.Count(script, "const { setupGlobals }"))
			require.Equal(t, 1, strings.Count(script, "setupGlobals(core, github, context, exec, io, getOctokit);"))
			assertWorkQueueScriptSyntax(t, script)
		}
		if step.ID == "agentic_execution" {
			executionIndex = index
			require.NotEqual(t, -1, originIndex, "trusted origin capture must precede engine execution")
		}
		if step.Name == "User origin ordering marker" {
			require.NotEqual(t, -1, originIndex, "trusted origin capture must precede user steps")
		}
	}
	require.GreaterOrEqual(t, downloadIndex, 0)
	require.Greater(t, originIndex, downloadIndex)
	require.Greater(t, executionIndex, originIndex)
	for _, name := range []constants.JobName{constants.AgentJobName, constants.SafeOutputsJobName} {
		job := compiled.Jobs[string(name)]
		expected := "${{ needs.activation.outputs.work_queue_origin }}"
		if name == constants.SafeOutputsJobName {
			expected = "${{ needs.agent.outputs.work_queue_origin }}"
		}
		require.Equal(t, expected, job.Env["GH_AW_WORK_QUEUE_INTENT_ORIGIN"], name)
		for _, step := range job.Steps {
			require.NotContains(t, step.Env, "GH_AW_WORK_QUEUE_INTENT_ORIGIN", "step %s must inherit its protected job-level origin rather than replacing it", step.ID)
		}
	}
}

func assertWorkQueueScriptSyntax(t *testing.T, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	command := exec.Command(node, "--check", "--input-type=commonjs")
	command.Stdin = strings.NewReader("(async () => {\n" + script + "\n});")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestWorkQueueControlScriptSyntax(t *testing.T) {
	for _, worker := range []bool{false, true} {
		name := "dispatcher"
		if worker {
			name = "worker"
		}
		t.Run(name, func(t *testing.T) {
			data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": worker}}, SafeOutputs: &SafeOutputsConfig{}}
			if !worker {
				data.SafeOutputs.DispatchWorkflow = &DispatchWorkflowConfig{Workflows: []string{"worker"}}
			}
			require.NoError(t, validateWorkQueueConfiguration(data))
			var steps []struct {
				With struct {
					Script string
				}
			}
			controlSteps, err := NewCompiler().buildWorkQueueControlProcessingStep(data)
			require.NoError(t, err)
			require.NoError(t, yaml.Unmarshal([]byte(strings.Join(controlSteps, "")), &steps))
			require.Len(t, steps, 1)
			require.NotEmpty(t, steps[0].With.Script)
			assertWorkQueueScriptSyntax(t, steps[0].With.Script)
		})
	}
}

func TestWorkQueueRepositoryObserversUseOrdinaryOutputs(t *testing.T) {
	for _, name := range []string{"daily-work-queues-report", "smoke-work-queue"} {
		t.Run(name, func(t *testing.T) {
			compiler := NewCompiler(WithNoEmit(true), WithVersion("integration"), WithWorkflowIdentifier(name))
			data, err := compiler.ParseWorkflowFile(filepath.Join("../../.github/workflows", name+".md"))
			require.NoError(t, err)
			require.Equal(t, "observer", workQueueRuntimeRole(data))
			require.False(t, workQueueRequiresAssignment(data))
			require.NotNil(t, data.SafeOutputs)
			require.NotNil(t, data.SafeOutputs.CreateIssues)
			require.Empty(t, compiler.buildWorkQueueClaimReconciliationStep(data))
			controlSteps, err := compiler.buildWorkQueueControlProcessingStep(data)
			require.NoError(t, err)
			require.Empty(t, controlSteps)
			permissions, _ := safeOutputsJobPermissions(data)
			require.Contains(t, permissions.RenderToYAML(), "issues: write")
			require.NotContains(t, permissions.RenderToYAML(), "contents: write")
			require.NotContains(t, permissions.RenderToYAML(), "actions: write")
			require.Equal(t, []string{"work_queue_read", "work_queue_explain"}, workQueueMCPToolNames(data))
			require.NoError(t, compiler.CompileWorkflow(filepath.Join("../../.github/workflows", name+".md")))
		})
	}
}

func TestWorkQueueRepositoryObserversEmitProtectedOrdinaryOutputs(t *testing.T) {
	for _, name := range []string{"daily-work-queues-report", "smoke-work-queue"} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(testutil.TempDir(t, "repository-observer-emission-"), ".github", "workflows")
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "shared"), 0o700))
			for _, relative := range []string{name + ".md", filepath.Join("shared", "reporting.md")} {
				source, err := os.ReadFile(filepath.Join("../../.github/workflows", relative))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(directory, relative), source, 0o600))
			}
			filename := filepath.Join(directory, name+".md")
			compiler := NewCompiler(WithVersion("integration"), WithWorkflowIdentifier(name))
			compiler.SetApprove(true)
			data, err := compiler.ParseWorkflowFile(filename)
			require.NoError(t, err)
			require.Equal(t, "observer", workQueueRuntimeRole(data))
			require.NoError(t, compiler.CompileWorkflowData(data, filename), "compile the actual repository source with emission enabled")
			content, err := os.ReadFile(filepath.Join(directory, name+".lock.yml"))
			require.NoError(t, err)
			compiled := string(content)
			require.Contains(t, compiled, `GH_AW_WORK_QUEUE_ROLE: "observer"`)
			require.NotContains(t, compiled, "GH_AW_WORK_QUEUE_POLICY:")
			require.NotContains(t, compiled, "GH_AW_WORK_QUEUE_INTENT_ORIGIN:")
			require.NotContains(t, compiled, "work_queue_claim_reconciliation")
			require.NotContains(t, compiled, "work_queue_controls")
			require.NotContains(t, compiled, constants.WorkQueueFinishIntentMount)
			require.Contains(t, compiled, `"tools": ["work_queue_read", "work_queue_explain"]`)
			safeOutputs := extractJobSection(compiled, string(constants.SafeOutputsJobName))
			require.Contains(t, safeOutputs, "issues: write")
			require.NotContains(t, safeOutputs, "contents: write")
			require.NotContains(t, safeOutputs, "actions: write")
			require.Contains(t, safeOutputs, "Process Safe Outputs")
			var document struct {
				Jobs map[string]struct {
					Steps []struct {
						ID  string            `yaml:"id"`
						Env map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &document))
			var registry map[string]json.RawMessage
			for _, step := range document.Jobs[string(constants.SafeOutputsJobName)].Steps {
				if step.ID == "process_safe_outputs" {
					require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &registry))
				}
			}
			require.Contains(t, registry, "create_issue")
			if name == "smoke-work-queue" {
				require.Contains(t, registry, "noop")
			}
		})
	}
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
		{"observer success", "{\"type\":\"noop\"}\n", "", true},
		{"reported failure", "{\"type\":\"create_issue\"}\n", "", false},
		{"failure with noop", "{\"type\":\"noop\"}\n{\"type\":\"create_issue\"}\n", "", false},
		{"unexpected worker completion", "{\"type\":\"noop\"}\n", "{\"outcome\":\"completed\"}\n", false},
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
