//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestSRTInstallationStepVersionPinning verifies that SRT installation uses a pinned version
func TestSRTInstallationStepVersionPinning(t *testing.T) {
	t.Run("SRT installation step uses pinned version", func(t *testing.T) {
		step := generateSRTInstallationStep()
		stepStr := strings.Join(step, "\n")

		expectedVersion := string(constants.DefaultSandboxRuntimeVersion)

		// Check that the npm install command includes the pinned version
		expectedInstall := "@anthropic-ai/sandbox-runtime@" + expectedVersion
		if !strings.Contains(stepStr, expectedInstall) {
			t.Errorf("Expected SRT installation step to contain '%s', got:\n%s", expectedInstall, stepStr)
		}

		// Verify the version is mentioned in the echo statement
		if !strings.Contains(stepStr, expectedVersion) {
			t.Errorf("Expected SRT installation step to mention version %s", expectedVersion)
		}
	})

	t.Run("SRT installation step does not use unpinned install", func(t *testing.T) {
		step := generateSRTInstallationStep()
		stepStr := strings.Join(step, "\n")

		// Check that we have a versioned npm install (with @version suffix)
		// The pattern should be "sandbox-runtime@" indicating a pinned version
		if !strings.Contains(stepStr, "@anthropic-ai/sandbox-runtime@") {
			t.Error("SRT installation step should use versioned npm install '@anthropic-ai/sandbox-runtime@VERSION'")
		}
	})
}

// TestCopilotEngineWithSRTVersionPinning verifies that Copilot engine SRT installation uses pinned version
func TestCopilotEngineWithSRTVersionPinning(t *testing.T) {
	t.Run("Copilot engine SRT installation uses pinned version", func(t *testing.T) {
		engine := NewCopilotEngine()
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "srt",
				},
			},
			Features: map[string]any{
				"sandbox-runtime": true,
			},
		}

		steps := engine.GetInstallationSteps(workflowData)

		// Find the SRT installation step
		var foundSRTStep bool
		var srtStepStr string
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install Sandbox Runtime") && !strings.Contains(stepStr, "System") {
				foundSRTStep = true
				srtStepStr = stepStr
				break
			}
		}

		if !foundSRTStep {
			t.Fatal("Expected to find SRT installation step when SRT is enabled")
		}

		// Verify it uses the pinned version
		expectedVersion := string(constants.DefaultSandboxRuntimeVersion)
		expectedInstall := "@anthropic-ai/sandbox-runtime@" + expectedVersion
		if !strings.Contains(srtStepStr, expectedInstall) {
			t.Errorf("SRT installation step should use pinned version %s, got:\n%s", expectedVersion, srtStepStr)
		}
	})
}
