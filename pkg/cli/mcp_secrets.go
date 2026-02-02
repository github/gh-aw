package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpSecretsLog = logger.New("cli:mcp_secrets")

// checkAndSuggestSecrets checks if required secrets exist in the repository and suggests CLI commands to add them
func checkAndSuggestSecrets(toolConfig map[string]any, verbose bool) error {
	mcpSecretsLog.Print("Checking and suggesting secrets for MCP tool configuration")

	// Extract environment variables from the tool config
	var requiredSecrets []string

	if mcpSection, ok := toolConfig["mcp"].(map[string]any); ok {
		if env, hasEnv := mcpSection["env"].(map[string]string); hasEnv {
			for _, value := range env {
				// Extract secret name from GitHub Actions syntax: ${{ secrets.SECRET_NAME }}
				if strings.HasPrefix(value, "${{ secrets.") && strings.HasSuffix(value, " }}") {
					secretName := value[12 : len(value)-3] // Remove "${{ secrets." and " }}"
					requiredSecrets = append(requiredSecrets, secretName)
				}
			}
		}
	}

	if len(requiredSecrets) == 0 {
		mcpSecretsLog.Print("No required secrets found in tool configuration")
		return nil
	}
	mcpSecretsLog.Printf("Found %d required secrets in configuration", len(requiredSecrets))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking repository secrets..."))
	}

	// Check each secret using GitHub CLI
	var missingSecrets []string
	for _, secretName := range requiredSecrets {
		exists, err := checkSecretExists(secretName)
		if err != nil {
			// If we get a 403 error, ignore it as requested
			if strings.Contains(err.Error(), "403") {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Repository secrets check skipped (insufficient permissions)"))
				}
				return nil
			}
			return err
		}

		if !exists {
			missingSecrets = append(missingSecrets, secretName)
		}
	}

	// Suggest CLI commands for missing secrets
	if len(missingSecrets) > 0 {
		mcpSecretsLog.Printf("Found %d missing secrets", len(missingSecrets))
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("The following secrets are required but not found in the repository:"))
		for _, secretName := range missingSecrets {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("To add %s secret:", secretName)))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("gh secret set %s", secretName)))
		}
	} else if verbose {
		mcpSecretsLog.Print("All required secrets are available in repository")
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All required secrets are available in the repository"))
	}

	return nil
}
