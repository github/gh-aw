package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPromptSequencePreservesLiteralContext(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for compiler-to-runtime coverage")
	}
	runtimeDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	directory, err := filepath.Abs(".prompt-provenance-generated-sequence")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(directory, 0700))
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	prompt := filepath.Join(directory, "prompt.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("Context: ${GH_AW_EXPR_CONTEXT}\nStyle: __GH_AW_EXPERIMENTS_STYLE__\nRelease: __GH_AW_NEEDS_RELEASE_OUTPUTS_ID__\nAUTHOR RULE"), 0600))
	const deferred = "__GH_AW_DEFERRED_RELEASE__"
	mappings := []*ExpressionMapping{
		{EnvVar: "GH_AW_EXPR_CONTEXT", Content: "inputs.comment"},
		{EnvVar: "GH_AW_EXPERIMENTS_STYLE", Content: "steps.pick-experiment.outputs.style"},
		{EnvVar: "GH_AW_NEEDS_RELEASE_OUTPUTS_ID", Content: deferred, IsLiteral: true},
	}
	data := &WorkflowData{
		MarkdownContent: "Context: ${GH_AW_EXPR_CONTEXT}",
		DeferredPromptExpressions: []*ExpressionMapping{
			{EnvVar: "GH_AW_DEFERRED_RELEASE", Content: "needs.release.outputs.id"},
		},
	}
	var generated strings.Builder
	compiler := NewCompiler()
	compiler.generateInterpolationAndTemplateStep(&generated, mappings, data, false)
	generatePlaceholderSubstitutionStep(&generated, mappings, "      ", data)
	compiler.generateDeferredPromptSubstitutionStep(&generated, data)
	var steps []struct {
		With struct {
			Script string `yaml:"script"`
		} `yaml:"with"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated.String()), &steps))
	require.Len(t, steps, 3)
	var scripts []string
	for _, step := range steps {
		scripts = append(scripts, strings.ReplaceAll(step.With.Script, SetupActionDestination, runtimeDir))
	}
	assert.Contains(t, scripts[0], "await main({ deferredBindings:")
	assert.Contains(t, scripts[1], "deferredBindings:")
	assert.NotContains(t, scripts[2], "deferredBindings:")
	serialized, err := json.Marshal(scripts)
	require.NoError(t, err)
	command := exec.Command(node, "-e", `
const fs = require("fs");
const { spawnSync } = require("child_process");
const core = { info() {}, debug() {}, warning() {}, setFailed(message) { throw new Error(message); } };
const context = { payload: {}, repo: { owner: "owner", repo: "repo" } };
const scripts = JSON.parse(process.argv[1]);
const AsyncFunction = Object.getPrototypeOf(async function() {}).constructor;
const payload = "__GH_AW_EXPERIMENTS_STYLE__ __GH_AW_DEFERRED_RELEASE__";
process.env.GH_AW_PROMPT = process.argv[2];
process.env.GITHUB_WORKSPACE = process.argv[3];
process.env.GH_AW_EXPR_CONTEXT = payload;
process.env.GH_AW_EXPERIMENTS_STYLE = "concise";
process.env.GH_AW_NEEDS_RELEASE_OUTPUTS_ID = "__GH_AW_DEFERRED_RELEASE__";
async function run(script) {
  await new AsyncFunction("require", "core", "github", "context", "exec", "io", "getOctokit", script)(
    require, core, { rest: {}, hook: { before() {} } }, context, {}, {}, () => {}
  );
}
function validate(deferred) {
  const result = spawnSync("bash", [process.argv[4]], {
    env: { ...process.env, GH_AW_DEFERRED_PLACEHOLDERS: deferred }, encoding: "utf8"
  });
  if (result.status !== 0) throw new Error(result.stdout + result.stderr);
}
(async () => {
  await run(scripts[0]);
  await run(scripts[1]);
  validate("__GH_AW_DEFERRED_RELEASE__");
  process.env.GH_AW_DEFERRED_RELEASE = "123 __GH_AW_EXPERIMENTS_STYLE__";
  await run(scripts[2]);
  validate("");
  process.stdout.write(fs.readFileSync(process.env.GH_AW_PROMPT, "utf8"));
})().catch(error => { console.error(error); process.exitCode = 1; });
`, string(serialized), prompt, directory, filepath.Join(runtimeDir, "..", "sh", "validate_prompt_placeholders.sh"))
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Equal(t, "Context: __GH_AW_EXPERIMENTS_STYLE__ __GH_AW_DEFERRED_RELEASE__\nStyle: concise\nRelease: 123 __GH_AW_EXPERIMENTS_STYLE__\nAUTHOR RULE", string(output))
}
