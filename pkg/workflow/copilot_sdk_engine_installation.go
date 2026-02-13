// This file provides Copilot SDK engine installation logic.
//
// The SDK engine requires:
//  1. Copilot CLI binary (still needed -- the SDK is a JSON-RPC client to the CLI)
//  2. copilot-runner binary (the Go binary that uses the SDK)
//
// Installation order:
//  1. Secret validation (COPILOT_GITHUB_TOKEN)
//  2. Node.js setup
//  3. Sandbox installation (SRT or AWF, if needed)
//  4. Copilot CLI installation
//  5. Runner binary installation (downloaded from gh-aw actions setup)
//
// The runner binary is expected to be pre-installed at /opt/gh-aw/actions/copilot-runner
// by the actions/setup step, similar to how other action binaries are distributed.

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotSDKInstallLog = logger.New("workflow:copilot_sdk_engine_installation")

// GetInstallationSteps generates the complete installation workflow for the Copilot SDK engine.
// This includes secret validation, Node.js setup, sandbox installation,
// Copilot CLI installation, and runner binary verification.
func (e *CopilotSDKEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	copilotSDKInstallLog.Printf("Generating installation steps for Copilot SDK engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		copilotSDKInstallLog.Printf("Skipping installation steps: custom command specified (%s)", workflowData.EngineConfig.Command)
		return []GitHubActionStep{}
	}

	var steps []GitHubActionStep

	// Define engine configuration for shared validation
	config := EngineInstallConfig{
		Secrets:         []string{"COPILOT_GITHUB_TOKEN"},
		DocsURL:         "https://github.github.com/gh-aw/reference/engines/#github-copilot-sdk",
		NpmPackage:      "@github/copilot",
		Version:         string(constants.DefaultCopilotVersion),
		Name:            "GitHub Copilot SDK",
		CliName:         "copilot",
		InstallStepName: "Install GitHub Copilot CLI (for SDK)",
	}

	// Add secret validation step
	secretValidation := GenerateMultiSecretValidationStep(
		config.Secrets,
		config.Name,
		config.DocsURL,
	)
	steps = append(steps, secretValidation)

	// Determine Copilot version
	copilotVersion := config.Version
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		copilotVersion = workflowData.EngineConfig.Version
	}

	// Determine if Copilot should be installed globally or locally
	installGlobally := !isSRTEnabled(workflowData)

	// Generate install steps
	var npmSteps []GitHubActionStep
	if installGlobally {
		copilotSDKInstallLog.Print("Using installer script for Copilot CLI installation")
		npmSteps = GenerateCopilotInstallerSteps(copilotVersion, config.InstallStepName)
	} else {
		copilotSDKInstallLog.Print("Using local Copilot installation for SRT compatibility")
		npmSteps = GenerateNpmInstallStepsWithScope(
			config.NpmPackage,
			copilotVersion,
			config.InstallStepName,
			config.CliName,
			true,  // Include Node.js setup
			false, // Install locally
		)
	}

	// Add Node.js setup step first
	if len(npmSteps) > 0 {
		steps = append(steps, npmSteps[0])
	}

	// Add sandbox installation steps (SRT and AWF are mutually exclusive)
	if isSRTEnabled(workflowData) {
		agentConfig := getAgentConfig(workflowData)
		if agentConfig == nil || agentConfig.Command == "" {
			copilotSDKInstallLog.Print("Adding Sandbox Runtime (SRT) installation steps")
			srtSystemDeps := generateSRTSystemDepsStep()
			steps = append(steps, srtSystemDeps)

			srtSystemConfig := generateSRTSystemConfigStep()
			steps = append(steps, srtSystemConfig)

			srtInstall := generateSRTInstallationStep()
			steps = append(steps, srtInstall)
		}
	} else if isFirewallEnabled(workflowData) {
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfVersion string
		if firewallConfig != nil {
			awfVersion = firewallConfig.Version
		}

		awfInstall := generateAWFInstallationStep(awfVersion, agentConfig)
		if len(awfInstall) > 0 {
			steps = append(steps, awfInstall)
		}
	}

	// Add Copilot CLI installation step
	if len(npmSteps) > 1 {
		steps = append(steps, npmSteps[1:]...)
	}

	// Add runner binary verification step
	runnerVerifyStep := generateRunnerVerificationStep()
	steps = append(steps, runnerVerifyStep)

	return steps
}

// generateRunnerVerificationStep creates a step to verify the copilot-runner binary is available.
// The runner binary is expected to be pre-installed by the actions/setup step.
func generateRunnerVerificationStep() GitHubActionStep {
	stepLines := []string{
		"      - name: Verify copilot-runner binary",
		"        run: |",
		fmt.Sprintf("          if [ ! -x \"%s\" ]; then", copilotRunnerBinaryPath),
		fmt.Sprintf("            echo \"Error: copilot-runner binary not found at %s\"", copilotRunnerBinaryPath),
		"            echo \"The Copilot SDK engine requires the copilot-runner binary.\"",
		"            echo \"Ensure the actions/setup step includes the runner binary.\"",
		"            exit 1",
		"          fi",
		fmt.Sprintf("          echo \"copilot-runner binary found at %s\"", copilotRunnerBinaryPath),
		fmt.Sprintf("          %s --version 2>/dev/null || true", copilotRunnerBinaryPath),
	}

	return GitHubActionStep(stepLines)
}
