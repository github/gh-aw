package lint

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsoundContains(t *testing.T) {
	for _, test := range []struct {
		name, condition string
		want            bool
	}{
		{"ref in literal", `contains('refs/heads/main refs/heads/develop', github.ref)`, true},
		{"wrapped", `${{ contains('refs/heads/main', github.ref) }}`, true},
		{"case insensitive", `Contains('main', GitHub.Ref_Name)`, true},
		{"head ref", `github.event_name == 'pull_request' && contains('release', github.head_ref)`, true},
		{"negated", `!contains('main develop', github.ref_name)`, true},
		{"actor", `contains('octocat hubot', github.actor)`, true},
		{"event field", `contains('bug enhancement', github.event.label.name)`, true},
		{"event index", `contains('opened', github.event['action'])`, true},
		{"input", `contains('staging production', inputs.environment)`, true},
		{"fromJSON array", `contains(fromJSON('["refs/heads/main","refs/heads/develop"]'), github.ref)`, false},
		{"equality", `github.ref == 'refs/heads/main' || github.ref == 'refs/heads/develop'`, false},
		{"literal in literal", `contains('refs/heads/main', 'main')`, false},
		{"literal needle", `contains(github.ref, 'refs/tags/')`, false},
		{"array context haystack", `contains(github.event.pull_request.labels.*.name, 'bug')`, false},
		{"non-controllable context", `contains('push schedule', github.event_name)`, false},
		{"repository", `contains('github/gh-aw', github.repository)`, false},
		{"job status", `contains('success', needs.build.result)`, false},
		{"github object", `contains('x', github)`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			yamlCondition := fmt.Sprintf("%q", test.condition)
			assertZizmorRuleInputs(t, "unsound-contains", contractJob+"    if: "+yamlCondition+"\n"+contractSafeSteps, test.want)
			assertZizmorRuleInputs(t, "unsound-contains", contractJob+"    steps:\n      - if: "+yamlCondition+"\n        run: echo public\n", test.want)
		})
	}
}

func TestUnsoundContainsAWConditions(t *testing.T) {
	input := ruleContractInput(t, "if: contains('main', github.ref_name)\nsteps: [{if: \"contains('main', github.ref)\", run: echo a}]\npost-steps: [{if: \"contains('main', inputs.target)\", run: echo b}]\nsafe-outputs:\n  jobs:\n    notify:\n      if: contains('main', github.head_ref)\n      steps: [{run: echo c}]\n", AWAST, false)
	runner, err := Default().Select("unsound-contains")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	var paths []string
	for _, diagnostic := range diagnostics {
		paths = append(paths, diagnostic.Path)
	}
	assert.ElementsMatch(t, []string{"if", "steps[0].if", "post-steps[0].if", "safe-outputs.jobs.notify.if"}, paths)
}

func TestUnsoundContainsPartialFailure(t *testing.T) {
	input := ruleContractInput(t, "jobs:\n  bad: {runs-on: ubuntu-latest, if: 'inputs.PRIVATE_VALUE +', steps: [{run: echo}]}\n  good: {runs-on: ubuntu-latest, if: \"contains('main', github.ref)\", steps: [{run: echo}]}\n", AWAST, false)
	runner, err := Default().Select("unsound-contains")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.good.if", diagnostics[0].Path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, input)
	assert.ErrorIs(t, err, context.Canceled)
}
