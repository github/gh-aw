package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var orchestratorWorkflowLog = logger.New("workflow:compiler_orchestrator_workflow")

// ParseWorkflowFile parses a workflow markdown file and returns a WorkflowData structure.
// This is the main orchestration function that coordinates all compilation phases.
func (c *Compiler) ParseWorkflowFile(markdownPath string) (*WorkflowData, error) {
	orchestratorWorkflowLog.Printf("Starting workflow file parsing: %s", markdownPath)

	// Parse frontmatter section
	parseResult, err := c.parseFrontmatterSection(markdownPath)
	if err != nil {
		return nil, err
	}

	// Handle shared workflows
	if parseResult.isSharedWorkflow {
		return nil, &SharedWorkflowError{Path: parseResult.cleanPath}
	}

	// Unpack parse result for convenience
	cleanPath := parseResult.cleanPath
	content := parseResult.content
	result := parseResult.frontmatterResult
	markdownDir := parseResult.markdownDir

	// Setup engine and process imports
	engineSetup, err := c.setupEngineAndImports(result, cleanPath, content, markdownDir)
	if err != nil {
		return nil, err
	}

	// Process tools and markdown
	toolsResult, err := c.processToolsAndMarkdown(result, cleanPath, markdownDir, engineSetup.agenticEngine, engineSetup.engineSetting, engineSetup.importsResult)
	if err != nil {
		return nil, err
	}

	// Build initial workflow data structure
	workflowData := c.buildInitialWorkflowData(result, toolsResult, engineSetup, engineSetup.importsResult)
	// Store a stable workflow identifier derived from the file name.
	workflowData.WorkflowID = GetWorkflowIDFromPath(cleanPath)

	// Validate bash tool configuration BEFORE applying defaults
	// This must happen before applyDefaults() which converts nil bash to default commands
	if err := validateBashToolConfig(workflowData.ParsedTools, workflowData.Name); err != nil {
		return nil, fmt.Errorf("%s: %w", cleanPath, err)
	}

	// Use shared action cache and resolver from the compiler
	actionCache, actionResolver := c.getSharedActionResolver()
	workflowData.ActionCache = actionCache
	workflowData.ActionResolver = actionResolver
	workflowData.ActionPinWarnings = c.actionPinWarnings

	// Extract YAML configuration sections from frontmatter
	c.extractYAMLSections(result.Frontmatter, workflowData)

	// Process and merge custom steps with imported steps
	c.processAndMergeSteps(result.Frontmatter, workflowData, engineSetup.importsResult)

	// Process and merge post-steps
	c.processAndMergePostSteps(result.Frontmatter, workflowData)

	// Process and merge services
	c.processAndMergeServices(result.Frontmatter, workflowData, engineSetup.importsResult)

	// Extract additional configurations (cache, safe-inputs, safe-outputs, etc.)
	if err := c.extractAdditionalConfigurations(
		result.Frontmatter,
		toolsResult.tools,
		markdownDir,
		workflowData,
		engineSetup.importsResult,
		result.Markdown,
		toolsResult.safeOutputs,
	); err != nil {
		return nil, err
	}

	// Process on section configuration and apply filters
	if err := c.processOnSectionAndFilters(result.Frontmatter, workflowData, cleanPath); err != nil {
		return nil, err
	}

	orchestratorWorkflowLog.Printf("Workflow file parsing completed successfully: %s", markdownPath)
	return workflowData, nil
}

// buildInitialWorkflowData creates the initial WorkflowData struct with basic fields populated
func (c *Compiler) buildInitialWorkflowData(
	result *parser.FrontmatterResult,
	toolsResult *toolsProcessingResult,
	engineSetup *engineSetupResult,
	importsResult *parser.ImportsResult,
) *WorkflowData {
	orchestratorWorkflowLog.Print("Building initial workflow data")

	return &WorkflowData{
		Name:                 toolsResult.workflowName,
		FrontmatterName:      toolsResult.frontmatterName,
		FrontmatterYAML:      strings.Join(result.FrontmatterLines, "\n"),
		Description:          c.extractDescription(result.Frontmatter),
		Source:               c.extractSource(result.Frontmatter),
		TrackerID:            toolsResult.trackerID,
		ImportedFiles:        importsResult.ImportedFiles,
		ImportedMarkdown:     toolsResult.importedMarkdown, // Only imports WITH inputs
		ImportPaths:          toolsResult.importPaths,      // Import paths for runtime-import macros (imports without inputs)
		MainWorkflowMarkdown: toolsResult.mainWorkflowMarkdown,
		IncludedFiles:        toolsResult.allIncludedFiles,
		ImportInputs:         importsResult.ImportInputs,
		Tools:                toolsResult.tools,
		ParsedTools:          NewTools(toolsResult.tools),
		Runtimes:             toolsResult.runtimes,
		PluginInfo:           toolsResult.pluginInfo,
		MarkdownContent:      toolsResult.markdownContent,
		AI:                   engineSetup.engineSetting,
		EngineConfig:         engineSetup.engineConfig,
		AgentFile:            importsResult.AgentFile,
		AgentImportSpec:      importsResult.AgentImportSpec,
		RepositoryImports:    importsResult.RepositoryImports,
		NetworkPermissions:   engineSetup.networkPermissions,
		SandboxConfig:        applySandboxDefaults(engineSetup.sandboxConfig, engineSetup.engineConfig),
		NeedsTextOutput:      toolsResult.needsTextOutput,
		ToolsTimeout:         toolsResult.toolsTimeout,
		ToolsStartupTimeout:  toolsResult.toolsStartupTimeout,
		TrialMode:            c.trialMode,
		TrialLogicalRepo:     c.trialLogicalRepoSlug,
		GitHubToken:          extractStringFromMap(result.Frontmatter, "github-token", nil),
		StrictMode:           c.strictMode,
		SecretMasking:        toolsResult.secretMasking,
		ParsedFrontmatter:    toolsResult.parsedFrontmatter,
		ActionMode:           c.actionMode,
	}
}

// extractYAMLSections extracts YAML configuration sections from frontmatter
func (c *Compiler) extractYAMLSections(frontmatter map[string]any, workflowData *WorkflowData) {
	orchestratorWorkflowLog.Print("Extracting YAML sections from frontmatter")

	workflowData.On = c.extractTopLevelYAMLSection(frontmatter, "on")
	workflowData.Permissions = c.extractPermissions(frontmatter)
	workflowData.Network = c.extractTopLevelYAMLSection(frontmatter, "network")
	workflowData.Concurrency = c.extractTopLevelYAMLSection(frontmatter, "concurrency")
	workflowData.RunName = c.extractTopLevelYAMLSection(frontmatter, "run-name")
	workflowData.Env = c.extractTopLevelYAMLSection(frontmatter, "env")
	workflowData.Features = c.extractFeatures(frontmatter)
	workflowData.If = c.extractIfCondition(frontmatter)

	// Extract timeout-minutes (canonical form)
	workflowData.TimeoutMinutes = c.extractTopLevelYAMLSection(frontmatter, "timeout-minutes")

	workflowData.RunsOn = c.extractTopLevelYAMLSection(frontmatter, "runs-on")
	workflowData.Environment = c.extractTopLevelYAMLSection(frontmatter, "environment")
	workflowData.Container = c.extractTopLevelYAMLSection(frontmatter, "container")
	workflowData.Cache = c.extractTopLevelYAMLSection(frontmatter, "cache")
}

// processAndMergeSteps handles the merging of imported steps with main workflow steps
func (c *Compiler) processAndMergeSteps(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) {
	orchestratorWorkflowLog.Print("Processing and merging custom steps")

	workflowData.CustomSteps = c.extractTopLevelYAMLSection(frontmatter, "steps")

	// Parse copilot-setup-steps if present (these go at the start)
	var copilotSetupSteps []any
	if importsResult.CopilotSetupSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.CopilotSetupSteps), &copilotSetupSteps); err != nil {
			orchestratorWorkflowLog.Printf("Failed to unmarshal copilot-setup steps: %v", err)
		} else {
			// Convert to typed steps for action pinning
			typedCopilotSteps, err := SliceToSteps(copilotSetupSteps)
			if err != nil {
				orchestratorWorkflowLog.Printf("Failed to convert copilot-setup steps to typed steps: %v", err)
			} else {
				// Apply action pinning to copilot-setup steps
				typedCopilotSteps = ApplyActionPinsToTypedSteps(typedCopilotSteps, workflowData)
				// Convert back to []any for YAML marshaling
				copilotSetupSteps = StepsToSlice(typedCopilotSteps)
			}
		}
	}

	// Parse other imported steps if present (these go after copilot-setup but before main steps)
	var otherImportedSteps []any
	if importsResult.MergedSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.MergedSteps), &otherImportedSteps); err == nil {
			// Convert to typed steps for action pinning
			typedOtherSteps, err := SliceToSteps(otherImportedSteps)
			if err != nil {
				orchestratorWorkflowLog.Printf("Failed to convert other imported steps to typed steps: %v", err)
			} else {
				// Apply action pinning to other imported steps
				typedOtherSteps = ApplyActionPinsToTypedSteps(typedOtherSteps, workflowData)
				// Convert back to []any for YAML marshaling
				otherImportedSteps = StepsToSlice(typedOtherSteps)
			}
		}
	}

	// If there are main workflow steps, parse them
	var mainSteps []any
	if workflowData.CustomSteps != "" {
		var mainStepsWrapper map[string]any
		if err := yaml.Unmarshal([]byte(workflowData.CustomSteps), &mainStepsWrapper); err == nil {
			if mainStepsVal, hasSteps := mainStepsWrapper["steps"]; hasSteps {
				if steps, ok := mainStepsVal.([]any); ok {
					mainSteps = steps
					// Convert to typed steps for action pinning
					typedMainSteps, err := SliceToSteps(mainSteps)
					if err != nil {
						orchestratorWorkflowLog.Printf("Failed to convert main steps to typed steps: %v", err)
					} else {
						// Apply action pinning to main steps
						typedMainSteps = ApplyActionPinsToTypedSteps(typedMainSteps, workflowData)
						// Convert back to []any for YAML marshaling
						mainSteps = StepsToSlice(typedMainSteps)
					}
				}
			}
		}
	}

	// Merge steps in the correct order:
	// 1. copilot-setup-steps (at start)
	// 2. other imported steps (after copilot-setup)
	// 3. main frontmatter steps (last)
	var allSteps []any
	if len(copilotSetupSteps) > 0 || len(mainSteps) > 0 || len(otherImportedSteps) > 0 {
		allSteps = append(allSteps, copilotSetupSteps...)
		allSteps = append(allSteps, otherImportedSteps...)
		allSteps = append(allSteps, mainSteps...)

		// Convert back to YAML with "steps:" wrapper
		stepsWrapper := map[string]any{"steps": allSteps}
		stepsYAML, err := yaml.Marshal(stepsWrapper)
		if err == nil {
			// Remove quotes from uses values with version comments
			workflowData.CustomSteps = unquoteUsesWithComments(string(stepsYAML))
		}
	}
}

// processAndMergePostSteps handles the processing of post-steps with action pinning
func (c *Compiler) processAndMergePostSteps(frontmatter map[string]any, workflowData *WorkflowData) {
	orchestratorWorkflowLog.Print("Processing post-steps")

	workflowData.PostSteps = c.extractTopLevelYAMLSection(frontmatter, "post-steps")

	// Apply action pinning to post-steps if any
	if workflowData.PostSteps != "" {
		var postStepsWrapper map[string]any
		if err := yaml.Unmarshal([]byte(workflowData.PostSteps), &postStepsWrapper); err == nil {
			if postStepsVal, hasPostSteps := postStepsWrapper["post-steps"]; hasPostSteps {
				if postSteps, ok := postStepsVal.([]any); ok {
					// Convert to typed steps for action pinning
					typedPostSteps, err := SliceToSteps(postSteps)
					if err != nil {
						orchestratorWorkflowLog.Printf("Failed to convert post-steps to typed steps: %v", err)
					} else {
						// Apply action pinning to post steps using type-safe version
						typedPostSteps = ApplyActionPinsToTypedSteps(typedPostSteps, workflowData)
						// Convert back to []any for YAML marshaling
						postSteps = StepsToSlice(typedPostSteps)
					}

					// Convert back to YAML with "post-steps:" wrapper
					stepsWrapper := map[string]any{"post-steps": postSteps}
					stepsYAML, err := yaml.Marshal(stepsWrapper)
					if err == nil {
						// Remove quotes from uses values with version comments
						workflowData.PostSteps = unquoteUsesWithComments(string(stepsYAML))
					}
				}
			}
		}
	}
}

// processAndMergeServices handles the merging of imported services with main workflow services
func (c *Compiler) processAndMergeServices(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) {
	orchestratorWorkflowLog.Print("Processing and merging services")

	workflowData.Services = c.extractTopLevelYAMLSection(frontmatter, "services")

	// Merge imported services if any
	if importsResult.MergedServices != "" {
		// Parse imported services from YAML
		var importedServices map[string]any
		if err := yaml.Unmarshal([]byte(importsResult.MergedServices), &importedServices); err == nil {
			// If there are main workflow services, parse and merge them
			if workflowData.Services != "" {
				// Parse main workflow services
				var mainServicesWrapper map[string]any
				if err := yaml.Unmarshal([]byte(workflowData.Services), &mainServicesWrapper); err == nil {
					if mainServices, ok := mainServicesWrapper["services"].(map[string]any); ok {
						// Merge: main workflow services take precedence over imported
						for key, value := range importedServices {
							if _, exists := mainServices[key]; !exists {
								mainServices[key] = value
							}
						}
						// Convert back to YAML with "services:" wrapper
						servicesWrapper := map[string]any{"services": mainServices}
						servicesYAML, err := yaml.Marshal(servicesWrapper)
						if err == nil {
							workflowData.Services = string(servicesYAML)
						}
					}
				}
			} else {
				// Only imported services exist, wrap in "services:" format
				servicesWrapper := map[string]any{"services": importedServices}
				servicesYAML, err := yaml.Marshal(servicesWrapper)
				if err == nil {
					workflowData.Services = string(servicesYAML)
				}
			}
		}
	}
}

// mergeJobsFromYAMLImports merges jobs from imported YAML workflows with main workflow jobs
// Main workflow jobs take precedence over imported jobs (override behavior)
func (c *Compiler) mergeJobsFromYAMLImports(mainJobs map[string]any, mergedJobsJSON string) map[string]any {
	orchestratorWorkflowLog.Print("Merging jobs from imported YAML workflows")

	if mergedJobsJSON == "" || mergedJobsJSON == "{}" {
		orchestratorWorkflowLog.Print("No imported jobs to merge")
		return mainJobs
	}

	// Initialize result with main jobs or create empty map
	result := make(map[string]any)
	for k, v := range mainJobs {
		result[k] = v
	}

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(mergedJobsJSON, "\n")
	orchestratorWorkflowLog.Printf("Processing %d job definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to map
		var importedJobs map[string]any
		if err := json.Unmarshal([]byte(line), &importedJobs); err != nil {
			orchestratorWorkflowLog.Printf("Skipping malformed job entry: %v", err)
			continue
		}

		// Merge jobs - main workflow jobs take precedence (don't override)
		for jobName, jobConfig := range importedJobs {
			if _, exists := result[jobName]; !exists {
				orchestratorWorkflowLog.Printf("Adding imported job: %s", jobName)
				result[jobName] = jobConfig
			} else {
				orchestratorWorkflowLog.Printf("Skipping imported job %s (already defined in main workflow)", jobName)
			}
		}
	}

	orchestratorWorkflowLog.Printf("Successfully merged jobs: total=%d, imported=%d", len(result), len(result)-len(mainJobs))
	return result
}

// extractAdditionalConfigurations extracts cache-memory, repo-memory, safe-inputs, and safe-outputs configurations
func (c *Compiler) extractAdditionalConfigurations(
	frontmatter map[string]any,
	tools map[string]any,
	markdownDir string,
	workflowData *WorkflowData,
	importsResult *parser.ImportsResult,
	markdown string,
	safeOutputs *SafeOutputsConfig,
) error {
	orchestratorWorkflowLog.Print("Extracting additional configurations")

	// Extract cache-memory config and check for errors
	cacheMemoryConfig, err := c.extractCacheMemoryConfigFromMap(tools)
	if err != nil {
		return err
	}
	workflowData.CacheMemoryConfig = cacheMemoryConfig

	// Extract repo-memory config and check for errors
	toolsConfig, err := ParseToolsConfig(tools)
	if err != nil {
		return err
	}
	repoMemoryConfig, err := c.extractRepoMemoryConfig(toolsConfig)
	if err != nil {
		return err
	}
	workflowData.RepoMemoryConfig = repoMemoryConfig

	// Extract and process safe-inputs and safe-outputs
	workflowData.Command, workflowData.CommandEvents = c.extractCommandConfig(frontmatter)
	workflowData.Jobs = c.extractJobsFromFrontmatter(frontmatter)

	// Merge jobs from imported YAML workflows
	if importsResult.MergedJobs != "" && importsResult.MergedJobs != "{}" {
		workflowData.Jobs = c.mergeJobsFromYAMLImports(workflowData.Jobs, importsResult.MergedJobs)
	}

	workflowData.Roles = c.extractRoles(frontmatter)
	workflowData.Bots = c.extractBots(frontmatter)

	// Use the already extracted output configuration
	workflowData.SafeOutputs = safeOutputs

	// Extract safe-inputs configuration
	workflowData.SafeInputs = c.extractSafeInputsConfig(frontmatter)

	// Merge safe-inputs from imports
	if len(importsResult.MergedSafeInputs) > 0 {
		workflowData.SafeInputs = c.mergeSafeInputs(workflowData.SafeInputs, importsResult.MergedSafeInputs)
	}

	// Extract safe-jobs from safe-outputs.jobs location
	topSafeJobs := extractSafeJobsFromFrontmatter(frontmatter)

	// Process @include directives to extract additional safe-outputs configurations
	includedSafeOutputsConfigs, err := parser.ExpandIncludesForSafeOutputs(markdown, markdownDir)
	if err != nil {
		return fmt.Errorf("failed to expand includes for safe-outputs: %w", err)
	}

	// Combine imported safe-outputs with included safe-outputs
	var allSafeOutputsConfigs []string
	if len(importsResult.MergedSafeOutputs) > 0 {
		allSafeOutputsConfigs = append(allSafeOutputsConfigs, importsResult.MergedSafeOutputs...)
	}
	if len(includedSafeOutputsConfigs) > 0 {
		allSafeOutputsConfigs = append(allSafeOutputsConfigs, includedSafeOutputsConfigs...)
	}

	// Merge safe-jobs from all safe-outputs configurations (imported and included)
	includedSafeJobs, err := c.mergeSafeJobsFromIncludedConfigs(topSafeJobs, allSafeOutputsConfigs)
	if err != nil {
		return fmt.Errorf("failed to merge safe-jobs from includes: %w", err)
	}

	// Merge app configuration from included safe-outputs configurations
	includedApp, err := c.mergeAppFromIncludedConfigs(workflowData.SafeOutputs, allSafeOutputsConfigs)
	if err != nil {
		return fmt.Errorf("failed to merge app from includes: %w", err)
	}

	// Ensure SafeOutputs exists and populate the Jobs field with merged jobs
	if workflowData.SafeOutputs == nil && len(includedSafeJobs) > 0 {
		workflowData.SafeOutputs = &SafeOutputsConfig{}
	}
	// Always use the merged includedSafeJobs as it contains both main and imported jobs
	if workflowData.SafeOutputs != nil && len(includedSafeJobs) > 0 {
		workflowData.SafeOutputs.Jobs = includedSafeJobs
	}

	// Populate the App field if it's not set in the top-level workflow but is in an included config
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.App == nil && includedApp != nil {
		workflowData.SafeOutputs.App = includedApp
	}

	// Merge safe-outputs types from imports
	mergedSafeOutputs, err := c.MergeSafeOutputs(workflowData.SafeOutputs, allSafeOutputsConfigs)
	if err != nil {
		return fmt.Errorf("failed to merge safe-outputs from imports: %w", err)
	}
	workflowData.SafeOutputs = mergedSafeOutputs

	return nil
}

// processOnSectionAndFilters processes the on section configuration and applies various filters
func (c *Compiler) processOnSectionAndFilters(
	frontmatter map[string]any,
	workflowData *WorkflowData,
	cleanPath string,
) error {
	orchestratorWorkflowLog.Print("Processing on section and filters")

	// Process stop-after configuration from the on: section
	if err := c.processStopAfterConfiguration(frontmatter, workflowData, cleanPath); err != nil {
		return err
	}

	// Process skip-if-match configuration from the on: section
	if err := c.processSkipIfMatchConfiguration(frontmatter, workflowData); err != nil {
		return err
	}

	// Process skip-if-no-match configuration from the on: section
	if err := c.processSkipIfNoMatchConfiguration(frontmatter, workflowData); err != nil {
		return err
	}

	// Process manual-approval configuration from the on: section
	if err := c.processManualApprovalConfiguration(frontmatter, workflowData); err != nil {
		return err
	}

	// Parse the "on" section for command triggers, reactions, and other events
	if err := c.parseOnSection(frontmatter, workflowData, cleanPath); err != nil {
		return err
	}

	// Apply defaults
	if err := c.applyDefaults(workflowData, cleanPath); err != nil {
		return err
	}

	// Apply pull request draft filter if specified
	c.applyPullRequestDraftFilter(workflowData, frontmatter)

	// Apply pull request fork filter if specified
	c.applyPullRequestForkFilter(workflowData, frontmatter)

	// Apply label filter if specified
	c.applyLabelFilter(workflowData, frontmatter)

	return nil
}
