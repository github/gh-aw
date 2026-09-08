//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestBuildDetectionJobStepsCodexAvoidsDuplicateContainerPullStep(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionJobSteps(data)
	stepsString := strings.Join(steps, "")

	if count := strings.Count(stepsString, "name: Download container images"); count != 1 {
		t.Errorf("Expected exactly one 'Download container images' step for Codex detection, got %d.\n%s", count, stepsString)
	}
}

func TestBuildDetectionJobStepsCodexExternalDetectorIncludesContainerDownload(t *testing.T) {
	// Regression test: when engine=codex and gh-aw-detection feature is enabled (external
	// detector path), the detection job must include a "Download container images" step.
	// Previously the step was omitted under the incorrect assumption that MCP setup generation
	// would emit it — MCP setup is only called for the inline codex detection path.
	compiler := NewCompiler()

	t.Run("codex with gh-aw-detection includes Download container images", func(t *testing.T) {
		data := &WorkflowData{
			AI: "codex",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Features: map[string]any{
				string(constants.GHAWDetectionFeatureFlag): true,
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}

		steps := compiler.buildDetectionJobSteps(data)
		joined := strings.Join(steps, "")

		if !strings.Contains(joined, "Download container images") {
			t.Errorf("expected 'Download container images' step in codex external detector detection job steps\ngot:\n%s", joined)
		}
		if !strings.Contains(joined, "download_docker_images.sh") {
			t.Errorf("expected 'download_docker_images.sh' in detection job steps\ngot:\n%s", joined)
		}
	})

	t.Run("codex with gh-aw-detection disabled emits exactly one container download (inline path via MCP setup)", func(t *testing.T) {
		data := &WorkflowData{
			AI: "codex",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Features: map[string]any{
				string(constants.GHAWDetectionFeatureFlag): false,
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}

		steps := compiler.buildDetectionJobSteps(data)
		joined := strings.Join(steps, "")

		// For the inline codex path, MCP setup generation (inside buildDetectionEngineExecutionStep)
		// emits the "Download container images" step exactly once. buildPullAWFContainersStep must
		// NOT also emit it, or the step would appear twice and trip duplicate-step validation.
		downloadCount := strings.Count(joined, "Download container images")
		if downloadCount != 1 {
			t.Errorf("expected exactly one 'Download container images' step for inline codex path, got %d\n%s", downloadCount, joined)
		}
	})
}

func TestBuildInstallDetectionEngineForExternalDetectorStepIncludesNodeRuntime(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                 string
		data                 *WorkflowData
		wantInstallStep      string
		wantArcDindSetup     bool
		wantCopilotInstalled bool
	}{
		{
			name: "copilot on standard topology",
			data: &WorkflowData{
				AI:          "copilot",
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:      "Install GitHub Copilot CLI",
			wantCopilotInstalled: true,
		},
		{
			name: "copilot on arc-dind",
			data: &WorkflowData{
				AI:           "copilot",
				RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
				SafeOutputs:  &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:      "Install GitHub Copilot CLI",
			wantArcDindSetup:     true,
			wantCopilotInstalled: true,
		},
		{
			name: "copilot detection custom command",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{ID: "copilot", Command: "/opt/custom/copilot"},
				}},
			},
			wantInstallStep:      "Install GitHub Copilot CLI",
			wantCopilotInstalled: true,
		},
		{
			name: "copilot inherited custom command",
			data: &WorkflowData{
				AI:           "copilot",
				EngineConfig: &EngineConfig{ID: "copilot", Command: "/opt/custom/copilot"},
				SafeOutputs:  &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:      "Install GitHub Copilot CLI",
			wantCopilotInstalled: true,
		},
		{
			name: "claude does not duplicate bundled node setup",
			data: &WorkflowData{
				AI:          "claude",
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep: "Install Claude Code CLI",
		},
		{
			name: "codex does not duplicate bundled node setup",
			data: &WorkflowData{
				AI:          "codex",
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep: "Install Codex CLI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := strings.Join(compiler.buildInstallDetectionEngineForExternalDetectorStep(tt.data), "")

			if count := strings.Count(steps, "- name: Setup Node.js"); count != 1 {
				t.Fatalf("expected exactly one Setup Node.js step, got %d:\n%s", count, steps)
			}
			if tt.wantInstallStep != "" {
				nodeIndex := strings.Index(steps, "- name: Setup Node.js")
				installIndex := strings.Index(steps, "- name: "+tt.wantInstallStep)
				if installIndex == -1 {
					t.Fatalf("expected %q step:\n%s", tt.wantInstallStep, steps)
				}
				if nodeIndex > installIndex {
					t.Errorf("Setup Node.js must precede %q:\n%s", tt.wantInstallStep, steps)
				}
			}
			if got := strings.Contains(steps, "Redirect tool cache and install paths for ARC/DinD"); got != tt.wantArcDindSetup {
				t.Errorf("ARC/DinD tool cache redirect presence = %v, want %v:\n%s", got, tt.wantArcDindSetup, steps)
			}
			if got := strings.Contains(steps, "Ensure Node.js is at daemon-visible path"); got != tt.wantArcDindSetup {
				t.Errorf("daemon-visible Node staging presence = %v, want %v:\n%s", got, tt.wantArcDindSetup, steps)
			}
			if tt.wantArcDindSetup {
				redirectIndex := strings.Index(steps, "Redirect tool cache and install paths for ARC/DinD")
				nodeIndex := strings.Index(steps, "- name: Setup Node.js")
				stagingIndex := strings.Index(steps, "Ensure Node.js is at daemon-visible path")
				if redirectIndex >= nodeIndex || nodeIndex >= stagingIndex {
					t.Errorf("expected ARC/DinD redirect, Node setup, and staging in order:\n%s", steps)
				}
			}
			if got := strings.Contains(steps, "- name: Install GitHub Copilot CLI"); got != tt.wantCopilotInstalled {
				t.Errorf("Copilot installation presence = %v, want %v:\n%s", got, tt.wantCopilotInstalled, steps)
			}
		})
	}
}

func TestBuildPullAWFContainersStepPropagatesFeatures(t *testing.T) {
	compiler := NewCompiler()

	t.Run("cli-proxy image included when feature flag is enabled", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Features: map[string]any{
				string(constants.CliProxyFeatureFlag): true,
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}

		steps := compiler.buildPullAWFContainersStep(data)
		stepsString := strings.Join(steps, "")

		if !strings.Contains(stepsString, "cli-proxy") {
			t.Error("Expected cli-proxy image in pull step when cli-proxy feature flag is enabled")
		}
	})

	t.Run("cli-proxy image excluded when feature flag is not set", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Features: map[string]any{},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}

		steps := compiler.buildPullAWFContainersStep(data)
		stepsString := strings.Join(steps, "")

		if strings.Contains(stepsString, "cli-proxy") {
			t.Error("Expected no cli-proxy image in pull step when cli-proxy feature flag is not set")
		}
	})
}

func TestBuildPullAWFContainersStepPropagatesRunnerTopology(t *testing.T) {
	compiler := NewCompiler()
	buildToolsImagePrefix := constants.DefaultFirewallRegistry + "/build-tools:"

	t.Run("arc-dind includes build-tools image", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildPullAWFContainersStep(data)
		stepsString := strings.Join(steps, "")

		if !strings.Contains(stepsString, buildToolsImagePrefix) {
			t.Errorf("expected build-tools image prefix %q in detection pull step for arc-dind;\ngot:\n%s", buildToolsImagePrefix, stepsString)
		}
	})

	t.Run("non-arc-dind excludes build-tools image", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildPullAWFContainersStep(data)
		stepsString := strings.Join(steps, "")

		if strings.Contains(stepsString, buildToolsImagePrefix) {
			t.Errorf("did not expect build-tools image prefix %q in detection pull step without arc-dind;\ngot:\n%s", buildToolsImagePrefix, stepsString)
		}
	})

	t.Run("permissions do not change pulled images", func(t *testing.T) {
		baseData := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}
		withPermissions := &WorkflowData{
			AI:                baseData.AI,
			SafeOutputs:       baseData.SafeOutputs,
			Permissions:       "contents: read",
			CachedPermissions: NewPermissionsContentsRead(),
		}

		baseSteps := strings.Join(compiler.buildPullAWFContainersStep(baseData), "")
		permissionSteps := strings.Join(compiler.buildPullAWFContainersStep(withPermissions), "")

		if permissionSteps != baseSteps {
			t.Errorf("expected detection pull step to ignore permissions when collecting images;\nwithout permissions:\n%s\nwith permissions:\n%s", baseSteps, permissionSteps)
		}
	})
}
