package lint

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func policyTestInput(t *testing.T, frontmatter string) Input {
	t.Helper()
	document, err := aw.Parse("---\n" + frontmatter + "\n---\nA prompt.\n")
	require.NoError(t, err)
	return Input{Workflow: &actions.Workflow{}, Document: document}
}

func TestAuthoredImportsPending(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		pending    bool
		invalid    bool
	}{
		{"absent", "", false, false},
		{"empty list", "imports: []", false, false},
		{"list", "imports: [shared.md]", true, false},
		{"empty object", "imports: {}", false, false},
		{"empty aw", "imports: {aw: []}", false, false},
		{"aw paths", "imports: {aw: [shared.md]}", true, false},
		{"aw mappings", "imports: {aw: [{path: shared.md, inputs: {key: value}}]}", true, false},
		{"invalid imports", "imports: PRIVATE_VALUE", false, true},
		{"invalid aw string", "imports: {aw: PRIVATE_VALUE}", false, true},
		{"invalid aw mapping", "imports: {aw: {path: PRIVATE_VALUE}}", false, true},
		{"invalid aw boolean", "imports: {aw: true}", false, true},
		{"invalid aw null", "imports: {aw: null}", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pending, err := authoredImportsPending(policyTestInput(t, tt.text))
			if tt.invalid {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.pending, pending)
		})
	}
	input := policyTestInput(t, "")
	for _, imports := range []any{[]string{"shared.md"}, map[string]any{"aw": []string{"shared.md"}}} {
		input.Document.Frontmatter["imports"] = imports
		pending, err := authoredImportsPending(input)
		require.NoError(t, err)
		assert.True(t, pending)
	}
	pending, err := authoredImportsPending(Input{})
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestSourcePolicySeverityAndBoundaries(t *testing.T) {
	for _, tt := range []struct {
		rule Rule
		text string
		path string
	}{
		{VersionCheckRule(), "check-for-updates: false", "check-for-updates"},
		{PrivateToPublicRule(), "tools:\n  github:\n    private-to-public-flows: allow", "tools.github.private-to-public-flows"},
		{PrivateToPublicRule(), "tools:\n  github:\n    private-to-public-flows: [one, two]", "tools.github.private-to-public-flows"},
		{PrivateToPublicRule(), "tools:\n  github:\n    private-to-public-flows: []", "tools.github.private-to-public-flows"},
		{InstallScriptsRule(), "runtimes:\n  node:\n    run-install-scripts: true", "runtimes.node.run-install-scripts"},
		{NetworkScopeRule(), "network:\n  allowed: ['*']", "network.allowed"},
		{NetworkScopeRule(), "network:\n  allowed: [defaults, '*']", "network.allowed"},
	} {
		t.Run(tt.rule.ID+"/"+tt.text, func(t *testing.T) {
			for _, strict := range []string{"", "strict: true\n", "strict: false\n"} {
				input := policyTestInput(t, strict+tt.text)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				runner, err := New(tt.rule)
				require.NoError(t, err)
				diagnostics, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tt.path, diagnostics[0].Path)
				severity := Error
				if strict == "strict: false\n" {
					severity = Warning
				}
				assert.Equal(t, severity, diagnostics[0].Severity)
				after, err := json.Marshal(input)
				require.NoError(t, err)
				assert.Equal(t, before, after)
				input.Document = nil
				diagnostics, err = runner.Run(context.Background(), input)
				require.NoError(t, err)
				assert.Empty(t, diagnostics, "do not invent AW source policy for native Actions")
			}
		})
	}
}

func TestSourcePolicySafeInputs(t *testing.T) {
	for _, text := range []string{
		"", "check-for-updates: true", "tools:\n  github:", "tools:\n  github: false",
		"tools:\n  github: true", "tools:\n  github:\n    mode: local",
		"tools:\n  github:\n    private-to-public-flows: null",
	} {
		for _, rule := range []Rule{VersionCheckRule(), PrivateToPublicRule(), ConditionalImportRule()} {
			diagnostics, err := rule.Check(context.Background(), policyTestInput(t, text))
			require.NoError(t, err)
			assert.Empty(t, diagnostics)
		}
	}
}

func TestConditionalImportRule(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want int
	}{
		{"strings", "imports: [shared/a.md, shared/b.md]", 0},
		{"object strings", "imports: {aw: [shared/a.md, shared/b.md]}", 0},
		{"empty object", "imports: {}", 0},
		{"empty aw", "imports: {aw: []}", 0},
		{"object inputs and uses aliases", "imports: {aw: [{uses: shared/a.md, with: {if: ordinary-input}}, {path: shared/b.md, inputs: {}}]}", 0},
		{"object true condition", "imports: {aw: [{path: shared/a.md, if: true}]}", 1},
		{"object false condition", "imports: {aw: [{uses: shared/a.md, if: false}]}", 1},
		{"object null condition", "imports: {aw: [{path: shared/a.md, if: null}]}", 1},
		{"object all entries inspected", "imports: {aw: [{path: shared/a.md, if: first-private-value}, {uses: shared/b.md, if: second-private-value}]}", 2},
		{"inputs and uses aliases", "imports:\n  - uses: shared/a.md\n    with:\n      if: ordinary-input\n  - path: shared/b.md\n    inputs: {}", 0},
		{"true condition", "imports:\n  - path: shared/a.md\n    if: true", 1},
		{"false condition still forbidden", "imports:\n  - uses: shared/a.md\n    if: false", 1},
		{"null condition still forbidden", "imports:\n  - path: shared/a.md\n    if: null", 1},
		{"all entries inspected", "imports:\n  - path: shared/a.md\n    if: first-private-value\n  - uses: shared/b.md\n    if: second-private-value", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := policyTestInput(t, tt.text)
			input.Document.Markdown = "{{#if condition}}{{#runtime-import? shared/a.md}}{{/if}}"
			runner, err := New(ConditionalImportRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			require.Len(t, diagnostics, tt.want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Error, diagnostic.Severity)
				if _, object := input.Document.Frontmatter["imports"].(map[string]any); object {
					assert.Contains(t, diagnostic.Path, "imports.aw[")
				} else {
					assert.Contains(t, diagnostic.Path, "imports[")
				}
				assert.NotContains(t, diagnostic.Message, "private-value")
			}
		})
	}
}

func TestSourcePolicyMalformedInputAndCancellation(t *testing.T) {
	for _, rule := range []Rule{PermissionScopeRule(), VersionCheckRule(), PrivateToPublicRule(), ConditionalImportRule()} {
		for _, input := range []Input{{}, {Workflow: &actions.Workflow{Jobs: []*actions.Job{nil}}}} {
			_, err := rule.Check(context.Background(), input)
			require.Error(t, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := rule.Check(ctx, Input{Workflow: &actions.Workflow{}})
		require.ErrorIs(t, err, context.Canceled)
		if rule.ID != "conditional-import" {
			_, err = rule.Check(context.Background(), policyTestInput(t, "strict: private-value"))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private-value")
		}
	}
	for _, tt := range []struct {
		rule Rule
		text string
	}{
		{VersionCheckRule(), "check-for-updates: private-value"},
		{PrivateToPublicRule(), "tools: private-value"},
		{PrivateToPublicRule(), "tools:\n  github: private-value"},
		{PrivateToPublicRule(), "tools:\n  github:\n    private-to-public-flows: private-value"},
		{PrivateToPublicRule(), "tools:\n  github:\n    private-to-public-flows: [42]"},
		{ConditionalImportRule(), "imports: private-value"},
		{ConditionalImportRule(), "imports: {aw: private-value}"},
		{ConditionalImportRule(), "imports: {aw: true}"},
		{ConditionalImportRule(), "imports: {aw: null}"},
		{ConditionalImportRule(), "imports: {aw: {path: private-value}}"},
		{ConditionalImportRule(), "imports: {aw: [42]}"},
		{ConditionalImportRule(), "imports: {aw: [{}]}"},
		{ConditionalImportRule(), "imports: [42]"},
		{ConditionalImportRule(), "imports: [{}]"},
		{ConditionalImportRule(), "imports:\n  - path: 42"},
		{ConditionalImportRule(), "imports:\n  - path: shared/a.md\n    with: private-value"},
	} {
		_, err := tt.rule.Check(context.Background(), policyTestInput(t, tt.text))
		require.Error(t, err, "%s: %s", tt.rule.ID, tt.text)
		assert.NotContains(t, err.Error(), "private-value")
	}
}

func TestSourcePolicyPartialDiagnosticsAndMidTraversalCancellation(t *testing.T) {
	input := policyTestInput(t, "imports:\n  - path: shared/a.md\n    if: true\n  - 42\n  - uses: shared/b.md\n    if: false")
	diagnostics, err := ConditionalImportRule().Check(context.Background(), input)
	require.Error(t, err)
	require.Len(t, diagnostics, 2)
	input.Document.Frontmatter["imports"] = map[string]any{"aw": input.Document.Frontmatter["imports"]}
	diagnostics, err = ConditionalImportRule().Check(context.Background(), input)
	require.Error(t, err)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "imports.aw[0].if", diagnostics[0].Path)
	assert.Equal(t, "imports.aw[2].if", diagnostics[1].Path)
	input.Document.Frontmatter["imports"] = []any{"first", "second", "third", "fourth"}
	ctx := &integrityCancelContext{Context: context.Background(), remaining: 4}
	_, err = ConditionalImportRule().Check(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
	input.Document.Frontmatter = map[string]any{"tools": map[string]any{"github": map[string]any{
		"private-to-public-flows": []any{"one", "two", "three", "four"},
	}}}
	ctx = &integrityCancelContext{Context: context.Background(), remaining: 4}
	_, err = PrivateToPublicRule().Check(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
}
