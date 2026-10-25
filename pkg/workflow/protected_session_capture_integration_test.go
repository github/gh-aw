//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This needs a Linux host with AWF, Docker, Node with execve, and Python.
func TestProtectedSessionCaptureAWFDescriptorIsolation(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("GH_AW_NATIVE_CAPTURE_E2E") != "1" {
		t.Skip("requires a Linux host, AWF, Docker, and GH_AW_NATIVE_CAPTURE_E2E=1")
	}
	awf, err := exec.LookPath("awf")
	require.NoError(t, err, "install the pinned AWF release before running this regression")
	platform, err := exec.CommandContext(t.Context(), "docker", "info", "--format", "{{.OSType}}").Output()
	require.NoError(t, err)
	require.Equal(t, "linux", strings.TrimSpace(string(platform)))
	runnerTemp := os.Getenv("RUNNER_TEMP")
	require.NotEmpty(t, runnerTemp, "RUNNER_TEMP must be outside agent-visible mounts")
	root, err := os.MkdirTemp(runnerTemp, "native-capture-isolation-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	workspace := filepath.Join(root, "workspace")
	scripts := filepath.Join(workspace, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(scripts, 0o700))

	// Tools know the pipe identity, but must not be able to reopen it through
	// any same-UID ancestor, including an accidentally retained shell or Node.
	tool := `
import os, sys
target = sys.argv[1]
pid = os.getppid()
for access in (os.O_RDONLY, os.O_WRONLY):
    try:
        fd = os.open(f"/proc/{pid}/fd/2", access | os.O_NONBLOCK)
    except PermissionError:
        continue
    else:
        os.close(fd)
        raise RuntimeError("recorder descriptor is accessible")
while pid > 0:
    base = f"/proc/{pid}"
    with open(base + "/status") as status:
        parent = next(int(line.split()[1]) for line in status if line.startswith("PPid:"))
    if os.stat(base).st_uid == os.getuid():
        try:
            descriptors = os.listdir(base + "/fd")
        except PermissionError:
            descriptors = []
        for descriptor in descriptors:
            file = base + "/fd/" + descriptor
            try:
                if os.readlink(file) != target:
                    continue
                for access in (os.O_RDONLY, os.O_WRONLY):
                    try:
                        fd = os.open(file, access | os.O_NONBLOCK)
                    except PermissionError:
                        continue
                    else:
                        os.close(fd)
                        raise RuntimeError("same-UID ancestor exposes recorder pipe")
            except FileNotFoundError:
                continue
    pid = parent
print("descriptor isolation checked")
`
	recorder := `
import ctypes, os, subprocess, sys
if ctypes.CDLL(None).prctl(4, 0, 0, 0, 0) != 0:
    raise RuntimeError("cannot isolate recorder")
pipe = os.readlink("/proc/self/fd/2")
os.write(2, b"recorder-before-tool\n")
tool = ` + string(mustJSONMarshal(t, tool)) + `
result = subprocess.run([sys.executable, "-c", tool, pipe], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, close_fds=True)
sys.stdout.buffer.write(result.stdout)
if result.returncode:
    raise SystemExit(result.returncode)
os.write(2, b"recorder-after-tool\n")
`
	recorderJSON := mustJSONMarshal(t, recorder)
	harness := `const { execve, env } = require("node:process");
if (typeof execve !== "function") throw new Error("Node execve is required");
execve("/usr/bin/python3", ["/usr/bin/python3", "-c", ` + string(recorderJSON) + `], env);
`
	def := &EngineDefinition{
		ID: "isolation-test", DisplayName: "Isolation test",
		Behaviors: &EngineBehaviorDefinition{
			Execution:     &EngineExecutionDefinition{CommandName: "unused", ProtectedSessionCapture: true},
			HarnessScript: harness,
			LogParser:     `function parseLog() { return { markdown: "", logEntries: [] }; }`,
		},
	}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(scripts, engine.harnessScriptFilename()), []byte(harness), 0o600))
	command := "export RUNNER_TEMP=" + shellEscapeArg(workspace) + "; " +
		engine.buildBehaviorDefinedEngineCommand(def.Behaviors.Execution, &WorkflowData{EngineConfig: &EngineConfig{}})
	require.Contains(t, command, `exec "$GH_AW_NODE_EXEC"`)
	capture := filepath.Join(root, "capture")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, awf, "--container-runtime", "docker",
		"--container-workdir", workspace, "--mount", workspace+":"+workspace+":ro",
		"--work-dir", filepath.Join(root, "awf-work"),
		"--capture-command-output", capture, "--", WrapCommandInShell(command))
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "PWD=" + workspace, "GITHUB_WORKSPACE=" + workspace, "RUNNER_TEMP=" + runnerTemp}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "AWF isolation regression failed: %s", output)
	var metadata struct {
		Status    string `json:"status"`
		Truncated bool   `json:"truncated"`
		Command   struct {
			ExitCode int `json:"exitCode"`
		} `json:"command"`
		Attempts []struct {
			Outcome string `json:"outcome"`
			Stderr  struct {
				File string `json:"file"`
			} `json:"stderr"`
		} `json:"attempts"`
	}
	data, err := os.ReadFile(filepath.Join(capture, "metadata.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &metadata))
	require.Equal(t, "complete", metadata.Status)
	require.False(t, metadata.Truncated)
	require.Zero(t, metadata.Command.ExitCode)
	var stderr []byte
	for _, attempt := range metadata.Attempts {
		if attempt.Outcome == "not_started" {
			continue
		}
		require.Equal(t, filepath.Base(attempt.Stderr.File), attempt.Stderr.File)
		stderr, err = os.ReadFile(filepath.Join(capture, attempt.Stderr.File))
		require.NoError(t, err)
	}
	require.Contains(t, string(stderr), "recorder-before-tool\n")
	require.Contains(t, string(stderr), "recorder-after-tool\n")
	require.False(t, strings.Contains(string(stderr), "descriptor isolation checked"), "tool output must stay on stdout")
}

func mustJSONMarshal(t *testing.T, value string) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
