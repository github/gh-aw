package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunExpressionActionsSyntax(t *testing.T) {
	tests := []struct {
		body       string
		prefix     string
		expression string
	}{
		{`secrets['TOKEN']`, "TOKEN", `${{ secrets.TOKEN }}`},
		{`env['KEY']`, "GH_AW_ENV_KEY", `${{ env.KEY }}`},
		{`github['token']`, "GH_AW_GITHUB_TOKEN", `${{ github.token }}`},
		{`secrets['TOKEN'] || 'fallback'`, "GH_AW_SECRET_TOKEN_", ""},
		{`format('secrets.TOKEN env.KEY github.token')`, "EXPR_", ""},
		{`'secrets.TOKEN'`, "EXPR_", ""},
		{`format('}} {0}', github.actor)`, "EXPR_", ""},
		{`secrets["TOKEN"]`, "", ""},
		{`secrets.TOKEN ||`, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			name, expression, ok := mapRunExpressionToEnvBinding(tt.body)
			if tt.prefix == "" {
				if ok {
					t.Fatalf("invalid syntax was accepted: %s", name)
				}
				return
			}
			if !ok || !strings.HasPrefix(name, tt.prefix) {
				t.Fatalf("got %q, %v; want prefix %q", name, ok, tt.prefix)
			}
			want := tt.expression
			if want == "" {
				want = "${{ " + tt.body + " }}"
			}
			if expression != want {
				t.Fatalf("got expression %q, want %q", expression, want)
			}
		})
	}
	line := `echo "${{ format('}} {0}', secrets['TOKEN']) }}"`
	got, bindings := replaceStepExpressionRefs(line, false, nil)
	if len(bindings) != 1 || got != `echo "$`+bindings[0].Name+`"` || bindings[0].Expression != `${{ format('}} {0}', secrets['TOKEN']) }}` {
		t.Fatalf("quote-aware hoisting failed: %q, %#v", got, bindings)
	}
	for _, malformed := range []string{`echo ${{ secrets.TOKEN || }}`, `echo ${{ secrets.TOKEN`} {
		got, bindings := replaceStepExpressionRefs(malformed, false, nil)
		if got != malformed || len(bindings) != 0 {
			t.Fatalf("malformed expression rewritten: %q, %#v", got, bindings)
		}
	}
}

func TestSanitizeGHAExpressionsActionsSyntax(t *testing.T) {
	for _, script := range []string{
		`echo ${{ format('}} {0}', github.actor) }}`,
		`echo ${{ format('it''s }} {0}', env['KEY']) }}`,
		"echo ${{ github.actor ||\n'fallback' }}",
	} {
		if got := sanitizeGHAExpressions(script); got != "echo __GHA_EXPR__" {
			t.Errorf("got %q for %q", got, script)
		}

	}
	for _, script := range []string{`echo ${{ env.KEY || }}`, `echo ${{ env["KEY"] }}`, `echo ${{ env.KEY`} {
		if got := sanitizeGHAExpressions(script); got != script {
			t.Errorf("malformed syntax was masked: %q", got)
		}
	}
}

func TestMCPSecretNameActionsSyntax(t *testing.T) {
	for _, expression := range []string{`${{ secrets.TOKEN }}`, `${{secrets['TOKEN']}}`, `${{  secrets.TOKEN  }}`} {
		if got := simpleMCPSecretName(expression); got != "TOKEN" {
			t.Errorf("got %q for %s", got, expression)
		}
	}
	for _, expression := range []string{
		`${{ 'secrets.TOKEN' }}`, `${{ secrets.TOKEN || 'fallback' }}`,
		`${{ secrets.TOKEN || }}`, `${{ secrets['TOKEN; echo injected'] }}`, `${{ secrets["TOKEN"] }}`,
	} {
		if got := simpleMCPSecretName(expression); got != "" {
			t.Errorf("unexpected secret %q for %s", got, expression)
		}
	}
}

func TestTrialRepositoryActionsSyntax(t *testing.T) {
	content := `${{ github.repository }} ${{github['repository']}} ${{ 'github.repository' }} ${{ format('}} {0}', github.repository) }}`
	want := `owner/repo owner/repo ${{ 'github.repository' }} ${{ format('}} {0}', github.repository) }}`
	if got, err := replaceTrialRepositoryExpressions(content, "owner/repo"); err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	malformed := `${{ github.repository || }}`
	if got, err := replaceTrialRepositoryExpressions(malformed, "owner/repo"); err != nil || got != malformed {
		t.Fatalf("malformed expression rewritten: %q, %v", got, err)
	}
	if _, err := replaceTrialRepositoryExpressions(`${{ github.repository`, "owner/repo"); err == nil {
		t.Fatal("expected malformed interpolation diagnostic")
	}
}

func TestExpressionParserDiagnosticsDoNotLogSource(t *testing.T) {
	const marker = "sensitive-expression-source-marker"
	if os.Getenv("GH_AW_TEST_EXPRESSION_LOG_REDACTION") == "1" {
		sanitizeGHAExpressions("${{ " + marker)
		sanitizeGHAExpressions("${{ format('" + marker + "', ) }}")
		_, _ = replaceTrialRepositoryExpressions("${{ format('"+marker+"', ) }}", "owner/repo")
		mapRunExpressionToEnvBinding("format('" + marker + "', )")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestExpressionParserDiagnosticsDoNotLogSource$")
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GH_AW_TEST_EXPRESSION_LOG_REDACTION" && key != "DEBUG" && key != "DEBUG_COLORS" {
			env = append(env, entry)
		}
	}
	cmd.Env = append(env,
		"GH_AW_TEST_EXPRESSION_LOG_REDACTION=1",
		"DEBUG=cli:*",
		"DEBUG_COLORS=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("debug-log subprocess failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), marker) {
		t.Fatalf("parser source was written to debug logs: %s", output)
	}
}
