package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivilegedCheckoutDependencyGuards(t *testing.T) {
	for _, tt := range []struct {
		name, gate, relay, job, step, needs, ref, inputs string
		count                                            int
	}{
		{name: "implicit transitive success"},
		{name: "output condition retains implicit success", job: "needs.relay.outputs.enabled == 'true'"},
		{name: "explicit success", job: "success()"},
		{name: "case insensitive status", job: "SuCcEsS()"},
		{name: "always bypass", job: "always()", count: 1},
		{name: "failure bypass", job: "failure()", count: 1},
		{name: "cancelled bypass", job: "cancelled()", count: 1},
		{name: "negated success bypass", job: "!success()", count: 1},
		{name: "explicit successful dependency", job: "always() && needs.relay.result == 'success'"},
		{name: "bracket successful dependency", job: "always() && 'SUCCESS' == needs['relay']['result']"},
		{name: "both OR branches guarded", job: "(always() && needs.relay.result == 'success') || (success() && true)"},
		{name: "OR bypass", job: "needs.relay.result == 'success' || always()", count: 1},
		{name: "late OR bypass", job: "needs.relay.result == 'success' || needs.relay.result == 'success' || always()", count: 1},
		{name: "not failure is insufficient", job: "always() && needs.relay.result != 'failure'", count: 1},
		{name: "non-skipped without cancellation guard", job: "always() && needs.relay.result != 'skipped'", count: 1},
		{name: "non-skipped retains implicit success", job: "needs.relay.result != 'skipped'"},
		{name: "non-skipped and non-cancelled", job: "(!cancelled()) && needs.relay.result != 'skipped'"},
		{name: "grouped cancellation guard", job: "!(cancelled()) && 'skipped' != needs['relay']['result']"},
		{name: "cancellation guard OR bypass", job: "(!cancelled() || always()) && needs.relay.result != 'skipped'", count: 1},
		{name: "cancelled is not non-cancelled", job: "cancelled() && needs.relay.result != 'skipped'", count: 1},
		{name: "output is not a result proof", job: "always() && needs.relay.outputs.enabled == 'true'", count: 1},
		{name: "output result is not job result", job: "always() && needs.relay.outputs.result == 'success'", count: 1},
		{name: "computed dependency is unknown", job: "always() && needs[inputs.dependency].result == 'success'", count: 1},
		{name: "wrong dependency success", needs: "[relay, other]", job: "always() && needs.other.result == 'success'", count: 1},
		{name: "step successful dependency", job: "always()", step: "needs.relay.result == 'success'"},
		{name: "step success is not dependency success", job: "always()", step: "success()", count: 1},
		{name: "step non-skipped guard", job: "always()", step: "!cancelled() && needs.relay.result != 'skipped'"},
		{name: "step non-skipped alone", job: "always()", step: "needs.relay.result != 'skipped'", count: 1},
		{name: "step OR bypass", job: "always()", step: "needs.relay.result == 'success' || always()", count: 1},
		{name: "relay status bypass", relay: "always()", count: 1},
		{name: "relay explicit success", relay: "always() && needs.gate.result == 'success'"},
		{name: "relay non-skipped alone", relay: "always() && needs.gate.result != 'skipped'", count: 1},
		{name: "relay non-skipped guard", relay: "!cancelled() && needs.gate.result != 'skipped'"},
		{name: "undeclared result reference", needs: "other", job: "always() && needs.relay.result == 'success'", count: 1},
		{name: "multiple dependencies", needs: "[other, relay]"},
		{name: "unguarded ancestor", gate: "true", count: 1},
		{name: "ancestor OR bypass", gate: "github.event.pull_request.merged || always()", count: 1},
		{name: "quoted ancestor guard", gate: "contains('github.event.pull_request.merged', 'merged')", count: 1},
		{name: "head still warned", ref: "${{ github.event.pull_request.head.sha }}", count: 1},
		{name: "merge fallback still warned", ref: "${{ github.event.pull_request.merge_commit_sha || 'main' }}", count: 1},
		{name: "fork repository still warned", inputs: "repository: '${{ github.event.pull_request.head.repo.full_name }}', ", count: 1},
		{name: "unsafe opt-out still warned", inputs: "allow-unsafe-pr-checkout: true, ", count: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gate, needs, ref := tt.gate, tt.needs, tt.ref
			if gate == "" {
				gate = "github.event.pull_request.merged == true"
			}
			if needs == "" {
				needs = "relay"
			}
			if ref == "" {
				ref = "${{ github.event.pull_request.merge_commit_sha }}"
			}
			source := "on: pull_request_target\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: >-\n      " + gate +
				"\n    steps: [{run: echo gate}]\n  relay:\n    runs-on: ubuntu-latest\n    needs: gate\n"
			if tt.relay != "" {
				source += "    if: >-\n      " + tt.relay + "\n"
			}
			source += "    steps: [{run: echo relay}]\n  other:\n    runs-on: ubuntu-latest\n    steps: [{run: echo other}]\n" +
				"  check:\n    runs-on: ubuntu-latest\n    needs: " + needs + "\n"
			if tt.job != "" {
				source += "    if: >-\n      " + tt.job + "\n"
			}
			source += "    steps:\n      - uses: actions/checkout@v7\n        with: {" + tt.inputs + "ref: \"" + ref + "\"}\n"
			if tt.step != "" {
				source += "        if: >-\n          " + tt.step + "\n"
			}
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				input := ruleContractInput(t, source, target, false)
				diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), input)
				assert.Len(t, diagnostics, tt.count, "target %v", target)
			}
		})
	}
}

func TestPrivilegedCheckoutGeneratedMergedChain(t *testing.T) {
	source := `on: {pull_request_target: {types: [closed]}}
jobs:
  pre_activation:
    if: github.event.pull_request.merged == true
    runs-on: ubuntu-latest
    steps: [{run: echo activate}]
  activation:
    needs: pre_activation
    if: needs.pre_activation.outputs.activated == 'true'
    runs-on: ubuntu-latest
    steps: [{run: echo activate}]
  agent:
    needs: activation
    if: needs.activation.outputs.daily_ai_credits_exceeded != 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: {ref: '${{ github.event.pull_request.merge_commit_sha }}'}
  detection:
    needs: [activation, agent]
    if: always() && needs.agent.result != 'skipped'
    runs-on: ubuntu-latest
    steps: [{run: echo detect}]
  safe_outputs:
    needs: [activation, agent, detection]
    if: (!cancelled()) && needs.agent.result != 'skipped' && needs.detection.result == 'success'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        if: (!cancelled()) && needs.agent.result != 'skipped' && contains(needs.agent.outputs.output_types, 'create_pull_request') || (!cancelled()) && needs.agent.result != 'skipped' && contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')
        with: {ref: '${{ github.event.pull_request.merge_commit_sha }}'}
`
	for _, tt := range []struct {
		name, source string
		count        int
	}{
		{"generated chain", source, 0},
		{"removed merge guard", strings.ReplaceAll(source, "github.event.pull_request.merged == true", "true"), 2},
		{"agent bypasses activation", strings.Replace(source, "if: needs.activation.outputs.daily_ai_credits_exceeded != 'true'", "if: always()", 1), 2},
		{"safe outputs bypasses agent", strings.ReplaceAll(source, "(!cancelled()) && needs.agent.result != 'skipped'", "always()"), 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				input := ruleContractInput(t, tt.source, target, false)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), input)
				assert.Len(t, diagnostics, tt.count, "target %v", target)
				after, err := json.Marshal(input)
				require.NoError(t, err)
				assert.Equal(t, before, after, "guard proofs must not change either AST")
				assert.Equal(t, diagnostics, integrityTestRun(t, PrivilegedCheckoutRule(), input))
			}
		})
	}
	t.Run("Actions graph takes precedence over companion guard", func(t *testing.T) {
		input := ruleContractInput(t, strings.ReplaceAll(source, "github.event.pull_request.merged == true", "true"), ActionsAST, false)
		input.Document = ruleContractInput(t, source, AWAST, false).Document
		assert.Len(t, integrityTestRun(t, PrivilegedCheckoutRule(), input), 2)
	})
}

func TestPrivilegedCheckoutGuardCyclesAndCancellation(t *testing.T) {
	input := ruleContractInput(t, "on: pull_request_target\n"+contractJob+
		"    steps:\n      - uses: actions/checkout@v7\n        with: {ref: '${{ github.event.pull_request.merge_commit_sha }}'}\n", ActionsAST, false)
	input.Workflow.Jobs[0].Needs = []actions.JobID{"check"}
	assert.Len(t, integrityTestRun(t, PrivilegedCheckoutRule(), input), 1)
	input.Workflow.Jobs[0].Needs = []actions.JobID{"missing"}
	assert.Len(t, integrityTestRun(t, PrivilegedCheckoutRule(), input), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proof := privilegedMergeProof{ctx: ctx, input: input, active: make(map[string]bool), proven: make(map[string]bool)}
	_, err := proof.job("check")
	require.ErrorIs(t, err, context.Canceled)
}

func TestPrivilegedCheckoutUnknownGuardExpressions(t *testing.T) {
	for _, value := range []any{"github.event.pull_request.merged &&", "${{ github.event.pull_request.merged }} trailing", 123} {
		node, known := privilegedGuardExpression(value)
		assert.False(t, known)
		assert.False(t, privilegedRequiresMerged(node))
	}
}
