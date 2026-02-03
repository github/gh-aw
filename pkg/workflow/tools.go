package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var toolsLog = logger.New("workflow:tools")

// applyDefaults applies default values for missing workflow sections
func (c *Compiler) applyDefaults(data *WorkflowData, markdownPath string) error {
	toolsLog.Printf("Applying defaults to workflow: name=%s, path=%s", data.Name, markdownPath)

	// Check if this is a command trigger workflow (by checking if user specified "on.command")
	isCommandTrigger := false
	if data.On == "" {
		// Check the original frontmatter for command trigger
		content, err := os.ReadFile(markdownPath)
		if err == nil {
			result, err := parser.ExtractFrontmatterFromContent(string(content))
			if err == nil {
				if onValue, exists := result.Frontmatter["on"]; exists {
					// Check for slash_command or command (deprecated)
					if onMap, ok := onValue.(map[string]any); ok {
						if _, hasSlashCommand := onMap["slash_command"]; hasSlashCommand {
							isCommandTrigger = true
						} else if _, hasCommand := onMap["command"]; hasCommand {
							isCommandTrigger = true
						}
					}
				}
			}
		}
	}

	if data.On == "" {
		if isCommandTrigger {
			toolsLog.Print("Workflow is command trigger, configuring command events")

			// Get the filtered command events based on CommandEvents field
			filteredEvents := FilterCommentEvents(data.CommandEvents)

			// Merge events for YAML generation (combines pull_request_comment and issue_comment into issue_comment)
			yamlEvents := MergeEventsForYAML(filteredEvents)

			// Build command events map from merged events
			commandEventsMap := make(map[string]any)
			for _, event := range yamlEvents {
				commandEventsMap[event.EventName] = map[string]any{
					"types": event.Types,
				}
			}

			// Check if there are other events to merge
			if len(data.CommandOtherEvents) > 0 {
				// Merge other events into command events
				for key, value := range data.CommandOtherEvents {
					commandEventsMap[key] = value
				}
			}

			// Convert merged events to YAML
			mergedEventsYAML, err := yaml.Marshal(map[string]any{"on": commandEventsMap})
			if err == nil {
				yamlStr := strings.TrimSuffix(string(mergedEventsYAML), "\n")
				// Post-process YAML to ensure cron expressions are quoted
				yamlStr = parser.QuoteCronExpressions(yamlStr)
				// Apply comment processing to filter fields (draft, forks, names)
				// Pass empty frontmatter since this is for command triggers
				yamlStr = c.commentOutProcessedFieldsInOnSection(yamlStr, map[string]any{})
				// Keep "on" quoted as it's a YAML boolean keyword
				data.On = yamlStr
			} else {
				// If conversion fails, build a basic YAML string manually
				var builder strings.Builder
				builder.WriteString(`"on":`)
				for _, event := range filteredEvents {
					builder.WriteString("\n  ")
					builder.WriteString(event.EventName)
					builder.WriteString(":\n    types: [")
					for i, t := range event.Types {
						if i > 0 {
							builder.WriteString(", ")
						}
						builder.WriteString(t)
					}
					builder.WriteString("]")
				}
				data.On = builder.String()
			}

			// Add conditional logic to check for command in issue content
			// Use event-aware condition that only applies command checks to comment-related events
			// Pass the filtered events to buildEventAwareCommandCondition
			hasOtherEvents := len(data.CommandOtherEvents) > 0
			commandConditionTree, err := buildEventAwareCommandCondition(data.Command, data.CommandEvents, hasOtherEvents)
			if err != nil {
				return fmt.Errorf("failed to build command condition: %w", err)
			}

			if data.If == "" {
				data.If = commandConditionTree.Render()
			}
		} else {
			data.On = `on:
  # Start either every 10 minutes, or when some kind of human event occurs.
  # Because of the implicit "concurrency" section, only one instance of this
  # workflow will run at a time.
  schedule:
    - cron: "0/10 * * * *"
  issues:
    types: [opened, edited, closed]
  issue_comment:
    types: [created, edited]
  pull_request:
    types: [opened, edited, closed]
  push:
    branches:
      - main
  workflow_dispatch:`
		}
	}

	// Check if this workflow has an issue trigger and we're in trial mode
	// If so, inject workflow_dispatch with issue_number input
	if c.trialMode && c.hasIssueTrigger(data.On) {
		data.On = c.injectWorkflowDispatchForIssue(data.On)
	}

	// Generate concurrency configuration using the dedicated concurrency module
	data.Concurrency = GenerateConcurrencyConfig(data, isCommandTrigger)

	if data.RunName == "" {
		data.RunName = fmt.Sprintf(`run-name: "%s"`, data.Name)
	}

	if data.TimeoutMinutes == "" {
		data.TimeoutMinutes = fmt.Sprintf("timeout-minutes: %d", int(constants.DefaultAgenticWorkflowTimeout/time.Minute))
	}

	if data.RunsOn == "" {
		data.RunsOn = "runs-on: ubuntu-latest"
	}
	// Apply default tools
	data.Tools = c.applyDefaultTools(data.Tools, data.SafeOutputs, data.SandboxConfig, data.NetworkPermissions)
	// Update ParsedTools to reflect changes made by applyDefaultTools
	data.ParsedTools = NewTools(data.Tools)

	// Check if permissions is explicitly empty ({}) - this means user wants no permissions
	// In this case, we should NOT apply default read-all
	if data.Permissions == "permissions: {}" {
		// Explicitly empty permissions - preserve the empty state
		// The agent job in dev mode will add contents: read if needed for local actions
		return nil
	}

	if data.Permissions == "" {
		// ============================================================================
		// PERMISSIONS DEFAULTS
		// ============================================================================
		// When no permissions are specified, set default to contents: read.
		// This provides minimal access needed for most workflows while following
		// the principle of least privilege.
		// ============================================================================
		perms := NewPermissionsContentsRead()
		yaml := perms.RenderToYAML()
		// RenderToYAML uses job-friendly indentation (6 spaces). WorkflowData.Permissions
		// is stored in workflow-level indentation (2 spaces) and later re-indented for jobs.
		lines := strings.Split(yaml, "\n")
		for i := 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "      ") {
				lines[i] = "  " + lines[i][6:]
			}
		}
		data.Permissions = strings.Join(lines, "\n")
	}
	return nil
}

// mergeToolsAndMCPServers merges tools, mcp-servers, and included tools
func (c *Compiler) mergeToolsAndMCPServers(topTools, mcpServers map[string]any, includedTools string) (map[string]any, error) {
	toolsLog.Printf("Merging tools and MCP servers: topTools=%d, mcpServers=%d", len(topTools), len(mcpServers))

	// Start with top-level tools
	result := topTools
	if result == nil {
		result = make(map[string]any)
	}

	// Add MCP servers to the tools collection
	for serverName, serverConfig := range mcpServers {
		result[serverName] = serverConfig
	}

	// Merge included tools
	return c.MergeTools(result, includedTools)
}

// mergeRuntimes merges runtime configurations from frontmatter and imports
func mergeRuntimes(topRuntimes map[string]any, importedRuntimesJSON string) (map[string]any, error) {
	toolsLog.Printf("Merging runtimes: topRuntimes=%d", len(topRuntimes))
	result := make(map[string]any)

	// Start with top-level runtimes
	for id, config := range topRuntimes {
		result[id] = config
	}

	// Merge imported runtimes (newline-separated JSON objects)
	if importedRuntimesJSON != "" {
		lines := strings.Split(strings.TrimSpace(importedRuntimesJSON), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || line == "{}" {
				continue
			}

			var importedRuntimes map[string]any
			if err := json.Unmarshal([]byte(line), &importedRuntimes); err != nil {
				return nil, fmt.Errorf("failed to parse imported runtimes JSON: %w", err)
			}

			// Merge imported runtimes - later imports override earlier ones
			for id, config := range importedRuntimes {
				result[id] = config
			}
		}
	}

	toolsLog.Printf("Merged %d total runtimes", len(result))
	return result, nil
}

// hasIssueTrigger checks if the workflow has an issue trigger in its 'on' section
func (c *Compiler) hasIssueTrigger(onSection string) bool {
	hasIssue := strings.Contains(onSection, "issues:") ||
		strings.Contains(onSection, "issue:") ||
		strings.Contains(onSection, "issue_comment:")
	toolsLog.Printf("Checking for issue trigger: has_issue=%t", hasIssue)
	return hasIssue
}

// injectWorkflowDispatchForIssue adds workflow_dispatch trigger with issue_number input
func (c *Compiler) injectWorkflowDispatchForIssue(onSection string) string {
	toolsLog.Print("Injecting workflow_dispatch trigger for issue workflows")
	// Parse the existing on section to understand its structure
	var onData map[string]any
	if err := yaml.Unmarshal([]byte(onSection), &onData); err != nil {
		// If parsing fails, append workflow_dispatch manually
		return onSection + "\n  workflow_dispatch:\n    inputs:\n      issue_number:\n        description: 'Issue number for trial mode'\n        required: true\n        type: string"
	}

	// Get the 'on' section
	if onMap, exists := onData["on"]; exists {
		if triggers, ok := onMap.(map[string]any); ok {
			// Add workflow_dispatch with issue_number input
			triggers["workflow_dispatch"] = map[string]any{
				"inputs": map[string]any{
					"issue_number": map[string]any{
						"description": "Issue number for trial mode",
						"required":    true,
						"type":        "string",
					},
				},
			}

			// Convert back to YAML
			updatedOnData := map[string]any{"on": triggers}
			if yamlBytes, err := yaml.Marshal(updatedOnData); err == nil {
				yamlStr := string(yamlBytes)
				// Keep "on" quoted as it's a YAML boolean keyword
				return strings.TrimSuffix(yamlStr, "\n")
			}
		}
	}

	// Fallback: append workflow_dispatch manually
	return onSection + "\n  workflow_dispatch:\n    inputs:\n      issue_number:\n        description: 'Issue number for trial mode'\n        required: true\n        type: string"
}

// replaceIssueNumberReferences replaces github.event.issue.number with inputs.issue_number in YAML content
func (c *Compiler) replaceIssueNumberReferences(yamlContent string) string {
	// Replace all occurrences of github.event.issue.number with inputs.issue_number
	return strings.ReplaceAll(yamlContent, "github.event.issue.number", "inputs.issue_number")
}
