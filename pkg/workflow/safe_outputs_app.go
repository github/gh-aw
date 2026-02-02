package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsAppLog = logger.New("workflow:safe_outputs_app")

// ========================================
// GitHub App Configuration
// ========================================

// GitHubAppConfig holds configuration for GitHub App-based token minting
type GitHubAppConfig struct {
	AppID        string   `yaml:"app-id,omitempty"`       // GitHub App ID (e.g., "${{ vars.APP_ID }}")
	PrivateKey   string   `yaml:"private-key,omitempty"`  // GitHub App private key (e.g., "${{ secrets.APP_PRIVATE_KEY }}")
	Owner        string   `yaml:"owner,omitempty"`        // Optional: owner of the GitHub App installation (defaults to current repository owner)
	Repositories []string `yaml:"repositories,omitempty"` // Optional: comma or newline-separated list of repositories to grant access to
}

// ========================================
// App Configuration Parsing
// ========================================

// parseAppConfig parses the app configuration from a map
func parseAppConfig(appMap map[string]any) *GitHubAppConfig {
	safeOutputsAppLog.Print("Parsing GitHub App configuration")
	appConfig := &GitHubAppConfig{}

	// Parse app-id (required)
	if appID, exists := appMap["app-id"]; exists {
		if appIDStr, ok := appID.(string); ok {
			appConfig.AppID = appIDStr
		}
	}

	// Parse private-key (required)
	if privateKey, exists := appMap["private-key"]; exists {
		if privateKeyStr, ok := privateKey.(string); ok {
			appConfig.PrivateKey = privateKeyStr
		}
	}

	// Parse owner (optional)
	if owner, exists := appMap["owner"]; exists {
		if ownerStr, ok := owner.(string); ok {
			appConfig.Owner = ownerStr
		}
	}

	// Parse repositories (optional)
	if repos, exists := appMap["repositories"]; exists {
		if reposArray, ok := repos.([]any); ok {
			var repoStrings []string
			for _, repo := range reposArray {
				if repoStr, ok := repo.(string); ok {
					repoStrings = append(repoStrings, repoStr)
				}
			}
			appConfig.Repositories = repoStrings
		}
	}

	return appConfig
}

// ========================================
// App Configuration Merging
// ========================================

// mergeAppFromIncludedConfigs merges app configuration from included safe-outputs configurations
// If the top-level workflow has an app configured, it takes precedence
// Otherwise, the first app configuration found in included configs is used
func (c *Compiler) mergeAppFromIncludedConfigs(topSafeOutputs *SafeOutputsConfig, includedConfigs []string) (*GitHubAppConfig, error) {
	safeOutputsAppLog.Printf("Merging app configuration: included_configs=%d", len(includedConfigs))
	// If top-level workflow already has app configured, use it (no merge needed)
	if topSafeOutputs != nil && topSafeOutputs.App != nil {
		safeOutputsAppLog.Print("Using top-level app configuration")
		return topSafeOutputs.App, nil
	}

	// Otherwise, find the first app configuration in included configs
	for _, configJSON := range includedConfigs {
		if configJSON == "" || configJSON == "{}" {
			continue
		}

		// Parse the safe-outputs configuration
		var safeOutputsConfig map[string]any
		if err := json.Unmarshal([]byte(configJSON), &safeOutputsConfig); err != nil {
			continue // Skip invalid JSON
		}

		// Extract app from the safe-outputs.app field
		if appData, exists := safeOutputsConfig["app"]; exists {
			if appMap, ok := appData.(map[string]any); ok {
				appConfig := parseAppConfig(appMap)

				// Return first valid app configuration found
				if appConfig.AppID != "" && appConfig.PrivateKey != "" {
					safeOutputsAppLog.Print("Found valid app configuration in included config")
					return appConfig, nil
				}
			}
		}
	}

	safeOutputsAppLog.Print("No app configuration found in included configs")
	return nil, nil
}

// ========================================
// GitHub App Token Steps Generation
// ========================================

// buildGitHubAppTokenMintStep generates the step to mint a GitHub App installation access token
// Permissions are automatically computed from the safe output job requirements
func (c *Compiler) buildGitHubAppTokenMintStep(app *GitHubAppConfig, permissions *Permissions) []string {
	safeOutputsAppLog.Printf("Building GitHub App token mint step: owner=%s, repos=%d", app.Owner, len(app.Repositories))
	var steps []string

	steps = append(steps, "      - name: Generate GitHub App token\n")
	steps = append(steps, "        id: safe-outputs-app-token\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/create-github-app-token")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          app-id: %s\n", app.AppID))
	steps = append(steps, fmt.Sprintf("          private-key: %s\n", app.PrivateKey))

	// Add owner - default to current repository owner if not specified
	owner := app.Owner
	if owner == "" {
		owner = "${{ github.repository_owner }}"
	}
	steps = append(steps, fmt.Sprintf("          owner: %s\n", owner))

	// Add repositories - default to current repository name if not specified
	if len(app.Repositories) > 0 {
		reposStr := strings.Join(app.Repositories, ",")
		steps = append(steps, fmt.Sprintf("          repositories: %s\n", reposStr))
	} else {
		// Extract repo name from github.repository (which is "owner/repo")
		// Using GitHub Actions expression: split(github.repository, '/')[1]
		steps = append(steps, "          repositories: ${{ github.event.repository.name }}\n")
	}

	// Always add github-api-url from environment variable
	steps = append(steps, "          github-api-url: ${{ github.api_url }}\n")

	// Add permission-* fields automatically computed from job permissions
	// Sort keys to ensure deterministic compilation order
	if permissions != nil {
		permissionFields := convertPermissionsToAppTokenFields(permissions)

		// Extract and sort keys for deterministic ordering
		keys := make([]string, 0, len(permissionFields))
		for key := range permissionFields {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		// Add permissions in sorted order
		for _, key := range keys {
			steps = append(steps, fmt.Sprintf("          %s: %s\n", key, permissionFields[key]))
		}
	}

	return steps
}

// convertPermissionsToAppTokenFields converts job Permissions to permission-* action inputs
// This follows GitHub's recommendation for explicit permission control
// Note: This only includes permissions that are valid for GitHub App tokens.
// Some GitHub Actions permissions (like 'discussions', 'models') don't have
// corresponding GitHub App permissions and are skipped.
func convertPermissionsToAppTokenFields(permissions *Permissions) map[string]string {
	fields := make(map[string]string)

	// Map GitHub Actions permissions to GitHub App permissions
	// Only include permissions that exist in the actions/create-github-app-token action
	// See: https://github.com/actions/create-github-app-token#permissions

	// Repository permissions that map directly
	if level, ok := permissions.Get(PermissionActions); ok {
		fields["permission-actions"] = string(level)
	}
	if level, ok := permissions.Get(PermissionChecks); ok {
		fields["permission-checks"] = string(level)
	}
	if level, ok := permissions.Get(PermissionContents); ok {
		fields["permission-contents"] = string(level)
	}
	if level, ok := permissions.Get(PermissionDeployments); ok {
		fields["permission-deployments"] = string(level)
	}
	if level, ok := permissions.Get(PermissionIssues); ok {
		fields["permission-issues"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPackages); ok {
		fields["permission-packages"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPages); ok {
		fields["permission-pages"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPullRequests); ok {
		fields["permission-pull-requests"] = string(level)
	}
	if level, ok := permissions.Get(PermissionSecurityEvents); ok {
		fields["permission-security-events"] = string(level)
	}
	if level, ok := permissions.Get(PermissionStatuses); ok {
		fields["permission-statuses"] = string(level)
	}
	if level, ok := permissions.Get(PermissionOrganizationProj); ok {
		fields["permission-organization-projects"] = string(level)
	}
	if level, ok := permissions.Get(PermissionDiscussions); ok {
		fields["permission-discussions"] = string(level)
	}

	// Note: The following GitHub Actions permissions do NOT have GitHub App equivalents:
	// - models (no GitHub App permission for this)
	// - id-token (not applicable to GitHub Apps)
	// - attestations (no GitHub App permission for this)
	// - repository-projects (removed - classic projects are sunset; use organization-projects for Projects v2 via PAT/GitHub App)

	return fields
}

// buildGitHubAppTokenInvalidationStep generates the step to invalidate the GitHub App token
// This step always runs (even on failure) to ensure tokens are properly cleaned up
// Only runs if a token was successfully minted
func (c *Compiler) buildGitHubAppTokenInvalidationStep() []string {
	var steps []string

	steps = append(steps, "      - name: Invalidate GitHub App token\n")
	steps = append(steps, "        if: always() && steps.safe-outputs-app-token.outputs.token != ''\n")
	steps = append(steps, "        env:\n")
	steps = append(steps, "          TOKEN: ${{ steps.safe-outputs-app-token.outputs.token }}\n")
	steps = append(steps, "        run: |\n")
	steps = append(steps, "          echo \"Revoking GitHub App installation token...\"\n")
	steps = append(steps, "          # GitHub CLI will auth with the token being revoked.\n")
	steps = append(steps, "          gh api \\\n")
	steps = append(steps, "            --method DELETE \\\n")
	steps = append(steps, "            -H \"Authorization: token $TOKEN\" \\\n")
	steps = append(steps, "            /installation/token || echo \"Token revoke may already be expired.\"\n")
	steps = append(steps, "          \n")
	steps = append(steps, "          echo \"Token invalidation step complete.\"\n")

	return steps
}
