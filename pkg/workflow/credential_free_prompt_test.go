package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialFreePromptEnvironment(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for prompt process coverage")
	}
	var output strings.Builder
	require.NoError(t, writeCredentialFreePromptSteps(&output, `
- name: Create prompt
  uses: actions/github-script@pinned # v9.0.0
  env:
    GH_AW_PUBLIC: public-value
  with:
    script: |
      for (const key of ["GITHUB_TOKEN", "GH_TOKEN", "INPUT_GITHUB-TOKEN", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ARBITRARY_SECRET", "NODE_OPTIONS", "NODE_PATH", "GH_AW_UNSELECTED"]) {
        if (key in process.env) throw new Error("Inherited credential or startup environment: " + key);
      }
      if (typeof global.github !== "undefined" || typeof global.getOctokit !== "undefined") throw new Error("Authenticated client available");
      if (process.env.GITHUB_OUTPUT !== "synthetic-output-path") throw new Error("github-script output path removed");
      process.stdout.write(process.env.GH_AW_PUBLIC);
`, &WorkflowData{Env: "env:\n  ARBITRARY_SECRET: ${{ secrets.PRIVATE }}\n"}))
	var steps []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &steps))
	require.Len(t, steps, 1)
	step := steps[0]
	assert.Equal(t, "actions/github-script@pinned", step.Uses)
	assert.Contains(t, output.String(), "uses: actions/github-script@pinned # v9.0.0")
	assert.Equal(t, promptGitHubScriptToken, step.With["github-token"])
	assert.Empty(t, step.Run)
	assert.Empty(t, step.Env["ARBITRARY_SECRET"])
	script, ok := step.With["script"].(string)
	require.True(t, ok)
	harness := `process.env.NODE_OPTIONS = "--require=/nonexistent-injected-startup.cjs";
process.env.NODE_PATH = "/nonexistent-injected-modules";
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
new AsyncFunction("core", "context", process.env.GH_AW_TEST_SCRIPT)({}, {})
  .catch(error => { console.error(error); process.exitCode = 1; });`
	command := exec.Command(node, "-e", harness)
	command.Env = append(os.Environ(),
		"GITHUB_TOKEN=synthetic-token", "GH_TOKEN=synthetic-token", "INPUT_GITHUB-TOKEN=synthetic-token",
		"ACTIONS_RUNTIME_TOKEN=synthetic-token", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=synthetic-token",
		"ARBITRARY_SECRET=synthetic-secret", "GH_AW_UNSELECTED=synthetic-secret",
		"GH_AW_TEST_SCRIPT="+script, "GITHUB_OUTPUT=synthetic-output-path",
		"NODE_OPTIONS=", "NODE_PATH=")
	for key, value := range step.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	result, err := command.CombinedOutput()
	require.NoError(t, err, "%s", result)
	assert.Equal(t, "public-value", string(result))
}

func TestCredentialFreePromptPreservesShellSteps(t *testing.T) {
	var output strings.Builder
	require.NoError(t, writeCredentialFreePromptSteps(&output, `
- name: Validate prompt
  run: bash "${RUNNER_TEMP}/gh-aw/actions/validate_prompt_placeholders.sh"
`, &WorkflowData{}))
	var steps []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &steps))
	require.Len(t, steps, 1)
	assert.Equal(t, `bash "${RUNNER_TEMP}/gh-aw/actions/validate_prompt_placeholders.sh"`, steps[0].Run)
	assert.NotContains(t, output.String(), "/usr/bin/env")
	assert.NotContains(t, output.String(), "GH_AW_PROMPT_SCRIPT")
}

func TestPromptRejectsSecretBackedAuthorBindings(t *testing.T) {
	for _, value := range []string{"${{ secrets.PRIVATE }}", "${{ secrets['PRIVATE'] || 'fallback' }}", "${{ github.token }}", "${{ github['token'] }}"} {
		t.Run(value, func(t *testing.T) {
			data := &WorkflowData{Env: "env:\n  PRIVATE: " + value + "\n"}
			require.ErrorContains(t, validatePromptSecretBindings(data, "Local: ${{ env.PRIVATE || 'fallback' }}"), "prompt creation has no secrets")
			require.ErrorContains(t, validatePromptSecretBindings(data, "Local: ${{ env['private'] }}"), "prompt creation has no secrets")
			data.MarkdownContent = "{{#if env.PRIVATE}}\nAuthor\n{{/if}}"
			require.ErrorContains(t, validatePromptSecretBindings(data, ""), "prompt creation has no secrets")
		})
	}
	require.NoError(t, validatePromptSecretBindings(&WorkflowData{
		Env: "env:\n  PUBLIC: public-value\n  PRIVATE: ${{ secrets.PRIVATE }}\n",
	}, "Local: ${{ env.PUBLIC }}"))
	require.ErrorContains(t, validatePromptSecretBindings(&WorkflowData{MarkdownContent: "${{ env.GITHUB_TOKEN }}"}, ""), "credential")
}

func TestCompilerRejectsSecretBackedLocalAuthorBinding(t *testing.T) {
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "outer.md"), []byte("{{#runtime-import inner.md}}"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "inner.md"), []byte("{{#if env.PRIVATE}}\n${{ env.PRIVATE || 'fallback' }}\n{{/if}}"), 0600))
	source := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\nenv:\n  PRIVATE: ${{ secrets.PRIVATE }}\n---\n{{#runtime-import outer.md}}\n"
	filename := filepath.Join(workflows, "test.md")
	require.NoError(t, os.WriteFile(filename, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.SetQuiet(true)
	require.ErrorContains(t, compiler.CompileWorkflow(filename), "prompt creation has no secrets")
}
