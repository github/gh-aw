//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestBuildDetectionEngineExecutionStepPropagatesAPITarget verifies that when engine.api-target
// is configured on the main engine, the threat detection AWF invocation also receives
// --copilot-api-target and the GHE domains in --allow-domains.
// Regression test for: Threat detection AWF run missing --copilot-api-target on data residency.
func TestBuildDetectionEngineExecutionStepPropagatesAPITarget(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name             string
		data             *WorkflowData
		expectedTarget   string
		unexpectedTarget string
	}{
		{
			name: "api-target from main engine config is propagated to detection step",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "copilot-api.contoso-aw.ghe.com",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			expectedTarget: "copilot-api.contoso-aw.ghe.com",
		},
		{
			name: "api-target inherited when threat detection has its own engine config without api-target",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "api.acme.ghe.com",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						Model: "gpt-4",
						EngineConfig: &EngineConfig{
							ID: "copilot",
							// No APITarget set - should be inherited from main engine config
						},
					},
				},
			},
			expectedTarget: "api.acme.ghe.com",
		},
		{
			name: "detection engine config api-target takes precedence over main engine config",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "api.acme.ghe.com",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID:        "copilot",
							APITarget: "api.custom-detection.ghe.com",
						},
					},
				},
			},
			expectedTarget:   "api.custom-detection.ghe.com",
			unexpectedTarget: "api.acme.ghe.com",
		},
		{
			name: "no api-target when main engine config has none",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildDetectionEngineExecutionStep(tt.data)

			if len(steps) == 0 {
				t.Fatal("Expected non-empty steps")
			}

			allSteps := strings.Join(steps, "")

			if tt.expectedTarget != "" {
				// With config file support, copilot API target is in the JSON config
				if !strings.Contains(allSteps, `\"copilot\"`) {
					t.Errorf("Expected detection steps to contain copilot target in config JSON.\nGenerated steps:\n%s", allSteps)
				}
				if !strings.Contains(allSteps, tt.expectedTarget) {
					t.Errorf("Expected detection steps to contain api-target %q.\nGenerated steps:\n%s", tt.expectedTarget, allSteps)
				}
			}

			if tt.unexpectedTarget != "" {
				if strings.Contains(allSteps, tt.unexpectedTarget) {
					t.Errorf("Expected detection steps to NOT contain api-target %q, but found it.\nGenerated steps:\n%s", tt.unexpectedTarget, allSteps)
				}
			}
		})
	}
}

func TestBuildDetectionEngineExecutionStepPropagatesBYOKProviderHost(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		data         *WorkflowData
		wantHost     string
		unwantedHost string
	}{
		{
			name: "detection allow-domains includes BYOK provider host",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "${{ secrets.PROVIDER_BASE_URL }}",
					},
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
					Allowed:  []string{"defaults", "llm.corp.example.com"},
				},
			},
			wantHost: "llm.corp.example.com",
		},
		{
			name: "detection allow-domains stays minimal without BYOK provider host",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
			unwantedHost: "llm.corp.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allSteps := strings.Join(compiler.buildDetectionEngineExecutionStep(tt.data), "")
			if tt.wantHost != "" && !strings.Contains(allSteps, tt.wantHost) {
				t.Errorf("Expected detection steps to contain BYOK provider host %q.\nGenerated steps:\n%s", tt.wantHost, allSteps)
			}
			if tt.unwantedHost != "" && strings.Contains(allSteps, tt.unwantedHost) {
				t.Errorf("Expected detection steps to exclude BYOK provider host %q.\nGenerated steps:\n%s", tt.unwantedHost, allSteps)
			}
		})
	}
}

func TestBuildDetectionEngineExecutionStepEmitsNodeSetupForCopilot(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                string
		data                *WorkflowData
		expectedInstallStep string
	}{
		{
			name: "copilot main engine emits Setup Node.js once before install",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			expectedInstallStep: "Install GitHub Copilot CLI",
		},
		{
			name: "copilot via threat-detection engine override emits Setup Node.js once",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID: "copilot",
						},
					},
				},
			},
			expectedInstallStep: "Install GitHub Copilot CLI",
		},
		{
			name: "claude main engine already bundles Setup Node.js — no duplicate",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			expectedInstallStep: "Install Claude Code CLI",
		},
		{
			name: "codex main engine already bundles Setup Node.js — no duplicate",
			data: &WorkflowData{
				AI: "codex",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			expectedInstallStep: "Install Codex CLI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildDetectionEngineExecutionStep(tt.data)
			if len(steps) == 0 {
				t.Fatal("expected non-empty steps")
			}
			s := strings.Join(steps, "")

			if c := strings.Count(s, "- name: Setup Node.js"); c != 1 {
				t.Errorf("want exactly one Setup Node.js, got %d.\n%s", c, s)
			}

			nodeIdx := strings.Index(s, "- name: Setup Node.js")
			installIdx := strings.Index(s, "- name: "+tt.expectedInstallStep)
			if installIdx == -1 {
				t.Fatalf("missing %q step in:\n%s", tt.expectedInstallStep, s)
			}
			if nodeIdx > installIdx {
				t.Errorf("Setup Node.js (at %d) must precede %q (at %d)", nodeIdx, tt.expectedInstallStep, installIdx)
			}
		})
	}
}

func TestInstallStepsContainNodeSetup(t *testing.T) {
	tests := []struct {
		name     string
		steps    []GitHubActionStep
		expected bool
	}{
		{
			name:     "empty input",
			steps:    nil,
			expected: false,
		},
		{
			name:     "canonical setup-node step from GenerateNodeJsSetupStep",
			steps:    []GitHubActionStep{GenerateNodeJsSetupStep()},
			expected: true,
		},
		{
			name: "install-only step without node setup",
			steps: []GitHubActionStep{
				{"      - name: Install Some CLI", "        run: npm install -g some-cli"},
			},
			expected: false,
		},
		{
			name: "setup-node preceded by unrelated step",
			steps: []GitHubActionStep{
				{"      - name: Checkout", "        uses: actions/checkout@v4"},
				GenerateNodeJsSetupStep(),
			},
			expected: true,
		},
		{
			name: "differently indented setup-node (extractStepName whitespace tolerance)",
			steps: []GitHubActionStep{
				{"    - name: Setup Node.js", "      uses: actions/setup-node@v4"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := installStepsContainNodeSetup(tt.steps)
			if got != tt.expected {
				t.Errorf("installStepsContainNodeSetup() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildDetectionEngineExecutionStepPropagatesHarnessScriptOverride(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID:            "copilot",
					HarnessScript: "custom_copilot_harness.cjs",
				},
			},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}

	s := strings.Join(steps, "")

	if !strings.Contains(s, "custom_copilot_harness.cjs") {
		t.Errorf("expected custom harness script in detection steps, got:\n%s", s)
	}
	if strings.Contains(s, "actions/copilot_harness.cjs") {
		t.Errorf("expected default harness to be replaced by custom override, got:\n%s", s)
	}
}

func TestBuildDetectionEngineExecutionStepUsesCopilotForPi(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "pi",
		EngineConfig: &EngineConfig{
			ID: "pi",
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}

	rendered := strings.Join(steps, "")
	if !strings.Contains(rendered, "Install GitHub Copilot CLI") {
		t.Fatal("expected detection steps to include the Copilot install step for pi workflows")
	}
	if strings.Contains(rendered, "Install Pi CLI") {
		t.Fatal("expected detection steps to avoid Pi install step")
	}
}

func TestBuildDetectionEngineExecutionStepDoesNotReuseCustomEngineVersion(t *testing.T) {
	compiler := NewCompiler()
	compiler.engineCatalog.Register(&EngineDefinition{ID: "goose", DetectionEngine: "copilot"})

	data := &WorkflowData{
		AI: "goose",
		EngineConfig: &EngineConfig{
			ID:      "goose",
			Version: "1.45.0",
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	rendered := strings.Join(steps, "")

	if !strings.Contains(rendered, `install_copilot_cli.sh"`) {
		t.Fatalf("expected detection to install the Copilot CLI, got:\n%s", rendered)
	}
	if strings.Contains(rendered, `install_copilot_cli.sh" 1.45.0`) {
		t.Fatalf("expected detection to avoid passing the Goose version to the Copilot installer, got:\n%s", rendered)
	}
}

// TestBuildDetectionEngineExecutionStepArcDindTopology verifies that the detection job
// correctly propagates arc-dind runner topology from the main workflow data.
// Regression: before the fix, RunnerConfig was not propagated to threatDetectionData,
// so isArcDindTopology(threatDetectionData) was always false — the Copilot staging step
// was never emitted and the engine was spawned as /usr/local/bin/copilot (ENOENT inside
// the AWF chroot which uses the dind daemon's filesystem).
func TestBuildDetectionEngineExecutionStepArcDindTopology(t *testing.T) {
	compiler := NewCompiler()

	t.Run("arc-dind: emits daemon-visible staging step and uses RUNNER_TEMP copilot path", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildDetectionEngineExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// The staging step copies the Copilot CLI to a daemon-visible path.
		if !strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("expected 'Copy Copilot CLI to daemon-visible path' step in detection job for arc-dind;\ngot:\n%s", allSteps)
		}

		// The copilot_harness.cjs invocation must use the daemon-visible path specifically.
		// Note: constants.GhAwRootDirShell+"/bin/copilot" also appears in the staging step's
		// copy command ("cp /usr/local/bin/copilot ..."), so checking the harness line
		// directly avoids a false positive from the staging step.
		harnessArcDindPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, harnessArcDindPath) {
			t.Errorf("expected copilot_harness.cjs to be invoked with daemon-visible path %q for arc-dind;\ngot:\n%s", harnessArcDindPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("copilot_harness.cjs must NOT be invoked with %q for arc-dind (ENOENT inside chroot);\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
	})

	t.Run("non-arc-dind: resolves activated Copilot CLI binary", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			// RunnerConfig is nil → default topology
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildDetectionEngineExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// No daemon-visible staging step for standard runners.
		if strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("unexpected 'Copy Copilot CLI to daemon-visible path' step for non-arc-dind detection job;\ngot:\n%s", allSteps)
		}

		if !strings.Contains(allSteps, `GH_AW_COPILOT_SRC="$(command -v copilot 2>/dev/null || true)"`) {
			t.Errorf("expected detection execution to resolve the activated Copilot CLI binary;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"`) {
			t.Errorf("expected detection execution to stage the Copilot CLI binary in its mounted directory;\ngot:\n%s", allSteps)
		}
		mountedCopilotPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, mountedCopilotPath) {
			t.Errorf("expected detection harness to use mounted Copilot CLI path %q;\ngot:\n%s", mountedCopilotPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("expected detection harness to avoid fixed path %q;\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
	})
}

// TestBuildDetectionEngineExecutionStepPropagatesModelMappings verifies that the
// ModelMappings from the main WorkflowData are propagated to the threat detection
// WorkflowData so the detection awf-config.json includes the apiProxy.models alias map.
// Without this, copilot_harness.cjs cannot resolve alias model names (e.g. "small")
// to concrete ids before spawning the Copilot CLI in the detection job.
func TestBuildDetectionEngineExecutionStepPropagatesModelMappings(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "copilot",
		Model: "small",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		ModelMappings: map[string][]string{
			"small": {"mini"},
			"mini":  {"copilot/claude-haiku-4.5"},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty detection steps")
	}

	allSteps := strings.Join(steps, "")

	// The awf-config.json shell command embeds the JSON with escaped quotes (\"key\").
	// Search for the plain key names as substrings: they appear inside \"small\" etc.
	// Both entries must be present in the models section so copilot_harness.cjs can resolve
	// the alias chain small → mini → copilot/claude-haiku-4.5.
	if !strings.Contains(allSteps, "models") {
		t.Errorf("expected detection awf-config.json to contain a models section; got:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "small") {
		t.Errorf("expected detection awf-config.json to contain model alias 'small'; got:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "mini") {
		t.Errorf("expected detection awf-config.json to contain model alias 'mini'; got:\n%s", allSteps)
	}
}

func TestBuildDetectionEngineExecutionStepPropagatesModelCostsProviders(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "claude",
		Model: "accounts/fireworks/models/minimax-m3",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		ModelCosts: map[string]any{
			"providers": map[string]any{
				"anthropic": map[string]any{
					"models": map[string]any{
						"accounts/fireworks/models/minimax-m3": map[string]any{
							"cost": map[string]any{
								"input":  "3e-07",
								"output": "1.5e-06",
							},
						},
					},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFAPIProxyProvidersMinVersion)},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Version: string(constants.AWFAPIProxyProvidersMinVersion),
			},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty detection steps")
	}

	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "providers") {
		t.Errorf("expected detection awf-config.json to contain apiProxy.providers; got:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "accounts/fireworks/models/minimax-m3") {
		t.Errorf("expected detection awf-config.json to contain custom model pricing key; got:\n%s", allSteps)
	}
}
