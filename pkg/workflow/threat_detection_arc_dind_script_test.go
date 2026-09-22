//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArcDindThreatDetectionFileRoundTrip(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "verdict"
		if fail {
			name = "engine failure"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			runnerTemp := filepath.Join(root, "shared temp")
			hostDir := filepath.Join(root, "runner private", "threat-detection")
			binDir := filepath.Join(root, "installed bin")
			stagedDir := filepath.Join(runnerTemp, "gh-aw", "threat-detection")
			for _, dir := range []string{hostDir, binDir, stagedDir} {
				require.NoError(t, os.MkdirAll(dir, 0700))
			}
			write := func(path, content string) {
				t.Helper()
				require.NoError(t, os.WriteFile(path, []byte(content), 0700))
			}
			write(filepath.Join(hostDir, "agent_output.json"), "input")
			write(filepath.Join(hostDir, "detection.log"), "host log")
			write(filepath.Join(hostDir, "execution.json"), "host evidence")
			write(filepath.Join(hostDir, "detection_result.json"), "stale downloaded verdict")
			write(filepath.Join(stagedDir, "detection_result.json"), "stale staged verdict")
			write(filepath.Join(binDir, "threat-detect"), `#!/bin/bash
set -euo pipefail
[[ "$1" = --engine && "$3" = --output && "$#" = 5 ]]
[[ "$(<"$5/agent_output.json")" = input ]]
printf 'usage' > "$5/detection_usage.json"
if [[ "${FAIL_DETECTION:-}" = true ]]; then
  echo 'engine failed'
  exit 37
fi
printf '{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}' > "$4"
printf 'full result' > "$5/detection_result_full.json"
printf 'usage events' > "$5/detection_usage.jsonl"
`)
			script := filepath.Join("..", "..", "actions", "setup", "sh", "stage_threat_detection_arc_dind.sh")
			runScript := func(mode string) {
				t.Helper()
				cmd := exec.Command("bash", script, mode, hostDir)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+runnerTemp, "PATH="+binDir+":"+os.Getenv("PATH"))
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", out)
			}
			runScript("stage")
			require.NoFileExists(t, filepath.Join(hostDir, "detection_result.json"))
			require.NoFileExists(t, filepath.Join(stagedDir, "detection_result.json"))
			// Remove the original installation: only the daemon-visible copy may run.
			require.NoError(t, os.Remove(filepath.Join(binDir, "threat-detect")))
			command := `export PATH="${RUNNER_TEMP}/gh-aw/bin:$PATH"; ` + buildThreatDetectCommand("true", "copilot", &ThreatDetectionConfig{}, true)
			cmd := exec.Command("bash", "-c", command)
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+runnerTemp)
			if fail {
				cmd.Env = append(cmd.Env, "FAIL_DETECTION=true")
			}
			out, err := cmd.CombinedOutput()
			if fail {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, 37, exit.ExitCode())
				require.Contains(t, string(out), "engine failed")
			} else {
				require.NoError(t, err, "%s", out)
			}
			write(filepath.Join(stagedDir, "detection.log"), "must not overwrite host log")
			write(filepath.Join(stagedDir, "execution.json"), "must not overwrite host evidence")
			runScript("collect")
			assertContent := func(name, expected string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(hostDir, name))
				require.NoError(t, err)
				require.Equal(t, expected, string(content))
			}
			assertContent("detection.log", "host log")
			assertContent("execution.json", "host evidence")
			assertContent("detection_usage.json", "usage")
			if fail {
				require.NoFileExists(t, filepath.Join(hostDir, "detection_result.json"))
			} else {
				assertContent("detection_result.json", `{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}`)
				assertContent("detection_result_full.json", "full result")
				assertContent("detection_usage.jsonl", "usage events")
			}
		})
	}
}
