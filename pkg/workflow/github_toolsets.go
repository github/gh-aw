package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var toolsetsLog = logger.New("workflow:github_toolsets")

// DefaultGitHubToolsets defines the toolsets that are enabled by default
// when toolsets are not explicitly specified in the GitHub MCP configuration.
// These match the documented default toolsets in github-mcp-server.instructions.md
var DefaultGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}

// ActionFriendlyGitHubToolsets defines the default toolsets that work with GitHub Actions tokens.
// This excludes "users" toolset because GitHub Actions tokens do not support user operations.
// Use this when the workflow will run in GitHub Actions with GITHUB_TOKEN.
var ActionFriendlyGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}

// ParseGitHubToolsets parses the toolsets string and expands "default" and "all"
// into their constituent toolsets. It handles comma-separated lists and deduplicates.
func ParseGitHubToolsets(toolsetsStr string) []string {
	toolsetsLog.Printf("Parsing GitHub toolsets: %q", toolsetsStr)

	if toolsetsStr == "" {
		toolsetsLog.Printf("Empty toolsets string, using defaults: %v", DefaultGitHubToolsets)
		return DefaultGitHubToolsets
	}

	toolsets := strings.Split(toolsetsStr, ",")
	var expanded []string
	seenToolsets := make(map[string]bool)

	for _, toolset := range toolsets {
		toolset = strings.TrimSpace(toolset)
		if toolset == "" {
			continue
		}

		switch toolset {
		case "default":
			// Add default toolsets
			toolsetsLog.Printf("Expanding 'default' to %d toolsets", len(DefaultGitHubToolsets))
			for _, dt := range DefaultGitHubToolsets {
				if !seenToolsets[dt] {
					expanded = append(expanded, dt)
					seenToolsets[dt] = true
				}
			}
		case "action-friendly":
			// Add action-friendly toolsets (excludes "users" which GitHub Actions tokens don't support)
			toolsetsLog.Printf("Expanding 'action-friendly' to %d toolsets", len(ActionFriendlyGitHubToolsets))
			for _, dt := range ActionFriendlyGitHubToolsets {
				if !seenToolsets[dt] {
					expanded = append(expanded, dt)
					seenToolsets[dt] = true
				}
			}
		case "all":
			// Add all toolsets from the toolset permissions map
			toolsetsLog.Printf("Expanding 'all' to %d toolsets from permissions map", len(toolsetPermissionsMap))
			for t := range toolsetPermissionsMap {
				if !seenToolsets[t] {
					expanded = append(expanded, t)
					seenToolsets[t] = true
				}
			}
		default:
			// Add individual toolset
			if !seenToolsets[toolset] {
				expanded = append(expanded, toolset)
				seenToolsets[toolset] = true
			}
		}
	}

	toolsetsLog.Printf("Parsed toolsets result: %d unique toolsets expanded from input", len(expanded))
	return expanded
}
