//go:build !integration

package workflow

import (
	"testing"
)

func TestNeutralToolsExpandsToClaudeTools(t *testing.T) {
	engine := NewClaudeEngine()

	// Test neutral tools input
	neutralTools := map[string]any{
		"bash":       []any{"echo", "ls"},
		"web-fetch":  nil,
		"web-search": nil,
		"edit":       nil,
		"playwright": nil,
		"github": map[string]any{
			"allowed": []any{"list_issues"},
		},
	}

	// Test with safe outputs that require git commands
	safeOutputs := &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
		},
	}

	// Extract cache-memory config
	compiler := NewCompiler()
	cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(neutralTools)
	result := engine.computeAllowedClaudeToolsString(neutralTools, safeOutputs, cacheMemoryConfig)

	// Verify that neutral tools are converted to Claude tools
	expectedTools := []string{
		"Bash(echo)",
		"Bash(ls)",
		"BashOutput",
		"KillBash",
		"WebFetch",
		"WebSearch",
		"Edit",
		"MultiEdit",
		"NotebookEdit",
		"Write",
		"mcp__github__list_issues",
	}

	for _, expectedTool := range expectedTools {
		if !containsTool(result, expectedTool) {
			t.Errorf("Expected tool '%s' not found in result: %s", expectedTool, result)
		}
	}

	// Verify default Claude tools are included
	defaultTools := []string{
		"Task",
		"Glob",
		"Grep",
		"ExitPlanMode",
		"TodoWrite",
		"LS",
		"Read",
		"NotebookRead",
	}

	for _, defaultTool := range defaultTools {
		if !containsTool(result, defaultTool) {
			t.Errorf("Expected default tool '%s' not found in result: %s", defaultTool, result)
		}
	}
}

func TestNeutralToolsWithoutSafeOutputs(t *testing.T) {
	engine := NewClaudeEngine()

	// Test neutral tools input
	neutralTools := map[string]any{
		"bash":      []any{"echo"},
		"web-fetch": nil,
		"edit":      nil,
	}

	// Extract cache-memory config
	compiler := NewCompiler()
	cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(neutralTools)
	result := engine.computeAllowedClaudeToolsString(neutralTools, nil, cacheMemoryConfig)

	// Should include converted neutral tools
	expectedTools := []string{
		"Bash(echo)",
		"BashOutput",
		"KillBash",
		"WebFetch",
		"Edit",
		"MultiEdit",
		"NotebookEdit",
		"Write",
	}

	for _, expectedTool := range expectedTools {
		if !containsTool(result, expectedTool) {
			t.Errorf("Expected tool '%s' not found in result: %s", expectedTool, result)
		}
	}

	// Should NOT include Git commands (no safe outputs)
	gitTools := []string{
		"Bash(git add:*)",
		"Bash(git commit:*)",
	}

	for _, gitTool := range gitTools {
		if containsTool(result, gitTool) {
			t.Errorf("Git tool '%s' should not be present without safe outputs: %s", gitTool, result)
		}
	}
}

// Helper function to check if a tool is present in the comma-separated result
func containsTool(result, tool string) bool {
	tools := splitTools(result)
	for _, t := range tools {
		if t == tool {
			return true
		}
	}
	return false
}

func splitTools(result string) []string {
	if result == "" {
		return []string{}
	}
	tools := []string{}
	for _, tool := range splitByComma(result) {
		trimmed := trimWhitespace(tool)
		if trimmed != "" {
			tools = append(tools, trimmed)
		}
	}
	return tools
}

func splitByComma(s string) []string {
	result := []string{}
	current := ""
	for _, char := range s {
		if char == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trimWhitespace(s string) string {
	// Simple whitespace trimming
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
