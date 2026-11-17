package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRuntimeStepPaths(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
	}{
		{"root", "path: /tmp/gh-aw\n", "path: " + runtimeDirectoryExpression + "\n"},
		{"mount", "-v /tmp/gh-aw:/tmp/gh-aw:rw", "-v " + runtimeDirectoryExpression + ":" + runtimeDirectoryExpression + ":rw"},
		{"config", `{"logs":"/tmp/gh-aw/logs","allowWrite":["/tmp/gh-aw/agent"]}`, `{"logs":"` + runtimeDirectoryExpression + `/logs","allowWrite":["` + runtimeDirectoryExpression + `/agent"]}`},
		{"artifact", "path: |\n  /tmp/gh-aw/detection/result.json\n  /tmp/gh-aw/mcp-logs/", "path: |\n  " + runtimeDirectoryExpression + "/detection/result.json\n  " + runtimeDirectoryExpression + "/mcp-logs/"},
		{"private scratch", "/tmp/gh-aw-runtime-abc /tmp/gh-aw-nvx.abc", "/tmp/gh-aw-runtime-abc /tmp/gh-aw-nvx.abc"},
		{"setup assets", "${RUNNER_TEMP}/gh-aw/actions/x.cjs", "${RUNNER_TEMP}/gh-aw/actions/x.cjs"},
		{"containers", `{"topologyAttach":["awmg-mcpg","awmg-cli-proxy"]}`, `{"topologyAttach":["awmg-mcpg-` + runtimeIdentityExpression + `","awmg-cli-proxy-` + runtimeIdentityExpression + `"]}`},
		{"scoped container", "awmg-mcpg-${GH_AW_RUNTIME_ID:?}", "awmg-mcpg-${GH_AW_RUNTIME_ID:?}"},
		{"proxy hostname", "https://awmg-cli-proxy:1234", "https://awmg-cli-proxy-" + runtimeIdentityExpression + ":1234"},
		{"conditional artifact", conditionalOTLPMetricsArtifactPath, "${{ env.GH_AW_OTEL_METRICS == 'true' && format('{0}/otel-metrics.jsonl', env.GH_AW_RUNTIME_DIR, env.GH_AW_RUNTIME_ID) || '' }}"},
		{"expression braces", "${{ '/tmp/gh-aw/{x}/it''s.json' }}", "${{ format('{0}/{{x}}/it''s.json', env.GH_AW_RUNTIME_DIR, env.GH_AW_RUNTIME_ID) }}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRuntimeStepPaths(tt.input)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got, resolveRuntimeStepPaths(got), "lowering must be idempotent")
			spans, err := actionslanguageservice.ExtractExpressions(got)
			require.NoError(t, err)
			for _, span := range spans {
				require.NoError(t, actionslanguageservice.ValidateExpression(span.Content))
			}
		})
	}
}

func TestResolveJobRuntimePaths(t *testing.T) {
	compiler := NewCompiler()
	for _, name := range []string{"activation", "agent", "detection", "conclusion", "safe_outputs", "graders"} {
		err := compiler.jobManager.AddJob(&Job{
			Name: name,
			Env:  map[string]string{"UNCHANGED": "${{ runner.temp }}/gh-aw"},
			Steps: []string{
				"      - uses: actions/upload-artifact@pin\n        with:\n          path: /tmp/gh-aw/usage/\n",
				"      - run: cat '/tmp/gh-aw/awf-config.json'\n",
			},
		})
		require.NoError(t, err)
	}
	compiler.resolveJobRuntimePaths()
	for _, job := range compiler.jobManager.GetAllJobs() {
		assert.Equal(t, "${{ runner.temp }}/gh-aw", job.Env["UNCHANGED"])
		for _, step := range job.Steps {
			assert.NotContains(t, step, "/tmp/gh-aw")
			assert.Contains(t, step, runtimeDirectoryExpression)
		}
	}
}

func TestScriptModeBootstrapOutsideRuntime(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetActionMode(ActionModeScript)
	checkout := strings.Join(compiler.generateCheckoutActionsFolder(nil), "")
	assert.Contains(t, checkout, "path: .gh-aw-actions-source")
	assert.NotContains(t, checkout, "/tmp/gh-aw")
	cleanup := compiler.generateScriptModeCleanupStep()
	assert.Contains(t, cleanup, "/gh-aw/actions/clean.sh")
	for _, suffix := range []string{"DIR", "ID", "DEV", "INO", "UID", "OWNER"} {
		assert.Contains(t, cleanup, "GH_AW_RUNTIME_"+suffix+": ${{ steps.setup.outputs.GH_AW_RUNTIME_"+suffix+" }}")
	}
}

func TestWorkQueueSnapshotUsesRuntimeDefault(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": true}}
	env := map[string]string{}
	addJobLevelWorkQueueEnvVars(env, data)
	assert.Equal(t, `"true"`, env["GH_AW_WORK_QUEUE_ENABLED"])
	assert.NotContains(t, env, "GH_AW_WORK_QUEUE_SNAPSHOT")
	job := NewCompiler().newWorkQueuePreparedAdapterJob(data, "example", 0, false)
	assert.NotContains(t, job.Env, "GH_AW_WORK_QUEUE_SNAPSHOT")
}

func TestCompiledWorkflowUsesPrivateRuntime(t *testing.T) {
	for _, mode := range []ActionMode{ActionModeRelease, ActionModeScript} {
		t.Run(string(mode), func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetActionMode(mode)
			source := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
network: defaults
tools:
  github:
    toolsets: [repos]
safe-outputs:
  add-comment:
---
Keep invocation state private.
`
			filename := filepath.Join(t.TempDir(), "runtime.md")
			data, err := compiler.ParseWorkflowString(source, filename)
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, filename)
			require.NoError(t, err)
			assert.NotRegexp(t, logicalRuntimePath, output)
			assert.Contains(t, output, runtimeDirectoryExpression+"/sandbox/firewall/audit")
			assert.Contains(t, output, runtimeDirectoryExpression+"/mcp-logs")
			assert.Contains(t, output, runtimeDirectoryExpression+"/agent")
			assert.Contains(t, output, `awmg-mcpg-'"${GH_AW_RUNTIME_ID:?}"'`)
			assert.Contains(t, output, `gh-aw.runtime-id='"${GH_AW_RUNTIME_ID:?}"'`)
			assert.Contains(t, output, "-e GH_AW_RUNTIME_DIR -e GH_AW_RUNTIME_ID")
			assert.Contains(t, output, "GH_AW_MCP_GATEWAY_PORT")
			assert.NotContains(t, output, "GH_AW_MCP_LOG_DIR: /tmp/")
			assert.Contains(t, output, "${RUNNER_TEMP}/gh-aw/actions/")
			assert.Contains(t, output, "/gh-aw/actions/run_awf_isolated.cjs\" -- ")
		})
	}
}
