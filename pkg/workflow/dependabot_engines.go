//go:build !js && !wasm

package workflow

import (
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var dependabotEnginesLog = logger.New("workflow:dependabot_engines")

// extractEnginePackages is manifest-only: engine installations must not change the
// general npx validation surface. Existing command dependencies retain precedence.
func (c *Compiler) extractEnginePackages(data *WorkflowData, ecosystem string) []string {
	if data == nil {
		return nil
	}
	packages := c.extractMainEnginePackages(data, ecosystem)
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		engineID := c.manifestDetectionEngineID(data)
		dependabotEnginesLog.Printf("Including detection engine packages for manifest: ecosystem=%s engineID=%s", ecosystem, engineID)
		if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
			packages = append(packages, c.extractMainEnginePackages(buildExternalDetectorWorkflowData(data, engineID), ecosystem)...)
		} else {
			config := resolveExternalDetectorEngineConfig(data, engineID)
			resetDetectionEngineTaskSettings(config)
			packages = append(packages, c.extractMainEnginePackages(&WorkflowData{
				AI: engineID, EngineConfig: config,
			}, ecosystem)...)
		}
	}
	return packages
}

func (c *Compiler) manifestDetectionEngineID(data *WorkflowData) string {
	if data.engineDefinition == nil {
		return c.getThreatDetectionEngineID(data)
	}
	// Imports are scoped to one workflow. Restore only that workflow's metadata
	// when resolving its detector after another workflow reset the catalog.
	dependabotEnginesLog.Printf("Restoring engine definition %q in a scoped catalog copy for detection resolution", data.engineDefinition.ID)
	resolver := *c
	catalog := *c.engineCatalog
	catalog.definitions = maps.Clone(catalog.definitions)
	catalog.definitions[data.engineDefinition.ID] = data.engineDefinition
	resolver.engineCatalog = &catalog
	return resolver.getThreatDetectionEngineID(data)
}

func (c *Compiler) extractMainEnginePackages(data *WorkflowData, ecosystem string) []string {
	engineID := ResolveEngineID(data)
	if engineID == "" {
		return nil
	}
	def := data.engineDefinition
	if def == nil {
		def = c.engineCatalog.Get(engineID)
	}
	runtimeID := engineID
	if def != nil && def.RuntimeID != "" {
		runtimeID = def.RuntimeID
	} else if engine, err := c.engineRegistry.GetEngineByPrefix(engineID); err == nil {
		runtimeID = engine.GetID()
	}
	dependabotEnginesLog.Printf("Resolved manifest runtime for engine: engineID=%s runtimeID=%s", engineID, runtimeID)
	var packages []string
	if data.EngineConfig == nil || data.EngineConfig.Command == "" {
		install := c.engineInstallationForManifest(def, runtimeID)
		if pkg := enginePackageForManifest(install, data.EngineConfig, ecosystem); pkg != "" {
			packages = append(packages, pkg)
		}
	}
	if runtimeID == "copilot" {
		packages = append(packages, extractCopilotSDKPackages(data, ecosystem)...)
	}
	return packages
}

func (c *Compiler) engineInstallationForManifest(def *EngineDefinition, runtimeID string) *EngineInstallationDefinition {
	if def != nil && def.Behaviors != nil {
		return def.Behaviors.Installation
	}
	// Copilot is downloaded as a release binary, but its npm package
	// represents the same CLI release for dependency alerts.
	name := ""
	switch runtimeID {
	case "claude":
		name = "@anthropic-ai/claude-code"
	case "codex":
		name = "@openai/codex"
	case "copilot":
		name = "@github/copilot"
	case "gemini":
		name = "@google/gemini-cli"
	case "pi":
		name = "@earendil-works/pi-coding-agent"
	}
	if name == "" {
		return nil
	}
	return &EngineInstallationDefinition{
		PackageManager: "npm", PackageName: name,
		Version: getVersionForSetup(&WorkflowData{AI: runtimeID}, c.engineRegistry),
	}
}

func enginePackageForManifest(install *EngineInstallationDefinition, config *EngineConfig, ecosystem string) string {
	if install == nil || install.PackageManager != ecosystem || install.PackageName == "" {
		return ""
	}
	version := install.Version
	if config != nil && config.Version != "" {
		version = config.Version
	}
	if strings.Contains(install.PackageName+version, "${{") {
		return ""
	}
	separator := "@"
	switch ecosystem {
	case "npm", "go":
	case "pip":
		separator = "=="
	default:
		return ""
	}
	if version == "" {
		return install.PackageName
	}
	return install.PackageName + separator + version
}

func extractCopilotSDKPackages(data *WorkflowData, ecosystem string) []string {
	// Reuse SDK installation selection (including inline/language drivers).
	// engine.version controls the CLI, not the independently pinned SDK.
	command := strings.Join(buildCopilotSDKInstallStep(data), "\n")
	switch ecosystem {
	case "npm":
		var installCommands []string
		for line := range strings.SplitSeq(command, "\n") {
			if strings.Contains(line, "npm install ") {
				installCommands = append(installCommands, line)
			}
		}
		extractor := PackageExtractor{CommandNames: []string{"npm"}, RequiredSubcommands: []string{"install"}, TrimSuffixes: "&|;"}
		return extractor.ExtractPackages(strings.Join(installCommands, "\n"))
	case "pip":
		// The SDK command has a --target argument, which the general
		// pip extractor does not distinguish from a package.
		var packages []string
		for token := range strings.FieldsSeq(command) {
			if strings.HasPrefix(token, "github-copilot-sdk==") {
				packages = append(packages, token)
			}
		}
		return packages
	case "go":
		return extractGoFromCommands(command)
	}
	return nil
}
