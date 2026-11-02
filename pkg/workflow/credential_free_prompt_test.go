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
	runtimeDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	var output strings.Builder
	require.NoError(t, writeCredentialFreePromptSteps(&output, `
- name: Create prompt
  uses: actions/github-script@pinned
  env:
    GH_AW_PUBLIC: public-value
  with:
    script: |
      for (const key of ["GITHUB_TOKEN", "GH_TOKEN", "INPUT_GITHUB-TOKEN", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ARBITRARY_SECRET", "NODE_OPTIONS", "NODE_PATH", "GH_AW_UNSELECTED"]) {
        if (key in process.env) throw new Error("Inherited credential or startup environment: " + key);
      }
      if (typeof global.github !== "undefined" || typeof global.getOctokit !== "undefined") throw new Error("Authenticated client available");
      process.stdout.write(process.env.GH_AW_PUBLIC);
`, &WorkflowData{Env: "env:\n  ARBITRARY_SECRET: ${{ secrets.PRIVATE }}\n"}))
	var steps []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &steps))
	require.Len(t, steps, 1)
	step := steps[0]
	assert.Empty(t, step.Uses)
	assert.Empty(t, step.With)
	assert.Empty(t, step.Env["ARBITRARY_SECRET"])
	step.Env["GH_AW_ACTIONS_DIR"] = runtimeDir
	command := exec.Command("bash", "--noprofile", "--norc", "-e", "-c", step.Run)
	command.Env = append(os.Environ(),
		"PATH="+filepath.Dir(node)+":/usr/bin:/bin",
		"GITHUB_TOKEN=synthetic-token", "GH_TOKEN=synthetic-token", "INPUT_GITHUB-TOKEN=synthetic-token",
		"ACTIONS_RUNTIME_TOKEN=synthetic-token", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=synthetic-token",
		"ARBITRARY_SECRET=synthetic-secret", "GH_AW_UNSELECTED=synthetic-secret",
		"NODE_OPTIONS=--require=/nonexistent-injected-startup.cjs", "NODE_PATH=/nonexistent-injected-modules")
	for key, value := range step.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	result, err := command.CombinedOutput()
	require.NoError(t, err, "%s", result)
	assert.Equal(t, "public-value", string(result))
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
