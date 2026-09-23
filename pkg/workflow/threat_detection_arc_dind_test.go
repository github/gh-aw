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

func TestCompileArcDindThreatDetectionStaging(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex"} {
		t.Run(engine, func(t *testing.T) {
			var standardConclusion map[string]any
			for _, tc := range []struct {
				name, detectionRunner string
				arc                   bool
			}{
				{name: "standard"},
				{name: "arc with default detection runner", arc: true},
				{name: "arc detection runner", arc: true, detectionRunner: "    runs-on: arc-scale-set\n"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := ""
					if tc.arc {
						runner = "runs-on: arc-scale-set\nrunner:\n  topology: arc-dind\n"
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
					reset, resetStep := find("Reset ARC/DinD threat detection staging")
					prepare, _ := find("Prepare threat detection files")
					install, installation := find("Install threat-detect binary")
					execute, execution := find("Execute threat detection with AWF")
					collect, collection := find("Collect ARC/DinD threat detection results")
					upload, _ := find("Upload threat detection artifact")
					_, conclusion := find("Conclude threat detection")
					require.NotNil(t, conclusion)
					if !tc.arc {
						standardConclusion = conclusion
						require.Equal(t, -1, reset)
						require.NotContains(t, installation["run"], "--rootless")
						require.Equal(t, -1, collect)
						require.NotContains(t, execution["run"], "stage_threat_detection_arc_dind.sh")
						require.Contains(t, execution["run"], "--output /tmp/gh-aw/threat-detection/detection_result.json /tmp/gh-aw/threat-detection")
						return
					}
					require.Equal(t, standardConclusion, conclusion, "conclusion step must remain unchanged")
					require.GreaterOrEqual(t, reset, 0)
					require.Less(t, reset, prepare)
					require.Less(t, reset, install)
					require.Equal(t, "always()", resetStep["if"], "cleanup must not depend on installation or detection being enabled")
					require.Contains(t, resetStep["run"], "stage_threat_detection_arc_dind.sh\" reset")
					require.Contains(t, installation["run"], "--rootless")
					require.Greater(t, execute, prepare)
					require.Greater(t, execute, install)
					require.Greater(t, collect, execute)
					require.Greater(t, upload, collect)
					require.Equal(t, "always() && steps.threat_detect_install.outcome == 'success' && steps.detection_agentic_execution.outcome != 'skipped'", collection["if"])
					require.Contains(t, execution["if"], "steps.threat_detect_install.outcome == 'success'")
					require.Contains(t, execution["if"], "steps.detection_staging_reset.outcome == 'success'")
					env := execution["env"].(map[string]any)
					require.Equal(t, "${{ steps.threat_detect_install.outputs.binary-path }}", env["THREAT_DETECT_BINARY"])
					run := execution["run"].(string)
					require.Contains(t, run, `bash "${RUNNER_TEMP}/gh-aw/actions/stage_threat_detection_arc_dind.sh" stage`)
					require.Contains(t, run, `export PATH="${RUNNER_TEMP}/gh-aw/bin:$PATH"`)
					require.Contains(t, run, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
					require.Contains(t, run, `--mount "${RUNNER_TEMP}/gh-aw/threat-detection:${RUNNER_TEMP}/gh-aw/threat-detection:rw"`)
					require.NotContains(t, run, "--mount /tmp/gh-aw/threat-detection:")
					require.Contains(t, run, `--output "${RUNNER_TEMP}/gh-aw/threat-detection/detection_result.json" "${RUNNER_TEMP}/gh-aw/threat-detection"`)
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
