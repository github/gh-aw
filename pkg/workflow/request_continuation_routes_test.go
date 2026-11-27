//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestContinuationRouteParsing(t *testing.T) {
	c := NewCompiler()
	parse := func(fields map[string]any) *RequestContinuationConfig {
		return c.parseRequestContinuationConfig(map[string]any{"request-continuation": fields})
	}
	for _, entry := range []struct {
		fields map[string]any
		route  string
		depth  int
	}{
		{map[string]any{"route": "workqueue"}, "workqueue", 1},
		{map[string]any{"route": "workqueue", "max-depth": 2, "max": 1}, "workqueue", 2},
		{map[string]any{"route": "issue"}, "issue", 0},
		{map[string]any{"route": "dispatch", "workflow": "summarize", "max-depth": float64(16)}, "dispatch", 16},
	} {
		config := parse(entry.fields)
		require.NoError(t, config.parseError, "%v", entry.fields)
		require.Equal(t, entry.route, config.Route)
		require.Equal(t, entry.depth, config.MaxDepth)
		require.Equal(t, "1", *config.Max)
	}
	for _, entry := range []struct {
		fields map[string]any
		field  string
	}{
		{map[string]any{"route": "queue"}, "route"},
		{map[string]any{"route": "${{ inputs.route }}"}, "route"},
		{map[string]any{"route": true}, "route"},
		{map[string]any{"route": "workqueue", "max": 2}, "max"},
		{map[string]any{"route": "workqueue", "max": "${{ inputs.max }}"}, "max"},
		{map[string]any{"route": "workqueue", "max-depth": 0}, "max-depth"},
		{map[string]any{"route": "workqueue", "max-depth": 17}, "max-depth"},
		{map[string]any{"route": "workqueue", "max-depth": 1.5}, "max-depth"},
		{map[string]any{"route": "workqueue", "max-depth": "${{ inputs.depth }}"}, "max-depth"},
		{map[string]any{"route": "workqueue", "workflow": "summarize"}, "workflow"},
		{map[string]any{"route": "issue", "max-depth": 2}, "max-depth"},
		{map[string]any{"route": "dispatch"}, "workflow"},
		{map[string]any{"route": "dispatch", "workflow": "../summarize"}, "workflow"},
		{map[string]any{"route": "dispatch", "workflow": "summarize.md"}, "workflow"},
		{map[string]any{"route": "dispatch", "workflow": "${{ inputs.workflow }}"}, "workflow"},
		{map[string]any{"route": "dispatch", "workflow": "summarize", "github-token": "x"}, "github-token"},
		{map[string]any{"workflow": "summarize"}, "workflow"},
		{map[string]any{"max-depth": 2}, "max-depth"},
	} {
		config := parse(entry.fields)
		require.Error(t, config.parseError, "%v", entry.fields)
		require.Contains(t, config.parseError.Error(), "safe-outputs.request-continuation."+entry.field, "%v", entry.fields)
	}
	// Advisory forms keep PR behavior.
	for _, fields := range []map[string]any{{}, {"max": 3}, {"max": "${{ inputs.max }}"}} {
		config := parse(fields)
		require.NoError(t, config.parseError)
		require.Empty(t, config.Route)
	}
}

func TestRequestContinuationRouteRoles(t *testing.T) {
	c := NewCompiler()
	worker := map[string]any{"work-queue": map[string]any{"worker": true}}
	dispatcher := map[string]any{"work-queue": true}
	for _, entry := range []struct {
		route   map[string]any
		tools   map[string]any
		message string
	}{
		{map[string]any{"route": "workqueue"}, nil, "requires an assignment-bound work-queue worker"},
		{map[string]any{"route": "issue"}, dispatcher, "requires an assignment-bound work-queue worker"},
		{map[string]any{"route": "dispatch", "workflow": "summarize"}, worker, "route: dispatch requires an ordinary workflow"},
		{map[string]any{"route": "dispatch", "workflow": "summarize"}, dispatcher, "route: dispatch requires an ordinary workflow"},
	} {
		data := &WorkflowData{Tools: entry.tools, SafeOutputs: &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": entry.route})}}
		err := c.validateRequestContinuationRoute(data, "/repo/.github/workflows/source.md")
		require.ErrorContains(t, err, entry.message)
		require.ErrorContains(t, err, "safe-outputs.request-continuation.route")
	}
	for _, route := range []string{"workqueue", "issue"} {
		data := &WorkflowData{Tools: worker, SafeOutputs: &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": map[string]any{"route": route}})}}
		require.NoError(t, c.validateRequestContinuationRoute(data, "/repo/.github/workflows/worker.md"))
	}
	authorized := &SafeOutputsConfig{RequestContinuation: &RequestContinuationConfig{Route: "dispatch", BaseSafeOutputConfig: BaseSafeOutputConfig{Authorization: &SafeOutputAuthorization{}}}}
	require.ErrorContains(t, validateRequestContinuationAuthorization(authorized), "safe-outputs.request-continuation.authorization")
}

func writeContinuationWorkflow(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestRequestContinuationDispatchTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	source := filepath.Join(dir, "source.md")
	writeContinuationWorkflow(t, dir, "source.md", "---\non: push\n---\nsource\n")
	writeContinuationWorkflow(t, dir, "summarize.md", "---\non:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n        required: false\n---\nsummarize\n")
	writeContinuationWorkflow(t, dir, "plain.yml", "on:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n      mode:\n        required: true\n        default: fast\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "missing-input.yml", "on:\n  workflow_dispatch:\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "push-only.yml", "on: push\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "required.yml", "on:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n      target:\n        required: true\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "defaulted.yml", "on:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n        default: '{}'\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "numeric.yml", "on:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: number\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	writeContinuationWorkflow(t, dir, "queue-worker.md", "---\non:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\ntools:\n  work-queue:\n    worker: true\n---\nworker\n")
	writeContinuationWorkflow(t, dir, "assignment.yml", "on:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n      work_queue_assignment:\n        type: string\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")
	// A stale compiled lock without the input hides a source that declares it.
	writeContinuationWorkflow(t, dir, "stale.md", "---\non:\n  workflow_dispatch:\n    inputs:\n      gh_aw_continuation:\n        type: string\n---\nstale\n")
	writeContinuationWorkflow(t, dir, "stale.lock.yml", "on:\n  workflow_dispatch:\n    inputs:\n      aw_context:\n        type: string\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n")

	file, awContext, err := resolveRequestContinuationDispatchTarget("summarize", source)
	require.NoError(t, err)
	require.Equal(t, "summarize.lock.yml", file)
	require.True(t, awContext)
	file, awContext, err = resolveRequestContinuationDispatchTarget("plain", source)
	require.NoError(t, err)
	require.Equal(t, "plain.yml", file)
	require.False(t, awContext)

	for name, message := range map[string]string{
		"source":        "is this workflow",
		"absent":        "not found",
		"missing-input": `must declare a workflow_dispatch string input "gh_aw_continuation"`,
		"push-only":     "does not support workflow_dispatch",
		"required":      `requires input "target" without a default`,
		"defaulted":     "nonempty default",
		"numeric":       "type: string",
		"queue-worker":  "work-queue workflow",
		"assignment":    "queue workers cannot be dispatch continuation targets",
		"stale":         "compiled stale.lock.yml",
	} {
		_, _, err := resolveRequestContinuationDispatchTarget(name, source)
		require.ErrorContains(t, err, message, name)
		require.ErrorContains(t, err, "safe-outputs.request-continuation.workflow", name)
	}

	c := NewCompiler()
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": map[string]any{"route": "dispatch", "workflow": "summarize", "max-depth": 2}})}}
	require.NoError(t, c.validateRequestContinuationRoute(data, source))
	require.Equal(t, map[string]any{"max": 1, "route": "dispatch", "max_depth": 2, "workflow": "summarize", "workflow_file": "summarize.lock.yml", "aw_context": true}, diagnosticHandlerRegistry["request_continuation"](data.SafeOutputs))
}

func TestRequestContinuationRouteRuntimeConfig(t *testing.T) {
	c := NewCompiler()
	routed := func(fields map[string]any) *SafeOutputsConfig {
		return &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": fields})}
	}
	descriptor, ok := getSafeOutputHandlerByKey("request-continuation")
	require.True(t, ok)

	advisory := routed(map[string]any{})
	require.True(t, safeOutputHandlerBuiltin(advisory, descriptor))
	require.False(t, hasNonBuiltinSafeOutputsEnabled(advisory))
	require.Nil(t, computeRequestContinuationRoutePermissions(advisory, false))

	queue := routed(map[string]any{"route": "workqueue", "max-depth": 3})
	require.False(t, safeOutputHandlerBuiltin(queue, descriptor))
	require.True(t, hasNonBuiltinSafeOutputsEnabled(queue))
	require.Equal(t, map[string]any{"max": 1, "route": "workqueue", "max_depth": 3}, diagnosticHandlerRegistry["request_continuation"](queue))
	require.Nil(t, computeRequestContinuationRoutePermissions(queue, false))

	issue := routed(map[string]any{"route": "issue"})
	require.Equal(t, map[string]any{"max": 1, "route": "issue", "max_depth": 1}, diagnosticHandlerRegistry["request_continuation"](issue))
	level, exists := ComputePermissionsForSafeOutputs(issue).Get(PermissionIssues)
	require.True(t, exists)
	require.Equal(t, PermissionWrite, level)
	staged := TemplatableBool("true")
	issue.Staged = &staged
	_, exists = ComputePermissionsForSafeOutputs(issue).Get(PermissionIssues)
	require.False(t, exists, "staged routes only preview")

	dispatch := routed(map[string]any{"route": "dispatch", "workflow": "summarize"})
	level, exists = ComputePermissionsForSafeOutputs(dispatch).Get(PermissionActions)
	require.True(t, exists)
	require.Equal(t, PermissionWrite, level)

	// Global authorization applies to routes; only queue workers register admission.
	for _, config := range []*SafeOutputsConfig{queue, dispatch} {
		config.Authorization = &SafeOutputAuthorization{}
	}
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(&WorkflowData{SafeOutputs: queue}))
	require.NotNil(t, queue.AuthorizationRequirements["request_continuation"])
	require.Equal(t, "manager", queue.AuthorizationCoverage["request_continuation"])
	require.Equal(t, "queue", queue.AuthorizationCoverage[requestContinuationAdmitAuthorization])
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(&WorkflowData{SafeOutputs: dispatch}))
	require.NotNil(t, dispatch.AuthorizationRequirements["request_continuation"])
	require.NotContains(t, dispatch.AuthorizationCoverage, requestContinuationAdmitAuthorization)

	// Dispatchers scheduling an issue-routed worker read approval Issues without writes.
	dispatcher := &SafeOutputsConfig{workQueueApprovalReads: true}
	level, exists = ComputePermissionsForSafeOutputs(dispatcher).Get(PermissionIssues)
	require.True(t, exists)
	require.Equal(t, PermissionRead, level)
	dispatcher.AddComments = &AddCommentsConfig{}
	level, _ = ComputePermissionsForSafeOutputs(dispatcher).Get(PermissionIssues)
	require.Equal(t, PermissionWrite, level, "stronger independent grants are preserved")
}

func TestRequestContinuationAgentConfigCarriesRouteOnly(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": map[string]any{"route": "dispatch", "workflow": "summarize", "max-depth": 2}})}}
	data.SafeOutputs.RequestContinuation.workflowFile = "summarize.lock.yml"
	encoded, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	require.Contains(t, encoded, `"request_continuation":{"max":1,"route":"dispatch"}`)
	require.NotContains(t, encoded, "summarize")
}

func TestWorkQueueWorkerContinuationRoute(t *testing.T) {
	dir := t.TempDir()
	writeContinuationWorkflow(t, dir, "shared.md", "---\nsafe-outputs:\n  request-continuation:\n    route: issue\n---\n")
	writeContinuationWorkflow(t, dir, "imported.md", "---\non: workflow_dispatch\nimports:\n  - shared.md\ntools:\n  work-queue:\n    worker: true\n---\nwork\n")
	writeContinuationWorkflow(t, dir, "direct.md", "---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  request-continuation:\n    route: workqueue\n---\nwork\n")
	writeContinuationWorkflow(t, dir, "advisory.md", "---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  request-continuation: true\n---\nwork\n")
	c := NewCompiler()
	for name, route := range map[string]string{"imported": "issue", "direct": "workqueue", "advisory": ""} {
		actual, err := c.workQueueWorkerContinuationRoute(filepath.Join(dir, name+".md"))
		require.NoError(t, err, name)
		require.Equal(t, route, actual, name)
	}
}
