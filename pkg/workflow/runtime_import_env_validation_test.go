//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeImportExpressionSafetyScope(t *testing.T) {
	require.NoError(t, validateRuntimeImportExpressionSafety("Visible prompt\n<!-- Configuration example: ${{ env.MY_VAR }} -->"),
		"runtime strips comments before evaluating imported expressions")
	for _, expression := range []string{
		"env.MY_VAR",
		"env['MY_VAR']",
		"env.MY_VAR || 'default'",
		"github.actor == 'bot' && env.MY_VAR",
		"(env.MY_VAR != '' && github.repository) || 'fallback'",
		"!env.MY_VAR",
	} {
		t.Run(expression, func(t *testing.T) {
			content := "${{ " + expression + " }}"
			require.NoError(t, validateExpressionSafety(content), "main prompt compatibility")
			err := validateRuntimeImportExpressionSafety(content)
			require.ErrorContains(t, err, "env")
			assert.NotContains(t, err.Error(), "  - env.*\n", "import diagnostics must not recommend env")
		})
	}

	for _, expression := range []string{
		"github.actor",
		"github.event.issue.number",
		"needs.pre_activation.outputs.version || 'unknown'",
		"steps.sanitized.outputs.text",
		"inputs.message",
		"github.event.inputs.message",
		"github.aw.inputs.message",
		"github.aw.import-inputs.message",
		"github.actor == 'bot' && github.repository",
		"'env.MY_VAR'",
	} {
		t.Run(expression, func(t *testing.T) {
			require.NoError(t, validateRuntimeImportExpressionSafety("${{ "+expression+" }}"))
		})
	}

	for _, expression := range []string{"secrets.TOKEN", "vars.PRIVATE", "github.actor || secrets.TOKEN", "github.actor && vars.PRIVATE"} {
		t.Run(expression, func(t *testing.T) {
			require.Error(t, validateRuntimeImportExpressionSafety("${{ "+expression+" }}"))
		})
	}
}

func TestCompilerRuntimeImportRejectsNestedEnv(t *testing.T) {
	for _, expression := range []string{"env.MY_VAR", "env.MY_VAR || 'default'", "github.actor == 'bot' && env.MY_VAR"} {
		t.Run(expression, func(t *testing.T) {
			root := t.TempDir()
			workflowsDir := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "outer.md"), []byte("{{#runtime-import inner.md}}"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "inner.md"), []byte("${{ "+expression+" }}"), 0o600))
			path := filepath.Join(workflowsDir, "test.md")
			source := "---\non: workflow_dispatch\nengine: copilot\nenv:\n  MY_VAR: author-value\n---\nMain: ${{ env.MY_VAR }}\n{{#runtime-import outer.md}}\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			err := compiler.CompileWorkflow(path)
			require.ErrorContains(t, err, "runtime-import files contain expression errors")
			require.ErrorContains(t, err, "inner.md")
			require.ErrorContains(t, err, "env.MY_VAR")
		})
	}
}

func TestCompilerMainPromptPreservesEnvBindings(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			frontmatter := ""
			if mode == "inlined-imports" {
				frontmatter = "inlined-imports: true\n"
			}
			markdown := "Value: ${{ env.MY_VAR }}\nDefault: ${{ env.MY_VAR || 'fallback' }}\n"
			source := "---\non: workflow_dispatch\nengine: copilot\nenv:\n  MY_VAR: author-value\n" + frontmatter + "---\n" + markdown
			path := filepath.Join(dir, "test.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = mode == "inline"
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
			require.NoError(t, err)
			mappings, err := NewExpressionExtractor().ExtractExpressions(markdown)
			require.NoError(t, err)
			require.NotEmpty(t, mappings)
			for _, mapping := range mappings {
				assert.Contains(t, string(content), mapping.EnvVar+": ${{ "+mapping.Content+" }}")
			}
		})
	}
}

func TestRuntimeImportAnalysisDoesNotBindEnv(t *testing.T) {
	root := t.TempDir()
	workflowsDir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "fragment.md"), []byte("Value: ${{ env.MY_VAR || 'fallback' }}"), 0o600))
	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "test.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import fragment.md}}"}
	assert.Empty(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data))
	assert.Empty(t, compiler.enrichExpressionMappings(data, nil, nil))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "fragment.md"), []byte("Visible\n<!-- ${{ env.MY_VAR }} -->"), 0o600))
	assert.Contains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), "Visible")
	assert.NotContains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), "env.MY_VAR")
	assert.Empty(t, compiler.enrichExpressionMappings(data, nil, nil))
}
