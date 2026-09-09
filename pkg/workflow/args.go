package workflow

import "github.com/github/gh-aw/pkg/logger"

var argsLog = logger.New("workflow:args")

// extractCustomArgs extracts custom args from tool configuration
// Handles both []any and []string formats
func extractCustomArgs(toolConfig map[string]any) []string {
	if argsValue, exists := toolConfig["args"]; exists {
		argsLog.Print("Extracting custom args from tool configuration")

		// Handle []any format
		if argsSlice, ok := argsValue.([]any); ok {
			customArgs := make([]string, 0, len(argsSlice))
			for _, arg := range argsSlice {
				if argStr, ok := arg.(string); ok {
					customArgs = append(customArgs, argStr)
				}
			}
			argsLog.Printf("Extracted %d args from []any format", len(customArgs))
			return customArgs
		}
		// Handle []string format
		if argsSlice, ok := argsValue.([]string); ok {
			argsLog.Printf("Extracted %d args from []string format", len(argsSlice))
			return argsSlice
		}
	}
	return nil
}

// getGitHubCustomArgs extracts custom args from GitHub tool configuration
func getGitHubCustomArgs(githubTool any) []string {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		return extractCustomArgs(toolConfig)
	}
	return nil
}
