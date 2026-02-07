package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var importLog = logger.New("parser:import_processor")

// ImportsResult holds the result of processing imports from frontmatter
type ImportsResult struct {
	MergedTools         string   // Merged tools configuration from all imports
	MergedMCPServers    string   // Merged mcp-servers configuration from all imports
	MergedEngines       []string // Merged engine configurations from all imports
	MergedSafeOutputs   []string // Merged safe-outputs configurations from all imports
	MergedSafeInputs    []string // Merged safe-inputs configurations from all imports
	MergedMarkdown      string   // Only contains imports WITH inputs (for compile-time substitution)
	ImportPaths         []string // List of import file paths for runtime-import macro generation (replaces MergedMarkdown)
	MergedSteps         string   // Merged steps configuration from all imports (excluding copilot-setup-steps)
	CopilotSetupSteps   string   // Steps from copilot-setup-steps.yml (inserted at start)
	MergedRuntimes      string   // Merged runtimes configuration from all imports
	MergedServices      string   // Merged services configuration from all imports
	MergedNetwork       string   // Merged network configuration from all imports
	MergedPermissions   string   // Merged permissions configuration from all imports
	MergedSecretMasking string   // Merged secret-masking steps from all imports
	MergedBots          []string // Merged bots list from all imports (union of bot names)
	MergedPlugins       []string // Merged plugins list from all imports (union of plugin repos)
	MergedPostSteps     string   // Merged post-steps configuration from all imports (appended in order)
	MergedLabels        []string // Merged labels from all imports (union of label names)
	MergedCaches        []string // Merged cache configurations from all imports (appended in order)
	MergedJobs          string   // Merged jobs from imported YAML workflows (JSON format)
	ImportedFiles       []string // List of imported file paths (for manifest)
	AgentFile           string   // Path to custom agent file (if imported)
	AgentImportSpec     string   // Original import specification for agent file (e.g., "owner/repo/path@ref")
	RepositoryImports   []string // List of repository imports (format: "owner/repo@ref") for .github folder merging
	// ImportInputs uses map[string]any because input values can be different types (string, number, boolean).
	// This is parsed from YAML frontmatter where the structure is dynamic and not known at compile time.
	// This is an appropriate use of 'any' for dynamic YAML/JSON data.
	// See scratchpad/go-type-patterns.md for guidance on when to use map[string]any.
	ImportInputs map[string]any // Aggregated input values from all imports (key = input name, value = input value)
}

// ImportInputDefinition defines an input parameter for a shared workflow import.
// Uses the same schema as workflow_dispatch inputs.
// NOTE: This type matches workflow.InputDefinition which is the canonical type for input parameters.
// The parser package uses map[string]any for actual parsing to avoid circular dependencies.
type ImportInputDefinition struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"` // Can be string, number, or boolean (dynamic type from YAML)
	Type        string   `yaml:"type,omitempty" json:"type,omitempty"`       // "string", "choice", "boolean", "number"
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // Options for choice type
}

// ImportSpec represents a single import specification (either a string path or an object with path and inputs)
type ImportSpec struct {
	Path string // Import path (required)
	// Inputs uses map[string]any because input values can be different types (string, number, boolean).
	// This is parsed from YAML frontmatter and validated against the imported workflow's input definitions.
	// This is an appropriate use of 'any' for dynamic YAML data. See scratchpad/go-type-patterns.md.
	Inputs map[string]any // Optional input values to pass to the imported workflow (values are string, number, or boolean)
}

// ProcessImportsFromFrontmatter processes imports field from frontmatter
// Returns merged tools and engines from imported files
//
// Type Pattern Note: frontmatter uses map[string]any because it represents parsed YAML with
// dynamic structure that varies by workflow. This is the appropriate pattern for parsing
// user-provided configuration files. See scratchpad/go-type-patterns.md for guidance.
func ProcessImportsFromFrontmatter(frontmatter map[string]any, baseDir string) (mergedTools string, mergedEngines []string, err error) {
	log.Printf("Processing imports from frontmatter: baseDir=%s", baseDir)
	result, err := ProcessImportsFromFrontmatterWithManifest(frontmatter, baseDir, nil)
	if err != nil {
		return "", nil, err
	}
	return result.MergedTools, result.MergedEngines, nil
}

// importQueueItem represents a file to be imported with its context
type importQueueItem struct {
	importPath  string         // Original import path (e.g., "file.md" or "file.md#Section")
	fullPath    string         // Resolved absolute file path
	sectionName string         // Optional section name (from file.md#Section syntax)
	baseDir     string         // Base directory for resolving nested imports
	inputs      map[string]any // Optional input values from parent import
}

// ProcessImportsFromFrontmatterWithManifest processes imports field from frontmatter
// Returns result containing merged tools, engines, markdown content, and list of imported files
// Uses BFS traversal with queues for deterministic ordering and cycle detection
func ProcessImportsFromFrontmatterWithManifest(frontmatter map[string]any, baseDir string, cache *ImportCache) (*ImportsResult, error) {
	return processImportsFromFrontmatterWithManifestAndSource(frontmatter, baseDir, cache, "", "")
}

// ProcessImportsFromFrontmatterWithSource processes imports field from frontmatter with source tracking
// This version includes the workflow file path and YAML content for better error reporting
func ProcessImportsFromFrontmatterWithSource(frontmatter map[string]any, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string) (*ImportsResult, error) {
	return processImportsFromFrontmatterWithManifestAndSource(frontmatter, baseDir, cache, workflowFilePath, yamlContent)
}

// processImportsFromFrontmatterWithManifestAndSource is the internal implementation that includes source tracking
func processImportsFromFrontmatterWithManifestAndSource(frontmatter map[string]any, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string) (*ImportsResult, error) {
	// Check if imports field exists
	importsField, exists := frontmatter["imports"]
	if !exists {
		return &ImportsResult{}, nil
	}

	log.Print("Processing imports from frontmatter with recursive BFS")

	// Parse imports field - can be array of strings or objects with path and inputs
	var importSpecs []ImportSpec
	switch v := importsField.(type) {
	case []any:
		for _, item := range v {
			switch importItem := item.(type) {
			case string:
				// Simple string import
				importSpecs = append(importSpecs, ImportSpec{Path: importItem})
			case map[string]any:
				// Object import with path and optional inputs
				pathValue, hasPath := importItem["path"]
				if !hasPath {
					return nil, fmt.Errorf("import object must have a 'path' field")
				}
				pathStr, ok := pathValue.(string)
				if !ok {
					return nil, fmt.Errorf("import 'path' must be a string")
				}
				var inputs map[string]any
				if inputsValue, hasInputs := importItem["inputs"]; hasInputs {
					if inputsMap, ok := inputsValue.(map[string]any); ok {
						inputs = inputsMap
					} else {
						return nil, fmt.Errorf("import 'inputs' must be an object")
					}
				}
				importSpecs = append(importSpecs, ImportSpec{Path: pathStr, Inputs: inputs})
			default:
				return nil, fmt.Errorf("import item must be a string or an object with 'path' field")
			}
		}
	case []string:
		for _, s := range v {
			importSpecs = append(importSpecs, ImportSpec{Path: s})
		}
	default:
		return nil, fmt.Errorf("imports field must be an array of strings or objects")
	}

	if len(importSpecs) == 0 {
		return &ImportsResult{}, nil
	}

	log.Printf("Found %d direct imports to process", len(importSpecs))

	// Initialize BFS queue and visited set for cycle detection
	var queue []importQueueItem
	visited := make(map[string]bool)
	processedOrder := []string{} // Track processing order for manifest

	// Initialize result accumulators
	var toolsBuilder strings.Builder
	var mcpServersBuilder strings.Builder
	var markdownBuilder strings.Builder // Only used for imports WITH inputs (compile-time substitution)
	var importPaths []string            // NEW: Track import paths for runtime-import macro generation
	var stepsBuilder strings.Builder
	var copilotSetupStepsBuilder strings.Builder // Track copilot-setup-steps.yml separately
	var runtimesBuilder strings.Builder
	var servicesBuilder strings.Builder
	var networkBuilder strings.Builder
	var permissionsBuilder strings.Builder
	var secretMaskingBuilder strings.Builder
	var postStepsBuilder strings.Builder
	var engines []string
	var safeOutputs []string
	var safeInputs []string
	var bots []string                    // Track unique bot names
	botsSet := make(map[string]bool)     // Set for deduplicating bots
	var plugins []string                 // Track unique plugin repos
	pluginsSet := make(map[string]bool)  // Set for deduplicating plugins
	var labels []string                  // Track unique labels
	labelsSet := make(map[string]bool)   // Set for deduplicating labels
	var caches []string                  // Track cache configurations (appended in order)
	var jobsBuilder strings.Builder      // Track jobs from imported YAML workflows
	var agentFile string                 // Track custom agent file
	var agentImportSpec string           // Track agent import specification for remote imports
	var repositoryImports []string       // Track repository-only imports for .github folder merging
	importInputs := make(map[string]any) // Aggregated input values from all imports

	// Seed the queue with initial imports
	for _, importSpec := range importSpecs {
		importPath := importSpec.Path

		// Check if this is a repository-only import (owner/repo@ref without file path)
		if isRepositoryImport(importPath) {
			log.Printf("Detected repository import: %s", importPath)
			repositoryImports = append(repositoryImports, importPath)
			// Repository imports don't need further processing - they're handled at runtime
			continue
		}

		// Handle section references (file.md#Section)
		var filePath, sectionName string
		if strings.Contains(importPath, "#") {
			parts := strings.SplitN(importPath, "#", 2)
			filePath = parts[0]
			sectionName = parts[1]
		} else {
			filePath = importPath
		}

		// Resolve import path (supports workflowspec format)
		fullPath, err := ResolveIncludePath(filePath, baseDir, cache)
		if err != nil {
			// If we have source information, create a structured import error
			if workflowFilePath != "" && yamlContent != "" {
				line, column := findImportItemLocation(yamlContent, importPath)
				importErr := &ImportError{
					ImportPath: importPath,
					FilePath:   workflowFilePath,
					Line:       line,
					Column:     column,
					Cause:      err,
				}
				return nil, FormatImportError(importErr, yamlContent)
			}
			// Fallback to generic error if no source information
			return nil, fmt.Errorf("failed to resolve import '%s': %w", filePath, err)
		}

		// Validate that .lock.yml files are not imported
		if strings.HasSuffix(strings.ToLower(fullPath), ".lock.yml") {
			if workflowFilePath != "" && yamlContent != "" {
				line, column := findImportItemLocation(yamlContent, importPath)
				importErr := &ImportError{
					ImportPath: importPath,
					FilePath:   workflowFilePath,
					Line:       line,
					Column:     column,
					Cause:      fmt.Errorf("cannot import .lock.yml files. Lock files are compiled outputs from gh-aw. Import the source .md file instead"),
				}
				return nil, FormatImportError(importErr, yamlContent)
			}
			return nil, fmt.Errorf("cannot import .lock.yml files: '%s'. Lock files are compiled outputs from gh-aw. Import the source .md file instead", importPath)
		}

		// Check for duplicates before adding to queue
		if !visited[fullPath] {
			visited[fullPath] = true
			queue = append(queue, importQueueItem{
				importPath:  importPath,
				fullPath:    fullPath,
				sectionName: sectionName,
				baseDir:     baseDir,
				inputs:      importSpec.Inputs,
			})
			log.Printf("Queued import: %s (resolved to %s)", importPath, fullPath)
		} else {
			log.Printf("Skipping duplicate import: %s (already visited)", importPath)
		}
	}

	// BFS traversal: process queue until empty
	for len(queue) > 0 {
		// Dequeue first item (FIFO for BFS)
		item := queue[0]
		queue = queue[1:]

		log.Printf("Processing import from queue: %s", item.fullPath)

		// Merge inputs from this import into the aggregated inputs map
		for k, v := range item.inputs {
			importInputs[k] = v
		}

		// Add to processing order
		processedOrder = append(processedOrder, item.importPath)

		// Check if this is a custom agent file (any markdown file under .github/agents)
		isAgentFile := strings.Contains(item.fullPath, "/.github/agents/") && strings.HasSuffix(strings.ToLower(item.fullPath), ".md")
		if isAgentFile {
			if agentFile != "" {
				// Multiple agent files found - error
				log.Printf("Multiple agent files found: %s and %s", agentFile, item.importPath)
				return nil, fmt.Errorf("multiple agent files found in imports: '%s' and '%s'. Only one agent file is allowed per workflow", agentFile, item.importPath)
			}
			// Extract relative path from repository root (from .github/ onwards)
			// This ensures the path works at runtime with $GITHUB_WORKSPACE
			var importRelPath string
			if idx := strings.Index(item.fullPath, "/.github/"); idx >= 0 {
				agentFile = item.fullPath[idx+1:] // +1 to skip the leading slash
				importRelPath = agentFile
			} else {
				agentFile = item.fullPath
				importRelPath = item.fullPath
			}
			log.Printf("Found agent file: %s (resolved to: %s)", item.fullPath, agentFile)

			// Store the original import specification for remote agents
			// This allows runtime detection and .github folder merging
			agentImportSpec = item.importPath
			log.Printf("Agent import specification: %s", agentImportSpec)

			// Track import path for runtime-import macro generation (only if no inputs)
			// Imports with inputs must be inlined for compile-time substitution
			if len(item.inputs) == 0 {
				// No inputs - use runtime-import macro
				importPaths = append(importPaths, importRelPath)
				log.Printf("Added agent import path for runtime-import: %s", importRelPath)
			} else {
				// Has inputs - must inline for compile-time substitution
				log.Printf("Agent file has inputs - will be inlined instead of runtime-imported")

				// For agent files, extract markdown content (only when inputs are present)
				markdownContent, err := processIncludedFileWithVisited(item.fullPath, item.sectionName, false, visited)
				if err != nil {
					return nil, fmt.Errorf("failed to process markdown from agent file '%s': %w", item.fullPath, err)
				}
				if markdownContent != "" {
					markdownBuilder.WriteString(markdownContent)
					// Add blank line separator between imported files
					if !strings.HasSuffix(markdownContent, "\n\n") {
						if strings.HasSuffix(markdownContent, "\n") {
							markdownBuilder.WriteString("\n")
						} else {
							markdownBuilder.WriteString("\n\n")
						}
					}
				}
			}

			// Agent files don't have nested imports, skip to next item
			continue
		}

		// Check if this is a YAML workflow file (not .lock.yml)
		if isYAMLWorkflowFile(item.fullPath) {
			log.Printf("Detected YAML workflow file: %s", item.fullPath)

			// Process YAML workflow import to extract jobs/steps and services
			// Special case: copilot-setup-steps.yml returns steps YAML instead of jobs JSON
			jobsOrStepsData, servicesJSON, err := processYAMLWorkflowImport(item.fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to process YAML workflow '%s': %w", item.importPath, err)
			}

			// Check if this is copilot-setup-steps.yml (returns steps YAML instead of jobs JSON)
			if isCopilotSetupStepsFile(item.fullPath) {
				// For copilot-setup-steps.yml, jobsOrStepsData contains steps in YAML format
				// Add to CopilotSetupSteps instead of MergedSteps (inserted at start of workflow)
				if jobsOrStepsData != "" {
					copilotSetupStepsBuilder.WriteString(jobsOrStepsData + "\n")
					log.Printf("Added copilot-setup steps (will be inserted at start): %s", item.importPath)
				}
			} else {
				// For regular YAML workflows, jobsOrStepsData contains jobs in JSON format
				if jobsOrStepsData != "" && jobsOrStepsData != "{}" {
					jobsBuilder.WriteString(jobsOrStepsData + "\n")
					log.Printf("Added jobs from YAML workflow: %s", item.importPath)
				}
			}

			// Append services to merged services (services from YAML are already in JSON format)
			// Need to convert to YAML format for consistency with other services
			if servicesJSON != "" && servicesJSON != "{}" {
				// Convert JSON services to YAML format
				var services map[string]any
				if err := json.Unmarshal([]byte(servicesJSON), &services); err == nil {
					servicesWrapper := map[string]any{"services": services}
					servicesYAML, err := yaml.Marshal(servicesWrapper)
					if err == nil {
						servicesBuilder.WriteString(string(servicesYAML) + "\n")
						log.Printf("Added services from YAML workflow: %s", item.importPath)
					}
				}
			}

			// YAML workflows don't have nested imports or markdown content, skip to next item
			continue
		}

		// Read the imported file to extract nested imports
		content, err := os.ReadFile(item.fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read imported file '%s': %w", item.fullPath, err)
		}

		// Extract frontmatter from imported file to discover nested imports
		result, err := ExtractFrontmatterFromContent(string(content))
		if err != nil {
			// If frontmatter extraction fails, continue with other processing
			log.Printf("Failed to extract frontmatter from %s: %v", item.fullPath, err)
		} else if result.Frontmatter != nil {
			// Check for nested imports field
			if nestedImportsField, hasImports := result.Frontmatter["imports"]; hasImports {
				var nestedImports []string
				switch v := nestedImportsField.(type) {
				case []any:
					for _, nestedItem := range v {
						if str, ok := nestedItem.(string); ok {
							nestedImports = append(nestedImports, str)
						}
					}
				case []string:
					nestedImports = v
				}

				// Add nested imports to queue (BFS: append to end)
				// Use the original baseDir for resolving nested imports, not the nested file's directory
				// This ensures that all imports are resolved relative to the workflows directory
				for _, nestedImportPath := range nestedImports {
					// Handle section references
					var nestedFilePath, nestedSectionName string
					if strings.Contains(nestedImportPath, "#") {
						parts := strings.SplitN(nestedImportPath, "#", 2)
						nestedFilePath = parts[0]
						nestedSectionName = parts[1]
					} else {
						nestedFilePath = nestedImportPath
					}

					// Resolve nested import path relative to the workflows directory, not the nested file's directory
					nestedFullPath, err := ResolveIncludePath(nestedFilePath, baseDir, cache)
					if err != nil {
						// If we have source information for the parent workflow, create a structured error
						if workflowFilePath != "" && yamlContent != "" {
							// For nested imports, we should report the error at the location where the parent import is defined
							// since the nested import file itself might not have source location
							line, column := findImportItemLocation(yamlContent, item.importPath)
							importErr := &ImportError{
								ImportPath: nestedImportPath,
								FilePath:   workflowFilePath,
								Line:       line,
								Column:     column,
								Cause:      err,
							}
							return nil, FormatImportError(importErr, yamlContent)
						}
						// Fallback to generic error
						return nil, fmt.Errorf("failed to resolve nested import '%s' from '%s': %w", nestedFilePath, item.fullPath, err)
					}

					// Check for cycles - skip if already visited
					if !visited[nestedFullPath] {
						visited[nestedFullPath] = true
						queue = append(queue, importQueueItem{
							importPath:  nestedImportPath,
							fullPath:    nestedFullPath,
							sectionName: nestedSectionName,
							baseDir:     baseDir, // Use original baseDir, not nestedBaseDir
						})
						log.Printf("Discovered nested import: %s -> %s (queued)", item.fullPath, nestedFullPath)
					} else {
						log.Printf("Skipping already visited nested import: %s (cycle detected)", nestedFullPath)
					}
				}
			}
		}

		// Extract tools from imported file
		toolsContent, err := processIncludedFileWithVisited(item.fullPath, item.sectionName, true, visited)
		if err != nil {
			return nil, fmt.Errorf("failed to process imported file '%s': %w", item.fullPath, err)
		}
		toolsBuilder.WriteString(toolsContent + "\n")

		// Track import path for runtime-import macro generation (only if no inputs)
		// Imports with inputs must be inlined for compile-time substitution
		// Extract relative path from repository root (from .github/ onwards)
		var importRelPath string
		if idx := strings.Index(item.fullPath, "/.github/"); idx >= 0 {
			importRelPath = item.fullPath[idx+1:] // +1 to skip the leading slash
		} else {
			// For files not under .github/, use the original import path
			importRelPath = item.importPath
		}

		if len(item.inputs) == 0 {
			// No inputs - use runtime-import macro
			importPaths = append(importPaths, importRelPath)
			log.Printf("Added import path for runtime-import: %s", importRelPath)
		} else {
			// Has inputs - must inline for compile-time substitution
			log.Printf("Import %s has inputs - will be inlined for compile-time substitution", importRelPath)

			// Extract markdown content from imported file (only for imports with inputs)
			markdownContent, err := processIncludedFileWithVisited(item.fullPath, item.sectionName, false, visited)
			if err != nil {
				return nil, fmt.Errorf("failed to process markdown from imported file '%s': %w", item.fullPath, err)
			}
			if markdownContent != "" {
				markdownBuilder.WriteString(markdownContent)
				// Add blank line separator between imported files
				if !strings.HasSuffix(markdownContent, "\n\n") {
					if strings.HasSuffix(markdownContent, "\n") {
						markdownBuilder.WriteString("\n")
					} else {
						markdownBuilder.WriteString("\n\n")
					}
				}
			}
		}

		// Extract engines from imported file
		engineContent, err := extractEngineFromContent(string(content))
		if err == nil && engineContent != "" {
			engines = append(engines, engineContent)
		}

		// Extract mcp-servers from imported file
		mcpServersContent, err := extractMCPServersFromContent(string(content))
		if err == nil && mcpServersContent != "" && mcpServersContent != "{}" {
			mcpServersBuilder.WriteString(mcpServersContent + "\n")
		}

		// Extract safe-outputs from imported file
		safeOutputsContent, err := extractSafeOutputsFromContent(string(content))
		if err == nil && safeOutputsContent != "" && safeOutputsContent != "{}" {
			safeOutputs = append(safeOutputs, safeOutputsContent)
		}

		// Extract safe-inputs from imported file
		safeInputsContent, err := extractSafeInputsFromContent(string(content))
		if err == nil && safeInputsContent != "" && safeInputsContent != "{}" {
			safeInputs = append(safeInputs, safeInputsContent)
		}

		// Extract steps from imported file
		stepsContent, err := extractStepsFromContent(string(content))
		if err == nil && stepsContent != "" {
			stepsBuilder.WriteString(stepsContent + "\n")
		}

		// Extract runtimes from imported file
		runtimesContent, err := extractRuntimesFromContent(string(content))
		if err == nil && runtimesContent != "" && runtimesContent != "{}" {
			runtimesBuilder.WriteString(runtimesContent + "\n")
		}

		// Extract services from imported file
		servicesContent, err := extractServicesFromContent(string(content))
		if err == nil && servicesContent != "" {
			servicesBuilder.WriteString(servicesContent + "\n")
		}

		// Extract network from imported file
		networkContent, err := extractNetworkFromContent(string(content))
		if err == nil && networkContent != "" && networkContent != "{}" {
			networkBuilder.WriteString(networkContent + "\n")
		}

		// Extract permissions from imported file
		permissionsContent, err := ExtractPermissionsFromContent(string(content))
		if err == nil && permissionsContent != "" && permissionsContent != "{}" {
			permissionsBuilder.WriteString(permissionsContent + "\n")
		}

		// Extract secret-masking from imported file
		secretMaskingContent, err := extractSecretMaskingFromContent(string(content))
		if err == nil && secretMaskingContent != "" && secretMaskingContent != "{}" {
			secretMaskingBuilder.WriteString(secretMaskingContent + "\n")
		}

		// Extract bots from imported file (merge into set to avoid duplicates)
		botsContent, err := extractBotsFromContent(string(content))
		if err == nil && botsContent != "" && botsContent != "[]" {
			// Parse bots JSON array
			var importedBots []string
			if jsonErr := json.Unmarshal([]byte(botsContent), &importedBots); jsonErr == nil {
				for _, bot := range importedBots {
					if !botsSet[bot] {
						botsSet[bot] = true
						bots = append(bots, bot)
					}
				}
			}
		}

		// Extract plugins from imported file (merge into set to avoid duplicates)
		// This now handles both simple string format and object format with MCP configs
		pluginsContent, err := extractPluginsFromContent(string(content))
		if err == nil && pluginsContent != "" && pluginsContent != "[]" {
			// Parse plugins - can be array of strings or objects
			var pluginsRaw []any
			if jsonErr := json.Unmarshal([]byte(pluginsContent), &pluginsRaw); jsonErr == nil {
				for _, item := range pluginsRaw {
					// Handle string format: "org/repo"
					if pluginStr, ok := item.(string); ok {
						if !pluginsSet[pluginStr] {
							pluginsSet[pluginStr] = true
							plugins = append(plugins, pluginStr)
						}
					} else if pluginObj, ok := item.(map[string]any); ok {
						// Handle object format: { "id": "org/repo", "mcp": {...} }
						if idVal, hasID := pluginObj["id"]; hasID {
							if pluginID, ok := idVal.(string); ok && !pluginsSet[pluginID] {
								pluginsSet[pluginID] = true
								plugins = append(plugins, pluginID)
								// Note: MCP configs from imports are currently not merged
								// They would need to be handled at a higher level in compiler_orchestrator_tools.go
							}
						}
					}
				}
			}
		}

		// Extract post-steps from imported file (append in order)
		postStepsContent, err := extractPostStepsFromContent(string(content))
		if err == nil && postStepsContent != "" {
			postStepsBuilder.WriteString(postStepsContent + "\n")
		}

		// Extract labels from imported file (merge into set to avoid duplicates)
		labelsContent, err := extractLabelsFromContent(string(content))
		if err == nil && labelsContent != "" && labelsContent != "[]" {
			// Parse labels JSON array
			var importedLabels []string
			if jsonErr := json.Unmarshal([]byte(labelsContent), &importedLabels); jsonErr == nil {
				for _, label := range importedLabels {
					if !labelsSet[label] {
						labelsSet[label] = true
						labels = append(labels, label)
					}
				}
			}
		}

		// Extract cache from imported file (append to list of caches)
		cacheContent, err := extractCacheFromContent(string(content))
		if err == nil && cacheContent != "" && cacheContent != "{}" {
			caches = append(caches, cacheContent)
		}
	}

	log.Printf("Completed BFS traversal. Processed %d imports in total", len(processedOrder))

	// Sort imports in topological order (roots first, dependencies before dependents)
	topologicalOrder := topologicalSortImports(processedOrder, baseDir, cache)
	log.Printf("Sorted imports in topological order: %v", topologicalOrder)

	return &ImportsResult{
		MergedTools:         toolsBuilder.String(),
		MergedMCPServers:    mcpServersBuilder.String(),
		MergedEngines:       engines,
		MergedSafeOutputs:   safeOutputs,
		MergedSafeInputs:    safeInputs,
		MergedMarkdown:      markdownBuilder.String(), // Only imports WITH inputs (for compile-time substitution)
		ImportPaths:         importPaths,              // Import paths for runtime-import macro generation
		MergedSteps:         stepsBuilder.String(),
		CopilotSetupSteps:   copilotSetupStepsBuilder.String(),
		MergedRuntimes:      runtimesBuilder.String(),
		MergedServices:      servicesBuilder.String(),
		MergedNetwork:       networkBuilder.String(),
		MergedPermissions:   permissionsBuilder.String(),
		MergedSecretMasking: secretMaskingBuilder.String(),
		MergedBots:          bots,
		MergedPlugins:       plugins,
		MergedPostSteps:     postStepsBuilder.String(),
		MergedLabels:        labels,
		MergedCaches:        caches,
		MergedJobs:          jobsBuilder.String(),
		ImportedFiles:       topologicalOrder,
		AgentFile:           agentFile,
		AgentImportSpec:     agentImportSpec,
		RepositoryImports:   repositoryImports,
		ImportInputs:        importInputs,
	}, nil
}

// topologicalSortImports sorts imports in topological order using Kahn's algorithm
// Returns imports sorted such that roots (files with no imports) come first,
// and each import has all its dependencies listed before it
func topologicalSortImports(imports []string, baseDir string, cache *ImportCache) []string {
	importLog.Printf("Starting topological sort of %d imports", len(imports))

	// Build dependency graph: map each import to its list of nested imports
	dependencies := make(map[string][]string)
	allImportsSet := make(map[string]bool)

	// Track all imports (including the ones we're sorting)
	for _, imp := range imports {
		allImportsSet[imp] = true
	}

	// Extract dependencies for each import by reading and parsing each file
	for _, importPath := range imports {
		// Resolve the import path to get the full path
		var filePath string
		if strings.Contains(importPath, "#") {
			parts := strings.SplitN(importPath, "#", 2)
			filePath = parts[0]
		} else {
			filePath = importPath
		}

		fullPath, err := ResolveIncludePath(filePath, baseDir, cache)
		if err != nil {
			importLog.Printf("Failed to resolve import path %s during topological sort: %v", importPath, err)
			dependencies[importPath] = []string{}
			continue
		}

		// Read and parse the file to extract its imports
		content, err := os.ReadFile(fullPath)
		if err != nil {
			importLog.Printf("Failed to read file %s during topological sort: %v", fullPath, err)
			dependencies[importPath] = []string{}
			continue
		}

		result, err := ExtractFrontmatterFromContent(string(content))
		if err != nil {
			importLog.Printf("Failed to extract frontmatter from %s during topological sort: %v", fullPath, err)
			dependencies[importPath] = []string{}
			continue
		}

		// Extract nested imports
		nestedImports := extractImportPaths(result.Frontmatter)
		dependencies[importPath] = nestedImports
		importLog.Printf("Import %s has %d dependencies: %v", importPath, len(nestedImports), nestedImports)
	}

	// Kahn's algorithm: Calculate in-degrees (number of dependencies for each import)
	inDegree := make(map[string]int)
	for _, imp := range imports {
		inDegree[imp] = 0
	}

	// Count dependencies: how many imports does each file depend on (within our import set)
	for imp, deps := range dependencies {
		for _, dep := range deps {
			// Only count dependencies that are in our import set
			if allImportsSet[dep] {
				inDegree[imp]++
			}
		}
	}

	importLog.Printf("Calculated in-degrees: %v", inDegree)

	// Start with imports that have no dependencies (in-degree = 0) - these are the roots
	var queue []string
	for _, imp := range imports {
		if inDegree[imp] == 0 {
			queue = append(queue, imp)
			importLog.Printf("Root import (no dependencies): %s", imp)
		}
	}

	// Process imports in topological order
	result := make([]string, 0, len(imports))
	for len(queue) > 0 {
		// Sort queue for deterministic output when multiple imports have same in-degree
		sort.Strings(queue)

		// Take the first import from queue
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		importLog.Printf("Processing import %s (in-degree was 0)", current)

		// For each import that depends on the current import, reduce its in-degree
		for imp, deps := range dependencies {
			for _, dep := range deps {
				if dep == current && allImportsSet[imp] {
					inDegree[imp]--
					importLog.Printf("Reduced in-degree of %s to %d (resolved dependency on %s)", imp, inDegree[imp], current)
					if inDegree[imp] == 0 {
						queue = append(queue, imp)
						importLog.Printf("Added %s to queue (in-degree reached 0)", imp)
					}
				}
			}
		}
	}

	importLog.Printf("Topological sort complete: %v", result)
	return result
}

// extractImportPaths extracts just the import paths from frontmatter
func extractImportPaths(frontmatter map[string]any) []string {
	var imports []string

	if frontmatter == nil {
		return imports
	}

	importsField, exists := frontmatter["imports"]
	if !exists {
		return imports
	}

	// Parse imports field - can be array of strings or objects with path
	switch v := importsField.(type) {
	case []any:
		for _, item := range v {
			switch importItem := item.(type) {
			case string:
				imports = append(imports, importItem)
			case map[string]any:
				if pathValue, hasPath := importItem["path"]; hasPath {
					if pathStr, ok := pathValue.(string); ok {
						imports = append(imports, pathStr)
					}
				}
			}
		}
	case []string:
		imports = v
	}

	return imports
}
