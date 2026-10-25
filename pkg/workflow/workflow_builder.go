package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var workflowBuilderLog = logger.New("workflow:workflow_builder")

// ParseStaleCheckMode returns whether stale checks are disabled or use full body checking.
func ParseStaleCheckMode(frontmatter map[string]any) (disabled, full bool) {
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		return false, false
	}
	switch staleCheck := on["stale-check"].(type) {
	case bool:
		return !staleCheck, false
	case string:
		return false, staleCheck == "full"
	default:
		return false, false
	}
}

// buildInitialWorkflowData creates the initial WorkflowData struct with basic fields populated.
func (c *Compiler) buildInitialWorkflowData(
	result *parser.FrontmatterResult,
	toolsResult *toolsProcessingResult,
	engineSetup *engineSetupResult,
	importsResult *parser.ImportsResult,
) (*WorkflowData, error) {
	workflowBuilderLog.Print("Building initial workflow data")
	_, subAgents, _ := parser.ExtractInlineSubAgents(result.Markdown)

	inlinedImports := resolveInlinedImports(result.Frontmatter)

	// When inlined-imports is true, agent file content is already inlined via ImportPaths → step 1b.
	// Clear AgentFile/AgentImportSpec so engines don't read it from disk separately at runtime.
	agentFile := importsResult.AgentFile
	agentImportSpec := importsResult.AgentImportSpec
	if inlinedImports {
		agentFile = ""
		agentImportSpec = ""
	}
	docs := c.extractMetadataDocs(result.Frontmatter)
	if docs == "" {
		docs = importsResult.MergedMetadataDocs
	}
	dailyCredits, dailyCreditsExplicit := resolveMaxDailyAICWithSource(result.Frontmatter, importsResult.MergedMaxDailyAICredits)

	workflowData := c.initialWorkflowMetadata(result, toolsResult, importsResult, docs, dailyCredits, dailyCreditsExplicit)
	populateInitialWorkflowTools(workflowData, result, toolsResult, importsResult, subAgents)
	c.populateInitialWorkflowEngine(workflowData, result, toolsResult, engineSetup, importsResult, agentFile, agentImportSpec, inlinedImports)
	c.applyImportedWorkflowConcurrency(workflowData, result, importsResult)
	if err := populateInitialWorkflowCheckout(workflowData, result, toolsResult, importsResult); err != nil {
		return nil, err
	}
	c.finalizeInitialWorkflowCheckout(workflowData, result)
	populateInitialWorkflowUpdateFlags(workflowData, result, toolsResult)
	c.populateInitialWorkflowModels(workflowData, result, toolsResult, importsResult)
	populateInitialWorkflowExcludedEnv(workflowData, toolsResult, importsResult)
	return workflowData, nil
}

func (c *Compiler) initialWorkflowMetadata(result *parser.FrontmatterResult, toolsResult *toolsProcessingResult, importsResult *parser.ImportsResult, docs string, dailyCredits *string, dailyCreditsExplicit bool) *WorkflowData {
	return &WorkflowData{
		Name:                       toolsResult.workflowName,
		AdmissionEnabled:           c.isAdmissionEnabled(result.Frontmatter),
		FrontmatterName:            toolsResult.frontmatterName,
		FrontmatterEmoji:           toolsResult.frontmatterEmoji,
		FrontmatterYAML:            strings.Join(result.FrontmatterLines, "\n"),
		ActionPinSourceVersions:    sourceActionPinVersions(result, importsResult.ActionPinSourceVersions),
		FrontmatterFieldLines:      result.FieldLines,
		RawMarkdown:                result.Markdown,
		Description:                c.extractDescription(result.Frontmatter),
		Intent:                     c.extractIntent(result.Frontmatter),
		Docs:                       docs,
		Source:                     c.extractSource(result.Frontmatter),
		Redirect:                   c.extractRedirect(result.Frontmatter),
		TrackerID:                  toolsResult.trackerID,
		MaxDailyAICredits:          dailyCredits,
		MaxDailyAICreditsExplicit:  dailyCreditsExplicit,
		MaxDailyAICreditsGitHubApp: extractMaxDailyAICGitHubApp(result.Frontmatter),
		MaxDailyAICContinueOnError: resolveMaxDailyAICContinueOnError(result.Frontmatter, importsResult.MergedMaxDailyAICredits),
		ImportedFiles:              importsResult.ImportedFiles,
	}
}

func populateInitialWorkflowTools(workflowData *WorkflowData, result *parser.FrontmatterResult, toolsResult *toolsProcessingResult, importsResult *parser.ImportsResult, subAgents []parser.InlineSubAgent) {
	workflowData.Skills = extractFrontmatterSkills(toolsResult.parsedFrontmatter, result.Frontmatter)
	workflowData.SkillReferences = extractFrontmatterSkillReferences(toolsResult.parsedFrontmatter, result.Frontmatter)
	workflowData.Plugins = mergeFrontmatterPlugins(toolsResult.parsedFrontmatter, result.Frontmatter, importsResult.MergedPlugins, importsResult.MergedPluginObjects)
	workflowData.PluginReferences = mergeFrontmatterPluginReferences(toolsResult.parsedFrontmatter, result.Frontmatter, importsResult.MergedPlugins, importsResult.MergedPluginObjects)
	workflowData.ImportedMarkdown = toolsResult.importedMarkdown
	workflowData.ImportPaths = toolsResult.importPaths
	workflowData.PromptImports = toolsResult.promptImports
	workflowData.MainWorkflowMarkdown = toolsResult.mainWorkflowMarkdown
	workflowData.IncludedFiles = toolsResult.allIncludedFiles
	workflowData.ImportInputs = importsResult.ImportInputs
	workflowData.Tools = toolsResult.tools
	workflowData.ExplicitlyDisabledTools = toolsResult.explicitlyDisabledTools
	workflowData.LSP = extractLSPConfig(toolsResult.parsedFrontmatter, result.Frontmatter)
	workflowData.ParsedTools = NewTools(toolsResult.tools)
	workflowData.Runtimes = toolsResult.runtimes
	workflowData.RunInstallScripts = toolsResult.runInstallScripts
	workflowData.MarkdownContent = toolsResult.markdownContent
	workflowData.SubAgentModels = append(toolsResult.subAgentModels, importsResult.SubAgentModels...)
	workflowData.SubAgents = append(subAgents, importsResult.SubAgents...)
}

func (c *Compiler) populateInitialWorkflowEngine(workflowData *WorkflowData, result *parser.FrontmatterResult, toolsResult *toolsProcessingResult, engineSetup *engineSetupResult, importsResult *parser.ImportsResult, agentFile, agentImportSpec string, inlinedImports bool) {
	workflowData.AI = engineSetup.engineSetting
	workflowData.Model = engineSetup.model
	workflowData.EngineConfig = engineSetup.engineConfig
	workflowData.GHES = c.ghesArtifactCompat
	workflowData.AgentFile = agentFile
	workflowData.AgentImportSpec = agentImportSpec
	workflowData.RepositoryImports = importsResult.RepositoryImports
	workflowData.NetworkPermissions = engineSetup.networkPermissions
	workflowData.SandboxConfig = applySandboxDefaults(engineSetup.sandboxConfig, engineSetup.engineConfig)
	workflowData.RunnerConfig = extractRunnerConfig(result.Frontmatter)
	workflowData.Enclaves = extractEnclavesConfig(result.Frontmatter)
	workflowData.NeedsTextOutput = toolsResult.needsTextOutput
	workflowData.ToolsTimeout = toolsResult.toolsTimeout
	workflowData.ToolsStartupTimeout = toolsResult.toolsStartupTimeout
	workflowData.TrialMode = c.trialMode
	workflowData.TrialLogicalRepo = c.trialLogicalRepoSlug
	workflowData.UseSamples = c.samplesEnabledFromImports(result.Frontmatter, importsResult.MergedFeatures)
	workflowData.StrictMode = c.strictMode
	workflowData.AllowActionRefs = c.allowActionRefs
	workflowData.ValidateAWFConfig = !c.skipValidation
	workflowData.SecretMasking = toolsResult.secretMasking
	workflowData.ParsedFrontmatter = toolsResult.parsedFrontmatter
	workflowData.RawFrontmatter = result.Frontmatter
	workflowData.ResolvedMCPServers = toolsResult.resolvedMCPServers
	workflowData.HasExplicitGitHubTool = toolsResult.hasExplicitGitHubTool
	workflowData.ActionMode = c.actionMode
	workflowData.InlinedImports = inlinedImports
	workflowData.EngineConfigSteps = engineSetup.configSteps
}

func (c *Compiler) applyImportedWorkflowConcurrency(workflowData *WorkflowData, result *parser.FrontmatterResult, importsResult *parser.ImportsResult) {
	if importsResult.MergedConcurrency != "" && !hasExplicitConcurrencyGroup(result.Frontmatter) {
		var importedConcurrency any
		if err := json.Unmarshal([]byte(importsResult.MergedConcurrency), &importedConcurrency); err == nil {
			workflowData.Concurrency = c.extractTopLevelYAMLSection(map[string]any{"concurrency": importedConcurrency}, "concurrency")
		} else {
			workflowBuilderLog.Printf("Skipping imported concurrency merge: invalid JSON: %v", err)
		}
	}
}

func populateInitialWorkflowCheckout(workflowData *WorkflowData, result *parser.FrontmatterResult, toolsResult *toolsProcessingResult, importsResult *parser.ImportsResult) error {
	// Populate checkout configs from parsed frontmatter.
	// Fall back to raw frontmatter parsing when full ParseFrontmatterConfig fails
	// (e.g. due to unrecognised tool config shapes like bash: ["*"]).
	if toolsResult.parsedFrontmatter != nil {
		workflowData.CheckoutConfigs = toolsResult.parsedFrontmatter.CheckoutConfigs
		workflowData.CheckoutDisabled = toolsResult.parsedFrontmatter.CheckoutDisabled
		workflowData.CheckoutExplicitlyDisabled = toolsResult.parsedFrontmatter.CheckoutExplicitlyDisabled
		workflowData.CheckoutSkipDefault = toolsResult.parsedFrontmatter.CheckoutSkipDefault
	} else {
		if rawCheckout, ok := result.Frontmatter["checkout"]; ok {
			if checkoutValue, ok := rawCheckout.(bool); ok && !checkoutValue {
				workflowData.CheckoutDisabled = true
				workflowData.CheckoutExplicitlyDisabled = true
			} else {
				configs, err := ParseCheckoutConfigs(rawCheckout)
				if err != nil {
					return fmt.Errorf("invalid checkout configuration: %w", err)
				}
				workflowData.CheckoutConfigs = configs
			}
		}
		// permissions.contents: none signals that the default workflow-repository
		// checkout should be skipped (see checkoutSkipDefaultFromPermissions, the
		// single source of truth shared with the primary ParseFrontmatterConfig path).
		if checkoutSkipDefaultFromPermissions(result.Frontmatter["permissions"]) {
			workflowData.CheckoutSkipDefault = true
		}
	}

	// Merge checkout configs from imported shared workflows.
	// Imported configs are appended after the main workflow's configs so that the main
	// workflow's entries take precedence when CheckoutManager deduplicates by (repository, path).
	// checkout: false in the main workflow disables all checkout (including imports).
	if !workflowData.CheckoutDisabled && importsResult.MergedCheckout != "" {
		for line := range strings.SplitSeq(strings.TrimSpace(importsResult.MergedCheckout), "\n") {
			if line == "" {
				continue
			}
			var raw any
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				return fmt.Errorf("invalid imported checkout JSON: %w", err)
			}
			importedConfigs, err := ParseCheckoutConfigs(raw)
			if err != nil {
				return fmt.Errorf("invalid imported checkout configuration: %w", err)
			}
			workflowData.CheckoutConfigs = append(workflowData.CheckoutConfigs, importedConfigs...)
		}
	}
	return nil
}

func (c *Compiler) finalizeInitialWorkflowCheckout(workflowData *WorkflowData, result *parser.FrontmatterResult) {
	// Warn when permissions.contents: none skips the default checkout but no other
	// checkout: entries (own repo, imports) are configured, since that leaves the
	// agent with no working-directory checkout at all (effectively equivalent to
	// checkout: false, but without the explicit intent that flag signals).
	if workflowData.CheckoutSkipDefault && !workflowData.CheckoutDisabled && len(workflowData.CheckoutConfigs) == 0 {
		warningMsg := "permissions.contents: none skips the default workflow-repository checkout, " +
			"but no other checkout: entries are configured; the agent job will have no repository " +
			"checked out. Add a target checkout: entry, or set checkout: false to make the intent explicit."
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
		c.IncrementWarningCount()
	}

	// Auto-disable checkout for pull_request_target-only workflows when not explicitly configured.
	// For pull_request_target events, the head branch is often deleted (closed/merged PRs)
	// or inaccessible (fork PRs), causing the "Checkout PR branch" step to fail.
	// Users who need checkout can explicitly set a checkout configuration in frontmatter.
	// This block runs after import merging so that imported checkout configs prevent auto-disable.
	// Auto-disable is skipped when pull_request (or other checkout-compatible) triggers co-exist,
	// because those events do have accessible head branches.
	onVal := result.Frontmatter["on"]
	hasPRT := frontmatterHasTrigger(onVal, "pull_request_target")
	hasPR := frontmatterHasTrigger(onVal, "pull_request")
	if hasPRT && !hasPR {
		// Mark the workflow as pull_request_target-only so ShouldGeneratePRCheckoutStep
		// suppresses the checkout_pr_branch.cjs step regardless of checkout configuration.
		workflowData.IsPullRequestTarget = true

		if !workflowData.CheckoutDisabled && len(workflowData.CheckoutConfigs) == 0 {
			if _, checkoutExplicitlySet := result.Frontmatter["checkout"]; !checkoutExplicitlySet {
				workflowBuilderLog.Print("Auto-disabling checkout for pull_request_target workflow")
				workflowData.CheckoutDisabled = true
			}
		}
	}
}

func populateInitialWorkflowUpdateFlags(workflowData *WorkflowData, result *parser.FrontmatterResult, toolsResult *toolsProcessingResult) {
	// Populate check-for-updates flag: disabled when check-for-updates: false is set in frontmatter.
	if toolsResult.parsedFrontmatter != nil && toolsResult.parsedFrontmatter.UpdateCheck != nil {
		workflowData.UpdateCheckDisabled = !*toolsResult.parsedFrontmatter.UpdateCheck
	} else if rawVal, ok := result.Frontmatter["check-for-updates"]; ok {
		if boolVal, ok := rawVal.(bool); ok && !boolVal {
			workflowData.UpdateCheckDisabled = true
		}
	}

	workflowData.StaleCheckDisabled, workflowData.StaleCheckFull = ParseStaleCheckMode(result.Frontmatter)
	// Populate report-blocked-version flag: disabled when on.report-blocked-version: false is
	// set in frontmatter (suppresses only the activation-stage notification issue, independent
	// of check-for-updates and safe-outputs.report-failure-as-issue).
	if onVal, ok := result.Frontmatter["on"]; ok {
		if onMap, ok := onVal.(map[string]any); ok {
			if reportBlockedVersion, ok := onMap["report-blocked-version"]; ok {
				if boolVal, ok := reportBlockedVersion.(bool); ok && !boolVal {
					workflowData.ReportBlockedVersionDisabled = true
				}
			}
		}
	}
}

func (c *Compiler) populateInitialWorkflowModels(workflowData *WorkflowData, result *parser.FrontmatterResult, toolsResult *toolsProcessingResult, importsResult *parser.ImportsResult) {
	// Populate model mappings: builtin, then imported, then main-workflow aliases (main wins).
	workflowData.ModelMappings = MergeImportedModelAliases(importsResult.MergedModels, extractMainModelAliases(result.Frontmatter))

	mainModelCosts := extractMainModelCostsOverlay(toolsResult, result.Frontmatter)
	mergedModelCosts := mergeModelCostOverlays(importsResult.MergedModelCosts, mainModelCosts)
	if len(mergedModelCosts) > 0 {
		workflowData.ModelCosts = mergedModelCosts
	}
	// Attempt to resolve pricing for the workflow model from models.dev when it is absent
	// from both the frontmatter overlay and the embedded models.json catalog.  The result
	// is injected into ModelCosts so the runtime receives it via GH_AW_INFO_MODEL_COSTS.
	workflowData.ModelCosts = c.resolveModelPricingIfMissing(workflowData.ModelCosts, workflowData)
	workflowData.ModelCosts = c.resolveFallbackModelPricing(workflowData.ModelCosts, workflowData)
	mainModelPolicy := extractMainModelPolicyOverlay(toolsResult, result.Frontmatter)
	allowedModels, disallowedModels := mergeModelPolicyOverlays(importsResult.MergedModelPolicies, mainModelPolicy)
	if len(allowedModels) > 0 {
		workflowData.ModelPolicyAllowed = allowedModels
	}
	if len(disallowedModels) > 0 {
		workflowData.ModelPolicyBlocked = disallowedModels
	}

	if pricing := resolveDefaultAiCreditsPricing(result.Frontmatter, importsResult.MergedDefaultAiCreditsPricing); pricing != nil {
		workflowData.DefaultAiCreditsPricing = pricing
	}
}

func populateInitialWorkflowExcludedEnv(workflowData *WorkflowData, toolsResult *toolsProcessingResult, importsResult *parser.ImportsResult) {
	// Populate explicitly excluded env var names: union of imported workflows' excluded-env
	// and the main workflow's excluded-env. Deduplicate and sort for stability.
	var mainExcludedEnv []string
	if toolsResult.parsedFrontmatter != nil {
		mainExcludedEnv = toolsResult.parsedFrontmatter.ExcludedEnv
	}
	if names := mergeExcludedEnvVarNames(importsResult.MergedExcludedEnv, mainExcludedEnv); len(names) > 0 {
		workflowData.ExcludedEnv = names
	}

}

// sourceActionPinVersions collects inline labels from frontmatter and imported action steps.
func sourceActionPinVersions(result *parser.FrontmatterResult, imported map[string]string) map[string]string {
	versions := parser.ActionPinSourceVersions([]byte(strings.Join(result.FrontmatterLines, "\n")))
	if versions == nil {
		versions = make(map[string]string)
	}
	for reference, label := range imported {
		if _, exists := versions[reference]; !exists {
			versions[reference] = label
		}
	}
	return versions
}
