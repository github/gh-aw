//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
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
			require.NoError(t, validateRuntimeImportExpressionSafety(content), "local files share author capabilities")
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

func TestCompilerRuntimeImportBindsNestedEnv(t *testing.T) {
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
			require.NoError(t, compiler.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(workflowsDir, "test.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(content), "authorExpressions:")
			assert.Contains(t, string(content), expression)
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
			if mode == "runtime" {
				var compiled struct {
					Jobs map[string]struct {
						Steps []struct {
							Name string
							Env  map[string]string
						}
					}
				}
				require.NoError(t, yaml.Unmarshal(content, &compiled))
				for _, job := range compiled.Jobs {
					for _, step := range job.Steps {
						if step.Name != "Create prompt with built-in context" {
							continue
						}
						var config struct {
							Items []struct {
								AuthorFile string `json:"author_file"`
							}
							Expressions map[string]string `json:"author_expressions"`
						}
						require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_PROMPT_CONFIG"]), &config))
						require.Contains(t, config.Items, struct {
							AuthorFile string `json:"author_file"`
						}{".github/workflows/test.md"})
						for _, mapping := range mappings {
							assert.Equal(t, mapping.EnvVar, config.Expressions[stripExpressionWrapper(mapping.Original)])
						}
						node, err := exec.LookPath("node")
						require.NoError(t, err)
						runtimeDir, err := filepath.Abs("../../actions/setup/js")
						require.NoError(t, err)
						command := exec.Command(node, "-e", `
const { renderPrompt } = require(process.argv[1] + "/create_prompt.cjs");
const config = JSON.parse(process.argv[2]);
const env = { GITHUB_WORKSPACE: process.argv[3] };
for (const binding of Object.values(config.author_expressions)) env[binding] = "author-value";
config.items = config.items.filter(item => item.author_file);
process.stdout.write(renderPrompt(config, env, process.argv[3]));
`, runtimeDir, step.Env["GH_AW_PROMPT_CONFIG"], filepath.Dir(filepath.Dir(dir)))
						output, err := command.CombinedOutput()
						require.NoError(t, err, "%s", output)
						assert.Equal(t, "Value: author-value\nDefault: author-value\n", string(output))
					}
				}
			}
		})
	}
}

func TestRuntimeImportAnalysisBindsAuthorEnv(t *testing.T) {
	root := t.TempDir()
	workflowsDir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "fragment.md"), []byte("Value: ${{ env.MY_VAR || 'fallback' }}"), 0o600))
	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "test.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import fragment.md}}"}
	assert.Contains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), "env.MY_VAR")
	assert.NotEmpty(t, compiler.enrichExpressionMappings(data, nil, nil))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "fragment.md"), []byte("Visible\n<!-- ${{ env.MY_VAR }} -->"), 0o600))
	assert.Contains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), "Visible")
	assert.NotContains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), "env.MY_VAR")
	assert.Empty(t, compiler.enrichExpressionMappings(data, nil, nil))
}
