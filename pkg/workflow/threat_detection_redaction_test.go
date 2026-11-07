//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectionRedactionOrderingAndGates(t *testing.T) {
	for _, external := range []bool{false, true} {
		for _, topology := range []RunnerTopology{"", RunnerTopologyArcDind} {
			t.Run(map[bool]string{false: "inline", true: "external"}[external]+"/"+string(topology), func(t *testing.T) {
				data := &WorkflowData{
					AI:               "copilot",
					Features:         map[string]any{string(constants.GHAWDetectionFeatureFlag): external},
					Env:              "env:\n  PRIVATE: ${{ secrets.ARBITRARY_SECRET }}",
					FrontmatterYAML:  "${{ secrets.FRONTMATTER_ONLY_SECRET }}",
					MarkdownContent:  "${{ secrets.PROMPT_SECRET }}",
					ImportedMarkdown: "${{ secrets.IMPORTED_PROMPT_SECRET }}",
					SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
						PostSteps: []any{map[string]any{"name": "Late detection output", "run": "echo output"}},
					}},
					SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}},
				}
				if topology != "" {
					data.RunnerConfig = &RunnerConfig{Topology: topology}
				}
				content := strings.Join(NewCompiler().buildDetectionJobSteps(data), "")
				var parsed struct {
					Steps []struct {
						Name            string            `yaml:"name"`
						ID              string            `yaml:"id"`
						If              string            `yaml:"if"`
						ContinueOnError bool              `yaml:"continue-on-error"`
						Env             map[string]string `yaml:"env"`
						With            map[string]any    `yaml:"with"`
					} `yaml:"steps"`
				}
				require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+content), &parsed))
				positions := map[string]int{}
				outcomes := map[string]string{
					"steps.redact_detection_logs.outcome":         "success",
					"steps.redact_detection_artifacts.outcome":    "success",
					"steps.detection_guard.outputs.run_detection": "true",
				}
				consumers := 0
				for i, step := range parsed.Steps {
					positions[step.Name] = i
					if step.ID == "redact_detection_logs" || step.ID == "redact_detection_artifacts" {
						node, err := ParseExpression(step.If)
						require.NoError(t, err)
						assert.True(t, evaluateDetectionRedactionGate(t, node, outcomes))
						outcomes["steps.detection_guard.outputs.run_detection"] = "false"
						assert.False(t, evaluateDetectionRedactionGate(t, node, outcomes), "no-output runs must not walk artifact files")
						outcomes["steps.detection_guard.outputs.run_detection"] = "true"
						assert.False(t, step.ContinueOnError, "redaction failures must fail the job even in warn mode")
						assert.Contains(t, step.Env["GH_AW_SECRET_NAMES"], "ARBITRARY_SECRET")
						assert.NotContains(t, step.Env["GH_AW_SECRET_NAMES"], "PROMPT_SECRET")
						assert.NotContains(t, step.Env["GH_AW_SECRET_NAMES"], "FRONTMATTER_ONLY_SECRET")
						assert.Equal(t, "${{ secrets.ARBITRARY_SECRET }}", step.Env["SECRET_ARBITRARY_SECRET"])
						continue
					}
					switch step.Name {
					case "Parse threat detection token usage for step summary", "Echo detection step summary", "Render detection log", "Upload threat detection log", "Upload threat detection artifact", "Parse and conclude threat detection", "Conclude threat detection":
					case "Upload skipped detection execution evidence":
						assert.Equal(t, detectionExecutionEvidencePath, step.With["path"], "skip uploads must exclude every unredacted payload")
					default:
						continue
					}
					consumers++
					if step.Name == "Upload threat detection log" || step.Name == "Upload threat detection artifact" {
						assert.Contains(t, strings.Split(step.With["path"].(string), "\n"), "/tmp/gh-aw/threat-detection/detection_harness_retry.json")
						assert.Equal(t, "ignore", step.With["if-no-files-found"])
					}
					node, err := ParseExpression(step.If)
					require.NoError(t, err)
					skipUpload := step.Name == "Upload skipped detection execution evidence"
					assert.Equal(t, !skipUpload, evaluateDetectionRedactionGate(t, node, outcomes), step.Name)
					isConclusion := step.ID == "detection_conclusion"
					outcomes["steps.detection_guard.outputs.run_detection"] = "false"
					outcomes["steps.redact_detection_logs.outcome"] = "skipped"
					outcomes["steps.redact_detection_artifacts.outcome"] = "skipped"
					assert.Equal(t, isConclusion || skipUpload, evaluateDetectionRedactionGate(t, node, outcomes), "%s on no-output runs", step.Name)
					outcomes["steps.detection_guard.outputs.run_detection"] = "true"
					outcomes["steps.redact_detection_logs.outcome"] = "success"
					outcomes["steps.redact_detection_artifacts.outcome"] = "success"
					if skipUpload {
						continue
					}
					for property := range outcomes {
						if !strings.HasSuffix(property, ".outcome") || (step.ID == "parse_detection_token_usage" && property == "steps.redact_detection_artifacts.outcome") {
							continue
						}
						for _, outcome := range []string{"failure", "skipped", "cancelled", ""} {
							outcomes[property] = outcome
							assert.False(t, evaluateDetectionRedactionGate(t, node, outcomes), "%s must be gated on %s=%s", step.Name, property, outcome)
						}
						outcomes[property] = "success"
					}
				}
				assert.GreaterOrEqual(t, consumers, 4)
				order := []string{"Copy detection firewall logs", "Late detection output", "Redact secrets in detection logs", "Parse threat detection token usage for step summary", "Redact secrets in detection artifacts", "Render detection log"}
				if external && topology == "arc-dind" {
					order = append([]string{"Collect ARC/DinD threat detection results"}, order...)
				}
				for i, name := range order {
					require.Contains(t, positions, name)
					if i > 0 {
						assert.Less(t, positions[order[i-1]], positions[name])
					}
				}
				assert.Contains(t, content, "/redact_secrets.cjs")
				assert.Contains(t, content, "/detection_mask_state.cjs")
				assert.Contains(t, content, "], persistDetectionMasks });")
				assert.Contains(t, content, `additionalDirectories: ["/tmp/gh-aw/threat-detection/sandbox/firewall"]`)
			})
		}
	}
}

func evaluateDetectionRedactionGate(t *testing.T, node ConditionNode, outcomes map[string]string) bool {
	t.Helper()
	switch node := node.(type) {
	case *OrNode:
		return evaluateDetectionRedactionGate(t, node.Left, outcomes) || evaluateDetectionRedactionGate(t, node.Right, outcomes)
	case *AndNode:
		return evaluateDetectionRedactionGate(t, node.Left, outcomes) && evaluateDetectionRedactionGate(t, node.Right, outcomes)
	default:
		return evaluateRedactionUploadGate(t, node, outcomes)
	}
}

func TestDetectionRedactionEffectiveSecretInputs(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprintf("external=%t", external), func(t *testing.T) {
			dir := t.TempDir()
			shared := `---
env:
  IMPORTED_ENV: ${{ secrets.IMPORTED_ENV_SECRET }}
safe-outputs:
  threat-detection:
    engine:
      id: copilot
      env:
        COPILOT_GITHUB_TOKEN: ${{ secrets.DETECTION_ENGINE_SECRET }}
    steps:
      - name: Imported detection pre-step
        env:
          PRE_KEY: ${{ secrets.DETECTION_PRE_SECRET }}
        run: echo pre
    post-steps:
      - name: Imported detection post-step
        env:
          POST_KEY: ${{ secrets.DETECTION_POST_SECRET }}
        run: echo post
---
Imported instructions.
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
			source := fmt.Sprintf(`---
on: push
strict: false
engine:
  id: claude
  env:
    ANTHROPIC_API_KEY: ${{ secrets.MAIN_PROVIDER_ONLY_SECRET }}
env:
  INHERITED_KEY: ${{ secrets.INHERITED_SECRET }}
imports:
  - shared.md
safe-outputs:
  github-token: ${{ secrets.SAFE_OUTPUT_ONLY_SECRET }}
  create-issue:
features:
  gh-aw-detection: %t
---
Review documentation.
`, external)
			path := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			compiled, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
			require.NoError(t, err)
			detection := extractJobSection(string(compiled), "detection")
			require.NotEmpty(t, detection)
			for _, name := range []string{"Redact secrets in detection logs", "Redact secrets in detection artifacts"} {
				step := extractWorkflowStepByName(t, detection, name)
				for _, secret := range []string{"DETECTION_ENGINE_SECRET", "DETECTION_PRE_SECRET", "DETECTION_POST_SECRET", "INHERITED_SECRET", "IMPORTED_ENV_SECRET"} {
					assert.Contains(t, step, "SECRET_"+secret+": ${{ secrets."+secret+" }}")
				}
			}
			for _, secret := range []string{"MAIN_PROVIDER_ONLY_SECRET", "SAFE_OUTPUT_ONLY_SECRET"} {
				assert.NotContains(t, detection, secret)
			}
		})
	}
}

func TestDetectionRedactionSameEngineInheritedSecret(t *testing.T) {
	for _, external := range []bool{false, true} {
		data := &WorkflowData{
			AI: "copilot",
			EngineConfig: &EngineConfig{
				ID:  "copilot",
				Env: map[string]string{"COPILOT_GITHUB_TOKEN": "${{ secrets.INHERITED_ENGINE_SECRET }}"},
			},
			Features:    map[string]any{string(constants.GHAWDetectionFeatureFlag): external},
			SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
		}
		steps := strings.Join(NewCompiler().buildDetectionJobSteps(data), "")
		for _, name := range []string{"Redact secrets in detection logs", "Redact secrets in detection artifacts"} {
			step := extractWorkflowStepByName(t, steps, name)
			assert.Contains(t, step, "SECRET_INHERITED_ENGINE_SECRET: ${{ secrets.INHERITED_ENGINE_SECRET }}")
		}
	}
}

func TestDetectionRedactionInlineSkipConclusion(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to verify the inline no-output conclusion")
	}
	// Loading is allowed, but a no-output conclusion must not read any payload.
	script := `
const { main } = require('../../actions/setup/js/parse_threat_detection_results.cjs');
const assert = require('node:assert/strict');
require('node:fs').readFileSync = () => { throw new Error('unexpected payload read'); };
const outputs = {};
global.core = {
  info() {}, exportVariable() {},
  error(message) { throw new Error(message); },
  setFailed(message) { throw new Error(message); },
  setOutput(name, value) { outputs[name] = value; },
};
main().then(() => {
  assert.equal(outputs.conclusion, 'skipped');
  assert.equal(outputs.success, 'true');
  assert.equal(outputs.reason, '');
}).catch(error => { console.error(error); process.exitCode = 1; });
`
	for _, warnMode := range []string{"false", "true"} {
		t.Run("warn="+warnMode, func(t *testing.T) {
			cmd := exec.Command(node, "-e", script)
			cmd.Env = append(os.Environ(), "RUN_DETECTION=false", "GH_AW_DETECTION_CONTINUE_ON_ERROR="+warnMode)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
