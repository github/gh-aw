//go:build !integration

package actions_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/github/gh-aw/pkg/workflow/actions"
)

// TestSpec_PublicAPI_ParseEmit follows README.md's Overview and Public API:
// parsed workflows retain authored syntax and opaque expression strings.
func TestSpec_PublicAPI_ParseEmit(t *testing.T) {
	source := "# authored workflow\nname: 'Preserved'\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ github.ref }}' # keep this comment\n"
	for _, lineEnding := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "LF", "\r\n": "CRLF"}[lineEnding], func(t *testing.T) {
			input := strings.ReplaceAll(source, "\n", lineEnding)
			workflow, err := actions.Parse([]byte(input))
			require.NoError(t, err, "native workflow YAML must parse")
			emitted, err := actions.Emit(workflow)
			require.NoError(t, err, "a parsed workflow must emit")
			assert.Equal(t, input, emitted, "unchanged source must retain comments, field order, quoting, expressions, and line endings")
		})
	}
}

// TestSpec_PublicAPI_Resolve follows README.md's nonmutating resolution and
// dependency derivation contracts using declared producer outputs.
func TestSpec_PublicAPI_Resolve(t *testing.T) {
	newWorkflow := func() *actions.Workflow {
		return &actions.Workflow{
			On: "push",
			Jobs: []*actions.Job{
				{
					ID: "producer", RunsOn: "ubuntu-latest",
					Steps: []actions.Step{actions.RunStep{
						StepOptions: actions.StepOptions{
							ID: "generate", Outputs: []actions.OutputDecl{{Name: "result"}},
						},
						Run: "echo result=ready",
					}},
				},
				{
					ID: "consumer", RunsOn: "ubuntu-latest",
					Steps: []actions.Step{actions.ActionStep{
						Uses: "example/action@v1",
						With: map[string]actions.Expression{
							"result": actions.OutputRef{Job: "producer", Step: "generate", Name: "result"},
						},
					}},
				},
			},
		}
	}
	input := newWorkflow()
	resolved, err := actions.Resolve(input)
	require.NoError(t, err, "a reference to a declared producer output must resolve")
	assert.Equal(t, newWorkflow(), input, "resolution must leave the entire input unchanged")
	require.Len(t, resolved.Jobs, 2, "resolution must retain both jobs")
	assert.Contains(t, resolved.Jobs[1].Needs, actions.JobID("producer"), "the consuming job must depend on its producer")
	assert.NotEmpty(t, resolved.Jobs[0].Outputs, "resolution must derive producer job output bindings")

	first, err := actions.Lower(resolved)
	require.NoError(t, err, "the resolved AST must lower to YAML-compatible values")
	second, err := actions.Lower(resolved)
	require.NoError(t, err, "repeated lowering must succeed")
	assert.Equal(t, first, second, "lowering the same resolved workflow must be deterministic")
}

// TestSpec_PublicAPI_ResolveStepOutputOrder enforces README.md's explicit
// requirement that a step can read only outputs from earlier steps in its job.
func TestSpec_PublicAPI_ResolveStepOutputOrder(t *testing.T) {
	for _, tc := range []struct {
		name          string
		producerFirst bool
	}{
		{name: "earlier producer", producerFirst: true},
		{name: "later producer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := actions.RunStep{
				StepOptions: actions.StepOptions{ID: "generate", Outputs: []actions.OutputDecl{{Name: "result"}}},
				Run:         "echo result=ready",
			}
			consumer := actions.ActionStep{
				Uses: "example/action@v1",
				With: map[string]actions.Expression{
					"result": actions.OutputRef{Job: "build", Step: "generate", Name: "result"},
				},
			}
			steps := []actions.Step{consumer, producer}
			if tc.producerFirst {
				steps = []actions.Step{producer, consumer}
			}
			_, err := actions.Resolve(&actions.Workflow{
				Jobs: []*actions.Job{{ID: "build", RunsOn: "ubuntu-latest", Steps: steps}},
			})
			if tc.producerFirst {
				require.NoError(t, err, "references to earlier declared step outputs must resolve")
			} else {
				assert.Error(t, err, "references to later step outputs must be rejected")
			}
		})
	}
}

// TestSpec_PublicAPI_ResolveWorkflowEnv enforces README.md's ban on step and
// job output references in workflow-level environment values.
func TestSpec_PublicAPI_ResolveWorkflowEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  actions.OutputRef
	}{
		{name: "step output", ref: actions.OutputRef{Job: "build", Step: "generate", Name: "result"}},
		{name: "job output", ref: actions.OutputRef{Job: "build", Name: "result"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := actions.Resolve(&actions.Workflow{
				Env: map[string]actions.Expression{"RESULT": tc.ref},
				Jobs: []*actions.Job{{
					ID: "build", RunsOn: "ubuntu-latest",
					Steps: []actions.Step{actions.RunStep{
						StepOptions: actions.StepOptions{ID: "generate", Outputs: []actions.OutputDecl{{Name: "result"}}},
						Run:         "echo result=ready",
					}},
					Outputs: map[actions.OutputName]actions.Expression{
						"result": actions.OutputRef{Job: "build", Step: "generate", Name: "result"},
					},
				}},
			})
			assert.Error(t, err, "workflow-level environment values must reject %s references", tc.name)
		})
	}
}

// TestSpec_PublicAPI_ResolveDependencyGraph follows README.md's requirement
// that resolution validates job dependencies.
func TestSpec_PublicAPI_ResolveDependencyGraph(t *testing.T) {
	for _, tc := range []struct {
		name string
		jobs []*actions.Job
	}{
		{name: "missing dependency", jobs: []*actions.Job{
			{ID: "build", Needs: []actions.JobID{"missing"}, RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RunStep{Run: "echo build"}}},
		}},
		{name: "cyclic dependencies", jobs: []*actions.Job{
			{ID: "first", Needs: []actions.JobID{"second"}, RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RunStep{Run: "echo first"}}},
			{ID: "second", Needs: []actions.JobID{"first"}, RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RunStep{Run: "echo second"}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := actions.Resolve(&actions.Workflow{Jobs: tc.jobs})
			assert.Error(t, err, "invalid dependency graph must be rejected: %s", tc.name)
		})
	}
}

// TestSpec_DesignDecision_JobScopedStepIDs follows README.md's job-scoped
// step ID contract. SPEC_AMBIGUITY: "Workflow ... IDs are job-scoped" does
// not identify a workflow ID API; only the explicit step ID scope is tested.
func TestSpec_DesignDecision_JobScopedStepIDs(t *testing.T) {
	workflow := &actions.Workflow{}
	for _, id := range []actions.JobID{"first", "second"} {
		workflow.Jobs = append(workflow.Jobs, &actions.Job{
			ID: id, RunsOn: "ubuntu-latest",
			Steps: []actions.Step{actions.RunStep{
				StepOptions: actions.StepOptions{ID: "shared"}, Run: "echo ready",
			}},
		})
	}
	_, err := actions.Resolve(workflow)
	require.NoError(t, err, "distinct jobs must be allowed to reuse a step ID")
}

// TestSpec_PublicAPI_LowerStep exercises README.md's documented typed and
// raw step representations through their public YAML conversion API.
// SPEC_AMBIGUITY: typed run-step interpolation restrictions are unspecified;
// authored expression opacity is checked separately on parsed source.
func TestSpec_PublicAPI_LowerStep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		step  actions.Step
		field string
		value string
	}{
		{name: "run", step: actions.RunStep{Run: "echo ready"}, field: "run", value: "echo ready"},
		{name: "action", step: actions.ActionStep{Uses: "example/action@v1"}, field: "uses", value: "example/action@v1"},
		{name: "raw", step: actions.RawStep{Fields: map[string]any{"run": "echo raw"}}, field: "run", value: "echo raw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := actions.Resolve(&actions.Workflow{
				Jobs: []*actions.Job{{ID: "build", RunsOn: "ubuntu-latest", Steps: []actions.Step{tc.step}}},
			})
			require.NoError(t, err, "the documented %s step representation must resolve", tc.name)
			lowered, err := actions.LowerStep(resolved.Jobs[0].Steps[0], "build", resolved)
			require.NoError(t, err, "the documented %s step representation must lower", tc.name)
			assert.Equal(t, tc.value, lowered[tc.field], "lowering must retain the authored %s value", tc.field)
		})
	}
}

// TestSpec_PublicAPI_ValidateJob follows README.md's native job validation API.
// SPEC_AMBIGUITY: the README does not enumerate invalid job structures.
func TestSpec_PublicAPI_ValidateJob(t *testing.T) {
	err := actions.ValidateJob(&actions.Job{
		ID: "build", RunsOn: "ubuntu-latest",
		Steps: []actions.Step{actions.RunStep{Run: "echo ready"}},
	})
	require.NoError(t, err, "a native runner job containing a run step must validate")
}

// TestSpec_PublicAPI_EditSource follows README.md's semantic editing contract.
func TestSpec_PublicAPI_EditSource(t *testing.T) {
	source := "# workflow comment\nname: 'Before' # name comment\non: push\n"
	before := map[string]any{"name": "Before", "on": "push"}
	after := map[string]any{"name": "After", "on": "push"}
	edited, err := actions.EditSource(source, before, after)
	require.NoError(t, err, "a semantic name edit must succeed")
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(edited), &parsed), "edited source must remain valid YAML")
	assert.Equal(t, after, parsed, "the edited YAML must represent the requested semantic change")
	assert.True(t, strings.HasPrefix(edited, "# workflow comment\n"), "the unaffected leading comment must remain")
	assert.True(t, strings.HasSuffix(edited, " # name comment\non: push\n"), "the unaffected inline comment and remaining fields must remain")
}
