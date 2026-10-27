//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRewriteRunnerTempInGithubScriptRequire(t *testing.T) {
	input := `steps:
  - uses: actions/github-script@v7
    with:
      script: |
        const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
        setupGlobals(core, github, context, exec, io, getOctokit);
        const { main } = require('${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs');
        await main(core, context);
`

	got := rewriteRunnerTempInExecutableBodies(input)

	if strings.Contains(got, "require('${{ runner.temp }}/gh-aw/actions/") {
		t.Fatalf("expected github-script require paths to avoid runner.temp expression:\n%s", got)
	}
	if !strings.Contains(got, "const path = require('path');") {
		t.Fatalf("expected path module prelude:\n%s", got)
	}
	if !strings.Contains(got, "const actionsDir = path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions');") {
		t.Fatalf("expected safe actionsDir prelude:\n%s", got)
	}
	if !strings.Contains(got, "const { setupGlobals } = require(path.join(actionsDir, 'setup_globals.cjs'));") {
		t.Fatalf("expected setup_globals require to use path.join:\n%s", got)
	}
	if !strings.Contains(got, "const { main } = require(path.join(actionsDir, 'generate_aw_info.cjs'));") {
		t.Fatalf("expected generated script require to use path.join:\n%s", got)
	}
	if err := validateNoRunnerTempInExecutableBodies(got); err != nil {
		t.Fatalf("rewritten output should pass executable body validation: %v", err)
	}
}

func TestRewriteRunnerTempInShellRunCommand(t *testing.T) {
	input := `steps:
  - run: node ${{ runner.temp }}/gh-aw/actions/generate_usage_activity_summary.cjs --json
`

	got := rewriteRunnerTempInExecutableBodies(input)

	if strings.Contains(got, "node ${{ runner.temp }}/gh-aw/actions/") {
		t.Fatalf("expected shell command to avoid runner.temp expression:\n%s", got)
	}
	if !strings.Contains(got, `run: node "${RUNNER_TEMP}/gh-aw/actions/generate_usage_activity_summary.cjs" --json`) {
		t.Fatalf("expected shell command to use quoted RUNNER_TEMP path:\n%s", got)
	}
	if err := validateNoRunnerTempInExecutableBodies(got); err != nil {
		t.Fatalf("rewritten output should pass executable body validation: %v", err)
	}
}

func TestRewriteRunnerTempOnlyRewritesRunCommands(t *testing.T) {
	input := `steps:
  - uses: github/gh-aw/actions/setup@v1
    with:
      command: node ${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs
  - run: node ${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs
`

	got := rewriteRunnerTempInExecutableBodies(input)

	if !strings.Contains(got, "command: node ${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs") {
		t.Fatalf("expected non-executable YAML field to remain unchanged:\n%s", got)
	}
	if !strings.Contains(got, `run: node "${RUNNER_TEMP}/gh-aw/actions/generate_aw_info.cjs"`) {
		t.Fatalf("expected run command to use quoted RUNNER_TEMP path:\n%s", got)
	}
}

func TestRewriteRunnerTempAdjacentShellCommands(t *testing.T) {
	for _, separator := range []string{";", "&&", "||", "|", "&", "; ", " && "} {
		t.Run(separator, func(t *testing.T) {
			input := "steps:\n  - run: node ${{ runner.temp }}/gh-aw/actions/a.cjs" + separator +
				"node ${{ runner.temp }}/gh-aw/actions/b.cjs\n"
			want := "steps:\n  - run: node \"${RUNNER_TEMP}/gh-aw/actions/a.cjs\"" + separator +
				"node \"${RUNNER_TEMP}/gh-aw/actions/b.cjs\"\n"
			got, err := finalizeRunnerTempSafety(input)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
			if twice := rewriteRunnerTempInExecutableBodies(got); twice != got {
				t.Fatalf("rewrite is not idempotent:\n%s", twice)
			}
		})
	}
}

func TestRewriteRunnerTempPreservesShellBoundaries(t *testing.T) {
	for _, suffix := range []string{">output", " 2>&1", ")", "\"", "'", "\\\n"} {
		t.Run(suffix, func(t *testing.T) {
			input := "node ${{ runner.temp }}/gh-aw/actions/a.cjs" + suffix
			want := "node \"${RUNNER_TEMP}/gh-aw/actions/a.cjs\"" + suffix
			if got := rewriteShellActionCommands(input); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestRewriteRunnerTempIsIdempotent(t *testing.T) {
	input := `steps:
  - uses: actions/github-script@v7
    with:
      script: |
        const path = require('path');
        const actionsDir = path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions');
        const { main } = require(path.join(actionsDir, 'generate_aw_info.cjs'));
`

	once := rewriteRunnerTempInExecutableBodies(input)
	twice := rewriteRunnerTempInExecutableBodies(once)
	if once != twice {
		t.Fatalf("rewrite is not idempotent\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

func TestRewriteRunnerTempPreservesCRLF(t *testing.T) {
	input := "steps:\r\n  - run: node ${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs\r\n"

	got := rewriteRunnerTempInExecutableBodies(input)

	if strings.Contains(got, "${{ runner.temp }}") {
		t.Fatalf("expected CRLF run command to avoid runner.temp expression:\n%s", got)
	}
	if !strings.Contains(got, "\r\n") {
		t.Fatalf("expected CRLF line endings to be preserved:\n%s", got)
	}
}

func TestValidateNoRunnerTempInExecutableBodiesAllowsYamlFields(t *testing.T) {
	input := `steps:
  - uses: github/gh-aw/actions/setup@v1
    with:
      destination: ${{ runner.temp }}/gh-aw/actions
`

	if err := validateNoRunnerTempInExecutableBodies(input); err != nil {
		t.Fatalf("non-executable YAML fields should be allowed: %v", err)
	}
}

func TestValidateNoRunnerTempInExecutableBodiesRejectsScriptRegression(t *testing.T) {
	input := `steps:
  - uses: actions/github-script@v7
    with:
      script: |
        const { main } = require('${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs');
`

	if err := validateNoRunnerTempInExecutableBodies(input); err == nil {
		t.Fatal("expected unsafe runner.temp expression in script body to be rejected")
	}
}

func TestRewriteRunnerTempRejectsEscapedActionPath(t *testing.T) {
	input := `steps:
  - uses: actions/github-script@v7
    with:
      script: |
        const { main } = require('${{ runner.temp }}/gh-aw/actions/evil\'); process.exit(1); //.cjs');
`

	got := rewriteRunnerTempInExecutableBodies(input)

	if !strings.Contains(got, "${{ runner.temp }}/gh-aw/actions/evil\\'") {
		t.Fatalf("expected escaped action path to remain unre-written for validation:\n%s", got)
	}
	if _, err := finalizeRunnerTempSafety(input); err == nil {
		t.Fatal("expected escaped action path to fail closed during validation")
	}
}

func TestSingleQuotedJS(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"generate_aw_info.cjs", `'generate_aw_info.cjs'`},
		{`a'b`, `'a\'b'`},
		{`a\'b`, `'a\\\'b'`},
		{`a\b`, `'a\\b'`},
		{`a"b`, `'a"b'`},
		{`a\"b`, `'a\\"b'`},
		{"a\ab", `'a\ab'`},
		{"a\bb", `'a\bb'`},
		{"a\fb", `'a\fb'`},
		{"a\nb", `'a\nb'`},
		{"a\rb", `'a\rb'`},
		{"a\tb", `'a\tb'`},
		{"a\vb", `'a\vb'`},
		{"a`b", "'a`b'"},
	}
	for _, tt := range tests {
		if got := singleQuotedJS(tt.in); got != tt.want {
			t.Errorf("singleQuotedJS(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
