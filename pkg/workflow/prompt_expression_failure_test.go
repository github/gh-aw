package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflowRejectsMalformedPromptConditions(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		for _, condition := range []string{
			"experiments.reasoning_depth iterative",
			`(eq experiments.prompt_compression "caveman")`,
		} {
			t.Run(mode+"/"+condition, func(t *testing.T) {
				dir := t.TempDir()
				source := filepath.Join(dir, "invalid.md")
				markdown := "---\non: push\nengine: copilot\nstrict: false\n---\nRepository: ${{ github.repository }}\n{{#if " + condition + "}}\nSecond pass\n{{/if}}\n"
				if mode == "inlined-imports" {
					markdown = strings.Replace(markdown, "engine: copilot\n", "engine: copilot\ninlined-imports: true\n", 1)
				}
				require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
				compiler := NewCompiler()
				compiler.inlinePrompt = mode == "inline"
				require.ErrorContains(t, compiler.CompileWorkflow(source), "prompt expressions")
				_, err := os.Stat(filepath.Join(dir, "invalid.lock.yml"))
				require.True(t, os.IsNotExist(err), "failed compilation must not publish a lock file")
			})
		}
	}
}

func TestPromptExpressionExtractionFailure(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		for _, imported := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/main", true: "/import"}[imported], func(t *testing.T) {
				dir := t.TempDir()
				workflows := filepath.Join(dir, ".github", "workflows")
				require.NoError(t, os.MkdirAll(workflows, 0o700))
				markdown := "Repository: ${{ github.repository }}\n{{#if experiments.reasoning_depth iterative}}\nSecond pass\n{{/if}}\nRun: ${{ github.run_id }}\n"
				if imported {
					require.NoError(t, os.WriteFile(filepath.Join(workflows, "fragment.md"), []byte(markdown), 0o600))
					markdown = "{{#runtime-import fragment.md}}"
				}
				data := &WorkflowData{
					MainWorkflowMarkdown: markdown, MarkdownContent: markdown,
					InlinedImports: mode == "inlined-imports",
				}
				compiler := NewCompiler()
				compiler.inlinePrompt = mode == "inline"
				compiler.markdownPath = filepath.Join(workflows, "main.md")
				var output strings.Builder
				err := compiler.generatePrompt(&output, data, false, nil)
				require.ErrorContains(t, err, "prompt expressions")
				require.ErrorContains(t, err, "iterative")
				assert.Empty(t, output.String(), "do not emit prompt steps without expression bindings")
			})
		}
	}
}

func TestImportedPromptExpressionExtractionFailure(t *testing.T) {
	for _, ordered := range []bool{false, true} {
		for _, fromFile := range []bool{false, true} {
			name := map[bool]string{false: "legacy", true: "ordered"}[ordered] + map[bool]string{false: "/inline", true: "/file"}[fromFile]
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				workflows := filepath.Join(dir, ".github", "workflows")
				require.NoError(t, os.MkdirAll(workflows, 0o700))
				const markdown = "Repository: ${{ github.repository }}\n{{#if experiments.reasoning_depth iterative}}\nSecond pass\n{{/if}}\n"
				data := &WorkflowData{MainWorkflowMarkdown: "Safe main prompt"}
				if fromFile {
					const importPath = ".github/workflows/fragment.md"
					require.NoError(t, os.WriteFile(filepath.Join(dir, importPath), []byte(markdown), 0o600))
					data.InlinedImports = true
					if ordered {
						data.PromptImports = []parser.PromptImportEntry{{ImportPath: importPath}}
					} else {
						data.ImportPaths = []string{importPath}
					}
				} else if ordered {
					data.PromptImports = []parser.PromptImportEntry{{Markdown: markdown}}
				} else {
					data.ImportedMarkdown = markdown
				}
				compiler := NewCompiler()
				compiler.markdownPath = filepath.Join(workflows, "main.md")
				var output strings.Builder
				require.ErrorContains(t, compiler.generatePrompt(&output, data, false, nil), "prompt expressions")
				assert.Empty(t, output.String(), "do not emit prompt steps after imported expression extraction fails")
			})
		}
	}
}

func TestGeneratedPromptEvaluatesRunMetadata(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		for _, variant := range []string{"single_pass", "iterative"} {
			t.Run(mode+"/"+variant, func(t *testing.T) {
				dir := t.TempDir()
				workflows := filepath.Join(dir, ".github", "workflows")
				require.NoError(t, os.MkdirAll(workflows, 0o700))
				const example = "{{#if experiments.<name> == '<variant>'}}example{{/if}}"
				markdown := "Repository: ${{ github.repository }}\n{{#if experiments.reasoning_depth == 'iterative'}}\nSecond pass\n{{#else}}\nSingle pass\n{{/if}}\n{{#if inputs.enabled}}\nEnabled\n{{/if}}\nRepository: ${{ github.repository }}\nRun ID: ${{ github.run_id }}\nRun URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\nInput: ${{ inputs.value }}\nExample: `" + example + "`\n~~~markdown\n" + example + "\n~~~\n"
				source := filepath.Join(workflows, "main.md")
				require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\n---\n"+markdown), 0o600))
				data := &WorkflowData{
					MainWorkflowMarkdown: markdown, MarkdownContent: markdown,
					InlinedImports: mode == "inlined-imports",
					Experiments:    map[string][]string{"reasoning_depth": {"single_pass", "iterative"}},
				}
				compiler := NewCompiler()
				compiler.inlinePrompt = mode == "inline"
				compiler.markdownPath = source
				var output strings.Builder
				require.NoError(t, compiler.generatePrompt(&output, data, false, nil))
				const payload = "${{ github.run_id }} {{#runtime-import missing.md}} __GH_AW_GITHUB_RUN_ID__"
				result := runGeneratedMetadataPrompt(t, dir, output.String(), map[string]string{
					"github.repository": "github/gh-aw", "github.run_id": "41739470047",
					"github.server_url": "https://github.com", "inputs.value": payload,
					"inputs.enabled": map[bool]string{false: "false", true: "true"}[variant == "iterative"],
					"steps.pick-experiment.outputs.reasoning_depth":                variant,
					"steps.pick-experiment.outputs.reasoning_depth == 'iterative'": map[bool]string{false: "false", true: "true"}[variant == "iterative"],
				})
				var promptFiles map[string]string
				require.NoError(t, json.Unmarshal([]byte(result), &promptFiles))
				for _, file := range []string{"prompt.txt", "user.txt"} {
					prompt := promptFiles[file]
					assert.Contains(t, prompt, "Repository: github/gh-aw\n")
					assert.Contains(t, prompt, "Run ID: 41739470047\n")
					assert.Contains(t, prompt, "Run URL: https://github.com/github/gh-aw/actions/runs/41739470047\n")
					assert.Contains(t, prompt, "Input: "+payload+"\n")
					assert.Contains(t, prompt, "Example: `"+example+"`")
					assert.Contains(t, prompt, "~~~markdown\n"+example+"\n~~~")
					assert.Equal(t, variant == "iterative", strings.Contains(prompt, "Second pass"))
					assert.Equal(t, variant == "single_pass", strings.Contains(prompt, "Single pass"))
					assert.Equal(t, variant == "iterative", strings.Contains(prompt, "Enabled"))
					assert.NotContains(t, strings.ReplaceAll(prompt, payload, ""), "${{")
				}
			})
		}
	}
}

func runGeneratedMetadataPrompt(t *testing.T, dir, generated string, values map[string]string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for compiler-to-runtime coverage")
	}
	runtimeDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	var steps []struct {
		Env  map[string]string `yaml:"env"`
		With struct {
			Script string `yaml:"script"`
		} `yaml:"with"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated), &steps))
	for i := range steps {
		for key, value := range steps[i].Env {
			if key == "GH_AW_ACTIONS_DIR" {
				steps[i].Env[key] = runtimeDir
			} else if expression := stripExpressionWrapper(value); isExpression(value) {
				if expression == "runner.temp" {
					steps[i].Env[key] = dir
				} else {
					require.Contains(t, values, expression, "supply a test double for each Actions expression")
					steps[i].Env[key] = values[expression]
				}
			} else {
				steps[i].Env[key] = strings.ReplaceAll(value, "${{ runner.temp }}", dir)
			}
		}
	}
	serialized, err := json.Marshal(steps)
	require.NoError(t, err)
	command := exec.Command(node, "-e", `
const fs = require("fs"), path = require("path");
const dir = process.argv[2];
fs.mkdirSync(path.join(dir, "gh-aw", "prompts"), { recursive: true });
fs.cpSync(path.join(process.argv[3], "..", "md"), path.join(dir, "gh-aw", "prompts"), { recursive: true });
const core = { info() {}, debug() {}, warning() {}, setFailed(message) { throw new Error(message); } };
const context = { payload: {}, repo: { owner: "github", repo: "gh-aw" } };
const AsyncFunction = Object.getPrototypeOf(async function() {}).constructor;
(async () => {
  for (const step of JSON.parse(process.argv[1])) {
    if (!step.With.Script) continue;
    process.env = { ...process.env, RUNNER_TEMP: dir, GITHUB_WORKSPACE: dir, ...step.Env };
    await new AsyncFunction("require", "core", "context", step.With.Script)(require, core, context);
  }
  const result = {};
  for (const file of ["prompt.txt", "user.txt"]) result[file] = fs.readFileSync(path.join(dir, "gh-aw", "aw-prompts", file), "utf8");
  process.stdout.write(JSON.stringify(result));
})().catch(error => { console.error(error); process.exitCode = 1; });
`, string(serialized), dir, runtimeDir)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}
