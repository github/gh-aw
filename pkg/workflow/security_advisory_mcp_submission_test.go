//go:build !integration

package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityAdvisoryMCPSubmissionPreservesBoundedContent(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for MCP submission tests")
	}
	config, err := generateSafeOutputsConfig(&WorkflowData{SafeOutputs: &SafeOutputsConfig{
		CreateSecurityAdvisory:     &CreateSecurityAdvisoryConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3"), GitHubToken: "${{ secrets.ADVISORY_TOKEN }}"}},
		AddSecurityAdvisoryComment: &AddSecurityAdvisoryCommentConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3"), GitHubToken: "${{ secrets.ADVISORY_TOKEN }}"}},
	}})
	require.NoError(t, err)
	require.NotContains(t, config, "secrets.")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	outputPath := filepath.Join(dir, "outputs.jsonl")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	jsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)

	var requests bytes.Buffer
	encoder := json.NewEncoder(&requests)
	require.NoError(t, encoder.Encode(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "advisory-test", "version": "1"}},
	}))
	id := 0
	for _, tool := range []string{"create_security_advisory", "add_security_advisory_comment"} {
		for _, length := range []int{64001, 65000, 65001} {
			id++
			args := map[string]any{"ghsa_id": "GHSA-2345-6789-cfgh", "body": strings.Repeat("x", length)}
			if tool == "create_security_advisory" {
				args = map[string]any{
					"summary": "Draft advisory", "description": strings.Repeat("x", length),
					"vulnerabilities": []any{map[string]any{"package": map[string]any{"ecosystem": "npm", "name": "example"}}},
				}
			}
			require.NoError(t, encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id, "method": "tools/call",
				"params": map[string]any{"name": tool, "arguments": args},
			}))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(jsDir, "safe_outputs_mcp_server.cjs"))
	cmd.Env = append(os.Environ(),
		"RUNNER_TEMP="+dir,
		"GH_AW_SAFE_OUTPUTS_CONFIG_PATH="+configPath,
		"GH_AW_SAFE_OUTPUTS="+outputPath,
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH="+filepath.Join(jsDir, "safe_outputs_tools.json"),
		"GH_AW_TARGET_REPO_SLUG=owner/repo",
	)
	cmd.Stdin = &requests
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "%s", stderr.String())
	responses := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.Len(t, responses, 7, "%s", output)
	for i, response := range responses[1:] {
		var decoded struct {
			Error  any `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(response), &decoded))
		if i%3 == 2 {
			assert.True(t, decoded.Error != nil || decoded.Result.IsError, "over-limit request must fail: %s", response)
		} else {
			assert.Nil(t, decoded.Error, "%s", response)
			assert.False(t, decoded.Result.IsError, "%s", response)
		}
	}
	recorded, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	entries := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	require.Len(t, entries, 4)
	for i, entry := range entries {
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(entry), &decoded))
		field := "description"
		if i >= 2 {
			field = "body"
		}
		length := []int{64001, 65000}[i%2]
		content, ok := decoded[field].(string)
		require.True(t, ok, "MCP must record the advisory content")
		assert.Len(t, content, length, "MCP must preserve the complete advisory content")
		assert.Equal(t, strings.Repeat("x", length), content, "MCP must not replace the advisory content")
	}
}
