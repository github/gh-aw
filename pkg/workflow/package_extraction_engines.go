package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// engineInstallations includes both the agent and the independently installed
// detection engine, using the same configuration inheritance as execution.
func (c *Compiler) engineInstallations(data *WorkflowData) []EngineInstallationDefinition {
	if data == nil {
		return nil
	}
	if data.engineInstallationDependencies != nil {
		return data.engineInstallationDependencies
	}
	installs := []EngineInstallationDefinition{}
	if install := c.engineInstallation(ResolveEngineID(data), data.EngineConfig); install != nil {
		installs = append(installs, *install)
	}
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		id := c.getThreatDetectionEngineID(data)
		if install := c.engineInstallation(id, resolveExternalDetectorEngineConfig(data, id)); install != nil {
			installs = append(installs, *install)
		}
	}
	return installs
}

func (c *Compiler) engineInstallation(id string, config *EngineConfig) *EngineInstallationDefinition {
	if config != nil && config.Command != "" {
		return nil
	}
	var install EngineInstallationDefinition
	var definition *EngineDefinition
	if c.engineCatalog != nil {
		definition = c.engineCatalog.Get(id)
	}
	if definition != nil && definition.Behaviors != nil {
		if definition.Behaviors.Installation == nil {
			return nil
		}
		install = *definition.Behaviors.Installation
	} else {
		if definition != nil && definition.RuntimeID != "" {
			id = definition.RuntimeID
		}
		engine, err := c.getAgenticEngine(id)
		if err != nil {
			return nil
		}
		install.PackageManager = "npm"
		switch engine.GetID() {
		case "copilot":
			install.PackageName, install.Version = "@github/copilot", string(constants.DefaultCopilotVersion)
		case "claude":
			install.PackageName, install.Version = "@anthropic-ai/claude-code", string(constants.DefaultClaudeCodeVersion)
		case "codex":
			install.PackageName, install.Version = "@openai/codex", string(constants.DefaultCodexVersion)
		case "pi":
			install.PackageName, install.Version = "@earendil-works/pi-coding-agent", string(constants.DefaultPiVersion)
		case "gemini":
			install.PackageName, install.Version = "@google/gemini-cli", string(constants.DefaultGeminiVersion)
		default:
			return nil
		}
	}
	if definition != nil && definition.Version != "" {
		install.Version = definition.Version
	}
	if config != nil && config.Version != "" {
		install.Version = config.Version
	}
	// Runtime expressions cannot be resolved into a static Dependabot manifest.
	if install.PackageName == "" || strings.ContainsAny(install.PackageName+install.Version, "$`") {
		return nil
	}
	return &install
}

func (c *Compiler) enginePackages(data *WorkflowData, ecosystem string) []string {
	var packages []string
	for _, install := range c.engineInstallations(data) {
		manager := install.PackageManager
		if manager == "uv" {
			manager = "pip"
		}
		if manager != ecosystem {
			continue
		}
		pkg := install.PackageName
		if install.Version != "" {
			if ecosystem == "pip" {
				if strings.ContainsAny(install.Version[:1], "=<>!~") {
					pkg += install.Version
				} else {
					pkg += "==" + install.Version
				}
			} else {
				pkg += "@" + install.Version
			}
		}
		packages = append(packages, pkg)
	}
	return packages
}
