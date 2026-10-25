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

type ruleContract struct {
	targets       ASTTargets
	threatRuleID  string
	unsafe        string
	safe          string
	path          string
	severity      Severity
	strictPolicy  bool
	javaScript    bool
	unsafePrompt  string
	safePrompt    string
	unsafeShell   string
	safeShell     string
	actionsUnsafe string
	actionsSafe   string
	actionsPath   string
}

const contractJob = "jobs:\n  check:\n    runs-on: ubuntu-latest\n"
const contractSafeSteps = "    steps:\n      - shell: bash\n        run: echo public\n"

func builtInRuleContracts() map[string]ruleContract {
	return map[string]ruleContract{
		"bash-case-pattern": {
			targets: ActionsAST | AWAST, severity: Warning,
			unsafe: contractJob + "    steps:\n      - run: 'matched=$(case \"$ref\" in $pattern) echo yes ;; esac)'\n",
			safe:   contractJob + "    steps:\n      - run: 'matched=$(case \"$ref\" in ($pattern) echo yes ;; esac)'\n",
			path:   "jobs.check.steps[0].run",
		},
		"needs-consistency": {
			targets: ActionsAST | AWAST, severity: Warning,
			unsafe: "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo build}]\n  check:\n    runs-on: ubuntu-latest\n    if: needs.build.result == 'success'\n    steps: [{run: echo check}]\n",
			safe:   "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo build}]\n  check:\n    runs-on: ubuntu-latest\n    needs: build\n    if: needs.build.result == 'success'\n    steps: [{run: echo check}]\n",
			path:   "jobs.check.if",
		},
		"gateway-dispatch-literals": {
			targets: AWAST,
			unsafe:  "on: {gateway-dispatch: {issue: {filters: {labels: ['${{ vars.PRIVATE_VALUE }}']}}}}\n",
			safe:    "on: {gateway-dispatch: {issue: {filters: {labels: [triage]}}}}\n",
			path:    "on.gateway-dispatch.issue.filters.labels[0]",
		},
		"artipacked": {
			targets: ActionsAST | AWAST, severity: Warning,
			unsafe: contractJob + "    steps:\n      - uses: actions/checkout@v6\n",
			safe:   contractJob + "    steps:\n      - uses: actions/checkout@v6\n        with:\n          persist-credentials: false\n",
			path:   "jobs.check.steps[0].uses",
		},
		"cache-poisoning": {
			targets: ActionsAST | AWAST, severity: Warning,
			unsafe: "on: release\n" + contractJob + "    steps:\n      - uses: actions/setup-go@v6\n",
			safe:   "on: release\n" + contractJob + "    steps:\n      - uses: actions/setup-go@v6\n        with:\n          cache: false\n",
			path:   "jobs.check.steps[0].uses",
		},
		"excessive-permissions": {
			targets: ActionsAST, severity: Warning, threatRuleID: "CTR-001",
			unsafe: "permissions: {contents: read, issues: write}\n" + contractJob + contractSafeSteps,
			safe:   "permissions: {contents: read}\n" + contractJob + "    permissions: {issues: write}\n" + contractSafeSteps,
			path:   "permissions.issues",
		},
		"obfuscation": {
			targets: ActionsAST | AWAST, severity: Warning,
			unsafe: contractJob + "    outputs: {comment_id: \"${{ '' }}\"}\n" + contractSafeSteps,
			safe:   contractJob + "    outputs: {comment_id: ''}\n" + contractSafeSteps,
			path:   "jobs.check.outputs.comment_id",
		},
		"grader-shell-file-write": {
			targets:     AWAST,
			unsafe:      "graders:\n  operational-value:\n    run: ./check.sh\n",
			safe:        "graders:\n  operational-value:\n    run: ./check.sh\n",
			unsafeShell: "#!/bin/bash\nprintf 1 > PRIVATE_FILE\n",
			safeShell:   "#!/bin/bash\njq '.events | length' \"$GH_AW_SESSION\"\n",
			path:        "graders.operational-value.run",
		},
		"grader-file-io": {
			targets: AWAST, javaScript: true,
			unsafe: "graders:\n  custom:\n    script: const fs = require('node:fs'); return fs.readFileSync('evidence');\n",
			safe:   "graders:\n  custom:\n    script: return helpers.createSessionLookups(session).stats.toolCalls;\n",
			path:   "graders.custom.script",
		},
		"custom-job-detection": {
			threatRuleID:  "CTR-005",
			targets:       ActionsAST | AWAST,
			unsafe:        contractJob + "    needs: agent\n" + contractSafeSteps,
			safe:          contractJob + "    needs: [agent, detection]\n" + contractSafeSteps,
			path:          "jobs.check.needs",
			actionsUnsafe: "jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps: [{run: echo agent}]\n  detection:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps: [{run: echo detection}]\n  check:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps: [{run: echo check}]\n",
			actionsSafe:   "jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps: [{run: echo agent}]\n  detection:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps: [{run: echo detection}]\n  check:\n    needs: [agent, detection]\n    runs-on: ubuntu-latest\n    steps: [{run: echo check}]\n",
		},
		"secret-leak": {
			threatRuleID: "CTR-017",
			targets:      ActionsAST | AWAST,
			unsafe:       contractJob + "    env:\n      TOKEN: '${{ secrets.CONTRACT_SECRET }}'\n    steps:\n      - shell: bash\n        run: echo \"$TOKEN\"\n",
			safe:         contractJob + "    env:\n      TOKEN: '${{ secrets.CONTRACT_SECRET }}'\n    steps:\n      - shell: bash\n        run: test -n \"$TOKEN\"\n",
			path:         "jobs.check.steps[0].run",
		},
		"otlp-secret-leak": {
			threatRuleID: "CTR-017",
			targets:      ActionsAST | AWAST,
			unsafe:       "observability:\n  otlp:\n    attributes: {account: '${{ secrets.CONTRACT_SECRET }}'}\n",
			safe:         "observability:\n  otlp:\n    attributes: {account: '${{ github.repository }}'}\n",
			path:         "observability.otlp.attributes.account",
			actionsUnsafe: "env:\n  GH_AW_OTLP_ATTRIBUTES: '{\"account\":\"${{ secrets.CONTRACT_SECRET }}\"}'\n" +
				contractJob + contractSafeSteps,
			actionsSafe: "env:\n  GH_AW_OTLP_ATTRIBUTES: '{\"note\":\"${{ ''secrets.CONTRACT_SECRET'' }}\"}'\n" +
				contractJob + contractSafeSteps,
			actionsPath: "env.GH_AW_OTLP_ATTRIBUTES",
		},
		"action-pin": {
			threatRuleID: "CTR-002",
			targets:      ActionsAST | AWAST, strictPolicy: true,
			unsafe: contractJob + "    steps:\n      - uses: actions/checkout@v4\n",
			safe:   contractJob + "    steps:\n      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567\n",
			path:   "jobs.check.steps[0].uses",
		},
		"job-timeout": {
			threatRuleID: "CTR-026",
			targets:      ActionsAST | AWAST,
			unsafe:       contractJob + "    timeout-minutes: '${{ inputs.timeout }}'\n" + contractSafeSteps,
			safe:         contractJob + "    timeout-minutes: 15\n" + contractSafeSteps,
			path:         "jobs.check.timeout-minutes",
		},
		"template-injection": {
			threatRuleID: "CTR-006",
			targets:      ActionsAST | AWAST,
			unsafe:       contractJob + "    steps:\n      - shell: bash\n        run: echo '${{ github.event.issue.title }}'\n",
			safe:         contractJob + "    steps:\n      - shell: bash\n        env:\n          TITLE: '${{ github.event.issue.title }}'\n        run: printf '%s\\n' \"$TITLE\"\n",
			path:         "jobs.check.steps[0].run",
		},
		"workflow-run-scope": {
			threatRuleID: "CTR-021",
			targets:      ActionsAST | AWAST, strictPolicy: true,
			unsafe: "on:\n  workflow_run:\n    workflows: [CI]\n    branches: ['*']\n" + contractJob + contractSafeSteps,
			safe:   "on:\n  workflow_run:\n    workflows: [CI]\n    branches: [main]\n" + contractJob + contractSafeSteps,
			path:   "on.workflow_run.branches",
		},
		"permission-scope": {
			threatRuleID: "CTR-001",
			targets:      ActionsAST | AWAST, strictPolicy: true,
			unsafe: "permissions: write-all\n" + contractJob + contractSafeSteps,
			safe:   "permissions:\n  contents: read\n  id-token: write\n" + contractJob + contractSafeSteps,
			path:   "permissions",
		},
		"version-check": {
			threatRuleID: "CTR-018",
			targets:      AWAST, strictPolicy: true,
			unsafe: "check-for-updates: false\n",
			safe:   "check-for-updates: true\n",
			path:   "check-for-updates",
		},
		"conditional-import": {
			threatRuleID: "CTR-020",
			targets:      AWAST,
			unsafe:       "imports:\n  - path: shared.md\n    if: false\n",
			safe:         "imports:\n  - path: shared.md\n    with:\n      if: false\n",
			path:         "imports[0].if",
		},
		"private-to-public": {
			threatRuleID: "CTR-031",
			targets:      AWAST, strictPolicy: true,
			unsafe: "tools:\n  github:\n    private-to-public-flows: allow\n",
			safe:   "tools:\n  github:\n    mode: local\n",
			path:   "tools.github.private-to-public-flows",
		},
		"install-scripts": {
			threatRuleID: "CTR-014",
			targets:      AWAST, strictPolicy: true,
			unsafe: "runtimes:\n  node:\n    run-install-scripts: true\n",
			safe:   "runtimes:\n  node:\n    run-install-scripts: false\n",
			path:   "runtimes.node.run-install-scripts",
		},
		"allowed-labels": {
			threatRuleID: "CTR-015",
			targets:      AWAST,
			unsafe:       "safe-outputs:\n  create-issue:\n    allowed-labels: ['*']\n",
			safe:         "safe-outputs:\n  create-issue:\n    allowed-labels: ['team-*']\n",
			path:         "safe-outputs.create-issue.allowed-labels",
		},
		"network-scope": {
			threatRuleID: "CTR-011",
			targets:      AWAST, strictPolicy: true,
			unsafe: "network:\n  allowed: [defaults, '*']\n",
			safe:   "network:\n  allowed: [defaults, '*.example.com']\n",
			path:   "network.allowed",
		},
		"push-target": {
			threatRuleID: "CTR-012",
			targets:      AWAST, severity: Warning,
			unsafe: "safe-outputs:\n  push-to-pull-request-branch:\n    target: '*'\n",
			safe:   "safe-outputs:\n  push-to-pull-request-branch:\n    target: '*'\n    required-labels: [automated]\n",
			path:   "safe-outputs.push-to-pull-request-branch.target",
		},
		"tool-scope": {
			threatRuleID: "CTR-003",
			targets:      AWAST, strictPolicy: true,
			unsafe: "tools:\n  bash: false\n  cli-proxy: true\n",
			safe:   "tools:\n  bash: false\n  cli-proxy: false\n",
			path:   "tools.cli-proxy",
		},
		"sandbox-config": {
			threatRuleID: "CTR-004",
			targets:      AWAST, strictPolicy: true,
			unsafe: "sandbox:\n  agent: false\n",
			safe:   "sandbox:\n  agent: awf\n",
			path:   "sandbox.agent",
		},
		"image-argument": {
			threatRuleID: "CTR-013",
			targets:      ActionsAST | AWAST,
			unsafe:       contractJob + "    container:\n      image: '--contract-image-option'\n" + contractSafeSteps,
			safe:         contractJob + "    container:\n      image: 'registry.example/app:1'\n" + contractSafeSteps,
			path:         "jobs.check.container.image",
		},
		"checkout-mode": {
			threatRuleID: "CTR-033",
			targets:      AWAST,
			unsafe:       "checkout:\n  multi: true\n  path: repositories\n  allowed-repos: ['*/*']\n",
			safe:         "checkout:\n  multi: true\n  path: repositories\n  allowed-repos: ['owner/*']\n",
			path:         "checkout.allowed-repos",
		},
		"fork-association": {
			threatRuleID: "CTR-034",
			targets:      AWAST,
			unsafe:       "on:\n  pull_request:\n    forks:\n      author-association: [owner]\n",
			safe:         "on:\n  pull_request:\n    forks:\n      author-association: [OWNER]\n",
			path:         "on.pull_request.forks.author-association",
		},
		"pull-request-target-roles": {
			threatRuleID: "CTR-008",
			targets:      AWAST, strictPolicy: true,
			unsafe: "on:\n  pull_request_target:\n  roles: [read]\ncheckout: false\n",
			safe:   "on:\n  pull_request_target:\n  roles: [write]\ncheckout: false\n",
			path:   "on.roles",
		},
		"safe-output-shell-expansion": {
			threatRuleID: "CTR-009",
			targets:      AWAST,
			unsafe:       "safe-outputs:\n  steps:\n    - run: value=$(cat /tmp/value)\n",
			safe:         "safe-outputs:\n  steps:\n    - run: cat /tmp/value\n",
			path:         "safe-outputs.steps[0].run",
		},
		"firewall-ssl-bump": {
			threatRuleID: "CTR-011",
			targets:      AWAST,
			unsafe:       "strict: false\nsandbox:\n  agent:\n    args: [--allow-urls, https://example.com/path]\n",
			safe:         "strict: false\nsandbox:\n  agent:\n    args: [--ssl-bump, --allow-urls, https://example.com/path]\n",
			path:         "sandbox.agent.args",
		},
		"pull-request-target-checkout": {
			threatRuleID: "CTR-008",
			targets:      AWAST, strictPolicy: true,
			unsafe: "on: pull_request_target\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}\n",
			safe:   "on: pull_request_target\ncheckout: {ref: '${{ github.event.pull_request.base.sha }}'}\n",
			path:   "checkout",
		},
		"prompt-expression": {
			threatRuleID: "CTR-010",
			targets:      AWAST,
			unsafePrompt: "${{\n github.repository\n}}",
			safePrompt:   "${{ github.repository }}",
			path:         "prompt",
		},
	}
}

func TestEveryBuiltInRulePositiveAndNegative(t *testing.T) {
	t.Parallel()
	contracts := builtInRuleContracts()
	rules := Default().Rules()
	require.Len(t, contracts, len(rules), "every registered rule needs a positive and negative contract")
	for _, rule := range rules {
		t.Run(rule.ID, func(t *testing.T) {
			t.Parallel()
			contract, exists := contracts[rule.ID]
			require.True(t, exists, "add a contract for the newly registered rule")
			require.Equal(t, contract.targets, rule.Targets, "each supported AST needs positive and negative coverage")
			require.Equal(t, contract.threatRuleID, rule.ThreatRuleID, "known compiler threat mappings must remain explicit")
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				if contract.targets&target == 0 {
					continue
				}
				t.Run(target.String(), func(t *testing.T) {
					runRuleContractPair(t, rule.ID, contract, target, false)
				})
			}
			if contract.targets&AWAST != 0 {
				t.Run("aw-nonstrict", func(t *testing.T) {
					runRuleContractPair(t, rule.ID, contract, AWAST, true)
				})
			}
		})
	}
}

func runRuleContractPair(t *testing.T, id string, contract ruleContract, target ASTTargets, nonstrict bool) {
	t.Helper()
	for _, test := range []struct {
		name, source, prompt string
		finding              bool
	}{
		{"positive", contractSource(contract, target, true), contract.unsafePrompt, true},
		{"negative", contractSource(contract, target, false), contract.safePrompt, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := ruleContractInput(t, test.source, target, nonstrict)
			if contract.safeShell != "" {
				source := contract.safeShell
				if test.finding {
					source = contract.unsafeShell
				}
				input = prepareGraderShellFixture(t, input, source)
			}
			if contract.javaScript {
				input.JavaScript = nativeGraderJavaScriptOptions(t)
			}
			if input.Document != nil && test.prompt != "" {
				input.Document.Markdown = test.prompt
			}
			before, err := json.Marshal(input)
			require.NoError(t, err)
			runner, err := Default().Select(id)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err, "fixtures must exercise rules, not fail parsing or traversal")
			after, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, before, after, "rules must not mutate either AST")
			assertRuleContractDiagnostics(t, id, contract, target, nonstrict, test.finding, diagnostics)
			again, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			assert.Equal(t, diagnostics, again, "results must be repeatable")
		})
	}
}

func ruleContractInput(t *testing.T, source string, target ASTTargets, nonstrict bool) Input {
	t.Helper()
	if target == ActionsAST {
		workflow, err := actions.Parse([]byte(source))
		require.NoError(t, err)
		return Input{Workflow: workflow}
	}
	document, err := aw.Parse("---\n" + source + "---\nPublic contract-test prompt.\n")
	require.NoError(t, err)
	if nonstrict {
		document.Frontmatter["strict"] = false
	}
	return Input{Document: document}
}

func assertRuleContractDiagnostics(t *testing.T, id string, contract ruleContract, target ASTTargets, nonstrict, positive bool, diagnostics []Diagnostic) {
	t.Helper()
	want := make(map[string]Severity)
	if id == "secret-leak" && target == AWAST {
		// AW-only analysis must retain its generated-execution coverage gap,
		// including in the negative case; it is not a secret disclosure.
		want["workflow"] = Warning
	}
	if positive {
		severity := contract.severity
		if severity == "" {
			severity = Error
		}
		if contract.strictPolicy && (nonstrict || target == ActionsAST) {
			severity = Warning
		}
		path := contract.path
		if target == ActionsAST && contract.actionsPath != "" {
			path = contract.actionsPath
		}
		want[path] = severity
	}

	require.Len(t, diagnostics, len(want), "positive cases must detect the violation; negative cases must reject false positives")
	for _, diagnostic := range diagnostics {
		severity, expected := want[diagnostic.Path]
		require.True(t, expected, "unexpected diagnostic at %s", diagnostic.Path)
		assert.Equal(t, id, diagnostic.RuleID)
		assert.Equal(t, contract.threatRuleID, diagnostic.ThreatRuleID)
		assert.Equal(t, severity, diagnostic.Severity)
		assert.NotEmpty(t, diagnostic.Message)
		if diagnostic.Path == "workflow" {
			assert.Equal(t, "Incomplete secret-leak analysis: only authored AW execution nodes are available; imports, generated steps, cross-job flows and compiler defaults require the compiled Actions AST", diagnostic.Message)
		}
		delete(want, diagnostic.Path)
	}
	assert.Empty(t, want, "every expected diagnostic must be emitted")
}

func contractSource(contract ruleContract, target ASTTargets, positive bool) string {
	if target == ActionsAST && contract.actionsUnsafe != "" {
		if positive {
			return contract.actionsUnsafe
		}
		return contract.actionsSafe
	}
	if positive {
		return contract.unsafe
	}
	return contract.safe
}
