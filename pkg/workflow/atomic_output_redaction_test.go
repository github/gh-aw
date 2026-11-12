//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertAtomicOutputCleanup(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "agent"))
	require.NoError(t, err)
	assert.Empty(t, entries, "private configuration must be removed on every exit")
}

func TestAtomicHarnessOutputRedaction(t *testing.T) {
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit-%d", exitCode), func(t *testing.T) {
			requestHeaders := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestHeaders <- r.Header.Clone()
				_, _ = w.Write([]byte("MCP authenticated"))
			}))
			defer server.Close()
			cmd, dir := atomicEnvironmentFixture(t, "node")
			config := map[string]any{
				"autoEnableCodemode": false,
				"mcpServers": map[string]any{
					"fixture": map[string]any{
						"type": "http", "url": server.URL, "exposure": "direct",
						"headers": map[string]string{
							"aUtHoRiZaTiOn": "bEaReR gateway-test-token",
							"X-Key":         "header-other-secret",
							"X-Escaped":     `escaped"secret\value`,
							"X-Padded":      " padded-header-secret ",
							"X-Overlap":     "gateway-test-token-tail",
							"X-Empty":       "",
						},
					},
					"short": map[string]any{"headers": map[string]string{"authorization": "Bearer qz"}},
				},
			}
			encoded, err := json.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.json"), encoded, 0o600))
			probe := filepath.Join(dir, "probe.cjs")
			executionArgs := loadAtomicSample(t).Behaviors.Execution.Args
			expectedArgs, err := json.Marshal(append(append([]string{}, executionArgs...), "--provider", "awf-proxy", "--model", "fixture-model"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(probe, []byte(fmt.Sprintf(`
const fs = require("fs"), path = require("path"), assert = require("assert/strict");
(async () => {
  const home = process.env.ATOMIC_CODING_AGENT_DIR;
  const file = path.join(home, "mcp.json");
  const privateConfig = fs.readFileSync(file, "utf8");
  const mcp = JSON.parse(privateConfig);
  assert.deepEqual(mcp, JSON.parse(fs.readFileSync(process.env.GH_AW_MCP_CONFIG, "utf8")));
  assert.equal(fs.statSync(home).mode & 0o777, 0o700);
  assert.equal(fs.statSync(file).mode & 0o777, 0o600);
  const headers = mcp.mcpServers.fixture.headers;
  const response = await fetch(mcp.mcpServers.fixture.url, { headers });
  assert.equal(await response.text(), "MCP authenticated");
  assert.equal(fs.readFileSync(0, "utf8"), "--help $(not-a-command)\nAtomic fixture prompt");
  assert.deepEqual(process.argv.slice(2), %s);
  const payload = [
    "private MCP and stdin/args verified", privateConfig,
    ...Object.values(headers), "gateway-test-token", "qz",
    JSON.stringify(headers["X-Escaped"]),
    '"bad\t" ' + JSON.stringify(headers["X-Escaped"]),
    JSON.stringify("gateway-test-token").replace("g", "\\u0067"),
    "unrelated ordinary output"
  ].join("\n") + "\n";
  process.stdout.write("stdout\n" + payload);
  process.stderr.write("stderr\n" + payload);
  process.exitCode = %d;
})().catch(error => { console.error(error); process.exitCode = 99; });
`, string(expectedArgs), exitCode)), 0o600))
			cmd.Args = append(cmd.Args, probe)
			cmd.Args = append(cmd.Args, executionArgs...)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			stdout, err := cmd.Output()
			if exitCode == 0 {
				require.NoError(t, err, "%s", stderr.String())
			} else {
				var failure *exec.ExitError
				require.ErrorAs(t, err, &failure)
				assert.Equal(t, exitCode, failure.ExitCode())
				assert.Contains(t, stderr.String(), "Atomic exited with code 7")
			}
			for _, output := range []string{string(stdout), stderr.String()} {
				assert.Contains(t, output, "private MCP and stdin/args verified")
				assert.Contains(t, output, "unrelated ordinary output")
				assert.Contains(t, output, "[REDACTED]")
				for _, secret := range []string{
					"gateway-test-token", "header-other-secret", "padded-header-secret",
					`escaped"secret\value`, `escaped\"secret\\value`, `\u0067ateway-test-token`, "qz",
				} {
					assert.NotContains(t, output, secret)
				}
				assert.NotContains(t, output, "[REDACTED]-tail", "overlapping values must be masked together")
			}
			select {
			case headers := <-requestHeaders:
				assert.Equal(t, "bEaReR gateway-test-token", headers.Get("Authorization"))
				assert.Equal(t, "header-other-secret", headers.Get("X-Key"))
				assert.Equal(t, `escaped"secret\value`, headers.Get("X-Escaped"))
			default:
				t.Fatal("private MCP headers must remain usable for authentication")
			}
			assertAtomicOutputCleanup(t, dir)
		})
	}
}

func TestAtomicHarnessOutputCaptureFailures(t *testing.T) {
	t.Run("complete output above default Node buffer limit", func(t *testing.T) {
		cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `
const payload = "x".repeat(2 * 1024 * 1024) + "\ncomplete output\n";
process.stdout.write(payload);
process.stderr.write(payload);
`, "--")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		diagnostic := stderr.String()
		if len(diagnostic) > 500 {
			diagnostic = diagnostic[len(diagnostic)-500:]
		}
		require.NoError(t, err, "%s", diagnostic)
		expected := strings.Repeat("x", 2*1024*1024) + "\ncomplete output\n"
		assert.Equal(t, expected, string(output))
		assert.True(t, strings.HasSuffix(stderr.String(), expected), "stderr output must be complete")
		assertAtomicOutputCleanup(t, dir)
	})
	t.Run("overflow", func(t *testing.T) {
		cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `
const fs = require("fs");
fs.writeSync(1, "normal stdout before overflow\n");
fs.writeSync(2, "normal stderr before overflow\n");
const payload = Buffer.alloc(8192, "x");
for (let i = 0; i < 100; i++) fs.writeSync(i % 2 ? 1 : 2, payload);
`, "--")
		harness := filepath.Join(dir, "atomic_harness.cjs")
		source, err := os.ReadFile(harness)
		require.NoError(t, err)
		require.Contains(t, string(source), "maxBuffer: 64 * 1024 * 1024")
		require.NoError(t, os.WriteFile(harness, []byte(strings.ReplaceAll(string(source), "maxBuffer: 64 * 1024 * 1024", "maxBuffer: 4096")), 0o600))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, stderr.String(), "capture limit; incomplete output withheld")
		assert.NotContains(t, stderr.String(), "normal stderr")
		assert.NotContains(t, stderr.String(), "xxxx")
		assertAtomicOutputCleanup(t, dir)
	})
	t.Run("spawn error diagnostics", func(t *testing.T) {
		cmd, dir := atomicEnvironmentFixture(t, "missing-command-header-secret")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{"fixture":{"headers":{"Authorization":"Bearer header-secret"}}}}`), 0o600))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, stderr.String(), "ENOENT")
		assert.Contains(t, stderr.String(), "missing-command-[REDACTED]")
		assert.NotContains(t, stderr.String(), "header-secret")
		assertAtomicOutputCleanup(t, dir)
	})
	t.Run("redactor failure", func(t *testing.T) {
		cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `
process.stdout.write("header-secret");
process.stderr.write("header-secret");
`, "--")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "add_mask_redaction.cjs"), []byte(`
module.exports = { redactArtifactMaskedValues: content => {
  if (content.includes("header-secret")) throw new Error("header-secret");
  return content;
} };
`), 0o600))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, stderr.String(), "[atomic-harness] Atomic output redaction failed; output withheld\n")
		assert.NotContains(t, stderr.String(), "header-secret")
		assertAtomicOutputCleanup(t, dir)
	})
	t.Run("signal", func(t *testing.T) {
		cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `process.kill(process.pid, "SIGTERM")`, "--")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		_, err := cmd.Output()
		require.Error(t, err)
		assert.Contains(t, stderr.String(), "Atomic terminated by signal SIGTERM")
		assertAtomicOutputCleanup(t, dir)
	})
}
