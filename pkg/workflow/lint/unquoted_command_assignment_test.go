package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnquotedCommandAssignment(t *testing.T) {
	for _, tt := range []struct {
		name, script, shell string
		lines               []int
	}{
		{"bare command", "QUERY_COMMAND=pr", "bash", []int{1}},
		{"multiple commands", "x=ls\ny=date z=grep", "sh", []int{1, 2}},
		{"nested and prefix", "if true; then\nx=pr echo ok\nfi", "bash --noprofile -e {0}", []int{2}},
		{"quoted", "x=\"pr\"\ny='ls'\nz=\\pr", "bash", nil},
		{"other values", "x=issue\ny=not-a-command\nz=pr-suffix", "bash", nil},
		{"substitution", "x=$(pr)\ny=$value\nz=${{ inputs.value }}", "bash", nil},
		{"comments and strings", "# x=pr\necho 'x=pr'", "bash", nil},
		{"heredoc", "cat <<'EOF'\nx=pr\nEOF", "bash", nil},
		{"other interpreter", "x=pr", "python", nil},
		{"invalid shell", "if", "bash", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &actions.Workflow{Jobs: []*actions.Job{{
				ID: "check", RunsOn: "ubuntu-latest",
				Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": tt.script, "shell": tt.shell}}},
			}}}
			runner, err := Default().Select("unquoted-command-assignment")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
			require.NoError(t, err)
			require.Len(t, diagnostics, len(tt.lines))
			for i, diagnostic := range diagnostics {
				assert.Equal(t, ActionsAST, diagnostic.Source)
				assert.Equal(t, Warning, diagnostic.Severity)
				assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
				assert.Equal(t, tt.lines[i], diagnostic.Line)
				assert.Contains(t, diagnostic.Message, "SC2209")
				assert.NotContains(t, diagnostic.Message, "QUERY_COMMAND")
			}
		})
	}
}

func TestUnquotedCommandAssignmentAWAndResolvedScripts(t *testing.T) {
	document, err := aw.Parse(`---
steps:
  - run: |
      step=pr
mcp-scripts:
  query:
    run: |
      QUERY_COMMAND=pr
  javascript:
    script: const x = "pr";
---
Prompt.
`)
	require.NoError(t, err)
	runner, err := Default().Select("unquoted-command-assignment")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: document})
	require.NoError(t, err)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "mcp-scripts.query.run", diagnostics[0].Path)
	assert.Equal(t, "steps[0].run", diagnostics[1].Path)
	assert.Equal(t, AWAST, diagnostics[0].Source)

	workflow := &actions.Workflow{Jobs: []*actions.Job{{ID: "check", RunsOn: "ubuntu-latest"}}}
	diagnostics, err = runner.Run(context.Background(), Input{
		Workflow: workflow, Document: document,
		ResolvedMCPScripts: map[string]string{"imported": "QUERY_COMMAND=pr"},
	})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "mcp-scripts.imported.run", diagnostics[0].Path)
	assert.Equal(t, AWAST, diagnostics[0].Source)
}

func TestUnquotedCommandAssignmentShellDefaults(t *testing.T) {
	workflow := &actions.Workflow{
		Defaults: map[string]any{"run": map[string]any{"shell": "python"}},
		Jobs: []*actions.Job{
			{ID: "python", RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": "x=pr"}}}},
			{ID: "bash", RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": "x=pr", "shell": "bash"}}}},
			{ID: "windows", RunsOn: "windows-latest", Defaults: map[string]any{"run": map[string]any{"shell": ""}}, Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": "x=pr"}}}},
		},
	}
	diagnostics, err := checkUnquotedCommandAssignments(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.bash.steps[0].run", diagnostics[0].Path)
}
