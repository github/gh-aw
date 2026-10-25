package workflow

import (
	"maps"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var runtimeSetupLog = logger.New("workflow:runtime_setup")

func cloneRuntimeRequirements(requirements []RuntimeRequirement) []RuntimeRequirement {
	if len(requirements) == 0 {
		return nil
	}
	cloned := make([]RuntimeRequirement, len(requirements))
	copy(cloned, requirements)
	for i, req := range cloned {
		if req.ExtraFields == nil {
			continue
		}
		extraFields := make(map[string]any, len(req.ExtraFields))
		maps.Copy(extraFields, req.ExtraFields)
		cloned[i].ExtraFields = extraFields
	}
	return cloned
}

func detectRuntimeRequirementsCached(workflowData *WorkflowData) []RuntimeRequirement {
	if workflowData == nil {
		return nil
	}
	if workflowData.CachedRuntimeRequirementsSet {
		return cloneRuntimeRequirements(workflowData.CachedRuntimeRequirements)
	}
	requirements := DetectRuntimeRequirements(workflowData)
	workflowData.CachedRuntimeRequirements = cloneRuntimeRequirements(requirements)
	workflowData.CachedRuntimeRequirementsSet = true
	return cloneRuntimeRequirements(workflowData.CachedRuntimeRequirements)
}

// DetectRuntimeRequirements analyzes workflow data to detect required runtimes
func DetectRuntimeRequirements(workflowData *WorkflowData) []RuntimeRequirement {
	runtimeSetupLog.Print("Detecting runtime requirements from workflow data")
	requirements := make(map[string]*RuntimeRequirement) // map of runtime ID -> requirement

	// Detect from custom steps
	if workflowData.CustomSteps != "" {
		detectFromCustomSteps(workflowData.CustomSteps, requirements)
	}

	// Detect from MCP server configurations
	if workflowData.ParsedTools != nil {
		detectFromMCPConfigs(workflowData.ParsedTools, requirements)
	}
	if workflowData.MCPScripts != nil {
		detectFromMCPScripts(workflowData.MCPScripts, requirements)
	}
	detectFromInlineEngineDriver(workflowData, requirements)

	detectNodeRuntimeRequirements(workflowData, requirements)
	finishRuntimeRequirements(workflowData, requirements)
	return sortedRuntimeRequirements(requirements)
}

func detectNodeRuntimeRequirements(workflowData *WorkflowData, requirements map[string]*RuntimeRequirement) {
	// When using a custom image runner, ensure Node.js is set up.
	// Standard GitHub-hosted runners (ubuntu-*, windows-*) have Node.js pre-installed,
	// but custom image runners (self-hosted, enterprise runners, non-standard labels) may not.
	// Node.js is required for gh-aw scripts such as start_safe_outputs_server.sh and
	// start_mcp_scripts_server.sh that invoke `node` directly.
	if isCustomImageRunner(workflowData.RunsOn) {
		runtimeSetupLog.Printf("Custom image runner detected (%q), ensuring Node.js is set up", workflowData.RunsOn)
		nodeRuntime := findRuntimeByID("node")
		if nodeRuntime != nil {
			updateRequiredRuntime(nodeRuntime, "", requirements)
		}
	}

	// When a custom harness script is configured for an engine that currently supports
	// harness wrappers, require Node.js runtime setup with the default version so workflows
	// consistently execute the harness with Node 24.
	if requiresNodeForEngineHarness(workflowData) {
		nodeRuntime := findRuntimeByID("node")
		if nodeRuntime != nil {
			updateRequiredRuntime(nodeRuntime, string(constants.DefaultNodeVersion), requirements)
		}
	}

	// When using a TypeScript Copilot SDK driver (.ts/.mts extension), require Node 24.
	// Node 24 runs TypeScript natively; earlier versions do not support this.
	// This only applies to file-extension-based driver detection, not to engine.command
	// configurations (e.g. "ts-node driver.ts") which manage their own toolchain.
	if requiresNode24ForTypeScriptSDKDriver(workflowData) {
		nodeRuntime := findRuntimeByID("node")
		if nodeRuntime != nil {
			updateRequiredRuntime(nodeRuntime, string(constants.DefaultNodeVersion), requirements)
		}
	}
}

func finishRuntimeRequirements(workflowData *WorkflowData, requirements map[string]*RuntimeRequirement) {
	// Detect runtimes required by LSP server configurations.
	// Each known LSP server declares the runtime it needs (e.g. "go" for gopls,
	// "ruby" for solargraph). Feeding these through the runtime manager ensures
	// the runtime is set up via a properly SHA-pinned setup action rather than
	// relying on whatever version happens to be pre-installed on the runner.
	if len(workflowData.LSP) > 0 {
		detectFromLSPServers(workflowData.LSP, requirements)
	}

	// Apply runtime overrides from frontmatter
	if workflowData.Runtimes != nil {
		applyRuntimeOverrides(workflowData.Runtimes, requirements)
	}

	// Add Python as dependency when uv is detected (uv requires Python)
	if _, hasUV := requirements["uv"]; hasUV {
		if _, hasPython := requirements["python"]; !hasPython {
			runtimeSetupLog.Print("UV detected without Python, automatically adding Python runtime")
			pythonRuntime := findRuntimeByID("python")
			if pythonRuntime != nil {
				updateRequiredRuntime(pythonRuntime, "", requirements)
			}
		}
	}

	// NOTE: We intentionally DO NOT filter out runtimes that already have setup actions.
	// Instead, we will deduplicate the setup actions from CustomSteps in the compiler.
	// This ensures runtime setup steps are always added BEFORE custom steps.
	// The deduplication happens in compiler_yaml.go to remove duplicate setup actions from custom steps.
}

func sortedRuntimeRequirements(requirements map[string]*RuntimeRequirement) []RuntimeRequirement {
	// Convert map to sorted slice (alphabetically by runtime ID)
	var result []RuntimeRequirement
	runtimeIDs := sliceutil.SortedKeys(requirements)

	for _, id := range runtimeIDs {
		result = append(result, *requirements[id])
	}

	if runtimeSetupLog.Enabled() {
		runtimeSetupLog.Printf("Detected %d runtime requirements: %v", len(result), runtimeIDs)
	}
	return result
}

// requiresNodeForEngineHarness returns true when workflow runtime setup must ensure Node.js
// for engine.harness execution based on current engine wrapper support.
func requiresNodeForEngineHarness(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.HarnessScript == "" {
		return false
	}

	engineID := workflowData.EngineConfig.ID
	if engineID == "" {
		engineID = workflowData.AI
	}
	if engineID == "" {
		engineID = string(constants.DefaultEngine)
	}

	// Both Copilot and Claude consume engine.harness in execution command generation.
	// Claude is excluded here because Node.js is already provisioned as part of its
	// installation steps (GenerateNpmInstallSteps with includeNodeSetup=true), so no
	// additional Node runtime requirement is needed for custom harness execution.
	return strings.EqualFold(engineID, string(constants.CopilotEngine))
}

// requiresNode24ForTypeScriptSDKDriver returns true when a Copilot SDK driver is a TypeScript
// file (.ts or .mts) detected by extension. Node 24 runs TypeScript natively; the runtime
// setup ensures the correct version is provisioned.
//
// This does not apply when engine.command is set (e.g., "ts-node driver.ts"), since those
// configurations manage their own TypeScript toolchain independently.
func requiresNode24ForTypeScriptSDKDriver(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return false
	}
	if !workflowData.EngineConfig.CopilotSDK {
		return false
	}
	// engine.command takes precedence; the user manages the toolchain explicitly.
	if workflowData.EngineConfig.Command != "" {
		return false
	}
	ext := filepath.Ext(workflowData.EngineConfig.Driver)
	return strings.EqualFold(ext, ".ts") || strings.EqualFold(ext, ".mts")
}

func detectFromInlineEngineDriver(workflowData *WorkflowData, requirements map[string]*RuntimeRequirement) {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.InlineDriver == nil {
		return
	}

	runtime := findRuntimeByID(workflowData.EngineConfig.InlineDriver.Runtime)
	if runtime != nil {
		updateRequiredRuntime(runtime, "", requirements)
	}
}

// detectFromCustomSteps scans custom steps YAML for runtime commands
func detectFromCustomSteps(customSteps string, requirements map[string]*RuntimeRequirement) {
	workflowLog.Print("Scanning custom steps for runtime commands")
	lines := strings.SplitSeq(customSteps, "\n")
	for line := range lines {
		// Look for run: commands
		if strings.Contains(line, "run:") {
			// Extract the command part
			parts := strings.SplitN(line, "run:", 2)
			if len(parts) == 2 {
				cmdLine := strings.TrimSpace(parts[1])
				detectRuntimeFromCommand(cmdLine, requirements)
			}
		}
	}
}

// detectRuntimeFromCommand scans a command string for runtime indicators
func detectRuntimeFromCommand(cmdLine string, requirements map[string]*RuntimeRequirement) {
	// Split by common shell delimiters and operators
	words := strings.FieldsFunc(cmdLine, func(r rune) bool {
		return r == ' ' || r == '|' || r == '&' || r == ';' || r == '\n' || r == '\t'
	})

	// Special handling for "gh aw" command pair.
	for i := range len(words) - 1 {
		if strings.EqualFold(words[i], "gh") && strings.EqualFold(words[i+1], "aw") {
			if runtime := findRuntimeByID("gh-aw"); runtime != nil {
				updateRequiredRuntime(runtime, getDefaultGhAWRuntimeVersion(), requirements)
			}
			break
		}
	}

	for _, word := range words {
		// Check if this word matches a known command
		if runtime, exists := commandToRuntime[word]; exists {
			// Special handling for "uv pip" to avoid detecting pip separately
			if word == "pip" || word == "pip3" {
				// Check if "uv" appears before this pip command
				uvIndex := -1
				pipIndex := -1
				for i, w := range words {
					if w == "uv" {
						uvIndex = i
					}
					if w == word {
						pipIndex = i
						break
					}
				}
				if uvIndex >= 0 && uvIndex < pipIndex {
					// This is "uv pip", skip pip detection
					continue
				}
			}

			updateRequiredRuntime(runtime, "", requirements)
		}
	}
}

// getDefaultGhAWRuntimeVersion returns the default gh-aw runtime version to inject.
// Release builds use the released compiler version; dev builds use the current build version.
func getDefaultGhAWRuntimeVersion() string {
	version := GetVersion()
	if version == "" {
		return "dev"
	}
	return version
}

// detectFromMCPConfigs scans MCP server configurations for runtime commands
func detectFromMCPConfigs(tools *ToolsConfig, requirements map[string]*RuntimeRequirement) {
	if tools == nil {
		return
	}

	allTools := tools.ToMap()
	workflowLog.Printf("Scanning %d MCP configurations for runtime commands", len(allTools))

	// Scan custom MCP tools for runtime commands
	// Skip containerized MCP servers as they don't need host runtime setup
	for _, tool := range tools.Custom {
		// Skip if the MCP server is containerized (has Container field set or Type is "docker")
		if tool.Container != "" || tool.Type == "docker" {
			runtimeSetupLog.Printf("Skipping runtime detection for containerized MCP server (container=%s, type=%s)", tool.Container, tool.Type)
			continue
		}

		// For non-containerized custom MCP servers, check the Command field
		if tool.Command != "" {
			if runtime, found := commandToRuntime[tool.Command]; found {
				updateRequiredRuntime(runtime, "", requirements)
			}
		}

	}
}

// detectFromMCPScripts scans mcp-scripts tool definitions for language runtimes.
func detectFromMCPScripts(mcpScripts *MCPScriptsConfig, requirements map[string]*RuntimeRequirement) {
	if mcpScripts == nil {
		return
	}

	for _, tool := range mcpScripts.Tools {
		switch {
		case tool.Script != "":
			if runtime := findRuntimeByID("node"); runtime != nil {
				updateRequiredRuntime(runtime, "", requirements)
			}
		case tool.Py != "":
			if runtime := findRuntimeByID("python"); runtime != nil {
				updateRequiredRuntime(runtime, "", requirements)
			}
		case tool.Go != "":
			if runtime := findRuntimeByID("go"); runtime != nil {
				updateRequiredRuntime(runtime, "", requirements)
			}
		}
	}
}

// detectFromLSPServers adds runtime requirements for the runtimes needed by configured
// LSP servers. Each entry in lspInstallSpecs declares the runtime ID it needs (e.g.
// "go" for gopls, "ruby" for solargraph, "node" for npm-based servers). Feeding these
// through the runtime manager ensures that the runtime is set up via a properly
// SHA-pinned setup action (e.g. actions/setup-go, ruby/setup-ruby) and with a pinned
// version, rather than relying on whatever happens to be pre-installed on the runner.
func detectFromLSPServers(lsp map[string]LSPServerConfig, requirements map[string]*RuntimeRequirement) {
	if len(lsp) == 0 {
		return
	}
	manager := NewLSPManager(lsp)
	for _, req := range manager.RuntimeRequirements() {
		runtimeSetupLog.Printf("LSP server requires runtime: %s", req.Runtime.ID)
		updateRequiredRuntime(req.Runtime, req.Version, requirements)
	}
}

// updateRequiredRuntime updates the version requirement, choosing the highest version
func updateRequiredRuntime(runtime *Runtime, newVersion string, requirements map[string]*RuntimeRequirement) {
	existing, exists := requirements[runtime.ID]

	if !exists {
		runtimeSetupLog.Printf("Adding new runtime requirement: %s (version=%s)", runtime.ID, newVersion)
		requirements[runtime.ID] = &RuntimeRequirement{
			Runtime:  runtime,
			Version:  newVersion,
			Cooldown: true,
		}
		return
	}

	// If new version is empty, keep existing
	if newVersion == "" {
		return
	}

	// If existing version is empty, use new version
	if existing.Version == "" {
		existing.Version = newVersion
		return
	}

	// Compare versions and keep the higher one
	if semverutil.Compare(newVersion, existing.Version) > 0 {
		existing.Version = newVersion
	}
}

// standardGitHubHostedRunners is the allowlist of known runner labels that
// ship with Node.js pre-installed. Only exact labels (case-insensitive) listed here are
// considered standard; everything else is treated as a custom image runner.
//
// Note: "ubuntu-slim" is gh-aw's own default framework runner image and it has Node.js
// pre-installed, so it is included in this allowlist alongside the official GitHub-hosted labels.
//
// Sources:
//   - https://docs.github.com/en/actions/using-github-hosted-runners/using-github-hosted-runners/about-github-hosted-runners
//   - https://docs.github.com/en/actions/using-github-hosted-runners/using-larger-runners/about-larger-runners
var standardGitHubHostedRunners = map[string]bool{
	// gh-aw default framework runner (Node.js pre-installed)
	"ubuntu-slim": true,
	// Linux
	"ubuntu-latest": true,
	"ubuntu-24.04":  true,
	"ubuntu-22.04":  true,
	"ubuntu-20.04":  true,
	// Linux ARM
	"ubuntu-latest-arm": true,
	"ubuntu-24.04-arm":  true,
	"ubuntu-22.04-arm":  true,
	// Windows
	"windows-latest": true,
	"windows-2025":   true,
	"windows-2022":   true,
	"windows-2019":   true,
	// Windows ARM
	"windows-latest-arm": true,
	"windows-11-arm":     true,
}

// isCustomImageRunner returns true if the runs-on configuration indicates a non-standard
// runner (custom image, self-hosted runner, or runner group). Custom image runners may not
// have Node.js pre-installed, so the compiler ensures Node.js is set up.
//
// Only the labels in standardGitHubHostedRunners are considered standard; everything else
// (e.g. "self-hosted", enterprise labels, GPU runner labels) is treated as custom.
//
// The runsOn parameter is a YAML string in the form produced by extractTopLevelYAMLSection,
// for example:
//   - "runs-on: ubuntu-latest"       → standard runner → returns false
//   - "runs-on: ubuntu-22.04"        → standard runner → returns false
//   - "runs-on: ubuntu-slim"         → standard runner → returns false
//   - "runs-on: self-hosted"         → custom runner   → returns true
//   - "runs-on:\n- self-hosted\n..."  → array form      → returns true
//   - "runs-on:\n  group: ..."        → object form     → returns true
func isCustomImageRunner(runsOn string) bool {
	if runsOn == "" {
		// Empty means the default "ubuntu-latest" will be applied — not a custom runner.
		return false
	}

	const keyPrefix = "runs-on: "
	if value, ok := strings.CutPrefix(runsOn, keyPrefix); ok {
		// Single-line value: check the label against the known-standard allowlist.
		value = strings.TrimSpace(strings.ToLower(value))
		return !standardGitHubHostedRunners[value]
	}

	// Multi-line value (array or object form) — always treat as custom image runner.
	return true
}
