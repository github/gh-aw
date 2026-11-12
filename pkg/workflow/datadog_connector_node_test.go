//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func datadogNode(t *testing.T) (string, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for bundled Datadog runtime coverage")
	}
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	return node, runtime
}

func TestDatadogConnectorBundledNodeFrozenPlans(t *testing.T) {
	node, runtime := datadogNode(t)
	source := strings.Replace(datadogFixture(t, "workflow.md"), `["12345678"]`, `["12345678", "12345679", "999999999999999"]`, 1)
	source = strings.Replace(source, "["+datadogIncidentID+"]", "["+datadogIncidentID+", 99999999-2222-4333-8444-555555555555]", 1)
	_, data, _, err := parseDatadogConsumer(t, source)
	require.NoError(t, err)
	config := data.Connectors["datadog"]
	reader, err := config.Agent.Encode()
	require.NoError(t, err)
	writer, err := config.SafeOutputs.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{
		"reader": reader, "writer": writer,
		"monitor": datadogFixture(t, "monitor.json"), "incident": datadogFixture(t, "incident.json"),
	})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "../connectors/testdata/providers/datadog/runtime.cjs", runtime)
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct{ Checks int }
	require.NoError(t, json.Unmarshal(output, &result))
	require.Equal(t, 48, result.Checks, "the bundled runtime must execute every Datadog boundary case")
}

func TestDatadogConnectorGeneratedEffectStagesInBundledNode(t *testing.T) {
	node, runtime := datadogNode(t)
	_, data, _, err := parseDatadogConsumer(t, datadogFixture(t, "workflow.md"))
	require.NoError(t, err)
	effect := data.SafeOutputs.Scripts["datadog-rename-incident"]
	require.NotNil(t, effect)
	root := t.TempDir()
	scripts := filepath.Join(root, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(scripts, 0700))
	for _, file := range []string{"connector_runtime.cjs", "connector_regex.cjs", "error_codes.cjs"} {
		content, err := os.ReadFile(filepath.Join(filepath.Dir(runtime), file))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(scripts, file), content, 0600))
	}
	input, err := json.Marshal(map[string]any{"script": effect.Script, "assets": filepath.Dir(runtime), "item": map[string]any{
		"type": "datadog_rename_incident", "incident-id": datadogIncidentID, "title": "Staged build evidence",
	}})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", `
const { readFileSync } = require("node:fs");
const { script, item, assets } = JSON.parse(readFileSync(0, "utf8"));
const realRequire = require;
const scopedRequire = name => {
  if (name === "./safe_output_helpers.cjs") return { isStagedMode: () => true };
  if (name === "./connector_effect_bindings.cjs") return {
    connectorEffectBindings: () => ({
      get env() { throw new Error("staging must not access credential bindings"); },
      record() { throw new Error("staging must not record a mutation"); }
    })
  };
  return name.startsWith("./") ? realRequire(require("node:path").join(assets, name)) : realRequire(name);
};
const result = new Function("require", "item", "temporaryIdMap", script)(scopedRequire, item, {});
process.stdout.write(JSON.stringify(result));
`)
	command.Env = []string{"RUNNER_TEMP=" + root, connectorEnvPrefix("datadog") + "PLAN=" + data.Connectors["datadog"].SafeOutputPlan}
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	request, err := data.Connectors["datadog"].SafeOutputs.PrepareSafeOutput("rename-incident", map[string]any{
		"incident-id": datadogIncidentID, "title": "Staged build evidence",
	})
	require.NoError(t, err)
	expected, err := json.Marshal(map[string]any{"staged": true, "request": request})
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(output))
	require.NotContains(t, string(output), "OAUTH_TOKEN")
	require.NotContains(t, string(output), "createdResource")
}
