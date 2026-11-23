//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWorkflowLintCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := actionlintTestDir(t)
	script := `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "--version" ]; then
  printf '1.7.12\n'
  exit 0
fi
printf '%s\n' "$@" > "$ACTIONLINT_TEST_ARGS"
printf '[]\n'
exit "$ACTIONLINT_TEST_EXIT"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actionlint"), []byte(script), 0o755))
	t.Setenv("PATH", dir)
	originalVersion, originalStats := actionlintVersion, actionlintStats
	actionlintVersion, actionlintStats = "", nil
	t.Cleanup(func() { actionlintVersion, actionlintStats = originalVersion, originalStats })
	for _, tt := range []struct {
		name           string
		run            string
		exit           string
		flags          []string
		wantASTError   bool
		wantASTWarning bool
		wantToolError  bool
		wantRule       string
	}{
		{name: "safe", run: "echo safe", exit: "0"},
		{name: "secret disclosure", run: "echo '${{ secrets.TOKEN }}'", exit: "0", wantASTError: true},
		{name: "actionlint failure", run: "echo safe", exit: "2", wantToolError: true},
		{name: "both fail", run: "echo '${{ secrets.TOKEN }}'", exit: "2", wantASTError: true, wantToolError: true},
		{name: "coverage warning hidden", run: "unknown '${{ secrets.TOKEN }}'", exit: "0"},
		{name: "debug coverage warning", run: "unknown '${{ secrets.TOKEN }}'", exit: "0", flags: []string{"--debug"}, wantASTWarning: true},
		{name: "script integrations", run: "echo '${{ secrets.TOKEN }}'", exit: "0", flags: []string{"--shellcheck", "--pyflakes"}, wantASTError: true},
		{name: "template injection", run: "echo '${{ github.event.issue.title }}'", exit: "0", wantASTError: true, wantRule: "template-injection"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(dir, tt.name+".lock.yml")
			require.NoError(t, os.WriteFile(file, []byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: "+tt.run+"\n"), 0o600))
			argsPath := file + ".args"
			t.Setenv("ACTIONLINT_TEST_ARGS", argsPath)
			t.Setenv("ACTIONLINT_TEST_EXIT", tt.exit)
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(append(tt.flags, file))
			var runErr error
			testutil.CaptureStderr(t, func() { runErr = cmd.Execute() })
			if tt.wantASTError {
				require.ErrorContains(t, runErr, "workflow lint found 1 error")
				ruleID := tt.wantRule
				if ruleID == "" {
					ruleID = "secret-leak"
				}
				assert.Contains(t, output.String(), "error ["+ruleID+"]")
			}
			if tt.wantASTWarning {
				assert.Contains(t, output.String(), "warning [secret-leak]")
			} else {
				assert.NotContains(t, output.String(), "warning [secret-leak]")
			}
			if tt.wantToolError {
				require.ErrorContains(t, runErr, "actionlint failed with exit code 2")
			}
			if !tt.wantASTError && !tt.wantToolError {
				require.NoError(t, runErr)
			}
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err, "actionlint must run even when AST lint fails")
			if !slices.Contains(tt.flags, "--shellcheck") {
				assert.Contains(t, string(args), "-shellcheck=\n")
				assert.Contains(t, string(args), "-pyflakes=\n")
			} else {
				assert.NotContains(t, string(args), "-shellcheck=")
				assert.NotContains(t, string(args), "-pyflakes=")
			}
		})
	}
}

func TestBuiltInThreatRulesCLI(t *testing.T) {
	for _, tt := range []struct {
		name     string
		workflow string
		ruleID   string
		severity workflowlint.Severity
	}{
		{
			name:     "mutable action",
			workflow: "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@main\n        with:\n          persist-credentials: false\n",
			ruleID:   "action-pin", severity: workflowlint.Warning,
		},
		{
			name:     "expression timeout",
			workflow: "jobs:\n  test:\n    runs-on: ubuntu-latest\n    timeout-minutes: ${{ inputs.timeout }}\n    steps:\n      - run: echo safe\n",
			ruleID:   "job-timeout", severity: workflowlint.Error,
		},
		{
			name:     "direct template interpolation",
			workflow: "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ github.event.issue.title }}'\n",
			ruleID:   "template-injection", severity: workflowlint.Error,
		},
		{
			name:     "missing workflow selection",
			workflow: "on:\n  workflow_run:\n    branches: [main]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n",
			ruleID:   "workflow-run-scope", severity: workflowlint.Error,
		},
		{
			name:     "blanket write scopes",
			workflow: "permissions: write-all\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n",
			ruleID:   "permission-scope", severity: workflowlint.Warning,
		},
		{
			name:     "invalid OIDC read grant",
			workflow: "permissions:\n  id-token: read\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n",
			ruleID:   "permission-scope", severity: workflowlint.Error,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "test.lock.yml")
			require.NoError(t, os.WriteFile(file, []byte(tt.workflow), 0o600))
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=", "--json", file})
			err := cmd.Execute()
			if tt.severity == workflowlint.Error {
				require.ErrorContains(t, err, "workflow lint found 1 error")
			} else {
				require.NoError(t, err)
			}
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.ruleID, diagnostics[0].RuleID)
			assert.Equal(t, tt.severity, diagnostics[0].Severity)
			assert.Equal(t, file, diagnostics[0].File)
		})
	}
}

func TestNeedsConsistencyCLI(t *testing.T) {
	const source = "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo build}]\n  check:\n    runs-on: ubuntu-latest\n    if: needs.build.result == 'success'\n    steps: [{run: echo check}]\n"
	for _, extension := range []string{".yml", ".md"} {
		t.Run(extension, func(t *testing.T) {
			content := source
			if extension == ".md" {
				content = "---\n" + source + "---\nA prompt.\n"
			}
			file := filepath.Join(t.TempDir(), "workflow"+extension)
			require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=needs-consistency", "--json", file})
			require.NoError(t, cmd.Execute())
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "needs-consistency", diagnostics[0].RuleID)
			assert.Equal(t, workflowlint.Warning, diagnostics[0].Severity)
			assert.Equal(t, "jobs.check.if", diagnostics[0].Path)
			assert.Equal(t, file, diagnostics[0].File)
		})
	}
}

func TestActionPinFormattingCLI(t *testing.T) {
	const source = "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: \"./action\\u2028PRIVATE_PIN\"\n"
	for _, extension := range []string{".yml", ".md"} {
		t.Run(extension, func(t *testing.T) {
			content := source
			if extension == ".md" {
				content = "---\nstrict: false\n" + source + "---\n"
			}
			file := filepath.Join(t.TempDir(), "workflow"+extension)
			require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=action-pin", "--json", file})
			require.ErrorContains(t, cmd.Execute(), "workflow lint found 1 error")
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "action-pin", diagnostics[0].RuleID)
			assert.Equal(t, workflowlint.Error, diagnostics[0].Severity)
			assert.Equal(t, "jobs.check.steps[0].uses", diagnostics[0].Path)
			assert.NotContains(t, output.String(), "PRIVATE_PIN")
		})
	}
}

func TestBuiltInSourcePolicyRulesCLI(t *testing.T) {
	for _, tt := range []struct {
		ruleID string
		source string
		path   string
		level  workflowlint.Severity
	}{
		{"version-check", "strict: false\ncheck-for-updates: false", "check-for-updates", workflowlint.Warning},
		{"conditional-import", "imports:\n  - path: shared/a.md\n    if: false", "imports[0].if", workflowlint.Error},
		{"gateway-dispatch-literals", "strict: false\non: {gateway-dispatch: {issue: {filters: {labels: ['${{ vars.PRIVATE_VALUE }}']}}}}", "on.gateway-dispatch.issue.filters.labels[0]", workflowlint.Error},
		{"private-to-public", "tools:\n  github:\n    private-to-public-flows: allow", "tools.github.private-to-public-flows", workflowlint.Error},
		{"install-scripts", "runtimes:\n  node:\n    run-install-scripts: true", "runtimes.node.run-install-scripts", workflowlint.Error},
		{"allowed-labels", "safe-outputs:\n  create-issue:\n    allowed-labels: ['*']", "safe-outputs.create-issue.allowed-labels", workflowlint.Error},
		{"network-scope", "network:\n  allowed: [defaults, '*']", "network.allowed", workflowlint.Error},
		{"push-target", "safe-outputs:\n  push-to-pull-request-branch:\n    target: '*'", "safe-outputs.push-to-pull-request-branch.target", workflowlint.Warning},
		{"pull-request-target-roles", "on: {pull_request_target: null, roles: read}", "on.roles", workflowlint.Error},
		{"safe-output-shell-expansion", "safe-outputs: {steps: [{run: '$(PRIVATE_VALUE)'}]}", "safe-outputs.steps[0].run", workflowlint.Error},
		{"safe-output-credentials", "safe-outputs: {steps: [{run: 'echo A=1 >> $GITHUB_ENV'}]}", "safe-outputs.steps[0].run", workflowlint.Error},
		{"firewall-ssl-bump", "strict: false\nsandbox: {agent: {args: [--allow-urls, 'https://example.com/path']}}", "sandbox.agent.args", workflowlint.Error},
		{"firewall-ssl-bump", "network: {firewall: {allow-urls: ['https://example.com/path'], ssl-bump: true}}", "network.firewall", workflowlint.Error},
		{"pull-request-target-checkout", "on: pull_request_target\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}", "checkout", workflowlint.Error},
	} {
		t.Run(tt.ruleID, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "test.lock.yml")
			require.NoError(t, os.WriteFile(file, []byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\n"+tt.source+"\n---\nA prompt.\n"), 0o600))
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=" + tt.ruleID, "--json", file})
			err := cmd.Execute()
			if tt.level == workflowlint.Error {
				require.ErrorContains(t, err, "workflow lint found 1 error")
			} else {
				require.NoError(t, err)
			}
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.ruleID, diagnostics[0].RuleID)
			assert.Equal(t, tt.path, diagnostics[0].Path)
			assert.Equal(t, tt.level, diagnostics[0].Severity)
		})
	}
}

func TestDangerousTriggersIntegrityCLI(t *testing.T) {
	t.Parallel()
	for _, trigger := range []string{"issue_comment", "pull_request_review", "pull_request_review_comment"} {
		for _, policy := range []struct {
			name, roles, threshold string
			warning                bool
		}{
			{"default roles", "", "none", false},
			{"trusted roles", ", roles: [admin, maintain, write]", "none", false},
			{"all roles", ", roles: all", "none", true},
			{"read role", ", roles: read", "none", true},
			{"triage role", ", roles: [write, triage]", "unapproved", true},
			{"approved", ", roles: all", "approved", false},
			{"unknown filtering", ", roles: all", "", false},
		} {
			for _, extension := range []string{".md", ".lock.yml"} {
				t.Run(trigger+"/"+policy.name+extension, func(t *testing.T) {
					dir := t.TempDir()
					threshold := ""
					if policy.threshold != "" {
						threshold = "min-integrity: " + policy.threshold
					}
					source := "---\non: {" + trigger + ": null" + policy.roles + "}\ntools: {github: {" + threshold + "}}\n---\nReview.\n"
					require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte(source), 0o600))
					file := filepath.Join(dir, "test"+extension)
					if extension != ".md" {
						graph := "on: {" + trigger + ": null}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo safe}]\n"
						require.NoError(t, os.WriteFile(file, []byte(graph), 0o600))
					}
					cmd := NewLintCommand()
					var output bytes.Buffer
					cmd.SetOut(&output)
					cmd.SetErr(&output)
					cmd.SetArgs([]string{"--rules=dangerous-triggers", "--json", file})
					require.NoError(t, cmd.Execute())
					var diagnostics []workflowLintDiagnostic
					require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
					if !policy.warning {
						require.Empty(t, diagnostics)
					} else {
						require.Len(t, diagnostics, 1)
						assert.Equal(t, "dangerous-triggers", diagnostics[0].RuleID)
						assert.Equal(t, "CTR-039", diagnostics[0].ThreatRuleID)
						assert.Equal(t, workflowlint.Warning, diagnostics[0].Severity)
						assert.Equal(t, "on."+trigger, diagnostics[0].Path)
					}
				})
			}
		}
	}
}

func TestNewThreatRulesCLIInputModes(t *testing.T) {
	const source = "---\non: {pull_request_target: {acknowledge-risk: true}}\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}\n---\nPrompt ${{ inputs.PRIVATE_VALUE.constructor }}\n"
	for _, extension := range []string{".md", ".lock.yml", ".yaml"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte(source), 0o600))
			file := filepath.Join(dir, "test"+extension)
			if extension != ".md" {
				require.NoError(t, os.WriteFile(file, []byte("on: pull_request_target\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo public\n"), 0o600))
			}
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=pull-request-target-checkout,prompt-expression", "--json", file})
			err := cmd.Execute()
			want := 2
			if extension == ".yaml" {
				want = 0
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "workflow lint found 2 error")
			}
			assert.NotContains(t, output.String(), "PRIVATE_VALUE")
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, map[string]string{"pull-request-target-checkout": "CTR-008", "prompt-expression": "CTR-010"}[diagnostic.RuleID], diagnostic.ThreatRuleID)
				assert.Equal(t, workflowlint.Error, diagnostic.Severity)
			}
		})
	}
}

func TestThreatPolicyRulesCLIInputModes(t *testing.T) {
	const source = "---\non: {pull_request_target: null, roles: read}\nsafe-outputs: {steps: [{run: '$(PRIVATE_VALUE)'}]}\nsandbox: {agent: {args: [--allow-urls, 'https://PRIVATE_VALUE/path']}}\n---\nA prompt.\n"
	const rules = "--rules=pull-request-target-roles,safe-output-shell-expansion,firewall-ssl-bump"
	const compiled = "on: pull_request_target\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: value=$(ordinary-step)\n"
	for _, extension := range []string{".md", ".lock.yml", ".yaml"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte(source), 0o600))
			file := filepath.Join(dir, "test"+extension)
			if extension != ".md" {
				require.NoError(t, os.WriteFile(file, []byte(compiled), 0o600))
			}
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{rules, "--json", file})
			err := cmd.Execute()
			assert.NotContains(t, output.String(), "PRIVATE_VALUE")
			wantCount := 3
			if extension == ".yaml" {
				wantCount = 0
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "workflow lint found 3 error")
			}
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.Len(t, diagnostics, wantCount)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, file, diagnostic.File)
				assert.Equal(t, workflowlint.Error, diagnostic.Severity)
				assert.Equal(t, map[string]string{
					"pull-request-target-roles":   "CTR-008",
					"safe-output-shell-expansion": "CTR-009",
					"firewall-ssl-bump":           "CTR-011",
				}[diagnostic.RuleID], diagnostic.ThreatRuleID)
			}
		})
	}
}

func TestCustomJobDetectionCLIInputModes(t *testing.T) {
	const source = "---\njobs:\n  report:\n    needs: [agent, detection]\n    if: always()\n---\nPrompt\n"
	const compiled = `jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - run: echo agent
  detection:
    needs: agent
    runs-on: ubuntu-latest
    steps:
      - run: echo detection
  report:
    needs: [agent, detection]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - run: echo report
`
	for _, extension := range []string{".md", ".lock.yml", ".yaml"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte(source), 0o600))
			file := filepath.Join(dir, "test"+extension)
			if extension != ".md" {
				require.NoError(t, os.WriteFile(file, []byte(compiled), 0o600))
			}
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=custom-job-detection", "--json", file})
			err := cmd.Execute()
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			require.ErrorContains(t, err, "workflow lint found 1 error")
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "custom-job-detection", diagnostics[0].RuleID)
			assert.Equal(t, "jobs.report.if", diagnostics[0].Path)
			assert.Equal(t, workflowlint.Error, diagnostics[0].Severity)
			assert.Equal(t, "CTR-005", diagnostics[0].ThreatRuleID)
		})
	}
}

func TestBuiltInWorkflowLintCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.lock.yml")
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	}
	execute := func(args ...string) (string, error) {
		t.Helper()
		cmd := NewLintCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return output.String(), err
	}
	write("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ secrets.TOKEN }}'\n")
	output, err := execute("--rules", "secret-leak", "--json", file)
	require.ErrorContains(t, err, "found 1 error")
	// Cobra appends usage/errors in a standalone command. The first JSON value
	// must remain independently decodable, including when findings fail the gate.
	var diagnostics []workflowLintDiagnostic
	require.NoError(t, json.NewDecoder(bytes.NewBufferString(output)).Decode(&diagnostics))
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "secret-leak", diagnostics[0].RuleID)
	assert.Equal(t, "CTR-017", diagnostics[0].ThreatRuleID)
	assert.Equal(t, file, diagnostics[0].File)
	require.Len(t, diagnostics[0].Related, 1)
	assert.NotContains(t, diagnostics[0].Message, "TOKEN")

	output, err = execute("--rules=secret-leak", file)
	require.ErrorContains(t, err, "found 1 error")
	assert.Contains(t, output, "error [secret-leak] (CTR-017)")

	write("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n")
	output, err = execute("--rules=secret-leak", "--json", file)
	require.NoError(t, err)
	assert.JSONEq(t, "[]", output)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\non: workflow_dispatch\n---\nPrint ${{ secrets.TOKEN }}.\n"), 0o600))
	output, err = execute("--rules=secret-leak", "--json", file)
	require.Error(t, err)
	require.NoError(t, json.NewDecoder(bytes.NewBufferString(output)).Decode(&diagnostics))
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "markdown", diagnostics[0].Path)

	output, err = execute("--list-rules")
	require.NoError(t, err)
	assert.Contains(t, output, "secret-leak")
	assert.Contains(t, output, "pull-request-target-roles")
	assert.Contains(t, output, "safe-output-shell-expansion")
	assert.Contains(t, output, "firewall-ssl-bump")
	assert.Contains(t, output, "custom-job-detection")
	assert.Contains(t, output, "grader-file-io")
	assert.Contains(t, output, "\tCTR-008\n")
	assert.Contains(t, output, "\tCTR-009\n")
	assert.Contains(t, output, "\tCTR-011\n")
	_, err = execute("--rules=missing", file)
	require.ErrorContains(t, err, "unknown workflow lint rule")
	_, err = execute("--json", file)
	require.ErrorContains(t, err, "--json requires --rules")
	_, err = execute("--rules=secret-leak", "--shellcheck", file)
	require.ErrorContains(t, err, "require actionlint")
}

func TestBuiltInWorkflowLintReadAndParseFailures(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.lock.yml")
	require.NoError(t, os.WriteFile(file, []byte("jobs: [private-literal"), 0o600))
	cmd := NewLintCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--rules=secret-leak", "--json", file})
	err := cmd.Execute()
	require.ErrorContains(t, err, "could not parse workflow Actions AST")
	assert.NotContains(t, output.String(), "private-literal")
}

func TestWorkflowLintPreservesDiagnosticsOnRuleFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.lock.yml")
	require.NoError(t, os.WriteFile(file, []byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n"), 0o600))
	runner, err := workflowlint.New(workflowlint.Rule{
		ID: "partial-check", Description: "Report partial findings on failure", Severity: workflowlint.Warning,
		Check: func(context.Context, workflowlint.Input) ([]workflowlint.Diagnostic, error) {
			return []workflowlint.Diagnostic{{Path: "jobs.test", Message: "check incomplete"}}, errors.New("could not complete check")
		},
	}, workflowlint.Rule{
		ID: "independent-check", Description: "Continue independent checks", Severity: workflowlint.Error,
		Check: func(context.Context, workflowlint.Input) ([]workflowlint.Diagnostic, error) {
			return []workflowlint.Diagnostic{{Path: "jobs.test", Message: "independent finding"}}, nil
		},
	})
	require.NoError(t, err)
	var output bytes.Buffer
	err = runWorkflowLint(context.Background(), []string{file}, runner, &output, true)
	require.ErrorContains(t, err, "could not complete check")
	require.ErrorContains(t, err, "workflow lint found 1 error")
	var diagnostics []workflowLintDiagnostic
	require.NoError(t, json.Unmarshal(output.Bytes(), &diagnostics))
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "independent-check", diagnostics[0].RuleID)
	assert.Equal(t, "partial-check", diagnostics[1].RuleID)
	assert.NotContains(t, output.String(), "threat_rule_id", "unmapped extension rules do not invent compiler threat references")
}
