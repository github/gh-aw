package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestTargetRolesPolicy(t *testing.T) {
	const optOut = "features:\n  dangerously-allow-roles-read-and-pull-request-target: true\n"
	for _, tt := range []struct {
		name, source string
		severity     Severity
	}{
		{"read scalar", "on: {pull_request_target: null, roles: read}", Error},
		{"all scalar", "on: {pull_request_target: null, roles: all}", Error},
		{"mixed roles", "on: {pull_request_target: null, roles: [write, read, all]}", Error},
		{"acknowledged and disabled checkout", "on: {pull_request_target: {acknowledge-risk: true}, roles: read}\ncheckout: false", Error},
		{"strict opt-out cannot bypass", optOut + "on: {pull_request_target: null, roles: all}", Error},
		{"nonstrict warning", "strict: false\non: {pull_request_target: null, roles: read}", Warning},
		{"nonstrict suppression", "strict: false\n" + optOut + "on: {pull_request_target: null, roles: read}", ""},
		{"string does not suppress", "strict: false\nfeatures: {dangerously-allow-roles-read-and-pull-request-target: 'true'}\non: {pull_request_target: null, roles: read}", Warning},
		{"false does not suppress", "strict: false\nfeatures: {dangerously-allow-roles-read-and-pull-request-target: false}\non: {pull_request_target: null, roles: read}", Warning},
		{"case insensitive flag", "strict: false\nfeatures: {DANGEROUSLY-ALLOW-ROLES-READ-AND-PULL-REQUEST-TARGET: true}\non: {pull_request_target: null, roles: all}", ""},
		{"canonical flag wins", "strict: false\nfeatures: {dangerously-allow-roles-read-and-pull-request-target: false, DANGEROUSLY-ALLOW-ROLES-READ-AND-PULL-REQUEST-TARGET: true}\non: {pull_request_target: null, roles: read}", Warning},
		{"trusted roles", "on: {pull_request_target: null, roles: [admin, maintainer, write]}", ""},
		{"default roles not inferred", "on: {pull_request_target: null}", ""},
		{"imports do not hide explicit roles", "imports: [shared.md]\non: {pull_request_target: null, roles: read}", Error},
		{"pull request unaffected", "on: {pull_request: null, roles: all}", ""},
		{"exact trigger names", "on: {pull_request_target_staging: null, roles: all}", ""},
		{"scalar trigger", "on: pull_request_target", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := runThreatPolicy(t, "pull-request-target-roles", tt.source)
			if tt.severity == "" {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "on.roles", diagnostics[0].Path)
			assert.Equal(t, tt.severity, diagnostics[0].Severity)
		})
	}
}

func TestSafeOutputShellExpansionPolicy(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		paths        []string
		line         int
	}{
		{"command substitution", "safe-outputs:\n  steps:\n    - run: |\n        echo public\n        value=$(cat /tmp/value)\n", []string{"safe-outputs.steps[0].run"}, 2},
		{"indirection", "safe-outputs:\n  steps:\n    - run: echo ${!VALUE}\n", []string{"safe-outputs.steps[0].run"}, 1},
		{"parameter transform", "safe-outputs:\n  steps:\n    - run: echo ${VALUE@P}\n", []string{"safe-outputs.steps[0].run"}, 1},
		{"backticks", "safe-outputs:\n  steps:\n    - run: value=`cat /tmp/value`\n", []string{"safe-outputs.steps[0].run"}, 1},
		{"all steps", "safe-outputs:\n  steps:\n    - run: value=$(first)\n    - uses: local/action\n    - run: value=$(second)\n", []string{"safe-outputs.steps[0].run", "safe-outputs.steps[2].run"}, 1},
		{"nonstrict remains error", "strict: false\nsafe-outputs:\n  steps:\n    - run: value=$(first)\n", []string{"safe-outputs.steps[0].run"}, 1},
		{"arithmetic and variables", "safe-outputs:\n  steps:\n    - run: echo $((1+2)) $VALUE ${VALUE}\n", nil, 0},
		{"actions expressions", "safe-outputs:\n  steps:\n    - run: echo ${{ github.repository }}\n", nil, 0},
		{"unrelated execution unaffected", "steps:\n  - run: value=$(ordinary-step)\n", nil, 0},
		{"custom job unaffected", "jobs: {check: {steps: [{run: 'value=$(ordinary-step)'}]}}\n", nil, 0},
		{"action input is not script", "safe-outputs:\n  steps:\n    - uses: local/action\n      with: {text: '$(data)'}\n", nil, 0},
		{"empty steps", "safe-outputs: {steps: []}\n", nil, 0},
		{"omitted", "on: workflow_dispatch\n", nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := runThreatPolicy(t, "safe-output-shell-expansion", tt.source)
			var paths []string
			for _, diagnostic := range diagnostics {
				paths = append(paths, diagnostic.Path)
				assert.Equal(t, Error, diagnostic.Severity)
				assert.Equal(t, tt.line, diagnostic.Line)
			}
			assert.Equal(t, tt.paths, paths)
		})
	}
}

func TestFirewallSSLBumpPolicy(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		finding      bool
	}{
		{"missing prerequisite", "sandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", true},
		{"equals URL argument", "sandbox: {agent: {args: ['--allow-urls=https://example.com/path']}}", true},
		{"nonstrict remains error", "strict: false\nsandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", true},
		{"imports do not hide authored flags", "imports: [shared.md]\nsandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", true},
		{"malformed unrelated imports do not hide missing SSL", "imports: PRIVATE_VALUE\nsandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", true},
		{"network defaults do not hide args", "network: defaults\nsandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", true},
		{"enabled", "sandbox: {agent: {args: [--ssl-bump, --allow-urls, 'https://example.com/path']}}", false},
		{"enabled after filters", "sandbox: {agent: {args: [--allow-urls, 'https://example.com/path', --ssl-bump]}}", false},
		{"empty filters", "sandbox: {agent: {args: [--allow-urls, '']}}", false},
		{"empty equals filters", "sandbox: {agent: {args: ['--allow-urls=']}}", false},
		{"missing filter operand", "sandbox: {agent: {args: [--allow-urls]}}", false},
		{"missing operand before SSL flag", "sandbox: {agent: {args: [--allow-urls, --ssl-bump]}}", false},
		{"empty operand before another filter", "sandbox: {agent: {args: [--allow-urls, '', --allow-urls, 'https://example.com/path']}}", true},
		{"no filters", "sandbox: {agent: {args: [--log-level, debug]}}", false},
		{"empty args", "sandbox: {agent: {args: []}}", false},
		{"AWF shorthand", "sandbox: awf", false},
		{"default shorthand", "sandbox: default", false},
		{"agent shorthand", "sandbox: {agent: awf}", false},
		{"disabled sandbox", "sandbox: {agent: false}", false},
		{"MCP args are not AWF args", "sandbox: {mcp: {args: [--allow-urls, 'https://example.com/path']}}", false},
		{"option terminator", "sandbox: {agent: {args: [--, --allow-urls, 'https://example.com/path']}}", false},
		{"SSL after terminator does not satisfy filters", "sandbox: {agent: {args: [--allow-urls, 'https://example.com/path', --, --ssl-bump]}}", true},
		{"flag substring is not a flag", "sandbox: {agent: {args: ['--label=--allow-urls=https://example.com/path']}}", false},
		{"SSL substring does not enable inspection", "sandbox: {agent: {args: ['--label=--ssl-bump', --allow-urls, 'https://example.com/path']}}", true},
		{"defaults shorthand", "network: defaults", false},
		{"explicit refusal", "sandbox: {agent: {args: ['--ssl-bump=false', '--allow-urls', 'https://example.com/path']}}", true},
		{"explicit true", "sandbox: {agent: {args: ['--allow-urls=https://example.com/path', '--ssl-bump=true']}}", false},
		{"later SSL disablement", "sandbox: {agent: {args: [--ssl-bump, --allow-urls, 'https://example.com/path', '--ssl-bump=false']}}", true},
		{"later SSL enablement", "sandbox: {agent: {args: ['--ssl-bump=false', --allow-urls, 'https://example.com/path', '--ssl-bump=true']}}", false},
		{"omitted", "on: workflow_dispatch", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := runThreatPolicy(t, "firewall-ssl-bump", tt.source)
			if !tt.finding {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "sandbox.agent.args", diagnostics[0].Path)
			assert.Equal(t, Error, diagnostics[0].Severity)
		})
	}
}

func TestFirewallSSLBumpLegacyMigration(t *testing.T) {
	for _, legacy := range []string{
		"false", "null", "PRIVATE_VALUE", "{}", "{allow-urls: ['PRIVATE_VALUE']}",
		"{allow-urls: ['PRIVATE_VALUE'], ssl-bump: true}",
	} {
		t.Run(legacy, func(t *testing.T) {
			diagnostics := runThreatPolicy(t, "firewall-ssl-bump", "network: {firewall: "+legacy+"}")
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "network.firewall", diagnostics[0].Path)
			assert.Equal(t, Error, diagnostics[0].Severity)
			assert.Contains(t, diagnostics[0].Message, "unsupported")
			assert.Contains(t, diagnostics[0].Message, "remove this legacy block")
			assert.NotContains(t, diagnostics[0].Message, "network.firewall.ssl-bump")
			assert.NotContains(t, diagnostics[0].Message, "PRIVATE_VALUE")
		})
	}
}

func runThreatPolicy(t *testing.T, id, source string) []Diagnostic {
	t.Helper()
	document, err := aw.Parse("---\n" + source + "\n---\n")
	require.NoError(t, err)
	runner, err := Default().Select(id)
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: document})
	require.NoError(t, err)
	return diagnostics
}

func TestThreatPolicyFailuresAndValueFreeDiagnostics(t *testing.T) {
	for _, tt := range []struct{ rule, fields string }{
		{"pull-request-target-roles", "strict: PRIVATE_VALUE\non: {pull_request_target: null, roles: read}"},
		{"pull-request-target-roles", "on: {pull_request_target: null, roles: [read, {key: PRIVATE_VALUE}]}"},
		{"pull-request-target-roles", "strict: false\nfeatures: PRIVATE_VALUE\non: {pull_request_target: null, roles: read}"},
		{"safe-output-shell-expansion", "safe-outputs: {steps: PRIVATE_VALUE}"},
		{"safe-output-shell-expansion", "safe-outputs: {steps: [PRIVATE_VALUE, {run: '$(PRIVATE_VALUE)'}]}"},
		{"safe-output-shell-expansion", "safe-outputs: {steps: [{run: [PRIVATE_VALUE]}]}"},
		{"safe-output-credentials", "safe-outputs: {steps: PRIVATE_VALUE}"},
		{"safe-output-credentials", "safe-outputs: {steps: [PRIVATE_VALUE, {run: 'echo >> $GITHUB_ENV'}]}"},
		{"firewall-ssl-bump", "sandbox: PRIVATE_VALUE"},
		{"firewall-ssl-bump", "sandbox: {agent: [PRIVATE_VALUE]}"},
		{"firewall-ssl-bump", "sandbox: {agent: {args: PRIVATE_VALUE}}"},
		{"firewall-ssl-bump", "sandbox: {agent: {args: ['PRIVATE_VALUE', 7]}}"},
		{"firewall-ssl-bump", "sandbox: {agent: {args: ['--allow-urls', {value: PRIVATE_VALUE}]}}"},
	} {
		t.Run(tt.rule+"/"+tt.fields, func(t *testing.T) {
			document, err := aw.Parse("---\n" + tt.fields + "\n---\n")
			require.NoError(t, err)
			runner, err := Default().Select(tt.rule)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: document})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			for _, diagnostic := range diagnostics {
				assert.NotContains(t, diagnostic.Message, "PRIVATE_VALUE")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = runner.Run(ctx, Input{Document: document})
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestThreatPolicySourceEvidence(t *testing.T) {
	runner, err := Default().Select("pull-request-target-roles", "safe-output-shell-expansion", "firewall-ssl-bump")
	require.NoError(t, err)
	workflow, err := actions.Parse([]byte("on: pull_request_target\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: value=$(ordinary-step)\n"))
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "native Actions cannot imply AW safe-output or admission policy")
	document, err := aw.Parse("---\non: {pull_request_target: null, roles: read}\nsafe-outputs: {steps: [{run: '$(PRIVATE_VALUE)'}]}\nsandbox: {agent: {args: [--allow-urls, 'PRIVATE_VALUE']}}\n---\n")
	require.NoError(t, err)
	diagnostics, err = runner.Run(context.Background(), Input{Workflow: workflow, Document: document})
	require.NoError(t, err)
	require.Len(t, diagnostics, 3)
	for _, diagnostic := range diagnostics {
		assert.NotContains(t, diagnostic.Message, "PRIVATE_VALUE")
	}
}

func TestRoleWarningSuppressionIsScoped(t *testing.T) {
	document, err := aw.Parse("---\nstrict: false\nfeatures: {dangerously-allow-roles-read-and-pull-request-target: true}\non: {pull_request_target: null, roles: read}\nsafe-outputs: {steps: [{run: '$(PRIVATE_VALUE)'}]}\nsandbox: {agent: {args: [--allow-urls, 'PRIVATE_VALUE']}}\n---\n")
	require.NoError(t, err)
	runner, err := Default().Select("pull-request-target-roles", "safe-output-shell-expansion", "firewall-ssl-bump")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: document})
	require.NoError(t, err)
	require.Len(t, diagnostics, 2, "the role opt-out must not suppress other rule findings")
	for _, diagnostic := range diagnostics {
		assert.NotEqual(t, "pull-request-target-roles", diagnostic.RuleID)
		assert.Equal(t, Error, diagnostic.Severity)
	}
}
