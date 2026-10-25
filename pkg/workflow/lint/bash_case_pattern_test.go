package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashCasePatterns(t *testing.T) {
	for _, tc := range []struct {
		name, script, shell string
		want                int
	}{
		{"unbalanced variable", `matched=$(case "$ref" in $pattern) echo yes ;; esac)`, "bash", 1},
		{"balanced variable", `matched=$(case "$ref" in ($pattern) echo yes ;; esac)`, "bash", 0},
		{"literal glob", `matched=$(case "$ref" in refs/heads/*) echo yes ;; esac)`, "", 1},
		{"multiple patterns", `matched=$(case "$ref" in main|feature/*) echo yes ;; *) echo no ;; esac)`, "", 2},
		{"partially balanced", `matched=$(case "$ref" in (main) echo yes ;; *) echo no ;; esac)`, "", 1},
		{"nested substitution", `matched=$(echo "$(case "$ref" in $pattern) echo yes ;; esac)")`, "", 1},
		{"outside substitution", `case "$ref" in $pattern) echo yes ;; esac`, "", 0},
		{"quoted script", `echo '$(case "$ref" in $pattern) echo yes ;; esac)'`, "", 0},
		{"balanced with comment", "matched=$(case \"$ref\" in (\n# comment\n$pattern) echo yes ;; esac)", "", 0},
		{"unbalanced with comment", "matched=$(case \"$ref\" in\n# (\n$pattern) echo yes ;; esac)", "", 1},
		{"expression", `matched=$(case "${{ env.REF }}" in ($pattern) echo yes ;; esac)`, "", 0},
		{"other interpreter", `matched=$(case "$ref" in $pattern) echo yes ;; esac)`, "python", 0},
		{"shell invocation", `matched=$(case "$ref" in $pattern) echo yes ;; esac)`, "bash --noprofile --norc -e {0}", 1},
		{"invalid syntax", "if", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := &actions.Workflow{Jobs: []*actions.Job{{
				ID: "check", RunsOn: "ubuntu-latest",
				Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": tc.script, "shell": tc.shell}}},
			}}}
			runner, err := Default().Select("bash-case-pattern")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
			require.NoError(t, err)
			require.Len(t, diagnostics, tc.want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Warning, diagnostic.Severity)
				assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
				assert.Positive(t, diagnostic.Line)
				assert.NotContains(t, diagnostic.Message, "$ref")
				assert.NotContains(t, diagnostic.Message, "$pattern")
			}
		})
	}
}

func TestBashCasePatternsResolveInheritedShell(t *testing.T) {
	const script = `matched=$(case "$ref" in $pattern) echo yes ;; esac)`
	defaults := func(shell string) map[string]any {
		return map[string]any{"run": map[string]any{"shell": shell}}
	}
	runStep := func(shell string) actions.Step {
		fields := map[string]any{"run": script}
		if shell != "" {
			fields["shell"] = shell
		}
		return actions.RawStep{Fields: fields}
	}

	t.Run("Actions step job workflow order", func(t *testing.T) {
		workflow := &actions.Workflow{
			Defaults: defaults("python"),
			Jobs: []*actions.Job{
				{ID: "job-bash", Defaults: defaults("bash"), Steps: []actions.Step{runStep("")}},
				{ID: "step-python", Defaults: defaults("bash"), Steps: []actions.Step{runStep("python")}},
				{ID: "step-bash", Defaults: defaults("python"), Steps: []actions.Step{runStep("bash")}},
				{ID: "workflow-python", Steps: []actions.Step{runStep("")}},
			},
		}
		diagnostics, err := checkBashCasePatterns(context.Background(), Input{Workflow: workflow})
		require.NoError(t, err)
		require.Len(t, diagnostics, 2)
		assert.Equal(t, "jobs.job-bash.steps[0].run", diagnostics[0].Path)
		assert.Equal(t, "jobs.step-bash.steps[0].run", diagnostics[1].Path)
	})

	t.Run("authored AW step job workflow order", func(t *testing.T) {
		step := func(shell string) map[string]any {
			fields := map[string]any{"run": script}
			if shell != "" {
				fields["shell"] = shell
			}
			return fields
		}
		document := &aw.Document{Frontmatter: map[string]any{
			"defaults": defaults("python"),
			"steps":    []any{step("")},
			"jobs": map[string]any{
				"job-bash":        map[string]any{"defaults": defaults("bash"), "steps": []any{step("")}},
				"step-python":     map[string]any{"defaults": defaults("bash"), "steps": []any{step("python")}},
				"step-bash":       map[string]any{"defaults": defaults("python"), "steps": []any{step("bash")}},
				"workflow-python": map[string]any{"steps": []any{step("")}},
			},
		}}
		diagnostics, err := checkBashCasePatterns(context.Background(), Input{Document: document})
		require.NoError(t, err)
		require.Len(t, diagnostics, 2)
		assert.Equal(t, "jobs.job-bash.steps[0].run", diagnostics[0].Path)
		assert.Equal(t, "jobs.step-bash.steps[0].run", diagnostics[1].Path)
	})
}
