package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

func validateGitHubToolsAgainstToolsetsCore(allowedTools []string, enabledToolsets []string) error {
	githubToolToToolsetLog.Printf("Validating GitHub tools against toolsets: allowed_tools=%d, enabled_toolsets=%d", len(allowedTools), len(enabledToolsets))
	if len(allowedTools) == 0 {
		githubToolToToolsetLog.Print("No tools to validate, skipping")
		// No specific tools restricted, validation not needed
		return nil
	}

	toolToToolsetMap, err := getGitHubToolToToolsetMap()
	if err != nil {
		return fmt.Errorf("failed to load GitHub tool-to-toolset mapping: %w", err)
	}

	// Create a set of enabled toolsets for fast lookup
	enabledSet := make(map[string]struct {
	})
	for _, toolset := range enabledToolsets {
		enabledSet[toolset] = struct {
		}{}
	}
	githubToolToToolsetLog.Printf("Enabled toolsets: %v", enabledToolsets)

	missingToolsets, unknownTools, suggestions := findGitHubToolsetProblems(allowedTools, enabledSet, toolToToolsetMap)
	if len(unknownTools) > 0 {
		return unknownGitHubToolsError(unknownTools, suggestions, toolToToolsetMap)
	}
	if len(missingToolsets) > 0 {
		githubToolToToolsetLog.Printf("Validation failed: missing %d toolsets", len(missingToolsets))
		return NewGitHubToolsetValidationError(missingToolsets)
	}
	githubToolToToolsetLog.Print("Validation successful: all tools have required toolsets")
	return nil
}

func findGitHubToolsetProblems(allowedTools []string, enabledSet map[string]struct{}, toolToToolsetMap map[string]string) (map[string][]string, []string, []string) {
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
		requiredToolset, exists := toolToToolsetMap[tool]
		if !exists {
			githubToolToToolsetLog.Printf("Tool %s not found in mapping, checking for typo", tool)

			// Get all valid tool names for suggestion
			validTools := sliceutil.SortedKeys(toolToToolsetMap)

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

		if !setutil.Contains(enabledSet, requiredToolset) {
			githubToolToToolsetLog.Printf("Tool %s requires missing toolset: %s", tool, requiredToolset)
			missingToolsets[requiredToolset] = append(missingToolsets[requiredToolset], tool)
		}
	}
	return missingToolsets, unknownTools, suggestions
}

func unknownGitHubToolsError(unknownTools, suggestions []string, toolToToolsetMap map[string]string) error {
	// Report unknown tools with suggestions if any were found
	githubToolToToolsetLog.Printf("Found %d unknown tools", len(unknownTools))
	var errMsg strings.Builder
	fmt.Fprintf(&errMsg, "Unknown GitHub tool(s): %s\n\n", stringutil.FormatList(unknownTools))

	if len(suggestions) > 0 {
		errMsg.WriteString("Did you mean:\n")
		for _, s := range suggestions {
			fmt.Fprintf(&errMsg, "  %s\n", s)
		}
		errMsg.WriteString("\n")
	}

	// Show a few examples of valid tools
	validTools := sliceutil.SortedKeys(toolToToolsetMap)

	exampleCount := min(10, len(validTools))
	fmt.Fprintf(&errMsg, "Valid GitHub tools include: %s\n\n", stringutil.FormatList(validTools[:exampleCount]))
	errMsg.WriteString("See all tools: https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_tool_to_toolset.json")

	return errors.New(errMsg.String())
}
