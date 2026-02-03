//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestAWFInstallationStepDefaultVersion verifies that AWF installation uses the default version when not specified
func TestAWFInstallationStepDefaultVersion(t *testing.T) {
	t.Run("uses default version when no version specified", func(t *testing.T) {
		step := generateAWFInstallationStep("", nil)
		stepStr := strings.Join(step, "\n")

		expectedVersion := string(constants.DefaultFirewallVersion)

		// Verify version is passed to the installation script
		if !strings.Contains(stepStr, expectedVersion) {
			t.Errorf("Expected to pass version %s to installation script, but it was not found", expectedVersion)
		}

		// Verify it calls the install_awf_binary.sh script
		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Expected to call install_awf_binary.sh script")
		}

		// Verify it uses the script from /opt/gh-aw/actions/
		if !strings.Contains(stepStr, "/opt/gh-aw/actions/install_awf_binary.sh") {
			t.Error("Expected to call script from /opt/gh-aw/actions/ directory")
		}

		// Ensure it's NOT using inline bash or the old unverified installer script
		if strings.Contains(stepStr, "raw.githubusercontent.com") {
			t.Error("Should NOT download installer script from raw.githubusercontent.com")
		}
	})

	t.Run("uses specified version when provided", func(t *testing.T) {
		customVersion := "v0.2.0"
		step := generateAWFInstallationStep(customVersion, nil)
		stepStr := strings.Join(step, "\n")

		// Verify custom version is passed to the script
		if !strings.Contains(stepStr, customVersion) {
			t.Errorf("Expected to pass custom version %s to installation script", customVersion)
		}

		// Verify it calls the install_awf_binary.sh script
		if !strings.Contains(stepStr, "install_awf_binary.sh") {
			t.Error("Expected to call install_awf_binary.sh script")
		}

		// Ensure it's NOT using the old unverified installer pattern
		if strings.Contains(stepStr, "raw.githubusercontent.com") {
			t.Error("Should NOT download installer script from raw.githubusercontent.com")
		}
	})
}

// TestCopilotEngineFirewallInstallation verifies that Copilot engine includes AWF installation when firewall is enabled
func TestCopilotEngineFirewallInstallation(t *testing.T) {
	t.Run("includes AWF installation step when firewall enabled", func(t *testing.T) {
		engine := NewCopilotEngine()
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)

		// Find the AWF installation step
		var foundAWFStep bool
		var awfStepStr string
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				foundAWFStep = true
				awfStepStr = stepStr
				break
			}
		}

		if !foundAWFStep {
			t.Fatal("Expected to find AWF installation step when firewall is enabled")
		}

		// Verify it passes the default version to the script
		if !strings.Contains(awfStepStr, string(constants.DefaultFirewallVersion)) {
			t.Errorf("AWF installation step should pass default version %s to script", string(constants.DefaultFirewallVersion))
		}
		// Verify it calls the install_awf_binary.sh script
		if !strings.Contains(awfStepStr, "install_awf_binary.sh") {
			t.Error("AWF installation should call install_awf_binary.sh script")
		}
		// Verify it's NOT using the old unverified installer script pattern
		if strings.Contains(awfStepStr, "raw.githubusercontent.com") {
			t.Error("AWF installation should NOT download from raw.githubusercontent.com")
		}
	})

	t.Run("uses custom version when specified in firewall config", func(t *testing.T) {
		engine := NewCopilotEngine()
		customVersion := "v0.3.0"
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: customVersion,
				},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)

		// Find the AWF installation step
		var foundAWFStep bool
		var awfStepStr string
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				foundAWFStep = true
				awfStepStr = stepStr
				break
			}
		}

		if !foundAWFStep {
			t.Fatal("Expected to find AWF installation step when firewall is enabled")
		}

		// Verify it passes the custom version to the script
		if !strings.Contains(awfStepStr, customVersion) {
			t.Errorf("AWF installation step should pass custom version %s to script", customVersion)
		}

		// Verify it calls the install_awf_binary.sh script
		if !strings.Contains(awfStepStr, "install_awf_binary.sh") {
			t.Error("AWF installation should call install_awf_binary.sh script")
		}

		// Verify it's NOT using the old unverified installer script pattern
		if strings.Contains(awfStepStr, "raw.githubusercontent.com") {
			t.Error("AWF installation should NOT download from raw.githubusercontent.com")
		}
	})

	t.Run("does not include AWF installation when firewall disabled", func(t *testing.T) {
		engine := NewCopilotEngine()
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: false,
				},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)

		// Should NOT find the AWF installation step
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				t.Error("Should not include AWF installation step when firewall is disabled")
			}
		}
	})
}
