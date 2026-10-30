//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractDecision(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		valid bool
	}{
		{"default", map[string]any{"questions": []any{"Should we run?"}}, true},
		{"model", map[string]any{"model": "mini", "questions": []any{"Should we run?"}}, true},
		{"absent questions", map[string]any{}, false},
		{"null", nil, false},
		{"blank", map[string]any{"questions": []any{" "}}, false},
		{"bytes not characters", map[string]any{"questions": []any{strings.Repeat("é", 2049)}}, false},
		{"filespec", map[string]any{"questions": []any{"\n @file.md"}}, false},
		{"expression model", map[string]any{"model": "${{ secrets.MODEL }}", "questions": []any{"a"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config, err := extractDecision(map[string]any{"on": map[string]any{"decision": tt.value}})
			if tt.valid {
				require.NoError(t, err)
				require.NotNil(t, config)
				require.NotEmpty(t, config.Model)
			} else {
				require.Error(t, err)
			}
		})
	}
	config, err := extractDecision(map[string]any{"on": "push"})
	require.NoError(t, err)
	require.Nil(t, config)
}

func TestDecisionCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "decision")
	source := `---
on:
  workflow_dispatch:
  roles: all
  decision:
    questions:
      - "Is ${{ github.event.issue.title }} actionable?"
  steps:
    - id: custom_check
      run: echo ok
engine: copilot
---
Perform the requested work.
`
	file := filepath.Join(dir, "decision.md")
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	lock, err := os.ReadFile(filepath.Join(dir, "decision.lock.yml"))
	require.NoError(t, err)
	text := string(lock)
	assert.Contains(t, text, "pre_activation:")
	assert.Contains(t, text, "GH_AW_DECISION_MODEL: \"decision\"")
	assert.Contains(t, text, `GH_AW_DECISION_QUESTION_0: "Is ${{ github.event.issue.title }} actionable?"`)
	assert.Contains(t, text, "steps.decision_gate.outputs.decision_result != 'skip'")
	assert.Less(t, strings.Index(text, "id: custom_check"), strings.Index(text, "id: decision_gate"))
	graph, err := actions.Parse(lock)
	require.NoError(t, err)
	require.NoError(t, validateDecisionQuestions(&WorkflowData{Decision: &DecisionConfig{Questions: []string{"safe"}}}, graph, file))
	assert.NotContains(t, text, "\n  decision:\n")
}

func TestDecisionCannotOverrideDeterministicChecks(t *testing.T) {
	data := &WorkflowData{
		Decision: &DecisionConfig{Model: "decision", Questions: []string{"Run?"}},
		SkipBots: []string{"dependabot"},
	}
	job, err := NewCompiler().buildPreActivationJob(data, false)
	require.NoError(t, err)
	text := strings.Join(job.Steps, "")
	assert.Contains(t, text, "GH_AW_DECISION_ELIGIBLE:")
	assert.Contains(t, text, "steps.check_skip_bots.outputs.skip_bots_ok == 'true'")
	assert.Contains(t, job.Outputs["activated"], "steps.check_skip_bots.outputs.skip_bots_ok == 'true'")
	assert.Contains(t, job.Outputs["activated"], "steps.decision_gate.outputs.decision_result != 'skip'")
	outputs := buildPreActivationJobOutputs(data, "protected", nil, map[string]string{"activated": "'true'"})
	assert.Equal(t, "protected", outputs["activated"])
}

func TestDecisionOnlyCreatesPreActivation(t *testing.T) {
	dir := testutil.TempDir(t, "decision-only")
	file := filepath.Join(dir, "decision.md")
	source := "---\non:\n  workflow_dispatch:\n  roles: all\n  decision:\n    questions: [Run?]\nengine: copilot\n---\nPerform the work.\n"
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	lock, err := os.ReadFile(filepath.Join(dir, "decision.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "pre_activation:")
	assert.Contains(t, string(lock), "steps.decision_gate.outputs.decision_result != 'skip'")
}

func TestDecisionWorkflowRunSafety(t *testing.T) {
	data := &WorkflowData{Decision: &DecisionConfig{Model: "decision", Questions: []string{"Run?"}}}
	compiler := NewCompiler()
	job, err := compiler.buildPreActivationJob(data, false)
	require.NoError(t, err)
	text := strings.Join(job.Steps, "")
	assert.Contains(t, text, "GH_AW_DECISION_ELIGIBLE:")
	assert.Contains(t, text, "github.event.workflow_run.repository.id == github.repository_id")
	assert.Contains(t, text, "!(github.event.workflow_run.repository.fork)")
	assert.Contains(t, job.Outputs["activated"], "github.event.workflow_run.repository.id == github.repository_id")
	assert.Contains(t, job.Outputs["activated"], "!(github.event.workflow_run.repository.fork)")

	_, err = decisionEligibleCondition(&WorkflowData{If: "needs.pre_activation.outputs.loop == 'yes'"},
		BuildStringLiteral("true"), nil, map[string]string{"loop": "${{ needs.pre_activation.outputs.loop }}"})
	require.ErrorContains(t, err, "cyclic")
}

func TestDecisionEligibleCustomPredicates(t *testing.T) {
	for _, tt := range []struct {
		name      string
		condition string
		needs     []string
		want      string
		wantErr   bool
	}{
		{"on steps", "needs.pre_activation.outputs.gate_result == 'success'", nil, "(steps.gate.outcome) == 'success'", false},
		{"declared dependency", "needs.check.outputs.ok == 'yes'", []string{"check"}, "needs.check.outputs.ok == 'yes'", false},
		{"unavailable dependency", "needs.check.outputs.ok == 'yes'", nil, "", true},
		{"unavailable self output", "needs.pre_activation.outputs.unknown == 'yes'", nil, "", true},
		{"false", "false", nil, "false", false},
		{"literal is not rewritten", "contains('needs.pre_activation.outputs.gate_result', 'x')", nil, "contains('needs.pre_activation.outputs.gate_result', 'x')", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{If: tt.condition, OnNeeds: tt.needs}
			condition, err := decisionEligibleCondition(data, BuildStringLiteral("true"), []string{"gate"}, nil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, condition.Render())
		})
	}
}

func TestDecisionCompilationRejectsAllSecretsWithoutLint(t *testing.T) {
	dir := testutil.TempDir(t, "decision-secret")
	file := filepath.Join(dir, "decision.md")
	source := `---
on:
  workflow_dispatch:
  roles: all
  decision:
    questions:
      - "private literal ${{ secrets.PRIVATE_ONE }}"
      - "${{ env.ALIAS }}"
env:
  ALIAS: ${{ secrets.PRIVATE_TWO }}
engine: copilot
---
Perform the work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	err := NewCompiler(WithLint(false)).CompileWorkflow(file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "on.decision.questions[0]")
	assert.Contains(t, err.Error(), "on.decision.questions[1]")
	assert.Contains(t, err.Error(), file+":7:")
	assert.Contains(t, err.Error(), file+":8:")
	assert.NotContains(t, err.Error(), "private literal")
	assert.NotContains(t, err.Error(), "PRIVATE_ONE")
	assert.NotContains(t, err.Error(), "PRIVATE_TWO")
}

func TestDecisionQuestionSecretAnalysis(t *testing.T) {
	for _, expression := range []string{
		"${{ secrets.SENSITIVE }}",
		"${{ toJSON(secrets) }}",
		"${{ format('{0}', secrets['SENSITIVE']) }}",
		"${{ secrets.SENSITIVE != '' }}",
		"${{ github.token }}",
		"${{ toJSON(github) }}",
		"${{ env.ALIAS }}",
		"${{ steps.alias.outputs.value }}",
		"${{ needs.previous.outputs.value }}",
	} {
		t.Run(expression, func(t *testing.T) {
			source := `on: workflow_dispatch
env:
  ALIAS: ${{ secrets.SENSITIVE }}
jobs:
  previous:
    runs-on: ubuntu-latest
    outputs:
      value: ${{ steps.alias.outputs.value }}
    steps:
      - id: alias
        run: echo "value=${{ secrets.SENSITIVE }}" >> "$GITHUB_OUTPUT"
  pre_activation:
    runs-on: ubuntu-latest
    needs: previous
    steps:
      - id: alias
        run: echo "value=${{ secrets.SENSITIVE }}" >> "$GITHUB_OUTPUT"
      - id: decision_gate
        uses: actions/github-script@v8
        env:
          GH_AW_DECISION_QUESTION_0: ` + expression + `
          GH_AW_DECISION_QUESTION_1: ${{ secrets.SECOND }}
        with:
          script: return;
`
			graph, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			data := &WorkflowData{Decision: &DecisionConfig{Questions: []string{expression, "${{ secrets.SECOND }}"}}}
			err = NewCompiler(WithLint(false)).lintCompiledWorkflow(data, source, "decision.md")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "on.decision.questions[0]")
			assert.Contains(t, err.Error(), "on.decision.questions[1]")
			assert.NotContains(t, err.Error(), "SENSITIVE")
			assert.NotContains(t, err.Error(), "SECOND")
			require.Error(t, validateDecisionQuestions(data, graph, "decision.md"))
		})
	}
}

func TestDecisionOpaqueActionCredentialOutput(t *testing.T) {
	for _, step := range []string{
		`      - id: alias
        uses: actions/github-script@v8
        env:
          PRIVATE: ${{ secrets.TOKEN }}
        with:
          script: core.setOutput("value", process.env.PRIVATE)
`,
		`      - id: alias
        uses: actions/create-github-app-token@v2
        with:
          app-id: "1"
          private-key: ${{ secrets.KEY }}
`,
	} {
		source := `on: workflow_dispatch
jobs:
  pre_activation:
    runs-on: ubuntu-latest
    steps:
` + step + `      - id: decision_gate
        uses: actions/github-script@v8
        env:
          GH_AW_DECISION_QUESTION_0: ${{ steps.alias.outputs.value }}
        with:
          script: return;
`
		data := &WorkflowData{Decision: &DecisionConfig{Questions: []string{"${{ steps.alias.outputs.value }}"}}}
		err := NewCompiler(WithLint(false)).lintCompiledWorkflow(data, source, "decision.md")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "on.decision.questions[0]")
		assert.NotContains(t, err.Error(), "TOKEN")
	}
}
