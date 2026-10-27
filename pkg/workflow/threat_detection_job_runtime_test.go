//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestDockerImagePullPolicySteps(t *testing.T) {
	var step strings.Builder
	generateDownloadDockerImagesStep(&step, []string{"registry.example.com/app:v1"}, "never")
	if !strings.Contains(step.String(), "GH_AW_DOCKER_IMAGE_PULL_POLICY: never") {
		t.Fatalf("agent download step missing local-only policy: %s", step.String())
	}
	step.Reset()
	generateDownloadDockerImagesStep(&step, []string{"registry.example.com/app:v1"}, "always")
	if !strings.Contains(step.String(), "GH_AW_DOCKER_IMAGE_PULL_POLICY: always") {
		t.Fatalf("agent download step missing explicit pull policy: %s", step.String())
	}
	step.Reset()
	generateDownloadDockerImagesStep(&step, []string{"registry.example.com/app:v1"}, "")
	if strings.Contains(step.String(), "GH_AW_DOCKER_IMAGE_PULL_POLICY") {
		t.Fatalf("default download step must not override policy: %s", step.String())
	}

	data := &WorkflowData{
		AI:             "codex",
		RawFrontmatter: map[string]any{"docker-image-pull-policy": "never"},
		SafeOutputs:    &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
		Features:       map[string]any{string(constants.GHAWDetectionFeatureFlag): true},
		SandboxConfig:  &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}},
	}
	steps := strings.Join(NewCompiler().buildDetectionJobSteps(data), "")
	if !strings.Contains(steps, "GH_AW_DOCKER_IMAGE_PULL_POLICY: never") {
		t.Fatalf("detection download step missing local-only policy: %s", steps)
	}
	data.Features[string(constants.GHAWDetectionFeatureFlag)] = false
	steps = strings.Join(NewCompiler().buildDetectionJobSteps(data), "")
	if !strings.Contains(steps, "GH_AW_DOCKER_IMAGE_PULL_POLICY: never") {
		t.Fatalf("inline detection download step missing local-only policy: %s", steps)
	}

	data.RawFrontmatter["docker-image-pull-policy"] = "always"
	for _, featureEnabled := range []bool{true, false} {
		data.Features[string(constants.GHAWDetectionFeatureFlag)] = featureEnabled
		steps = strings.Join(NewCompiler().buildDetectionJobSteps(data), "")
		if !strings.Contains(steps, "GH_AW_DOCKER_IMAGE_PULL_POLICY: always") {
			t.Fatalf("detection download step missing explicit pull policy with feature enabled=%v: %s", featureEnabled, steps)
		}
	}
}

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
		wantNodeActionRef    string
		wantNodeVersion      string
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
		{
			name: "copilot injected node setup honors runtimes.node action override",
			data: &WorkflowData{
				AI:          "copilot",
				Runtimes:    map[string]any{"node": map[string]any{"action-repo": "myorg/myrepo/.github/actions/setup-node", "action-version": "main"}},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:      "Install GitHub Copilot CLI",
			wantCopilotInstalled: true,
			wantNodeActionRef:    "myorg/myrepo/.github/actions/setup-node@main",
		},
		{
			name: "claude bundled node setup honors runtimes.node action override",
			data: &WorkflowData{
				AI:          "claude",
				Runtimes:    map[string]any{"node": map[string]any{"action-repo": "myorg/myrepo/.github/actions/setup-node", "action-version": "main"}},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:   "Install Claude Code CLI",
			wantNodeActionRef: "myorg/myrepo/.github/actions/setup-node@main",
		},
		{
			name: "codex bundled node setup honors runtimes.node action override",
			data: &WorkflowData{
				AI:          "codex",
				Runtimes:    map[string]any{"node": map[string]any{"action-repo": "myorg/myrepo/.github/actions/setup-node", "action-version": "main"}},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep:   "Install Codex CLI",
			wantNodeActionRef: "myorg/myrepo/.github/actions/setup-node@main",
		},
		{
			name: "external detector honors pinned node version",
			data: &WorkflowData{
				AI:          "claude",
				Runtimes:    map[string]any{"node": map[string]any{"version": "24.21.0"}},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			},
			wantInstallStep: "Install Claude Code CLI",
			wantNodeVersion: "24.21.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := strings.Join(compiler.buildInstallDetectionEngineForExternalDetectorStep(tt.data), "")

			wantNodeActionRef := tt.wantNodeActionRef
			if wantNodeActionRef == "" {
				wantNodeActionRef = getActionPin("actions/setup-node")
			}
			if !strings.Contains(steps, "uses: "+wantNodeActionRef+"\n") {
				t.Errorf("expected Setup Node.js to use %q:\n%s", wantNodeActionRef, steps)
			}

			if count := strings.Count(steps, "- name: Setup Node.js"); count != 1 {
				t.Fatalf("expected exactly one Setup Node.js step, got %d:\n%s", count, steps)
			}
			if tt.wantNodeVersion != "" && !strings.Contains(steps, "node-version: '"+tt.wantNodeVersion+"'") {
				t.Errorf("expected Node version %q:\n%s", tt.wantNodeVersion, steps)
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

func TestBuildPullAWFContainersStepPropagatesGitHubCLIMode(t *testing.T) {
	compiler := NewCompiler()

	t.Run("cli-proxy image included when GitHub gh-proxy mode is enabled", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
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
			t.Error("Expected cli-proxy image in pull step when GitHub gh-proxy mode is enabled")
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
			t.Error("Expected no cli-proxy image in pull step when GitHub gh-proxy mode is not enabled")
		}
	})
}

func TestBuildThreatDetectionAWFConfigCACert(t *testing.T) {
	tests := []struct {
		name            string
		sandbox         *SandboxConfig
		safe            *SafeOutputsConfig
		detectionEngine string
		want            string
	}{
		{name: "no sandbox or safe outputs"},
		{name: "no agent sandbox", sandbox: &SandboxConfig{}},
		{name: "no certificate", sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{}}},
		{
			name:    "inherits agent certificate",
			sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			want:    "/tmp/agent-ca.pem",
		},
		{
			name:    "inherits with default detection settings",
			sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			safe:    &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			want:    "/tmp/agent-ca.pem",
		},
		{
			name:    "inherits without detection config",
			sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			safe:    &SafeOutputsConfig{},
			want:    "/tmp/agent-ca.pem",
		},
		{
			name:    "detection override",
			sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			safe:    &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{CACert: "/tmp/detection-ca.pem"}},
			want:    "/tmp/detection-ca.pem",
		},
		{
			name:    "explicitly disables inheritance",
			sandbox: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			safe:    &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{DisableCACert: true}},
		},
		{
			name:            "different detection engine does not inherit",
			sandbox:         &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			safe:            &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "copilot"}}},
			detectionEngine: "copilot",
		},
		{
			name: "detection certificate without agent sandbox",
			safe: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{CACert: "/tmp/detection-ca.pem"}},
			want: "/tmp/detection-ca.pem",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{AI: "claude", SandboxConfig: tt.sandbox, SafeOutputs: tt.safe}
			originalCACert := extractAPIProxyCACert(data)
			detectionData := buildThreatDetectionWorkflowData(data, tt.detectionEngine)
			engineName := tt.detectionEngine
			if engineName == "" {
				engineName = "claude"
			}
			if got := detectionData.SandboxConfig.Agent.CACert; got != tt.want {
				t.Fatalf("detection CA certificate = %q, want %q", got, tt.want)
			}
			configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName:   engineName,
				WorkflowData: detectionData,
			})
			if err != nil {
				t.Fatalf("BuildAWFConfigJSON() error = %v", err)
			}
			var config struct {
				APIProxy struct {
					CACert string `json:"caCert"`
				} `json:"apiProxy"`
			}
			if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
				t.Fatalf("unmarshal AWF config: %v", err)
			}
			if config.APIProxy.CACert != tt.want {
				t.Errorf("apiProxy.caCert = %q, want %q", config.APIProxy.CACert, tt.want)
			}
			if tt.want == "" && strings.Contains(configJSON, `"caCert"`) {
				t.Error("apiProxy.caCert must be omitted when not configured")
			}
			detectionData.SandboxConfig.Agent.CACert = "/tmp/changed.pem"
			if extractAPIProxyCACert(data) != originalCACert {
				t.Error("detection sandbox must not mutate the agent sandbox")
			}
		})
	}
}

func TestBuildThreatDetectionAWFConfigTargets(t *testing.T) {
	tests := []struct {
		name            string
		detectionEngine string
		wantTargets     bool
	}{
		{name: "same engine inherits targets", detectionEngine: "copilot", wantTargets: true},
		{name: "different engine does not inherit targets", detectionEngine: "claude"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentTargets := map[string]*AgentAPIProxyTargetConfig{
				"copilot": {
					ExtraHeaders:    map[string]string{"x-gateway-id": "my-id"},
					ExtraBodyFields: map[string]string{"provider": "anthropic"},
					SessionId:       "agent-session",
				},
			}
			data := &WorkflowData{
				AI: "copilot",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Targets: agentTargets},
				},
			}

			detectionData := buildThreatDetectionWorkflowData(data, tt.detectionEngine)
			configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName:   tt.detectionEngine,
				WorkflowData: detectionData,
			})
			if err != nil {
				t.Fatalf("BuildAWFConfigJSON() error = %v", err)
			}

			var config struct {
				APIProxy struct {
					Targets map[string]struct {
						ExtraHeaders    map[string]string `json:"extraHeaders"`
						ExtraBodyFields map[string]string `json:"extraBodyFields"`
						SessionID       string            `json:"sessionId"`
					} `json:"targets"`
				} `json:"apiProxy"`
			}
			if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
				t.Fatalf("unmarshal AWF config: %v", err)
			}

			target, exists := config.APIProxy.Targets["copilot"]
			if !tt.wantTargets {
				if exists {
					t.Fatal("detection config must not inherit targets for a different engine")
				}
				return
			}
			if !exists {
				t.Fatal("detection config is missing inherited Copilot target")
			}
			if target.ExtraHeaders["x-gateway-id"] != "my-id" {
				t.Errorf("extraHeaders = %v, want gateway header", target.ExtraHeaders)
			}
			if target.ExtraBodyFields["provider"] != "anthropic" {
				t.Errorf("extraBodyFields = %v, want provider field", target.ExtraBodyFields)
			}
			if target.SessionID != "agent-session" {
				t.Errorf("sessionId = %q, want agent-session", target.SessionID)
			}

			detectionData.SandboxConfig.Agent.Targets["copilot"].ExtraHeaders["x-gateway-id"] = "changed"
			detectionData.SandboxConfig.Agent.Targets["copilot"].ExtraBodyFields["provider"] = "changed"
			if agentTargets["copilot"].ExtraHeaders["x-gateway-id"] != "my-id" ||
				agentTargets["copilot"].ExtraBodyFields["provider"] != "anthropic" {
				t.Error("detection targets must not mutate the agent targets")
			}
		})
	}
}

func TestDetectionExecutionStepsCACert(t *testing.T) {
	compiler := NewCompiler()
	for _, engine := range []string{"claude", "codex", "copilot"} {
		for _, override := range []string{"", "/tmp/detection-ca.pem"} {
			data := &WorkflowData{
				AI:            engine,
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
				SafeOutputs:   &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{CACert: override}},
			}
			want := "/tmp/agent-ca.pem"
			if override != "" {
				want = override
			}
			for name, build := range map[string]func(*WorkflowData) []string{
				"external": compiler.buildExternalDetectorExecutionStep,
				"inline":   compiler.buildDetectionEngineExecutionStep,
			} {
				t.Run(engine+"/"+name+"/"+want, func(t *testing.T) {
					steps := strings.ReplaceAll(strings.Join(build(data), ""), `\"`, `"`)
					if !strings.Contains(steps, `"caCert":"`+want+`"`) {
						t.Errorf("detection execution steps missing apiProxy.caCert %q", want)
					}
					if override != "" && strings.Contains(steps, "/tmp/agent-ca.pem") {
						t.Error("detection execution steps must not use the overridden agent certificate")
					}
				})
			}
		}
	}

	t.Run("different detection engine does not inherit", func(t *testing.T) {
		data := &WorkflowData{
			AI:            "claude",
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{ID: "copilot"},
			}},
		}
		for name, build := range map[string]func(*WorkflowData) []string{
			"external": compiler.buildExternalDetectorExecutionStep,
			"inline":   compiler.buildDetectionEngineExecutionStep,
		} {
			t.Run(name, func(t *testing.T) {
				steps := strings.Join(build(data), "")
				if strings.Contains(steps, `"caCert":`) || strings.Contains(steps, "/tmp/agent-ca.pem") {
					t.Errorf("different-engine detection must not inherit the agent CA: %s", steps)
				}
			})
		}
	})

	t.Run("public endpoint override can disable inherited CA", func(t *testing.T) {
		data := &WorkflowData{
			AI: "claude",
			EngineConfig: &EngineConfig{
				ID:        "claude",
				APITarget: "private-upstream.example.com",
			},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{CACert: "/tmp/agent-ca.pem"}},
			SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
				DisableCACert: true,
				EngineConfig: &EngineConfig{
					ID:        "claude",
					APITarget: "public-upstream.example.com",
				},
			}},
		}
		for name, build := range map[string]func(*WorkflowData) []string{
			"external": compiler.buildExternalDetectorExecutionStep,
			"inline":   compiler.buildDetectionEngineExecutionStep,
		} {
			t.Run(name, func(t *testing.T) {
				steps := strings.Join(build(data), "")
				if strings.Contains(steps, `"caCert":`) || strings.Contains(steps, "/tmp/agent-ca.pem") {
					t.Errorf("public-upstream detection must not inherit the agent CA: %s", steps)
				}
				if !strings.Contains(steps, "public-upstream.example.com") {
					t.Errorf("expected detection to use its public endpoint override: %s", steps)
				}
			})
		}
	})
}

func TestBuildThreatDetectionWorkflowDataPropagatesImageOverrides(t *testing.T) {
	images := map[string]string{
		awfImageRoleSquid:    "mirror.example.com/awf/squid:v1@sha256:" + strings.Repeat("a", 64),
		awfImageRoleAgent:    "mirror.example.com/awf/agent:v1@sha256:" + strings.Repeat("b", 64),
		awfImageRoleAPIProxy: "mirror.example.com/awf/api-proxy:v1@sha256:" + strings.Repeat("c", 64),
	}
	containerPins := map[string]string{
		"ghcr.io/github/gh-aw-firewall/squid:v1": images[awfImageRoleSquid],
	}
	data := &WorkflowData{
		AI: "copilot",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type:   SandboxTypeAWF,
				Images: images,
			},
		},
		ContainerPinMappings: containerPins,
	}

	detectionData := buildThreatDetectionWorkflowData(data, "")

	for role, image := range images {
		if detectionData.SandboxConfig.Agent.Images[role] != image {
			t.Errorf("detection image %q = %q, want %q", role, detectionData.SandboxConfig.Agent.Images[role], image)
		}
	}
	for source, target := range containerPins {
		if detectionData.ContainerPinMappings[source] != target {
			t.Errorf("detection container pin %q = %q, want %q", source, detectionData.ContainerPinMappings[source], target)
		}
	}

	detectionData.SandboxConfig.Agent.Images[awfImageRoleSquid] = "changed"
	detectionData.ContainerPinMappings["ghcr.io/github/gh-aw-firewall/squid:v1"] = "changed"
	if images[awfImageRoleSquid] == "changed" {
		t.Error("detection image overrides must not alias the parent map")
	}
	if containerPins["ghcr.io/github/gh-aw-firewall/squid:v1"] == "changed" {
		t.Error("detection container pin mappings must not alias the parent map")
	}
}

func TestBuildPullAWFContainersStepUsesSandboxAgentImages(t *testing.T) {
	compiler := NewCompiler()
	mirrorRegistry := "mirror.example.com/awf/"
	data := &WorkflowData{
		AI: "copilot",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
				Images: map[string]string{
					awfImageRoleSquid:    mirrorRegistry + "squid:v1@sha256:" + strings.Repeat("a", 64),
					awfImageRoleAgent:    mirrorRegistry + "agent:v1@sha256:" + strings.Repeat("b", 64),
					awfImageRoleAPIProxy: mirrorRegistry + "api-proxy:v1@sha256:" + strings.Repeat("c", 64),
				},
			},
		},
	}

	steps := strings.Join(compiler.buildPullAWFContainersStep(data), "")

	if !strings.Contains(steps, mirrorRegistry) {
		t.Errorf("expected detection pull step to use sandbox.agent.images overrides; got:\n%s", steps)
	}
	if strings.Contains(steps, constants.DefaultFirewallRegistry) {
		t.Errorf("expected detection pull step not to use default AWF registry; got:\n%s", steps)
	}
}

func TestBuildPullAWFContainersStepUsesContainerPinMappings(t *testing.T) {
	compiler := NewCompiler()
	source := constants.DefaultFirewallRegistry + "/squid:1.2.3"
	target := "mirror.example.com/awf/squid:1.2.3@sha256:" + strings.Repeat("a", 64)
	data := &WorkflowData{
		AI: "copilot",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type:    SandboxTypeAWF,
				Version: "v1.2.3",
			},
		},
		ContainerPinMappings: map[string]string{
			source: target,
		},
	}

	steps := strings.Join(compiler.buildPullAWFContainersStep(data), "")

	if !strings.Contains(steps, target) {
		t.Errorf("expected detection pull step to use container_pins mapping %q; got:\n%s", target, steps)
	}
	if strings.Contains(steps, source) {
		t.Errorf("expected detection pull step not to use mapped source image %q; got:\n%s", source, steps)
	}
}

func TestBuildThreatDetectionAWFConfigUsesContainerPinMappings(t *testing.T) {
	const version = "1.2.3"
	containerPins := map[string]string{}
	expectedImages := map[string]string{}
	for i, role := range []string{awfImageRoleSquid, awfImageRoleAgent, awfImageRoleAPIProxy} {
		source := defaultAWFImageForRole(role, version)
		target := "mirror.example.com/awf/" + strings.TrimPrefix(source, constants.DefaultFirewallRegistry+"/") +
			"@sha256:" + strings.Repeat(string(rune('a'+i)), 64)
		containerPins[source] = target
		expectedImages[role] = target
	}
	data := &WorkflowData{
		AI: "copilot",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: version},
		},
		ContainerPinMappings: containerPins,
	}

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData:   buildThreatDetectionWorkflowData(data, ""),
	})
	if err != nil {
		t.Fatalf("BuildAWFConfigJSON() error = %v", err)
	}

	var config struct {
		Container AWFContainerConfig `json:"container"`
	}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("unmarshal AWF config: %v", err)
	}
	if config.Container.ImageTag != "" {
		t.Errorf("detection imageTag = %q, want empty when mapped images are emitted", config.Container.ImageTag)
	}
	for role, target := range expectedImages {
		if config.Container.Images[role] != target {
			t.Errorf("detection runtime image %q = %q, want %q", role, config.Container.Images[role], target)
		}
	}
	if strings.Contains(configJSON, constants.DefaultFirewallRegistry) {
		t.Errorf("detection AWF config must not use the default registry when all required roles are mapped: %s", configJSON)
	}
}

func TestBuildThreatDetectionContainerImagesRequiresPinnedOptionalRoles(t *testing.T) {
	const version = "1.2.3"
	data := &WorkflowData{
		RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: version},
		},
		ContainerPinMappings: map[string]string{
			defaultAWFImageForRole(awfImageRoleSquid, version): "mirror.example.com/awf/squid:" + version +
				"@sha256:" + strings.Repeat("a", 64),
		},
	}

	if images := buildThreatDetectionContainerImages(data); images != nil {
		t.Errorf("buildThreatDetectionContainerImages() = %v, want nil for unpinned buildTools role", images)
	}
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
