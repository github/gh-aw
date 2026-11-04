//go:build !integration

package workflow

import (
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

func reviewBatchMCPGateway(t *testing.T, engine, configuration string) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.md")
	source := fmt.Sprintf("---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\nengine: %s\n%s\n---\nInspect the configured tools.\n", engine, configuration)
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(path))
	compiled, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal(compiled, &workflow))
	for _, job := range workflow["jobs"].(map[string]any) {
		for _, raw := range job.(map[string]any)["steps"].([]any) {
			step := raw.(map[string]any)
			if step["id"] == "start-mcp-gateway" {
				return step
			}
		}
	}
	t.Fatal("gateway step not found")
	return nil
}

func reviewBatchMCPJSON(t *testing.T, step map[string]any) map[string]any {
	t.Helper()
	run := step["run"].(string)
	_, heredoc, found := strings.Cut(run, "cat << ")
	require.True(t, found)
	header, body, found := strings.Cut(heredoc, "\n")
	require.True(t, found)
	body, _, found = strings.Cut(body, "\n"+strings.Fields(header)[0])
	require.True(t, found)
	command := exec.Command("bash", "-e", "-c", "cat <<EOF\n"+body+"\nEOF")
	command.Env = append(os.Environ(), "MCP_GATEWAY_PORT=8080", "GITHUB_MCP_SERVER_TOKEN=test", "GITHUB_SERVER_URL=https://github.com", "GITHUB_MCP_GUARD_MIN_INTEGRITY=none", "GITHUB_MCP_GUARD_REPOS=all")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var config map[string]any
	require.NoError(t, json.Unmarshal(output, &config), "%s", output)
	return config
}

func reviewBatchMCPResolve(t *testing.T, config any, step map[string]any, values map[string]string) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	command := exec.Command("node", "-e", `
const fs = require("fs");
const {resolveMCPConfigExpressions} = require("../../actions/setup/js/start_mcp_gateway.cjs");
process.stdout.write(JSON.stringify(resolveMCPConfigExpressions(JSON.parse(fs.readFileSync(0, "utf8")))));
`)
	command.Stdin = strings.NewReader(string(encoded))
	command.Env = append(os.Environ(), "GITHUB_WORKSPACE=/workspace", "TOKEN=token-data")
	for name, expression := range step["env"].(map[string]any) {
		if value, ok := values[fmt.Sprint(expression)]; ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var resolved map[string]any
	require.NoError(t, json.Unmarshal(output, &resolved))
	return resolved
}

func TestReviewBatchMCPFieldsTransportAcrossEngines(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "agy"} {
		t.Run(engine, func(t *testing.T) {
			step := reviewBatchMCPGateway(t, engine, `mcp-servers:
  custom:
    container: 'example/mcp'
    version: '${{ env.MCP_VERSION }}'
    entrypoint: '${{ env.MCP_EXECUTABLE }}'
    registry: '${{ env.MCP_REGISTRY }}'
    proxy-args: ['${{ env.MCP_PROXY }}']
    allowed: ['${{ env.MCP_TOOL }}']
    args: ['\$(printf literal)', '\`+"`printf literal\\`"+`', '\$TOKEN']
    entrypointArgs: ['\$(printf entrypoint)']
    env:
      LITERAL: '\$(printf environment)'
`)
			assert.NotContains(t, step["run"], "${{")
			values := map[string]string{
				"${{ env.MCP_VERSION }}":    "v1",
				"${{ env.MCP_EXECUTABLE }}": "python",
				"${{ env.MCP_REGISTRY }}":   "https://registry.test/\"$(printf registry)`literal`",
				"${{ env.MCP_PROXY }}":      "--proxy=\"$(printf proxy)`literal`\\path\nnext",
				"${{ env.MCP_TOOL }}":       "inspect",
			}
			bindings := step["env"].(map[string]any)
			for expression := range values {
				_, expected := replaceMCPExpressions(expression, false)
				for name := range expected {
					assert.Equal(t, expression, bindings[name])
					assert.Contains(t, step["run"], " -e "+name)
				}
			}
			parsed := reviewBatchMCPJSON(t, step)
			resolved := reviewBatchMCPResolve(t, parsed["mcpServers"], step, values)
			custom := resolved["custom"].(map[string]any)
			assert.Equal(t, "example/mcp:v1", custom["container"])
			assert.Equal(t, "python", custom["entrypoint"])
			assert.Equal(t, values["${{ env.MCP_REGISTRY }}"], custom["registry"])
			assert.Equal(t, []any{values["${{ env.MCP_PROXY }}"]}, custom["proxy-args"])
			assert.Equal(t, []any{"inspect"}, custom["tools"])
			assert.Equal(t, []any{"$(printf literal)", "`printf literal`", "$TOKEN"}, custom["args"])
			assert.Equal(t, []any{"$(printf entrypoint)"}, custom["entrypointArgs"])
			assert.Equal(t, "$(printf environment)", custom["env"].(map[string]any)["LITERAL"])
		})
	}
}

func TestReviewBatchMCPEscapedLiteralComputations(t *testing.T) {
	for _, test := range []struct{ authored, want string }{
		{`\$(printf literal)`, "$(printf literal)"},
		{"\\`printf literal\\`", "`printf literal`"},
		{`\$((1+2))`, "$((1+2))"},
		{`\${TOKEN:-fallback}`, "${TOKEN:-fallback}"},
		{`\$TOKEN`, "$TOKEN"},
		{`$(printf literal)`, "$(printf literal)"},
		{"`printf literal`", "`printf literal`"},
	} {
		t.Run(test.authored, func(t *testing.T) {
			command := exec.Command("bash", "-eu", "-c", "cat <<EOF\n"+marshalMCPHeredocString(normalizeMCPShellReferences(test.authored))+"\nEOF")
			command.Env = append(os.Environ(), "TOKEN=must-not-expand")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			var value string
			require.NoError(t, json.Unmarshal(output, &value))
			assert.Equal(t, test.want, value)
		})
	}
}

func TestReviewBatchMCPUnsupportedFieldExpressions(t *testing.T) {
	for _, field := range []string{"entrypoint", "container", "version", "registry", "proxy-args", "allowed", "ca-cert-file"} {
		t.Run(field, func(t *testing.T) {
			var value any = "${{ env.PRIVATE_VALUE }}"
			if field == "proxy-args" || field == "allowed" {
				value = []any{value}
			}
			warnings, err := mcpConfigExpressionDiagnostics("example", map[string]any{field: value})
			if field == "ca-cert-file" {
				require.ErrorContains(t, err, "mcp-servers.example.ca-cert-file: unsupported Actions expression")
				assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			} else {
				require.NoError(t, err)
				assert.Empty(t, warnings)
			}
		})
	}
	_, err := mcpConfigExpressionDiagnostics("example", map[string]any{
		"entrypoint": `${{ env.PRIVATE_VALUE }}:\${GATEWAY_VALUE}`,
	})
	require.ErrorContains(t, err, "mcp-servers.example.entrypoint: cannot mix transported")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	_, err = validateMCPConfigsWithWarnings(map[string]any{
		"github": map[string]any{"args": []any{"--volume", `${{ env.DATA_DIR }}/\${GATEWAY_VALUE}:/data:ro`}},
	})
	require.ErrorContains(t, err, "tools.github.args[1]: cannot mix transported")
}

func TestReviewBatchMCPUnsupportedFieldsFailCompilation(t *testing.T) {
	for _, field := range []string{"entrypoint", "version", "registry", "proxy-args", "allowed", "ca-cert-file"} {
		t.Run(field, func(t *testing.T) {
			config := map[string]any{"container": "example/mcp:latest"}
			var value any = `${{ env.PRIVATE_VALUE }}:\${GATEWAY_VALUE}`
			if field == "proxy-args" || field == "allowed" {
				value = []any{value}
			}
			if field == "ca-cert-file" {
				config = map[string]any{"type": "http", "url": "https://example.test/mcp"}
				value = "${{ env.PRIVATE_VALUE }}/cert.pem"
			}
			config[field] = value
			frontmatter, err := yaml.Marshal(map[string]any{
				"on": "workflow_dispatch", "strict": false,
				"permissions": map[string]any{"contents": "read"},
				"mcp-servers": map[string]any{"example": config},
			})
			require.NoError(t, err)
			path := filepath.Join(t.TempDir(), "unsupported.md")
			require.NoError(t, os.WriteFile(path, []byte("---\n"+string(frontmatter)+"---\nInspect the configured tool.\n"), 0600))
			err = NewCompiler().CompileWorkflow(path)
			require.ErrorContains(t, err, "mcp-servers.example."+field)
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			assert.NoFileExists(t, strings.TrimSuffix(path, ".md")+".lock.yml")
		})
	}
}

func TestReviewBatchMCPMountAllowlistDataParity(t *testing.T) {
	for _, source := range []string{
		"/srv/$(printf evaluated)",
		"/srv/`printf evaluated`",
		`/srv/$(printf "quoted")`,
		`/srv/\$(printf literal)`,
		"/srv/\\`printf literal\\`",
		`/srv/\$TOKEN`,
		`/srv/$TOKEN`,
		`/srv/${TOKEN}`,
		`/srv/\${TOKEN}`,
		`/srv/${TOKEN:-fallback}`,
		`${{ env.DATA_DIR }}/$(printf literal)`,
	} {
		t.Run(source, func(t *testing.T) {
			for _, surface := range []string{"mounts", "args", "entrypointArgs"} {
				t.Run(surface, func(t *testing.T) {
					mount := source + ":/data:ro"
					var field any = []any{mount}
					if surface != "mounts" {
						field = []any{"--volume", mount}
					}
					tools := map[string]any{"custom": map[string]any{
						"container": "example/mcp:latest",
						surface:     field,
					}}
					step := map[string]any{"env": map[string]any{}}
					for name, expression := range collectMCPEnvironmentVariables(tools, nil, &WorkflowData{}, false) {
						step["env"].(map[string]any)[name] = expression
					}
					roots := escapeMCPMountRootsShell(buildMCPGatewayAllowedMountRoots(tools, nil))
					command := exec.Command("bash", "-eu", "-c", `export MCP_GATEWAY_ALLOWED_MOUNT_ROOTS="`+roots+`"; printf '%s' "$MCP_GATEWAY_ALLOWED_MOUNT_ROOTS"`)
					command.Env = append(os.Environ(), "TOKEN=token-data", "GITHUB_WORKSPACE=/workspace", "RUNNER_TEMP=/runner")
					for name, expression := range step["env"].(map[string]any) {
						if expression == "${{ env.DATA_DIR }}" {
							command.Env = append(command.Env, name+"=/expression/\"$(printf dynamic)`literal`")
						}
					}
					output, err := command.CombinedOutput()
					require.NoError(t, err, "%s", output)
					rendered, _ := replaceMCPExpressions(mount, false)
					encoded := marshalMCPHeredocString(normalizeMCPShellReferences(rendered))
					command = exec.Command("bash", "-eu", "-c", "cat <<EOF\n"+encoded+"\nEOF")
					data, err := command.CombinedOutput()
					require.NoError(t, err, "%s", data)
					var parsedMount string
					require.NoError(t, json.Unmarshal(data, &parsedMount))
					config := reviewBatchMCPResolve(t, map[string]any{"mount": parsedMount}, step, map[string]string{
						"${{ env.DATA_DIR }}": "/expression/\"$(printf dynamic)`literal`",
					})
					resolvedMount := strings.ReplaceAll(config["mount"].(string), "${TOKEN}", "token-data")
					source, mode, ok := parseMCPGatewayAllowlistMount(resolvedMount)
					require.True(t, ok)
					assert.Contains(t, string(output), source+":"+mode)
				})
			}
		})
	}
}

func TestReviewBatchMCPBuiltinVolumeBindings(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "agy"} {
		t.Run(engine, func(t *testing.T) {
			step := reviewBatchMCPGateway(t, engine, `tools:
  github:
    type: local
    toolsets: [repos]
    args: ['--volume', '${{ env.DATA_DIR }}:/data:ro', '\$(printf literal)']
`)
			_, bindings := replaceMCPExpressions("${{ env.DATA_DIR }}", false)
			for name, expression := range bindings {
				assert.Equal(t, expression, step["env"].(map[string]any)[name])
				assert.Contains(t, step["run"], " -e "+name)
			}
			assert.NotContains(t, step["run"], "${{")
			config := reviewBatchMCPJSON(t, step)
			resolved := reviewBatchMCPResolve(t, config["mcpServers"], step, map[string]string{
				"${{ env.DATA_DIR }}": "/srv/\"$(printf data)`literal`",
			})
			args := resolved["github"].(map[string]any)["args"].([]any)
			assert.Contains(t, args, "/srv/\"$(printf data)`literal`:/data:ro")
			assert.Contains(t, args, "$(printf literal)")
			for line := range strings.SplitSeq(step["run"].(string), "\n") {
				if strings.HasPrefix(line, "export MCP_GATEWAY_ALLOWED_MOUNT_ROOTS=") {
					command := exec.Command("bash", "-eu", "-c", line+`; printf '%s' "$MCP_GATEWAY_ALLOWED_MOUNT_ROOTS"`)
					command.Env = append(os.Environ(), "GITHUB_WORKSPACE=/workspace", "RUNNER_TEMP=/runner")
					for name := range bindings {
						command.Env = append(command.Env, name+"=/srv/\"$(printf data)`literal`")
					}
					output, err := command.CombinedOutput()
					require.NoError(t, err, "%s", output)
					assert.Contains(t, string(output), "/srv/\"$(printf data)`literal`:ro")
					return
				}
			}
			t.Fatal("mount allowlist export not found")
		})
	}
}
