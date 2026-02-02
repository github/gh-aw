package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var secretsLog = logger.New("cli:secrets")

// SecretInfo contains information about a required secret
type SecretInfo struct {
	Name      string // Secret name (e.g., "DD_API_KEY")
	EnvKey    string // Environment variable key where it should be set
	Available bool   // Whether the secret is available
	Source    string // Where the secret was found ("env", "actions", or "")
	Value     string // The secret value (if fetched)
}

// checkSecretExists checks if a secret exists in the repository using GitHub CLI
func checkSecretExists(secretName string) (bool, error) {
	secretsLog.Printf("Checking if secret exists: %s", secretName)

	// Use gh CLI to list repository secrets
	output, err := workflow.RunGH("Listing secrets...", "secret", "list", "--json", "name")
	if err != nil {
		// Check if it's a 403 error by examining the error
		if exitError, ok := err.(*exec.ExitError); ok {
			if strings.Contains(string(exitError.Stderr), "403") {
				return false, fmt.Errorf("403 access denied")
			}
		}
		return false, fmt.Errorf("failed to list secrets: %w", err)
	}

	// Parse the JSON output
	var secrets []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(output, &secrets); err != nil {
		return false, fmt.Errorf("failed to parse secrets list: %w", err)
	}

	// Check if our secret exists
	for _, secret := range secrets {
		if secret.Name == secretName {
			return true, nil
		}
	}

	return false, nil
}

// extractSecretsFromConfig extracts all required secrets from an MCP server config
func extractSecretsFromConfig(config parser.MCPServerConfig) []SecretInfo {
	secretsLog.Printf("Extracting secrets from MCP config: command=%s", config.Command)
	var secrets []SecretInfo
	seen := make(map[string]bool)

	// Extract from HTTP headers
	for key, value := range config.Headers {
		secretName := workflow.ExtractSecretName(value)
		if secretName != "" && !seen[secretName] {
			secrets = append(secrets, SecretInfo{
				Name:   secretName,
				EnvKey: key,
			})
			seen[secretName] = true
		}
	}

	// Extract from environment variables
	for key, value := range config.Env {
		secretName := workflow.ExtractSecretName(value)
		if secretName != "" && !seen[secretName] {
			secrets = append(secrets, SecretInfo{
				Name:   secretName,
				EnvKey: key,
			})
			seen[secretName] = true
		}
	}

	secretsLog.Printf("Extracted %d secrets from config", len(secrets))
	return secrets
}

// checkSecretsAvailability checks which secrets are available and where
func checkSecretsAvailability(secrets []SecretInfo, useActionsSecrets bool) []SecretInfo {
	for i := range secrets {
		// First check if it's in environment variables
		if value := os.Getenv(secrets[i].Name); value != "" {
			secrets[i].Available = true
			secrets[i].Source = "env"
			secrets[i].Value = value
			continue
		}

		// If --check-secrets flag is enabled, try to fetch from GitHub Actions
		if useActionsSecrets {
			exists, err := checkSecretExists(secrets[i].Name)
			if err != nil {
				// If we get a 403 error, skip silently
				if !strings.Contains(err.Error(), "403") {
					continue
				}
			}
			if exists {
				secrets[i].Available = true
				secrets[i].Source = "actions"
				// Note: We can't actually fetch the secret value from GitHub Actions
				// The secret exists but its value is not accessible via gh CLI
				continue
			}
		}

		// Secret not available
		secrets[i].Available = false
		secrets[i].Source = ""
	}

	return secrets
}
