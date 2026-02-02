package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

// checkExistingSecrets fetches which secrets already exist in the repository
func (c *AddInteractiveConfig) checkExistingSecrets() error {
	addInteractiveLog.Print("Checking existing repository secrets")

	c.existingSecrets = make(map[string]bool)

	// Use gh api to list repository secrets
	output, err := workflow.RunGH("Checking repository secrets...", "api", fmt.Sprintf("/repos/%s/actions/secrets", c.RepoOverride), "--jq", ".secrets[].name")
	if err != nil {
		addInteractiveLog.Printf("Could not fetch existing secrets: %v", err)
		// Continue without error - we'll just assume no secrets exist
		return nil
	}

	// Parse the output - each secret name is on its own line
	secretNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, name := range secretNames {
		name = strings.TrimSpace(name)
		if name != "" {
			c.existingSecrets[name] = true
			addInteractiveLog.Printf("Found existing secret: %s", name)
		}
	}

	if c.Verbose && len(c.existingSecrets) > 0 {
		fmt.Fprintf(os.Stderr, "Found %d existing repository secret(s)\n", len(c.existingSecrets))
	}

	return nil
}

// addRepositorySecret adds a secret to the repository
func (c *AddInteractiveConfig) addRepositorySecret(name, value string) error {
	output, err := workflow.RunGHCombined("Adding repository secret...", "secret", "set", name, "--repo", c.RepoOverride, "--body", value)
	if err != nil {
		return fmt.Errorf("failed to set secret: %w (output: %s)", err, string(output))
	}
	return nil
}

// getSecretInfo returns the secret name and value based on the selected engine
// Returns empty value if the secret already exists in the repository
func (c *AddInteractiveConfig) getSecretInfo() (name string, value string, err error) {
	addInteractiveLog.Printf("Getting secret info for engine: %s", c.EngineOverride)

	opt := constants.GetEngineOption(c.EngineOverride)
	if opt == nil {
		return "", "", fmt.Errorf("unknown engine: %s", c.EngineOverride)
	}

	name = opt.SecretName

	// If secret already exists in repo, we don't need a value
	if c.existingSecrets[name] {
		addInteractiveLog.Printf("Secret %s already exists in repository", name)
		return name, "", nil
	}

	// Get value from environment variable (use EnvVarName if specified, otherwise SecretName)
	envVar := opt.SecretName
	if opt.EnvVarName != "" {
		envVar = opt.EnvVarName
	}
	value = os.Getenv(envVar)

	if value == "" {
		return "", "", fmt.Errorf("API key not found for engine %s", c.EngineOverride)
	}

	return name, value, nil
}
