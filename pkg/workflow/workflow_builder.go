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

// buildInitialWorkflowData creates the initial WorkflowData struct with basic fields populated.
func (c *Compiler) buildInitialWorkflowData(
	result *parser.FrontmatterResult,
	toolsResult *toolsProcessingResult,
	engineSetup *engineSetupResult,
	importsResult *parser.ImportsResult,
) *WorkflowData {
	workflowBuilderLog.Print("Building initial workflow data")

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

	workflowData := &WorkflowData{
		Name:                       toolsResult.workflowName,
		FrontmatterName:            toolsResult.frontmatterName,
		FrontmatterEmoji:           toolsResult.frontmatterEmoji,
		FrontmatterYAML:            strings.Join(result.FrontmatterLines, "\n"),
		FrontmatterFieldLines:      result.FieldLines,
		RawMarkdown:                result.Markdown,
		Description:                c.extractDescription(result.Frontmatter),
		Intent:                     c.extractIntent(result.Frontmatter),
		Docs:                       docs,
		Source:                     c.extractSource(result.Frontmatter),
		Redirect:                   c.extractRedirect(result.Frontmatter),
		TrackerID:                  toolsResult.trackerID,
		MaxDailyAICredits:          resolveMaxDailyAIC(result.Frontmatter, importsResult.MergedMaxDailyAICredits),
		MaxDailyAICreditsGitHubApp: extractMaxDailyAICGitHubApp(result.Frontmatter),
		ImportedFiles:              importsResult.ImportedFiles,
		Skills:                     extractFrontmatterSkills(toolsResult.parsedFrontmatter, result.Frontmatter),
		SkillReferences:            extractFrontmatterSkillReferences(toolsResult.parsedFrontmatter, result.Frontmatter),
		Plugins:                    mergeFrontmatterPlugins(toolsResult.parsedFrontmatter, result.Frontmatter, importsResult.MergedPlugins, importsResult.MergedPluginObjects),
		PluginReferences:           mergeFrontmatterPluginReferences(toolsResult.parsedFrontmatter, result.Frontmatter, importsResult.MergedPlugins, importsResult.MergedPluginObjects),
		ImportedMarkdown:           toolsResult.importedMarkdown, // Only imports WITH inputs
		ImportPaths:                toolsResult.importPaths,      // Import paths for runtime-import macros (imports without inputs)
		PromptImports:              toolsResult.promptImports,    // Ordered prompt contributions from imports
		MainWorkflowMarkdown:       toolsResult.mainWorkflowMarkdown,
		IncludedFiles:              toolsResult.allIncludedFiles,
		ImportInputs:               importsResult.ImportInputs,
		Tools:                      toolsResult.tools,
		LSP:                        extractLSPConfig(toolsResult.parsedFrontmatter, result.Frontmatter),
		ParsedTools:                NewTools(toolsResult.tools),
		Runtimes:                   toolsResult.runtimes,
		RunInstallScripts:          toolsResult.runInstallScripts,
		MarkdownContent:            toolsResult.markdownContent,
		AI:                         engineSetup.engineSetting,
		Model:                      engineSetup.model,
		EngineConfig:               engineSetup.engineConfig,
		GHES:                       c.ghesArtifactCompat,
		AgentFile:                  agentFile,
		AgentImportSpec:            agentImportSpec,
		RepositoryImports:          importsResult.RepositoryImports,
		NetworkPermissions:         engineSetup.networkPermissions,
		SandboxConfig:              applySandboxDefaults(engineSetup.sandboxConfig, engineSetup.engineConfig),
		RunnerConfig:               extractRunnerConfig(result.Frontmatter),
		Enclaves:                   extractEnclavesConfig(result.Frontmatter),
		NeedsTextOutput:            toolsResult.needsTextOutput,
		ToolsTimeout:               toolsResult.toolsTimeout,
		ToolsStartupTimeout:        toolsResult.toolsStartupTimeout,
		TrialMode:                  c.trialMode,
		TrialLogicalRepo:           c.trialLogicalRepoSlug,
		UseSamples:                 c.samplesEnabledFromImports(result.Frontmatter, importsResult.MergedFeatures),
		StrictMode:                 c.strictMode,
		AllowActionRefs:            c.allowActionRefs,
		ValidateAWFConfig:          !c.skipValidation,
		SecretMasking:              toolsResult.secretMasking,
		ParsedFrontmatter:          toolsResult.parsedFrontmatter,
		RawFrontmatter:             result.Frontmatter,
		ResolvedMCPServers:         toolsResult.resolvedMCPServers,
		HasExplicitGitHubTool:      toolsResult.hasExplicitGitHubTool,
		ActionMode:                 c.actionMode,
		InlinedImports:             inlinedImports,
		EngineConfigSteps:          engineSetup.configSteps,
	}
	if importsResult.MergedConcurrency != "" && !hasExplicitConcurrencyGroup(result.Frontmatter) {
		var importedConcurrency any
		if err := json.Unmarshal([]byte(importsResult.MergedConcurrency), &importedConcurrency); err == nil {
			workflowData.Concurrency = c.extractTopLevelYAMLSection(map[string]any{"concurrency": importedConcurrency}, "concurrency")
		} else {
			workflowBuilderLog.Printf("Skipping imported concurrency merge: invalid JSON: %v", err)
		}
	}

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
			} else if configs, err := ParseCheckoutConfigs(rawCheckout); err == nil {
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
				workflowBuilderLog.Printf("Failed to unmarshal imported checkout JSON: %v", err)
				continue
			}
			importedConfigs, err := ParseCheckoutConfigs(raw)
			if err != nil {
				workflowBuilderLog.Printf("Failed to parse imported checkout configs: %v", err)
				continue
			}
			workflowData.CheckoutConfigs = append(workflowData.CheckoutConfigs, importedConfigs...)
		}
	}

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

	// Populate check-for-updates flag: disabled when check-for-updates: false is set in frontmatter.
	if toolsResult.parsedFrontmatter != nil && toolsResult.parsedFrontmatter.UpdateCheck != nil {
		workflowData.UpdateCheckDisabled = !*toolsResult.parsedFrontmatter.UpdateCheck
	} else if rawVal, ok := result.Frontmatter["check-for-updates"]; ok {
		if boolVal, ok := rawVal.(bool); ok && !boolVal {
			workflowData.UpdateCheckDisabled = true
		}
	}

	// Populate stale-check flag: disabled when on.stale-check: false is set in frontmatter;
	// full mode when on.stale-check: full is set.
	if onVal, ok := result.Frontmatter["on"]; ok {
		if onMap, ok := onVal.(map[string]any); ok {
			if staleCheck, ok := onMap["stale-check"]; ok {
				if boolVal, ok := staleCheck.(bool); ok && !boolVal {
					workflowData.StaleCheckDisabled = true
				} else if strVal, ok := staleCheck.(string); ok && strVal == "full" {
					workflowData.StaleCheckFull = true
				}
			}
		}
	}

	// Populate model mappings: merge builtin aliases with any imported-workflow aliases.
	workflowData.ModelMappings = MergeImportedModelAliases(importsResult.MergedModels, nil)

	mainModelCosts := extractMainModelCostsOverlay(toolsResult, result.Frontmatter)
	mergedModelCosts := mergeModelCostOverlays(importsResult.MergedModelCosts, mainModelCosts)
	if len(mergedModelCosts) > 0 {
		workflowData.ModelCosts = mergedModelCosts
	}
	// Attempt to resolve pricing for the workflow model from models.dev when it is absent
	// from both the frontmatter overlay and the embedded models.json catalog.  The result
	// is injected into ModelCosts so the runtime receives it via GH_AW_INFO_MODEL_COSTS.
	workflowData.ModelCosts = c.resolveModelPricingIfMissing(workflowData.ModelCosts, workflowData)
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

	// Populate explicitly excluded env var names: union of imported workflows' excluded-env
	// and the main workflow's excluded-env. Deduplicate and sort for stability.
	var mainExcludedEnv []string
	if toolsResult.parsedFrontmatter != nil {
		mainExcludedEnv = toolsResult.parsedFrontmatter.ExcludedEnv
	}
	if names := mergeExcludedEnvVarNames(importsResult.MergedExcludedEnv, mainExcludedEnv); len(names) > 0 {
		workflowData.ExcludedEnv = names
	}

	return workflowData
}
