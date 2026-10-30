package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZizmorCheckoutCredentials(t *testing.T) {
	// Regressions from #2991, #14449 and #25873: checkout inputs in both
	// custom jobs and imported setup steps must disable credential persistence.
	for _, test := range []struct {
		name, uses, with string
		want             bool
	}{
		{"omitted", "actions/checkout@v6", "", true},
		{"enabled", "actions/checkout@v6", "persist-credentials: true", true},
		{"dynamic", "actions/checkout@v6", "persist-credentials: '${{ inputs.persist }}'", true},
		{"null", "actions/checkout@v6", "persist-credentials: null", true},
		{"boolean", "actions/checkout@v6", "persist-credentials: false", false},
		{"string", "actions/checkout@v6", "persist-credentials: 'false'", false},
		{"case insensitive string", "actions/checkout@v6", "persist-credentials: 'FALSE'", false},
		{"constant boolean", "actions/checkout@v6", "persist-credentials: '${{ false }}'", false},
		{"constant string", "actions/checkout@v6", `persist-credentials: "${{ 'false' }}"`, false},
		{"unrelated action", "owner/checkout@v6", "", false},
		{"local lookalike", "./actions/checkout@v6", "", false},
		{"subdirectory lookalike", "actions/checkout/other@v6", "", false},
		{"case insensitive repository", "Actions/Checkout@v6", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := contractJob + "    steps:\n      - uses: " + test.uses + "\n"
			if test.with != "" {
				source += "        with: {" + test.with + "}\n"
			}
			assertZizmorRuleInputs(t, "artipacked", source, test.want)
		})
	}
}

func TestZizmorReleaseCaches(t *testing.T) {
	// #5214 disabled Node's implicit package-manager cache; #5872 and
	// #19865 disabled both explicit and default setup-go cache restoration.
	for _, test := range []struct {
		name, on, uses, with string
		want                 bool
	}{
		{"release Go default", "release", "actions/setup-go@v6", "", true},
		{"release Go explicit", "release", "actions/setup-go@v6", "cache: true", true},
		{"release Go disabled", "release", "actions/setup-go@v6", "cache: false", false},
		{"release Go string disabled", "release", "actions/setup-go@v6", "cache: 'false'", false},
		{"release Go constant disabled", "release", "actions/setup-go@v6", "cache: '${{ false }}'", false},
		{"release Go dynamic", "release", "actions/setup-go@v6", "cache: '${{ inputs.cache }}'", true},
		{"old Go default", "release", "actions/setup-go@v3", "", false},
		{"old Go explicit", "release", "actions/setup-go@v3", "cache: true", true},
		{"pinned Go unknown default", "release", "actions/setup-go@0123456789abcdef0123456789abcdef01234567", "", true},
		{"tag Node default", "{push: {tags: ['v*']}}", "actions/setup-node@v6", "", true},
		{"mixed tag and branch", "{push: {tags: ['v*'], branches: [main]}}", "actions/setup-node@v6", "", true},
		{"tag Node disabled", "{push: {tags: ['v*']}}", "actions/setup-node@v6", "package-manager-cache: false", false},
		{"Node explicit cache overrides opt-out", "release", "actions/setup-node@v6", "cache: npm, package-manager-cache: false", true},
		{"Node empty cache", "release", "actions/setup-node@v6", "cache: '', package-manager-cache: false", false},
		{"old Node default", "release", "actions/setup-node@v4", "", false},
		{"old Node explicit", "release", "actions/setup-node@v4", "cache: npm", true},
		{"Python explicit", "release", "actions/setup-python@v5", "cache: pip", true},
		{"Python empty", "release", "actions/setup-python@v5", "cache: ''", false},
		{"Java explicit", "release", "actions/setup-java@v5", "cache: maven", true},
		{"Java empty", "release", "actions/setup-java@v5", "cache: ''", false},
		{"cache restore", "release", "actions/cache/restore@v4", "", true},
		{"cache lookup only", "release", "actions/cache/restore@v4", "lookup-only: true", false},
		{"cache combined", "release", "actions/cache@v4", "", true},
		{"cache save only", "release", "actions/cache/save@v4", "", false},
		{"ordinary push", "push", "actions/setup-go@v6", "", false},
		{"ordinary pull request", "pull_request", "actions/setup-go@v6", "", false},
		{"branch scope", "{push: {branches: [main]}}", "actions/setup-go@v6", "", false},
		{"unrelated setup action", "release", "owner/setup-go@v6", "", false},
		{"release event list", "[workflow_dispatch, release]", "actions/setup-go@v6", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "on: " + test.on + "\n" + contractJob + "    steps:\n      - uses: " + test.uses + "\n"
			if test.with != "" {
				source += "        with: {" + test.with + "}\n"
			}
			assertZizmorRuleInputs(t, "cache-poisoning", source, test.want)
		})
	}
}

func assertZizmorRuleInputs(t *testing.T, id, source string, want bool) {
	t.Helper()
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		t.Run(target.String(), func(t *testing.T) {
			input := ruleContractInput(t, source, target, false)
			before, err := json.Marshal(input)
			require.NoError(t, err)
			runner, err := Default().Select(id)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			if want {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, Warning, diagnostics[0].Severity)
			} else {
				assert.Empty(t, diagnostics)
			}
			again, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			assert.Equal(t, diagnostics, again)
			after, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func TestZizmorAuthoredExecutionSurfaces(t *testing.T) {
	for _, surface := range []struct{ source, path string }{
		{"steps: [%s]", "steps[0]"},
		{"post-steps: [%s]", "post-steps[0]"},
		{"jobs: {check: {steps: [%s]}}", "jobs.check.steps[0]"},
		{"safe-jobs: {check: {steps: [%s]}}", "safe-jobs.check.steps[0]"},
		{"safe-outputs: {steps: [%s]}", "safe-outputs.steps[0]"},
		{"safe-outputs: {jobs: {check: {steps: [%s]}}}", "safe-outputs.jobs.check.steps[0]"},
	} {
		for _, test := range []struct{ id, step, suffix string }{
			{"artipacked", "{uses: actions/checkout@v6}", ".uses"},
			{"cache-poisoning", "{uses: actions/setup-go@v6}", ".uses"},
			{"obfuscation", `{run: "echo ${{ 'hidden' }}"}`, ".run"},
			{"template-injection", `{uses: actions/github-script@v8, with: {script: "console.log('${{ inputs.title }}')"}}`, ".with.script"},
		} {
			t.Run(test.id+"/"+surface.path, func(t *testing.T) {
				input := ruleContractInput(t, "on: release\n"+fmt.Sprintf(surface.source, test.step)+"\n", AWAST, false)
				runner, err := Default().Select(test.id)
				require.NoError(t, err)
				diagnostics, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				require.Len(t, diagnostics, 1)
				assert.Equal(t, surface.path+test.suffix, diagnostics[0].Path)
			})
		}
	}
}

func TestZizmorWorkflowWriteScopes(t *testing.T) {
	// #5229 moved issues: write from AW agent permissions to a dedicated
	// publisher job. AW root grants are not Actions workflow-wide grants.
	source := "permissions: {contents: read, issues: write, id-token: write}\n" + contractJob + contractSafeSteps
	input := ruleContractInput(t, source, ActionsAST, false)
	runner, err := Default().Select("excessive-permissions")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "permissions.id-token", diagnostics[0].Path)
	assert.Equal(t, "permissions.issues", diagnostics[1].Path)
	authored := ruleContractInput(t, source, AWAST, false)
	diagnostics, err = runner.Run(context.Background(), authored)
	require.NoError(t, err)
	assert.Empty(t, diagnostics)
	// The compiled AST, not the companion source, owns workflow-wide scope.
	input.Document = authored.Document
	diagnostics, err = runner.Run(context.Background(), input)
	require.NoError(t, err)
	assert.Len(t, diagnostics, 2)
}

func TestZizmorLiteralExpressionObfuscation(t *testing.T) {
	// #4398 removed empty-string expression outputs; #14257 removed
	// expression wrappers around static cache-memory environment values.
	for _, test := range []struct {
		name, fields string
		want         bool
	}{
		{"empty output", `outputs: {comment_id: "${{ '' }}"}`, true},
		{"static path", `env: {CACHE_DIR: "${{ '/tmp/gh-aw/cache-memory/' }}"}`, true},
		{"escaped quotes and closing delimiter", `env: {VALUE: "${{ 'it''s }} data' }}"}`, true},
		{"parenthesized literal", `env: {VALUE: "${{ ('hidden') }}"}`, true},
		{"normal output", `outputs: {comment_id: ''}`, false},
		{"ordinary expression", `env: {REPO: '${{ github.repository }}'}`, false},
		{"fallback literal", `env: {VALUE: "${{ inputs.value || 'fallback' }}"}`, false},
		{"comparison literal", `if: github.event_name == 'release'`, false},
		{"boolean expression", `if: '${{ false }}'`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertZizmorRuleInputs(t, "obfuscation", contractJob+"    "+test.fields+"\n"+contractSafeSteps, test.want)
		})
	}
	input := ruleContractInput(t, "description: \"${{ 'plain data' }}\"\ngraders: {custom: {script: \"${{ 'plain data' }}\"}}\n", AWAST, false)
	input.Document.Markdown = "${{ 'plain data' }}"
	runner, err := Default().Select("obfuscation")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "non-runner AW text is not interpolated by Actions")
}

func TestZizmorPolicyPartialFailures(t *testing.T) {
	for _, test := range []struct{ id, before, invalid, after string }{
		{"artipacked", "", "steps: [{uses: actions/checkout@v6, with: PRIVATE_VALUE}]", "jobs: {check: {steps: [{uses: actions/checkout@v6}]}}"},
		{"artipacked", "", "steps: [{uses: actions/checkout@v6, with: {persist-credentials: [PRIVATE_VALUE]}}]", "jobs: {check: {steps: [{uses: actions/checkout@v6}]}}"},
		{"artipacked", "", "steps: [{uses: actions/checkout@v6, with: {persist-credentials: '${{ inputs.PRIVATE_VALUE + }}'}}]", "jobs: {check: {steps: [{uses: actions/checkout@v6}]}}"},
		{"cache-poisoning", "on: release\n", "steps: [{uses: actions/setup-go@v6, with: {cache: [PRIVATE_VALUE]}}]", "jobs: {check: {steps: [{uses: actions/setup-go@v6}]}}"},
		{"obfuscation", "", "env: {BAD: '${{ inputs.PRIVATE_VALUE + }}'}", `jobs: {check: {env: {GOOD: "${{ 'hidden' }}"}}}`},
	} {
		t.Run(test.id+"/"+test.invalid, func(t *testing.T) {
			input := ruleContractInput(t, test.before+test.invalid+"\n"+test.after+"\n", AWAST, false)
			runner, err := Default().Select(test.id)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			require.Len(t, diagnostics, 1, "retain findings in other valid scopes")
			assert.NotContains(t, fmt.Sprint(diagnostics), "PRIVATE_VALUE")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = runner.Run(ctx, input)
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestZizmorTypedActionSteps(t *testing.T) {
	workflow := &actions.Workflow{On: "release", Jobs: []*actions.Job{{
		ID: "check", RunsOn: "ubuntu-latest",
		Steps: []actions.Step{
			actions.ActionStep{Uses: "actions/checkout@v6", With: map[string]actions.Expression{"persist-credentials": actions.Boolean(false)}},
			actions.ActionStep{Uses: "actions/setup-go@v6", With: map[string]actions.Expression{"cache": actions.Boolean(false)}},
			actions.ActionStep{Uses: "actions/github-script@v8", With: map[string]actions.Expression{"script": actions.Literal("console.log('${{ inputs.title }}')")}},
		},
	}}}
	runner, err := Default().Select("artipacked", "cache-poisoning", "template-injection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.check.steps[2].with.script", diagnostics[0].Path)
}

func TestZizmorCompiledGraphTakesPrecedence(t *testing.T) {
	document, err := aw.Parse("---\non: release\nsteps: [{uses: actions/checkout@v6}, {uses: actions/setup-go@v6}]\n---\n")
	require.NoError(t, err)
	workflow, err := actions.Parse([]byte(contractJob + contractSafeSteps))
	require.NoError(t, err)
	runner, err := Default().Select("artipacked", "cache-poisoning", "obfuscation")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow, Document: document})
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "do not duplicate authored steps stripped or transformed by compilation")
}
