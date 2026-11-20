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
      if (global.core.marker !== "core" || global.context.marker !== "context") throw new Error("Prompt globals missing");
      if (process.env.GITHUB_OUTPUT !== "synthetic-output-path") throw new Error("github-script output path removed");
      process.stdout.write(process.env.GH_AW_PUBLIC);
`, &WorkflowData{Env: "env:\n  ARBITRARY_SECRET: ${{ secrets.PRIVATE }}\n"}, nil))
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
	assert.Contains(t, script, "setupPromptGlobals(core, context);")
	assert.NotContains(t, script, "Object.keys(process.env)")
	assert.NotContains(t, script, "global.core =")
	assert.NotContains(t, script, "global.context =")
	harness := `process.env.NODE_OPTIONS = "--require=/nonexistent-injected-startup.cjs";
process.env.NODE_PATH = "/nonexistent-injected-modules";
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
new AsyncFunction("core", "context", process.env.GH_AW_TEST_SCRIPT)({marker: "core"}, {marker: "context"})
  .catch(error => { console.error(error); process.exitCode = 1; });`
	command := exec.Command(node, "-e", harness)
	command.Env = append(os.Environ(),
		"GITHUB_TOKEN=synthetic-token", "GH_TOKEN=synthetic-token", "INPUT_GITHUB-TOKEN=synthetic-token",
		"ACTIONS_RUNTIME_TOKEN=synthetic-token", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=synthetic-token",
		"ARBITRARY_SECRET=synthetic-secret", "GH_AW_UNSELECTED=synthetic-secret",
		"GH_AW_TEST_SCRIPT="+script, "GITHUB_OUTPUT=synthetic-output-path",
		"NODE_OPTIONS=", "NODE_PATH=")
	for key, value := range step.Env {
		if key == "GH_AW_ACTIONS_DIR" {
			value, err = filepath.Abs("../../actions/setup/js")
			require.NoError(t, err)
		}
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
`, &WorkflowData{}, nil))
	var steps []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &steps))
	require.Len(t, steps, 1)
	assert.Equal(t, `bash "${RUNNER_TEMP}/gh-aw/actions/validate_prompt_placeholders.sh"`, steps[0].Run)
	assert.NotContains(t, output.String(), "/usr/bin/env")
	assert.NotContains(t, output.String(), "GH_AW_PROMPT_SCRIPT")
}

func TestCredentialFreePromptExcludesRuntimeOTLPDefaults(t *testing.T) {
	data := &WorkflowData{}
	NewCompiler().injectOTLPConfig(data)
	var output strings.Builder
	require.NoError(t, writeCredentialFreePromptSteps(&output, `
- name: Validate prompt
  run: bash "${RUNNER_TEMP}/gh-aw/actions/validate_prompt_placeholders.sh"
`, data, nil))
	var steps []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &steps))
	require.Len(t, steps, 1)
	for _, key := range otlpSandboxExcludedEnvVarNames {
		assert.Contains(t, steps[0].Env, key, "runtime credentials must be explicitly cleared")
		assert.Empty(t, steps[0].Env[key])
		require.ErrorContains(t, validatePromptSecretBindings(data, "${{ env."+key+" }}"), "credential")
	}
}

func TestCredentialFreePromptPreservesIndentedChunks(t *testing.T) {
	content := strings.Repeat("x", 20889) + "\n   Continuation text\n145. Step 145: do the thing\n"
	chunks := splitContentIntoChunks(content)
	require.Len(t, chunks, 2)
	require.True(t, strings.HasPrefix(chunks[1], "   Continuation text\n"))
	chunks = append(chunks, "", "\n\n  - indented: after blank lines\n", "\tTabbed # not a comment\r\n\"quoted\" 'text' \\ backslash\n\n\n")

	compiler := &Compiler{}
	data := &WorkflowData{ParsedTools: NewTools(map[string]any{})}
	var generated strings.Builder
	_, sourceContent := compiler.generateUnifiedPromptCreationStepWithContent(&generated, nil, chunks, nil, data)
	require.Len(t, sourceContent, len(chunks))
	require.Equal(t, "   Continuation text\n145. Step 145: do the thing\n\n", sourceContent["GH_AW_PROMPT_CONTENT_0001"])
	require.Equal(t, "\tTabbed # not a comment\r\n\"quoted\" 'text' \\ backslash\n\n\n", sourceContent["GH_AW_PROMPT_CONTENT_0004"])

	var output strings.Builder
	require.NoError(t, writeCredentialFreePromptSteps(&output, generated.String(), data, sourceContent))
	var actual []WorkflowStep
	require.NoError(t, yaml.Unmarshal([]byte(output.String()), &actual))
	promptValues := 0
	for _, step := range actual {
		for key, value := range step.Env {
			if strings.HasPrefix(key, "GH_AW_PROMPT_CONTENT_") {
				assert.Equal(t, sourceContent[key], value, key)
				promptValues++
			}
		}
	}
	require.Equal(t, len(sourceContent), promptValues)
}

func TestValidatePromptContentRoundTrip(t *testing.T) {
	expected := []WorkflowStep{{
		Name: "Create prompt",
		Env:  map[string]string{"GH_AW_PROMPT_CONTENT_0000": "   continuation\nnext\n"},
	}}
	intendedContent := map[string]string{"GH_AW_PROMPT_CONTENT_0000": "   continuation\nnext\n"}
	for _, tt := range []struct {
		name    string
		encoded string
		message string
	}{
		{"unchanged", "- env:\n    GH_AW_PROMPT_CONTENT_0000: \"   continuation\\nnext\\n\"\n", ""},
		{"invalid YAML", "- env: [", "parse encoded prompt steps"},
		{"missing step", "[]", "prompt step count"},
		{"missing content", "- env: {}", "GH_AW_PROMPT_CONTENT_0000"},
		{"changed content", "- env:\n    GH_AW_PROMPT_CONTENT_0000: \"continuation\\nnext\\n\"\n", "preserved exactly"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePromptContentRoundTrip([]byte(tt.encoded), expected, intendedContent)
			if tt.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.message)
			}
		})
	}

	t.Run("first parse differs from source", func(t *testing.T) {
		generated := "- name: Create prompt\n  env:\n    GH_AW_PROMPT_CONTENT_0000: continuation\n"
		err := writeCredentialFreePromptSteps(&strings.Builder{}, generated, &WorkflowData{}, intendedContent)
		require.ErrorContains(t, err, "preserved exactly")
	})
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

func TestPromptValueUsesCredentials(t *testing.T) {
	for _, test := range []struct {
		value      string
		credential bool
	}{
		{value: "${{ vars.LOG_LEVEL || 'info' }}"},
		{value: "${{ vars['LOG_LEVEL'] }}"},
		{value: "${{ github.actor == 'bot' && vars.LOG_LEVEL }}"},
		{value: "${{ vars.LOG_LEVEL || secrets.PRIVATE }}", credential: true},
		{value: "${{ vars['LOG_LEVEL'] || secrets['PRIVATE'] }}", credential: true},
		{value: "${{ vars.LOG_LEVEL || github.token }}", credential: true},
		{value: "${{ vars.LOG_LEVEL || github['token'] }}", credential: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			assert.Equal(t, test.credential, promptValueUsesCredentials(test.value))
		})
	}
}

func TestCredentialWildcardReferences(t *testing.T) {
	for _, test := range []struct {
		value      string
		credential bool
	}{
		{value: "${{ github.* }}", credential: true},
		{value: "${{ (GitHub).* }}", credential: true},
		{value: "${{ ((GITHUB.*)) }}", credential: true},
		{value: "${{ toJSON(github.*) }}", credential: true},
		{value: "${{ github.*.value }}", credential: true},
		{value: "${{ github.token }}", credential: true},
		{value: "${{ github }}", credential: true},
		{value: "${{ secrets.* }}", credential: true},
		{value: "${{ secrets.*.value }}", credential: true},
		{value: "${{ github.repository }}"},
		{value: "${{ github.event.* }}"},
		{value: "${{ 'github.*' }}"},
		{value: "${{ 'toJSON(github.*)' }}"},
		{value: "${{ vars.VALUE }}"},
		{value: "${{ vars.* }}"},
		{value: "${{ vars.*.value }}"},
		{value: "${{ needs.*.result }}"},
	} {
		t.Run(test.value, func(t *testing.T) {
			assert.Equal(t, test.credential, promptValueUsesCredentials(test.value))
			assert.Equal(t, test.credential, valueUsesCredentialReference(test.value))
		})
	}
}

func TestCompilerPromptConfigurationVariableBoundary(t *testing.T) {
	for _, strict := range []string{"true", "false"} {
		for _, location := range []string{"main", "local-import"} {
			t.Run("strict="+strict+"/"+location, func(t *testing.T) {
				workflows := filepath.Join(t.TempDir(), ".github", "workflows")
				require.NoError(t, os.MkdirAll(workflows, 0700))
				for _, test := range []struct {
					name       string
					value      string
					expression string
					errorText  string
				}{
					{
						name:       "explicit non-secret alias",
						value:      "${{ vars.LOG_LEVEL || 'info' }}",
						expression: "env.LOG_LEVEL || 'info'",
					},
					{
						name:       "direct vars access",
						value:      "${{ vars.LOG_LEVEL }}",
						expression: "vars.LOG_LEVEL || 'info'",
						errorText:  "unauthorized expressions",
					},
					{
						name:       "alias with secret fallback",
						value:      "${{ vars.LOG_LEVEL || secrets.PRIVATE }}",
						expression: "env.LOG_LEVEL || 'info'",
						errorText:  "prompt creation has no secrets",
					},
				} {
					t.Run(test.name, func(t *testing.T) {
						body := "Level: ${{ " + test.expression + " }}\n"
						if location == "local-import" {
							require.NoError(t, os.WriteFile(filepath.Join(workflows, "inner.md"), []byte(body), 0600))
							require.NoError(t, os.WriteFile(filepath.Join(workflows, "outer.md"), []byte("{{#runtime-import inner.md}}"), 0600))
							body = "{{#runtime-import outer.md}}\n"
						}
						filename := filepath.Join(workflows, "test.md")
						source := "---\non: workflow_dispatch\nstrict: " + strict + "\nengine: copilot\nenv:\n  LOG_LEVEL: " + test.value + "\n---\n" + body
						require.NoError(t, os.WriteFile(filename, []byte(source), 0600))
						compiler := NewCompiler()
						compiler.SetQuiet(true)
						err := compiler.CompileWorkflow(filename)
						if test.errorText != "" {
							errorText := test.errorText
							if strict == "true" && test.name == "alias with secret fallback" {
								errorText = "strict mode: secrets detected in 'env' section"
							}
							require.ErrorContains(t, err, errorText)
							return
						}
						require.NoError(t, err)
						compiled, err := os.ReadFile(filepath.Join(workflows, "test.lock.yml"))
						require.NoError(t, err)
						assert.Contains(t, string(compiled), "LOG_LEVEL: "+test.value)
						assert.Contains(t, string(compiled), ": ${{ "+test.expression+" }}")
					})
				}
			})
		}
	}
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
