package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var compilerSafeOutputsLog = logger.New("workflow:compiler_safe_outputs")

// parseOnSection handles parsing of the "on" section from frontmatter, extracting command triggers,
// reactions, and stop-after configurations while detecting conflicts with other event types.
func (c *Compiler) parseOnSection(frontmatter map[string]any, workflowData *WorkflowData, markdownPath string) error {
	compilerSafeOutputsLog.Printf("Parsing on section: workflow=%s, markdownPath=%s", workflowData.Name, markdownPath)
	// Check if "slash_command" or "command" (deprecated) is used as a trigger in the "on" section
	// Also extract "reaction" from the "on" section
	var hasCommand bool
	var hasReaction bool
	var hasStopAfter bool
	var otherEvents map[string]any

	// Use cached On field from ParsedFrontmatter if available, otherwise fall back to map access
	var onValue any
	var exists bool
	if workflowData.ParsedFrontmatter != nil && workflowData.ParsedFrontmatter.On != nil {
		onValue = workflowData.ParsedFrontmatter.On
		exists = true
	} else {
		onValue, exists = frontmatter["on"]
	}

	if exists {
		// Check for new format: on.slash_command/on.command and on.reaction
		if onMap, ok := onValue.(map[string]any); ok {
			// Check for stop-after in the on section
			if _, hasStopAfterKey := onMap["stop-after"]; hasStopAfterKey {
				hasStopAfter = true
			}

			// Extract reaction from on section
			if reactionValue, hasReactionField := onMap["reaction"]; hasReactionField {
				hasReaction = true
				reactionStr, err := parseReactionValue(reactionValue)
				if err != nil {
					return err
				}
				// Validate reaction value
				if !isValidReaction(reactionStr) {
					return fmt.Errorf("invalid reaction value '%s': must be one of %v", reactionStr, getValidReactions())
				}
				// Set AIReaction even if it's "none" - "none" explicitly disables reactions
				workflowData.AIReaction = reactionStr
			}

			// Extract lock-for-agent from on.issues section
			if issuesValue, hasIssues := onMap["issues"]; hasIssues {
				if issuesMap, ok := issuesValue.(map[string]any); ok {
					if lockForAgent, hasLockForAgent := issuesMap["lock-for-agent"]; hasLockForAgent {
						if lockBool, ok := lockForAgent.(bool); ok {
							workflowData.LockForAgent = lockBool
							compilerSafeOutputsLog.Printf("lock-for-agent enabled for issues: %v", lockBool)
						}
					}
				}
			}

			// Extract lock-for-agent from on.issue_comment section
			if issueCommentValue, hasIssueComment := onMap["issue_comment"]; hasIssueComment {
				if issueCommentMap, ok := issueCommentValue.(map[string]any); ok {
					if lockForAgent, hasLockForAgent := issueCommentMap["lock-for-agent"]; hasLockForAgent {
						if lockBool, ok := lockForAgent.(bool); ok {
							workflowData.LockForAgent = lockBool
							compilerSafeOutputsLog.Printf("lock-for-agent enabled for issue_comment: %v", lockBool)
						}
					}
				}
			}

			// Check for slash_command (preferred) or command (deprecated)
			if _, hasSlashCommandKey := onMap["slash_command"]; hasSlashCommandKey {
				hasCommand = true
				// Set default command to filename if not specified in the command section
				if len(workflowData.Command) == 0 {
					baseName := strings.TrimSuffix(filepath.Base(markdownPath), ".md")
					workflowData.Command = []string{baseName}
				}
				// Check for conflicting events (but allow issues/pull_request with labeled/unlabeled types)
				conflictingEvents := []string{"issues", "issue_comment", "pull_request", "pull_request_review_comment"}
				for _, eventName := range conflictingEvents {
					if eventValue, hasConflict := onMap[eventName]; hasConflict {
						// Special case: allow issues/pull_request if they only have labeled/unlabeled types
						if (eventName == "issues" || eventName == "pull_request") && parser.IsLabelOnlyEvent(eventValue) {
							continue // Allow this - it doesn't conflict with command triggers
						}
						return fmt.Errorf("cannot use 'slash_command' with '%s' in the same workflow", eventName)
					}
				}

				// Clear the On field so applyDefaults will handle command trigger generation
				workflowData.On = ""
			} else if _, hasCommandKey := onMap["command"]; hasCommandKey {
				hasCommand = true
				// Set default command to filename if not specified in the command section
				if len(workflowData.Command) == 0 {
					baseName := strings.TrimSuffix(filepath.Base(markdownPath), ".md")
					workflowData.Command = []string{baseName}
				}
				// Check for conflicting events (but allow issues/pull_request with labeled/unlabeled types)
				conflictingEvents := []string{"issues", "issue_comment", "pull_request", "pull_request_review_comment"}
				for _, eventName := range conflictingEvents {
					if eventValue, hasConflict := onMap[eventName]; hasConflict {
						// Special case: allow issues/pull_request if they only have labeled/unlabeled types
						if (eventName == "issues" || eventName == "pull_request") && parser.IsLabelOnlyEvent(eventValue) {
							continue // Allow this - it doesn't conflict with command triggers
						}
						return fmt.Errorf("cannot use 'command' with '%s' in the same workflow", eventName)
					}
				}

				// Clear the On field so applyDefaults will handle command trigger generation
				workflowData.On = ""
			}
			// Extract other (non-conflicting) events excluding slash_command, command, reaction, and stop-after
			otherEvents = filterMapKeys(onMap, "slash_command", "command", "reaction", "stop-after")
		}
	}

	// Clear command field if no command trigger was found
	if !hasCommand {
		workflowData.Command = nil
	}

	// Auto-enable "eyes" reaction for command triggers if no explicit reaction was specified
	if hasCommand && !hasReaction && workflowData.AIReaction == "" {
		workflowData.AIReaction = "eyes"
	}

	// Store other events for merging in applyDefaults
	if hasCommand && len(otherEvents) > 0 {
		// We'll store this and handle it in applyDefaults
		workflowData.On = "" // This will trigger command handling in applyDefaults
		workflowData.CommandOtherEvents = otherEvents
	} else if (hasReaction || hasStopAfter) && len(otherEvents) > 0 {
		// Only re-marshal the "on" if we have to
		onEventsYAML, err := yaml.Marshal(map[string]any{"on": otherEvents})
		if err == nil {
			yamlStr := strings.TrimSuffix(string(onEventsYAML), "\n")
			// Post-process YAML to ensure cron expressions are quoted
			yamlStr = parser.QuoteCronExpressions(yamlStr)
			// Apply comment processing to filter fields (draft, forks, names)
			yamlStr = c.commentOutProcessedFieldsInOnSection(yamlStr, frontmatter)
			// Add zizmor ignore comment if workflow_run trigger is present
			yamlStr = c.addZizmorIgnoreForWorkflowRun(yamlStr)
			// Keep "on" quoted as it's a YAML boolean keyword
			workflowData.On = yamlStr
		} else {
			// Fallback to extracting the original on field (this will include reaction but shouldn't matter for compilation)
			workflowData.On = c.extractTopLevelYAMLSection(frontmatter, "on")
		}
	}

	return nil
}

// generateJobName converts a workflow name to a valid YAML job identifier
func (c *Compiler) generateJobName(workflowName string) string {
	// Convert to lowercase and replace spaces and special characters with hyphens
	jobName := strings.ToLower(workflowName)

	// Replace spaces and common punctuation with hyphens
	jobName = strings.ReplaceAll(jobName, " ", "-")
	jobName = strings.ReplaceAll(jobName, ":", "-")
	jobName = strings.ReplaceAll(jobName, ".", "-")
	jobName = strings.ReplaceAll(jobName, ",", "-")
	jobName = strings.ReplaceAll(jobName, "(", "-")
	jobName = strings.ReplaceAll(jobName, ")", "-")
	jobName = strings.ReplaceAll(jobName, "/", "-")
	jobName = strings.ReplaceAll(jobName, "\\", "-")
	jobName = strings.ReplaceAll(jobName, "@", "-")
	jobName = strings.ReplaceAll(jobName, "'", "")
	jobName = strings.ReplaceAll(jobName, "\"", "")

	// Remove multiple consecutive hyphens
	for strings.Contains(jobName, "--") {
		jobName = strings.ReplaceAll(jobName, "--", "-")
	}

	// Remove leading/trailing hyphens
	jobName = strings.Trim(jobName, "-")

	// Ensure it's not empty and starts with a letter or underscore
	if jobName == "" || (!strings.ContainsAny(string(jobName[0]), "abcdefghijklmnopqrstuvwxyz_")) {
		jobName = "workflow-" + jobName
	}

	return jobName
}

// mergeSafeJobsFromIncludes merges safe-jobs from included files and detects conflicts
func (c *Compiler) mergeSafeJobsFromIncludes(topSafeJobs map[string]*SafeJobConfig, includedContentJSON string) (map[string]*SafeJobConfig, error) {
	if includedContentJSON == "" || includedContentJSON == "{}" {
		return topSafeJobs, nil
	}

	// Parse the included content as frontmatter to extract safe-jobs
	var includedContent map[string]any
	if err := json.Unmarshal([]byte(includedContentJSON), &includedContent); err != nil {
		return topSafeJobs, nil // Return original safe-jobs if parsing fails
	}

	// Extract safe-jobs from the included content
	includedSafeJobs := extractSafeJobsFromFrontmatter(includedContent)

	// Merge with conflict detection
	mergedSafeJobs, err := mergeSafeJobs(topSafeJobs, includedSafeJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to merge safe-jobs: %w", err)
	}

	return mergedSafeJobs, nil
}

// mergeSafeJobsFromIncludedConfigs merges safe-jobs from included safe-outputs configurations
func (c *Compiler) mergeSafeJobsFromIncludedConfigs(topSafeJobs map[string]*SafeJobConfig, includedConfigs []string) (map[string]*SafeJobConfig, error) {
	compilerSafeOutputsLog.Printf("Merging safe-jobs from included configs: includedCount=%d", len(includedConfigs))
	result := topSafeJobs
	if result == nil {
		result = make(map[string]*SafeJobConfig)
	}

	for _, configJSON := range includedConfigs {
		if configJSON == "" || configJSON == "{}" {
			continue
		}

		// Parse the safe-outputs configuration
		var safeOutputsConfig map[string]any
		if err := json.Unmarshal([]byte(configJSON), &safeOutputsConfig); err != nil {
			continue // Skip invalid JSON
		}

		// Extract safe-jobs from the safe-outputs.jobs field
		includedSafeJobs := extractSafeJobsFromFrontmatter(map[string]any{
			"safe-outputs": safeOutputsConfig,
		})

		// Merge with conflict detection
		var err error
		result, err = mergeSafeJobs(result, includedSafeJobs)
		if err != nil {
			return nil, fmt.Errorf("failed to merge safe-jobs from includes: %w", err)
		}
	}

	return result, nil
}

// applyDefaultTools adds default read-only GitHub MCP tools, creating github tool if not present
func (c *Compiler) applyDefaultTools(tools map[string]any, safeOutputs *SafeOutputsConfig, sandboxConfig *SandboxConfig, networkPermissions *NetworkPermissions) map[string]any {
	compilerSafeOutputsLog.Printf("Applying default tools: existingToolCount=%d", len(tools))
	// Always apply default GitHub tools (create github section if it doesn't exist)

	if tools == nil {
		tools = make(map[string]any)
	}

	// Get existing github tool configuration
	githubTool := tools["github"]

	// Check if github is explicitly disabled (github: false)
	if githubTool == false {
		// Remove the github tool entirely when set to false
		delete(tools, "github")
	} else {
		// Process github tool configuration
		var githubConfig map[string]any

		if toolConfig, ok := githubTool.(map[string]any); ok {
			githubConfig = make(map[string]any)
			for k, v := range toolConfig {
				githubConfig[k] = v
			}
		} else {
			githubConfig = make(map[string]any)
		}

		// Parse the existing GitHub tool configuration for type safety
		parsedConfig := parseGitHubTool(githubTool)

		// Create a set of existing tools for efficient lookup
		existingToolsSet := make(map[string]bool)
		if parsedConfig != nil {
			for _, tool := range parsedConfig.Allowed {
				existingToolsSet[string(tool)] = true
			}
		}

		// Only set allowed tools if explicitly configured
		// Don't add default tools - let the MCP server use all available tools
		if len(existingToolsSet) > 0 {
			// Convert back to []any for the map
			existingAllowed := make([]any, 0, len(parsedConfig.Allowed))
			for _, tool := range parsedConfig.Allowed {
				existingAllowed = append(existingAllowed, string(tool))
			}
			githubConfig["allowed"] = existingAllowed
		}
		tools["github"] = githubConfig
	}

	// Enable edit and bash tools by default when sandbox is enabled
	// The sandbox is enabled when:
	// 1. Explicitly configured via sandbox.agent (awf/srt)
	// 2. Auto-enabled by firewall default enablement (when network restrictions are present)
	if isSandboxEnabled(sandboxConfig, networkPermissions) {
		compilerSafeOutputsLog.Print("Sandbox enabled, applying default edit and bash tools")

		// Add edit tool if not present
		if _, exists := tools["edit"]; !exists {
			tools["edit"] = true
			compilerSafeOutputsLog.Print("Added edit tool (sandbox enabled)")
		}

		// Add bash tool with wildcard if not present
		if _, exists := tools["bash"]; !exists {
			tools["bash"] = []any{"*"}
			compilerSafeOutputsLog.Print("Added bash tool with wildcard (sandbox enabled)")
		}
	}

	// Add Git commands and file editing tools when safe-outputs includes create-pull-request or push-to-pull-request-branch
	if safeOutputs != nil && needsGitCommands(safeOutputs) {

		// Add edit tool with null value
		if _, exists := tools["edit"]; !exists {
			tools["edit"] = nil
		}
		gitCommands := []any{
			"git checkout:*",
			"git branch:*",
			"git switch:*",
			"git add:*",
			"git rm:*",
			"git commit:*",
			"git merge:*",
			"git status",
		}

		// Add bash tool with Git commands if not already present
		if _, exists := tools["bash"]; !exists {
			// bash tool doesn't exist, add it with Git commands
			tools["bash"] = gitCommands
		} else {
			// bash tool exists, merge Git commands with existing commands
			existingBash := tools["bash"]
			if existingCommands, ok := existingBash.([]any); ok {
				// Convert existing commands to strings for comparison
				existingSet := make(map[string]bool)
				for _, cmd := range existingCommands {
					if cmdStr, ok := cmd.(string); ok {
						existingSet[cmdStr] = true
						// If we see :* or *, all bash commands are already allowed
						if cmdStr == ":*" || cmdStr == "*" {
							// Don't add specific Git commands since all are already allowed
							goto bashComplete
						}
					}
				}

				// Add Git commands that aren't already present
				newCommands := make([]any, len(existingCommands))
				copy(newCommands, existingCommands)
				for _, gitCmd := range gitCommands {
					if gitCmdStr, ok := gitCmd.(string); ok {
						if !existingSet[gitCmdStr] {
							newCommands = append(newCommands, gitCmd)
						}
					}
				}
				tools["bash"] = newCommands
			} else if existingBash == nil {
				_ = existingBash // Keep the nil value as-is
			}
		}
	bashComplete:
	}

	// Add default bash commands when bash is enabled but no specific commands are provided
	// This runs after git commands logic, so it only applies when git commands weren't added
	// Behavior:
	//   - bash: true → All commands allowed (converted to ["*"])
	//   - bash: false → Tool disabled (removed from tools)
	//   - bash: nil → Add default commands
	//   - bash: [] → No commands (empty array means no tools allowed)
	//   - bash: ["cmd1", "cmd2"] → Add default commands + specific commands
	if bashTool, exists := tools["bash"]; exists {
		// Check if bash was left as nil or true after git processing
		if bashTool == nil {
			// bash is nil - only add defaults if this wasn't processed by git commands
			// If git commands were needed, bash would have been set to git commands or left as nil intentionally
			if safeOutputs == nil || !needsGitCommands(safeOutputs) {
				defaultCommands := make([]any, len(constants.DefaultBashTools))
				for i, cmd := range constants.DefaultBashTools {
					defaultCommands[i] = cmd
				}
				tools["bash"] = defaultCommands
			}
		} else if bashTool == true {
			// bash is true - convert to wildcard (allow all commands)
			tools["bash"] = []any{"*"}
		} else if bashTool == false {
			// bash is false - disable the tool by removing it
			delete(tools, "bash")
		} else if bashArray, ok := bashTool.([]any); ok {
			// bash is an array - merge default commands with custom commands
			if len(bashArray) > 0 {
				// Create a set to track existing commands to avoid duplicates
				existingCommands := make(map[string]bool)
				for _, cmd := range bashArray {
					if cmdStr, ok := cmd.(string); ok {
						existingCommands[cmdStr] = true
					}
				}

				// Start with default commands (append handles capacity automatically)
				var mergedCommands []any
				for _, cmd := range constants.DefaultBashTools {
					if !existingCommands[cmd] {
						mergedCommands = append(mergedCommands, cmd)
					}
				}

				// Add the custom commands
				mergedCommands = append(mergedCommands, bashArray...)
				tools["bash"] = mergedCommands
			}
			// Note: bash with empty array (bash: []) means "no bash tools allowed" and is left as-is
		}
	}

	return tools
}

// needsGitCommands checks if safe outputs configuration requires Git commands
func needsGitCommands(safeOutputs *SafeOutputsConfig) bool {
	if safeOutputs == nil {
		return false
	}
	return safeOutputs.CreatePullRequests != nil || safeOutputs.PushToPullRequestBranch != nil
}

// isSandboxEnabled checks if the sandbox is enabled (either explicitly or auto-enabled)
// Returns true when:
// - sandbox.agent is explicitly set to a sandbox type (awf, srt, etc.)
// - Firewall is auto-enabled (networkPermissions.Firewall is set and enabled)
// - SRT sandbox is enabled
// Returns false when:
// - sandbox.agent is false (explicitly disabled)
// - No sandbox configuration and no auto-enabled firewall
func isSandboxEnabled(sandboxConfig *SandboxConfig, networkPermissions *NetworkPermissions) bool {
	// Check if sandbox.agent is explicitly disabled
	if sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		return false
	}

	// Check if sandbox.agent is explicitly configured with a type
	if sandboxConfig != nil && sandboxConfig.Agent != nil {
		agentType := getAgentType(sandboxConfig.Agent)
		if isSupportedSandboxType(agentType) {
			return true
		}
	}

	// Check if SRT is enabled via legacy Type field
	if sandboxConfig != nil && (sandboxConfig.Type == SandboxTypeSRT || sandboxConfig.Type == SandboxTypeRuntime) {
		return true
	}

	// Check if firewall is auto-enabled (AWF)
	if networkPermissions != nil && networkPermissions.Firewall != nil && networkPermissions.Firewall.Enabled {
		return true
	}

	return false
}
