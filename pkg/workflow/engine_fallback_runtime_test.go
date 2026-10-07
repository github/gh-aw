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

func TestEngineFallbackRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for harness runtime tests")
	}
	for _, id := range []string{"copilot", "claude", "codex"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			stub := filepath.Join(dir, "engine.cjs")
			require.NoError(t, os.WriteFile(stub, []byte(`
const fs = require("fs");
const args = process.argv.slice(2);
const model = args[args.indexOf("--model") + 1];
fs.appendFileSync(process.env.FALLBACK_ATTEMPTS_LOG, JSON.stringify({ model, args, infoModel: process.env.GH_AW_INFO_MODEL }) + "\n");
if (model !== "last") {
  console.log(JSON.stringify({ type: "session.error", data: { status: 503, message: "503 Service Unavailable" } }));
  process.exitCode = 1;
} else {
  fs.appendFileSync(process.env.GH_AW_SAFE_OUTPUTS, '{"type":"noop","message":"complete"}\n');
  console.log("completed");
}
`), 0o600))
			prompt := filepath.Join(dir, "prompt.txt")
			require.NoError(t, os.WriteFile(prompt, []byte("private task instructions"), 0o600))
			infoPath := filepath.Join(dir, "aw_info.json")
			require.NoError(t, os.WriteFile(infoPath, []byte(`{"model":"primary","workflow_name":"fallback-test"}`), 0o600))
			safePath := filepath.Join(dir, "safe-outputs.jsonl")
			staged := "{\"type\":\"report_incomplete\",\"message\":\"existing evidence\"}\n"
			require.NoError(t, os.WriteFile(safePath, []byte(staged), 0o600))
			attemptsPath := filepath.Join(dir, "attempts.jsonl")
			harness, err := filepath.Abs(filepath.Join("../../actions/setup/js", id+"_harness.cjs"))
			require.NoError(t, err)
			cmd := exec.Command(node, harness, node, stub, "--model", "primary", "--prompt-file", prompt)
			cmd.Env = append(os.Environ(),
				"GH_AW_SKIP_REFLECT=true", "AWF_REFLECT_ENABLED=0", "COPILOT_SDK_URI=",
				"GH_AW_FALLBACK_MODELS=[\"secondary\",\"last\"]", "COPILOT_MODEL=primary",
				"GH_AW_HARNESS_MAX_RETRIES=1", "GH_AW_HARNESS_INITIAL_DELAY_MS=1", "GH_AW_HARNESS_MAX_DELAY_MS=1",
				"CODEX_API_KEY=test-key", "ANTHROPIC_API_KEY=test-key",
				"GH_AW_TMP_DIR="+dir, "GITHUB_WORKSPACE="+dir, "GH_AW_ENGINE_CWD="+dir,
				"GITHUB_ENV=", "GH_AW_PHASE=agent", "GH_AW_LLM_PROVIDER=",
				"FALLBACK_ATTEMPTS_LOG="+attemptsPath, "GH_AW_SAFE_OUTPUTS="+safePath)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			require.NotContains(t, string(output), "private task instructions")
			attempts, err := os.ReadFile(attemptsPath)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(attempts)), "\n")
			require.Len(t, lines, 5, string(output))
			for i, expected := range []string{"primary", "primary", "secondary", "secondary", "last"} {
				var attempt struct {
					Model     string   `json:"model"`
					Args      []string `json:"args"`
					InfoModel string   `json:"infoModel"`
				}
				require.NoError(t, json.Unmarshal([]byte(lines[i]), &attempt))
				require.Equal(t, expected, attempt.Model)
				if i == 2 || i == 4 {
					require.NotContains(t, attempt.Args, "--continue")
					require.NotContains(t, attempt.Args, "--resume")
					require.Equal(t, expected, attempt.InfoModel)
				}
			}

			info, err := os.ReadFile(infoPath)
			require.NoError(t, err)
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(info, &metadata))
			require.Equal(t, "last", metadata["model"])
			require.Equal(t, "fallback-test", metadata["workflow_name"])
			safeOutputs, err := os.ReadFile(safePath)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(safeOutputs), staged))
			require.Contains(t, string(safeOutputs), `"message":"complete"`)
		})
	}
}
