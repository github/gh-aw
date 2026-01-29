package workflow

import (
	"fmt"
	"strings"

	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
)

var parallelInstallLog = logger.New("workflow:parallel_installation")

// ParallelInstallConfig holds configuration for parallel installation
type ParallelInstallConfig struct {
	AWFVersion     string   // AWF binary version to install (empty to skip)
	CopilotVersion string   // Copilot CLI version to install (empty to skip)
	ClaudeVersion  string   // Claude Code CLI version to install (empty to skip)
	DockerImages   []string // Docker images to download (empty to skip)
}

// generateParallelInstallationStep generates a single step that installs dependencies in parallel
// This parallelizes AWF binary installation, CLI installation, and Docker image downloads
// to reduce sequential execution time by 8-12 seconds.
func generateParallelInstallationStep(config ParallelInstallConfig) GitHubActionStep {
	if config.AWFVersion == "" && config.CopilotVersion == "" && config.ClaudeVersion == "" && len(config.DockerImages) == 0 {
		parallelInstallLog.Print("No parallel installations configured, skipping")
		return GitHubActionStep([]string{})
	}

	// Count how many operations will run in parallel
	operationCount := 0
	if config.AWFVersion != "" {
		operationCount++
	}
	if config.CopilotVersion != "" {
		operationCount++
	}
	if config.ClaudeVersion != "" {
		operationCount++
	}
	if len(config.DockerImages) > 0 {
		operationCount++
	}

	parallelInstallLog.Printf("Generating parallel installation step for %d operations", operationCount)

	stepLines := []string{
		"      - name: Install dependencies in parallel",
		"        run: |",
		"          # Install dependencies in parallel to reduce setup time",
		"          # This parallelizes AWF binary, CLI, and Docker image downloads",
		"          bash /opt/gh-aw/actions/install_parallel_setup.sh \\",
	}

	// Add AWF installation argument
	if config.AWFVersion != "" {
		stepLines = append(stepLines, fmt.Sprintf("            --awf %s \\", config.AWFVersion))
	}

	// Add Copilot installation argument
	if config.CopilotVersion != "" {
		stepLines = append(stepLines, fmt.Sprintf("            --copilot %s \\", config.CopilotVersion))
	}

	// Add Claude installation argument
	if config.ClaudeVersion != "" {
		stepLines = append(stepLines, fmt.Sprintf("            --claude %s \\", config.ClaudeVersion))
	}

	// Add Docker images argument
	if len(config.DockerImages) > 0 {
		var dockerArgs strings.Builder
		dockerArgs.WriteString("            --docker")
		for _, image := range config.DockerImages {
			dockerArgs.WriteString(fmt.Sprintf(" %s", image))
		}
		stepLines = append(stepLines, dockerArgs.String())
	} else {
		// Remove trailing backslash from last line if no docker images
		lastLine := stepLines[len(stepLines)-1]
		if strings.HasSuffix(lastLine, " \\") {
			stepLines[len(stepLines)-1] = strings.TrimSuffix(lastLine, " \\")
		}
	}

	return GitHubActionStep(stepLines)
}

// ShouldUseParallelInstallation determines if parallel installation should be used
// based on the workflow configuration. Parallel installation is used when:
// - AWF binary needs to be installed (firewall enabled)
// - CLI needs to be installed (Copilot or Claude)
// - Docker images need to be downloaded
// - SRT is NOT enabled (SRT has sequential dependencies)
func ShouldUseParallelInstallation(workflowData *WorkflowData, engine CodingAgentEngine) bool {
	// Don't use parallel installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		return false
	}

	// Don't use parallel installation for SRT (has sequential dependencies)
	if isSRTEnabled(workflowData) {
		return false
	}

	// Use parallel installation if firewall is enabled (AWF binary needed)
	// and we're installing a CLI (Copilot or Claude)
	if isFirewallEnabled(workflowData) {
		engineID := engine.GetID()
		if engineID == "copilot" || engineID == "claude" {
			return true
		}
	}

	// Also use parallel if we have Docker images to download
	dockerImages := collectDockerImages(workflowData.Tools, workflowData)
	if len(dockerImages) > 0 && (isFirewallEnabled(workflowData) || engine.GetID() == "copilot" || engine.GetID() == "claude") {
		return true
	}

	return false
}

// GetParallelInstallConfig extracts the parallel installation configuration
// from the workflow data and engine configuration
func GetParallelInstallConfig(workflowData *WorkflowData, engine CodingAgentEngine) ParallelInstallConfig {
	config := ParallelInstallConfig{}

	// Get AWF version if firewall is enabled
	if isFirewallEnabled(workflowData) {
		agentConfig := getAgentConfig(workflowData)
		// Only install AWF if no custom command is specified
		if agentConfig == nil || agentConfig.Command == "" {
			firewallConfig := getFirewallConfig(workflowData)
			if firewallConfig != nil && firewallConfig.Version != "" {
				config.AWFVersion = firewallConfig.Version
			} else {
				config.AWFVersion = string(constants.DefaultFirewallVersion)
			}
		}
	}

	// Get CLI version based on engine
	engineID := engine.GetID()
	if engineID == "copilot" {
		version := string(constants.DefaultCopilotVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}
		// Only use parallel if installing globally (not for SRT local installation)
		if !isSRTEnabled(workflowData) {
			config.CopilotVersion = version
		}
	} else if engineID == "claude" {
		version := string(constants.DefaultClaudeCodeVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}
		config.ClaudeVersion = version
	}

	// Get Docker images
	config.DockerImages = collectDockerImages(workflowData.Tools, workflowData)

	return config
}
