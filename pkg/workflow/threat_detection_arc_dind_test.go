//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCompileBundledDetectionFeature(t *testing.T) {
	for _, flag := range []string{"absent", "true", "false"} {
		t.Run(flag, func(t *testing.T) {
			feature := ""
			if flag != "absent" {
				feature = "features:\n  gh-aw-detection: " + flag + "\n"
			}
			source := filepath.Join(t.TempDir(), "detection.md")
			require.NoError(t, os.WriteFile(source, []byte("---\non: push\nengine: copilot\nsafe-outputs:\n  noop:\n"+feature+"---\nAssess.\n"), 0600))
			require.NoError(t, NewCompiler().CompileWorkflow(source))
			lock, err := os.ReadFile(strings.TrimSuffix(source, ".md") + ".lock.yml")
			require.NoError(t, err)
			detection := extractJobSection(string(lock), "detection")
			require.NotEmpty(t, detection)
			require.NotContains(t, detection, "install_threat_detect_binary")
			require.NotContains(t, detection, "threat_detect_install")
			require.Equal(t, flag != "false", strings.Contains(detection, "/actions/detect_threats.cjs"))
			require.Equal(t, flag != "false", strings.Contains(detection, "conclude_threat_detection.sh"))
			require.Equal(t, flag == "false", strings.Contains(detection, "parse_threat_detection_results.cjs"))
		})
	}
}

func TestCompileArcDindThreatDetectionStaging(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex"} {
		t.Run(engine, func(t *testing.T) {

			for _, tc := range []struct {
				name, detectionRunner, installation string
				arc                                 bool
			}{
				{name: "standard"},
				{name: "native rootless", installation: "rootless"},
				{name: "native privileged", installation: "privileged"},
				{name: "arc with default detection runner", arc: true},
				{name: "arc detection runner", arc: true, detectionRunner: "    runs-on: arc-scale-set\n"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := ""
					if tc.arc {
						runner = "runs-on: arc-scale-set\nrunner:\n  topology: arc-dind\n"
					} else if tc.installation != "" {
						runner = "runs-on: linux64-selfhosted\nrunner:\n  installation: " + tc.installation + "\n"
					}
					source := fmt.Sprintf("---\non: workflow_dispatch\nengine: %s\n%ssafe-outputs:\n  create-issue:\n  threat-detection:\n%s    continue-on-error: true\nfeatures:\n  gh-aw-detection: true\n---\nReport a finding.\n", engine, runner, tc.detectionRunner)
					path := filepath.Join(t.TempDir(), "detection.md")
					require.NoError(t, os.WriteFile(path, []byte(source), 0600))
					require.NoError(t, NewCompiler().CompileWorkflow(path))
					compiled, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
					require.NoError(t, err)
					var workflow struct {
						Jobs map[string]struct {
							Steps []map[string]any `yaml:"steps"`
						} `yaml:"jobs"`
					}
					require.NoError(t, yaml.Unmarshal(compiled, &workflow))
					steps := workflow.Jobs["detection"].Steps
					find := func(name string) (int, map[string]any) {
						for i, step := range steps {
							if step["name"] == name {
								return i, step
							}
						}
						return -1, nil
					}
					reset, resetStep := find("Reset threat detection staging")
					prepare, _ := find("Prepare threat detection files")
					install, _ := find("Install threat-detect binary")
					require.Equal(t, -1, install)
					execute, execution := find("Execute threat detection with AWF")
					collect, collection := find("Collect ARC/DinD threat detection results")
					upload, _ := find("Upload threat detection artifact")
					_, conclusion := find("Conclude threat detection")
					require.NotNil(t, conclusion)
					executionEnv := execution["env"].(map[string]any)
					if engine == "copilot" {
						inputDir := "/tmp/gh-aw/threat-detection-inputs"
						if tc.arc {
							inputDir = "${{ runner.temp }}/gh-aw/threat-detection-inputs"
						}
						require.Equal(t, inputDir, executionEnv["GH_AW_DETECTION_INPUT_DIR"])
						require.Equal(t, "detection", executionEnv["GH_AW_PHASE"])
					} else {
						require.NotContains(t, executionEnv, "GH_AW_DETECTION_INPUT_DIR")
					}
					rootless := tc.arc || tc.installation == "rootless"
					if engine == "copilot" {
						_, copilotInstall := find("Install GitHub Copilot CLI")
						require.NotNil(t, copilotInstall)
						require.Equal(t, rootless, strings.Contains(copilotInstall["run"].(string), "--rootless"))
						foundAgentInstaller := false
						for _, step := range workflow.Jobs["agent"].Steps {
							if step["name"] == "Install GitHub Copilot CLI" {
								foundAgentInstaller = true
								require.Equal(t, rootless, strings.Contains(step["run"].(string), "--rootless"))
							}
						}
						require.True(t, foundAgentInstaller)
					}
					if !tc.arc {
						require.Less(t, reset, prepare)
						require.GreaterOrEqual(t, reset, 0)
						require.Equal(t, -1, collect)
						require.NotContains(t, execution["run"], "stage_threat_detection_arc_dind.sh")
						require.Contains(t, execution["run"], "--output /tmp/gh-aw/threat-detection/detection_result.json /tmp/gh-aw/threat-detection-inputs")
						if rootless {
							require.NotContains(t, string(compiled), `"topology":"arc-dind"`)
							require.NotContains(t, string(compiled), "Redirect tool cache to shared volume")
						}
						return
					}
					require.Contains(t, conclusion["env"], "DETECTION_COLLECTION_OUTCOME")
					require.GreaterOrEqual(t, reset, 0)
					require.Less(t, reset, prepare)
					require.Equal(t, "always()", resetStep["if"], "cleanup must not depend on installation or detection being enabled")
					require.Contains(t, resetStep["run"], "stage_threat_detection_arc_dind.sh\" reset")
					require.Greater(t, execute, prepare)
					require.Greater(t, collect, execute)
					require.Greater(t, upload, collect)
					require.Contains(t, collection["if"], "steps.detection_agentic_execution.outcome == 'failure'")
					require.Contains(t, execution["if"], "steps.detection_staging_reset.outcome == 'success'")
					run := execution["run"].(string)
					require.Contains(t, run, `bash "${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh" stage`)
					require.Contains(t, run, `export PATH="${RUNNER_TEMP}/gh-aw/bin:$PATH"`)
					require.Contains(t, run, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
					require.Contains(t, run, `--mount "${RUNNER_TEMP}/gh-aw/threat-detection:${RUNNER_TEMP}/gh-aw/threat-detection:rw"`)
					require.NotContains(t, run, "--mount /tmp/gh-aw/threat-detection:")
					require.Contains(t, run, `--output "${RUNNER_TEMP}/gh-aw/threat-detection/detection_result.json" "${RUNNER_TEMP}/gh-aw/threat-detection-inputs"`)
					if engine == "codex" {
						_, config := find("Prepare Codex config for threat-detect")
						require.Contains(t, config["run"], `"/tmp/gh-aw/threat-detection/codex-home/config.toml"`)
						require.Contains(t, run, `export CODEX_HOME="${RUNNER_TEMP}/gh-aw/threat-detection/codex-home"`)
					}
				})
			}
		})
	}
}
