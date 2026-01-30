package workflow

import (
	"fmt"
	"strings"

	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
)

var parallelInstallLog = logger.New("workflow:parallel_installation")

// CLIInstallMethod defines how a CLI should be installed
type CLIInstallMethod string

const (
	CLIInstallMethodScript   CLIInstallMethod = "script"   // Use installer script from URL
	CLIInstallMethodNpm      CLIInstallMethod = "npm"      // Use npm install
	CLIInstallMethodDownload CLIInstallMethod = "download" // Direct binary download
)

// CLIInstallInfo contains information about how to install a CLI
type CLIInstallInfo struct {
	Method      CLIInstallMethod // Installation method
	Version     string           // Version to install
	PackageName string           // NPM package name (for npm method)
	ScriptURL   string           // Installer script URL (for script method)
	BinaryURL   string           // Binary download URL (for download method)
	VerifyCmd   string           // Command to verify installation (e.g., "copilot --version")
}

// ParallelInstallConfig holds configuration for parallel installation
type ParallelInstallConfig struct {
	AWFVersion   string          // AWF binary version to install (empty to skip)
	CLIInfo      *CLIInstallInfo // CLI installation info (nil to skip)
	DockerImages []string        // Docker images to download (empty to skip)
}

// generateParallelInstallationStep generates a single step that installs dependencies in parallel
// This parallelizes AWF binary installation, CLI installation, and Docker image downloads
// to reduce sequential execution time by 8-12 seconds.
func generateParallelInstallationStep(config ParallelInstallConfig) GitHubActionStep {
	if config.AWFVersion == "" && config.CLIInfo == nil && len(config.DockerImages) == 0 {
		parallelInstallLog.Print("No parallel installations configured, skipping")
		return GitHubActionStep([]string{})
	}

	// Count how many operations will run in parallel
	operationCount := 0
	if config.AWFVersion != "" {
		operationCount++
	}
	if config.CLIInfo != nil {
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

	// Add CLI installation arguments based on method
	if config.CLIInfo != nil {
		switch config.CLIInfo.Method {
		case CLIInstallMethodScript:
			// Pass script URL and version
			stepLines = append(stepLines, fmt.Sprintf("            --cli-script %s \\", config.CLIInfo.ScriptURL))
			if config.CLIInfo.Version != "" {
				stepLines = append(stepLines, fmt.Sprintf("            --cli-version %s \\", config.CLIInfo.Version))
			}
			if config.CLIInfo.VerifyCmd != "" {
				stepLines = append(stepLines, fmt.Sprintf("            --cli-verify %q \\", config.CLIInfo.VerifyCmd))
			}
		case CLIInstallMethodNpm:
			// Pass npm package and version
			stepLines = append(stepLines, fmt.Sprintf("            --cli-npm %s \\", config.CLIInfo.PackageName))
			if config.CLIInfo.Version != "" {
				stepLines = append(stepLines, fmt.Sprintf("            --cli-version %s \\", config.CLIInfo.Version))
			}
			if config.CLIInfo.VerifyCmd != "" {
				stepLines = append(stepLines, fmt.Sprintf("            --cli-verify %q \\", config.CLIInfo.VerifyCmd))
			}
		case CLIInstallMethodDownload:
			// Pass binary URL
			stepLines = append(stepLines, fmt.Sprintf("            --cli-download %s \\", config.CLIInfo.BinaryURL))
			if config.CLIInfo.VerifyCmd != "" {
				stepLines = append(stepLines, fmt.Sprintf("            --cli-verify %q \\", config.CLIInfo.VerifyCmd))
			}
		}
	}

	// Add Docker images argument
	if len(config.DockerImages) > 0 {
		var dockerArgs strings.Builder
		dockerArgs.WriteString("            --docker")
		for _, image := range config.DockerImages {
			fmt.Fprintf(&dockerArgs, " %s", image)
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
// - CLI needs to be installed (Copilot, Claude, or Codex)
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
	// and we're installing a CLI (Copilot, Claude, or Codex)
	if isFirewallEnabled(workflowData) {
		engineID := engine.GetID()
		if engineID == "copilot" || engineID == "claude" || engineID == "codex" {
			return true
		}
	}

	// Also use parallel if we have Docker images to download
	dockerImages := collectDockerImages(workflowData.Tools, workflowData)
	engineID := engine.GetID()
	if len(dockerImages) > 0 && (isFirewallEnabled(workflowData) || engineID == "copilot" || engineID == "claude" || engineID == "codex") {
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

	// Get CLI installation info based on engine
	engineID := engine.GetID()
	switch engineID {
	case "copilot":
		version := string(constants.DefaultCopilotVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}
		// Only use parallel if installing globally (not for SRT local installation)
		if !isSRTEnabled(workflowData) {
			config.CLIInfo = &CLIInstallInfo{
				Method:    CLIInstallMethodScript,
				Version:   version,
				ScriptURL: "https://raw.githubusercontent.com/github/copilot-cli/main/install.sh",
				VerifyCmd: "copilot --version",
			}
		}
	case "claude":
		version := string(constants.DefaultClaudeCodeVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}
		config.CLIInfo = &CLIInstallInfo{
			Method:      CLIInstallMethodNpm,
			Version:     version,
			PackageName: "@anthropic-ai/claude-code",
			VerifyCmd:   "claude-code --version",
		}
	case "codex":
		version := string(constants.DefaultCodexVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}
		config.CLIInfo = &CLIInstallInfo{
			Method:      CLIInstallMethodNpm,
			Version:     version,
			PackageName: "@openai/codex",
			VerifyCmd:   "codex --version",
		}
	}

	// Get Docker images
	config.DockerImages = collectDockerImages(workflowData.Tools, workflowData)

	return config
}
