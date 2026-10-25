package taint

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outputWorkflow(t *testing.T, producer, consumer, jobOutputs string) *actions.Workflow {
	t.Helper()
	text := `jobs:
  test:
    runs-on: ubuntu-latest
    env:
      TOKEN: '${{ secrets.TOKEN }}'
` + jobOutputs + "    steps:\n" + producer + consumer
	workflow, err := actions.Parse([]byte(text))
	require.NoError(t, err)
	return workflow
}

func TestNamedStepOutputShellTransport(t *testing.T) {
	testStepOutputTransports(t, nil, []string{
		`run: echo "token=$TOKEN" >> "$GITHUB_OUTPUT"`,
		`run: 'copy="$TOKEN"; destination="$GITHUB_OUTPUT"; echo "token=$copy" >> "$destination"'`,
		`run: printf 'token=%s\n' "$TOKEN" >> "$GITHUB_OUTPUT"`,
		`run: printf '%s\n' "token=$TOKEN" >> "$GITHUB_OUTPUT"`,
	})
}

func TestNamedStepOutputJavaScriptTransport(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Files = fstest.MapFS{"producer.cjs": {Data: []byte(`const {setOutput: put}=require("@actions/core"); put("token", process.env.TOKEN);`)}}
	testStepOutputTransports(t, options, []string{
		`uses: actions/github-script@v7
        with:
          script: 'const alias=core; const token=process.env.TOKEN; alias.setOutput("token", token);'`,
		`uses: actions/github-script@v7
        with:
          script: 'return { token: process.env.TOKEN };'`,
		`run: node -e 'const {setOutput:put}=require("@actions/core"); put("token", process.env.TOKEN)'`,
		`run: node producer.cjs`,
		`shell: node {0}
        run: 'require("@actions/core").setOutput("token", process.env.TOKEN)'`,
	})
}

func testStepOutputTransports(t *testing.T, options *JavaScriptOptions, producers []string) {
	t.Helper()
	consumers := []struct {
		name, fields string
		kind         Kind
	}{
		{"unused", "run: 'true'", ""},
		{"authentication", `env:
          COPY: '${{ steps.producer.outputs.token }}'
        run: 'alias="$COPY"; curl --oauth2-bearer "$alias" https://example.test'`, ""},
		{"log", `run: echo '${{ steps.producer.outputs.token }}'`, Log},
		{"summary", `run: echo '${{ steps.producer.outputs.token }}' >> "$GITHUB_STEP_SUMMARY"`, Summary},
		{"network", `env:
          COPY: '${{ steps.producer.outputs.token }}'
        run: curl --data "$COPY" https://example.test`, Network},
		{"publication", `uses: peter-evans/create-or-update-comment@v4
        with:
          body: '${{ steps.producer.outputs.token }}'`, ActionInput},
		{"artifact", `run: echo '${{ steps.producer.outputs.token }}' > token.txt
      - uses: actions/upload-artifact@v4
        with:
          path: token.txt`, Artifact},
		{"persisted env", `run: echo 'COPY=${{ steps.producer.outputs.token }}' >> "$GITHUB_ENV"
      - run: echo "$COPY"`, Log},
	}
	rule := SecretLeakRule()
	rule.JavaScript = options
	for i, fields := range producers {
		for _, consumer := range consumers {
			t.Run(fmt.Sprintf("%d/%s", i, consumer.name), func(t *testing.T) {
				ref := "token"
				if strings.Contains(fields, "return {") {
					ref = "result"
				}
				next := strings.ReplaceAll(consumer.fields, "outputs.token", "outputs."+ref)
				producer := "      - id: producer\n        " + fields + "\n"
				result, err := Analyze(outputWorkflow(t, producer, "      - "+next+"\n", ""), rule)
				require.NoError(t, err)
				require.Empty(t, result.Gaps)
				if consumer.kind == "" {
					assert.Empty(t, result.Findings)
				} else {
					require.Len(t, result.Findings, 1, "%+v", result.Findings)
					assert.Equal(t, consumer.kind, result.Findings[0].Site.Kind)
					assert.Equal(t, "secrets.token", result.Findings[0].Sources[0].Reference)
				}
			})
		}
	}
}

func TestStepOutputJobBoundaryAndGenericRule(t *testing.T) {
	for _, js := range []bool{false, true} {
		rule := SecretLeakRule()
		producer := "      - id: producer\n        run: echo 'token=${{ secrets.TOKEN }}' >> \"$GITHUB_OUTPUT\"\n"
		if js {
			rule.JavaScript = nativeJavaScriptOptions(t)
			producer = "      - id: producer\n        uses: actions/github-script@v7\n        with:\n          script: core.setOutput('token', process.env.TOKEN)\n"
		}
		workflow := outputWorkflow(t, producer, "", "    outputs:\n      token: '${{ steps.producer.outputs.token }}'\n")
		result, err := Analyze(workflow, rule)
		require.NoError(t, err)
		require.Len(t, result.Findings, 1)
		assert.Equal(t, "jobs.test.outputs.token", result.Findings[0].Site.Path)
		rule.ID = "custom-output-policy"
		result, err = Analyze(workflow, rule)
		require.NoError(t, err)
		require.Len(t, result.Findings, 2, "custom rules still see the transport Output")
	}
}

func TestUnresolvedStepOutputTransport(t *testing.T) {
	tests := []struct{ name, producer string }{
		{"dynamic name", "      - id: producer\n        run: echo \"$NAME=$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"command substitution name", "      - id: producer\n        run: echo \"pre$(unknown)fix=$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"multiple printf records", "      - id: producer\n        run: printf 'first=ok\\nsecond=%s\\n' \"$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"ANSI quoted records", "      - id: producer\n        run: echo $'first=ok\\nsecond='\"$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"ANSI quoted suffix", "      - id: producer\n        run: echo \"first=ok\"$'\\x0asecond='\"$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"ANSI quoted printf payload", "      - id: producer\n        run: printf '%s\\n' $'first=ok\\012second='\"$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"idless", "      - run: echo \"token=$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"},
		{"compound", "      - id: producer\n        run: '{ echo \"first=ok\"; echo \"second=$TOKEN\"; } >> \"$GITHUB_OUTPUT\"'\n"},
		{"unmodeled", "      - id: producer\n        run: printf '%s' \"$TOKEN\" | tee \"$GITHUB_OUTPUT\" > /dev/null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := "anything"
			if tt.name == "idless" {
				ref = "token"
			}
			result, err := Analyze(outputWorkflow(t, tt.producer, "      - run: echo '${{ steps.producer.outputs."+ref+" }}'\n", ""), SecretLeakRule())
			require.NoError(t, err)
			require.NotEmpty(t, result.Gaps)
			require.Len(t, result.Findings, 2, "%+v", result.Findings)
			assert.Equal(t, Output, result.Findings[0].Site.Kind)
			assert.Equal(t, Log, result.Findings[1].Site.Kind)
		})
	}
}

func TestUnresolvedJavaScriptOutputTransport(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	for _, script := range []string{`core.setOutput(process.env.NAME, process.env.TOKEN)`, `require("fs").appendFileSync(process.env.GITHUB_OUTPUT, process.env.TOKEN)`} {
		producer := "      - id: producer\n        uses: actions/github-script@v7\n        with:\n          script: " + script + "\n"
		result, err := Analyze(outputWorkflow(t, producer, "      - run: echo '${{ steps.producer.outputs.anything }}'\n", ""), rule)
		require.NoError(t, err)
		require.NotEmpty(t, result.Gaps)
		require.Len(t, result.Findings, 2)
		assert.Equal(t, Output, result.Findings[0].Site.Kind)
		assert.Equal(t, Log, result.Findings[1].Site.Kind)
	}
}

func TestStepOutputAuthoredShellAndUnresolvedID(t *testing.T) {
	rule := SecretLeakRule()
	a := &analyzer{rule: rule, workflowContext: true}
	env := scope{"shell.TOKEN": {{Reference: "secrets.token", Path: "env.TOKEN"}}}
	require.NoError(t, a.step(map[string]any{
		"id": "${{ env.UNKNOWN }}", "shell": "bash", "run": `echo "token=$TOKEN" >> "$GITHUB_OUTPUT"`,
	}, "steps[0]", env, make(scope), make(scope)))
	require.Len(t, a.result.Findings, 1)
	require.NotEmpty(t, a.result.Gaps)
	require.NotEmpty(t, env.lookup("steps.any-id.outputs.token"))

	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - id: producer
    shell: bash
    run: echo "token=$TOKEN" >> "$GITHUB_OUTPUT"
---
Prompt
`)
	require.NoError(t, err)
	result, err := AnalyzeAuthoredDocument(t.Context(), document, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, Output, result.Findings[0].Site.Kind)
}

func TestJavaScriptOutputTransportCrossJobAndIdless(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	for _, id := range []string{"id: producer\n        ", ""} {
		producer := "      - " + id + `uses: actions/github-script@v7
        with:
          script: return process.env.TOKEN;
`
		result, err := Analyze(outputWorkflow(t, producer, "      - run: echo '${{ steps.producer.outputs.result }}'\n", ""), rule)
		require.NoError(t, err)
		if id == "" {
			require.Len(t, result.Findings, 2)
			require.NotEmpty(t, result.Gaps)
		} else {
			require.Len(t, result.Findings, 1)
			require.Empty(t, result.Gaps)
		}
		assert.Equal(t, Log, result.Findings[len(result.Findings)-1].Site.Kind)
	}
	workflow, err := actions.Parse([]byte(`jobs:
  producer:
    runs-on: ubuntu-latest
    outputs:
      token: '${{ steps.emit.outputs.result }}'
    steps:
      - id: emit
        uses: actions/github-script@v7
        env:
          TOKEN: '${{ secrets.TOKEN }}'
        with:
          script: return process.env.TOKEN;
  consumer:
    runs-on: ubuntu-latest
    needs: producer
    steps:
      - run: echo '${{ needs.producer.outputs.token }}'
`))
	require.NoError(t, err)
	result, err := Analyze(workflow, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2)
	assert.Equal(t, Log, result.Findings[0].Site.Kind)
	assert.Equal(t, "jobs.producer.outputs.token", result.Findings[1].Site.Path)
}

func TestAuthoredOutputTransportRequiresWorkflowContext(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - id: producer
    uses: actions/github-script@v7
    with:
      script: core.setOutput("token", process.env.TOKEN)
  - env:
      COPY: '${{ steps.producer.outputs.token }}'
    run: curl --oauth2-bearer "$COPY" https://example.test
---
Prompt
`)
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	standalone, err := AnalyzeAuthoredDocument(t.Context(), document, rule)
	require.NoError(t, err)
	require.Len(t, standalone.Findings, 1)
	assert.Equal(t, Output, standalone.Findings[0].Site.Kind)
	producer := "      - id: producer\n        uses: actions/github-script@v7\n        with:\n          script: core.setOutput(\"token\", process.env.TOKEN)\n"
	consumer := "      - env:\n          COPY: '${{ steps.producer.outputs.token }}'\n        run: curl --oauth2-bearer \"$COPY\" https://example.test\n"
	for _, mode := range []JavaScriptMode{JavaScriptFast, JavaScriptDeep} {
		options.Mode = mode
		result, err := AnalyzeDocument(document, outputWorkflow(t, producer, consumer, ""), rule)
		require.NoError(t, err)
		assert.Empty(t, result.Findings, "%s: %+v", mode, result.Findings)
		// The authored transport must also reach compiled job-level outputs.
		result, err = AnalyzeDocument(document, outputWorkflow(t, producer, consumer, "    outputs:\n      token: '${{ steps.producer.outputs.token }}'\n"), rule)
		require.NoError(t, err)
		require.Len(t, result.Findings, 1, "%s: %+v", mode, result.Findings)
		assert.Equal(t, "jobs.test.outputs.token", result.Findings[0].Site.Path)
	}
}

func TestFastStepOutputKeepsEveryCompiledConsumer(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	rule.JavaScript.Mode = JavaScriptFast
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - id: producer
    uses: actions/github-script@v7
    with:
      script: return process.env.TOKEN;
  - shell: bash
    run: echo '${{ steps.producer.outputs.result }}'
---
Prompt
`)
	require.NoError(t, err)
	compiled, err := actions.Parse([]byte(`env:
  TOKEN: '${{ secrets.TOKEN }}'
jobs:
  first:
    runs-on: ubuntu-latest
    outputs:
      token: '${{ steps.producer.outputs.result }}'
    steps:
      - id: producer
        uses: actions/github-script@v7
        with:
          script: return process.env.TOKEN;
      - shell: bash
        run: echo '${{ steps.producer.outputs.result }}'
  second:
    runs-on: ubuntu-latest
    needs: first
    steps:
      - run: echo '${{ needs.first.outputs.token }}'
      - run: curl --data '${{ needs.first.outputs.token }}' https://example.test
`))
	require.NoError(t, err)
	result, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 4, "%+v", result.Findings)
	var paths []string
	for _, finding := range result.Findings {
		paths = append(paths, finding.Site.Path)
	}
	assert.ElementsMatch(t, []string{
		"jobs.first.outputs.token", "jobs.first.steps[1].run",
		"jobs.second.steps[0].run", "jobs.second.steps[1].run",
	}, paths)
}

func TestFastOutputMismatchRemainsConservative(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	rule.JavaScript.Mode = JavaScriptFast
	for _, source := range []string{
		`// authored comment
return process.env.TOKEN;`,
		`core.setOutput("token", process.env.TOKEN); // authored comment`,
	} {
		document := &aw.Document{Frontmatter: map[string]any{
			"env": map[string]any{"TOKEN": "${{ secrets.TOKEN }}"},
			"steps": []any{map[string]any{
				"id": "producer", "uses": "actions/github-script@v7",
				"with": map[string]any{"script": source},
			}},
		}}
		producer := "      - id: producer\n        uses: actions/github-script@v7\n        with:\n          script: return process.env.TOKEN;\n"
		result, err := AnalyzeDocument(document, outputWorkflow(t, producer, "", ""), rule)
		require.NoError(t, err)
		require.Len(t, result.Findings, 1, "%+v", result.Findings)
		assert.Equal(t, Output, result.Findings[0].Site.Kind)
		assert.Contains(t, fmt.Sprint(result.Gaps), "no verified compiled entry")
	}
}

func TestFastOutputDeduplicationPreservesDistinctSteps(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	rule.JavaScript.Mode = JavaScriptFast
	step := map[string]any{"uses": "actions/github-script@v7", "with": map[string]any{"script": "return process.env.TOKEN;"}}
	document := &aw.Document{Frontmatter: map[string]any{
		"env": map[string]any{"TOKEN": "${{ secrets.TOKEN }}"}, "steps": []any{step, step},
	}}
	producer := "      - uses: actions/github-script@v7\n        with:\n          script: return process.env.TOKEN;\n"
	result, err := AnalyzeDocument(document, outputWorkflow(t, producer, "", ""), rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2, "%+v", result.Findings)
	assert.Equal(t, "jobs.test.steps[0].with.script.javascript", result.Findings[0].Site.Path)
	assert.Equal(t, "steps[1].with.script.javascript", result.Findings[1].Site.Path)
}
