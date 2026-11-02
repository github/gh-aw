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

	"github.com/github/gh-aw/pkg/parser"
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
      X-Mixed: "${{ secrets.MCP_TOKEN }}:${{ github.ref }}"
      X-Runner: "${{ secrets.MCP_TOKEN }}:$GITHUB_RUN_ID"
  container:
    container: example/mcp:latest
    args:
      - "--ref=${{ github.ref }}"
      - "${{ steps.setup.outputs.port }}"
      - "${GITHUB_WORKSPACE}"
      - '\$TOKEN'
    entrypointArgs:
      - "${{ github.event.repository.description }}"
    env:
      DESCRIPTION: "${{ github.event.repository.description }}"
      WORKSPACE: "$GITHUB_WORKSPACE"
      CACHE: "${{ github.workspace }}/cache-${{ github.run_id }}"
      MIXED: "${{ secrets.MCP_TOKEN }}:${{ github.ref }}"
      RUNNER: "${{ secrets.MCP_TOKEN }}:$GITHUB_RUN_ID"
    mounts:
      - "${{ steps.setup.outputs.path }}:/data:ro"
      - "${{ github.workspace }}/cache-${{ github.run_id }}:/cache:ro"
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
				"${{ steps.setup.outputs.path }}",
				"${{ github.workspace }}",
				"${{ github.run_id }}",
				"${{ secrets.MCP_TOKEN }}",
			} {
				placeholder, env := replaceMCPExpressions(expression, false)
				assert.Contains(t, run, placeholder)
				for name, value := range env {
					assert.Equal(t, value, bindings[name])
					assert.Contains(t, run, " -e "+name, "gateway container must receive binding")
				}
			}
			_, heredoc, found := strings.Cut(run, "cat << ")
			require.True(t, found)
			header, body, found := strings.Cut(heredoc, "\n")
			require.True(t, found)
			body, _, found = strings.Cut(body, "\n"+strings.Fields(header)[0])
			require.True(t, found)
			command := exec.Command("bash", "-c", "cat <<EOF\n"+body+"\nEOF")
			command.Env = append(os.Environ(), "MCP_GATEWAY_PORT=8080")
			output, err := command.Output()
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, json.Unmarshal(output, &config), "heredoc must preserve valid JSON")
			servers := config["mcpServers"].(map[string]any)
			assert.Contains(t, servers["container"].(map[string]any)["args"].([]any), "$TOKEN")
		})
	}
}

func TestMCPNormalizeLegacyShellReferences(t *testing.T) {
	assert.Equal(t, `http://host.docker.internal:\${GH_AW_MCP_ENV_GH_AW_MCP_SCRIPTS_PORT}`,
		normalizeMCPShellReferences("http://host.docker.internal:$GH_AW_MCP_SCRIPTS_PORT"))
	assert.Equal(t, `\${GH_AW_MCP_ENV_GITHUB_WORKSPACE}/\${GH_AW_MCP_ENV_RUNNER_TEMP}`,
		normalizeMCPShellReferences("${GITHUB_WORKSPACE}/$RUNNER_TEMP"))
	assert.Equal(t, `\${TOKEN}`, normalizeMCPShellReferences(`\${TOKEN}`))
	assert.Equal(t, `$TOKEN`, normalizeMCPShellReferences(`\$TOKEN`))
	command := exec.Command("bash", "-c", "cat <<EOF\n"+marshalMCPHeredocString(normalizeMCPShellReferences(`\$TOKEN`))+"\nEOF")
	command.Env = append(os.Environ(), "TOKEN=must-not-expand")
	output, err := command.Output()
	require.NoError(t, err)
	var decoded string
	require.NoError(t, json.Unmarshal(output, &decoded))
	assert.Equal(t, "$TOKEN", decoded)
	placeholder := `\${GH_AW_MCP_EXPR_` + strings.Repeat("A", 64) + "}"
	assert.Equal(t, placeholder, normalizeMCPShellReferences(placeholder))
}

func TestMCPExpressionBindingNormalization(t *testing.T) {
	config := &parser.RegistryMCPServerConfig{
		Headers: map[string]string{"X-Spaced": "${{ github.ref }}", "X-Compact": "${{github.ref}}"},
		Env:     map[string]string{"SPACED": "${{  github.ref  }}", "COMPACT": "${{github.ref}}"},
	}
	placeholder, expected := replaceMCPExpressions("${{ github.ref }}", false)
	for range 100 {
		assert.Equal(t, expected, collectMCPConfigExpressionBindings(config))
	}
	for _, expression := range []string{"${{github.ref}}", "${{  github.ref  }}", "${{\tgithub.ref\t}}"} {
		actualPlaceholder, actual := replaceMCPExpressions(expression, false)
		assert.Equal(t, placeholder, actualPlaceholder)
		assert.Equal(t, expected, actual)
	}
}

func TestMCPExpressionMountAllowlistUsesTransport(t *testing.T) {
	expression := "${{ steps.setup.outputs.path }}"
	roots := buildMCPGatewayAllowedMountRoots(map[string]any{
		"container": map[string]any{
			"container": "example/mcp:latest",
			"mounts":    []any{expression + ":/data:ro"},
		},
	}, nil)
	assert.NotContains(t, roots, "${{")
	command := exec.Command("bash", "-c", `printf '%s' "`+roots+`"`)
	command.Env = os.Environ()
	value := "/srv/\"$(printf executed)`printf also-executed`"
	_, bindings := replaceMCPExpressions(expression, false)
	for name := range bindings {
		command.Env = append(command.Env, name+"="+value)
	}
	output, err := command.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), value+":ro")
}

func TestMCPMixedLegacyExpressions(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{"${{ github.workspace }}/cache-${{ github.run_id }}", "/workspace/cache-123"},
		{"${{ secrets.MCP_TOKEN }}:${{ github.ref }}", "token:refs/heads/main"},
		{"${{ env.MCP_TOKEN }}:${{ github.ref }}", "token:refs/heads/main"},
		{"${{ secrets.MCP_TOKEN }}:$GITHUB_RUN_ID", "token:123"},
		{"${{ env.MCP_TOKEN }}:${GITHUB_RUN_ID}", "token:123"},
	} {
		t.Run(test.value, func(t *testing.T) {
			rendered, bindings := replaceMCPExpressions(test.value, true)
			command := exec.Command("bash", "-c", "cat <<EOF\n"+marshalMCPHeredocString(normalizeMCPShellReferences(rendered))+"\nEOF")
			output, err := command.Output()
			require.NoError(t, err)
			command = exec.Command("node", "-e", `
const fs = require("fs");
const {resolveMCPConfigExpressions} = require("../../actions/setup/js/start_mcp_gateway.cjs");
process.stdout.write(JSON.stringify(resolveMCPConfigExpressions(JSON.parse(fs.readFileSync(0, "utf8")))));
`)
			command.Stdin = strings.NewReader(string(output))
			command.Env = append(os.Environ(), "GITHUB_RUN_ID=123")
			values := map[string]string{
				"${{ github.workspace }}":  "/workspace",
				"${{ github.run_id }}":     "123",
				"${{ secrets.MCP_TOKEN }}": "token",
				"${{ env.MCP_TOKEN }}":     "token",
				"${{ github.ref }}":        "refs/heads/main",
			}
			for name, expression := range bindings {
				value, ok := values[expression]
				require.True(t, ok, "unexpected expression %s", expression)
				command.Env = append(command.Env, name+"="+value)
			}
			resolved, err := command.CombinedOutput()
			require.NoError(t, err, "%s", resolved)
			var decoded string
			require.NoError(t, json.Unmarshal(resolved, &decoded))
			assert.Equal(t, test.want, decoded)
		})
	}
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
