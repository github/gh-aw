//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/constants"
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

// TestCopilotEngineFirewallInstallation verifies that Copilot engine uses parallel installation when firewall is enabled
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

		// AWF installation should NOT be in engine installation steps anymore
		// It's deferred to parallel installation step
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				t.Error("AWF installation should be deferred to parallel installation step")
			}
		}

		// Verify that parallel installation should be used
		if !ShouldUseParallelInstallation(workflowData, engine) {
			t.Error("Parallel installation should be enabled with firewall and Copilot engine")
		}

		// Verify parallel installation config includes AWF with default version
		config := GetParallelInstallConfig(workflowData, engine)
		if config.AWFVersion != string(constants.DefaultFirewallVersion) {
			t.Errorf("Expected AWF version %s, got %s", string(constants.DefaultFirewallVersion), config.AWFVersion)
		}
		if config.CLIInfo == nil {
			t.Error("Parallel installation should include CLI info for Copilot")
		}

		// Generate the parallel installation step to verify it contains AWF installation
		parallelStep := generateParallelInstallationStep(config)
		parallelStepStr := strings.Join(parallelStep, "\n")

		// Verify it passes the default version to the script
		if !strings.Contains(parallelStepStr, string(constants.DefaultFirewallVersion)) {
			t.Errorf("Parallel installation step should include default version %s", string(constants.DefaultFirewallVersion))
		}
		// Verify it calls the install_parallel_setup.sh script
		if !strings.Contains(parallelStepStr, "install_parallel_setup.sh") {
			t.Error("Parallel installation should call install_parallel_setup.sh script")
		}
		// Verify it includes --awf flag
		if !strings.Contains(parallelStepStr, "--awf") {
			t.Error("Parallel installation should include --awf flag")
		}
		// Verify it includes CLI installation flags
		if !strings.Contains(parallelStepStr, "--cli-") {
			t.Error("Parallel installation should include CLI installation flags")
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

		// AWF installation should NOT be in engine installation steps anymore
		// It's deferred to parallel installation step
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				t.Error("AWF installation should be deferred to parallel installation step")
			}
		}

		// Verify parallel installation config includes AWF with custom version
		config := GetParallelInstallConfig(workflowData, engine)
		if config.AWFVersion != customVersion {
			t.Errorf("Expected AWF version %s, got %s", customVersion, config.AWFVersion)
		}

		// Generate the parallel installation step to verify it contains custom version
		parallelStep := generateParallelInstallationStep(config)
		parallelStepStr := strings.Join(parallelStep, "\n")

		// Verify it passes the custom version to the script
		if !strings.Contains(parallelStepStr, customVersion) {
			t.Errorf("Parallel installation step should include custom version %s", customVersion)
		}

		// Verify it calls the install_parallel_setup.sh script
		if !strings.Contains(parallelStepStr, "install_parallel_setup.sh") {
			t.Error("Parallel installation should call install_parallel_setup.sh script")
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
