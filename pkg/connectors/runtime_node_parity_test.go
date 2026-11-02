package connectors

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func connectorNodeRuntime(t *testing.T) (string, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for bundled connector runtime parity")
	}
	runtime, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup", "js", "connector_runtime.cjs"))
	require.NoError(t, err)
	return node, runtime
}

func TestConnectorNodeDecodesGoPhaseSnapshots(t *testing.T) {
	node, runtime := connectorNodeRuntime(t)
	full := fixturePlan(t, "https://api.example.com")
	for _, phase := range []string{"agent", "context", "safe-output"} {
		t.Run(phase, func(t *testing.T) {
			plan, err := full.ForPhase(phase)
			require.NoError(t, err)
			encoded, err := plan.Encode()
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), node, "-e", `
const { decodePlan } = require(process.argv[1]);
const plan = decodePlan(require("node:fs").readFileSync(0, "utf8"));
process.stdout.write(plan.digest);
`, runtime)
			command.Stdin = strings.NewReader(encoded)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Equal(t, plan.Digest, string(output))
		})
	}
}

func TestConnectorNodeStagesGoSafeOutputSnapshot(t *testing.T) {
	node, runtime := connectorNodeRuntime(t)
	full := fixturePlan(t, "https://api.example.com")
	plan, err := full.ForPhase("safe-output")
	require.NoError(t, err)
	args := map[string]any{"issue": "ISSUE-1", "text": "Untrusted <text> & ${{ data }}"}
	request, err := plan.PrepareSafeOutput("add-comment", args)
	require.NoError(t, err)
	expected, err := json.Marshal(map[string]any{"staged": true, "request": request})
	require.NoError(t, err)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	input, err := json.Marshal(args)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, runtime, "invoke", "safe-output", "add-comment", "--stage")
	command.Env = append(os.Environ(), "GH_AW_CONNECTOR_PLAN="+encoded)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.JSONEq(t, string(expected), string(output))
}
