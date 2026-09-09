package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/syncutil"
)

var importsLog = logger.New("workflow:imports")

// MergeTools merges two tools maps, combining allowed arrays when keys coincide
// Handles newline-separated JSON objects from multiple imports/includes
func (c *Compiler) MergeTools(topTools map[string]any, includedToolsJSON string) (map[string]any, error) {
	importsLog.Print("Merging tools from imports")

	if includedToolsJSON == "" || includedToolsJSON == "{}" {
		importsLog.Print("No included tools to merge")
		return topTools, nil
	}

	// Split by newlines to handle multiple JSON objects from different imports/includes
	lines := strings.Split(includedToolsJSON, "\n")
	result := topTools
	if result == nil {
		result = make(map[string]any)
	}

	importsLog.Printf("Processing %d tool definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		var includedTools map[string]any
		if err := json.Unmarshal([]byte(line), &includedTools); err != nil {
			continue // Skip invalid lines
		}

		// Merge this set of tools
		merged, err := parser.MergeTools(result, includedTools)
		if err != nil {
			importsLog.Printf("Failed to merge tools: %v", err)
			return nil, fmt.Errorf("failed to merge tools: %w", err)
		}
		result = merged
	}

	importsLog.Printf("Successfully merged %d tools", len(result))
	return result, nil
}

// MergeMCPServers merges mcp-servers from imports with top-level mcp-servers
// Takes object maps and merges them directly
func (c *Compiler) MergeMCPServers(topMCPServers map[string]any, importedMCPServersJSON string) (map[string]any, error) {
	importsLog.Print("Merging MCP servers from imports")

	if importedMCPServersJSON == "" || importedMCPServersJSON == "{}" {
		importsLog.Print("No imported MCP servers to merge")
		return topMCPServers, nil
	}

	// Initialize result with top-level MCP servers
	result := make(map[string]any)
	maps.Copy(result, topMCPServers)

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(importedMCPServersJSON, "\n")
	importsLog.Printf("Processing %d MCP server definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to map
		var importedMCPServers map[string]any
		if err := json.Unmarshal([]byte(line), &importedMCPServers); err != nil {
			continue // Skip invalid lines
		}

		// Merge MCP servers - imported servers take precedence over top-level ones
		for serverName, serverConfig := range importedMCPServers {
			importsLog.Printf("Merging MCP server: %s", serverName)
			result[serverName] = serverConfig
		}
	}

	importsLog.Printf("Successfully merged %d MCP servers", len(result))
	return result, nil
}

// MergeNetworkPermissions merges network permissions from imports with top-level network permissions
// Combines allowed domains from both sources into a single list
func (c *Compiler) MergeNetworkPermissions(topNetwork *NetworkPermissions, importedNetworkJSON string) (*NetworkPermissions, error) {
	importsLog.Print("Merging network permissions from imports")

	// If no imported network config, return top-level network as-is
	if importedNetworkJSON == "" || importedNetworkJSON == "{}" {
		importsLog.Print("No imported network permissions to merge")
		return topNetwork, nil
	}

	// Start with top-level network or create a new one
	result := &NetworkPermissions{}
	if topNetwork != nil {
		result.Allowed = make([]string, len(topNetwork.Allowed))
		copy(result.Allowed, topNetwork.Allowed)
		importsLog.Printf("Starting with %d top-level allowed domains", len(topNetwork.Allowed))
	}

	// Track domains to avoid duplicates
	domainSet := make(map[string]struct {
	})
	for _, domain := range result.Allowed {
		domainSet[domain] = struct {
		}{}
	}

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(importedNetworkJSON, "\n")
	importsLog.Printf("Processing %d network permission lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to NetworkPermissions struct
		var importedNetwork NetworkPermissions
		if err := json.Unmarshal([]byte(line), &importedNetwork); err != nil {
			continue // Skip invalid lines
		}

		// Merge allowed domains from imported network
		for _, domain := range importedNetwork.Allowed {
			if !setutil.Contains(domainSet, domain) {
				result.Allowed = append(result.Allowed, domain)
				domainSet[domain] = struct {
				}{}
			}
		}
	}

	// Sort the final domain list for consistent output
	sort.Strings(result.Allowed)

	importsLog.Printf("Successfully merged network permissions with %d allowed domains", len(result.Allowed))
	return result, nil
}

// getSafeOutputTypeKeys returns the list of safe output type keys from the embedded schema.
// This is a cached wrapper around parser.GetSafeOutputTypeKeys() to avoid parsing on every call.
var safeOutputTypeKeysLoader syncutil.OnceLoader[[]string]

func getSafeOutputTypeKeys() ([]string, error) {
	return safeOutputTypeKeysLoader.Get(func() ([]string, error) {
		return parser.GetSafeOutputTypeKeys()
	})
}

// MergeSafeOutputs merges safe-outputs configurations from imports into the top-level safe-outputs.
// Returns an error if a conflict is detected (same safe-output type defined in both main and imported).
//
// topRawSafeOutputs is the raw safe-outputs map from the main workflow's frontmatter (may be nil).
// When provided, only keys explicitly present in the raw map are treated as "defined" in the main
// workflow for the purpose of conflict detection. This prevents auto-defaults applied by
// extractSafeOutputsConfig (e.g. threat-detection, noop, missing-tool) from blocking import
// configurations for types the user never explicitly configured.
// When nil, the processed topSafeOutputs config fields are used to determine defined types
// (legacy behavior used by unit tests that construct configs directly).
func (c *Compiler) MergeSafeOutputs(topSafeOutputs *SafeOutputsConfig, importedSafeOutputsJSON []string, topRawSafeOutputs map[string]any) (*SafeOutputsConfig, error) {
	importsLog.Print("Merging safe-outputs from imports")

	if len(importedSafeOutputsJSON) == 0 {
		importsLog.Print("No imported safe-outputs to merge")
		return topSafeOutputs, nil
	}

	// Get safe output type keys from the embedded schema
	typeKeys, err := getSafeOutputTypeKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to get safe output type keys: %w", err)
	}

	// Collect all safe output types defined in the top-level config.
	// When topRawSafeOutputs is provided (from raw frontmatter), use only keys that are
	// explicitly present in the raw map to avoid counting auto-defaults as user-defined types.
	// When nil, fall back to inspecting the processed config struct (legacy/test behaviour).
	topDefinedTypes := make(map[string]struct {
	})
	if topSafeOutputs != nil {
		for _, key := range typeKeys {
			if topRawSafeOutputs != nil {
				if _, exists := topRawSafeOutputs[key]; exists {
					topDefinedTypes[key] = struct {
					}{}
				}
			} else if hasSafeOutputType(topSafeOutputs, key) {
				topDefinedTypes[key] = struct {
				}{}
			}
		}
	}
	importsLog.Printf("Top-level safe-outputs defines %d types", len(topDefinedTypes))

	// Track types defined in imported configs for conflict detection
	importedDefinedTypes := make(map[string]struct {
	})

	// Collect all imported configs. This includes configs with only meta fields (like allowed-domains,
	// staged, env, github-token, max-patch-size, runs-on) as well as those defining safe output types.
	// Meta fields can be imported even when no safe output types are defined.
	var importedConfigs []map[string]any
	// Collect protected-files exclude lists from type-conflicting imports so they can be
	// merged as a set even when the importing config already defines the same handler type.
	// These are keyed by handler type name (e.g. "create-pull-request").
	accumulatedExclude := map[string][]string{}

	for _, configJSON := range importedSafeOutputsJSON {
		if configJSON == "" || configJSON == "{}" {
			continue
		}

		var config map[string]any
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			importsLog.Printf("Skipping malformed safe-outputs config: %v", err)
			continue
		}

		// Check for conflicts and remove types already defined in top-level config
		// Main workflow definitions take precedence over imports (override behavior).
		// Exception: protected-files.exclude is always extracted before deletion so that
		// exclude lists from imported configs are merged as a set into the result.
		for _, key := range typeKeys {
			if _, exists := config[key]; exists {
				if setutil.Contains(topDefinedTypes, key) {
					// Main workflow overrides imported definition — extract protected-files
					// exclude lists before removing the type entry.
					if handlerCfg, ok := config[key].(map[string]any); ok {
						if pf, ok := handlerCfg["protected-files"].(map[string]any); ok {
							if excludeFiles := parseStringSliceAny(pf["exclude"], importsLog); len(excludeFiles) > 0 {
								accumulatedExclude[key] = sliceutil.MergeUnique(accumulatedExclude[key], excludeFiles...)
								importsLog.Printf("Saved protected-files exclude from overridden import %s: %v", key, excludeFiles)
							}
						}
					}
					importsLog.Printf("Main workflow overrides imported safe-output: %s", key)
					delete(config, key)
					continue
				}
				if setutil.Contains(importedDefinedTypes, key) {
					return nil, fmt.Errorf("safe-outputs conflict: '%s' is defined in multiple imported workflows. Each safe-output type can only be defined once", key)
				}
				importedDefinedTypes[key] = struct {
				}{}
			}
		}
		if topRawSafeOutputs != nil {
			if _, exists := topRawSafeOutputs["steer"]; exists {
				delete(config, "steer")
			}
		}

		importedConfigs = append(importedConfigs, config)
	}

	importsLog.Printf("Found %d imported safe-outputs configs with %d types", len(importedConfigs), len(importedDefinedTypes))

	// If no imported configs found (neither safe output types nor meta fields), return the original
	if len(importedConfigs) == 0 {
		return topSafeOutputs, nil
	}

	// Initialize result with top-level config or create new one
	result := topSafeOutputs
	if result == nil {
		result = &SafeOutputsConfig{}
	}

	// Merge each imported config
	for _, config := range importedConfigs {
		var err error
		result, err = mergeSafeOutputConfig(result, config, c)
		if err != nil {
			return nil, err
		}
	}

	// Recompute the implicit report-incomplete create-issue default now that imports are
	// merged. The default is originally computed from only the main workflow's own
	// safe-outputs block, before Linear (or other) handlers supplied by an import are
	// visible. Without recomputing here, a main workflow that declares only global fields
	// (e.g. linear-token) could inherit an implicit `create-issue: true` default meant for
	// a GitHub-only configuration, even though the merged result is Linear-only.
	if result.ReportIncomplete != nil && result.ReportIncomplete.Implicit {
		recomputed := defaultReportIncompleteCreateIssue(result)
		result.ReportIncomplete.CreateIssue = &recomputed
	}

	// Apply protected-files exclude lists accumulated from type-conflicting imports.
	// These are merged as a set so that importing a base workflow can add to exclusions
	// without completely replacing the main workflow's handler configuration.
	if len(accumulatedExclude) > 0 {
		if result.CreatePullRequests != nil {
			if excludeFiles, ok := accumulatedExclude["create-pull-request"]; ok && len(excludeFiles) > 0 {
				result.CreatePullRequests.ProtectedFilesExclude = sliceutil.MergeUnique(
					result.CreatePullRequests.ProtectedFilesExclude,
					excludeFiles...,
				)
				importsLog.Printf("Merged %d accumulated protected-files exclude(s) into create-pull-request", len(excludeFiles))
			}
		}
		if result.PushToPullRequestBranch != nil {
			if excludeFiles, ok := accumulatedExclude["push-to-pull-request-branch"]; ok && len(excludeFiles) > 0 {
				result.PushToPullRequestBranch.ProtectedFilesExclude = sliceutil.MergeUnique(
					result.PushToPullRequestBranch.ProtectedFilesExclude,
					excludeFiles...,
				)
				importsLog.Printf("Merged %d accumulated protected-files exclude(s) into push-to-pull-request-branch", len(excludeFiles))
			}
		}
	}

	importsLog.Printf("Successfully merged safe-outputs from imports")
	return result, nil
}

// hasSafeOutputType checks if a SafeOutputsConfig has a specific safe output type defined
func hasSafeOutputType(config *SafeOutputsConfig, key string) bool {
	if config == nil {
		return false
	}

	handler, ok := getSafeOutputHandlerByKey(key)
	if !ok {
		return false
	}

	return hasSafeOutputFieldSet(config, handler.StructField)
}

// mergeSafeOutputConfig merges a single imported config map into the result SafeOutputsConfig
func mergeSafeOutputConfig(result *SafeOutputsConfig, config map[string]any, c *Compiler) (*SafeOutputsConfig, error) {
	importsLog.Printf("Merging imported safe-output config: key_count=%d", len(config))
	// Create a frontmatter-like structure for extractSafeOutputsConfig
	frontmatter := map[string]any{
		"safe-outputs": config,
	}

	// Use the existing extraction logic to parse the config
	importedConfig := c.extractSafeOutputsConfig(frontmatter)
	if importedConfig == nil {
		importsLog.Print("Imported safe-output config extracted no fields, skipping merge")
		return result, nil
	}

	// Merge each safe output type (only set if nil in result).
	// Types with custom merge semantics are handled below.
	specialMergeFields := map[string]struct {
	}{
		"CreatePullRequests":      {},
		"PushToPullRequestBranch": {},
		"MissingTool":             {},
		"MissingData":             {},
		"NoOp":                    {},
		"ReportIncomplete":        {},
		"ThreatDetection":         {},
	}
	for _, handler := range safeOutputHandlers {
		if setutil.Contains(specialMergeFields, handler.StructField) {
			continue
		}
		mergeSafeOutputFieldIfNil(result, importedConfig, handler.StructField)
	}

	if result.CreatePullRequests == nil && importedConfig.CreatePullRequests != nil {
		result.CreatePullRequests = importedConfig.CreatePullRequests
	} else if result.CreatePullRequests != nil && importedConfig.CreatePullRequests != nil {
		// Merge protected-files exclude lists as a set so that imports can extend exclusions
		// without replacing the top-level configuration entirely.
		result.CreatePullRequests.ProtectedFilesExclude = sliceutil.MergeUnique(
			result.CreatePullRequests.ProtectedFilesExclude,
			importedConfig.CreatePullRequests.ProtectedFilesExclude...,
		)
	}
	if result.PushToPullRequestBranch == nil && importedConfig.PushToPullRequestBranch != nil {
		result.PushToPullRequestBranch = importedConfig.PushToPullRequestBranch
	} else if result.PushToPullRequestBranch != nil && importedConfig.PushToPullRequestBranch != nil {
		// Merge protected-files exclude lists as a set so that imports can extend exclusions
		// without replacing the top-level configuration entirely.
		result.PushToPullRequestBranch.ProtectedFilesExclude = sliceutil.MergeUnique(
			result.PushToPullRequestBranch.ProtectedFilesExclude,
			importedConfig.PushToPullRequestBranch.ProtectedFilesExclude...,
		)
	}
	// missing-tool, missing-data, noop, and report-incomplete are auto-defaulted by
	// extractSafeOutputsConfig whenever any safe-outputs are present, even when the user
	// has not explicitly configured those types. This means result.X can be non-nil (the
	// auto-default) even though the main workflow never explicitly set it. We therefore use
	// the presence of the key in the raw imported config map as the authoritative signal:
	// if the import explicitly carries the key, its value wins over any auto-default in result.
	// The "|| result.X == nil" arm preserves the legacy path where result has no value at all.
	_, hasMissingTool := config["missing-tool"]
	if (hasMissingTool || result.MissingTool == nil) && importedConfig.MissingTool != nil {
		result.MissingTool = importedConfig.MissingTool
	}
	_, hasMissingData := config["missing-data"]
	if (hasMissingData || result.MissingData == nil) && importedConfig.MissingData != nil {
		result.MissingData = importedConfig.MissingData
	}
	_, hasNoop := config["noop"]
	if (hasNoop || result.NoOp == nil) && importedConfig.NoOp != nil {
		result.NoOp = importedConfig.NoOp
	}
	_, hasReportIncomplete := config["report-incomplete"]
	if (hasReportIncomplete || result.ReportIncomplete == nil) && importedConfig.ReportIncomplete != nil {
		result.ReportIncomplete = importedConfig.ReportIncomplete
	}
	// ThreatDetection is also auto-defaulted by extractSafeOutputsConfig; apply the same
	// pattern — the import's explicit threat-detection key takes precedence over the result's
	// auto-default empty struct (which is not user-authored). If the main workflow explicitly
	// defined threat-detection, MergeSafeOutputs will have already removed it from the import
	// config (via topDefinedTypes), so config["threat-detection"] won't exist in that case.
	if _, hasTD := config["threat-detection"]; hasTD && importedConfig.ThreatDetection != nil {
		result.ThreatDetection = importedConfig.ThreatDetection
	}

	// Merge meta-configuration fields (only set if empty/zero in result)
	if !result.Steer && importedConfig.Steer {
		result.Steer = true
	}
	if len(result.AllowedDomains) == 0 && len(importedConfig.AllowedDomains) > 0 {
		result.AllowedDomains = importedConfig.AllowedDomains
	}
	if result.URLs == "" && importedConfig.URLs != "" {
		result.URLs = importedConfig.URLs
	}
	if result.Staged == nil && importedConfig.Staged != nil {
		result.Staged = importedConfig.Staged
	}
	if len(result.Env) == 0 && len(importedConfig.Env) > 0 {
		result.Env = importedConfig.Env
	}
	if result.GitHubToken == "" && importedConfig.GitHubToken != "" {
		result.GitHubToken = importedConfig.GitHubToken
	}
	if result.LinearToken == "" && importedConfig.LinearToken != "" {
		result.LinearToken = importedConfig.LinearToken
	}
	if result.GitHubApp == nil && importedConfig.GitHubApp != nil {
		result.GitHubApp = importedConfig.GitHubApp
	}
	if result.MaximumPatchSize == 0 && importedConfig.MaximumPatchSize > 0 {
		result.MaximumPatchSize = importedConfig.MaximumPatchSize
	}
	if isEmptyRunsOnValue(result.RunsOn) && !isEmptyRunsOnValue(importedConfig.RunsOn) {
		result.RunsOn = importedConfig.RunsOn
	}
	if len(importedConfig.Needs) > 0 {
		result.Needs = sliceutil.MergeUnique(result.Needs, importedConfig.Needs...)
	}

	// Merge Messages configuration at field level (main workflow entries override imported entries)
	if importedConfig.Messages != nil {
		if result.Messages == nil {
			// If main has no messages, use imported messages entirely
			result.Messages = importedConfig.Messages
		} else {
			// Merge individual message fields, main takes precedence
			result.Messages = mergeMessagesConfig(result.Messages, importedConfig.Messages)
		}
	}

	// Merge additional meta-configuration fields
	if result.Footer == nil && importedConfig.Footer != nil {
		result.Footer = importedConfig.Footer
	}
	if len(result.AllowGitHubReferences) == 0 && len(importedConfig.AllowGitHubReferences) > 0 {
		result.AllowGitHubReferences = importedConfig.AllowGitHubReferences
	}
	if !result.GroupReports && importedConfig.GroupReports {
		result.GroupReports = true
	}
	if result.ReportFailureAsIssue == nil && importedConfig.ReportFailureAsIssue != nil {
		result.ReportFailureAsIssue = importedConfig.ReportFailureAsIssue
		result.ReportFailureAsIssueCategories = importedConfig.ReportFailureAsIssueCategories
		result.ReportFailureAsIssueExcludedCategories = importedConfig.ReportFailureAsIssueExcludedCategories
	}
	if result.FailureIssueRepo == "" && importedConfig.FailureIssueRepo != "" {
		result.FailureIssueRepo = importedConfig.FailureIssueRepo
	}
	if result.MaxBotMentions == nil && importedConfig.MaxBotMentions != nil {
		result.MaxBotMentions = importedConfig.MaxBotMentions
	}
	if result.Mentions == nil && importedConfig.Mentions != nil {
		result.Mentions = importedConfig.Mentions
	}

	// Merge steps: concatenate imported steps after main workflow's steps
	if len(importedConfig.Steps) > 0 {
		result.Steps = append(result.Steps, importedConfig.Steps...)
	}

	// NOTE: Jobs are NOT merged here. They are handled separately in compiler_orchestrator.go
	// via mergeSafeJobsFromIncludedConfigs and extractSafeJobsFromFrontmatter.
	// The Jobs field is managed independently from other safe-output types to support
	// complex merge scenarios and conflict detection across multiple imports.

	importsLog.Print("Safe-output config merge completed")
	return result, nil
}

// mergeMessagesConfig merges two SafeOutputMessagesConfig structs at the field level.
// The result config (from main workflow) takes precedence - only empty fields are filled from imported.
func mergeMessagesConfig(result, imported *SafeOutputMessagesConfig) *SafeOutputMessagesConfig {
	if result.Footer == "" && imported.Footer != "" {
		result.Footer = imported.Footer
	}
	if result.FooterInstall == "" && imported.FooterInstall != "" {
		result.FooterInstall = imported.FooterInstall
	}
	if result.FooterWorkflowRecompile == "" && imported.FooterWorkflowRecompile != "" {
		result.FooterWorkflowRecompile = imported.FooterWorkflowRecompile
	}
	if result.FooterWorkflowRecompileComment == "" && imported.FooterWorkflowRecompileComment != "" {
		result.FooterWorkflowRecompileComment = imported.FooterWorkflowRecompileComment
	}
	if result.StagedTitle == "" && imported.StagedTitle != "" {
		result.StagedTitle = imported.StagedTitle
	}
	if result.StagedDescription == "" && imported.StagedDescription != "" {
		result.StagedDescription = imported.StagedDescription
	}
	if result.RunStarted == "" && imported.RunStarted != "" {
		result.RunStarted = imported.RunStarted
	}
	if result.RunSuccess == "" && imported.RunSuccess != "" {
		result.RunSuccess = imported.RunSuccess
	}
	if result.RunFailure == "" && imported.RunFailure != "" {
		result.RunFailure = imported.RunFailure
	}
	if result.DetectionFailure == "" && imported.DetectionFailure != "" {
		result.DetectionFailure = imported.DetectionFailure
	}
	if result.AgentFailureIssue == "" && imported.AgentFailureIssue != "" {
		result.AgentFailureIssue = imported.AgentFailureIssue
	}
	if result.AgentFailureComment == "" && imported.AgentFailureComment != "" {
		result.AgentFailureComment = imported.AgentFailureComment
	}
	if !result.AppendOnlyComments && imported.AppendOnlyComments {
		result.AppendOnlyComments = imported.AppendOnlyComments
	}
	if result.ActivationComments == "" && imported.ActivationComments != "" {
		result.ActivationComments = imported.ActivationComments
	}
	if result.PullRequestCreated == "" && imported.PullRequestCreated != "" {
		result.PullRequestCreated = imported.PullRequestCreated
	}
	if result.IssueCreated == "" && imported.IssueCreated != "" {
		result.IssueCreated = imported.IssueCreated
	}
	if result.CommitPushed == "" && imported.CommitPushed != "" {
		result.CommitPushed = imported.CommitPushed
	}
	return result
}

// MergeFeatures merges features configurations from imports with top-level features
// Features from top-level take precedence over imported features
func (c *Compiler) MergeFeatures(topFeatures map[string]any, importedFeatures []map[string]any) (map[string]any, error) {
	importsLog.Print("Merging features from imports")

	// If no imported features, return top-level features as-is
	if len(importedFeatures) == 0 {
		importsLog.Print("No imported features to merge")
		return topFeatures, nil
	}

	// Start with top-level features or create a new map
	result := make(map[string]any)
	if topFeatures != nil {
		maps.Copy(result, topFeatures)
		importsLog.Printf("Starting with %d top-level features", len(topFeatures))
	}

	// Process each imported features map
	importsLog.Printf("Processing %d imported feature maps", len(importedFeatures))

	for _, importedFeaturesMap := range importedFeatures {
		// Merge features - top-level features take precedence over imported ones
		for featureName, featureValue := range importedFeaturesMap {
			// Only add feature if it's not already defined in top-level
			if _, exists := result[featureName]; !exists {
				importsLog.Printf("Merging feature from import: %s", featureName)
				result[featureName] = featureValue
			} else {
				importsLog.Printf("Skipping imported feature (top-level takes precedence): %s", featureName)
			}
		}
	}

	importsLog.Printf("Successfully merged features: total=%d", len(result))
	return result, nil
}

// mergeEnv merges env var configurations from imports with top-level env vars.
// Top-level env vars take precedence over imported env vars.
// Conflicts between imports (same key in two different imported files) are detected
// earlier in the importAccumulator and fail compilation before mergeEnv is called.
func mergeEnv(topEnv map[string]any, importedEnvJSON string) (map[string]any, error) {
	importsLog.Printf("Merging env: topEnv=%d", len(topEnv))
	result := make(map[string]any)

	// Merge imported env vars first (newline-separated JSON objects, each from a distinct import)
	if importedEnvJSON != "" {
		lines := strings.SplitSeq(strings.TrimSpace(importedEnvJSON), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || line == "{}" {
				continue
			}

			var importedEnv map[string]any
			if err := json.Unmarshal([]byte(line), &importedEnv); err != nil {
				return nil, fmt.Errorf("failed to parse imported env JSON: %w", err)
			}

			maps.Copy(result, importedEnv)
		}
	}

	// Top-level env vars take precedence: copy last so they override any imported values
	maps.Copy(result, topEnv)
	if err := normalizeWorkflowEnvReferences(result); err != nil {
		return nil, err
	}

	importsLog.Printf("Merged %d total env vars", len(result))
	return result, nil
}

func normalizeWorkflowEnvReferences(env map[string]any) error {
	if len(env) == 0 {
		return nil
	}

	resolved := make(map[string]string, len(env))
	resolving := make(map[string]struct{}, len(env))
	for key, raw := range env {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		normalized, err := resolveWorkflowEnvValue(key, value, env, resolved, resolving)
		if err != nil {
			return err
		}
		env[key] = normalized
	}
	return nil
}

func resolveWorkflowEnvValue(key, value string, env map[string]any, resolved map[string]string, resolving map[string]struct{}) (string, error) {
	if normalized, ok := resolved[key]; ok {
		return normalized, nil
	}
	if _, ok := resolving[key]; ok {
		return "", fmt.Errorf("workflow env %q references itself recursively", key)
	}

	resolving[key] = struct{}{}
	defer delete(resolving, key)

	normalized, err := inlineWorkflowEnvReferences(value, env, resolved, resolving)
	if err != nil {
		return "", err
	}
	resolved[key] = normalized
	return normalized, nil
}

func inlineWorkflowEnvReferences(value string, env map[string]any, resolved map[string]string, resolving map[string]struct{}) (string, error) {
	inner, ok := extractWrappedGitHubExpression(value)
	if !ok {
		return value, nil
	}

	var builder strings.Builder
	for i := 0; i < len(inner); {
		if inner[i] == '\'' {
			next := consumeSingleQuotedGitHubExpressionString(inner, i)
			builder.WriteString(inner[i:next])
			i = next
			continue
		}

		if !isGitHubExpressionIdentifierStart(inner, i) {
			builder.WriteByte(inner[i])
			i++
			continue
		}

		start := i
		for i < len(inner) && isGitHubExpressionIdentifierChar(inner[i]) {
			i++
		}
		if inner[start:i] != "env" || i >= len(inner) || inner[i] != '.' || i+1 >= len(inner) || !isGitHubExpressionIdentifierStart(inner, i+1) {
			builder.WriteString(inner[start:i])
			continue
		}

		refStart := i + 1
		refEnd := refStart + 1
		for refEnd < len(inner) && isGitHubExpressionIdentifierChar(inner[refEnd]) {
			refEnd++
		}

		replacement, replaced, err := renderWorkflowEnvReferenceValue(inner[refStart:refEnd], env, resolved, resolving)
		if err != nil {
			return "", err
		}
		if replaced {
			builder.WriteString(replacement)
		} else {
			builder.WriteString(inner[start:refEnd])
		}
		i = refEnd
	}

	return wrapGitHubExpression(builder.String()), nil
}

func renderWorkflowEnvReferenceValue(name string, env map[string]any, resolved map[string]string, resolving map[string]struct{}) (string, bool, error) {
	raw, ok := env[name]
	if !ok {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", false, nil
	}

	normalized, err := resolveWorkflowEnvValue(name, value, env, resolved, resolving)
	if err != nil {
		return "", false, err
	}
	if inner, ok := extractWrappedGitHubExpression(normalized); ok {
		return "(" + inner + ")", true, nil
	}
	return BuildStringLiteral(normalized).Render(), true, nil
}
