package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runInsecureCommands(t *testing.T, input Input) []Diagnostic {
	t.Helper()
	runner, err := Default().Select("insecure-commands")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	return diagnostics
}

func TestInsecureCommandsEnvLevels(t *testing.T) {
	const flag = "ACTIONS_ALLOW_UNSECURE_COMMANDS"
	for _, tc := range []struct {
		name, source, path string
	}{
		{"workflow", "env: {" + flag + ": true}\n" + contractJob + contractSafeSteps, "env." + flag},
		{"job", contractJob + "    env: {" + flag + ": 'TRUE'}\n" + contractSafeSteps, "jobs.check.env." + flag},
		{"step", contractJob + "    steps:\n      - env: {" + flag + ": ' true '}\n        run: echo public\n", "jobs.check.steps[0].env." + flag},
		{"constant expression", contractJob + "    env: {" + flag + ": '${{ true }}'}\n" + contractSafeSteps, "jobs.check.env." + flag},
	} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			t.Run(tc.name+"/"+target.String(), func(t *testing.T) {
				diagnostics := runInsecureCommands(t, ruleContractInput(t, tc.source, target, false))
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tc.path, diagnostics[0].Path)
				assert.Equal(t, Error, diagnostics[0].Severity)
				assert.Contains(t, diagnostics[0].Message, "GITHUB_ENV")
			})
		}
	}
}

func TestInsecureCommandsAWOnlyLocations(t *testing.T) {
	const flag = "ACTIONS_ALLOW_UNSECURE_COMMANDS: true"
	for _, tc := range []struct{ source, path string }{
		{"engine:\n  id: copilot\n  env: {" + flag + "}\n", "engine.env.ACTIONS_ALLOW_UNSECURE_COMMANDS"},
		{"steps:\n  - env: {" + flag + "}\n    run: echo public\n", "steps[0].env.ACTIONS_ALLOW_UNSECURE_COMMANDS"},
		{"safe-outputs:\n  jobs:\n    notify:\n      env: {" + flag + "}\n      steps: [{run: echo public}]\n", "safe-outputs.jobs.notify.env.ACTIONS_ALLOW_UNSECURE_COMMANDS"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			diagnostics := runInsecureCommands(t, ruleContractInput(t, tc.source, AWAST, false))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tc.path, diagnostics[0].Path)
		})
	}
}

func TestInsecureCommandsInertAndDynamicValues(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  Severity
	}{
		{"false", ""},
		{"'0'", ""},
		{"1", ""},
		{"'yes'", ""},
		{"''", ""},
		{"'${{ false }}'", ""},
		{"\"${{ 'false' }}\"", ""},
		{"\"${{ 'true' }}\"", Error},
		{"'${{ inputs.allow }}'", Warning},
		{"'${{ vars.ALLOW }}'", Warning},
	} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			t.Run(tc.value+"/"+target.String(), func(t *testing.T) {
				source := contractJob + "    env:\n      ACTIONS_ALLOW_UNSECURE_COMMANDS: " + tc.value + "\n" + contractSafeSteps
				diagnostics := runInsecureCommands(t, ruleContractInput(t, source, target, false))
				if tc.want == "" {
					assert.Empty(t, diagnostics)
					return
				}
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tc.want, diagnostics[0].Severity)
				assert.NotContains(t, diagnostics[0].Message, "inputs.allow")
			})
		}
	}
}

func TestInsecureCommandsIgnoresOtherNames(t *testing.T) {
	source := contractJob + "    env: {actions_allow_unsecure_commands: true, ALLOW_UNSECURE_COMMANDS: true}\n" + contractSafeSteps
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		assert.Empty(t, runInsecureCommands(t, ruleContractInput(t, source, target, false)), target.String())
	}
}

func TestInsecureCommandsDeprecatedScriptCommands(t *testing.T) {
	for _, tc := range []struct {
		name, script, shell string
		want                int
		file                string
	}{
		{"set-output", `echo "::set-output name=value::${{ steps.x.outputs.y }}"`, "", 1, "$GITHUB_OUTPUT"},
		{"set-env", `echo ::set-env name=FOO::bar`, "bash", 1, "$GITHUB_ENV"},
		{"add-path single quoted", `echo '::add-path::/opt/bin'`, "sh", 1, "$GITHUB_PATH"},
		{"save-state printf", `printf '::save-state name=pid::%s\n' "$pid"`, "", 1, "$GITHUB_STATE"},
		{"nested in condition", "if true; then\n  echo \"::set-output name=a::b\"\nfi", "", 1, "$GITHUB_OUTPUT"},
		{"replacement", `echo "value=1" >> "$GITHUB_OUTPUT"`, "", 0, ""},
		{"supported command", `echo "::add-mask::$TOKEN"`, "", 0, ""},
		{"similar prefix", `echo "::set-outputs name=a::b"`, "", 0, ""},
		{"not leading", `echo "note ::set-output name=a::b"`, "", 0, ""},
		{"dynamic prefix", `echo "$PREFIX::set-output name=a::b"`, "", 0, ""},
		{"other command", `grep "::set-output" log.txt`, "", 0, ""},
		{"comment", `# echo "::set-output name=a::b"`, "", 0, ""},
		{"powershell", `echo "::set-output name=a::b"`, "pwsh", 0, ""},
		{"invalid syntax", "if", "", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"run": tc.script}
			if tc.shell != "" {
				fields["shell"] = tc.shell
			}
			workflow := &actions.Workflow{Jobs: []*actions.Job{{
				ID: "check", RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RawStep{Fields: fields}},
			}}}
			diagnostics := runInsecureCommands(t, Input{Workflow: workflow})
			require.Len(t, diagnostics, tc.want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Warning, diagnostic.Severity)
				assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
				assert.Positive(t, diagnostic.Line)
				assert.Contains(t, diagnostic.Message, tc.file)
				assert.NotContains(t, diagnostic.Message, "name=")
			}
		})
	}
}

func TestInsecureCommandsAWScriptsAndCancellation(t *testing.T) {
	document, err := aw.Parse("---\nsafe-outputs:\n  steps:\n    - run: echo \"::set-output name=a::b\"\n---\nPrompt.\n")
	require.NoError(t, err)
	diagnostics := runInsecureCommands(t, Input{Document: document})
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "safe-outputs.steps[0].run", diagnostics[0].Path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = checkInsecureCommands(ctx, Input{Document: document})
	require.ErrorIs(t, err, context.Canceled)
}
