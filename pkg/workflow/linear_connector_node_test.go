//go:build !integration

package workflow

import (
	"encoding/json"
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestLinearConnectorDocumentationBundledHost(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to verify the documented bundled host")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	for _, example := range loadLinearDocumentation(t).Examples {
		data, _ := compileLinearDocumentationExample(t, example)
		plan := data.Connectors["linear"].SafeOutputs
		for _, operation := range plan.SafeOutputNames() {
			t.Run(operation, func(t *testing.T) {
				inputs, err := plan.Inputs("safe-output", operation)
				require.NoError(t, err)
				args := linearDocumentedArguments(t, inputs)
				t.Run("ascii", func(t *testing.T) {
					checkLinearBundledIntent(t, node, runtime, plan, operation, args)
				})
				for _, field := range []string{"title", "body"} {
					if input, exists := inputs[field]; exists {
						t.Run("unicode-"+field, func(t *testing.T) {
							unicode := maps.Clone(args)
							unicode[field] = strings.Repeat("\u754c", input.MaxLength)
							checkLinearBundledIntent(t, node, runtime, plan, operation, unicode)
						})
					}
				}
			})
		}
	}
}

func checkLinearBundledIntent(t *testing.T, node, runtime string, plan *connectors.Plan, operation string, args map[string]any) {
	t.Helper()
	intent, err := plan.PrepareSafeOutputIntent(operation, args)
	require.NoError(t, err, "the documented arguments must be valid in the Go host")
	expected, err := json.Marshal(map[string]any{"staged": true, "request": intent})
	require.NoError(t, err)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{"plan": encoded, "operation": operation, "arguments": args})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", `
const { executeConnector } = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const env = new Proxy({}, { get() { throw new Error("Staging must not read secrets"); } });
executeConnector(input.plan, "safe-output", input.operation, input.arguments, {
  staged: true, env,
  fetchImpl: async () => { throw new Error("Staging must not contact Linear"); }
}).then(result => process.stdout.write(JSON.stringify(result))).catch(error => {
  process.stderr.write(error.message);
  process.exitCode = 1;
});
`, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.JSONEq(t, string(expected), string(output))
}
