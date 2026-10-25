package taint

import (
	"crypto/sha256"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFastStepLocation(t *testing.T) {
	tests := []struct {
		name, location, prefix, suffix string
		index                          int
		ok                             bool
	}{
		{name: "normal", location: "jobs.agent.steps[2].run", prefix: "jobs.agent.", index: 2, suffix: ".run", ok: true},
		{name: "no step", location: "jobs.agent.with.script", ok: false},
		{name: "malformed index", location: "jobs.agent.steps[x].run", prefix: "jobs.agent.", suffix: ".run", ok: false},
		{name: "nested steps", location: "jobs.agent.steps[0].matrix.steps[3].run", prefix: "jobs.agent.steps[0].matrix.", index: 3, suffix: ".run", ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix, index, suffix, ok := fastStepLocation(test.location)
			assert.Equal(t, test.prefix, prefix)
			assert.Equal(t, test.index, index)
			assert.Equal(t, test.suffix, suffix)
			assert.Equal(t, test.ok, ok)
		})
	}
}

func TestFastSiblingSite(t *testing.T) {
	tests := []struct {
		name, compiled, authored, target, from string
		want                                   bool
	}{
		{
			name:     "matched sibling",
			compiled: "jobs.agent.steps[2].run", authored: "steps[1].run",
			target: "jobs.agent.steps[2].run", from: "steps[1].run", want: true,
		},
		{
			name:     "nested steps",
			compiled: "jobs.agent.steps[0].matrix.steps[3].run", authored: "steps[1].matrix.steps[0].run",
			target: "jobs.agent.steps[0].matrix.steps[3].run", from: "steps[1].matrix.steps[0].run", want: true,
		},
		{
			name: "missing step", compiled: "jobs.agent.run", authored: "run",
			target: "jobs.agent.steps[0].run", from: "steps[0].run",
		},
		{
			name: "malformed index", compiled: "jobs.agent.steps[x].run", authored: "steps[0].run",
			target: "jobs.agent.steps[0].run", from: "steps[0].run",
		},
		{
			name: "mismatched prefixes", compiled: "jobs.other.steps[2].run", authored: "steps[1].run",
			target: "jobs.agent.steps[2].run", from: "steps[1].run",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, fastSiblingSite(test.compiled, test.authored, test.target, test.from))
		})
	}
}

func TestJavaScriptFastPairedGraph(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - shell: bash
    run: echo "$TOKEN"
  - uses: actions/github-script@v7
    with:
      script: console.log(process.env.TOKEN);
mcp-scripts:
  custom:
    script: return process.env.TOKEN;
engine:
  behaviors:
    harness-script: console.log(process.env.TOKEN);
    log-parser: 'function parseLog(content) { return { markdown: process.env.TOKEN }; }'
    mcp:
      config-adapter: console.log(process.env.TOKEN);
---
Prompt
`)
	require.NoError(t, err)
	generated := `unknownGeneratedRuntime(process.env.TOKEN);`
	compiled, err := actions.Parse([]byte(`jobs:
  agent:
    runs-on: ubuntu-latest
    env:
      TOKEN: '${{ secrets.TOKEN }}'
    steps:
      - shell: bash
        run: echo "$TOKEN"
      - uses: actions/github-script@v7
        with:
          script: ` + generated + `
  producer:
    runs-on: ubuntu-latest
    outputs:
      token: '${{ secrets.JOB_TOKEN }}'
    steps:
      - run: "true"
  consumer:
    runs-on: ubuntu-latest
    needs: producer
    steps:
      - shell: bash
        run: echo '${{ needs.producer.outputs.token }}'
`))
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	require.NoError(t, PrimeAuthoredJavaScript(t.Context(), []*aw.Document{document}, rule, *options))
	result, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	var paths []string
	for _, finding := range result.Findings {
		paths = append(paths, finding.Site.Path)
	}
	assert.Contains(t, paths, "jobs.agent.steps[0].run", "compiled shell checking remains enabled")
	assert.Contains(t, paths, "jobs.consumer.steps[0].run", "compiled cross-job flows remain enabled")
	assert.Contains(t, paths, "steps[1].with.script.javascript")
	assert.Contains(t, paths, "mcp-scripts.custom.script.javascript")
	for _, hook := range []string{"harness-script", "log-parser", "mcp.config-adapter"} {
		assert.Contains(t, paths, "engine.behaviors."+hook+".javascript")
	}
	assert.NotContains(t, paths, "steps[0].run", "ordinary source findings do not duplicate compiled findings")
	parser := options.Parser.(*TypeScriptParser)
	assert.NotContains(t, parser.cache, sha256.Sum256([]byte(generated)))
	repeated, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	assert.Equal(t, result, repeated)
	options.Mode = JavaScriptDeep
	_, err = AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	assert.Contains(t, parser.cache, sha256.Sum256([]byte(generated)), "explicit deep mode still checks generated JavaScript")
}

func TestJavaScriptFastAuthoredShellDelta(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	script := `value=$(node -e 'process.stdout.write(process.env.TOKEN)'); echo "$value"`
	document := &aw.Document{Frontmatter: map[string]any{
		"env":   map[string]any{"TOKEN": "${{ secrets.TOKEN }}"},
		"steps": []any{map[string]any{"shell": "bash", "run": script}},
	}}
	compiled, err := actions.Parse([]byte("jobs:\n  agent:\n    runs-on: ubuntu-latest\n    env:\n      TOKEN: '${{ secrets.TOKEN }}'\n    steps:\n      - shell: bash\n        run: |\n          " + script + "\n"))
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	result, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "jobs.agent.steps[0].run", result.Findings[0].Site.Path)
}

func TestJavaScriptFastTransportVerificationIsPerSourceOccurrence(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	authored := `node -e 'require("@actions/core").setOutput("first", process.env.TOKEN)'; node -e 'require("@actions/core").setOutput("second", process.env.TOKEN)'`
	compiledScript := `node -e 'require("@actions/core").setOutput("first", process.env.TOKEN)'; node -e 'require("@actions/core").setOutput("other", process.env.TOKEN)'`
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - id: producer
    shell: bash
    run: ` + authored + `
---
Prompt
`)
	require.NoError(t, err)
	compiled, err := actions.Parse([]byte(`jobs:
  agent:
    runs-on: ubuntu-latest
    env:
      TOKEN: '${{ secrets.TOKEN }}'
    steps:
      - id: producer
        shell: bash
        run: ` + compiledScript + `
`))
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	result, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1, "%+v", result.Findings)
	assert.Equal(t, Output, result.Findings[0].Site.Kind)
}

func TestJavaScriptFastUnverifiedStdoutKeepsShellOutputFinding(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	authored := `value=$(node -e 'process.stdout.write(process.env.TOKEN)'); echo "token=$value" >> "$GITHUB_OUTPUT"`
	compiledScript := `value=$(node -e 'process.stdout.write("safe")'); echo "token=$value" >> "$GITHUB_OUTPUT"`
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
steps:
  - id: producer
    shell: bash
    run: ` + authored + `
---
Prompt
`)
	require.NoError(t, err)
	compiled, err := actions.Parse([]byte(`jobs:
  agent:
    runs-on: ubuntu-latest
    env:
      TOKEN: '${{ secrets.TOKEN }}'
    steps:
      - id: producer
        shell: bash
        run: ` + compiledScript + `
`))
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	result, err := AnalyzeDocument(document, compiled, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1, "%+v", result.Findings)
	assert.Equal(t, Output, result.Findings[0].Site.Kind)
}

func TestJavaScriptAuthoredDriverSource(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	options.Files = fstest.MapFS{
		"drivers/main.cjs":   {Data: []byte(`require("./helper.cjs").consume(process.env.TOKEN)`)},
		"drivers/helper.cjs": {Data: []byte(`module.exports = {consume: token => console.log(token)}`)},
	}
	document := &aw.Document{Frontmatter: map[string]any{
		"env":    map[string]any{"TOKEN": "${{ secrets.TOKEN }}"},
		"engine": map[string]any{"driver": "drivers/main.cjs"},
	}}
	rule := SecretLeakRule()
	rule.JavaScript = options
	result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "engine.driver.javascript[drivers/helper.cjs]", result.Findings[0].Site.Path)
}

func TestJavaScriptAuthoredDriverInterpolation(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	options.Files = fstest.MapFS{
		"driver.cjs": {Data: []byte("console.log(\"${{ secrets.TOKEN }}\"); console.log(`${{a: 1}.a}`);")},
	}
	rule := SecretLeakRule()
	rule.JavaScript = options
	for _, test := range []struct {
		name     string
		driver   any
		findings int
	}{
		{"repository-file-is-literal", "driver.cjs", 0},
		{"inline-source-is-interpolated", map[string]any{"node": `console.log("${{ secrets.TOKEN }}");`}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := &aw.Document{Frontmatter: map[string]any{
				"engine": map[string]any{"driver": test.driver},
			}}
			require.NoError(t, PrimeAuthoredJavaScript(t.Context(), []*aw.Document{document}, rule, *options))
			result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
			require.NoError(t, err)
			assert.Len(t, result.Findings, test.findings)
			assert.Empty(t, result.Gaps)
		})
	}
}

func TestAuthoredOperationalGraderEncoding(t *testing.T) {
	document, err := aw.Parse(`---
graders:
  operational-value:
    script: |
      echo '${{ secrets.NOT_INTERPOLATED }}'
      echo "$GH_TOKEN"
---
`)
	require.NoError(t, err)
	result, err := AnalyzeAuthoredScripts(t.Context(), document, SecretLeakRule())
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	require.Len(t, result.Findings[0].Sources, 1)
	assert.Equal(t, "github.token", result.Findings[0].Sources[0].Reference)
}

func TestAuthoredOperationalGraderEnvironment(t *testing.T) {
	allowed := []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot", "ComSpec", "GH_HOST", "GITHUB_API_URL", "GITHUB_GRAPHQL_URL", "GITHUB_SERVER_URL"}
	names := append(append([]string{}, allowed...), "GH_TOKEN", "TOKEN", "GITHUB_TOKEN", "GITHUB_WORKSPACE", "RUNNER_TEMP", "CI")
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			document := &aw.Document{Frontmatter: map[string]any{
				"env": map[string]any{name: "${{ secrets.BINDING }}"},
				"graders": map[string]any{"operational-value": map[string]any{
					"script": `echo "$` + name + `"`,
				}},
			}}
			result, err := AnalyzeAuthoredScripts(t.Context(), document, SecretLeakRule())
			require.NoError(t, err)
			if name != "GH_TOKEN" && !slices.Contains(allowed, name) {
				assert.Empty(t, result.Findings)
				return
			}
			require.Len(t, result.Findings, 1)
			require.Len(t, result.Findings[0].Sources, 1)
			expected := "secrets.binding"
			if name == "GH_TOKEN" {
				expected = "github.token"
			}
			assert.Equal(t, expected, result.Findings[0].Sources[0].Reference)
		})
	}
}
