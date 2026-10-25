package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var toolsetsLog = logger.New("workflow:github_toolsets")

// DefaultGitHubToolsets defines the toolsets that are enabled by default
// when toolsets are not explicitly specified in the GitHub MCP configuration.
// These match the documented default toolsets in github-mcp-server.md
var DefaultGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}

// ActionFriendlyGitHubToolsets defines the default toolsets that work with GitHub Actions tokens.
// This excludes "users" toolset because GitHub Actions tokens do not support user operations.
// Use this when the workflow will run in GitHub Actions with GITHUB_TOKEN.
var ActionFriendlyGitHubToolsets = []string{"context", "repos", "issues", "pull_requests"}

// GitHubToolsetsExcludedFromAll defines toolsets that are NOT included when "all" is specified.
// These toolsets are opt-in only to avoid granting unnecessary permissions by default.
var GitHubToolsetsExcludedFromAll = []string{"dependabot"}

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
	seenToolsets := make(map[string]struct {
	})

	for _, toolset := range toolsets {
		toolset = strings.TrimSpace(toolset)
		if toolset == "" {
			continue
		}

		expanded = expandGitHubToolset(expanded, seenToolsets, toolset)
	}

	toolsetsLog.Printf("Parsed toolsets result: %d unique toolsets expanded from input", len(expanded))
	return expanded
}

func expandGitHubToolset(expanded []string, seenToolsets map[string]struct{}, toolset string) []string {
	switch toolset {
	case "default":
		// Add default toolsets
		toolsetsLog.Printf("Expanding 'default' to %d toolsets", len(DefaultGitHubToolsets))
		for _, dt := range DefaultGitHubToolsets {
			if !setutil.Contains(seenToolsets, dt) {
				expanded = append(expanded, dt)
				seenToolsets[dt] = struct {
				}{}
			}
		}
	case "action-friendly":
		// Add action-friendly toolsets (excludes "users" which GitHub Actions tokens don't support)
		toolsetsLog.Printf("Expanding 'action-friendly' to %d toolsets", len(ActionFriendlyGitHubToolsets))
		for _, dt := range ActionFriendlyGitHubToolsets {
			if !setutil.Contains(seenToolsets, dt) {
				expanded = append(expanded, dt)
				seenToolsets[dt] = struct {
				}{}
			}
		}
	case "all":
		expanded = expandAllGitHubToolsets(expanded, seenToolsets)
	default:
		// Add individual toolset
		if !setutil.Contains(seenToolsets, toolset) {
			expanded = append(expanded, toolset)
			seenToolsets[toolset] = struct {
			}{}
		}
	}
	return expanded
}

func expandAllGitHubToolsets(expanded []string, seenToolsets map[string]struct{}) []string {
	// Add all toolsets from the toolset permissions map, excluding those that
	// require GitHub App-only permissions (see GitHubToolsetsExcludedFromAll).
	toolsetsLog.Printf("Expanding 'all' to toolsets from permissions map (excluding %v)", GitHubToolsetsExcludedFromAll)
	excludedMap := make(map[string]struct {
	}, len(GitHubToolsetsExcludedFromAll))
	for _, ex := range GitHubToolsetsExcludedFromAll {
		excludedMap[ex] = struct {
		}{}
	}
	toolsetPermissionsMap := getToolsetPermissionsMap()
	for t := range toolsetPermissionsMap {
		if setutil.Contains(excludedMap, t) {
			continue
		}
		if !setutil.Contains(seenToolsets, t) {
			expanded = append(expanded, t)
			seenToolsets[t] = struct {
			}{}
		}
	}
	return expanded
}
