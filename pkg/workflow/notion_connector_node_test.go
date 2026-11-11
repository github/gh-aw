//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNotionConnectorBundledNodeRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to verify the bundled connector runtime")
	read, write := notionFrozenPlans(t)
	readEncoded, err := read.Encode()
	require.NoError(t, err)
	writeEncoded, err := write.Encode()
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"read": readEncoded, "write": writeEncoded,
		"page": json.RawMessage(notionResponseFixture(t, "page")), "comment": json.RawMessage(notionResponseFixture(t, "comment")),
	})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "testdata/notion/runtime.cjs", runtime)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "passed bundled Node wire, MCP, staging and rejection checks")
}

func TestNotionConnectorGeneratedScriptSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	source := strings.Replace(notionConsumerExample(t), "  staged: true\n", "", 1)
	compiler, data, path, err := parseNotionConsumer(t, source)
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	var document struct {
		Jobs map[string]struct {
			Steps []struct {
				Name, Run string
			}
		}
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	found := false
	for _, step := range document.Jobs["safe_outputs"].Steps {
		if step.Name != "Configure Safe Output Scripts" {
			continue
		}
		found = true
		first, rest, ok := strings.Cut(step.Run, "\n")
		require.True(t, ok)
		require.Contains(t, first, "safe_output_script_notion_add_comment.cjs")
		_, delimiter, ok := strings.Cut(first, "<< '")
		require.True(t, ok)
		code, _, ok := strings.Cut(rest, "\n"+strings.TrimSuffix(delimiter, "'")+"\n")
		require.True(t, ok)
		command := exec.CommandContext(t.Context(), node, "--check", "-")
		command.Stdin = strings.NewReader(code)
		result, err := command.CombinedOutput()
		require.NoError(t, err, "%s", result)
	}
	require.True(t, found, "the actual compiled wrapper must be syntax-checked")
}
