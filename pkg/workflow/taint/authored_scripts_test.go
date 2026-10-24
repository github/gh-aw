package taint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthoredScriptEntryPoints(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
  HOME: '${{ secrets.HOME }}'
steps:
  - uses: actions/github-script@v7
    with:
      script: return process.env.TOKEN;
post-steps:
  - shell: node
    run: console.log(process.env.TOKEN);
jobs:
  custom:
    steps:
      - shell: bash
        run: node -e 'console.log(process.env.TOKEN)'
safe-outputs:
  jobs:
    custom:
      steps:
        - shell: node
          run: console.log(process.env.TOKEN);
  scripts:
    custom:
      script: return process.env.TOKEN;
mcp-scripts:
  js:
    env:
      LOCAL: '${{ secrets.LOCAL }}'
    script: return process.env.LOCAL;
  shell:
    run: echo "$TOKEN"
  python:
    py: print("not JavaScript")
  go:
    go: package main
graders:
  custom:
    script: return trace.token;
  operational-value:
    script: echo "$TOKEN"
tools:
  cache-memory:
    validation:
      script: console.log(process.env.HOME);
  drive-memory:
    - validation: console.log(process.env.HOME);
  repo-memory:
    - validation-script: console.log(process.env.HOME);
    - custom-validation: console.log(process.env.HOME);
engine:
  id: test
  driver:
    node: console.log(process.env.TOKEN);
  behaviors:
    harness-script: console.log(process.env.TOKEN);
    log-parser: 'function parseLog(content) { return { markdown: process.env.TOKEN }; }'
    mcp:
      config-adapter: console.log(process.env.TOKEN);
---
Prompt
`)
	require.NoError(t, err)
	before, err := json.Marshal(document)
	require.NoError(t, err)
	rule := SecretLeakRule()
	isSource := rule.IsSource
	rule.IsSource = func(ref string) bool { return ref == "trace.token" || isSource(ref) }
	require.NoError(t, PrimeAuthoredJavaScript(t.Context(), []*aw.Document{document}, rule, *options))
	parser := options.Parser.(*TypeScriptParser)
	parsed := len(parser.cache)
	rule.JavaScript = options
	result, err := AnalyzeAuthoredDocument(t.Context(), document, rule)
	require.NoError(t, err)
	var paths []string
	for _, finding := range result.Findings {
		paths = append(paths, finding.Site.Path)
	}
	assert.ElementsMatch(t, []string{
		"steps[0].with.script.javascript",
		"post-steps[0].run.javascript",
		"jobs.custom.steps[0].run.javascript",
		"safe-outputs.jobs.custom.steps[0].run.javascript",
		"safe-outputs.scripts.custom.script.javascript",
		"mcp-scripts.js.script.javascript",
		"mcp-scripts.shell.run",
		"graders.custom.script.javascript",
		"tools.cache-memory.validation.script.javascript",
		"tools.drive-memory[0].validation.javascript",
		"tools.repo-memory[0].validation-script.javascript",
		"tools.repo-memory[1].custom-validation.javascript",
		"engine.driver.node.javascript",
		"engine.behaviors.harness-script.javascript",
		"engine.behaviors.log-parser.javascript",
		"engine.behaviors.mcp.config-adapter.javascript",
	}, paths)
	assert.Len(t, parser.cache, parsed, "all entry scripts were warmed, including Node eval sources")
	for _, language := range []string{"python.py", "go.go"} {
		assert.Contains(t, result.Gaps, CoverageGap{
			Path: "mcp-scripts." + language, Reason: "authored script language is not modeled",
		})
	}
	after, err := json.Marshal(document)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "source AST remains read-only")
}

func TestAuthoredScriptSandboxAndPayloadBoundaries(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	document, err := aw.Parse(`---
env:
  TOKEN: '${{ secrets.TOKEN }}'
graders:
  custom:
    script: 'console.log(process.env.TOKEN); return "${{ secrets.TOKEN }}";'
tools:
  cache-memory:
    validation:
      script: 'console.log(process.env.TOKEN); console.log("${{ secrets.TOKEN }}");'
safe-outputs:
  scripts:
    custom:
      script: 'console.log("${{ secrets.TOKEN }}");'
mcp-scripts:
  custom:
    script: 'console.log("${{ secrets.TOKEN }}");'
---
`)
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	require.NoError(t, PrimeAuthoredJavaScript(t.Context(), []*aw.Document{document}, rule, *options))
	result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2)
	for _, finding := range result.Findings {
		assert.True(t, strings.HasPrefix(finding.Site.Path, "mcp-scripts.") || strings.HasPrefix(finding.Site.Path, "safe-outputs.scripts."))
	}
}

func TestAuthoredScriptSchemaLocations(t *testing.T) {
	document, err := aw.Parse(`---
mcp-scripts:
  custom:
    script: console.log(1)
    inputs:
      data:
        schema:
          properties:
            script:
              default: not code
enclaves:
  - script:
      max-script-bytes: 100
tools:
  unrelated:
    script: not code
---
` + "```javascript\nnot code\n```\n")
	require.NoError(t, err)
	scripts, err := authoredScripts(document)
	require.NoError(t, err)
	require.Len(t, scripts, 1)
	assert.Equal(t, "mcp-scripts.custom.script", scripts[0].path)
	document.Frontmatter["mcp-scripts"] = map[string]any{"custom": map[string]any{"script": 42}}
	_, err = authoredScripts(document)
	require.ErrorContains(t, err, "script must be a string")
	assert.NotContains(t, err.Error(), "42")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = AnalyzeAuthoredScripts(ctx, document, SecretLeakRule())
	require.ErrorIs(t, err, context.Canceled)
}

func TestAuthoredScriptOwningEnvironments(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	options.Files = fstest.MapFS{"driver.cjs": {Data: []byte(`console.log(process.env.TOKEN);`)}}
	rule := SecretLeakRule()
	rule.JavaScript = options
	for _, hook := range []string{"inline-driver", "file-driver", "harness", "safe-output"} {
		for _, test := range []struct {
			name      string
			workflow  string
			owner     string
			reference string
		}{
			{"owner-secret", "public", "${{ secrets.OWNER }}", "secrets.owner"},
			{"owner-overrides-secret", "${{ secrets.WORKFLOW }}", "public", ""},
			{"inherits-workflow", "${{ secrets.WORKFLOW }}", "", "secrets.workflow"},
			{"isolates-other-owners", "public", "", ""},
			{"owner-expression-sees-workflow", "${{ secrets.WORKFLOW }}", "${{ env.TOKEN }}", "secrets.workflow"},
		} {
			t.Run(hook+"/"+test.name, func(t *testing.T) {
				engine := map[string]any{}
				outputs := map[string]any{}
				owner := engine
				site := "engine.driver.node.javascript"
				switch hook {
				case "inline-driver":
					engine["driver"] = map[string]any{"node": "console.log(process.env.TOKEN);"}
				case "file-driver":
					engine["driver"] = "driver.cjs"
					site = "engine.driver.javascript[driver.cjs]"
				case "harness":
					engine["behaviors"] = map[string]any{"harness-script": "console.log(process.env.TOKEN);"}
					site = "engine.behaviors.harness-script.javascript"
				case "safe-output":
					owner = outputs
					outputs["scripts"] = map[string]any{"custom": map[string]any{"script": "return process.env.TOKEN;"}}
					site = "safe-outputs.scripts.custom.script.javascript"
				}
				if test.owner != "" {
					owner["env"] = map[string]any{"TOKEN": test.owner}
				}
				if hook == "safe-output" {
					engine["env"] = map[string]any{"TOKEN": "${{ secrets.UNRELATED }}"}
				} else {
					outputs["env"] = map[string]any{"TOKEN": "${{ secrets.UNRELATED }}"}
				}
				document := &aw.Document{Frontmatter: map[string]any{
					"env":          map[string]any{"TOKEN": test.workflow},
					"engine":       engine,
					"safe-outputs": outputs,
				}}
				result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
				require.NoError(t, err)
				if test.reference == "" {
					assert.Empty(t, result.Findings)
					return
				}
				require.Len(t, result.Findings, 1)
				assert.Equal(t, site, result.Findings[0].Site.Path)
				require.Len(t, result.Findings[0].Sources, 1)
				assert.Equal(t, test.reference, result.Findings[0].Sources[0].Reference)
			})
		}
	}
}

func TestAuthoredEngineEnvironmentPrecedence(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	rule := SecretLeakRule()
	rule.JavaScript = options
	for _, test := range []struct {
		name      string
		execution string
		engine    string
		agent     string
		reference string
	}{
		{"execution", "${{ secrets.EXECUTION }}", "", "", "secrets.execution"},
		{"engine-overrides-execution", "${{ secrets.EXECUTION }}", "public", "", ""},
		{"agent-overrides-engine", "public", "${{ secrets.ENGINE }}", "public", ""},
		{"agent-secret", "public", "public", "${{ secrets.AGENT }}", "secrets.agent"},
		{"agent-expression-sees-workflow", "public", "public", "${{ env.TOKEN }}", "secrets.workflow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := map[string]any{
				"behaviors": map[string]any{
					"execution":      map[string]any{"env": map[string]any{"TOKEN": test.execution}},
					"harness-script": "console.log(process.env.TOKEN);",
				},
			}
			agent := map[string]any{}
			if test.engine != "" {
				engine["env"] = map[string]any{"TOKEN": test.engine}
			}
			if test.agent != "" {
				agent["env"] = map[string]any{"TOKEN": test.agent}
			}
			document := &aw.Document{Frontmatter: map[string]any{
				"env":     map[string]any{"TOKEN": "${{ secrets.WORKFLOW }}"},
				"engine":  engine,
				"sandbox": map[string]any{"agent": agent},
			}}
			result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
			require.NoError(t, err)
			if test.reference == "" {
				assert.Empty(t, result.Findings)
				return
			}
			require.Len(t, result.Findings, 1)
			require.Len(t, result.Findings[0].Sources, 1)
			assert.Equal(t, test.reference, result.Findings[0].Sources[0].Reference)
		})
	}
}

func TestAuthoredEngineEnvironmentSandboxShorthands(t *testing.T) {
	t.Parallel()
	analyzer := &analyzer{rule: SecretLeakRule()}
	for _, test := range []struct {
		name    string
		sandbox any
		invalid bool
	}{
		{"absent", nil, false},
		{"awf", "awf", false},
		{"default", "default", false},
		{"empty object", map[string]any{}, false},
		{"null agent", map[string]any{"agent": nil}, false},
		{"disabled agent", map[string]any{"agent": false}, false},
		{"awf agent", map[string]any{"agent": "awf"}, false},
		{"object agent", map[string]any{"agent": map[string]any{"id": "awf"}}, false},
		{"typo sandbox", "PRIVATE_VALUE", true},
		{"boolean sandbox", false, true},
		{"list sandbox", []any{"awf"}, true},
		{"typo agent", map[string]any{"agent": "PRIVATE_VALUE"}, true},
		{"default agent", map[string]any{"agent": "default"}, true},
		{"true agent", map[string]any{"agent": true}, true},
		{"numeric agent", map[string]any{"agent": 123}, true},
		{"list agent", map[string]any{"agent": []any{"awf"}}, true},
		{"invalid agent environment", map[string]any{"agent": map[string]any{"env": 123}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := &aw.Document{Frontmatter: map[string]any{
				"engine": map[string]any{
					"env":    map[string]any{"TOKEN": "${{ secrets.ENGINE }}"},
					"driver": map[string]any{"node": "console.log(process.env.TOKEN);"},
				},
				"sandbox": test.sandbox,
			}}
			if test.sandbox == nil {
				delete(document.Frontmatter, "sandbox")
			}
			bindings, err := analyzer.authoredScriptEnvironment(t.Context(), document, authoredScript{path: "engine.driver.node"}, scope{}, nil)
			if test.invalid {
				require.ErrorContains(t, err, "sandbox")
				assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
				return
			}
			require.NoError(t, err)
			require.Len(t, bindings["shell.TOKEN"], 1)
			assert.Equal(t, "secrets.engine", bindings["shell.TOKEN"][0].Reference)
		})
	}
}

func TestAuthoredHookWriteEnvironment(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	rule := SecretLeakRule()
	rule.JavaScript = options
	for _, test := range []struct {
		name     string
		workflow string
		owner    string
		findings int
	}{
		{"write-inherits-workflow", "${{ secrets.WORKFLOW }}", "public", 5},
		{"write-does-not-inherit-runtime-overrides", "public", "${{ secrets.OWNER }}", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `console.log("${{ env.TOKEN }}");`
			env := map[string]any{"TOKEN": test.owner}
			document := &aw.Document{Frontmatter: map[string]any{
				"env": map[string]any{"TOKEN": test.workflow},
				"engine": map[string]any{
					"env":       env,
					"driver":    map[string]any{"node": source},
					"behaviors": map[string]any{"harness-script": source},
				},
				"safe-outputs": map[string]any{
					"env":     env,
					"scripts": map[string]any{"custom": map[string]any{"script": source}},
				},
				"mcp-scripts": map[string]any{
					"javascript": map[string]any{"env": env, "script": source},
					"shell":      map[string]any{"run": `echo '${{ env.TOKEN }}'`},
				},
			}}
			result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
			require.NoError(t, err)
			require.Len(t, result.Findings, test.findings)
			for _, finding := range result.Findings {
				require.Len(t, finding.Sources, 1)
				assert.Equal(t, "secrets.workflow", finding.Sources[0].Reference)
			}
		})
	}
}

func TestAuthoredEngineBehaviorSchemaAndScopes(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	document, err := aw.Parse(`---
env:
  TOKEN: public
engine:
  env:
    TOKEN: '${{ secrets.ENGINE }}'
  behavior:
    harness-script: console.log("${{ secrets.UNSUPPORTED }}");
  behaviors:
    log-parser: 'function parseLog(content) { return { markdown: process.env.TOKEN }; }'
    mcp:
      config-adapter: console.log(process.env.TOKEN);
---
`)
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.JavaScript = options
	result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
	require.NoError(t, err)
	assert.Empty(t, result.Findings, "log parsing and gateway steps do not inherit engine execution env")
	engine := document.Frontmatter["engine"].(map[string]any)
	engine["behaviors"] = map[string]any{"mcp": "invalid"}
	_, err = authoredScripts(document)
	require.ErrorContains(t, err, "engine.behaviors.mcp")
}

func TestAuthoredMCPSharedEnvironment(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	rule := SecretLeakRule()
	rule.JavaScript = options
	for _, test := range []struct {
		name      string
		workflow  map[string]any
		first     map[string]any
		last      map[string]any
		reference string
	}{
		{
			name:      "all-tools-inherit",
			first:     map[string]any{"TOKEN": "${{ secrets.FIRST }}"},
			reference: "secrets.first",
		},
		{
			name:  "last-public-overrides-every-tool",
			first: map[string]any{"TOKEN": "${{ secrets.FIRST }}"},
			last:  map[string]any{"TOKEN": "public"},
		},
		{
			name:      "last-secret-overrides-every-tool",
			first:     map[string]any{"TOKEN": "public"},
			last:      map[string]any{"TOKEN": "${{ secrets.LAST }}"},
			reference: "secrets.last",
		},
		{
			name:     "tool-overrides-workflow",
			workflow: map[string]any{"TOKEN": "${{ secrets.WORKFLOW }}"},
			first:    map[string]any{"TOKEN": "public"},
		},
		{
			name:      "expressions-see-workflow-not-other-tools",
			workflow:  map[string]any{"ROOT": "${{ secrets.WORKFLOW }}"},
			first:     map[string]any{"ROOT": "public"},
			last:      map[string]any{"TOKEN": "${{ env.ROOT }}"},
			reference: "secrets.workflow",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := map[string]any{"script": "return process.env.TOKEN;"}
			last := map[string]any{"run": `echo "$TOKEN"`}
			if test.first != nil {
				first["env"] = test.first
			}
			if test.last != nil {
				last["env"] = test.last
			}
			document := &aw.Document{Frontmatter: map[string]any{
				"env": test.workflow,
				"mcp-scripts": map[string]any{
					"alpha": first,
					"beta":  map[string]any{"script": "return process.env.TOKEN;"},
					"zeta":  last,
				},
			}}
			result, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
			require.NoError(t, err)
			if test.reference == "" {
				assert.Empty(t, result.Findings)
				return
			}
			require.Len(t, result.Findings, 3)
			for _, finding := range result.Findings {
				require.Len(t, finding.Sources, 1)
				assert.Equal(t, test.reference, finding.Sources[0].Reference)
			}
			repeated, err := AnalyzeAuthoredScripts(t.Context(), document, rule)
			require.NoError(t, err)
			assert.Equal(t, result, repeated)
		})
	}
}
