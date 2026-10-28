package lint

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func TestPrivilegedShellMultilineInterpolations(t *testing.T) {
	for _, expression := range []string{
		"${{\n github.event.pull_request.head.sha\n }}",
		"${{\n\n github.event.pull_request.head.sha\n\n }}",
		"${{\r\n github.event.pull_request.head.sha\r\n }}",
	} {
		for _, quote := range []string{"", "'", `"`} {
			script := "echo start\ngit checkout " + quote + expression + quote + "\ngh pr checkout 42\n"
			input := Input{Document: &aw.Document{}}
			block := aw.ExecutionBlock{Fields: map[string]any{"runs-on": "ubuntu-latest"}}
			step := aw.ExecutionStep{Path: "steps[0]", Fields: map[string]any{"run": script}}
			var slices []string
			diagnostics, err := policyShellCalls(context.Background(), input, block, step, func(call *syntax.CallExpr, original string) ([]Diagnostic, error) {
				assert.Equal(t, script, original)
				if policyShellArgument(call, 0) == "git" {
					require.Len(t, call.Args, 3)
					word := call.Args[2]
					slices = append(slices, original[word.Pos().Offset():word.End().Offset()])
				}
				return privilegedCheckoutCommand(context.Background(), call, original, step.Path+".run")
			})
			require.NoError(t, err)
			require.Equal(t, []string{quote + expression + quote}, slices)
			require.Len(t, diagnostics, 2)
			assert.Equal(t, 2, diagnostics[0].Line)
			assert.Equal(t, strings.Count(expression, "\n")+3, diagnostics[1].Line)
		}
	}
}

func TestPrivilegedShellRunnerDefaults(t *testing.T) {
	for _, tt := range []struct {
		name, runner, defaults, jobDefaults, shell string
		inspect                                    bool
	}{
		{name: "linux", runner: "ubuntu-latest", inspect: true},
		{name: "macos", runner: "macos-latest", inspect: true},
		{name: "windows", runner: "windows-latest"},
		{name: "windows labels", runner: "[self-hosted, Windows, X64]"},
		{name: "linux labels", runner: "[self-hosted, Linux, X64]", inspect: true},
		{name: "runner group", runner: "{group: builders, labels: linux}", inspect: true},
		{name: "unknown labels", runner: "[self-hosted, custom]"},
		{name: "unknown runner", runner: "custom"},
		{name: "conflicting labels", runner: "[linux, windows]"},
		{name: "dynamic runner", runner: "'${{ matrix.os }}'"},
		{name: "dynamic labels", runner: "[linux, '${{ matrix.os }}']"},
		{name: "windows explicit bash", runner: "windows-latest", shell: "bash", inspect: true},
		{name: "unknown explicit sh", runner: "custom", shell: "sh", inspect: true},
		{name: "linux explicit powershell", runner: "ubuntu-latest", shell: "pwsh"},
		{name: "workflow shell override", runner: "windows-latest", defaults: "defaults: {run: {shell: bash}}\n", inspect: true},
		{name: "job shell override", runner: "windows-latest", jobDefaults: "    defaults: {run: {shell: sh}}\n", inspect: true},
		{name: "job overrides workflow", runner: "ubuntu-latest", defaults: "defaults: {run: {shell: bash}}\n", jobDefaults: "    defaults: {run: {shell: pwsh}}\n"},
		{name: "step overrides job", runner: "windows-latest", jobDefaults: "    defaults: {run: {shell: pwsh}}\n", shell: "bash", inspect: true},
		{name: "dynamic shell", runner: "ubuntu-latest", shell: "'${{ matrix.shell }}'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, rule := range []Rule{PrivilegedCheckoutRule(), ArtifactExecutionRule()} {
				script := "gh pr checkout 42"
				download := ""
				if rule.ID == "artifact-execution" {
					script = "bash incoming/run.sh"
					download = "      - uses: actions/download-artifact@v4\n        with: {github-token: '${{ github.token }}', run-id: 42, path: incoming}\n"
				}
				for _, target := range []ASTTargets{AWAST, ActionsAST} {
					source := fmt.Sprintf("on: workflow_run\n%sjobs:\n  check:\n    runs-on: %s\n%s    steps:\n%s      - run: %s\n", tt.defaults, tt.runner, tt.jobDefaults, download, script)
					if tt.shell != "" {
						source += "        shell: " + tt.shell + "\n"
					}
					diagnostics := integrityTestRun(t, rule, ruleContractInput(t, source, target, false))
					if tt.inspect {
						require.Len(t, diagnostics, 1, "%s target %v", rule.ID, target)
					} else {
						assert.Empty(t, diagnostics, "%s target %v", rule.ID, target)
					}
				}
			}
		})
	}
}

func TestPrivilegedShellSkipsPowerShellSyntax(t *testing.T) {
	for _, rule := range []Rule{PrivilegedCheckoutRule(), ArtifactExecutionRule()} {
		for _, target := range []ASTTargets{AWAST, ActionsAST} {
			source := "on: workflow_run\njobs:\n  check:\n    runs-on: windows-latest\n    steps:\n" +
				"      - uses: actions/download-artifact@v4\n        with: {github-token: '${{ github.token }}', run-id: 42, path: incoming}\n" +
				"      - run: |\n          $files = @('incoming/run.ps1')\n          foreach ($file in $files) { & $file }\n"
			assert.Empty(t, integrityTestRun(t, rule, ruleContractInput(t, source, target, false)))
		}
	}
}

func TestPrivilegedShellAWDefaultsAndContainers(t *testing.T) {
	for _, tt := range []struct {
		name, frontmatter, path, fields string
		want                            string
		known                           bool
	}{
		{name: "AW default", want: "bash", known: true},
		{name: "AW windows", fields: "runs-on: windows-latest", want: "pwsh", known: true},
		{name: "container", fields: "runs-on: ubuntu-latest\ncontainer: alpine", want: "sh", known: true},
		{name: "safe outputs default", path: "safe-outputs", want: "bash", known: true},
		{name: "safe outputs framework runner", path: "safe-outputs", frontmatter: "runs-on-slim: windows-latest", want: "pwsh", known: true},
		{name: "safe outputs runner override", path: "safe-outputs", frontmatter: "runs-on-slim: windows-latest", fields: "runs-on: ubuntu-latest", want: "bash", known: true},
		{name: "unknown authored job", path: "jobs.check"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			document, err := aw.Parse("---\n" + tt.frontmatter + "\n---\n")
			require.NoError(t, err)
			fields, err := aw.Parse("---\n" + tt.fields + "\n---\n")
			require.NoError(t, err)
			shell, known := policyRunShell(Input{Document: document}, aw.ExecutionBlock{Path: tt.path, Fields: fields.Frontmatter}, aw.ExecutionStep{})
			assert.Equal(t, tt.want, shell)
			assert.Equal(t, tt.known, known)
		})
	}
}
