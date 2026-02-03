package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var actionsLog = logger.New("cli:actions")

// convertToGitHubActionsEnv converts environment variables from shell syntax to GitHub Actions syntax
// Uses IsSecret field to determine between secrets.* and env.* syntax
// Leaves existing GitHub Actions syntax unchanged
func convertToGitHubActionsEnv(env any, envVarMetadata []EnvironmentVariable) map[string]string {
	actionsLog.Printf("Converting environment variables: metadata_count=%d", len(envVarMetadata))
	result := make(map[string]string)

	// Create a map for quick lookup of environment variable metadata
	envMetaMap := make(map[string]EnvironmentVariable)
	for _, envVar := range envVarMetadata {
		envMetaMap[envVar.Name] = envVar
	}

	if envMap, ok := env.(map[string]any); ok {
		convertedCount := 0
		unchangedCount := 0
		for key, value := range envMap {
			if valueStr, ok := value.(string); ok {
				// Only convert shell syntax ${TOKEN_NAME}, leave GitHub Actions syntax unchanged
				if strings.HasPrefix(valueStr, "${") && strings.HasSuffix(valueStr, "}") && !strings.Contains(valueStr, "{{") {
					tokenName := valueStr[2 : len(valueStr)-1] // Remove ${ and }

					// Check if we have metadata for this environment variable
					if envMeta, exists := envMetaMap[tokenName]; exists {
						if envMeta.IsSecret {
							result[key] = fmt.Sprintf("${{ secrets.%s }}", tokenName)
						} else {
							result[key] = fmt.Sprintf("${{ env.%s }}", tokenName)
						}
					} else {
						// Default to secrets if no metadata found (backward compatibility)
						result[key] = fmt.Sprintf("${{ secrets.%s }}", tokenName)
					}
					convertedCount++
				} else {
					// Keep as-is if not shell syntax or already GitHub Actions syntax
					result[key] = valueStr
					unchangedCount++
				}
			}
		}
		actionsLog.Printf("Environment variable conversion complete: converted=%d, unchanged=%d", convertedCount, unchangedCount)
	}

	return result
}
