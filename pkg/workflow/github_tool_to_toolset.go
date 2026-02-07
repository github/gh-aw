package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var githubToolToToolsetLog = logger.New("workflow:github_tool_to_toolset")

//go:embed data/github_tool_to_toolset.json
var githubToolToToolsetJSON []byte

// GitHubToolToToolsetMap maps individual GitHub MCP tools to their respective toolsets
// This mapping is loaded from an embedded JSON file based on the documentation
// in .github/instructions/github-mcp-server.instructions.md
var GitHubToolToToolsetMap map[string]string

func init() {
	// Load the mapping from embedded JSON
	if err := json.Unmarshal(githubToolToToolsetJSON, &GitHubToolToToolsetMap); err != nil {
		panic("failed to load GitHub tool to toolset mapping: " + err.Error())
	}
}

// ValidateGitHubToolsAgainstToolsets validates that all allowed GitHub tools have their
// corresponding toolsets enabled in the configuration
func ValidateGitHubToolsAgainstToolsets(allowedTools []string, enabledToolsets []string) error {
	githubToolToToolsetLog.Printf("Validating GitHub tools against toolsets: allowed_tools=%d, enabled_toolsets=%d", len(allowedTools), len(enabledToolsets))

	if len(allowedTools) == 0 {
		githubToolToToolsetLog.Print("No tools to validate, skipping")
		// No specific tools restricted, validation not needed
		return nil
	}

	// Create a set of enabled toolsets for fast lookup
	enabledSet := make(map[string]bool)
	for _, toolset := range enabledToolsets {
		enabledSet[toolset] = true
	}
	githubToolToToolsetLog.Printf("Enabled toolsets: %v", enabledToolsets)

	// Track missing toolsets and which tools need them
	missingToolsets := make(map[string][]string) // toolset -> list of tools that need it

	// Track unknown tools for suggestions
	var unknownTools []string
	var suggestions []string

	for _, tool := range allowedTools {
		// Skip wildcard - it means "allow all tools"
		if tool == "*" {
			continue
		}

		requiredToolset, exists := GitHubToolToToolsetMap[tool]
		if !exists {
			githubToolToToolsetLog.Printf("Tool %s not found in mapping, checking for typo", tool)

			// Get all valid tool names for suggestion
			validTools := make([]string, 0, len(GitHubToolToToolsetMap))
			for validTool := range GitHubToolToToolsetMap {
				validTools = append(validTools, validTool)
			}
			sort.Strings(validTools)

			// Try to find close matches
			matches := parser.FindClosestMatches(tool, validTools, 1)
			if len(matches) > 0 {
				githubToolToToolsetLog.Printf("Found suggestion for unknown tool %s: %s", tool, matches[0])
				unknownTools = append(unknownTools, tool)
				suggestions = append(suggestions, fmt.Sprintf("%s → %s", tool, matches[0]))
			} else {
				githubToolToToolsetLog.Printf("No suggestion found for unknown tool: %s", tool)
				unknownTools = append(unknownTools, tool)
			}
			// Tool not in our mapping - this could be a new tool or a typo
			// We'll skip validation for unknown tools to avoid false positives
			continue
		}

		if !enabledSet[requiredToolset] {
			githubToolToToolsetLog.Printf("Tool %s requires missing toolset: %s", tool, requiredToolset)
			missingToolsets[requiredToolset] = append(missingToolsets[requiredToolset], tool)
		}
	}

	// Report unknown tools with suggestions if any were found
	if len(unknownTools) > 0 {
		githubToolToToolsetLog.Printf("Found %d unknown tools", len(unknownTools))
		errMsg := fmt.Sprintf("Unknown GitHub tool(s): %s\n\n", formatList(unknownTools))

		if len(suggestions) > 0 {
			errMsg += "Did you mean:\n"
			for _, s := range suggestions {
				errMsg += fmt.Sprintf("  %s\n", s)
			}
			errMsg += "\n"
		}

		// Show a few examples of valid tools
		validTools := make([]string, 0, len(GitHubToolToToolsetMap))
		for tool := range GitHubToolToToolsetMap {
			validTools = append(validTools, tool)
		}
		sort.Strings(validTools)

		exampleCount := 10
		if len(validTools) < exampleCount {
			exampleCount = len(validTools)
		}
		errMsg += fmt.Sprintf("Valid GitHub tools include: %s\n\n", formatList(validTools[:exampleCount]))
		errMsg += "See all tools: https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_tool_to_toolset.json"

		return fmt.Errorf("%s", errMsg)
	}

	if len(missingToolsets) > 0 {
		githubToolToToolsetLog.Printf("Validation failed: missing %d toolsets", len(missingToolsets))
		return NewGitHubToolsetValidationError(missingToolsets)
	}

	githubToolToToolsetLog.Print("Validation successful: all tools have required toolsets")
	return nil
}

// formatList formats a list of strings as a comma-separated list
func formatList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	if len(items) == 2 {
		return items[0] + " and " + items[1]
	}
	return fmt.Sprintf("%s, and %s", formatList(items[:len(items)-1]), items[len(items)-1])
}
