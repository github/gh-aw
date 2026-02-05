package workflow

import (
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var runtimeSetupLog = logger.New("workflow:runtime_setup")

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

	// Detect from engine requirements
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Steps) > 0 {
		detectFromEngineSteps(workflowData.EngineConfig.Steps, requirements)
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

	// Convert map to sorted slice (alphabetically by runtime ID)
	var result []RuntimeRequirement
	var runtimeIDs []string
	for id := range requirements {
		runtimeIDs = append(runtimeIDs, id)
	}
	sort.Strings(runtimeIDs)

	for _, id := range runtimeIDs {
		result = append(result, *requirements[id])
	}

	if runtimeSetupLog.Enabled() {
		runtimeSetupLog.Printf("Detected %d runtime requirements: %v", len(result), runtimeIDs)
	}
	return result
}

// detectFromCustomSteps scans custom steps YAML for runtime commands
func detectFromCustomSteps(customSteps string, requirements map[string]*RuntimeRequirement) {
	log.Print("Scanning custom steps for runtime commands")
	lines := strings.Split(customSteps, "\n")
	for _, line := range lines {
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

// detectFromMCPConfigs scans MCP server configurations for runtime commands
func detectFromMCPConfigs(tools *ToolsConfig, requirements map[string]*RuntimeRequirement) {
	if tools == nil {
		return
	}

	allTools := tools.ToMap()
	log.Printf("Scanning %d MCP configurations for runtime commands", len(allTools))

	// Note: Serena and other built-in MCP servers now run in containers and do not
	// require runtime detection. Language services are provided inside the containers.
	// EXCEPTION: Serena in local mode uses uvx and requires uv runtime + language runtimes

	// Check if Serena is in local mode - if so, add uvx runtime and language runtimes
	if tools.Serena != nil && tools.Serena.Mode == "local" {
		runtimeSetupLog.Print("Serena configured in local mode, adding uvx runtime requirement")
		uvRuntime := findRuntimeByID("uv")
		if uvRuntime != nil {
			updateRequiredRuntime(uvRuntime, "", requirements)
		}

		// Add language runtimes based on configured languages
		detectSerenaLanguageRuntimes(tools.Serena, requirements)
	}

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

// detectFromEngineSteps scans engine steps for runtime commands
func detectFromEngineSteps(steps []map[string]any, requirements map[string]*RuntimeRequirement) {
	for _, step := range steps {
		if run, hasRun := step["run"]; hasRun {
			if runStr, ok := run.(string); ok {
				detectRuntimeFromCommand(runStr, requirements)
			}
		}
	}
}

// updateRequiredRuntime updates the version requirement, choosing the highest version
func updateRequiredRuntime(runtime *Runtime, newVersion string, requirements map[string]*RuntimeRequirement) {
	existing, exists := requirements[runtime.ID]

	if !exists {
		runtimeSetupLog.Printf("Adding new runtime requirement: %s (version=%s)", runtime.ID, newVersion)
		requirements[runtime.ID] = &RuntimeRequirement{
			Runtime: runtime,
			Version: newVersion,
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
	if compareVersions(newVersion, existing.Version) > 0 {
		existing.Version = newVersion
	}
}

// detectSerenaLanguageRuntimes detects and adds runtime requirements for Serena language services
// when running in local mode. Maps Serena language names to runtime IDs.
func detectSerenaLanguageRuntimes(serenaConfig *SerenaToolConfig, requirements map[string]*RuntimeRequirement) {
	if serenaConfig == nil {
		return
	}

	// Map of Serena language names to runtime IDs
	languageToRuntime := map[string]string{
		"go":         "go",
		"typescript": "node",
		"javascript": "node",
		"python":     "python",
		"java":       "java",
		"rust":       "rust", // rust is not in knownRuntimes yet, but including for completeness
		"csharp":     "dotnet",
	}

	// Check ShortSyntax first (array format like ["go", "typescript"])
	if len(serenaConfig.ShortSyntax) > 0 {
		for _, langName := range serenaConfig.ShortSyntax {
			if runtimeID, ok := languageToRuntime[langName]; ok {
				runtime := findRuntimeByID(runtimeID)
				if runtime != nil {
					runtimeSetupLog.Printf("Serena local mode: adding runtime for language '%s' -> '%s'", langName, runtimeID)
					updateRequiredRuntime(runtime, "", requirements)
				} else {
					runtimeSetupLog.Printf("Serena local mode: runtime '%s' not found for language '%s'", runtimeID, langName)
				}
			}
		}
		return
	}

	// Check Languages map (detailed configuration)
	if serenaConfig.Languages != nil {
		for langName, langConfig := range serenaConfig.Languages {
			if runtimeID, ok := languageToRuntime[langName]; ok {
				runtime := findRuntimeByID(runtimeID)
				if runtime != nil {
					// Use version from language config if specified
					version := ""
					if langConfig != nil && langConfig.Version != "" {
						version = langConfig.Version
					}

					runtimeSetupLog.Printf("Serena local mode: adding runtime for language '%s' -> '%s' (version: %s)", langName, runtimeID, version)

					// For Go, check if go-mod-file is specified
					if runtimeID == "go" && langConfig != nil && langConfig.GoModFile != "" {
						// Create a requirement with GoModFile set
						req := &RuntimeRequirement{
							Runtime:   runtime,
							Version:   version,
							GoModFile: langConfig.GoModFile,
						}
						requirements[runtimeID] = req
					} else {
						updateRequiredRuntime(runtime, version, requirements)
					}
				} else {
					runtimeSetupLog.Printf("Serena local mode: runtime '%s' not found for language '%s'", runtimeID, langName)
				}
			}
		}
	}
}
