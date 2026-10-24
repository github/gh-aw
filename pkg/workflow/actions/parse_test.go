package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNativeWorkflowProperties(t *testing.T) {
	source := `name: Native
run-name: "${{ github.workflow }}"
on: [push, workflow_dispatch]
permissions: read-all
env: {MESSAGE: hello}
defaults: {run: {shell: bash}}
concurrency: {group: native, cancel-in-progress: true}
cache-mode: read-only
jobs:
  setup:
    runs-on: ubuntu-latest
    permissions: {contents: read}
    timeout-minutes: "${{ fromJSON(vars.TIMEOUT) }}"
    continue-on-error: false
    environment: production
    strategy: {matrix: {os: [ubuntu-latest]}}
    concurrency: setup
    container: node:22
    services: {redis: {image: "redis:7"}}
    defaults: {run: {shell: bash}}
    outputs: {value: "${{ steps.produce.outputs.value }}"}
    steps:
      - id: produce
        run: echo test
  call:
    name: Reuse
    uses: ./.github/workflows/worker.yml
    needs: setup
    if: "${{ needs.setup.outputs.value == 'ready' }}"
    with: {message: "${{ needs.setup.outputs.value }}", enabled: false, count: 3}
    secrets: {token: "${{ secrets.API_TOKEN }}"}
`
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	require.Len(t, input.Jobs, 2)
	assert.Equal(t, "read-only", input.CacheMode)
	call, setup := input.Jobs[0], input.Jobs[1]
	assert.Equal(t, JobID("call"), call.ID)
	assert.Equal(t, []JobID{"setup"}, call.Needs)
	assert.Equal(t, Boolean(false), call.With["enabled"])
	require.IsType(t, Number(0), call.With["count"])
	assert.InDelta(t, 3, float64(call.With["count"].(Number)), 0)
	assert.Equal(t, Literal("${{ secrets.API_TOKEN }}"), call.Secrets["token"])
	assert.Equal(t, Context("fromJSON(vars.TIMEOUT)"), setup.TimeoutMinutesExpression)
	assert.Equal(t, Boolean(false), setup.ContinueOnError)
	assert.Equal(t, "production", setup.Environment)
	assert.Equal(t, "node:22", setup.Container)
	assert.NotNil(t, setup.Services)
	assert.NotNil(t, setup.Strategy)
	assert.NotNil(t, setup.Permissions)
	assert.NotNil(t, setup.Defaults)
	assert.Equal(t, "setup", setup.Concurrency)
	emitted, err := Emit(input)
	require.NoError(t, err)
	reparsed, err := Parse([]byte(emitted))
	require.NoError(t, err)
	assert.Equal(t, input, reparsed)
}

func TestParseRejectsInvalidNativeYAML(t *testing.T) {
	for _, source := range []string{
		"not: [valid",
		"jobs: {}",
		"jobs: {a: {uses: './worker.yml'}}\n---\njobs: {b: {uses: './worker.yml'}}",
		"stages: []\njobs: {build: {uses: './worker.yml'}}",
		"jobs: {build: null}",
		"jobs: {build: {uses: './worker.yml', pool: ubuntu}}",
		"jobs: {build: {uses: './worker.yml', needs: 1}}",
		"jobs: {build: {uses: './worker.yml', needs: [1]}}",
		"jobs: {build: {uses: './worker.yml', with: {input: [unsupported]}}}",
		"jobs: {build: {uses: './worker.yml', secrets: bad}}",
		"jobs: {build: {uses: './worker.yml', secrets: []}}",
		"jobs: {build: {uses: './worker.yml', if: 1}}",
		"jobs: {build: {uses: './worker.yml', if: '${{ invalid'}}}",
		"jobs: {build: {runs-on: ubuntu-latest, timeout-minutes: 1.5, steps: [{run: echo}]}}",
		"jobs: {build: {runs-on: ubuntu-latest, timeout-minutes: -1, steps: [{run: echo}]}}",
		"jobs: {build: {runs-on: ubuntu-latest, timeout-minutes: 18446744073709551615, steps: [{run: echo}]}}",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := Parse([]byte(source))
			require.Error(t, err)
		})
	}
}

func TestParseMultilineAuthoredConditions(t *testing.T) {
	condition := "contains(github.event.comment.body, '/q\n') &&\n github.event_name == 'issue_comment'"
	source := "jobs:\n  call:\n    uses: ./.github/workflows/worker.yml\n    if: " +
		"\"contains(github.event.comment.body, '/q\\n') &&\\n github.event_name == 'issue_comment'\"\n"
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	require.Len(t, input.Jobs, 1)
	rendered, err := RenderExpression(input.Jobs[0].If, "call", input, false)
	require.NoError(t, err)
	assert.Equal(t, condition, rendered, "must not flatten line breaks inside Actions string literals")
	content, err := Emit(input)
	require.NoError(t, err)
	roundTrip, err := Parse([]byte(content))
	require.NoError(t, err)
	assert.Equal(t, input, roundTrip)
	input.Jobs[0].If = Context(condition)
	_, err = Emit(input)
	require.ErrorContains(t, err, "single-line")
}

func TestParseBlockCondition(t *testing.T) {
	input, err := Parse([]byte(`jobs:
  call:
    uses: ./.github/workflows/worker.yml
    if: |-
      github.event_name == 'push' &&
      github.ref == 'refs/heads/main'
`))
	require.NoError(t, err)
	emitted, err := Emit(input)
	require.NoError(t, err)
	reparsed, err := Parse([]byte(emitted))
	require.NoError(t, err)
	assert.Equal(t, input, reparsed)
}

func TestParseNativeLiteralsAndInheritedSecrets(t *testing.T) {
	input, err := Parse([]byte(`env: {COUNT: 3, ENABLED: true, EMPTY: null}
jobs:
  call:
    uses: ./.github/workflows/worker.yml
    secrets: inherit
    with: {empty: null, negative: -2, fractional: 0.5}
`))
	require.NoError(t, err)
	require.Len(t, input.Jobs, 1)
	assert.True(t, input.Jobs[0].SecretsInherit)
	assert.Equal(t, Context("null"), input.Jobs[0].With["empty"])
	for field, expected := range map[string]float64{"negative": -2, "fractional": 0.5} {
		value := input.Jobs[0].With[field]
		require.IsType(t, Number(0), value)
		assert.InDelta(t, expected, float64(value.(Number)), 0)
	}
}

func TestExpressionTimeouts(t *testing.T) {
	input := outputWorkflow()
	ref := OutputRef{Job: "setup", Step: "produce", Name: "value"}
	input.Jobs[1].TimeoutMinutesExpression = Call{Name: "fromJSON", Args: []Expression{ref}}
	input.Jobs[1].Steps = []Step{RunStep{
		StepOptions: StepOptions{TimeoutMinutesExpression: Context("fromJSON(vars.STEP_TIMEOUT)")},
		Run:         "echo test",
	}}
	content, err := Emit(input)
	require.NoError(t, err)
	assert.Contains(t, content, "timeout-minutes: ${{ fromJSON(needs.setup.outputs.aw_7_produce_value) }}")
	assert.Contains(t, content, "timeout-minutes: ${{ fromJSON(vars.STEP_TIMEOUT) }}")
	input.Jobs[1].TimeoutMinutes = 5
	_, err = Emit(input)
	require.ErrorContains(t, err, "literal and expression timeout-minutes")
	input.Jobs[1].TimeoutMinutes = 0
	input.Jobs[1].Steps = []Step{RunStep{
		StepOptions: StepOptions{TimeoutMinutes: 5, TimeoutMinutesExpression: Context("1")},
		Run:         "echo test",
	}}
	_, err = Emit(input)
	require.ErrorContains(t, err, "literal and expression timeout-minutes")
}
