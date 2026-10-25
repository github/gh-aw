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
			fields := map[string]any{"run": tc.script}
			if tc.shell != "" {
				fields["shell"] = tc.shell
			}
			workflow := &actions.Workflow{Jobs: []*actions.Job{{
				ID: "check", RunsOn: "ubuntu-latest",
				Steps: []actions.Step{actions.RawStep{Fields: fields}},
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

func TestBashCasePatternsInActionsUseInheritedShell(t *testing.T) {
	script := `matched=$(case "$ref" in $pattern) echo yes ;; esac)`
	defaults := func(shell string) map[string]any {
		return map[string]any{"run": map[string]any{"shell": shell}}
	}
	for _, tc := range []struct {
		name, stepShell, workflowShell, jobShell string
		want                                     int
	}{
		{name: "workflow default", workflowShell: "python", want: 0},
		{name: "job default overrides workflow", workflowShell: "bash", jobShell: "python", want: 0},
		{name: "step shell overrides workflow", stepShell: "bash", workflowShell: "python", want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"run": script}
			if tc.stepShell != "" {
				fields["shell"] = tc.stepShell
			}
			job := &actions.Job{
				ID: "check", RunsOn: "ubuntu-latest",
				Steps: []actions.Step{actions.RawStep{Fields: fields}},
			}
			if tc.jobShell != "" {
				job.Defaults = defaults(tc.jobShell)
			}
			workflow := &actions.Workflow{Jobs: []*actions.Job{job}}
			if tc.workflowShell != "" {
				workflow.Defaults = defaults(tc.workflowShell)
			}
			runner, err := Default().Select("bash-case-pattern")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
			require.NoError(t, err)
			assert.Len(t, diagnostics, tc.want)
		})
	}
}

func TestBashCasePatternsInAWUseInheritedShell(t *testing.T) {
	runner, err := Default().Select("bash-case-pattern")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{
			name: "workflow default",
			source: `---
defaults:
  run:
    shell: python
steps:
  - run: |
      matched=$(case "$ref" in $pattern) echo yes ;; esac)
---
Prompt
`,
			want: 0,
		},
		{
			name: "job default overrides workflow default",
			source: `---
defaults:
  run:
    shell: bash
jobs:
  check:
    defaults:
      run:
        shell: python
    steps:
      - run: |
          matched=$(case "$ref" in $pattern) echo yes ;; esac)
---
Prompt
`,
			want: 0,
		},
		{
			name: "step shell overrides workflow default",
			source: `---
defaults:
  run:
    shell: python
jobs:
  check:
    steps:
      - shell: bash
        run: |
          matched=$(case "$ref" in $pattern) echo yes ;; esac)
---
Prompt
`,
			want: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := aw.Parse(tc.source)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: document})
			require.NoError(t, err)
			assert.Len(t, diagnostics, tc.want)
		})
	}
}
