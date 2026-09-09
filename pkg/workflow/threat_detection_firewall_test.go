//go:build !integration

package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCleanFirewallDirsStepPresent(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionJobSteps(data)
	stepsString := strings.Join(steps, "")

	// The cleanup step should be present
	if !strings.Contains(stepsString, "Clean stale firewall files from agent artifact") {
		t.Error("Expected 'Clean stale firewall files from agent artifact' step in detection steps")
	}

	// It should remove the firewall logs and audit directories
	if !strings.Contains(stepsString, constants.AWFProxyLogsDir.String()) {
		t.Errorf("Expected cleanup step to reference %s", constants.AWFProxyLogsDir.String())
	}
	if !strings.Contains(stepsString, constants.AWFAuditDir.String()) {
		t.Errorf("Expected cleanup step to reference %s", constants.AWFAuditDir.String())
	}
}

func TestCleanFirewallDirsStepOrdering(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionJobSteps(data)
	stepsString := strings.Join(steps, "")

	cleanIdx := strings.Index(stepsString, "Clean stale firewall files from agent artifact")
	guardIdx := strings.Index(stepsString, "Check if detection needed")

	if cleanIdx < 0 {
		t.Fatal("Expected 'Clean stale firewall files from agent artifact' step")
	}
	if guardIdx < 0 {
		t.Fatal("Expected 'Check if detection needed' step")
	}

	// The cleanup step must come before the detection guard
	if cleanIdx > guardIdx {
		t.Error("Cleanup firewall dirs step should appear before detection guard step")
	}
}

func TestBuildCopyDetectionFirewallLogsStep(t *testing.T) {
	compiler := NewCompiler()

	t.Run("returns nil when firewall disabled", func(t *testing.T) {
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}
		if steps := compiler.buildCopyDetectionFirewallLogsStep(data); steps != nil {
			t.Fatalf("expected nil steps when firewall is disabled, got %v", steps)
		}
	})

	t.Run("default topology copies from tmp paths and directory contents", func(t *testing.T) {
		data := &WorkflowData{
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := strings.Join(compiler.buildCopyDetectionFirewallLogsStep(data), "")
		if !strings.Contains(steps, "Copy detection firewall logs") {
			t.Fatalf("expected copy step to be present, got:\n%s", steps)
		}
		if !strings.Contains(steps, "continue-on-error: true") {
			t.Fatalf("expected continue-on-error in copy step, got:\n%s", steps)
		}
		if !strings.Contains(steps, "if [ -d "+constants.AWFProxyLogsDir.String()+" ]; then") {
			t.Fatalf("expected logs copy to be guarded by an explicit directory check, got:\n%s", steps)
		}
		if !strings.Contains(steps, "if [ -d "+constants.AWFAuditDir.String()+" ]; then") {
			t.Fatalf("expected audit copy to be guarded by an explicit directory check, got:\n%s", steps)
		}
		if strings.Contains(steps, "|| true") {
			t.Fatalf("expected copy failures not to be suppressed, got:\n%s", steps)
		}
		if !strings.Contains(steps, "cp -r "+constants.AWFProxyLogsDir.String()+"/. "+detectionFirewallLogsDir(data)+"/logs/") {
			t.Fatalf("expected logs copy to use source contents and stable destination, got:\n%s", steps)
		}
		if !strings.Contains(steps, "cp -r "+constants.AWFAuditDir.String()+"/. "+detectionFirewallLogsDir(data)+"/audit/") {
			t.Fatalf("expected audit copy to use source contents and stable destination, got:\n%s", steps)
		}
	})

	t.Run("arc-dind topology copies from runner_temp paths", func(t *testing.T) {
		data := &WorkflowData{
			RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := strings.Join(compiler.buildCopyDetectionFirewallLogsStep(data), "")
		if !strings.Contains(steps, `${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs`) {
			t.Fatalf("expected arc-dind logs source path under ${RUNNER_TEMP}/gh-aw, got:\n%s", steps)
		}
		if !strings.Contains(steps, `${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit`) {
			t.Fatalf("expected arc-dind audit source path under ${RUNNER_TEMP}/gh-aw, got:\n%s", steps)
		}
	})
}

func TestGetThreatDetectionAdditionalAllowedDomains_WithCustomProviderBaseURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURLVar   string
		baseURLValue string
	}{
		{
			name:         "openai base URL",
			baseURLVar:   "OPENAI_BASE_URL",
			baseURLValue: "https://llm-router.internal.example.com/v1",
		},
		{
			name:         "anthropic base URL",
			baseURLVar:   "ANTHROPIC_BASE_URL",
			baseURLValue: "https://anthropic-router.internal.example.com/v1",
		},
		{
			name:         "copilot provider base URL",
			baseURLVar:   constants.CopilotProviderBaseURL,
			baseURLValue: "https://copilot-router.internal.example.com/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						tt.baseURLVar: tt.baseURLValue,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Allowed: []string{
						"llm-router.internal.example.com",
						"anthropic-router.internal.example.com",
						"copilot-router.internal.example.com",
						"api.openai.com",
						"${{ inputs.allowed_domains }}",
						"chatgpt.com",
					},
				},
			}

			got := getThreatDetectionAdditionalAllowedDomains(data)
			want := []string{
				"llm-router.internal.example.com",
				"anthropic-router.internal.example.com",
				"copilot-router.internal.example.com",
				"api.openai.com",
				"chatgpt.com",
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("expected additional allowed domains %v, got %v", want, got)
			}
		})
	}
}

// TestGetThreatDetectionAdditionalAllowedDomains_DetectionOnlyBaseURL verifies that
// a custom base URL configured only in safe-outputs.threat-detection.engine.env (not
// in the main engine env) still triggers domain propagation. This is the case where
// the effective merged detection env must be evaluated, not just data.EngineConfig.Env.
func TestGetThreatDetectionAdditionalAllowedDomains_DetectionOnlyBaseURL(t *testing.T) {
	data := &WorkflowData{
		// Main engine has no custom base URL.
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				"SOME_OTHER_VAR": "value",
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://detection-router.internal.example.com/v1",
					},
				},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Allowed: []string{
				"detection-router.internal.example.com",
				"api.openai.com",
			},
		},
	}

	got := getThreatDetectionAdditionalAllowedDomains(data)
	want := []string{
		"detection-router.internal.example.com",
		"api.openai.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected additional allowed domains %v, got %v (detection-only base URL must also trigger propagation)", want, got)
	}
}

func TestAppendThreatDetectionRWMount(t *testing.T) {
	threatDetectionMount := constants.ThreatDetectionDir + ":" + constants.ThreatDetectionDir + ":rw"

	t.Run("appends missing mount without clobbering existing mounts", func(t *testing.T) {
		existingMounts := []string{
			"/tmp/existing:/tmp/existing:ro",
			"/tmp/other:/tmp/other:rw",
		}

		got := appendThreatDetectionRWMount(append([]string(nil), existingMounts...), &WorkflowData{})

		want := append(append([]string(nil), existingMounts...), threatDetectionMount)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected mounts %v, got %v", want, got)
		}
	})

	t.Run("does not duplicate existing threat-detection mount", func(t *testing.T) {
		existingMounts := []string{
			"/tmp/existing:/tmp/existing:ro",
			threatDetectionMount,
		}

		got := appendThreatDetectionRWMount(append([]string(nil), existingMounts...), &WorkflowData{})

		if !reflect.DeepEqual(got, existingMounts) {
			t.Fatalf("expected mounts %v, got %v", existingMounts, got)
		}
	})
}
