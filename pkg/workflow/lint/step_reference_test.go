package lint

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepReferenceExpressions(t *testing.T) {
	for _, tt := range []struct {
		name, expression string
		missing          bool
	}{
		{"missing dot", "steps.PRIVATE_VALUE.outputs.value", true},
		{"missing bracket", "steps['PRIVATE_VALUE']['outputs']['value']", true},
		{"case insensitive", "STEPS['PrOdUcEr'].outputs.external", false},
		{"external output", "steps.producer.outputs.not_declared", false},
		{"step object", "toJSON(steps.PRIVATE_VALUE)", true},
		{"step result", "steps.PRIVATE_VALUE.outcome == 'success'", true},
		{"nested function", "format('{0}', fromJSON(steps.PRIVATE_VALUE.outputs.value))", true},
		{"dynamic output", "steps.PRIVATE_VALUE.outputs[inputs.output]", true},
		{"dynamic id", "steps[inputs.step].outputs.value", false},
		{"computed literal id", "steps[format('{0}', 'PRIVATE_VALUE')].outcome", false},
		{"nested computed id", "steps[steps.PRIVATE_VALUE.outputs.id].outcome", true},
		{"nested computed output", "steps[inputs.step].outputs[steps.PRIVATE_VALUE.outputs.key]", true},
		{"nested existing", "steps[steps.producer.outputs.id].outputs.value", false},
		{"whole context", "toJSON(steps)", false},
		{"wildcard", "toJSON(steps.*.outputs.value)", false},
		{"literal star", "steps['*'].outputs.value", true},
		{"numeric index", "steps[0].outputs.value", false},
		{"quoted text", "'steps.PRIVATE_VALUE.outputs.value'", false},
		{"quoted delimiters", "format('}} steps.PRIVATE_VALUE', steps.producer.outcome)", false},
		{"escaped quote", "'it''s steps.PRIVATE_VALUE'", false},
		{"other context", "needs.PRIVATE_VALUE.outputs.value", false},
		{"nested property", "inputs.steps.PRIVATE_VALUE", false},
		{"repeated missing", "steps.PRIVATE_VALUE.outcome || steps.other.outcome", true},
	} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			t.Run(tt.name+"/"+target.String(), func(t *testing.T) {
				source := contractJob + "    env:\n      VALUE: ${{ " + tt.expression + " }}\n" +
					"    steps: [{id: producer, uses: external/action@v1}]\n"
				input := ruleContractInput(t, source, target, false)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				runner, err := Default().Select("step-reference")
				require.NoError(t, err)
				found, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				if tt.missing {
					require.Len(t, found, 1)
					assert.Equal(t, "jobs.check.env.VALUE", found[0].Path)
					assert.Equal(t, Warning, found[0].Severity)
					assert.NotContains(t, found[0].Message, "PRIVATE_VALUE")
				} else {
					assert.Empty(t, found)
				}
				again, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				assert.Equal(t, found, again, "diagnostics must be deterministic")
				after, err := json.Marshal(input)
				require.NoError(t, err)
				assert.Equal(t, string(before), string(after), "input is read-only")
			})
		}
	}
}

func TestStepReferenceFields(t *testing.T) {
	for _, tt := range []struct{ fields, path string }{
		{"    outputs: {value: '${{ steps.missing.outputs.value }}'}\n" + contractSafeSteps, "outputs.value"},
		{"    env: {VALUE: '${{ steps.missing.outputs.value }}'}\n" + contractSafeSteps, "env.VALUE"},
		{"    if: steps.missing.outcome == 'success'\n" + contractSafeSteps, "if"},
		{"    steps: [{if: \"steps.missing.outcome == 'success'\", run: echo check}]\n", "steps[0].if"},
		{"    steps: [{if: \"${{ steps.missing.outcome == 'success' }}\", run: echo check}]\n", "steps[0].if"},
		{"    steps: [{env: {VALUE: '${{ steps.missing.outputs.value }}'}, run: echo check}]\n", "steps[0].env.VALUE"},
		{"    steps: [{uses: ./local, with: {value: '${{ steps.missing.outputs.value }}'}}]\n", "steps[0].with.value"},
		{"    steps: [{run: 'echo ${{ steps.missing.outputs.value }}'}]\n", "steps[0].run"},
		{"    steps: [{run: 'echo ${{ steps.producer.outcome }} / ${{ steps.missing.outcome }}'}, {id: producer, run: echo check}]\n", "steps[0].run"},
	} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			input := ruleContractInput(t, contractJob+tt.fields, target, false)
			found, err := StepReferenceRule().Check(context.Background(), input)
			require.NoError(t, err, tt.fields)
			require.Len(t, found, 1, tt.fields)
			assert.Equal(t, "jobs.check."+tt.path, found[0].Path)
		}
	}
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		input := ruleContractInput(t, contractJob+"    steps:\n"+
			"      - uses: ./local\n        with: {if: steps.missing is ordinary text}\n"+
			"      - run: echo steps.missing.outputs.value\n"+
			"      - if: steps.producer.outcome == 'success'\n        run: echo check\n"+
			"      - id: producer\n        run: echo producer\n", target, false)
		found, err := StepReferenceRule().Check(context.Background(), input)
		require.NoError(t, err)
		assert.Empty(t, found, "no ordering check, no implicit parsing of with.if or plain run text")
	}
}

func TestStepReferenceParticipantConclusionRegression(t *testing.T) {
	const source = "jobs:\n  conclusion:\n    runs-on: ubuntu-latest\n" +
		"    outputs:\n" +
		"      incomplete_count: ${{ steps.report_incomplete.outputs.incomplete_count }}\n" +
		"      noop_message: ${{ steps.noop.outputs.noop_message }}\n" +
		"      tools_reported: ${{ steps.missing_tool.outputs.tools_reported }}\n" +
		"      total_count: ${{ steps.missing_tool.outputs.total_count }}\n" +
		"    steps:\n      - run: echo conclude\n        env:\n" +
		"          GH_AW_JOB_DURATIONS: ${{ steps.report_failed_jobs.outputs.job_durations }}\n"
	input := ruleContractInput(t, source, ActionsAST, false)
	found, err := StepReferenceRule().Check(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, found, 5, "PR #71898 workqueue participant jobs omit these reporting steps")
	assert.Equal(t, []string{
		"jobs.conclusion.outputs.incomplete_count", "jobs.conclusion.outputs.noop_message",
		"jobs.conclusion.outputs.tools_reported", "jobs.conclusion.outputs.total_count",
		"jobs.conclusion.steps[0].env.GH_AW_JOB_DURATIONS",
	}, stepReferencePaths(found))

	for _, id := range []string{"report_incomplete", "noop", "missing_tool", "report_failed_jobs"} {
		input.Workflow.Jobs[0].Steps = append(input.Workflow.Jobs[0].Steps,
			actions.RawStep{Fields: map[string]any{"id": id, "run": "echo reporting"}})
	}
	found, err = StepReferenceRule().Check(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, found, "ordinary conclusion jobs still have their reporting producers")
}

func stepReferencePaths(found []Diagnostic) []string {
	var paths []string
	for _, diagnostic := range found {
		paths = append(paths, diagnostic.Path)
	}
	return paths
}

func TestStepReferenceJobIsolation(t *testing.T) {
	const source = "jobs:\n  producer:\n    runs-on: ubuntu-latest\n    steps: [{id: shared, run: echo check}]\n" +
		"  check:\n    runs-on: ubuntu-latest\n    needs: producer\n" +
		"    outputs: {value: '${{ steps.shared.outputs.value }}'}\n    steps: [{run: echo check}]\n"
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		found, err := StepReferenceRule().Check(context.Background(), ruleContractInput(t, source, target, false))
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, "jobs.check.outputs.value", found[0].Path)
	}
}

func TestStepReferenceAuthoredScopes(t *testing.T) {
	for _, tt := range []struct{ name, source, path string }{
		{"custom job", "jobs: {check: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}", "jobs.check.steps[0].run"},
		{"authored repo memory job name", "jobs: {push_repo_memory: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}", "jobs.push_repo_memory.steps[0].run"},
		{"authored cache memory job name", "jobs: {update_cache_memory: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}", "jobs.update_cache_memory.steps[0].run"},
		{"generated GHES host step", "jobs: {check: {steps: [{run: \"${{ steps['ghes-host-config'].outcome }}\"}]}}", ""},
		{"safe job", "safe-jobs: {check: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}", "safe-jobs.check.steps[0].run"},
		{"safe job metadata uses agent steps", "safe-jobs: {check: {description: '${{ steps.prepare.outputs.value }}', inputs: {value: {default: '${{ steps.prepare.outputs.value }}'}}}}", ""},
		{"safe output job", "safe-outputs: {jobs: {check: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}}", "safe-outputs.jobs.check.steps[0].run"},
		{"post step", "jobs: {check: {post-steps: [{if: steps.missing.outcome, run: echo check}]}}", "jobs.check.post-steps[0].if"},
		{"post producer", "jobs: {check: {steps: [{run: '${{ steps.producer.outputs.value }}'}], post-steps: [{id: producer, run: echo check}]}}", ""},
		{"safe post producer", "safe-outputs: {jobs: {check: {steps: [{run: '${{ steps.producer.outputs.value }}'}], post-steps: [{id: producer, run: echo check}]}}}", ""},
		{"root generated", "steps: [{run: '${{ steps.generated.outputs.value }}'}]", ""},
		{"safe output generated", "safe-outputs: {steps: [{run: '${{ steps.generated.outputs.value }}'}], post-steps: [{if: steps.generated.outcome, run: echo check}]}", ""},
		{"builtin override", "jobs: {conclusion: {steps: [{run: '${{ steps.generated.outputs.value }}'}]}}", ""},
		{"imported job", "imports: [shared.md]\njobs: {check: {steps: [{run: '${{ steps.imported.outputs.value }}'}]}}", ""},
		{"mapped imports", "imports: {aw: [shared.md]}\nsafe-outputs: {jobs: {check: {steps: [{run: '${{ steps.imported.outputs.value }}'}]}}}", ""},
		{"non AW imports", "imports: {js: [shared.js]}\njobs: {check: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}", "jobs.check.steps[0].run"},
		{"dynamic id", "jobs: {check: {steps: [{id: '${{ inputs.id }}', run: '${{ steps.unknown.outputs.value }}'}]}}", ""},
		{"restore memory generated", "jobs: {check: {restore-memory: {cache-memory: true}, steps: [{run: '${{ steps.restore_cache_memory_0.outputs.cache-hit }}'}]}}", ""},
		{"prompt and config", "env: {VALUE: '${{ steps.generated.outputs.value }}'}\ntools: {bash: ['${{ steps.unknown.outputs.value }}']}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := ruleContractInput(t, tt.source+"\n", AWAST, false)
			before, err := json.Marshal(input)
			require.NoError(t, err)
			found, err := StepReferenceRule().Check(context.Background(), input)
			require.NoError(t, err)
			if tt.path == "" {
				assert.Empty(t, found)
			} else {
				require.Len(t, found, 1)
				assert.Equal(t, tt.path, found[0].Path)
			}
			after, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after))
		})
	}
}

func TestStepReferenceAuthoredSetupAndPreSteps(t *testing.T) {
	for _, field := range []string{"setup-steps", "pre-steps"} {
		for _, tt := range []struct{ name, steps, path string }{
			{"producer", "    " + field + ": [{id: producer, run: echo setup}]\n" +
				"    outputs: {value: '${{ steps.producer.outputs.value }}'}\n" +
				"    steps: [{run: '${{ steps.producer.outputs.value }}'}]\n", ""},
			{"generated GHES host step", "    " + field + ": [{run: \"${{ steps['ghes-host-config'].outcome }}\"}]\n", ""},
			{"interpolation", "    " + field + ": [{run: '${{ steps.missing.outputs.value }}'}]\n", field + "[0].run"},
			{"bare condition", "    " + field + ": [{if: steps.missing.outcome, run: echo setup}]\n", field + "[0].if"},
			{"existing condition", "    " + field + ": [{if: steps.producer.outcome, run: echo setup}]\n" +
				"    steps: [{id: producer, run: echo check}]\n", ""},
			{"input named if", "    " + field + ": [{uses: ./local, with: {if: ordinary text}}]\n", ""},
			{"dynamic ID", "    " + field + ": [{id: '${{ inputs.id }}', run: echo setup}]\n" +
				"    steps: [{run: '${{ steps.unknown.outputs.value }}'}]\n", ""},
		} {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				input := ruleContractInput(t, contractJob+tt.steps, AWAST, false)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				found, err := StepReferenceRule().Check(context.Background(), input)
				require.NoError(t, err)
				if tt.path == "" {
					assert.Empty(t, found)
				} else {
					require.Len(t, found, 1)
					assert.Equal(t, "jobs.check."+tt.path, found[0].Path)
				}
				after, err := json.Marshal(input)
				require.NoError(t, err)
				assert.Equal(t, string(before), string(after))
			})
		}
		for _, value := range []string{"invalid", "[invalid]", "[{id: 123, run: echo setup}]"} {
			input := ruleContractInput(t, contractJob+"    "+field+": "+value+"\n", AWAST, false)
			_, err := StepReferenceRule().Check(context.Background(), input)
			require.Error(t, err)
		}
	}
}

func TestStepReferenceTypedAndCompiledPrecedence(t *testing.T) {
	doc, err := aw.Parse("---\nimports: invalid\njobs: {check: {steps: [{run: '${{ steps.missing.outputs.value }}'}]}}\n---\n")
	require.NoError(t, err)
	wf := &actions.Workflow{Jobs: []*actions.Job{{
		ID: "check", RunsOn: "ubuntu-latest",
		Outputs: map[actions.OutputName]actions.Expression{"value": actions.Context("steps.producer.outputs.external")},
		Steps: []actions.Step{
			actions.RunStep{StepOptions: actions.StepOptions{If: actions.Context("steps.producer.outcome == 'success'")}, Run: "echo check"},
			actions.ActionStep{StepOptions: actions.StepOptions{ID: "producer"}, Uses: "./external",
				With: map[string]actions.Expression{"value": actions.Context("steps.producer.outputs.undeclared")}},
		},
	}}}
	input := Input{Document: doc, Workflow: wf}
	before, err := json.Marshal(input)
	require.NoError(t, err)
	found, err := StepReferenceRule().Check(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, found, "compiled graph wins, including transformed or removed authored steps")
	wf.Jobs[0].Steps = append(wf.Jobs[0].Steps, actions.RunStep{
		StepOptions: actions.StepOptions{Env: map[string]actions.Expression{"VALUE": actions.Context("steps.missing.outputs.value")}},
		Run:         "echo check",
	})
	found, err = StepReferenceRule().Check(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "jobs.check.steps[2].env.VALUE", found[0].Path)
	wf.Jobs[0].Steps = wf.Jobs[0].Steps[:2]
	after, err := json.Marshal(input)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

func TestStepReferenceCompiledGraphDoesNotAssumeAuthoredProducer(t *testing.T) {
	input := Input{Workflow: &actions.Workflow{Jobs: []*actions.Job{{
		ID: "check", RunsOn: "ubuntu-latest",
		Outputs: map[actions.OutputName]actions.Expression{
			"value": actions.Context("steps['ghes-host-config'].outcome"),
		},
		Steps: []actions.Step{actions.RunStep{Run: "echo check"}},
	}}}}
	found, err := StepReferenceRule().Check(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "jobs.check.outputs.value", found[0].Path)
}

func TestStepReferenceInvalidAndCancelled(t *testing.T) {
	for _, source := range []string{
		"jobs: invalid", "jobs: {check: invalid}", "jobs: {check: {steps: invalid}}",
		"jobs: {check: {steps: [invalid]}}", "jobs: {check: {steps: [{id: 123, run: echo check}]}}",
		"jobs: {check: {steps: [{if: 'steps.PRIVATE_VALUE &&', run: echo check}]}}",
		"jobs: {check: {steps: [{run: '${{ steps.PRIVATE_VALUE'}]}}",
		"imports: PRIVATE_VALUE",
	} {
		_, err := StepReferenceRule().Check(context.Background(), ruleContractInput(t, source+"\n", AWAST, false))
		require.Error(t, err, source)
		assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	}
	for _, input := range []Input{
		{}, {Workflow: &actions.Workflow{Jobs: []*actions.Job{nil}}},
		{Workflow: &actions.Workflow{Jobs: []*actions.Job{{ID: "check"}}}},
	} {
		_, err := StepReferenceRule().Check(context.Background(), input)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		input := ruleContractInput(t, contractJob+contractSafeSteps, target, false)
		_, err := StepReferenceRule().Check(ctx, input)
		require.ErrorIs(t, err, context.Canceled)
	}
	// One malformed expression must not hide findings from independent fields.
	input := ruleContractInput(t, contractJob+"    env: {BAD: '${{ steps.PRIVATE_VALUE', GOOD: '${{ steps.missing.outputs.value }}'}\n"+contractSafeSteps, AWAST, false)
	found, err := StepReferenceRule().Check(context.Background(), input)
	require.Error(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "jobs.check.env.GOOD", found[0].Path)
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
}
