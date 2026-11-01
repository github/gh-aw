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
		name   string
		script string
		unsafe bool
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
		{"continued unquoted delimiter", "cat <<EO\\\nF\n" + expression + "\nEOF", true},
		{"escaped delimiter", "cat <<\\EOF\n" + expression + "\nEOF", false},
		{"escaped delimiter followed by command", "cat <<\\EOF\nliteral\\\nEOF\necho " + expression, true},
		{"quoted tab stripping", "cat <<-'custom-marker'\n\t" + expression + "\n\tcustom-marker", false},
		{"mismatched suffix", "cat <<'FIRST_EOF'\n" + expression + "\nSECOND_EOF", true},
		{"trailing delimiter spaces", "cat <<'EOF'\n" + expression + "\nEOF ", true},
		{"leading delimiter spaces", "cat <<'EOF'\n" + expression + "\n EOF", true},
		{"tabs without strip operator", "cat <<'EOF'\n" + expression + "\n\tEOF", true},
		{"spaces with strip operator", "cat <<-'EOF'\n" + expression + "\n EOF", true},
		{"unterminated", "cat <<'EOF'\n" + expression, true},
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

			assert.Equal(t, tt.unsafe, strings.Contains(removeHeredocContent(tt.script), expression))
			assert.Equal(t, tt.unsafe, validateNoTemplateInjectionFromParsed(parsed) != nil)
			assert.Equal(t, tt.unsafe, validateNoGitHubExpressionsInRunScriptsFromParsed(parsed) != nil)
			scan := scanRunContentExpressions(yaml)
			assert.Equal(t, tt.unsafe, scan.hasUnsafe, "raw YAML unsafe scan")
			assert.Equal(t, tt.unsafe, scan.hasDisallowed, "raw YAML expression guardrail")

			sanitized, warnings, changed := sanitizeRunStepExpressions(step)
			_, parsedShell := shellHeredocRegions(tt.script, false)
			assert.Equal(t, tt.unsafe && parsedShell, changed)
			assert.Equal(t, tt.script, step["run"], "input must not be mutated")
			if tt.unsafe && parsedShell {
				require.Len(t, warnings, 1)
				assert.Equal(t, expression, sanitized["env"].(map[string]any)["GH_AW_GITHUB_EVENT_ISSUE_TITLE"])
				assert.NotContains(t, executableShellContent(sanitized["run"].(string)), expression)
				assert.NoError(t, validateNoTemplateInjectionFromParsed(map[string]any{"steps": []any{sanitized}}))
			} else {
				assert.Empty(t, warnings)
				assert.Equal(t, tt.script, sanitized["run"], "quoted body must remain literal")
				if tt.unsafe {
					assert.Error(t, validateNoTemplateInjectionFromParsed(parsed))
				}
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, parsed := shellHeredocRegions(tt.script, false)
			require.False(t, parsed, "unproven Bash boundaries must not grant an exemption")
			assert.Contains(t, executableShellContent(tt.script), expression)
			assert.Equal(t, tt.script, replaceOutsideQuotedHeredocs(tt.script, expression, "$TITLE"))
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
}

func TestHeredocCommentsPreserveFullScriptContext(t *testing.T) {
	const expression = "${{ github.event.issue.title }}"
	script := "# " + expression + "\necho \"$(cat <<'EOF'\nliteral\nEOF\n) # " + expression + "\"\n# " + expression
	sanitized, _, changed := sanitizeRunStepExpressions(map[string]any{"run": script})
	require.True(t, changed)
	assert.Equal(t, "# "+expression+"\necho \"$(cat <<'EOF'\nliteral\nEOF\n) # $GH_AW_GITHUB_EVENT_ISSUE_TITLE\"\n# "+expression, sanitized["run"])
	assert.NotContains(t, executableShellContent(sanitized["run"].(string)), expression)
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
