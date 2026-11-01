//go:build !integration

package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPAuthoredExpressionsCompile(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "agy"} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "expressions.md")
			content := fmt.Sprintf(`---
on: workflow_dispatch
strict: false
permissions:
  contents: read
engine: %s
steps:
  - id: setup
    run: echo "port=8765" >> "$GITHUB_OUTPUT"
mcp-servers:
  legacy:
    type: http
    url: "http://host.docker.internal:$GH_AW_MCP_SCRIPTS_PORT"
    headers:
      X-Key: '\${GH_AW_MCP_SCRIPTS_API_KEY}'
  remote:
    type: http
    url: "http://localhost:${{ steps.setup.outputs.port }}/mcp"
    headers:
      X-Ref: "${{ github.ref }}"
      X-Computed: "${{ format('{0}: {1}', github.ref, steps.setup.outputs.port) }}"
  container:
    container: example/mcp:latest
    args:
      - "--ref=${{ github.ref }}"
      - "${{ steps.setup.outputs.port }}"
      - "${GITHUB_WORKSPACE}"
    entrypointArgs:
      - "${{ github.event.repository.description }}"
    env:
      DESCRIPTION: "${{ github.event.repository.description }}"
      WORKSPACE: "$GITHUB_WORKSPACE"
---
Inspect configured tools.
`, engine)
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(path))
			compiled, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			var workflow map[string]any
			require.NoError(t, yaml.Unmarshal(compiled, &workflow))
			jobs := workflow["jobs"].(map[string]any)
			var gateway map[string]any
			for _, job := range jobs {
				for _, step := range job.(map[string]any)["steps"].([]any) {
					if candidate := step.(map[string]any); candidate["id"] == "start-mcp-gateway" {
						gateway = candidate
					}
				}
			}
			require.NotNil(t, gateway)
			run := gateway["run"].(string)
			assert.NotContains(t, run, "${{", "MCP expressions must not be shell source")
			assert.Contains(t, run, `http://host.docker.internal:\${GH_AW_MCP_ENV_GH_AW_MCP_SCRIPTS_PORT}`)
			assert.Contains(t, run, `\${GH_AW_MCP_ENV_GITHUB_WORKSPACE}`)
			assert.Contains(t, run, `\${GH_AW_MCP_SCRIPTS_API_KEY}`, "escaped references must still be resolved by the gateway")
			bindings := gateway["env"].(map[string]any)
			for _, expression := range []string{
				"${{ steps.setup.outputs.port }}",
				"${{ github.ref }}",
				"${{ format('{0}: {1}', github.ref, steps.setup.outputs.port) }}",
				"${{ github.event.repository.description }}",
			} {
				placeholder, env := replaceMCPExpressions(expression, false)
				assert.Contains(t, run, placeholder)
				for name, value := range env {
					assert.Equal(t, value, bindings[name])
					assert.Contains(t, run, " -e "+name, "gateway container must receive binding")
				}
			}
		})
	}
}

func TestMCPNormalizeLegacyShellReferences(t *testing.T) {
	assert.Equal(t, `http://host.docker.internal:\${GH_AW_MCP_ENV_GH_AW_MCP_SCRIPTS_PORT}`,
		normalizeMCPShellReferences("http://host.docker.internal:$GH_AW_MCP_SCRIPTS_PORT"))
	assert.Equal(t, `\${GH_AW_MCP_ENV_GITHUB_WORKSPACE}/\${GH_AW_MCP_ENV_RUNNER_TEMP}`,
		normalizeMCPShellReferences("${GITHUB_WORKSPACE}/$RUNNER_TEMP"))
	assert.Equal(t, `\${TOKEN}`, normalizeMCPShellReferences(`\${TOKEN}`))
	placeholder := `\${GH_AW_MCP_EXPR_` + strings.Repeat("A", 64) + "}"
	assert.Equal(t, placeholder, normalizeMCPShellReferences(placeholder))
}

func TestMCPExpressionBindingsUseFullDigest(t *testing.T) {
	for _, expression := range []string{"github.ref", "steps.setup.outputs.port", "format('{0}: {1}', github.ref, steps.setup.outputs.port)"} {
		full := "${{ " + expression + " }}"
		placeholder, bindings := replaceMCPExpressions(full, false)
		name := fmt.Sprintf("%s%X", mcpExpressionEnvPrefix, sha256.Sum256([]byte(expression)))
		require.Len(t, bindings, 1)
		assert.Equal(t, full, bindings[name])
		assert.Equal(t, `\${`+name+"}", placeholder)
		assert.Len(t, name, len(mcpExpressionEnvPrefix)+64)
	}
}

func TestMCPExistingAgyConformanceShellURLCompile(t *testing.T) {
	shared, err := os.ReadFile("../../.github/workflows/shared/agy-conformance.md")
	require.NoError(t, err)
	frontmatter, _, found := strings.Cut(strings.TrimPrefix(string(shared), "---\n"), "\n---\n")
	require.True(t, found)
	var sharedConfig map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &sharedConfig))
	mcpConfig, err := yaml.Marshal(map[string]any{
		"mcp-servers": sharedConfig["mcp-servers"],
		"mcp-scripts": sharedConfig["mcp-scripts"],
	})
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "conformance.md")
	content := `---
on: workflow_dispatch
strict: false
permissions:
  contents: read
engine: agy
safe-outputs:
  noop:
` + string(mcpConfig) + "\n---\nInspect conformance tools.\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(path))
	compiled, err := os.ReadFile(filepath.Join(dir, "conformance.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(compiled), `"url": "http://host.docker.internal:\${GH_AW_MCP_ENV_GH_AW_MCP_SCRIPTS_PORT}"`)
	assert.Contains(t, string(compiled), `GH_AW_MCP_SCRIPTS_PORT: ${{ steps.mcp-scripts-start.outputs.port }}`)
}

func TestMCPHeredocStringPreservesJSONBytes(t *testing.T) {
	value := "\"\\\n\r\t`literal` $(literal) ${LITERAL} 日本語"
	encoded := marshalMCPHeredocString(value)
	command := exec.Command("bash", "-c", "cat <<EOF\n"+encoded+"\nEOF")
	output, err := command.Output()
	require.NoError(t, err)
	var decoded string
	require.NoError(t, json.Unmarshal(output, &decoded))
	assert.Equal(t, value, decoded)
}
