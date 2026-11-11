// import_accumulator.go defines importAccumulator, which centralizes result-building
// state for imported workflow files.
package parser

import (
	"strings"
)

// importAccumulator centralizes the builder/slice/set variables used during
// BFS import traversal. It accumulates results from all imported files and provides
// a method to convert the accumulated state into the final ImportsResult.
type importAccumulator struct {
	connectors               []ConnectorImport
	toolsBuilder             strings.Builder
	mcpServersBuilder        strings.Builder
	markdownBuilder          strings.Builder // imports with substituted inputs or schema defaults (compile-time substitution)
	importPaths              []string        // Import paths for runtime-import macro generation
	promptImports            []PromptImportEntry
	stepsBuilder             strings.Builder
	copilotSetupStepsBuilder strings.Builder // Steps from copilot-setup-steps.yml (inserted at start)
	actionPinSourceVersions  map[string]string
	preStepsBuilder          strings.Builder
	preAgentStepsBuilder     strings.Builder
	runtimesBuilder          strings.Builder
	servicesBuilder          strings.Builder
	networkBuilder           strings.Builder
	permissionsBuilder       strings.Builder
	secretMaskingBuilder     strings.Builder
	postStepsBuilder         strings.Builder
	jobsBuilder              strings.Builder   // Jobs from imported YAML workflows
	envBuilder               strings.Builder   // env vars from imported workflows (JSON, one object per line)
	envSources               map[string]string // env var name → source import path (for conflict detection and header listing)
	observabilityConfigs     []string          // observability config JSON blobs from all imports (merged into endpoint array)
	engines                  []string
	plugins                  []string
	pluginObjects            []map[string]any
	safeOutputs              []string
	gradersBuilder           strings.Builder
	graderRunSources         map[string]string
	mcpScripts               []string
	bots                     []string
	botsSet                  map[string]bool
	labels                   []string
	labelsSet                map[string]bool
	skipRoles                []string
	skipRolesSet             map[string]bool
	skipBots                 []string
	skipBotsSet              map[string]bool
	skipIfMatch              string
	skipIfNoMatch            string
	ambientFolders           []string
	ambientFoldersSet        map[string]bool
	sandboxAgentMounts       []string
	sandboxAgentMountsSet    map[string]bool
	caches                   []string
	features                 []map[string]any
	metadataDocs             string
	models                   []map[string][]string // model alias maps from each imported file (appended in import order)
	modelPolicies            []map[string][]string // model policy sets from each imported file (appended in import order)
	modelCosts               []map[string]any      // model pricing overlays from each imported file (appended in import order)
	defaultAiCreditsPricing  map[string]any        // first models.default-ai-credits-pricing object found in imports (first-wins)
	runInstallScripts        bool                  // true if any imported workflow sets runtimes.node.run-install-scripts: true
	agentFile                string
	agentImportSpec          string
	repositoryImports        []string
	importInputs             map[string]any
	// First on.github-token / on.github-app found across all imported files (first-wins strategy)
	activationGitHubToken string
	activationGitHubApp   string // JSON-encoded GitHubAppConfig
	// First top-level github-app found across all imported files (first-wins strategy)
	topLevelGitHubApp string // JSON-encoded GitHubAppConfig
	// Checkout configs from all imported files (append in order; main workflow's checkouts take precedence)
	checkouts []string // JSON-encoded checkout values, one per import
	// First engine.mcp.tool-timeout / engine.mcp.session-timeout found across all imported files (first-wins strategy)
	mergedEngineMCPToolTimeout    string // Go duration string (e.g. "10m", "30s")
	mergedEngineMCPSessionTimeout string // Go duration string (e.g. "4h", "30m")
	// First engine.model found in imports that have no engine.id (first-wins strategy).
	// These express a model preference without selecting a specific engine.
	mergedEngineModel string
	// engineModelChoiceDeclared records that an import declares a top-level model
	// or a preference-only engine model choice, including empty values.
	engineModelChoiceDeclared bool
	// First top-level max-turns / max-runs / max-ai-credits /
	// max-daily-ai-credits found across imports (first-wins).
	// Values are stored as JSON-encoded raw values so numeric literals and strings
	// round-trip consistently through import processing.
	mergedMaxTurns           string
	mergedMaxToolDenials     string
	mergedMaxToolCalls       string
	mergedMaxRuns            string
	mergedMaxTurnCacheMisses string
	mergedMaxAICredits       string
	mergedMaxDailyAICredits  string
	mergedConcurrency        string
	mergedJobDiscriminator   string
	// Union of excluded-env lists from all imported files (deduplicated).
	excludedEnv    []string
	excludedEnvSet map[string]bool
	// Best-effort sub-agent frontmatter warnings collected during BFS traversal.
	warnings       []string
	subAgentModels []SubAgentModel
	subAgents      []InlineSubAgent
}

// newImportAccumulator creates and initializes a new importAccumulator.
// Maps (botsSet, etc.) are explicitly initialized to prevent nil map panics
// during deduplication. Slices are left as nil, which is valid for append operations.
func newImportAccumulator() *importAccumulator {
	return &importAccumulator{
		botsSet:                 make(map[string]bool),
		labelsSet:               make(map[string]bool),
		skipRolesSet:            make(map[string]bool),
		skipBotsSet:             make(map[string]bool),
		ambientFoldersSet:       make(map[string]bool),
		importInputs:            make(map[string]any),
		envSources:              make(map[string]string),
		sandboxAgentMountsSet:   make(map[string]bool),
		excludedEnvSet:          make(map[string]bool),
		actionPinSourceVersions: make(map[string]string),
		graderRunSources:        make(map[string]string),
	}
}

// toImportsResult converts the accumulated state to a final ImportsResult.
// topologicalOrder is the result from topologicalSortImports.
func (acc *importAccumulator) toImportsResult(topologicalOrder []string) *ImportsResult {
	parserLog.Printf("Building ImportsResult: importedFiles=%d, importPaths=%d, engines=%d, bots=%d, labels=%d",
		len(topologicalOrder), len(acc.importPaths), len(acc.engines), len(acc.bots), len(acc.labels))
	result := acc.buildImportsResult()
	result.ImportedFiles = topologicalOrder
	return result
}

// buildImportsResult constructs the ImportsResult from accumulated state, excluding
// ImportedFiles which is populated separately from the topological sort order.
func (acc *importAccumulator) buildImportsResult() *ImportsResult {
	result := &ImportsResult{
		Connectors:                    acc.connectors,
		MergedTools:                   acc.toolsBuilder.String(),
		MergedMCPServers:              acc.mcpServersBuilder.String(),
		MergedEngines:                 acc.engines,
		MergedPlugins:                 acc.plugins,
		MergedPluginObjects:           acc.pluginObjects,
		MergedSafeOutputs:             acc.safeOutputs,
		MergedGraders:                 acc.gradersBuilder.String(),
		GraderRunSources:              acc.graderRunSources,
		MergedMCPScripts:              acc.mcpScripts,
		MergedMarkdown:                acc.markdownBuilder.String(),
		ImportPaths:                   acc.importPaths,
		PromptImports:                 acc.promptImports,
		MergedSteps:                   acc.stepsBuilder.String(),
		CopilotSetupSteps:             acc.copilotSetupStepsBuilder.String(),
		ActionPinSourceVersions:       acc.actionPinSourceVersions,
		MergedPreSteps:                acc.preStepsBuilder.String(),
		MergedPreAgentSteps:           acc.preAgentStepsBuilder.String(),
		MergedRuntimes:                acc.runtimesBuilder.String(),
		MergedRunInstallScripts:       acc.runInstallScripts,
		MergedServices:                acc.servicesBuilder.String(),
		MergedNetwork:                 acc.networkBuilder.String(),
		MergedSandboxAgentMounts:      acc.sandboxAgentMounts,
		MergedPermissions:             acc.permissionsBuilder.String(),
		MergedSecretMasking:           acc.secretMaskingBuilder.String(),
		MergedBots:                    acc.bots,
		MergedSkipRoles:               acc.skipRoles,
		MergedSkipBots:                acc.skipBots,
		MergedSkipIfMatch:             acc.skipIfMatch,
		MergedSkipIfNoMatch:           acc.skipIfNoMatch,
		MergedAmbientFolders:          acc.ambientFolders,
		MergedPostSteps:               acc.postStepsBuilder.String(),
		MergedLabels:                  acc.labels,
		MergedCaches:                  acc.caches,
		MergedJobs:                    acc.jobsBuilder.String(),
		MergedEnv:                     acc.envBuilder.String(),
		MergedEnvSources:              acc.envSources,
		MergedFeatures:                acc.features,
		MergedMetadataDocs:            acc.metadataDocs,
		MergedModels:                  acc.models,
		MergedModelPolicies:           acc.modelPolicies,
		MergedModelCosts:              acc.modelCosts,
		MergedDefaultAiCreditsPricing: acc.defaultAiCreditsPricing,
		MergedObservability:           mergeObservabilityConfigs(acc.observabilityConfigs),
		AgentFile:                     acc.agentFile,
		SubAgentModels:                acc.subAgentModels,
		SubAgents:                     acc.subAgents,
		AgentImportSpec:               acc.agentImportSpec,
		RepositoryImports:             acc.repositoryImports,
		ImportInputs:                  acc.importInputs,
		Warnings:                      acc.warnings,
	}
	acc.populateImportsResultScalars(result)
	return result
}
func (acc *importAccumulator) populateImportsResultScalars(result *ImportsResult) {
	result.MergedActivationGitHubToken = acc.activationGitHubToken
	result.MergedActivationGitHubApp = acc.activationGitHubApp
	result.MergedTopLevelGitHubApp = acc.topLevelGitHubApp
	result.MergedCheckout = strings.Join(acc.checkouts, "\n")
	result.MergedEngineMCPToolTimeout = acc.mergedEngineMCPToolTimeout
	result.MergedEngineMCPSessionTimeout = acc.mergedEngineMCPSessionTimeout
	result.MergedEngineModel = acc.mergedEngineModel
	result.EngineModelChoiceDeclared = acc.engineModelChoiceDeclared
	result.MergedMaxTurns = acc.mergedMaxTurns
	result.MergedMaxToolDenials = acc.mergedMaxToolDenials
	result.MergedMaxToolCalls = acc.mergedMaxToolCalls
	result.MergedMaxRuns = acc.mergedMaxRuns
	result.MergedMaxTurnCacheMisses = acc.mergedMaxTurnCacheMisses
	result.MergedMaxAICredits = acc.mergedMaxAICredits
	result.MergedMaxDailyAICredits = acc.mergedMaxDailyAICredits
	result.MergedConcurrency = acc.mergedConcurrency
	result.MergedJobDiscriminator = acc.mergedJobDiscriminator
	result.MergedExcludedEnv = acc.excludedEnv
}
