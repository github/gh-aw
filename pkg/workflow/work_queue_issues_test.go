package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueIssuesProjectorAppTokensStayInHooks(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{GitHubApp: &GitHubAppConfig{
		AppID: "${{ secrets.PROJECTOR_APP_ID }}", PrivateKey: "${{ secrets.PROJECTOR_APP_KEY }}",
	}}}
	for _, job := range []string{"activation", "conclusion"} {
		steps := compiler.workQueueProjectorTokenSteps(data, job)
		var document map[string]any
		require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+strings.Join(steps, "")), &document))
		minted := document["steps"].([]any)[0].(map[string]any)
		require.Equal(t, "write", minted["with"].(map[string]any)["permission-issues"])
		if job == "conclusion" {
			require.Equal(t, "always()", minted["if"])
		}
		steps = append(steps, "        with:\n")
		compiler.addWorkQueueProjectorToken(&steps, data)
		result := strings.Join(steps, "")
		require.Contains(t, result, "id: work-queue-projector-token")
		require.Contains(t, result, "permission-contents: write")
		require.Contains(t, result, "permission-issues: write")
		require.Contains(t, result, "permission-actions: read")
		require.Contains(t, result, "github-token: ${{ steps.work-queue-projector-token.outputs.token }}")
		require.NotContains(t, result, "steps.safe-outputs-app-token.outputs.token")
		if job == "conclusion" {
			require.Contains(t, result, "if: always()")
		}
	}
}

func TestWorkQueueIssuesConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		valid bool
		label string
	}{
		{"true", true, true, "work"},
		{"false", false, true, ""},
		{"object", map[string]any{"label": "cookie"}, true, "cookie"},
		{"maximum-label-prefix", map[string]any{"label": strings.Repeat("x", 33)}, true, strings.Repeat("x", 33)},
		{"empty-object", map[string]any{}, true, "work"},
		{"blank-label", map[string]any{"label": " "}, false, ""},
		{"label-prefix-too-long", map[string]any{"label": strings.Repeat("x", 34)}, false, ""},
		{"status-field", map[string]any{"status-field": "CustomStatus"}, false, ""},
		{"unknown", map[string]any{"storage": "issues"}, false, ""},
		{"null", nil, false, ""},
		{"expression", map[string]any{"label": "${{ inputs.label }}"}, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true, "issues": test.value}}}
			if !test.valid {
				require.Error(t, validateWorkQueueIssuesConfig(data))
				return
			}
			require.NoError(t, validateWorkQueueIssuesConfig(data))
			config := workQueueIssuesConfig(data)
			if test.label == "" {
				require.Nil(t, config)
			} else {
				require.Equal(t, test.label, config.Label)
			}
		})
	}
}

func TestWorkQueueIssuesCompilationUsesExistingJobs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "worker.md")
	compile := func(issues string, trial bool) map[string]any {
		source := "---\non: workflow_dispatch\nengine: claude\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  noop:\n---\nProcess Claims.\n"
		require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755))
		config := "{}"
		if issues != "" {
			config = `{"work_queue":{"issues":` + issues + `}}`
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, RepoConfigFileName), []byte(config), 0o600))
		compiler := NewCompiler(WithVersion("integration"))
		compiler.gitRoot = dir
		compiler.SetApprove(true)
		compiler.SetTrialMode(trial)
		require.NoError(t, compiler.CompileWorkflow(file))
		data, err := os.ReadFile(filepath.Join(dir, "worker.lock.yml"))
		require.NoError(t, err)
		var compiled map[string]any
		require.NoError(t, yaml.Unmarshal(data, &compiled))
		return compiled
	}
	baseline := compile("", false)
	enabled := compile(`{"label":"cookie"}`, false)
	baseJobs := baseline["jobs"].(map[string]any)
	jobs := enabled["jobs"].(map[string]any)
	require.Len(t, jobs, len(baseJobs))
	for name := range baseJobs {
		require.Contains(t, jobs, name)
	}
	require.Equal(t, baseline["on"], enabled["on"])
	activation := jobs["activation"].(map[string]any)
	conclusion := jobs["conclusion"].(map[string]any)
	require.Equal(t, "always()", conclusion["if"])
	for _, job := range []map[string]any{activation, conclusion} {
		perms := job["permissions"].(map[string]any)
		require.Equal(t, "write", perms["contents"])
		require.Equal(t, "write", perms["issues"])
		require.Equal(t, "read", perms["actions"])
	}
	agent := jobs["agent"].(map[string]any)
	perms := agent["permissions"].(map[string]any)
	require.NotEqual(t, "write", perms["issues"])
	require.NotEqual(t, "write", perms["contents"])
	seen := false
	for _, value := range conclusion["steps"].([]any) {
		step := value.(map[string]any)
		if step["id"] == "work_queue_issues" {
			require.Equal(t, "always()", step["if"])
			env := step["env"].(map[string]any)
			require.JSONEq(t, `{"label":"cookie"}`, env["GH_AW_WORK_QUEUE_ISSUES"].(string))
			seen = true
		}
	}
	require.True(t, seen)
	trial := compile("true", true)
	for _, name := range []string{"activation", "conclusion"} {
		for _, value := range trial["jobs"].(map[string]any)[name].(map[string]any)["steps"].([]any) {
			step := value.(map[string]any)
			if step["id"] == "work_queue_snapshot" || step["id"] == "work_queue_issues" {
				require.Equal(t, "true", step["env"].(map[string]any)["GH_AW_SAFE_OUTPUTS_STAGED"])
			}
		}
	}
}
