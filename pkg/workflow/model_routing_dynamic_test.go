//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type dynamicRoutingLockStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	If   string            `yaml:"if"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

type dynamicRoutingLock struct {
	Jobs map[string]struct {
		Outputs map[string]string        `yaml:"outputs"`
		Steps   []dynamicRoutingLockStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func findDynamicRoutingStep(t *testing.T, lock dynamicRoutingLock, job string, match func(dynamicRoutingLockStep) bool) dynamicRoutingLockStep {
	t.Helper()
	for _, step := range lock.Jobs[job].Steps {
		if match(step) {
			return step
		}
	}
	require.FailNow(t, "step not found in job "+job)
	return dynamicRoutingLockStep{}
}

// TestDynamicModelRoutingResolvesModelExpressionAtActivation runs the generated
// activation decision step for `model: ${{ inputs.level }}` with stubbed inputs,
// then evaluates aw_info, the agent and detection model env, the routing flag and
// mode, and the AWF config branch, proving one activation decision drives them all.
func TestDynamicModelRoutingResolvesModelExpressionAtActivation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	compiled := compileDefaultRoutingSource(t, `---
on:
  workflow_dispatch:
    inputs:
      level:
        type: string
permissions:
  contents: read
engine: copilot
model: ${{ inputs.level }}
safe-outputs:
  create-issue:
---
Report.
`)
	require.NoError(t, compiled.err)
	require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker, "dynamic routing is not the implicit default")
	var lock dynamicRoutingLock
	require.NoError(t, yaml.Unmarshal([]byte(compiled.lock), &lock))
	require.Equal(t, "${{ "+modelRoutingModeInActivation+" }}", lock.Jobs["activation"].Outputs[modelRoutingModeOutput])

	decision := findDynamicRoutingStep(t, lock, "activation", func(s dynamicRoutingLockStep) bool { return s.ID == modelRoutingDecisionStepID })
	info := findDynamicRoutingStep(t, lock, "activation", func(s dynamicRoutingLockStep) bool { return s.Env["GH_AW_INFO_MODEL_CONFIG"] != "" })
	conversation := findDynamicRoutingStep(t, lock, "agent", func(s dynamicRoutingLockStep) bool { return s.Name == "Prepare model-routing conversation" })
	execution := findDynamicRoutingStep(t, lock, "agent", func(s dynamicRoutingLockStep) bool {
		return strings.Contains(s.Run, `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`)
	})
	detection := findDynamicRoutingStep(t, lock, "detection", func(s dynamicRoutingLockStep) bool { return s.Env["COPILOT_MODEL"] != "" })

	configStart := strings.Index(execution.Run, `if [ "${GH_AW_MODEL_ROUTING:-}" = "1" ]; then`)
	require.GreaterOrEqual(t, configStart, 0)
	configEnd := strings.Index(execution.Run[configStart:], "\nfi\n")
	require.Positive(t, configEnd)
	configScript := execution.Run[configStart : configStart+configEnd+4]

	for _, tc := range []struct {
		name, level, wantMode, wantError string
	}{
		{name: "router value", level: "router/robust", wantMode: "robust"},
		{name: "auto router value", level: "router", wantMode: "auto"},
		{name: "model name", level: "gpt-5.6-luna"},
		{name: "empty", level: ""},
		{name: "router value with whitespace", level: " router/economy ", wantMode: "economy"},
		{name: "differently cased router value", level: "Router/Economy", wantError: "ERR_CONFIG"},
		{name: "router value with parameters", level: "router/economy?effort=high", wantError: "ERR_CONFIG"},
		{name: "unknown router value", level: "router/fast", wantError: "ERR_CONFIG"},
		{name: "line break", level: "gpt-5.6-luna\nmodel-routing=true", wantError: "ERR_CONFIG"},
		{name: "trailing line break", level: "router/robust\n", wantError: "ERR_CONFIG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contexts := map[string]string{"inputs.level": tc.level}
			dir := t.TempDir()
			outputFile := filepath.Join(dir, "output")
			runnerTemp := filepath.Join(dir, "runner")
			actionsDir := filepath.Join(runnerTemp, "gh-aw", "actions")
			require.NoError(t, os.MkdirAll(actionsDir, 0o755))
			routingScript, err := os.ReadFile("../../actions/setup/sh/resolve_model_routing.sh")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(actionsDir, "resolve_model_routing.sh"), routingScript, 0o755))
			cmd := exec.Command("bash", "-e", "-c", decision.Run)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_OUTPUT=" + outputFile, "RUNNER_TEMP=" + runnerTemp}
			for key, value := range decision.Env {
				cmd.Env = append(cmd.Env, key+"="+evaluateTestTemplate(t, value, contexts))
			}
			out, err := cmd.CombinedOutput()
			if tc.wantError != "" {
				require.Error(t, err, "an unsupported router value must stop activation")
				require.Contains(t, string(out), tc.wantError)
				return
			}
			require.NoError(t, err, string(out))
			wantRouting := tc.wantMode != ""

			outputs := map[string]string{}
			rawOutputs, err := os.ReadFile(outputFile)
			require.NoError(t, err)
			for line := range strings.SplitSeq(strings.TrimSpace(string(rawOutputs)), "\n") {
				key, value, _ := strings.Cut(line, "=")
				outputs[key] = value
			}
			require.Equal(t, map[bool]string{true: "true", false: "false"}[wantRouting], outputs[modelRoutingDecisionOutput])
			require.Equal(t, tc.wantMode, outputs[modelRoutingModeOutput])
			require.Equal(t, strings.TrimSpace(tc.level), outputs[modelSelectorOutput])

			activation := map[string]string{
				"steps." + modelRoutingDecisionStepID + ".outputs." + modelRoutingDecisionOutput: outputs[modelRoutingDecisionOutput],
				modelRoutingModeInActivation: outputs[modelRoutingModeOutput],
			}
			var modelConfig struct {
				Routing awInfoModelRouting `json:"routing"`
			}
			require.NoError(t, json.Unmarshal([]byte(evaluateTestTemplate(t, info.Env["GH_AW_INFO_MODEL_CONFIG"], activation)), &modelConfig))
			require.Equal(t, wantRouting, modelConfig.Routing.Enabled)
			require.Equal(t, tc.wantMode, modelConfig.Routing.Mode)
			if wantRouting {
				require.Equal(t, strings.TrimSpace(tc.level), modelConfig.Routing.Requested)
			}

			// Later jobs see only activation's outputs, not the inputs the expression read.
			downstream := map[string]string{
				"needs.activation.outputs." + modelRoutingDecisionOutput: outputs[modelRoutingDecisionOutput],
				modelRoutingModeDownstream:                               outputs[modelRoutingModeOutput],
				modelSelectorDownstream:                                  outputs[modelSelectorOutput],
			}
			require.Equal(t, wantRouting, evaluateTestCondition(t, conversation.If, downstream), "conversation step runs only when routed")
			wantModel := tc.level
			if wantRouting {
				wantModel = ""
			}
			require.Equal(t, wantModel, evaluateTestTemplate(t, execution.Env["COPILOT_MODEL"], downstream), "a routed run never passes the router value as a model")
			routingFlag := evaluateTestTemplate(t, execution.Env["GH_AW_MODEL_ROUTING"], downstream)
			require.Equal(t, wantRouting, routingFlag == "1")
			routingMode := evaluateTestTemplate(t, execution.Env[modelRoutingModeEnvVar], downstream)
			require.Equal(t, tc.wantMode, routingMode)

			detectionModel := evaluateTestTemplate(t, detection.Env["COPILOT_MODEL"], downstream)
			if wantRouting {
				require.Equal(t, "detection", detectionModel, "threat detection never inherits a router value")
			} else {
				require.Equal(t, tc.level, detectionModel, "threat detection still inherits a non-routed model")
			}

			cmd = exec.Command("bash", "-e", "-c", configScript)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "RUNNER_TEMP=" + runnerTemp, "GH_AW_MODEL_ROUTING=" + routingFlag,
				modelRoutingModeEnvVar + "=" + routingMode, "GH_AW_MAX_AI_CREDITS=0"}
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			configJSON, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw", "awf-config.json"))
			require.NoError(t, err)
			var awfConfig struct {
				APIProxy struct {
					Routing *AWFModelRoutingConfig `json:"routing"`
				} `json:"apiProxy"`
			}
			require.NoError(t, json.Unmarshal(configJSON, &awfConfig))
			require.Equal(t, wantRouting, awfConfig.APIProxy.Routing != nil, "AWF config routing must follow the activation decision")
			if wantRouting {
				require.Equal(t, AWFModelRoutingObjective{Goal: "cost", Mode: tc.wantMode}, awfConfig.APIProxy.Routing.Objective)
			}
		})
	}
}

// TestDynamicModelRoutingRejectsInvalidRuntimeMode proves the routed AWF config is
// not written with a mode outside the router's modes.
func TestDynamicModelRoutingRejectsInvalidRuntimeMode(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	compiled := compileDefaultRoutingSource(t, "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nmodel: ${{ vars.LEVEL }}\n---\nReport.\n")
	require.NoError(t, compiled.err)
	var lock dynamicRoutingLock
	require.NoError(t, yaml.Unmarshal([]byte(compiled.lock), &lock))
	execution := findDynamicRoutingStep(t, lock, "agent", func(s dynamicRoutingLockStep) bool {
		return strings.Contains(s.Run, `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`)
	})
	configStart := strings.Index(execution.Run, `if [ "${GH_AW_MODEL_ROUTING:-}" = "1" ]; then`)
	require.GreaterOrEqual(t, configStart, 0)
	configEnd := strings.Index(execution.Run[configStart:], "\nfi\n")
	require.Positive(t, configEnd)
	runnerTemp := filepath.Join(t.TempDir(), "runner")
	require.NoError(t, os.MkdirAll(filepath.Join(runnerTemp, "gh-aw"), 0o755))
	cmd := exec.Command("bash", "-e", "-c", execution.Run[configStart:configStart+configEnd+4])
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "RUNNER_TEMP=" + runnerTemp, "GH_AW_MODEL_ROUTING=1", modelRoutingModeEnvVar + "=fast", "GH_AW_MAX_AI_CREDITS=0"}
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "ERR_CONFIG")
	_, statErr := os.Stat(filepath.Join(runnerTemp, "gh-aw", "awf-config.json"))
	require.True(t, os.IsNotExist(statErr), "no AWF config is written for an invalid mode")
}

// TestDynamicModelRoutingEligibility covers which model expressions may resolve to
// a router value at run time and which keep today's non-routed compilation.
func TestDynamicModelRoutingEligibility(t *testing.T) {
	const header = "---\non:\n  workflow_dispatch:\n    inputs:\n      level:\n        type: string\npermissions:\n  contents: read\n"
	tests := []struct {
		name        string
		frontmatter string
		wantDynamic bool
	}{
		{name: "copilot input", frontmatter: "engine: copilot\nmodel: ${{ inputs.level || 'router/balanced' }}\n", wantDynamic: true},
		{name: "engine.model expression", frontmatter: "engine:\n  id: copilot\n  model: ${{ inputs.level }}\n", wantDynamic: true},
		{name: "claude on Copilot inference", frontmatter: "engine:\n  id: claude\n  model-provider: github\nmodel: ${{ inputs.level }}\n", wantDynamic: true},
		{name: "codex on Copilot inference", frontmatter: "engine:\n  id: codex\n  model-provider: github\nmodel: ${{ inputs.level }}\n", wantDynamic: true},
		{name: "expression mixed with text", frontmatter: "engine: copilot\nmodel: ${{ inputs.level }}?effort=high\n"},
		{name: "agent step output", frontmatter: "engine: copilot\nsteps:\n  - id: pick\n    run: echo \"model=gpt-5.6-luna\" >> \"$GITHUB_OUTPUT\"\nmodel: ${{ steps.pick.outputs.model }}\n"},
		{name: "custom job output", frontmatter: "engine: copilot\njobs:\n  choose:\n    runs-on: ubuntu-latest\n    outputs:\n      model: ${{ steps.c.outputs.model }}\n    steps:\n      - id: c\n        run: echo \"model=gpt-5.6-luna\" >> \"$GITHUB_OUTPUT\"\nmodel: ${{ needs.choose.outputs.model }}\n"},
		{name: "literal model", frontmatter: "engine: copilot\nmodel: gpt-5.6-luna\n"},
		{name: "routing disabled", frontmatter: "engine:\n  id: copilot\n  model-routing: false\nmodel: ${{ inputs.level }}\n"},
		{name: "fallback models", frontmatter: "engine:\n  id: copilot\n  fallback-models: [gpt-5.6-luna]\nmodel: ${{ inputs.level }}\n"},
		{name: "firewall disabled", frontmatter: "engine: copilot\nmodel: ${{ inputs.level }}\nfeatures:\n  dangerously-disable-sandbox-agent: true\nsandbox:\n  agent: false\nstrict: false\n"},
		{name: "pi has no runtime model variables", frontmatter: "engine: pi\nmodel: ${{ inputs.level }}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, header+tt.frontmatter+"---\nReport.\n")
			require.NoError(t, compiled.err)
			require.Equal(t, tt.wantDynamic, isModelRoutingDynamic(compiled.data))
			require.Equal(t, tt.wantDynamic, strings.Contains(compiled.lock, "GH_AW_MODEL_SELECTOR:"))
			require.Equal(t, tt.wantDynamic, strings.Contains(compiled.lock, awfRuntimeDynamicRoutingObjective) ||
				strings.Contains(compiled.lock, strings.ReplaceAll(awfRuntimeDynamicRoutingObjective, `"`, `\"`)))
			require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker)
		})
	}
}

// TestActivationResolvesModelExpression covers which model expressions the
// activation job resolves to the value later jobs would see.
func TestActivationResolvesModelExpression(t *testing.T) {
	experiments := map[string][]string{"level": {"router/economy", "router/robust"}}
	for expression, want := range map[string]bool{
		"inputs.level || 'router/balanced'": true,
		"case(contains(github.event.pull_request.labels.*.name, 'security'), 'router/robust', 'router/economy')": true,
		"case(fromJSON(github.run_attempt) > 1, 'router/robust', vars.LEVEL)":                                    true,
		"needs.pre_activation.outputs.level":                                                                     true,
		"needs.activation.outputs.level":                                                                         true,
		"NEEDS.Activation.outputs.level":                                                                         true,
		"needs.activation.outputs.other":                                                                         false,
		"steps.pick.outputs.level":                                                                               false,
		"needs.choose.outputs.level":                                                                             false,
		"github.job == 'agent' && 'router/robust' || 'router/economy'":                                           false,
		"contains(github.*, 'agent') && 'router/robust' || 'router/economy'":                                     false,
		"github.event_path":                                                                                      false,
		"toJSON(github)":                                                                                         false,
		"hashFiles('go.mod') != '' && 'router/robust' || 'router/economy'":                                       false,
		"'steps.pick.outputs.level'":                                                                             true,
	} {
		parsed, err := actionslanguageservice.ParseSyntax(expression)
		require.NoError(t, err, expression)
		require.Equal(t, want, activationResolvesModelExpression(parsed, experiments), expression)
	}
}

// TestDynamicModelRoutingGuardsEngineModelEnv proves Claude and Codex pass the
// model expression only on the non-routed path.
func TestDynamicModelRoutingGuardsEngineModelEnv(t *testing.T) {
	for _, tt := range []struct {
		engine, modelEnv string
	}{
		{engine: "claude", modelEnv: "ANTHROPIC_MODEL"},
		{engine: "codex", modelEnv: "GH_AW_MODEL_AGENT_CODEX"},
	} {
		t.Run(tt.engine, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, "---\non:\n  workflow_dispatch:\n    inputs:\n      level:\n        type: string\npermissions:\n  contents: read\nengine:\n  id: "+tt.engine+"\n  model-provider: github\nmodel: ${{ inputs.level }}\n---\nReport.\n")
			require.NoError(t, compiled.err)
			require.True(t, isModelRoutingDynamic(compiled.data))
			agent := extractJobSection(compiled.lock, "agent")
			require.Contains(t, agent, tt.modelEnv+": ${{ needs.activation.outputs.model-routing != 'true' && (needs.activation.outputs.model-selector) || '' }}")
			require.Contains(t, agent, modelRoutingModeEnvVar+": ${{ "+modelRoutingModeDownstream+" }}")
		})
	}
}

// TestDynamicModelRoutingWithExperimentVariants proves router values can be
// experiment variants of the main agent's model: activation reads the selected
// variant from the pick-experiment step, and later jobs read the resolved value,
// which an experiment named model cannot shadow.
func TestDynamicModelRoutingWithExperimentVariants(t *testing.T) {
	compiled := compileDefaultRoutingSource(t, `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
model: ${{ experiments.model }}
experiments:
  model: [router/economy, router/robust, gpt-5.6-luna]
---
Report.
`)
	require.NoError(t, compiled.err)
	require.True(t, isModelRoutingDynamic(compiled.data))
	var lock dynamicRoutingLock
	require.NoError(t, yaml.Unmarshal([]byte(compiled.lock), &lock))
	outputs := lock.Jobs["activation"].Outputs
	require.Equal(t, "${{ steps.pick-experiment.outputs.model }}", outputs["model"])
	require.Equal(t, "${{ "+modelSelectorInActivation+" }}", outputs[modelSelectorOutput])
	activation := extractJobSection(compiled.lock, "activation")
	require.Contains(t, activation, `GH_AW_MODEL_SELECTOR: "${{ steps.pick-experiment.outputs.model }}"`)
	require.Less(t, strings.Index(activation, "id: pick-experiment"), strings.Index(activation, "id: "+modelRoutingDecisionStepID),
		"the experiment is picked before the routing decision")
	require.Contains(t, extractJobSection(compiled.lock, "agent"),
		"COPILOT_MODEL: ${{ needs.activation.outputs.model-routing != 'true' && (needs.activation.outputs.model-selector) || '' }}")
}

// TestDynamicModelRoutingFromPreActivationOutput proves a model chosen by a
// pre-activation step reaches later jobs through the activation job, which is
// the only one of them that needs pre_activation.
func TestDynamicModelRoutingFromPreActivationOutput(t *testing.T) {
	compiled := compileDefaultRoutingSource(t, `---
on:
  workflow_dispatch:
  steps:
    - id: pick
      run: echo "level=router/robust" >> "$GITHUB_OUTPUT"
jobs:
  pre-activation:
    outputs:
      level: ${{ steps.pick.outputs.level }}
permissions:
  contents: read
engine: copilot
model: ${{ needs.pre_activation.outputs.level }}
safe-outputs:
  create-issue:
---
Report.
`)
	require.NoError(t, compiled.err)
	require.True(t, isModelRoutingDynamic(compiled.data))
	require.NotContains(t, compiled.stderr, "needs-consistency")
	require.Contains(t, extractJobSection(compiled.lock, "activation"), `GH_AW_MODEL_SELECTOR: "${{ needs.pre_activation.outputs.level }}"`)
	for _, job := range []string{"agent", "detection", "safe_outputs", "conclusion"} {
		section := extractJobSection(compiled.lock, job)
		require.NotEmpty(t, section, job)
		require.NotContains(t, section, "needs.pre_activation", job)
	}
}
