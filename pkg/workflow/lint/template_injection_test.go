package lint

import (
	"context"
	"fmt"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func templateInjectionWorkflow(script string, typed bool) *actions.Workflow {
	var step actions.Step = actions.RawStep{Fields: map[string]any{"run": script}}
	if typed {
		step = actions.RunStep{Run: script}
	}
	return &actions.Workflow{Jobs: []*actions.Job{{ID: "test", RunsOn: "ubuntu-latest", Steps: []actions.Step{step}}}}
}

func runTemplateInjection(t *testing.T, workflow *actions.Workflow) []Diagnostic {
	t.Helper()
	runner, err := New(TemplateInjectionRule())
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	return diagnostics
}

func TestTemplateInjectionSources(t *testing.T) {
	for _, expr := range []string{
		"github.event.issue.title", "github.event.issue.body", "github.event.issue.user.login",
		"github.event.pull_request.title", "github.event.pull_request.body", "github.event.pull_request.user.login",
		"github.event.comment.body", "github.event.comment.user.login",
		"github.event.review.body", "github.event.review.user.login",
		"github.event.review_comment.body", "github.event.review_comment.user.login",
		"github.actor", "github.triggering_actor", "github.head_ref", "github.ref", "github.ref_name",
		"github.event.pull_request.head.ref", "github.event.pull_request.head.label",
		"github.event.pull_request.head.repo.default_branch",
		"github.event.label.name", "github.event.issue.labels[0].name",
		"github.event.pages[0].page_name",
		"join(github.event.pull_request.labels.*.name, ',')",
		"github.event.head_commit.message", "github.event.commits[0].author.name",
		"github.event.workflow_run.head_branch", "github.event.discussion.body",
		"github.event.milestone.description", "github.event.release.tag_name",
		"inputs.script", "github.event.inputs.script", "github.event.client_payload.command",
		"GiTHub['EVENT']['ISSUE']['TITLE']",
		"github.event['pull_request'].head['ref']",
		"github.event.issue[inputs.field]",
		"github.event.issue[env.FIELD]",
		"format('wrapped {0}', github.event.issue.title)",
		"toJSON(github.event.comment.body)",
		"fromJSON(inputs.payload).command",
		"format('it''s }} {0}', github.event.issue.body)",
		"format('${{literal}} {0}', github.event.issue.body)",
		"github.event.issue.title || 'fallback'",
		"contains(github.event.issue.body, 'x') && github.event.issue.title || 'safe'",
		"toJSON(github.event.issue)", "toJSON(github.event)", "toJSON(github)",
	} {
		t.Run(expr, func(t *testing.T) {
			for _, typed := range []bool{false, true} {
				diagnostics := runTemplateInjection(t, templateInjectionWorkflow("echo '${{ "+expr+" }}'", typed))
				require.Len(t, diagnostics, 1)
				diagnostic := diagnostics[0]
				assert.Equal(t, "template-injection", diagnostic.RuleID)
				assert.Equal(t, Error, diagnostic.Severity)
				assert.Equal(t, "jobs.test.steps[0].run", diagnostic.Path)
				assert.Equal(t, 1, diagnostic.Line)
				assert.Contains(t, diagnostic.Message, "bind it through env")
				assert.NotEmpty(t, diagnostic.Related)
				assert.NotContains(t, diagnostic.Message, expr)
			}
		})
	}
}

func TestTemplateInjectionSafeExpressions(t *testing.T) {
	for _, expr := range []string{
		"github.event.issue.number", "github.event.issue.id",
		"github.event.pull_request.number", "github.event.pull_request.head.sha",
		"github.event.comment.id", "github.event.repository.full_name",
		"github.repository", "github.sha",
		"github.run_id", "github.run_number", "github.event_name",
		"github.event.workflow_run.id", "github.event.workflow_run.head_sha",
		"steps.compiler.outputs.validated", "needs.activation.outputs.ref",
		"vars.SETTING", "runner.os", "matrix.os", "secrets.TOKEN",
		"env.TRUSTED", "42", "true", "null",
		"'github.event.issue.title'", "'inputs.command'",
		"github.event.issue.title == 'expected'",
		"github.event.issue.title != ''",
		"github.event.issue.number > 0",
		"!github.event.issue.title",
		"!!inputs.flag",
		"contains(github.event.issue.title, 'PRIVATE_LITERAL')",
		"startsWith(github.head_ref, 'feature/')",
		"ENDSWith(github.event.comment.body, 'suffix')",
		"contains(inputs.body, 'x') && 'yes' || 'no'",
		"format('{0}', github.event.issue.title == 'x')",
		"toJSON(contains(inputs.text, 'x'))",
		"always()", "success()", "cancelled()", "failure()",
	} {
		t.Run(expr, func(t *testing.T) {
			assert.Empty(t, runTemplateInjection(t, templateInjectionWorkflow("echo ${{ "+expr+" }}", false)))
		})
	}
}

func TestTemplateInjectionAllExecutableShellsAndQuoting(t *testing.T) {
	for _, shell := range []string{"", "bash", "sh", "zsh", "pwsh", "powershell", "cmd", "python", "bash --noprofile --norc -e -o pipefail {0}", "custom-interpreter {0}"} {
		for _, script := range []string{
			"echo \"${{ github.event.issue.title }}\"",
			"echo '${{ github.event.issue.title }}'",
			"cat <<'DATA'\n${{ github.event.issue.title }}\nDATA",
			"# ${{ github.event.issue.title }}",
			"print(r'''${{ github.event.issue.title }}''')",
		} {
			t.Run(shell+"/"+script, func(t *testing.T) {
				for _, typed := range []bool{false, true} {
					workflow := templateInjectionWorkflow(script, typed)
					if typed {
						workflow.Jobs[0].Steps[0] = actions.RunStep{Run: script, Shell: shell}
					} else {
						workflow.Jobs[0].Steps[0] = actions.RawStep{Fields: map[string]any{"run": script, "shell": shell}}
					}
					diagnostics := runTemplateInjection(t, workflow)
					require.Len(t, diagnostics, 1)
					if script[0] == 'c' {
						assert.Equal(t, 2, diagnostics[0].Line)
					} else {
						assert.Equal(t, 1, diagnostics[0].Line)
					}
				}
			})
		}
	}
}

func TestTemplateInjectionEnvironmentIndirection(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
env:
  TITLE: ${{ github.event.issue.title }}
jobs:
  test:
    runs-on: ubuntu-latest
    env:
      BODY: ${{ github.event.comment.body }}
      ALIAS: ${{ env.TITLE }}
      LITERAL_NUMBER: 300
      LITERAL_BOOLEAN: true
    steps:
      - env:
          INPUT: ${{ inputs.text }}
          LITERAL_NUMBER: 1
          LITERAL_BOOLEAN: false
        run: |
          printf '%s\n' "$TITLE" "$BODY" "$ALIAS" "$INPUT"
      - shell: pwsh
        run: Write-Output "$env:TITLE"
      - shell: cmd
        run: echo "%TITLE%"
      - run: echo "${{ env.TITLE }}"
      - env:
          TITLE: fixed
        run: echo "${{ env.TITLE }}"
      - env:
          TITLE: ${{ env.BODY }}
        run: echo "${{ env.TITLE }}"
      - env:
          TITLE: ${{ contains(inputs.title, 'x') }}
        run: echo "${{ env.TITLE }}"
      - env:
          TITLE: fixed
        run: echo "${{ env.ALIAS }}"
      - uses: actions/github-script@v7
        with:
          script: ${{ github.event.issue.body }}
`))
	require.NoError(t, err)
	diagnostics := runTemplateInjection(t, workflow)
	require.Len(t, diagnostics, 4)
	assert.Equal(t, "jobs.test.steps[3].run", diagnostics[0].Path)
	assert.Equal(t, "env.TITLE", diagnostics[0].Related[0].Path)
	assert.Equal(t, "jobs.test.steps[5].run", diagnostics[1].Path)
	assert.Equal(t, "jobs.test.env.BODY", diagnostics[1].Related[0].Path)
	assert.Equal(t, "jobs.test.steps[7].run", diagnostics[2].Path)
	assert.Equal(t, "jobs.test.steps[8].with.script", diagnostics[3].Path)

	typed := templateInjectionWorkflow("echo \"$TITLE\"", true)
	typed.Env = map[string]actions.Expression{"TITLE": actions.Context("github.event.issue.title")}
	typed.Jobs[0].Env = map[string]actions.Expression{"ALIAS": actions.Context("env.TITLE")}
	typed.Jobs[0].Steps = []actions.Step{actions.RunStep{
		StepOptions: actions.StepOptions{Env: map[string]actions.Expression{"LOCAL": actions.Context("env.ALIAS")}},
		Run:         "echo \"$LOCAL\"\necho '${{ env.LOCAL }}'",
	}}
	diagnostics = runTemplateInjection(t, typed)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, 2, diagnostics[0].Line)
	assert.Equal(t, "env.TITLE", diagnostics[0].Related[0].Path)
}

func TestTemplateInjectionParseGapsAndPartialFindings(t *testing.T) {
	for _, script := range []string{
		"echo ${{ github.event.issue.title }}\necho ${{ inputs.PRIVATE_VALUE",
		"echo ${{ github.event.issue.title }}\necho ${{ format('PRIVATE_VALUE', inputs.body) + }}",
	} {
		diagnostics := runTemplateInjection(t, templateInjectionWorkflow(script, false))
		require.Len(t, diagnostics, 2)
		assert.Equal(t, Error, diagnostics[0].Severity)
		assert.Equal(t, 1, diagnostics[0].Line)
		assert.Equal(t, Warning, diagnostics[1].Severity)
		assert.Equal(t, 2, diagnostics[1].Line)
		assert.Contains(t, diagnostics[1].Message, "Incomplete")
		assert.NotContains(t, fmt.Sprint(diagnostics), "PRIVATE_VALUE")
	}
	workflow := templateInjectionWorkflow(`echo "$TITLE"`, false)
	workflow.Env = map[string]actions.Expression{"TITLE": actions.Literal("${{ inputs.PRIVATE_VALUE")}
	diagnostics := runTemplateInjection(t, workflow)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, Warning, diagnostics[0].Severity)
	assert.Equal(t, "env.TITLE", diagnostics[0].Path)
	assert.NotContains(t, fmt.Sprint(diagnostics), "PRIVATE_VALUE")
}

func TestTemplateInjectionDiagnosticsDoNotExposeExpressionValues(t *testing.T) {
	workflow := templateInjectionWorkflow("echo ${{ format('PRIVATE_LITERAL', inputs['PRIVATE_KEY']) }}", false)
	diagnostics := runTemplateInjection(t, workflow)
	require.Len(t, diagnostics, 1)
	assert.NotContains(t, fmt.Sprint(diagnostics), "PRIVATE")
	assert.NotContains(t, fmt.Sprint(diagnostics), "private")
}

func TestTemplateInjectionDeterministicReadOnlyAndLines(t *testing.T) {
	script := "prefix\n${{\n github.event.issue.title\n }} ${{ inputs.text }}\n${{ github.event.comment.body }}"
	workflow := templateInjectionWorkflow(script, false)
	workflow.Jobs = append(workflow.Jobs, &actions.Job{
		ID: "alpha", RunsOn: "ubuntu-latest",
		Steps: []actions.Step{actions.RawStep{Fields: map[string]any{"run": "${{ inputs.text }}"}}},
	})
	original := workflow.Jobs[0]
	first := runTemplateInjection(t, workflow)
	require.Len(t, first, 4)
	assert.Equal(t, "jobs.alpha.steps[0].run", first[0].Path)
	assert.Equal(t, []int{1, 2, 4, 5}, []int{first[0].Line, first[1].Line, first[2].Line, first[3].Line})
	for range 10 {
		assert.Equal(t, first, runTemplateInjection(t, workflow))
	}
	assert.Same(t, original, workflow.Jobs[0])
	assert.Equal(t, script, workflow.Jobs[0].Steps[0].(actions.RawStep).Fields["run"])
}

func TestTemplateInjectionFailuresAndCancellation(t *testing.T) {
	rule := TemplateInjectionRule()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rule.Check(ctx, Input{Workflow: templateInjectionWorkflow("echo ok", false)})
	require.ErrorIs(t, err, context.Canceled)
	_, err = rule.Check(context.Background(), Input{})
	require.ErrorContains(t, err, "Actions AST")
	workflow := templateInjectionWorkflow("echo ok", false)
	workflow.Jobs[0].Steps[0] = actions.RawStep{Fields: map[string]any{"run": 42}}
	_, err = rule.Check(context.Background(), Input{Workflow: workflow})
	require.ErrorContains(t, err, "run script must be a string")
	workflow.Jobs[0].Steps[0] = actions.RawStep{Fields: map[string]any{"run": "echo ok", "env": "PRIVATE_VALUE"}}
	_, err = rule.Check(context.Background(), Input{Workflow: workflow})
	require.ErrorContains(t, err, "environment must be a mapping")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
}
