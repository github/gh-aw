package workflow

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsPrivateRuntimePaths(t *testing.T) {
	job := `    steps:
      - name: Setup Scripts
        uses: ./actions/setup
        with:
          destination: ${{ runner.temp }}/gh-aw/actions
      - name: Write runtime data
        run: |
          mkdir -p "${{ env.GH_AW_RUNTIME_DIR }}/agent"
          printf '%s\n' '${{ env.GH_AW_RUNTIME_DIR }}/agent/output.json'
          cat <<'CONFIG'
          {"output":"${{ env.GH_AW_RUNTIME_DIR }}/agent/output.json"}
          CONFIG
          node "${{ runner.temp }}/gh-aw/actions/helper.cjs"
      - name: Upload runtime data
        uses: actions/upload-artifact@v4
        with:
          path: ${{ env.GH_AW_RUNTIME_DIR }}/agent/output.json
`
	rendered := renderStepForRunner(job, "runs-on: windows-latest", "agent")
	assert.NotContains(t, rendered, "/tmp/gh-aw")
	assert.NotContains(t, rendered, "gh-aw-setup")
	assert.Contains(t, rendered, "destination: ${{ runner.temp }}/gh-aw/actions")
	assert.Contains(t, rendered, "path: ${{ env.GH_AW_RUNTIME_DIR }}/agent/output.json")
	script := decodeWindowsWSLRunScript(t, rendered)
	assert.Contains(t, script, `mkdir -p "${GH_AW_RUNTIME_DIR}/agent"`)
	assert.Contains(t, script, `printf '%s\n' '${GH_AW_RUNTIME_DIR}/agent/output.json'`)
	assert.Contains(t, script, `{"output":"${GH_AW_RUNTIME_DIR}/agent/output.json"}`)
	assert.Contains(t, script, `node "${RUNNER_TEMP}/gh-aw/actions/helper.cjs"`)
	assert.NotContains(t, script, runtimeDirectoryExpression)
	// Rewrite before Bash parsing, including single-quoted strings and heredocs.
	assert.Contains(t, rendered, "$scriptContent.Replace('${GH_AW_RUNTIME_DIR}', $wslRuntimeDirectory)")
	assert.Contains(t, rendered, "$runtimeIdentityExpression = '$' + '{{ env.GH_AW_RUNTIME_ID }}'")
	assert.Contains(t, rendered, "$wslRuntimeDirectory -cnotmatch '^/[a-zA-Z0-9_./-]+$'")
}

func decodeWindowsWSLRunScript(t *testing.T, rendered string) string {
	t.Helper()
	const start = "FromBase64String('"
	index := strings.Index(rendered, start)
	if index < 0 {
		t.Fatalf("WSL run wrapper does not contain its encoded script:\n%s", rendered)
	}
	encoded := rendered[index+len(start):]
	end := strings.IndexByte(encoded, '\'')
	if end < 0 {
		t.Fatalf("WSL run wrapper has an unterminated encoded script:\n%s", rendered)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded[:end])
	if err != nil {
		t.Fatalf("decode compressed WSL run script: %v", err)
	}
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()
	script, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("decompress WSL run script: %v", err)
	}
	return string(script)
}

func TestWindowsGatewayRuntimeIdentityIsLoweredBeforeExecution(t *testing.T) {
	job := `    steps:
      - name: Start gateway
        run: docker run --name awmg-mcpg-${{ env.GH_AW_RUNTIME_ID }} --label gh-aw.runtime-id=${{ env.GH_AW_RUNTIME_ID }} image
`
	rendered := renderStepForRunner(job, "runs-on: windows-latest", "agent")
	script := decodeWindowsWSLRunScript(t, rendered)
	assert.Contains(t, script, "awmg-mcpg-${{ env.GH_AW_RUNTIME_ID }}")
	assert.Contains(t, rendered, "$scriptContent.Replace($runtimeIdentityExpression, $env:GH_AW_RUNTIME_ID)")
}

func TestWindowsPrivateRuntimeEnvironment(t *testing.T) {
	setup := windowsWSLEnvironmentSetup(nil)
	rendered := strings.Join(setup, "\n")
	var pathVariables, allowedVariables string
	for _, line := range setup {
		if strings.HasPrefix(line, "$pathVariables = ") {
			pathVariables = line
		}
		if strings.HasPrefix(line, "$allowedEnvironmentVariables = ") {
			allowedVariables = line
		}
	}
	require.NotEmpty(t, pathVariables)
	require.NotEmpty(t, allowedVariables)
	for _, name := range []string{
		"GH_AW_RUNTIME_DIR", "GH_AW_RUNTIME_TMP_DIR", "GH_AW_MCP_LOG_DIR", "GITHUB_STATE",
	} {
		assert.Contains(t, pathVariables, "'"+name+"'")
		assert.Contains(t, allowedVariables, "'"+name+"'")
	}
	for _, name := range []string{
		"GH_AW_RUNTIME_ID", "GH_AW_RUNTIME_DEV", "GH_AW_RUNTIME_INO",
		"GH_AW_RUNTIME_UID", "GH_AW_RUNTIME_OWNER", "GH_AW_RUNTIME_TMP_ID",
		"GH_AW_CLI_PROXY_PORT", "GH_AW_MCP_GATEWAY_PORT", "GH_AW_MCP_CONTROL_PORT",
		"GH_AW_MCP_SCRIPTS_PORT", "GH_AW_SAFE_OUTPUTS_PORT", "GH_AW_COPILOT_SDK_PORT",
	} {
		assert.Contains(t, allowedVariables, "'"+name+"'")
		assert.NotContains(t, pathVariables, "'"+name+"'")
	}
	assert.Contains(t, rendered, `if ($pathVariableSet.Contains($_) -or $value -match`)
	assert.NotContains(t, allowedVariables, "'GITHUB_TOKEN'")
	assert.NotContains(t, rendered, "$processEnvironment.Keys")
}

func TestWindowsPrivateRuntimeOutputPaths(t *testing.T) {
	for _, root := range []string{
		runtimeDirectoryExpression, "${GH_AW_RUNTIME_DIR}", "${GH_AW_RUNTIME_DIR:?}",
	} {
		t.Run(root, func(t *testing.T) {
			script := `echo "GH_AW_SAFE_OUTPUTS=` + root + `/safeoutputs/outputs.jsonl"` + "\n"
			got := rewriteWindowsPathOutputs("Set runtime paths", script)
			assert.Contains(t, got, `echo "GH_AW_SAFE_OUTPUTS=$(wslpath -w "`+root+`/safeoutputs/outputs.jsonl")"`)
			assert.NotContains(t, got, "${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl")

			agentOutput := `echo "GH_AW_AGENT_OUTPUT=` + root + `/agent_output.json" >> "$GITHUB_OUTPUT"` + "\n"
			got = rewriteWindowsPathOutputs("Setup agent output environment variable", agentOutput)
			assert.Contains(t, got, `echo "GH_AW_AGENT_OUTPUT=$(wslpath -w "${GH_AW_RUNTIME_DIR:?}/agent_output.json")" >> "$GITHUB_OUTPUT"`)

			logOutput := `echo "GH_AW_MCP_LOG_DIR=` + root + `/mcp-logs/safeoutputs" >> "$GITHUB_ENV"` + "\n"
			got = rewriteWindowsPathOutputs("Set runtime paths", logOutput)
			assert.Contains(t, got, `echo "GH_AW_MCP_LOG_DIR=$(wslpath -w "`+root+`/mcp-logs/safeoutputs")" >> "$GITHUB_ENV"`)
		})
	}
}

func TestWindowsRuntimeNativeActionPaths(t *testing.T) {
	step := `      - name: Native helper
        uses: actions/github-script@v7
        env:
          GH_AW_RUNTIME_DIR: ${{ env.GH_AW_RUNTIME_DIR }}
          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_RUNTIME_DIR }}/agent_output.json
          GH_AW_ACTIONS_DIR: ${{ runner.temp }}/gh-aw/actions
        with:
          script: |
            require(process.env.GH_AW_ACTIONS_DIR + '/helper.cjs');
`
	assert.Equal(t, step, rewriteWindowsActionTempPath(step))
	assert.Equal(t, step, renderWindowsPOSIXRunStep(step, nil))
}

func TestWindowsWSLScriptUsesOwnedRuntime(t *testing.T) {
	rendered := strings.Join(windowsWSLScriptSetup("encoded"), "\n")
	assert.Contains(t, rendered, "$scriptDirectory = if ($env:GH_AW_RUNTIME_DIR) { $env:GH_AW_RUNTIME_DIR } else { $env:RUNNER_TEMP }")
	assert.Contains(t, rendered, "$scriptContent.Contains('${GH_AW_RUNTIME_DIR}') -and -not $env:GH_AW_RUNTIME_DIR")
	assert.Contains(t, rendered, "[System.IO.FileMode]::CreateNew")
	assert.NotContains(t, rendered, "New-Item")
	assert.NotContains(t, rendered, "Join-Path $env:RUNNER_TEMP 'gh-aw'")
	assert.Contains(t, rendered, "Remove-Item -LiteralPath $hostScriptPath -Force")
	assert.NotContains(t, rendered, "Remove-Item -Recurse")
	cleanupStart := strings.Index(rendered, "$exitCode = 0\ntry {")
	mapping := strings.Index(rendered, "$wslScriptPath = ")
	cleanup := strings.Index(rendered, "Remove-Item -LiteralPath $hostScriptPath")
	require.NotEqual(t, -1, cleanupStart)
	assert.Less(t, cleanupStart, mapping, "mapping failures must still remove the owned script")
	assert.Less(t, mapping, cleanup)
}

func TestWindowsWSLBootstrapDoesNotAliasRuntime(t *testing.T) {
	assert.NotContains(t, windowsWSLBootstrapStep, "/tmp/gh-aw")
	assert.NotContains(t, windowsWSLBootstrapStep, "$RUNNER_TEMP/gh-aw")
	assert.NotContains(t, windowsWSLBootstrapStep, "ln -s")
}
