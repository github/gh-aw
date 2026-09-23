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
			write(filepath.Join(stagedDir, ".stale-input"), "must not reach the detector")
			require.NoError(t, os.MkdirAll(filepath.Join(stagedDir, "aw-prompts"), 0700))
			write(filepath.Join(stagedDir, "aw-prompts", "prompt-template.txt"), "stale optional prompt")
			require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "codex-home"), 0700))
			write(filepath.Join(hostDir, "codex-home", "config.toml"), "provider config")
			write(filepath.Join(binDir, "threat-detect"), `#!/bin/bash
set -euo pipefail
[[ "$1" = --engine && "$3" = --output && "$#" = 5 ]]
[[ "$(<"$5/agent_output.json")" = input ]]
[[ ! -e "$5/.stale-input" && ! -e "$5/aw-prompts/prompt-template.txt" ]]
[[ "$(<"$5/codex-home/config.toml")" = 'provider config' ]]
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
				cmd.Env = append(cmd.Env, "THREAT_DETECT_BINARY="+filepath.Join(binDir, "threat-detect"))
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

func TestArcDindThreatDetectionResetWithoutExecution(t *testing.T) {
	root := t.TempDir()
	hostDir := filepath.Join(root, "host")
	stagedDir := filepath.Join(root, "gh-aw", "threat-detection")
	for _, dir := range []string{hostDir, stagedDir} {
		require.NoError(t, os.MkdirAll(dir, 0700))
		for _, name := range []string{"detection_result.json", "detection_result_full.json", "detection_usage.json", "detection_usage.jsonl"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0600))
		}
	}
	// No binary is installed and neither stage nor collect runs: this models
	// failed/cancelled installation and skipped detection execution.
	cmd := exec.Command("bash", "../../actions/setup/sh/stage_threat_detection_arc_dind.sh", "reset", hostDir)
	cmd.Env = append(os.Environ(), "RUNNER_TEMP="+root, "THREAT_DETECT_BINARY=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, dir := range []string{hostDir, stagedDir} {
		files, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Empty(t, files, "no inherited verdict or usage may survive a skipped execution")
	}
}

func TestArcDindThreatDetectionRejectsUnsafeDirectories(t *testing.T) {
	for _, mode := range []string{"reset", "stage", "collect"} {
		for _, target := range []string{"host", "staged", "same directory"} {
			t.Run(mode+"/"+target, func(t *testing.T) {
				root := t.TempDir()
				hostDir := filepath.Join(root, "host")
				stagedDir := filepath.Join(root, "gh-aw", "threat-detection")
				outside := filepath.Join(root, "outside")
				require.NoError(t, os.MkdirAll(outside, 0700))
				require.NoError(t, os.MkdirAll(filepath.Dir(stagedDir), 0700))
				marker := filepath.Join(outside, "detection_result.json")
				require.NoError(t, os.WriteFile(marker, []byte("untouched"), 0600))
				switch target {
				case "host":
					require.NoError(t, os.Symlink(outside, hostDir))
				case "staged":
					require.NoError(t, os.Symlink(outside, stagedDir))
				default:
					hostDir = stagedDir
				}
				cmd := exec.Command("bash", "../../actions/setup/sh/stage_threat_detection_arc_dind.sh", mode, hostDir)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+root)
				out, err := cmd.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(out), "separate, non-symlink directories")
				content, err := os.ReadFile(marker)
				require.NoError(t, err)
				require.Equal(t, "untouched", string(content))
			})
		}
	}
}

func TestArcDindThreatDetectionUsesVerifiedBinary(t *testing.T) {
	for _, state := range []string{"verified", "missing", "not executable", "symlink", "unset"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			hostDir := filepath.Join(root, "host")
			pathDir := filepath.Join(root, "path")
			for _, dir := range []string{hostDir, pathDir} {
				require.NoError(t, os.MkdirAll(dir, 0700))
			}
			shadow := filepath.Join(pathDir, "threat-detect")
			require.NoError(t, os.WriteFile(shadow, []byte("#!/bin/bash\nexit 99\n"), 0700))
			verified := filepath.Join(root, "verified detector")
			payload := []byte("#!/bin/bash\nprintf verified\n")
			switch state {
			case "verified":
				require.NoError(t, os.WriteFile(verified, payload, 0700))
			case "not executable":
				require.NoError(t, os.WriteFile(verified, payload, 0600))
			case "symlink":
				require.NoError(t, os.Symlink(shadow, verified))
			case "unset":
				verified = ""
			}
			cmd := exec.Command("bash", "../../actions/setup/sh/stage_threat_detection_arc_dind.sh", "stage", hostDir)
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+root, "THREAT_DETECT_BINARY="+verified, "PATH="+pathDir+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			stagedBinary := filepath.Join(root, "gh-aw", "bin", "threat-detect")
			if state != "verified" {
				require.Error(t, err, "%s", out)
				require.NoFileExists(t, stagedBinary)
				return
			}
			require.NoError(t, err, "%s", out)
			out, err = exec.Command(stagedBinary).CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Equal(t, "verified", string(out), "PATH shadow must not be staged")
		})
	}
}
