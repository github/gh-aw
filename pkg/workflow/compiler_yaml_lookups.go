package workflow

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var compilerYamlLookupsLog = logger.New("workflow:compiler_yaml_lookups")

// gitDescribeSHAPattern matches git-describe output ending with -N-gSHA (pre-compiled for performance)
var gitDescribeSHAPattern = regexp.MustCompile(`-\d+-g([0-9a-f]+)$`)

// getVersionForSetup returns the agent version to inject as GH_AW_INFO_VERSION in setup steps.
// It mirrors getInstallationVersion but derives the engine ID from data fields (not from a
// CodingAgentEngine object), allowing it to be called in contexts where the engine object
// is not available (e.g. compiler_yaml_step_generation.go).
func getVersionForSetup(data *WorkflowData, registry *EngineRegistry) string {
	if data == nil {
		return ""
	}
	if data.EngineConfig != nil && data.EngineConfig.Version != "" {
		return data.EngineConfig.Version
	}
	engineID := ""
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		engineID = data.EngineConfig.ID
	} else if data.AI != "" {
		engineID = data.AI
	}
	switch engineID {
	case string(constants.CopilotEngine):
		return string(constants.DefaultCopilotVersion)
	case string(constants.ClaudeEngine):
		return string(constants.DefaultClaudeCodeVersion)
	case string(constants.CodexEngine):
		return string(constants.DefaultCodexVersion)
	case string(constants.GeminiEngine):
		return string(constants.DefaultGeminiVersion)
	case string(constants.PiEngine):
		return string(constants.DefaultPiVersion)
	default:
		return behaviorEngineDefaultVersion(engineID, registry)
	}
}

// behaviorEngineDefaultVersion returns the installation version declared by a
// behavior-defined engine definition, or "" when the engine is unknown or declares
// no installation version.
func behaviorEngineDefaultVersion(engineID string, registry *EngineRegistry) string {
	engine, err := registry.GetEngine(strings.ToLower(engineID))
	if err != nil {
		return ""
	}
	behaviorEngine, ok := engine.(*BehaviorDefinedEngine)
	if !ok {
		return ""
	}
	behavior := behaviorEngine.behavior()
	if behavior == nil || behavior.Installation == nil {
		return ""
	}
	return behavior.Installation.Version
}

// getAWFVersionForSetup returns the AWF runtime version to inject as
// GH_AW_INFO_AWF_VERSION in setup steps so all job-local OTel spans can
// attribute telemetry to the firewall runtime that executed the job.
func getAWFVersionForSetup(data *WorkflowData) string {
	if data == nil {
		return ""
	}
	firewallConfig := getFirewallConfig(data)
	if firewallConfig == nil || !firewallConfig.Enabled {
		return ""
	}
	if firewallConfig.Version != "" {
		return firewallConfig.Version
	}
	return string(constants.DefaultFirewallVersion)
}

// getInstallationVersion returns the version that will be installed for the given engine.
// This matches the logic in BuildStandardNpmEngineInstallSteps.
func getInstallationVersion(data *WorkflowData, engine CodingAgentEngine, registry *EngineRegistry) string {
	engineID := engine.GetID()
	compilerYamlLookupsLog.Printf("Getting installation version for engine: %s", engineID)

	// If version is specified in engine config, use it
	if data.EngineConfig != nil && data.EngineConfig.Version != "" {
		compilerYamlLookupsLog.Printf("Using engine config version: %s", data.EngineConfig.Version)
		return data.EngineConfig.Version
	}

	// Otherwise, use the default version for the engine
	switch engineID {
	case string(constants.CopilotEngine):
		return string(constants.DefaultCopilotVersion)
	case string(constants.ClaudeEngine):
		return string(constants.DefaultClaudeCodeVersion)
	case string(constants.CodexEngine):
		return string(constants.DefaultCodexVersion)
	case string(constants.PiEngine):
		return string(constants.DefaultPiVersion)
	default:
		if version := behaviorEngineDefaultVersion(engineID, registry); version != "" {
			return version
		}
		// Custom or unknown engines don't have a default version
		compilerYamlLookupsLog.Printf("No default version for custom engine: %s", engineID)
		return ""
	}
}

// getDefaultAgentModel returns the model display value to use when no explicit model is configured.
// For the copilot engine this matches the CopilotBYOKDefaultModel used in COPILOT_MODEL so that
// GH_AW_INFO_MODEL and COPILOT_MODEL agree on the same fallback.
// Returns "agent" for other known engines whose model is dynamically determined by the AI provider,
// or empty string for custom/unknown engines.
func getDefaultAgentModel(engineID string) string {
	switch engineID {
	case string(constants.CopilotEngine):
		return constants.CopilotBYOKDefaultModel
	case string(constants.ClaudeEngine), string(constants.GeminiEngine), string(constants.PiEngine):
		return constants.AgentDefaultModel
	case string(constants.CodexEngine):
		return constants.CodexDefaultModel
	default:
		return ""
	}
}

// getDefaultModelOverrideVar returns the enterprise override variable used for the engine's
// default model fallback when the GH_AW_MODEL_AGENT/DETECTION_* variable is unset.
func getDefaultModelOverrideVar(engineID string) string {
	switch engineID {
	case string(constants.CopilotEngine):
		return compilerenv.DefaultModelCopilot
	case string(constants.ClaudeEngine):
		return compilerenv.DefaultModelClaude
	case string(constants.CodexEngine):
		return compilerenv.DefaultModelCodex
	default:
		return ""
	}
}

// versionToGitRef converts a compiler version string to a valid git ref for use
// in actions/checkout ref: fields.
//
// The version string is typically produced by `git describe --tags --always --dirty`
// and may contain suffixes that are not valid git refs. This function normalises it:
//   - "dev" or empty → "" (no ref, checkout will use the repository default branch)
//   - "v1.2.3-60-ge284d1e" → "e284d1e" (extract SHA from git-describe output)
//   - "v1.2.3-60-ge284d1e-dirty" → "e284d1e" (strip -dirty, then extract SHA)
//   - "v1.2.3-dirty" → "v1.2.3" (strip -dirty, valid tag)
//   - "v1.2.3" → "v1.2.3" (valid tag, used as-is)
//   - "e284d1e" → "e284d1e" (plain short SHA, used as-is)
func versionToGitRef(version string) string {
	compilerYamlLookupsLog.Printf("Converting version to git ref: %s", version)
	if version == "" || version == "dev" {
		return ""
	}
	// Strip optional -dirty suffix (appended by `git describe --dirty`)
	clean := strings.TrimSuffix(version, "-dirty")
	// If the version looks like `git describe` output with -N-gSHA, extract the SHA.
	// Pattern: anything ending with -<digits>-g<hexchars>
	if m := gitDescribeSHAPattern.FindStringSubmatch(clean); m != nil {
		compilerYamlLookupsLog.Printf("Extracted SHA from git-describe version: %s -> %s", version, m[1])
		return m[1]
	}
	compilerYamlLookupsLog.Printf("Using version as git ref: %s -> %s", version, clean)
	return clean
}

// collectEngineVersionsForMetadata returns engine version metadata for gh-aw lock files.
// It includes only engines that are active in the current workflow, applies explicit version
// overrides for those engines, and includes copilot-sdk only when enabled on an active copilot engine.
func (c *Compiler) collectEngineVersionsForMetadata(data *WorkflowData) map[string]string {
	if data == nil {
		return map[string]string{}
	}

	versions := map[string]string{
		string(constants.CopilotEngine): string(constants.DefaultCopilotVersion),
		string(constants.ClaudeEngine):  string(constants.DefaultClaudeCodeVersion),
		string(constants.CodexEngine):   string(constants.DefaultCodexVersion),
		string(constants.GeminiEngine):  string(constants.DefaultGeminiVersion),
		string(constants.PiEngine):      string(constants.DefaultPiVersion),
	}

	mainEngineID := strings.TrimSpace(ResolveEngineID(data))
	if mainEngineID == "" {
		mainEngineID = string(constants.DefaultEngine)
	}
	activeEngineIDs := map[string]struct{}{mainEngineID: {}}

	applyMetadataEngineVersionOverrides(versions, data.EngineConfig, mainEngineID)
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		detectionConfig := data.SafeOutputs.ThreatDetection.EngineConfig
		detectionEngineID := c.getThreatDetectionEngineID(data)
		activeEngineIDs[detectionEngineID] = struct{}{}
		applyMetadataEngineVersionOverrides(versions, detectionConfig, detectionEngineID)
	}

	filteredVersions := make(map[string]string, len(activeEngineIDs)+1)
	for engineID := range activeEngineIDs {
		version := strings.TrimSpace(versions[engineID])
		if version == "" {
			// Behavior-defined engines declare their version in the engine definition.
			version = strings.TrimSpace(behaviorEngineDefaultVersion(engineID, c.engineRegistry))
		}
		if version != "" {
			filteredVersions[engineID] = version
		}
	}

	if isCopilotSDKEnabledForActiveEngine(data, mainEngineID) {
		filteredVersions["copilot-sdk"] = string(constants.DefaultCopilotSDKVersion)
	}

	return filteredVersions
}

func applyMetadataEngineVersionOverrides(versions map[string]string, engineConfig *EngineConfig, fallbackEngineID string) {
	if engineConfig == nil {
		return
	}

	engineID := strings.TrimSpace(engineConfig.ID)
	if engineID == "" {
		engineID = strings.TrimSpace(fallbackEngineID)
	}
	if engineID != "" && strings.TrimSpace(engineConfig.Version) != "" {
		versions[engineID] = strings.TrimSpace(engineConfig.Version)
	}
}

func isCopilotSDKEnabledForActiveEngine(data *WorkflowData, mainEngineID string) bool {
	if mainEngineID == string(constants.CopilotEngine) && data.EngineConfig != nil && data.EngineConfig.CopilotSDK {
		return true
	}

	if !IsDetectionJobEnabled(data.SafeOutputs) || data.SafeOutputs == nil || data.SafeOutputs.ThreatDetection == nil {
		return false
	}

	detectionConfig := data.SafeOutputs.ThreatDetection.EngineConfig
	if detectionConfig == nil {
		return false
	}

	detectionEngineID := strings.TrimSpace(detectionConfig.ID)
	if detectionEngineID == "" {
		detectionEngineID = mainEngineID
	}
	return detectionEngineID == string(constants.CopilotEngine) && detectionConfig.CopilotSDK
}

// resolveAgentImageRunnerIdentifier returns a stable identifier for the configured runs-on value.
// For string values it returns the value directly; for array/object values it returns JSON.
func resolveAgentImageRunnerIdentifier(frontmatter map[string]any) string {
	if frontmatter == nil {
		return ""
	}
	runsOn, exists := frontmatter["runs-on"]
	if !exists || runsOn == nil {
		return ""
	}
	if rawRunner, ok := runsOn.(string); ok {
		return strings.TrimSpace(rawRunner)
	}

	serialized, err := json.Marshal(runsOn)
	if err != nil {
		return ""
	}
	return string(serialized)
}
