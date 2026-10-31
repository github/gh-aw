package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var addInteractiveRunGH = workflow.RunGH

type secretSource string

const (
	secretSourceRepository           secretSource = "repository"
	secretSourceOrganizationAll      secretSource = "organization (all repositories)"
	secretSourceOrganizationPrivate  secretSource = "organization (private repositories)"
	secretSourceOrganizationSelected secretSource = "organization (selected repository)"
)

type organizationSecret struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

type organizationSecretsResponse struct {
	Secrets []organizationSecret `json:"secrets"`
}

// checkExistingSecrets fetches which secrets already exist in the repository or its organization
func (c *AddInteractiveConfig) checkExistingSecrets() error {
	addInteractiveLog.Print("Checking existing repository secrets")

	c.existingSecrets = make(map[string]struct{})
	c.secretSources = make(map[string]secretSource)

	// Use gh api to list repository secrets
	output, err := addInteractiveRunGH("Checking repository secrets...", "api", fmt.Sprintf("/repos/%s/actions/secrets", c.RepoOverride), "--jq", ".secrets[].name")
	if err != nil {
		addInteractiveLog.Printf("Could not fetch existing secrets: %v", err)
		// Continue without error - we'll just assume no secrets exist
	} else {
		for _, name := range parseSecretNames(output) {
			c.existingSecrets[name] = struct{}{}
			c.secretSources[name] = secretSourceRepository
			addInteractiveLog.Printf("Found existing repository secret: %s", name)
		}
	}

	// Also check org-level secrets if the repo belongs to an organization
	if org, _, found := strings.Cut(c.RepoOverride, "/"); found && org != "" {
		orgOutput, orgErr := addInteractiveRunGH("Checking organization secrets...", "api", fmt.Sprintf("/orgs/%s/actions/secrets", org), "--paginate", "--slurp")
		if orgErr != nil {
			addInteractiveLog.Printf("Could not fetch org secrets (this is expected for personal repos or if org access is restricted): %v", orgErr)
		} else {
			responses, err := parseOrganizationSecretsResponses(orgOutput)
			if err != nil {
				addInteractiveLog.Printf("Could not parse organization secrets: %v", err)
			} else {
				for _, response := range responses {
					for _, secret := range response.Secrets {
						if c.organizationSecretAvailable(org, secret) {
							c.existingSecrets[secret.Name] = struct{}{}
							c.secretSources[secret.Name] = organizationSecretSource(secret.Visibility)
							addInteractiveLog.Printf("Found available organization secret: %s", secret.Name)
						}
					}
				}
			}
		}
	}

	if c.Verbose && len(c.existingSecrets) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing secret(s) (repository + organization)", len(c.existingSecrets))))
	}

	return nil
}

func parseOrganizationSecretsResponses(output []byte) ([]organizationSecretsResponse, error) {
	var responses []organizationSecretsResponse
	if err := json.Unmarshal(output, &responses); err == nil {
		return responses, nil
	}
	var response organizationSecretsResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, err
	}
	return []organizationSecretsResponse{response}, nil
}

func (c *AddInteractiveConfig) organizationSecretAvailable(org string, secret organizationSecret) bool {
	switch secret.Visibility {
	case "all":
		return true
	case "private":
		return c.repositoryVisibility == "private"
	case "selected":
		output, err := addInteractiveRunGH(
			"Checking organization secret repository access...",
			"api",
			fmt.Sprintf("/orgs/%s/actions/secrets/%s/repositories", org, secret.Name),
			"--paginate",
			"--jq",
			".repositories[].full_name",
		)
		if err != nil {
			addInteractiveLog.Printf("Could not check repository access for organization secret %s: %v", secret.Name, err)
			return false
		}
		return sliceutil.Any(parseSecretNames(output), func(repo string) bool {
			return repo == c.RepoOverride
		})
	default:
		addInteractiveLog.Printf("Organization secret %s has unsupported visibility %q", secret.Name, secret.Visibility)
		return false
	}
}

func organizationSecretSource(visibility string) secretSource {
	switch visibility {
	case "all":
		return secretSourceOrganizationAll
	case "private":
		return secretSourceOrganizationPrivate
	case "selected":
		return secretSourceOrganizationSelected
	default:
		return ""
	}
}

// addRepositorySecret adds a secret to the repository
func (c *AddInteractiveConfig) addRepositorySecret(name, value string) error {
	if err := validateActionsStorageValue(value); err != nil {
		return err
	}
	output, err := workflow.RunGHInputContext(c.Ctx, "Adding repository secret...", bytes.NewBufferString(value), "secret", "set", name, "--repo", c.RepoOverride)
	if err != nil {
		return fmt.Errorf("failed to set secret: %w (output: %s)", err, string(output))
	}
	return nil
}

// parseSecretNames parses newline-delimited GitHub API output and returns the
// non-empty, trimmed secret names.
func parseSecretNames(output []byte) []string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	return sliceutil.Filter(
		sliceutil.Map(lines, strings.TrimSpace),
		func(name string) bool { return name != "" },
	)
}

// resolveEngineApiKeyCredential returns the secret name and value based on the selected engine
// Returns empty value if the secret already exists in the repository
func (c *AddInteractiveConfig) resolveEngineApiKeyCredential() (name string, value string, err error) {
	addInteractiveLog.Printf("Getting secret info for engine: %s", c.EngineOverride)

	secretName, secretValue, existsInRepo, err := GetEngineSecretNameAndValue(c.EngineOverride, c.existingSecrets)
	if err != nil {
		return "", "", err
	}

	// If secret exists in repo, return early
	if existsInRepo {
		return secretName, "", nil
	}

	// If value not found in environment, return error
	if secretValue == "" {
		return "", "", fmt.Errorf("API key not found for engine %s", c.EngineOverride)
	}

	return secretName, secretValue, nil
}
