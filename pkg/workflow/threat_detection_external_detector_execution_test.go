//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExternalDetectorExecutionStepIncludesThreatDetectionContext(t *testing.T) {
	compiler := NewCompiler()

	t.Run("configured prompt", func(t *testing.T) {
		data := &WorkflowData{
			AI:          "copilot",
			Name:        "Threat Detection Test",
			Description: "Checks generated workflows for threats.",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					ContinueOnError: boolPtr(false),
					Prompt:          "Treat credentials as sensitive.",
				},
			},
		}

		steps := strings.Join(compiler.buildExternalDetectorExecutionStep(data), "")
		for _, want := range []string{
			`GH_AW_DETECTION_CONTINUE_ON_ERROR: "false"`,
			"HAS_PATCH: ${{ needs.agent.outputs.has_patch }}",
			`WORKFLOW_NAME: "Threat Detection Test"`,
			`WORKFLOW_DESCRIPTION: "Checks generated workflows for threats."`,
			`CUSTOM_PROMPT: "Treat credentials as sensitive."`,
		} {
			if !strings.Contains(steps, want) {
				t.Errorf("expected external detector execution step to contain %q:\n%s", want, steps)
			}
		}
	})

	t.Run("unset prompt", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := strings.Join(compiler.buildExternalDetectorExecutionStep(data), "")
		if strings.Contains(steps, "CUSTOM_PROMPT:") {
			t.Errorf("expected external detector execution step to omit CUSTOM_PROMPT when unset:\n%s", steps)
		}
	})
}

func containsExternalDetectorCopilotPathPrefix(steps string) bool {
	return strings.Contains(steps, `export PATH="${RUNNER_TEMP}/gh-aw/bin:$PATH"`) ||
		strings.Contains(steps, `export PATH=\"${RUNNER_TEMP}/gh-aw/bin:$PATH\"`)
}

func TestBuildExternalDetectorPathSetup(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name              string
		data              *WorkflowData
		engineID          string
		wantHostSetup     bool
		wantCommandPrefix bool
	}{
		{
			name: "copilot on standard topology stages binary and prepends PATH",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			engineID:          "copilot",
			wantHostSetup:     true,
			wantCommandPrefix: true,
		},
		{
			name: "copilot on arc-dind prepends PATH without host staging",
			data: &WorkflowData{
				AI: "copilot",
				RunnerConfig: &RunnerConfig{
					Topology: RunnerTopologyArcDind,
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			engineID:          "copilot",
			wantHostSetup:     false,
			wantCommandPrefix: true,
		},
		{
			name: "copilot custom command still stages standard binary",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID:      "copilot",
							Command: "/opt/custom/copilot",
						},
					},
				},
			},
			engineID:          "copilot",
			wantHostSetup:     true,
			wantCommandPrefix: true,
		},
		{
			name: "non-copilot engine skips setup",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			engineID:          "claude",
			wantHostSetup:     false,
			wantCommandPrefix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compiler.buildExternalDetectorPathSetup(buildExternalDetectorWorkflowData(tt.data, tt.engineID), tt.engineID)
			if hasHostSetup := strings.Contains(got.hostSetup, "GH_AW_COPILOT_SRC"); hasHostSetup != tt.wantHostSetup {
				t.Errorf("host setup presence = %v, want %v; setup:\n%s", hasHostSetup, tt.wantHostSetup, got.hostSetup)
			}
			if hasCommandPrefix := containsExternalDetectorCopilotPathPrefix(got.commandPrefix); hasCommandPrefix != tt.wantCommandPrefix {
				t.Errorf("command prefix presence = %v, want %v; prefix:\n%s", hasCommandPrefix, tt.wantCommandPrefix, got.commandPrefix)
			}
		})
	}
}

func TestExternalDetectorExecutionStepStagesInstalledCopilotBinary(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := strings.Join(compiler.buildExternalDetectorExecutionStep(data), "")
	if !containsExternalDetectorCopilotPathPrefix(steps) {
		t.Errorf("expected external detector execution to prepend staged Copilot bin dir to PATH;\ngot:\n%s", steps)
	}
	if !strings.Contains(steps, "GH_AW_COPILOT_SRC") {
		t.Errorf("expected external detector execution to stage installed Copilot binary;\ngot:\n%s", steps)
	}
	if !strings.Contains(steps, `cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"`) {
		t.Errorf("expected external detector execution to copy installed Copilot binary;\ngot:\n%s", steps)
	}
}

func TestExternalDetectorExecutionStepStagesInstalledCopilotBinaryForCustomCommand(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID:      "copilot",
					Command: "/opt/custom/copilot",
				},
			},
		},
	}

	steps := strings.Join(compiler.buildExternalDetectorExecutionStep(data), "")
	if !containsExternalDetectorCopilotPathPrefix(steps) {
		t.Errorf("expected external detector execution to prepend standard Copilot bin dir for custom command;\ngot:\n%s", steps)
	}
	if !strings.Contains(steps, "GH_AW_COPILOT_SRC") {
		t.Errorf("expected external detector execution to stage standard Copilot binary for custom command;\ngot:\n%s", steps)
	}
}

func TestBuildExternalDetectorExecutionStepDefaultsHarnessMaxRetriesToZero(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildExternalDetectorExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "GH_AW_HARNESS_MAX_RETRIES: 0") {
		t.Fatalf("expected external detector execution to default GH_AW_HARNESS_MAX_RETRIES to 0, got:\n%s", allSteps)
	}
}

func TestBuildExternalDetectorExecutionStepRespectsExplicitHarnessMaxRetries(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		EngineConfig: &EngineConfig{
			ID:                "codex",
			HarnessMaxRetries: "5",
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildExternalDetectorExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "GH_AW_HARNESS_MAX_RETRIES: 5") {
		t.Fatalf("expected external detector execution to honor explicit GH_AW_HARNESS_MAX_RETRIES=5, got:\n%s", allSteps)
	}
}

func TestBuildExternalDetectorExecutionStepPropagatesRunnerTopology(t *testing.T) {
	compiler := NewCompiler()

	t.Run("arc-dind uses daemon-visible AWF paths", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if !strings.Contains(allSteps, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`) {
			t.Errorf("expected arc-dind external detector execution to mount ${RUNNER_TEMP}/gh-aw read-only;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `--mount "${RUNNER_TEMP}/gh-aw/home:${RUNNER_TEMP}/gh-aw/home:rw"`) {
			t.Errorf("expected arc-dind external detector execution to mount ${RUNNER_TEMP}/gh-aw/home read-write;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `\"proxyLogsDir\":\"${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs\"`) {
			t.Errorf("expected arc-dind external detector execution to rewrite proxyLogsDir under ${RUNNER_TEMP}/gh-aw;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `\"auditDir\":\"${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit\"`) {
			t.Errorf("expected arc-dind external detector execution to rewrite auditDir under ${RUNNER_TEMP}/gh-aw;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, "export HOME=${RUNNER_TEMP}/gh-aw/home") {
			t.Errorf("expected arc-dind external detector execution to export HOME under ${RUNNER_TEMP}/gh-aw/home;\ngot:\n%s", allSteps)
		}
		if !containsExternalDetectorCopilotPathPrefix(allSteps) {
			t.Errorf("expected arc-dind external detector execution to prepend staged Copilot bin dir to PATH;\ngot:\n%s", allSteps)
		}
		if strings.Contains(allSteps, "GH_AW_COPILOT_SRC") {
			t.Errorf("did not expect arc-dind external detector execution to stage Copilot binary on the host;\ngot:\n%s", allSteps)
		}
	})

	t.Run("non-arc-dind keeps standard AWF paths", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if strings.Contains(allSteps, `--mount "${RUNNER_TEMP}/gh-aw/home:${RUNNER_TEMP}/gh-aw/home:rw"`) {
			t.Errorf("did not expect non-arc-dind external detector execution to mount ${RUNNER_TEMP}/gh-aw/home read-write;\ngot:\n%s", allSteps)
		}
		if strings.Contains(allSteps, "export HOME=${RUNNER_TEMP}/gh-aw/home") {
			t.Errorf("did not expect non-arc-dind external detector execution to export HOME under ${RUNNER_TEMP}/gh-aw/home;\ngot:\n%s", allSteps)
		}
		if strings.Contains(allSteps, `\"proxyLogsDir\":\"${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs\"`) {
			t.Errorf("did not expect non-arc-dind external detector execution to rewrite proxyLogsDir under ${RUNNER_TEMP}/gh-aw;\ngot:\n%s", allSteps)
		}
	})
}

// TestBuildExternalDetectorExecutionStepEmitsTimeoutMinutes verifies that the external
// detector execution step is bounded by a step-level timeout, matching the inline
// detection path. Without it the detection job only relies on GH_AW_TIMEOUT_MINUTES,
// which the binary can only honour once it is running.
func TestBuildExternalDetectorExecutionStepEmitsTimeoutMinutes(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildExternalDetectorExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}
	joined := strings.Join(steps, "")

	expectedTimeout := resolveDetectionJobTimeoutValue(data)
	if !strings.Contains(joined, "        timeout-minutes: "+expectedTimeout+"\n") {
		t.Errorf("expected external detector execution step to declare timeout-minutes: %s;\ngot:\n%s", expectedTimeout, joined)
	}
	if !strings.Contains(joined, "GH_AW_TIMEOUT_MINUTES: "+expectedTimeout) {
		t.Errorf("expected external detector execution step to keep GH_AW_TIMEOUT_MINUTES aligned with the step timeout;\ngot:\n%s", joined)
	}
}

func TestBuildThreatDetectCommandOmitsUnsetOptionalFlags(t *testing.T) {
	cmd := buildThreatDetectCommand("setup-path", "copilot", &ThreatDetectionConfig{})

	if strings.Contains(cmd, "--engine-timeout") {
		t.Fatalf("expected --engine-timeout to be omitted when unset, got: %s", cmd)
	}
	if strings.Contains(cmd, "--max-turns") {
		t.Fatalf("expected --max-turns to be omitted when unset, got: %s", cmd)
	}
	if strings.Contains(cmd, "--retries") {
		t.Fatalf("expected --retries to be omitted when unset, got: %s", cmd)
	}
}

func TestBuildThreatDetectCommandEmitsConfiguredOptionalFlags(t *testing.T) {
	cmd := buildThreatDetectCommand("setup-path", "copilot", &ThreatDetectionConfig{
		EngineTimeout: strPtr("10m"),
		MaxTurns: func() *int {
			v := 100
			return &v
		}(),
		Retries: func() *int {
			v := 1
			return &v
		}(),
	})

	for _, want := range []string{
		"--engine-timeout 10m",
		"--max-turns 100",
		"--retries 1",
		"--output /tmp/gh-aw/threat-detection/detection_result.json /tmp/gh-aw/threat-detection",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got: %s", want, cmd)
		}
	}
}

func TestBuildThreatDetectCommandShellEscapesEngineID(t *testing.T) {
	cmd := buildThreatDetectCommand("setup-path", "copilot next", &ThreatDetectionConfig{})

	if !strings.Contains(cmd, "--engine 'copilot next'") {
		t.Fatalf("expected engine argument to be shell-escaped as a single argument, got: %s", cmd)
	}
}

// TestBuildInstallAWFForExternalDetectorStepUsesRootless verifies that the detection
// job installs the AWF binary in the same mode used to invoke awf in that job.
func TestBuildInstallAWFForExternalDetectorStepUsesRootless(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	lines := compiler.buildInstallAWFForExternalDetectorStep(data)
	if len(lines) == 0 {
		t.Fatal("expected non-empty AWF install step")
	}
	joined := strings.Join(lines, "")

	if !strings.Contains(joined, "install_awf_binary.sh") {
		t.Fatalf("expected AWF install step to invoke install_awf_binary.sh;\ngot:\n%s", joined)
	}
	if !strings.Contains(joined, "--rootless") {
		t.Errorf("expected detection AWF install to pass --rootless like the agent job;\ngot:\n%s", joined)
	}
}
