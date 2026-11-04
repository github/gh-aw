//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeredocInjectionControls(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	tests := []struct {
		name    string
		script  string
		rewrite bool
	}{
		{"unquoted", "cat <<EOF\n" + expression + "\nEOF", true},
		{"unquoted custom", "cat <<custom-marker\n" + expression + "\ncustom-marker", true},
		{"numeric delimiter", "cat <<123\n" + expression + "\n123", true},
		{"unquoted tab stripping", "cat <<-EOF\n\t" + expression + "\n\tEOF", true},
		{"body hash is not a comment", "cat <<EOF\n# " + expression + "\nEOF", true},
		{"body quotes are not shell quotes", "cat <<EOF\n' # " + expression + "\nEOF", true},
		{"single quoted", "cat <<'EOF'\n" + expression + "\nEOF", false},
		{"double quoted custom", "cat <<\"custom-marker\"\n" + expression + "\ncustom-marker", false},
		{"partially quoted", "cat <<E'OF'\n" + expression + "\nEOF", false},
		{"prefix quoted", "cat <<'E'OF\n" + expression + "\nEOF", false},
		{"ANSI quoted without escapes", "cat <<$'EOF'\n" + expression + "\nEOF", false},
		{"single quoted literal backslash", "cat <<'E\\OF'\n" + expression + "\nE\\OF", false},
		{"continued unquoted delimiter", "cat <<EO\\\nF\n" + expression + "\nEOF", true},
		{"escaped delimiter", "cat <<\\EOF\n" + expression + "\nEOF", false},
		{"escaped delimiter followed by command", "cat <<\\EOF\nliteral\\\nEOF\necho " + expression, true},
		{"quoted tab stripping", "cat <<-'custom-marker'\n\t" + expression + "\n\tcustom-marker", false},
		{"mismatched suffix", "cat <<'FIRST_EOF'\n" + expression + "\nSECOND_EOF", false},
		{"trailing delimiter spaces", "cat <<'EOF'\n" + expression + "\nEOF ", false},
		{"leading delimiter spaces", "cat <<'EOF'\n" + expression + "\n EOF", false},
		{"tabs without strip operator", "cat <<'EOF'\n" + expression + "\n\tEOF", false},
		{"spaces with strip operator", "cat <<-'EOF'\n" + expression + "\n EOF", false},
		{"unterminated", "cat <<'EOF'\n" + expression, false},
		{"opener in string", "echo \"<<'EOF'\"\necho " + expression + "\nEOF", true},
		{"opener in comment", "# cat <<'EOF'\necho " + expression + "\nEOF", true},
		{"here string", "cat <<<\"<<'EOF'\"\necho " + expression + "\nEOF", true},
		{"arithmetic shift", "echo $((1 << 2))\necho " + expression, true},
		{"opening command retained", "cat <<'EOF'; echo " + expression + "\nliteral\nEOF", true},
		{"after exact close", "cat <<'FIRST_EOF'\nliteral\nFIRST_EOF\necho " + expression + "\nSECOND_EOF", true},
		{"same suffix inside body", "cat <<'FIRST_EOF'\nSECOND_EOF\n" + expression + "\nFIRST_EOF", false},
		{"multiple redirects", "cat <<'FIRST' <<SECOND\nliteral\nFIRST\n# " + expression + "\nSECOND", true},
		{"quoted body after unquoted body", "cat <<FIRST <<'SECOND'\nliteral\nFIRST\n" + expression + "\nSECOND", false},
		{"nested command substitution", "value=$(cat <<'EOF'\n" + expression + "\nEOF\n)", false},
		{"outer double quote hash", "echo \"$(cat <<'EOF'\nliteral\nEOF\n) # " + expression + "\"", true},
		{"quoted inner body in expanding outer body", "cat <<OUTER\n$(cat <<'INNER'\n" + expression + "\nINNER\n)\nOUTER", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := map[string]any{"run": tt.script}
			parsed := map[string]any{"steps": []any{step}}
			yaml := "jobs:\n  test:\n    steps:\n      - run: |\n          " +
				strings.ReplaceAll(tt.script, "\n", "\n          ") + "\n"

			assert.Contains(t, tt.script, expression)
			require.Error(t, validateNoTemplateInjectionFromParsed(parsed))
			require.Error(t, validateNoGitHubExpressionsInRunScriptsFromParsed(parsed))
			scan := scanRunContentExpressions(yaml)
			assert.True(t, scan.hasUnsafe, "raw YAML unsafe scan")
			assert.True(t, scan.hasDisallowed, "raw YAML expression guardrail")

			sanitized, warnings, changed := sanitizeRunStepExpressions(step)
			_, parsedShell := shellHeredocRegions(tt.script, false)
			assert.Equal(t, tt.rewrite && parsedShell, changed)
			assert.Equal(t, tt.script, step["run"], "input must not be mutated")
			if tt.rewrite && parsedShell {
				require.Len(t, warnings, 1)
				assert.Equal(t, expression, sanitized["env"].(map[string]any)["GH_AW_GITHUB_EVENT_ISSUE_TITLE"])
				assert.NotContains(t, shellExpansionContent(sanitized["run"].(string)), expression)
				if strings.Contains(sanitized["run"].(string), expression) {
					require.Error(t, validateNoTemplateInjectionFromParsed(map[string]any{"steps": []any{sanitized}}))
				} else {
					require.NoError(t, validateNoTemplateInjectionFromParsed(map[string]any{"steps": []any{sanitized}}))
				}
			} else {
				assert.Empty(t, warnings)
				assert.Equal(t, tt.script, sanitized["run"], "quoted body must remain literal")
				require.Error(t, validateNoTemplateInjectionFromParsed(map[string]any{"steps": []any{sanitized}}))
			}
		})
	}
}

func TestHeredocSanitizerPreservesQuotedCopy(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	script := "cat <<'FIRST_EOF'\n" + expression + "\nFIRST_EOF\ncat <<SECOND_EOF\n# " + expression + "\nSECOND_EOF"
	sanitized, _, changed := sanitizeRunStepExpressions(map[string]any{"run": script})
	require.True(t, changed)
	assert.Equal(t, "cat <<'FIRST_EOF'\n"+expression+"\nFIRST_EOF\ncat <<SECOND_EOF\n# $GH_AW_GITHUB_EVENT_ISSUE_TITLE\nSECOND_EOF", sanitized["run"])
	require.Error(t, validateNoGitHubExpressionsInRunScriptsFromParsed(map[string]any{"steps": []any{sanitized}}))
}

func TestHeredocMalformedExpressionFailsValidation(t *testing.T) {
	step := map[string]any{"run": "cat <<EOF\n${{}}${{\nEOF"}
	sanitized, _, changed := sanitizeRunStepExpressions(step)
	require.False(t, changed, "malformed shell syntax must not be rewritten")
	require.Error(t, validateNoGitHubExpressionsInRunScriptsFromParsed(map[string]any{"steps": []any{sanitized}}))
}

func TestHeredocUnknownBoundariesPreserveScript(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	tests := []struct {
		name   string
		script string
	}{
		{"parse error", "cat <<'EOF'\n" + expression + "\nEOF\nif"},
		{"prefix quote continuation parser error", "cat <<'E'OF\nliteral\\\nEOF\necho " + expression + "\n"},
		{"prefix quote continuation mismatch", "cat <<'E'OF\nliteral\\\nEOF\necho " + expression + "\nEOF\n"},
		{"double prefix quote continuation mismatch", "cat <<\"E\"OF\nliteral\\\nEOF\necho " + expression + "\nEOF\n"},
		{"ANSI quoted escaped delimiter mismatch", "cat <<$'E\\x4fF'\nliteral\nEOF\necho " + expression + "\nE\\x4fF\n"},
		{"double quoted escaped delimiter mismatch", "cat <<\"E\\$OF\"\nliteral\nE$OF\necho " + expression + "\nE\\$OF\n"},
		{"partially ANSI quoted escaped delimiter mismatch", "cat <<E$'\\x4f'F\nliteral\nEOF\necho " + expression + "\nE\\x4fF\n"},
		{"partially double quoted escaped delimiter mismatch", "cat <<E\"\\$\"OF\nliteral\nE$OF\necho " + expression + "\nE\\$OF\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, parsed := shellHeredocRegions(tt.script, false)
			require.False(t, parsed, "unproven Bash boundaries must not grant an exemption")
			assert.Contains(t, tt.script, expression)
			step := map[string]any{"run": tt.script}
			sanitized, warnings, changed := sanitizeRunStepExpressions(step)
			assert.False(t, changed)
			assert.Empty(t, warnings)
			assert.Equal(t, tt.script, sanitized["run"])
			parsedSteps := map[string]any{"steps": []any{sanitized}}
			require.Error(t, validateNoTemplateInjectionFromParsed(parsedSteps))
			require.Error(t, validateNoGitHubExpressionsInRunScriptsFromParsed(parsedSteps))
			yaml := "steps:\n  - run: |\n      " + strings.ReplaceAll(tt.script, "\n", "\n      ") + "\n"
			scan := scanRunContentExpressions(yaml)
			assert.True(t, scan.hasUnsafe)
			assert.True(t, scan.hasDisallowed)
		})
	}
}

func TestHeredocSanitizerPreservesNestedQuotedCopy(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	script := "cat <<OUTER\n$(cat <<'INNER'\n" + expression + "\nINNER\n)\n" + expression + "\nOUTER"
	sanitized, _, changed := sanitizeRunStepExpressions(map[string]any{"run": script})
	require.True(t, changed)
	assert.Equal(t, "cat <<OUTER\n$(cat <<'INNER'\n"+expression+"\nINNER\n)\n$GH_AW_GITHUB_EVENT_ISSUE_TITLE\nOUTER", sanitized["run"])
	require.Error(t, validateNoGitHubExpressionsInRunScriptsFromParsed(map[string]any{"steps": []any{sanitized}}))
}

func TestHeredocCommentsPreserveFullScriptContext(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	script := "# " + expression + "\necho \"$(cat <<'EOF'\nliteral\nEOF\n) # " + expression + "\"\n# " + expression
	sanitized, _, changed := sanitizeRunStepExpressions(map[string]any{"run": script})
	require.True(t, changed)
	assert.Equal(t, "# $GH_AW_GITHUB_EVENT_ISSUE_TITLE\necho \"$(cat <<'EOF'\nliteral\nEOF\n) # $GH_AW_GITHUB_EVENT_ISSUE_TITLE\"\n# $GH_AW_GITHUB_EVENT_ISSUE_TITLE", sanitized["run"])
	assert.NotContains(t, shellExpansionContent(sanitized["run"].(string)), expression)
	require.NoError(t, validateNoGitHubExpressionsInRunScriptsFromParsed(map[string]any{"steps": []any{sanitized}}))
}

func TestHeredocParserFailureDoesNotLogScript(t *testing.T) {
	const secret = "sensitive_delimiter_7498"
	const child = "GH_AW_TEST_HEREDOC_LOG_CHILD"
	if os.Getenv(child) == "1" {
		_, parsed := shellHeredocRegions("cat <<'"+secret+"'\nunterminated", false)
		require.False(t, parsed)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestHeredocParserFailureDoesNotLogScript$")
	command.Env = append(command.Environ(), child+"=1", "DEBUG=workflow:template_injection_validation", "DEBUG_COLORS=0")
	output, err := command.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "Cannot identify safe heredoc regions; scanning entire script")
	assert.NotContains(t, string(output), secret)
	assert.NotContains(t, string(output), "unterminated")
}

func TestHeredocDelimiterSemanticsMatchBash(t *testing.T) {
	tests := []struct {
		name, script, output string
	}{
		{"prefix quote", "cat <<'E'OF\nliteral\\\nEOF\nprintf later", "literal\\\nlater"},
		{"continued unquoted delimiter", "cat <<EO\\\nF\n$(printf expanded)\nEOF", "expanded\n"},
		{"ANSI quoted escaped delimiter", "cat <<$'E\\x4fF'\nliteral\nEOF\nprintf later", "literal\nlater"},
		{"double quoted escaped delimiter", "cat <<\"E\\$OF\"\nliteral\nE$OF\nprintf later", "literal\nlater"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := exec.Command("bash", "-c", tt.script).CombinedOutput()
			require.NoError(t, err)
			assert.Equal(t, tt.output, string(output))
		})
	}
}

func TestUnquotedHeredocSanitizerPreventsCommandSubstitution(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	const payload = "$(printf injected)"
	script := "cat <<EOF\n" + expression + "\nEOF"
	originalOutput, err := exec.Command("bash", "-c", strings.ReplaceAll(script, expression, payload)).CombinedOutput()
	require.NoError(t, err)
	assert.Equal(t, "injected\n", string(originalOutput), "original interpolation executes command substitution")

	sanitized, _, changed := sanitizeRunStepExpressions(map[string]any{"run": script})
	require.True(t, changed)
	command := exec.Command("bash", "-c", sanitized["run"].(string))
	command.Env = append(command.Environ(), "GH_AW_GITHUB_EVENT_ISSUE_TITLE="+payload)
	output, err := command.CombinedOutput()
	require.NoError(t, err)
	assert.Equal(t, payload+"\n", string(output), "environment-variable expansion must preserve the payload as data")
}

func TestQuotedHeredocEnvExpressionRejectedInBothValidationPaths(t *testing.T) {
	const expression = "${{ env.BODY }}"
	script := "cat <<'EOF'\n" + expression + "\nEOF"
	workflow := map[string]any{"steps": []any{map[string]any{
		"env": map[string]any{"BODY": "${{ steps.fetch.outputs.result }}"},
		"run": script,
	}}}

	err := validateNoTemplateInjectionFromParsed(workflow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quoted heredoc")
	assert.Contains(t, err.Error(), "unquoted heredoc or printf")

	err = validateNoGitHubExpressionsInRunScriptsFromParsed(workflow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unquoted heredoc or printf")

	yaml := `jobs:
  test:
    steps:
      - id: fetch
        run: echo result
      - env:
          BODY: ${{ steps.fetch.outputs.result }}
        run: |
          cat <<'EOF'
            ${{ env.BODY }}
          EOF
`
	scan := scanRunContentExpressions(yaml)
	assert.True(t, scan.hasUnsafe)
	assert.True(t, scan.hasDisallowed)

	compiler := NewCompiler(WithNoEmit(true))
	for _, parsed := range []map[string]any{workflow, nil} {
		require.Error(t, compiler.validateTemplateInjection(yaml, "test.lock.yml", "test.md", parsed))
	}
}

func TestQuotedHeredocGuardrailDiagnostic(t *testing.T) {
	for _, expression := range []string{"${{ secrets.NPM_TOKEN }}", "${{ github.sha }}"} {
		t.Run(expression, func(t *testing.T) {
			workflow := map[string]any{"steps": []any{map[string]any{
				"run": "cat <<'EOF'\n" + expression + "\nEOF",
			}}}
			err := validateNoGitHubExpressionsInRunScriptsFromParsed(workflow)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Quoted heredoc contents are still interpolated by GitHub Actions before Bash")
			assert.Contains(t, err.Error(), "printf '%s\\n' \"$VALUE\"")
		})
	}
}

func TestQuotedHeredocEnvExpressionWithUnprovenBoundaryRejected(t *testing.T) {
	const script = "cat <<'EOF'\n${{ env.BODY }}\nEOF\nif"
	workflow := map[string]any{"steps": []any{map[string]any{"run": script}}}

	err := validateNoTemplateInjectionFromParsed(workflow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unproven heredoc boundary")

	yaml := "steps:\n  - run: |\n      " + strings.ReplaceAll(script, "\n", "\n      ") + "\n"
	scan := scanRunContentExpressions(yaml)
	assert.True(t, scan.hasUnsafe)
	assert.True(t, scan.hasDisallowed)
}
