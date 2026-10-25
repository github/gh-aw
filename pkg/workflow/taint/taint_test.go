package taint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checkSteps(t *testing.T, steps string) Result {
	t.Helper()
	content := "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + steps
	workflow, err := actions.Parse([]byte(content))
	require.NoError(t, err)
	before, err := actions.Emit(workflow)
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	after, err := actions.Emit(workflow)
	require.NoError(t, err)
	assert.Equal(t, before, after, "analysis must not mutate the input AST or retained source")
	return result
}

func TestCheckSecretLeaksReportsOTLPAttributeValues(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
env:
  GH_AW_OTLP_ATTRIBUTES: '{"account":"${{ secrets.API_TOKEN }}"}'
jobs:
  export:
    runs-on: ubuntu-latest
    steps:
      - run: echo ready
`))
	require.NoError(t, err)

	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, OTLPAttribute, result.Findings[0].Site.Kind)
	assert.Equal(t, "env.GH_AW_OTLP_ATTRIBUTES", result.Findings[0].Site.Path)
}

func TestSecretLeakSinksAndFilters(t *testing.T) {
	tests := []struct {
		name  string
		run   string
		kind  Kind
		count int
	}{
		{"echo", `echo "$TOKEN"`, Log, 1},
		{"printf", `printf '%s\n' "$TOKEN"`, Log, 1},
		{"summary", `echo "$TOKEN" >> "$GITHUB_STEP_SUMMARY"`, Summary, 1},
		{"output", `echo "token=$TOKEN" >> "$GITHUB_OUTPUT"`, Output, 1},
		{"alias", `copy="$TOKEN"; echo "$copy"`, Log, 1},
		{"export alias", `export copy="$TOKEN"; echo "$copy"`, Log, 1},
		{"function", `show() { echo "$1"; }; show "$TOKEN"`, Log, 1},
		{"captured function", `encode() { printf '%s' "$1" | base64; }; copy=$(encode "$TOKEN"); echo "$copy"`, Log, 1},
		{"captured transformation", `copy=$(printf '%s' "$TOKEN" | base64); echo "$copy"`, Log, 1},
		{"pipeline", `printf '%s' "$TOKEN" | base64`, Log, 1},
		{"tee", `printf '%s' "$TOKEN" | tee /tmp/secret`, Log, 1},
		{"heredoc", "cat <<EOF\n$TOKEN\nEOF", Log, 1},
		{"quoted heredoc", "cat <<'EOF'\n$TOKEN\nEOF", "", 0},
		{"printenv", `printenv TOKEN`, Log, 1},
		{"env", `env`, Log, 1},
		{"redirect then print", `echo "$TOKEN" > token.txt; cat token.txt`, Log, 1},
		{"curl URL", `curl "https://example.test/?value=$TOKEN"`, Network, 1},
		{"curl body", `curl -d "$TOKEN" https://example.test`, Network, 1},
		{"authentication", `curl --oauth2-bearer "$TOKEN" https://example.test`, "", 0},
		{"xtrace authentication", `set -x; curl --oauth2-bearer "$TOKEN" https://example.test`, Log, 1},
		{"fixed point does not carry later tracing", `copy="$TOKEN"; curl --oauth2-bearer "$copy" https://example.test; set -x`, "", 0},
		{"xtrace can be disabled", `set -x; echo "$TOKEN"; set +x; echo "$TOKEN"`, Log, 1},
		{"xtrace option can be disabled", `set -o xtrace; echo "$TOKEN"; set +o xtrace; echo "$TOKEN"`, Log, 1},
		{"trace state is isolated between branches", `if true; then set -x; else curl --oauth2-bearer "$TOKEN" https://example.test; fi`, "", 0},
		{"trace state merges after branches", `if true; then set -x; fi; curl --oauth2-bearer "$TOKEN" https://example.test`, Log, 1},
		{"trace state is isolated between case arms", `case value in value) set -x;; other) curl --oauth2-bearer "$TOKEN" https://example.test;; esac`, "", 0},
		{"trace state flows through logical command", `set -x && curl --oauth2-bearer "$TOKEN" https://example.test`, Log, 1},
		{"api key authentication", `curl -H "x-goog-api-key: $TOKEN" https://example.test`, "", 0},
		{"non-authentication header", `curl -H "X-Data: $TOKEN" https://example.test`, Network, 1},
		{"stderr", `echo "$TOKEN" >&2`, Log, 1},
		{"stderr-only redirection", `echo "$TOKEN" 2>/dev/null`, Log, 1},
		{"mask registration", `echo "::add-mask::$TOKEN"`, "", 0},
		{"printf mask registration", `printf '::add-mask::%s\n' "$TOKEN"`, "", 0},
		{"mask-looking content with other output", `printf '::add-mask::dummy\n%s\n' "$TOKEN"`, Log, 1},
		{"mask is not sanitizer", `echo "::add-mask::$TOKEN"; echo "$TOKEN"`, Log, 1},
		{"assignment-only redirection", `VALUE=ok > result.txt`, "", 0},
		{"target constant reassignment", `FILE=/dev/null; FILE=token.txt; printf '%s' "$TOKEN" > "$FILE"; cat token.txt`, Log, 1},
		{"export preserves literal target", `FILE=token.txt; export FILE; echo "$TOKEN" > "$FILE"; cat token.txt`, Log, 1},
		{"branch first constant target", `if true; then FILE=first.txt; else FILE=second.txt; fi; echo "$TOKEN" > "$FILE"; cat first.txt`, Log, 1},
		{"branch second constant target", `if true; then FILE=first.txt; else FILE=second.txt; fi; echo "$TOKEN" > "$FILE"; cat second.txt`, Log, 1},
		{"prefix environment", `COPY="$TOKEN" printenv COPY`, Log, 1},
		{"prefix environment is restored", `COPY="$TOKEN" true; echo "$COPY"`, "", 0},
		{"prefix assignment does not affect argument expansion", `COPY="$TOKEN" echo "$COPY"`, "", 0},
		{"env local environment", `env COPY="$TOKEN" printenv COPY`, Log, 1},
		{"env command does not print environment", `env COPY="$TOKEN" true`, "", 0},
		{"env clears environment after argument expansion", `env -i echo "$TOKEN"`, Log, 1},
		{"function aggregate parameters", `show() { printf '%s\n' "$@"; }; show "$TOKEN"`, Log, 1},
		{"function star parameters", `show() { echo "$*"; }; show "$TOKEN"`, Log, 1},
		{"nested function parameters", `show() { echo "$@"; }; relay() { show "$1"; }; relay "$TOKEN"`, Log, 1},
		{"function arguments expand before positional binding", `show() { echo "$@"; }; swap() { show "$2" "$1"; }; swap "$TOKEN" clean`, Log, 1},
		{"pipeline network body", `printf '%s' "$TOKEN" | curl --data-binary @- https://example.test`, Network, 1},
		{"upload tracked file", `printf '%s' "$TOKEN" > payload.txt; curl --data-binary @payload.txt https://example.test`, Network, 1},
		{"upload tracked glob", `printf '%s' "$TOKEN" > payload.txt; curl --data-binary @payload*.txt https://example.test`, Network, 1},
		{"curl upload file option", `printf '%s' "$TOKEN" > payload.txt; curl -T payload.txt https://example.test`, Network, 1},
		{"curl stdin upload option", `printf '%s' "$TOKEN" | curl -T - https://example.test`, Network, 1},
		{"curl nonblocking stdin upload option", `printf '%s' "$TOKEN" | curl -T . https://example.test`, Network, 1},
		{"curl long nonblocking stdin upload option", `printf '%s' "$TOKEN" | curl --upload-file . https://example.test`, Network, 1},
		{"wget tracked post file", `printf '%s' "$TOKEN" > payload.txt; wget --post-file=payload.txt https://example.test`, Network, 1},
		{"wget dot is not stdin", `printf '%s' "$TOKEN" | wget --post-file=. https://example.test`, "", 0},
		{"wget separated dot is not stdin", `printf '%s' "$TOKEN" | wget --post-file . https://example.test`, "", 0},
		{"wget dash is not stdin", `printf '%s' "$TOKEN" | wget --post-file=- https://example.test`, "", 0},
		{"wget separated dash is not stdin", `printf '%s' "$TOKEN" | wget --post-file - https://example.test`, "", 0},
		{"wget tracked dash file", `printf '%s' "$TOKEN" > ./-; wget --post-file=- https://example.test`, Network, 1},
		{"wget separated tracked dash file", `printf '%s' "$TOKEN" > ./-; wget --post-file - https://example.test`, Network, 1},
		{"read tracked glob", `printf '%s' "$TOKEN" > payload.txt; curl --data-binary "$(cat *.txt)" https://example.test`, Network, 1},
		{"summary preserves mask-looking content", `echo "::add-mask::$TOKEN" >> "$GITHUB_STEP_SUMMARY"`, Summary, 1},
		{"discard", `echo "$TOKEN" > /dev/null`, "", 0},
		{"file only", `echo "$TOKEN" > token.txt`, "", 0},
		{"single quotes", `echo '$TOKEN'`, "", 0},
		{"case sensitive shell", `echo "$token"`, "", 0},
		{"unrelated", `echo "no credentials"`, "", 0},
		{"branch union", `if true; then copy="$TOKEN"; else copy=clean; fi; echo "$copy"`, Log, 1},
		{"loop carried", `while false; do echo "$copy"; copy="$TOKEN"; done`, Log, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var steps strings.Builder
			steps.WriteString("      - env:\n          TOKEN: ${{ secrets.TEST_TOKEN }}\n        run: |\n")
			for line := range strings.SplitSeq(tt.run, "\n") {
				steps.WriteString("          " + line + "\n")
			}
			result := checkSteps(t, steps.String())
			require.Empty(t, result.Gaps)
			require.Len(t, result.Findings, tt.count, "%+v", result.Findings)
			if tt.count > 0 {
				assert.Equal(t, tt.kind, result.Findings[0].Site.Kind)
				assert.Equal(t, "secrets.test_token", result.Findings[0].Sources[0].Reference)
			}
		})
	}
}

func TestShellInvocationTraceOptions(t *testing.T) {
	result := checkSteps(t, `      - shell: "bash -x {0}"
        env:
          TOKEN: ${{ secrets.TEST_TOKEN }}
        run: curl --oauth2-bearer "$TOKEN" https://example.test
`)
	require.Len(t, result.Findings, 1, "%+v", result.Findings)
	assert.Equal(t, Log, result.Findings[0].Site.Kind)
}

func TestEnvCommandExecutionIsNotEnvironmentListing(t *testing.T) {
	result := checkSteps(t, `      - env:
          TOKEN: ${{ secrets.TEST_TOKEN }}
        run: |
          env true
          env GOOS=linux go build ./...
`)
	assert.Empty(t, result.Findings, "%+v", result.Findings)
	require.Len(t, result.Gaps, 1, "%+v", result.Gaps)
	assert.Contains(t, result.Gaps[0].Reason, "env command")
}

func TestPersistedEnvironmentRecordsAndUnknownNames(t *testing.T) {
	result := checkSteps(t, `      - env:
          TOKEN: ${{ secrets.TEST_TOKEN }}
        run: |
          {
            echo 'COPY<<EOF'
            echo "$TOKEN"
            echo EOF
          } >> "$GITHUB_ENV"
      - run: echo "$COPY"
`)
	require.Empty(t, result.Gaps)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, Log, result.Findings[0].Site.Kind)

	result = checkSteps(t, `      - env:
          TOKEN: ${{ secrets.TEST_TOKEN }}
        run: |
          echo "$NAME=$TOKEN" >> "$GITHUB_ENV"
          F=$(mktemp)
          echo "$TOKEN" > "$F"
`)
	assert.Empty(t, result.Findings)
	require.Len(t, result.Gaps, 2, "%+v", result.Gaps)

	result = checkSteps(t, `      - env:
          TOKEN: ${{ secrets.TEST_TOKEN }}
          NAME: key
        run: printf '%s=%s\n' "$NAME" "$TOKEN" >> "$GITHUB_ENV"
`)
	assert.Empty(t, result.Findings)
	require.Len(t, result.Gaps, 1)
	assert.Contains(t, result.Gaps[0].Reason, "dynamic GITHUB_ENV")
}

func TestActionsExpressionTaint(t *testing.T) {
	tests := []struct {
		expr    string
		tainted bool
	}{
		{"secrets.TOKEN", true},
		{"SECRETS['TOKEN']", true},
		{"secrets[vars.SECRET_NAME]", true},
		{"github.token", true},
		{"toJSON(secrets)", true},
		{"toJSON(github)", true},
		{"format('prefix {0}', secrets.TOKEN)", true},
		{"join(fromJSON(secrets.TOKEN), ',')", true},
		{"secrets.TOKEN || secrets.FALLBACK", true},
		{"secrets.TOKEN != '' && secrets.TOKEN || 'fallback'", true},
		{"secrets.TOKEN != '' && 'present' || 'absent'", false},
		{"secrets.TOKEN == 'literal-sensitive-value'", false},
		{"!secrets.TOKEN", false},
		{"contains(secrets.TOKEN, 'x')", false},
		{"'secrets.TOKEN'", false},
		{"format('}} {0}', secrets.TOKEN)", true},
		{"format('it''s }} {0}', secrets.TOKEN)", true},
		{"github.repository", false},
		{"env[secrets.INDEX].value", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			result := checkSteps(t, "      - run: |\n          echo '${{ "+tt.expr+" }}'\n")
			if tt.tainted {
				require.Len(t, result.Findings, 1)
			} else {
				assert.Empty(t, result.Findings)
			}
		})
	}
}

func TestEnvironmentAndOutputPropagation(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
env:
  TOKEN: ${{ secrets.TEST_TOKEN }}
jobs:
  producer:
    runs-on: ubuntu-latest
    outputs:
      credential: ${{ steps.emit.outputs.credential }}
    steps:
      - id: emit
        run: echo "credential=$TOKEN" >> "$GITHUB_OUTPUT"
      - run: echo "COPY=$TOKEN" >> "$GITHUB_ENV"
      - run: echo "$COPY"
      - env:
          TOKEN: safe
        run: echo "$TOKEN"
      - run: echo "$TOKEN"
      - run: echo '${{ steps.emit.outputs.credential }}'
  consumer:
    needs: producer
    runs-on: ubuntu-latest
    steps:
      - env:
          FORWARDED: ${{ needs.producer.outputs.credential }}
        run: echo "$FORWARDED"
  unrelated:
    runs-on: ubuntu-latest
    steps:
      - run: echo "$COPY"
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Empty(t, result.Gaps)
	require.Len(t, result.Findings, 6, "%+v", result.Findings)
	for _, finding := range result.Findings {
		assert.NotContains(t, finding.Site.Path, "unrelated")
		assert.NotEqual(t, "jobs.producer.steps[3].run", finding.Site.Path)
	}
}

func TestPersistedEnvironmentIsAvailableToJobOutputs(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  producer:
    runs-on: ubuntu-latest
    outputs:
      credential: ${{ env.COPY }}
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: printf '%s\n' "COPY=$TOKEN" >> "$GITHUB_ENV"
  consumer:
    needs: producer
    runs-on: ubuntu-latest
    steps:
      - env:
          FORWARDED: ${{ needs.producer.outputs.credential }}
        run: echo "$FORWARDED"
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Empty(t, result.Gaps)
	require.Len(t, result.Findings, 2, "%+v", result.Findings)
	kinds := []Kind{result.Findings[0].Site.Kind, result.Findings[1].Site.Kind}
	assert.Contains(t, kinds, Output)
	assert.Contains(t, kinds, Log)
}

func TestActionSinksAndCredentialOutputs(t *testing.T) {
	result := checkSteps(t, `      - id: app
        uses: actions/create-github-app-token@v2
        with:
          private-key: ${{ secrets.PRIVATE_KEY }}
      - uses: actions/checkout@v6
        with:
          token: ${{ steps.app.outputs.token }}
      - run: echo '${{ steps.app.outputs.token }}'
      - uses: actions/cache@v4
        with:
          path: cache
          key: ${{ secrets.TOKEN }}
      - uses: peter-evans/create-or-update-comment@v4
        with:
          token: ${{ github.token }}
          body: ${{ secrets.TOKEN }}
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: echo "$TOKEN" > token.txt
      - uses: actions/upload-artifact@v4
        with:
          path: token.txt
`)
	require.Len(t, result.Findings, 4, "%+v", result.Findings)
	assert.Contains(t, result.Findings[0].Sources[0].Reference, "steps.app.outputs.token")
	assert.Equal(t, Artifact, result.Findings[3].Site.Kind)
}

func TestReusableWorkflowInputs(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  call:
    uses: ./.github/workflows/worker.yml
    with:
      public-input: ${{ secrets.TOKEN }}
    secrets:
      token: ${{ secrets.TOKEN }}
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, WorkflowInput, result.Findings[0].Site.Kind)
}

func TestGroupedOutputsAndArtifactPaths(t *testing.T) {
	result := checkSteps(t, `      - id: emit
        env:
          TOKEN: ${{ secrets.TOKEN }}
        run: |
          DIR=/tmp/taint
          FILE="$DIR/token.txt"
          printf '%s' "$TOKEN" > "$FILE"
          {
            echo 'credential<<EOF'
            echo "$TOKEN"
            echo EOF
          } >> "$GITHUB_OUTPUT"
      - uses: actions/upload-artifact@v4
        with:
          path: /tmp/taint/token.txt
      - uses: actions/cache@v4
        with:
          key: ${{ steps.emit.outputs.credential }}
          path: cache
`)
	require.Empty(t, result.Gaps)
	require.Len(t, result.Findings, 3)
	assert.Equal(t, Output, result.Findings[0].Site.Kind)
	assert.Equal(t, Artifact, result.Findings[1].Site.Kind)
	assert.Equal(t, ActionInput, result.Findings[2].Site.Kind)
}

func TestWorkingDirectoryArtifactPatternsAndExclusions(t *testing.T) {
	tests := []struct {
		name         string
		patterns     string
		wantArtifact bool
	}{
		{"recursive match", "**/*.txt", true},
		{"excluded secret file", "subdir\n!subdir/token.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow, err := actions.Parse([]byte(fmt.Sprintf(`
defaults:
  run:
    shell: bash
    working-directory: subdir
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: printf '%%s' "$TOKEN" > token.txt
      - uses: actions/upload-artifact@v4
        with:
          path: %q
`, tt.patterns)))
			require.NoError(t, err)
			result, err := CheckSecretLeaks(workflow)
			require.NoError(t, err)
			require.Empty(t, result.Gaps)
			if tt.wantArtifact {
				require.Len(t, result.Findings, 1, "%+v", result.Findings)
				assert.Equal(t, Artifact, result.Findings[0].Site.Kind)
			} else {
				assert.Empty(t, result.Findings)
			}
		})
	}
}

func TestInheritedWindowsShellReportsCoverageGap(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  test:
    runs-on: windows-latest
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: Write-Output $env:TOKEN
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Empty(t, result.Findings)
	require.Len(t, result.Gaps, 1)
	assert.Contains(t, result.Gaps[0].Reason, "only bash and sh")
}

func TestRunShellDefaultPrecedence(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
defaults:
  run:
    shell: pwsh
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        shell: bash
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: echo "$TOKEN"
      - shell: pwsh
        env:
          TOKEN: ${{ secrets.TOKEN }}
        run: Write-Output $env:TOKEN
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, Log, result.Findings[0].Site.Kind)
	require.Len(t, result.Gaps, 1)
	assert.Contains(t, result.Gaps[0].Reason, "only bash and sh")
}

func TestEffectiveWorkingDirectoryPrecedence(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
defaults:
  run:
    working-directory: workflow-dir
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: job-dir
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: printf '%s' "$TOKEN" > token.txt
      - working-directory: step-dir
        env:
          TOKEN: ${{ secrets.TOKEN }}
        run: printf '%s' "$TOKEN" > token.txt
      - uses: actions/upload-artifact@v4
        with:
          path: job-dir/token.txt
      - uses: actions/upload-artifact@v4
        with:
          path: step-dir/token.txt
`))
	require.NoError(t, err)
	result, err := CheckSecretLeaks(workflow)
	require.NoError(t, err)
	require.Empty(t, result.Gaps)
	require.Len(t, result.Findings, 2, "%+v", result.Findings)
	for _, finding := range result.Findings {
		assert.Equal(t, Artifact, finding.Site.Kind)
	}
}

func TestPromptEnvironmentAlias(t *testing.T) {
	document, err := aw.Parse("---\non: workflow_dispatch\n---\nUse ${{ env.CREDENTIAL }}.\n")
	require.NoError(t, err)
	workflow, err := actions.Parse([]byte(`
jobs:
  activation:
    runs-on: ubuntu-latest
    env:
      CREDENTIAL: ${{ secrets.TOKEN }}
    steps:
      - run: echo safe
`))
	require.NoError(t, err)
	result, err := AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, Prompt, result.Findings[0].Site.Kind)
}

func TestPromptDoesNotUseAgentEnvironment(t *testing.T) {
	document, err := aw.Parse("---\non: workflow_dispatch\n---\nUse ${{ env.CREDENTIAL }}.\n")
	require.NoError(t, err)
	workflow, err := actions.Parse([]byte(`
jobs:
  activation:
    runs-on: ubuntu-latest
    steps:
      - run: echo safe
  agent:
    runs-on: ubuntu-latest
    env:
      CREDENTIAL: ${{ secrets.TOKEN }}
    steps:
      - run: echo safe
`))
	require.NoError(t, err)
	result, err := AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	assert.Empty(t, result.Findings)
}

func TestTypedASTAndAWPrompt(t *testing.T) {
	document, err := aw.Parse("---\non: workflow_dispatch\n---\nUse ${{ secrets.TOKEN }}.\n")
	require.NoError(t, err)
	workflow := &actions.Workflow{Jobs: []*actions.Job{{
		ID: "test", RunsOn: "ubuntu-latest",
		Steps: []actions.Step{actions.RunStep{
			StepOptions: actions.StepOptions{Env: map[string]actions.Expression{"TOKEN": actions.Secret("TOKEN")}},
			Run:         `echo "$TOKEN"`,
		}},
	}}}
	result, err := AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	require.Len(t, result.Findings, 2)
	assert.Equal(t, Prompt, result.Findings[1].Site.Kind)
}

func TestAnalysisCoverageAndErrors(t *testing.T) {
	for _, shell := range []string{"pwsh", "python"} {
		t.Run(shell, func(t *testing.T) {
			result := checkSteps(t, fmt.Sprintf("      - shell: %s\n        run: echo anything\n", shell))
			require.Len(t, result.Gaps, 1)
			assert.Contains(t, result.Gaps[0].Reason, "bash and sh")
		})
	}
	result := checkSteps(t, "      - run: |\n          echo 'unterminated\n")
	require.Len(t, result.Gaps, 1)
	assert.NotContains(t, result.Gaps[0].Reason, "unterminated")
	result = checkSteps(t, "      - env:\n          TOKEN: ${{ secrets.TOKEN }}\n        run: printf '%s' \"$TOKEN\" | external-program\n")
	require.Len(t, result.Gaps, 1)
	assert.Contains(t, result.Gaps[0].Reason, "external command")
	assert.Empty(t, result.Findings, "unknown external output is a coverage gap, not a confirmed disclosure")
	_, err := Analyze(nil, SecretLeakRule())
	require.Error(t, err)
	_, err = Analyze(&actions.Workflow{}, Rule{})
	require.Error(t, err)
	a := &analyzer{rule: SecretLeakRule()}
	_, err = a.text("${{ secrets.TOKEN + 'sensitive-literal' }}", "env.TOKEN", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sensitive-literal")
	_, err = optionalField[string](map[string]any{"name": 42}, "name", "steps[0]")
	require.ErrorContains(t, err, "invalid field type at steps[0].name")
}

func TestCustomTaintRule(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ inputs.untrusted }}'
`))
	require.NoError(t, err)
	rule := Rule{
		ID:       "untrusted-log",
		IsSource: func(ref string) bool { return strings.HasPrefix(ref, "inputs.") },
		IsSink:   func(site Site) bool { return site.Kind == Log },
	}
	result, err := Analyze(workflow, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "untrusted-log", result.Findings[0].RuleID)
}
