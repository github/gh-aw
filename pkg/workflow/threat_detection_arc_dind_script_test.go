//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the production staging, command, reporting shim, host conclusion and
// downstream policy reader. Only AWF itself and its engine harness are simulated.
func TestBundledThreatDetectionRoundTrip(t *testing.T) {
	setup, err := filepath.Abs("../../actions/setup")
	require.NoError(t, err)
	conclude := stageConclusionScript(t)
	for _, topology := range []string{"hosted", "arc"} {
		t.Run(topology, func(t *testing.T) {
			root := t.TempDir()
			runner := filepath.Join(root, "shared temp")
			actions := filepath.Join(runner, "gh-aw/actions")
			require.NoError(t, os.MkdirAll(actions, 0755))
			files, err := filepath.Glob(filepath.Join(setup, "js/threat_*"))
			require.NoError(t, err)
			for _, file := range append(files, filepath.Join(setup, "js/detect_threats.cjs"), filepath.Join(setup, "js/actions_secret_masking.cjs"), filepath.Join(setup, "js/otlp_header_helpers.cjs")) {
				data, err := os.ReadFile(file)
				require.NoError(t, err)
				writeConclusionInput(t, filepath.Join(actions, filepath.Base(file)), string(data))
			}
			// This harness receives exactly the launch argv and permission/eligibility
			// environment that the compiler-selected harness would see inside AWF.
			writeConclusionInput(t, filepath.Join(actions, "copilot_harness.cjs"), `
const fs = require("node:fs"), {execFileSync} = require("node:child_process");
const e = process.env;
fs.writeFileSync(e.OBSERVED, JSON.stringify({
  engine: process.argv[2], model: e.COPILOT_MODEL,
  injection: e.THREAT_DETECTION_ELIGIBLE_PROMPT_INJECTION,
  secret: e.THREAT_DETECTION_ELIGIBLE_SECRET_LEAK, patch: e.THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH
}));
if (e.SCENARIO === "failure") process.exit(7);
execFileSync("threat_detection_result", ["--prompt-injection=false", "--secret-leak=false",
  "--malicious-patch=" + e.THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH, "--reason=private reason"]);
`)
			host := filepath.Join(root, "runner private")
			resultDir := filepath.Join(host, "threat-detection")
			inputDir := filepath.Join(host, "threat-detection-inputs")
			stageScript := filepath.Join(setup, "sh/stage_threat_detection_arc_dind.sh")
			env := []string{"RUNNER_TEMP=" + runner, "COPILOT_MODEL=fixture-model", "OBSERVED=" + filepath.Join(root, "observed")}
			run := func(extra []string, command string, args ...string) (string, int) {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), command, args...)
				cmd.Env = append(append(os.Environ(), env...), extra...)
				out, err := cmd.CombinedOutput()
				if err == nil {
					return string(out), 0
				}
				var failure *exec.ExitError
				require.ErrorAs(t, err, &failure, "%s", out)
				return string(out), failure.ExitCode()
			}
			out, code := run(nil, "node", filepath.Join(setup, "js/stage_threat_detection_runtime.cjs"), "copilot")
			require.Zero(t, code, out)
			for _, scenario := range []string{"empty-output", "empty-items", "memory", "unstaged-patch", "failure", "skipped"} {
				t.Run(scenario, func(t *testing.T) {
					write := func(name, content string) {
						file := filepath.Join(host, name)
						require.NoError(t, os.MkdirAll(filepath.Dir(file), 0755))
						writeConclusionInput(t, file, content)
					}
					write("aw-prompts/prompt.txt", "static")
					write("aw-prompts/prompt-template.txt", "static")
					write("aw-prompts/prompt-import-tree.json", "{}")
					write("agent_output.json", "")
					if scenario == "empty-items" {
						write("agent_output.json", `{"items":[]}`)
					}
					write("threat-detection/detection_result.json", cleanDetectionVerdict)
					write("threat-detection/aw-stale.patch", "do not stage")
					write("threat-detection-inputs/aw-stale.patch", "do not stage")
					require.NoError(t, os.RemoveAll(filepath.Join(host, "comment-memory")))
					if scenario == "memory" {
						write("comment-memory/context.md", "comment")
					}
					// Run exactly the compiled cleanup body against private fixture paths.
					data := &WorkflowData{}
					if topology == "arc" {
						data.RunnerConfig = &RunnerConfig{Topology: "arc-dind"}
					}
					reset := strings.Join(buildResetDetectionResultsStep(data), "")
					_, reset, ok := strings.Cut(reset, "        run: |\n")
					require.True(t, ok)
					reset = strings.ReplaceAll(reset, "/tmp/gh-aw/", shellEscapeArg(host)+"/")
					reset = strings.ReplaceAll(reset, `"${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh" reset`, shellEscapeArg(stageScript)+" reset "+shellEscapeArg(resultDir))
					out, code := run(nil, "bash", "-ec", reset)
					require.Zero(t, code, out)
					dest := filepath.Join(resultDir, "detection_result.json")
					require.NoFileExists(t, dest)
					if scenario == "skipped" {
						got := runConclusion(t, "bash", []string{conclude, dest}, "RUN_DETECTION=false")
						require.Equal(t, "skipped", got.outputs["conclusion"])
						return
					}
					out, code = run(nil, "bash", filepath.Join(setup, "sh/prepare_threat_detection_files.sh"), host)
					require.Zero(t, code, out)
					if topology == "arc" {
						out, code = run(nil, "bash", stageScript, "stage", resultDir)
						require.Zero(t, code, out)
					}
					cmd := buildThreatDetectCommand("true", &ThreatDetectionConfig{}, topology == "arc")
					cmd = strings.ReplaceAll(cmd, "/tmp/gh-aw/threat-detection/detection_result.json", shellEscapeArg(dest))
					cmd = strings.ReplaceAll(cmd, "/tmp/gh-aw/threat-detection-inputs", shellEscapeArg(inputDir))
					extra := []string{"SCENARIO=" + scenario, "HAS_PATCH=" + strconv.FormatBool(scenario == "unstaged-patch"), "GH_AW_DETECTION_CONTINUE_ON_ERROR=true"}
					out, code = run(extra, "bash", "-ec", cmd)
					require.Contains(t, out, "[copilot-harness] Detection harness started")
					write("threat-detection/detection.log", out)
					if topology == "arc" {
						collect, status := run(nil, "bash", stageScript, "collect", resultDir)
						require.Zero(t, status, collect)
					}
					observed, err := os.ReadFile(filepath.Join(root, "observed"))
					require.NoError(t, err)
					var got map[string]string
					require.NoError(t, json.Unmarshal(observed, &got))
					require.Equal(t, "copilot", got["engine"])
					require.Equal(t, "fixture-model", got["model"])
					require.Equal(t, scenario == "memory", got["injection"] == "true")
					require.Equal(t, scenario == "unstaged-patch", got["patch"] == "true")
					require.Equal(t, scenario == "memory" || scenario == "unstaged-patch" || scenario == "empty-items", got["secret"] == "true")
					conclusion := runConclusion(t, "bash", []string{conclude, dest}, "GH_AW_DETECTION_CONTINUE_ON_ERROR=false")
					if scenario == "failure" {
						require.Equal(t, 2, code, out)
						require.NoFileExists(t, dest)
						require.Equal(t, "agent_failure", conclusion.outputs["reason"])
						return
					}
					want := "success"
					if scenario == "unstaged-patch" {
						want = "failure"
						require.Equal(t, 1, code, out)
						require.Equal(t, "malicious_patch", conclusion.outputs["threat_kinds"])
						require.Contains(t, conclusion.log, "HAS_PATCH=true")
						public, err := os.ReadFile(dest)
						require.NoError(t, err)
						require.NotContains(t, string(public), "private reason")
						require.Contains(t, conclusion.log, "private reason")
					} else {
						require.Zero(t, code, out)
					}
					require.Equal(t, want, conclusion.outputs["conclusion"])
					gate, status := run(nil, "node", "-e", `console.log(require(process.argv[1]).getEffectiveDetectionConclusion(process.argv[2], process.argv[3]))`, filepath.Join(setup, "js/threat_detection_warning.cjs"), conclusion.outputs["conclusion"], conclusion.outputs["reason"])
					require.Zero(t, status, gate)
					require.Equal(t, want, strings.TrimSpace(gate))
				})
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
	cmd.Env = append(os.Environ(), "RUNNER_TEMP="+root)
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
