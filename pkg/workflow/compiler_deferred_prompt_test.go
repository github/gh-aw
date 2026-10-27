package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionMappingEnvironmentValue(t *testing.T) {
	for _, content := range []string{
		"needs.release.outputs.id",
		"'authored literal'",
		"'first' || 'second'",
		"needs.release.outputs.id || 'fallback'",
	} {
		mapping := &ExpressionMapping{Content: content}
		assert.Equal(t, "${{ "+content+" }}", mapping.environmentValue(), "authored expressions remain expressions")
	}
	for _, content := range []string{"__GH_AW_DEFERRED_1234__", "", "true", "quote: \"value\"\nnext\\line"} {
		mapping := &ExpressionMapping{EnvVar: "VALUE", Content: content, IsLiteral: true}
		var environment map[string]any
		require.NoError(t, yaml.Unmarshal([]byte("VALUE: "+mapping.environmentValue()), &environment))
		assert.Equal(t, content, environment["VALUE"])
		var substitution strings.Builder
		generatePlaceholderEnvironment(&substitution, []*ExpressionMapping{mapping}, "")
		require.NoError(t, yaml.Unmarshal([]byte(substitution.String()), &environment))
		assert.Equal(t, content, environment["VALUE"])
	}
}

func TestRewriteDeferredPromptExpressions(t *testing.T) {
	jobs := map[string]any{
		"release": map[string]any{"needs": "activation", "outputs": map[string]any{"release_id": "public"}},
		"report":  map[string]any{"needs": "agent", "outputs": map[string]any{"value": "public"}},
		"middle":  map[string]any{"needs": []any{"report"}, "outputs": map[string]any{"value": "public"}},
		"final":   map[string]any{"needs": []string{"release", "middle"}, "outputs": map[string]any{"value": "public"}},
	}
	for _, tt := range []struct {
		expression string
		before     []string
		deferred   bool
	}{
		{"needs.release.outputs.release_id", nil, true},
		{"needs.release.result", nil, true},
		{"needs.release.outputs.release_id", []string{"release"}, false},
		{"needs.release.outputs.unknown", nil, false},
		{"needs.unknown.outputs.value", nil, false},
		{"needs.report.outputs.value", nil, false},
		{"needs.middle.outputs.value", nil, false},
		{"needs.middle.result", nil, false},
		{"needs.final.outputs.value", nil, false},
		{"needs.final.result", nil, false},
		{"needs.agent.outputs.value", nil, false},
		{"needs.release.outputs.release_id || 'fallback'", nil, false},
		{"github.repository", nil, false},
	} {
		t.Run(tt.expression+strings.Join(tt.before, ","), func(t *testing.T) {
			original := &ExpressionMapping{Original: "${{ " + tt.expression + " }}", Content: tt.expression, EnvVar: "GH_AW_VALUE"}
			got, deferred := rewriteDeferredPromptExpressions([]*ExpressionMapping{original, original}, jobs, tt.before, original.Original)
			require.Len(t, got, 2)
			assert.Equal(t, tt.expression, original.Content, "do not mutate the original mappings")
			if tt.deferred {
				require.Len(t, deferred, 1)
				assert.Equal(t, tt.expression, deferred[0].Content)
				assert.Equal(t, "__"+deferred[0].EnvVar+"__", got[0].Content)
				assert.True(t, got[0].IsLiteral)
				assert.False(t, deferred[0].IsLiteral)
				assert.False(t, original.IsLiteral)
				assert.Equal(t, original.EnvVar, got[0].EnvVar)
			} else {
				assert.Empty(t, deferred)
				assert.Equal(t, original, got[0])
			}
		})
	}
}

func TestDeferredPromptDoesNotChangeRendering(t *testing.T) {
	jobs := map[string]any{"release": map[string]any{
		"needs": "activation", "outputs": map[string]any{"release_id": "public"},
	}}
	for _, markdown := range []string{
		"${{ needs.release.outputs.release_id || 'fallback' }}",
		"{{#if ${{ needs.release.outputs.release_id }} }}yes{{/if}}",
		"{{#if needs.release.outputs.release_id}}yes{{/if}}",
	} {
		extractor := NewExpressionExtractor()
		mappings, err := extractor.ExtractExpressions(wrapExpressionsInTemplateConditionals(markdown))
		require.NoError(t, err)
		_, deferred := rewriteDeferredPromptExpressions(mappings, jobs, nil, markdown)
		assert.Empty(t, deferred, markdown)
	}
}

func TestDeferredPromptReferenceBoundaries(t *testing.T) {
	jobs := map[string]any{"release": map[string]any{
		"needs": "activation",
		"outputs": map[string]any{
			"release_id": "public", "release_id_extra": "public", "id": "public", "id_set": "public",
		},
	}}
	for _, tt := range []struct {
		name     string
		markdown string
		want     []string
	}{
		{"sibling outputs", "${{ needs.release.outputs.release_id }} ${{ needs.release.outputs.release_id_extra }}", []string{"needs.release.outputs.release_id", "needs.release.outputs.release_id_extra"}},
		{"sibling fallback", "${{ needs.release.outputs.release_id }} ${{ needs.release.outputs.release_id_extra || 'none' }}", []string{"needs.release.outputs.release_id"}},
		{"sibling condition", "${{ needs.release.outputs.id }} {{#if needs.release.outputs.id_set}}yes{{/if}}", []string{"needs.release.outputs.id"}},
		{"sibling expression condition", "${{ needs.release.outputs.id }} {{#if ${{ needs.release.outputs.id_set }} }}yes{{/if}}", []string{"needs.release.outputs.id"}},
		{"sibling elseif", "${{ needs.release.outputs.id }} {{#if false}}no{{#elseif needs.release.outputs.id_set}}yes{{/if}}", []string{"needs.release.outputs.id"}},
		{"actual fallback operand", "${{ needs.release.outputs.id }} ${{ needs.release.outputs.id || 'none' }}", nil},
		{"actual condition", "${{ needs.release.outputs.id }} {{#if needs.release.outputs.id}}yes{{/if}}", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mappings, err := NewExpressionExtractor().ExtractExpressions(wrapExpressionsInTemplateConditionals(tt.markdown))
			require.NoError(t, err)
			_, deferred := rewriteDeferredPromptExpressions(mappings, jobs, nil, tt.markdown)
			var contents []string
			for _, mapping := range deferred {
				contents = append(contents, mapping.Content)
			}
			assert.ElementsMatch(t, tt.want, contents)
		})
	}
}

func TestDeferredPromptCompilationPreservesCompoundMappings(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			frontmatter := ""
			if mode == "inlined-imports" {
				frontmatter = "inlined-imports: true\n"
			}
			source := "---\non: workflow_dispatch\nengine: copilot\n" + frontmatter + `jobs:
  release:
    needs: activation
    runs-on: ubuntu-latest
    outputs:
      id: ${{ steps.release.outputs.id }}
    steps:
      - id: release
        run: echo "id=123" >> "$GITHUB_OUTPUT"
---
Release ID: ${{ needs.release.outputs.id || 'none' }}
{{#if needs.release.outputs.id}}
Release exists
{{/if}}
`
			path := filepath.Join(dir, "release.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = mode == "inline"
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "release.lock.yml"))
			require.NoError(t, err)
			activation := strings.SplitN(string(content), "\n  agent:", 2)[0]
			mappings, err := NewExpressionExtractor().ExtractExpressions(source[strings.LastIndex(source, "\n---\n")+5:])
			require.NoError(t, err)
			for _, mapping := range mappings {
				assert.Contains(t, activation, mapping.EnvVar+": ${{ "+mapping.Content+" }}")
			}
			assert.NotContains(t, activation, "GH_AW_DEFERRED_")
		})
	}
}

func TestDeferredPromptImportedCompilation(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime", true: "inline"}[inline], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Release: ${{ needs.release.outputs.id }}"), 0o600))
			source := `---
on: workflow_dispatch
engine: copilot
jobs:
  release:
    needs: activation
    runs-on: ubuntu-latest
    outputs:
      id: ${{ steps.release.outputs.id }}
    steps:
      - id: release
        run: echo "id=123" >> "$GITHUB_OUTPUT"
---
{{#runtime-import prompt.md}}
`
			path := filepath.Join(dir, "release.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = inline
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "release.lock.yml"))
			require.NoError(t, err)
			parts := strings.SplitN(string(content), "\n  agent:", 2)
			require.Len(t, parts, 2)
			assert.NotContains(t, parts[0], "${{ needs.release.outputs.id }}")
			assert.Contains(t, parts[0], "GH_AW_DEFERRED_PLACEHOLDERS:")
			assert.Contains(t, parts[1], "${{ needs.release.outputs.id }}")
		})
	}
}

func TestDeferredPromptCompilation(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			source := `---
on: workflow_dispatch
engine: copilot
jobs:
  release:
    needs: activation
    runs-on: ubuntu-latest
    outputs:
      release_id: ${{ steps.release.outputs.id }}
    steps:
      - id: release
        run: echo "id=123" >> "$GITHUB_OUTPUT"
---
Release ID: ${{ needs.release.outputs.release_id }}
Release ID again: ${{ needs.release.outputs.release_id }}
Release result: ${{ needs.release.result }}
`
			if mode == "inlined-imports" {
				source = strings.Replace(source, "engine: copilot\n", "engine: copilot\ninlined-imports: true\n", 1)
			}
			path := filepath.Join(dir, "release.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = mode == "inline"
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			require.NoError(t, compiler.CompileWorkflowData(data, path))
			content, err := os.ReadFile(filepath.Join(dir, "release.lock.yml"))
			require.NoError(t, err)
			lock := string(content)
			agent := strings.Index(lock, "\n  agent:")
			require.Positive(t, agent)
			activation := lock[:agent]
			assert.NotContains(t, activation, "${{ needs.release.outputs.release_id }}")
			assert.Contains(t, activation, "GH_AW_DEFERRED_PLACEHOLDERS:")
			assert.Contains(t, lock[agent:], "Resolve deferred prompt expressions")
			assert.Contains(t, lock[agent:], "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt")
			assert.Contains(t, lock[agent:], "${{ needs.release.outputs.release_id }}")
			assert.Less(t, strings.Index(lock[agent:], "Download activation artifact"), strings.Index(lock[agent:], "Resolve deferred prompt expressions"))

			require.Len(t, data.DeferredPromptExpressions, 2)
			var document struct {
				Jobs map[string]struct {
					Steps []struct {
						Name string         `yaml:"name"`
						Env  map[string]any `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &document))
			for _, name := range []string{
				"Create prompt with built-in context",
				"Interpolate variables and render templates",
				"Substitute placeholders",
			} {
				found := false
				for _, step := range document.Jobs["activation"].Steps {
					if step.Name != name {
						continue
					}
					found = true
					for _, mapping := range data.DeferredPromptExpressions {
						original := NewExpressionExtractor().generateEnvVarName(mapping.Content)
						assert.Equal(t, "__"+mapping.EnvVar+"__", step.Env[original], name)
					}
				}
				assert.True(t, found, name)
			}
			foundResolver := false
			for _, step := range document.Jobs["agent"].Steps {
				if step.Name == "Resolve deferred prompt expressions" {
					foundResolver = true
					for _, mapping := range data.DeferredPromptExpressions {
						assert.Equal(t, "${{ "+mapping.Content+" }}", step.Env[mapping.EnvVar])
					}
				}
			}
			assert.True(t, foundResolver)
			compiled, err := actions.Parse(content)
			require.NoError(t, err)
			diagnostics, err := workflowlint.ObfuscationRule().Check(context.Background(), workflowlint.Input{Workflow: compiled})
			require.NoError(t, err)
			assert.Empty(t, diagnostics, "generated constants must not require linter exemptions")
		})
	}
}

func TestDeferredPromptCompilationExcludesPostAgentJobs(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime", true: "inline"}[inline], func(t *testing.T) {
			dir := t.TempDir()
			source := `---
on: workflow_dispatch
engine: copilot
jobs:
  middle:
    needs: agent
    runs-on: ubuntu-latest
    steps:
      - run: echo middle
  release:
    needs: [middle]
    runs-on: ubuntu-latest
    outputs:
      release_id: ${{ steps.release.outputs.id }}
    steps:
      - id: release
        run: echo "id=123" >> "$GITHUB_OUTPUT"
---
Release ID: ${{ needs.release.outputs.release_id }}
Release result: ${{ needs.release.result }}
`
			path := filepath.Join(dir, "release.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = inline
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "release.lock.yml"))
			require.NoError(t, err)
			var lock struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &lock))
			for _, jobName := range []string{"activation", "agent"} {
				job, ok := lock.Jobs[jobName]
				require.True(t, ok, jobName)
				needs := parseNeedsField(job["needs"])
				assert.NotContains(t, needs, "middle")
				assert.NotContains(t, needs, "release")
				rendered, err := yaml.Marshal(job)
				require.NoError(t, err)
				assert.NotContains(t, string(rendered), "${{ needs.release.")
				assert.NotContains(t, string(rendered), "GH_AW_DEFERRED_")
				assert.NotContains(t, string(rendered), "Resolve deferred prompt expressions")
			}
			assert.Equal(t, []string{"agent"}, parseNeedsField(lock.Jobs["middle"]["needs"]))
			assert.Equal(t, []string{"middle"}, parseNeedsField(lock.Jobs["release"]["needs"]))
		})
	}
}
