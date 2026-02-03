package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
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
	for k, v := range topMCPServers {
		result[k] = v
	}

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
	domainSet := make(map[string]bool)
	for _, domain := range result.Allowed {
		domainSet[domain] = true
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
			if !domainSet[domain] {
				result.Allowed = append(result.Allowed, domain)
				domainSet[domain] = true
			}
		}
	}

	// Sort the final domain list for consistent output
	SortStrings(result.Allowed)

	importsLog.Printf("Successfully merged network permissions with %d allowed domains", len(result.Allowed))
	return result, nil
}

// ValidateIncludedPermissions validates that the main workflow permissions satisfy the imported workflow requirements
// This function is specifically used when merging included/imported workflow files to ensure the main workflow
// has sufficient permissions to support the requirements from all imported files.
// Takes the top-level permissions YAML string and imported permissions JSON string
// Returns an error if the main workflow permissions are insufficient
//
// Use ValidatePermissions (in permissions_validator.go) for general permission validation against GitHub MCP toolsets.
// Use ValidateIncludedPermissions (this function) when validating permissions from included/imported workflow files.
func (c *Compiler) ValidateIncludedPermissions(topPermissionsYAML string, importedPermissionsJSON string) error {
	importsLog.Print("Validating permissions from imports")

	// If no imported permissions, no validation needed
	if importedPermissionsJSON == "" || importedPermissionsJSON == "{}" {
		importsLog.Print("No imported permissions to validate")
		return nil
	}

	// Parse top-level permissions
	var topPerms *Permissions
	if topPermissionsYAML != "" {
		topPerms = NewPermissionsParser(topPermissionsYAML).ToPermissions()
	} else {
		topPerms = NewPermissions()
	}

	// Track missing permissions
	missingPermissions := make(map[PermissionScope]PermissionLevel)
	insufficientPermissions := make(map[PermissionScope]struct {
		required PermissionLevel
		current  PermissionLevel
	})

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(importedPermissionsJSON, "\n")
	importsLog.Printf("Processing %d permission definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to permissions map
		var importedPermsMap map[string]any
		if err := json.Unmarshal([]byte(line), &importedPermsMap); err != nil {
			importsLog.Printf("Skipping malformed permission entry: %q (error: %v)", line, err)
			continue
		}

		// Check each permission from the imported map
		for scopeStr, levelValue := range importedPermsMap {
			scope := PermissionScope(scopeStr)

			// Parse the level - it might be a string or already unmarshaled
			var requiredLevel PermissionLevel
			if levelStr, ok := levelValue.(string); ok {
				requiredLevel = PermissionLevel(levelStr)
			} else {
				// Skip invalid level values
				continue
			}

			// Get current level for this scope
			currentLevel, exists := topPerms.Get(scope)

			// Validate that the main workflow has sufficient permissions
			if !exists || currentLevel == PermissionNone {
				// Permission is missing entirely
				missingPermissions[scope] = requiredLevel
				importsLog.Printf("Missing permission: %s: %s", scope, requiredLevel)
			} else if !isPermissionSufficient(currentLevel, requiredLevel) {
				// Permission exists but is insufficient
				insufficientPermissions[scope] = struct {
					required PermissionLevel
					current  PermissionLevel
				}{requiredLevel, currentLevel}
				importsLog.Printf("Insufficient permission: %s: has %s, needs %s", scope, currentLevel, requiredLevel)
			}
		}
	}

	// If there are missing or insufficient permissions, return an error
	if len(missingPermissions) > 0 || len(insufficientPermissions) > 0 {
		var errorMsg strings.Builder
		errorMsg.WriteString("ERROR: Imported workflows require permissions that are not granted in the main workflow.\n\n")
		errorMsg.WriteString("The permission set must be explicitly declared in the main workflow.\n\n")

		if len(missingPermissions) > 0 {
			errorMsg.WriteString("Missing permissions:\n")
			// Sort for consistent output
			var scopes []PermissionScope
			for scope := range missingPermissions {
				scopes = append(scopes, scope)
			}
			SortPermissionScopes(scopes)
			for _, scope := range scopes {
				level := missingPermissions[scope]
				fmt.Fprintf(&errorMsg, "  - %s: %s\n", scope, level)
			}
			errorMsg.WriteString("\n")
		}

		if len(insufficientPermissions) > 0 {
			errorMsg.WriteString("Insufficient permissions:\n")
			// Sort for consistent output
			var scopes []PermissionScope
			for scope := range insufficientPermissions {
				scopes = append(scopes, scope)
			}
			SortPermissionScopes(scopes)
			for _, scope := range scopes {
				info := insufficientPermissions[scope]
				fmt.Fprintf(&errorMsg, "  - %s: has %s, requires %s\n", scope, info.current, info.required)
			}
			errorMsg.WriteString("\n")
		}

		errorMsg.WriteString("Suggested fix: Add the required permissions to your main workflow frontmatter:\n")
		errorMsg.WriteString("permissions:\n")

		// Combine all required permissions for the suggestion
		allRequired := make(map[PermissionScope]PermissionLevel)
		for scope, level := range missingPermissions {
			allRequired[scope] = level
		}
		for scope, info := range insufficientPermissions {
			allRequired[scope] = info.required
		}

		var scopes []PermissionScope
		for scope := range allRequired {
			scopes = append(scopes, scope)
		}
		SortPermissionScopes(scopes)
		for _, scope := range scopes {
			level := allRequired[scope]
			fmt.Fprintf(&errorMsg, "  %s: %s\n", scope, level)
		}

		return fmt.Errorf("%s", errorMsg.String())
	}

	importsLog.Print("All imported permissions are satisfied by main workflow")
	return nil
}

// isPermissionSufficient checks if the current permission level is sufficient for the required level
// write > read > none
func isPermissionSufficient(current, required PermissionLevel) bool {
	if current == required {
		return true
	}
	// write satisfies read requirement
	if current == PermissionWrite && required == PermissionRead {
		return true
	}
	return false
}

// getSafeOutputTypeKeys returns the list of safe output type keys from the embedded schema.
// This is a cached wrapper around parser.GetSafeOutputTypeKeys() to avoid parsing on every call.
var (
	safeOutputTypeKeys     []string
	safeOutputTypeKeysOnce sync.Once
	safeOutputTypeKeysErr  error
)

func getSafeOutputTypeKeys() ([]string, error) {
	safeOutputTypeKeysOnce.Do(func() {
		safeOutputTypeKeys, safeOutputTypeKeysErr = parser.GetSafeOutputTypeKeys()
	})
	return safeOutputTypeKeys, safeOutputTypeKeysErr
}

// MergeSafeOutputs merges safe-outputs configurations from imports into the top-level safe-outputs
// Returns an error if a conflict is detected (same safe-output type defined in both main and imported)
func (c *Compiler) MergeSafeOutputs(topSafeOutputs *SafeOutputsConfig, importedSafeOutputsJSON []string) (*SafeOutputsConfig, error) {
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

	// Collect all safe output types defined in the top-level config
	topDefinedTypes := make(map[string]bool)
	if topSafeOutputs != nil {
		for _, key := range typeKeys {
			if hasSafeOutputType(topSafeOutputs, key) {
				topDefinedTypes[key] = true
			}
		}
	}
	importsLog.Printf("Top-level safe-outputs defines %d types", len(topDefinedTypes))

	// Track types defined in imported configs for conflict detection
	importedDefinedTypes := make(map[string]bool)

	// Collect all imported configs. This includes configs with only meta fields (like allowed-domains,
	// staged, env, github-token, max-patch-size, runs-on) as well as those defining safe output types.
	// Meta fields can be imported even when no safe output types are defined.
	var importedConfigs []map[string]any
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
		// Main workflow definitions take precedence over imports (override behavior)
		for _, key := range typeKeys {
			if _, exists := config[key]; exists {
				if topDefinedTypes[key] {
					// Main workflow overrides imported definition - remove from imported config
					importsLog.Printf("Main workflow overrides imported safe-output: %s", key)
					delete(config, key)
					continue
				}
				if importedDefinedTypes[key] {
					return nil, fmt.Errorf("safe-outputs conflict: '%s' is defined in multiple imported workflows. Each safe-output type can only be defined once", key)
				}
				importedDefinedTypes[key] = true
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

	importsLog.Printf("Successfully merged safe-outputs from imports")
	return result, nil
}

// hasSafeOutputType checks if a SafeOutputsConfig has a specific safe output type defined
func hasSafeOutputType(config *SafeOutputsConfig, key string) bool {
	if config == nil {
		return false
	}

	switch key {
	case "create-issue":
		return config.CreateIssues != nil
	case "create-discussion":
		return config.CreateDiscussions != nil
	case "close-discussion":
		return config.CloseDiscussions != nil
	case "close-issue":
		return config.CloseIssues != nil
	case "close-pull-request":
		return config.ClosePullRequests != nil
	case "add-comment":
		return config.AddComments != nil
	case "create-pull-request":
		return config.CreatePullRequests != nil
	case "create-pull-request-review-comment":
		return config.CreatePullRequestReviewComments != nil
	case "create-code-scanning-alert":
		return config.CreateCodeScanningAlerts != nil
	case "add-labels":
		return config.AddLabels != nil
	case "remove-labels":
		return config.RemoveLabels != nil
	case "add-reviewer":
		return config.AddReviewer != nil
	case "assign-milestone":
		return config.AssignMilestone != nil
	case "assign-to-agent":
		return config.AssignToAgent != nil
	case "update-issue":
		return config.UpdateIssues != nil
	case "update-pull-request":
		return config.UpdatePullRequests != nil
	case "push-to-pull-request-branch":
		return config.PushToPullRequestBranch != nil
	case "upload-asset":
		return config.UploadAssets != nil
	case "update-release":
		return config.UpdateRelease != nil
	case "create-agent-session":
		return config.CreateAgentSessions != nil
	case "create-agent-task": // Backward compatibility
		return config.CreateAgentSessions != nil
	case "update-project":
		return config.UpdateProjects != nil
	case "missing-tool":
		return config.MissingTool != nil
	case "noop":
		return config.NoOp != nil
	case "threat-detection":
		return config.ThreatDetection != nil
	default:
		return false
	}
}

// mergeSafeOutputConfig merges a single imported config map into the result SafeOutputsConfig
func mergeSafeOutputConfig(result *SafeOutputsConfig, config map[string]any, c *Compiler) (*SafeOutputsConfig, error) {
	// Create a frontmatter-like structure for extractSafeOutputsConfig
	frontmatter := map[string]any{
		"safe-outputs": config,
	}

	// Use the existing extraction logic to parse the config
	importedConfig := c.extractSafeOutputsConfig(frontmatter)
	if importedConfig == nil {
		return result, nil
	}

	// Merge each safe output type (only set if nil in result)
	if result.CreateIssues == nil && importedConfig.CreateIssues != nil {
		result.CreateIssues = importedConfig.CreateIssues
	}
	if result.CreateDiscussions == nil && importedConfig.CreateDiscussions != nil {
		result.CreateDiscussions = importedConfig.CreateDiscussions
	}
	if result.UpdateDiscussions == nil && importedConfig.UpdateDiscussions != nil {
		result.UpdateDiscussions = importedConfig.UpdateDiscussions
	}
	if result.CloseDiscussions == nil && importedConfig.CloseDiscussions != nil {
		result.CloseDiscussions = importedConfig.CloseDiscussions
	}
	if result.CloseIssues == nil && importedConfig.CloseIssues != nil {
		result.CloseIssues = importedConfig.CloseIssues
	}
	if result.ClosePullRequests == nil && importedConfig.ClosePullRequests != nil {
		result.ClosePullRequests = importedConfig.ClosePullRequests
	}
	if result.MarkPullRequestAsReadyForReview == nil && importedConfig.MarkPullRequestAsReadyForReview != nil {
		result.MarkPullRequestAsReadyForReview = importedConfig.MarkPullRequestAsReadyForReview
	}
	if result.AddComments == nil && importedConfig.AddComments != nil {
		result.AddComments = importedConfig.AddComments
	}
	if result.CreatePullRequests == nil && importedConfig.CreatePullRequests != nil {
		result.CreatePullRequests = importedConfig.CreatePullRequests
	}
	if result.CreatePullRequestReviewComments == nil && importedConfig.CreatePullRequestReviewComments != nil {
		result.CreatePullRequestReviewComments = importedConfig.CreatePullRequestReviewComments
	}
	if result.CreateCodeScanningAlerts == nil && importedConfig.CreateCodeScanningAlerts != nil {
		result.CreateCodeScanningAlerts = importedConfig.CreateCodeScanningAlerts
	}
	if result.AutofixCodeScanningAlert == nil && importedConfig.AutofixCodeScanningAlert != nil {
		result.AutofixCodeScanningAlert = importedConfig.AutofixCodeScanningAlert
	}
	if result.AddLabels == nil && importedConfig.AddLabels != nil {
		result.AddLabels = importedConfig.AddLabels
	}
	if result.RemoveLabels == nil && importedConfig.RemoveLabels != nil {
		result.RemoveLabels = importedConfig.RemoveLabels
	}
	if result.AddReviewer == nil && importedConfig.AddReviewer != nil {
		result.AddReviewer = importedConfig.AddReviewer
	}
	if result.AssignMilestone == nil && importedConfig.AssignMilestone != nil {
		result.AssignMilestone = importedConfig.AssignMilestone
	}
	if result.AssignToAgent == nil && importedConfig.AssignToAgent != nil {
		result.AssignToAgent = importedConfig.AssignToAgent
	}
	if result.AssignToUser == nil && importedConfig.AssignToUser != nil {
		result.AssignToUser = importedConfig.AssignToUser
	}
	if result.UpdateIssues == nil && importedConfig.UpdateIssues != nil {
		result.UpdateIssues = importedConfig.UpdateIssues
	}
	if result.UpdatePullRequests == nil && importedConfig.UpdatePullRequests != nil {
		result.UpdatePullRequests = importedConfig.UpdatePullRequests
	}
	if result.PushToPullRequestBranch == nil && importedConfig.PushToPullRequestBranch != nil {
		result.PushToPullRequestBranch = importedConfig.PushToPullRequestBranch
	}
	if result.UploadAssets == nil && importedConfig.UploadAssets != nil {
		result.UploadAssets = importedConfig.UploadAssets
	}
	if result.UpdateRelease == nil && importedConfig.UpdateRelease != nil {
		result.UpdateRelease = importedConfig.UpdateRelease
	}
	if result.CreateAgentSessions == nil && importedConfig.CreateAgentSessions != nil {
		result.CreateAgentSessions = importedConfig.CreateAgentSessions
	}
	if result.UpdateProjects == nil && importedConfig.UpdateProjects != nil {
		result.UpdateProjects = importedConfig.UpdateProjects
	}
	if result.CreateProjects == nil && importedConfig.CreateProjects != nil {
		result.CreateProjects = importedConfig.CreateProjects
	}
	if result.CreateProjectStatusUpdates == nil && importedConfig.CreateProjectStatusUpdates != nil {
		result.CreateProjectStatusUpdates = importedConfig.CreateProjectStatusUpdates
	}
	if result.LinkSubIssue == nil && importedConfig.LinkSubIssue != nil {
		result.LinkSubIssue = importedConfig.LinkSubIssue
	}
	if result.HideComment == nil && importedConfig.HideComment != nil {
		result.HideComment = importedConfig.HideComment
	}
	if result.DispatchWorkflow == nil && importedConfig.DispatchWorkflow != nil {
		result.DispatchWorkflow = importedConfig.DispatchWorkflow
	}
	if result.MissingTool == nil && importedConfig.MissingTool != nil {
		result.MissingTool = importedConfig.MissingTool
	}
	if result.MissingData == nil && importedConfig.MissingData != nil {
		result.MissingData = importedConfig.MissingData
	}
	if result.NoOp == nil && importedConfig.NoOp != nil {
		result.NoOp = importedConfig.NoOp
	}
	if result.ThreatDetection == nil && importedConfig.ThreatDetection != nil {
		result.ThreatDetection = importedConfig.ThreatDetection
	}

	// Merge meta-configuration fields (only set if empty/zero in result)
	if len(result.AllowedDomains) == 0 && len(importedConfig.AllowedDomains) > 0 {
		result.AllowedDomains = importedConfig.AllowedDomains
	}
	if !result.Staged && importedConfig.Staged {
		result.Staged = importedConfig.Staged
	}
	if len(result.Env) == 0 && len(importedConfig.Env) > 0 {
		result.Env = importedConfig.Env
	}
	if result.GitHubToken == "" && importedConfig.GitHubToken != "" {
		result.GitHubToken = importedConfig.GitHubToken
	}
	if result.MaximumPatchSize == 0 && importedConfig.MaximumPatchSize > 0 {
		result.MaximumPatchSize = importedConfig.MaximumPatchSize
	}
	if result.RunsOn == "" && importedConfig.RunsOn != "" {
		result.RunsOn = importedConfig.RunsOn
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

	// NOTE: Jobs are NOT merged here. They are handled separately in compiler_orchestrator.go
	// via mergeSafeJobsFromIncludedConfigs and extractSafeJobsFromFrontmatter.
	// The Jobs field is managed independently from other safe-output types to support
	// complex merge scenarios and conflict detection across multiple imports.

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
	return result
}
