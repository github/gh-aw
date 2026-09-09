package workflow

import "reflect"

// countRuntimes counts the number of non-nil runtimes in RuntimesConfig
func countRuntimes(config *RuntimesConfig) int {
	if config == nil {
		return 0
	}
	count := 0
	if config.Node != nil {
		count++
	}
	if config.Python != nil {
		count++
	}
	if config.Go != nil {
		count++
	}
	if config.UV != nil {
		count++
	}
	if config.Bun != nil {
		count++
	}
	if config.Deno != nil {
		count++
	}
	if config.GhAw != nil {
		count++
	}
	return count
}

// ExtractMapField is a convenience wrapper for extracting map[string]any fields
// from frontmatter. This maintains backward compatibility with existing extraction
// patterns while preserving original types (avoiding JSON conversion which would
// convert all numbers to float64).
//
// Returns an empty map if the key doesn't exist (for backward compatibility).
func ExtractMapField(frontmatter map[string]any, key string) map[string]any {
	// Check if key exists and value is not nil
	value, exists := frontmatter[key]
	if !exists || value == nil {
		frontmatterTypesLog.Printf("Field '%s' not found in frontmatter, returning empty map", key)
		return make(map[string]any)
	}

	// Direct type assertion to preserve original types (especially integers)
	// This avoids JSON marshaling which would convert integers to float64
	if valueMap, ok := value.(map[string]any); ok {
		frontmatterTypesLog.Printf("Extracted map field '%s' with %d entries", key, len(valueMap))
		return valueMap
	}

	// For backward compatibility, return empty map if not a map
	frontmatterTypesLog.Printf("Field '%s' is not a map type, returning empty map", key)
	return make(map[string]any)
}

// ToMap converts FrontmatterConfig back to map[string]any for backward compatibility
// This allows gradual migration from map[string]any to strongly-typed config
func (fc *FrontmatterConfig) ToMap() map[string]any {
	frontmatterTypesLog.Printf("Converting FrontmatterConfig to map: name=%s", fc.Name)
	result := make(map[string]any)

	// Core fields
	if fc.Name != "" {
		result["name"] = fc.Name
	}
	if fc.Description != "" {
		result["description"] = fc.Description
	}
	if fc.Intent != "" {
		result["intent"] = fc.Intent
	}
	if fc.Engine != nil {
		result["engine"] = fc.Engine
	}
	if fc.Source != "" {
		result["source"] = fc.Source
	}
	if fc.Redirect != "" {
		result["redirect"] = fc.Redirect
	}
	if fc.TrackerID != "" {
		result["tracker-id"] = fc.TrackerID
	}
	if fc.TimeoutMinutes != nil {
		result["timeout-minutes"] = fc.TimeoutMinutes.ToValue()
	}
	if fc.Strict != nil {
		result["strict"] = *fc.Strict
	}
	if len(fc.Labels) > 0 {
		result["labels"] = fc.Labels
	}
	if len(fc.AmbientFolders) > 0 {
		result["ambient-folders"] = fc.AmbientFolders
	}
	if fc.GitHubApp != nil {
		result["github-app"] = githubAppConfigToMap(fc.GitHubApp)
	}

	// Configuration sections
	if fc.Tools != nil {
		result["tools"] = fc.Tools.ToMap()
	}
	if fc.MCPServers != nil {
		result["mcp-servers"] = fc.MCPServers
	}
	// Prefer RuntimesTyped over Runtimes for conversion
	if fc.RuntimesTyped != nil {
		result["runtimes"] = runtimesConfigToMap(fc.RuntimesTyped)
	} else if fc.Runtimes != nil {
		result["runtimes"] = fc.Runtimes
	}
	if fc.Jobs != nil {
		result["jobs"] = fc.Jobs
	}
	if fc.SafeOutputs != nil {
		// Convert SafeOutputsConfig to map - would need a ToMap method
		result["safe-outputs"] = fc.SafeOutputs
	}
	if fc.MCPScripts != nil {
		// Convert MCPScriptsConfig to map - would need a ToMap method
		result["mcp-scripts"] = fc.MCPScripts
	}
	if len(fc.Enclaves) > 0 {
		result["enclaves"] = fc.Enclaves
	}

	// Event and trigger configuration
	if fc.On != nil {
		result["on"] = fc.On
	}
	// Prefer PermissionsTyped over Permissions for conversion
	if fc.PermissionsTyped != nil {
		result["permissions"] = permissionsConfigToMap(fc.PermissionsTyped)
	} else if fc.Permissions != nil {
		result["permissions"] = fc.Permissions
	}
	if fc.Concurrency != nil {
		result["concurrency"] = fc.Concurrency
	}
	if fc.If != "" {
		result["if"] = fc.If
	}

	// Network and sandbox
	if fc.Network != nil {
		// Convert NetworkPermissions to map format
		// If allowed list is just ["defaults"], convert to string format "defaults"
		if len(fc.Network.Allowed) == 1 && fc.Network.Allowed[0] == "defaults" && !fc.Network.AllowedInput && fc.Network.Firewall == nil && len(fc.Network.Blocked) == 0 {
			result["network"] = "defaults"
		} else {
			networkMap := make(map[string]any)
			if len(fc.Network.Allowed) > 0 {
				networkMap["allowed"] = fc.Network.Allowed
			}
			if fc.Network.AllowedInput {
				networkMap["allowed-input"] = true
			}
			if len(fc.Network.Blocked) > 0 {
				networkMap["blocked"] = fc.Network.Blocked
			}
			if fc.Network.Firewall != nil {
				networkMap["firewall"] = fc.Network.Firewall
			}
			if len(networkMap) > 0 {
				result["network"] = networkMap
			}
		}
	}
	if fc.Sandbox != nil {
		result["sandbox"] = fc.Sandbox
	}

	// Features and environment
	if fc.Features != nil {
		result["features"] = fc.Features
	}
	if fc.Env != nil {
		result["env"] = fc.Env
	}
	if fc.Secrets != nil {
		result["secrets"] = fc.Secrets
	}

	// Execution settings
	if !isNilValue(fc.RunsOn) {
		result["runs-on"] = fc.RunsOn
	}
	if !isEmptyRunsOnValue(fc.RunsOnSlim) {
		result["runs-on-slim"] = fc.RunsOnSlim
	}
	if fc.RunName != "" {
		result["run-name"] = fc.RunName
	}
	if fc.PreSteps != nil {
		result["pre-steps"] = fc.PreSteps
	}
	if fc.Steps != nil {
		result["steps"] = fc.Steps
	}
	if fc.PreAgentSteps != nil {
		result["pre-agent-steps"] = fc.PreAgentSteps
	}
	if fc.PostSteps != nil {
		result["post-steps"] = fc.PostSteps
	}
	if fc.Environment != nil {
		result["environment"] = fc.Environment
	}
	if fc.Container != nil {
		result["container"] = fc.Container
	}
	if fc.Services != nil {
		result["services"] = fc.Services
	}
	if fc.Cache != nil {
		result["cache"] = fc.Cache
	}

	// Import and inclusion
	if fc.Imports != nil {
		result["imports"] = fc.Imports
	}

	// Metadata
	if fc.Metadata != nil {
		result["metadata"] = fc.Metadata
	}
	if fc.SecretMasking != nil {
		result["secret-masking"] = fc.SecretMasking
	}

	return result
}

func githubAppConfigToMap(app *GitHubAppConfig) map[string]any {
	result := make(map[string]any)
	if app.AppID != "" {
		result["client-id"] = app.AppID
	}
	if app.PrivateKey != "" {
		result["private-key"] = app.PrivateKey
	}
	if app.IgnoreIfMissing {
		result["ignore-if-missing"] = true
	}
	if app.Owner != "" {
		result["owner"] = app.Owner
	}
	if len(app.Repositories) > 0 {
		repositories := make([]any, len(app.Repositories))
		for i, repository := range app.Repositories {
			repositories[i] = repository
		}
		result["repositories"] = repositories
	}
	if len(app.Permissions) > 0 {
		permissions := make(map[string]any, len(app.Permissions))
		for permission, level := range app.Permissions {
			permissions[permission] = level
		}
		result["permissions"] = permissions
	}
	return result
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// runtimeConfigToMap converts a single RuntimeConfig to map[string]any
func runtimeConfigToMap(rc *RuntimeConfig) map[string]any {
	m := map[string]any{}
	if rc.Version != "" {
		m["version"] = rc.Version
	}
	if rc.If != "" {
		m["if"] = rc.If
	}
	if rc.ActionRepo != "" {
		m["action-repo"] = rc.ActionRepo
	}
	if rc.ActionVersion != "" {
		m["action-version"] = rc.ActionVersion
	}
	if rc.Cooldown != nil {
		m["cooldown"] = *rc.Cooldown
	}
	if rc.RunInstallScripts != nil {
		m["run-install-scripts"] = *rc.RunInstallScripts
	}
	return m
}

// runtimesConfigToMap converts RuntimesConfig back to map[string]any
func runtimesConfigToMap(config *RuntimesConfig) map[string]any {
	if config == nil {
		return nil
	}
	frontmatterTypesLog.Printf("Converting RuntimesConfig to map: %d runtime(s) configured", countRuntimes(config))

	result := make(map[string]any)

	runtimes := []struct {
		key string
		rc  *RuntimeConfig
	}{
		{"node", config.Node},
		{"python", config.Python},
		{"go", config.Go},
		{"uv", config.UV},
		{"bun", config.Bun},
		{"deno", config.Deno},
		{"dotnet", config.Dotnet},
		{"elixir", config.Elixir},
		{"gh-aw", config.GhAw},
		{"haskell", config.Haskell},
		{"java", config.Java},
		{"ruby", config.Ruby},
	}
	for _, r := range runtimes {
		if r.rc != nil {
			if m := runtimeConfigToMap(r.rc); len(m) > 0 {
				result[r.key] = m
			}
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// permissionsConfigToMap converts PermissionsConfig back to map[string]any
func permissionsConfigToMap(config *PermissionsConfig) map[string]any {
	if config == nil {
		return nil
	}

	// If shorthand is set, return it directly
	if config.Shorthand != "" {
		frontmatterTypesLog.Printf("Converting PermissionsConfig to map via shorthand: %s", config.Shorthand)
		return map[string]any{config.Shorthand: config.Shorthand}
	}

	frontmatterTypesLog.Print("Converting detailed PermissionsConfig to map")
	result := make(map[string]any)

	// GitHub Actions permission scopes
	if config.Actions != "" {
		result["actions"] = config.Actions
	}
	if config.Checks != "" {
		result["checks"] = config.Checks
	}
	if config.CodeQuality != "" {
		result["code-quality"] = config.CodeQuality
	}
	if config.Contents != "" {
		result["contents"] = config.Contents
	}
	if config.Deployments != "" {
		result["deployments"] = config.Deployments
	}
	if config.IDToken != "" {
		result["id-token"] = config.IDToken
	}
	if config.Issues != "" {
		result["issues"] = config.Issues
	}
	if config.Discussions != "" {
		result["discussions"] = config.Discussions
	}
	if config.Drives != "" {
		result["drives"] = config.Drives
	}
	if config.Packages != "" {
		result["packages"] = config.Packages
	}
	if config.Pages != "" {
		result["pages"] = config.Pages
	}
	if config.PullRequests != "" {
		result["pull-requests"] = config.PullRequests
	}
	if config.RepositoryProjects != "" {
		result["repository-projects"] = config.RepositoryProjects
	}
	if config.SecurityEvents != "" {
		result["security-events"] = config.SecurityEvents
	}
	if config.Statuses != "" {
		result["statuses"] = config.Statuses
	}
	if config.VulnerabilityAlerts != "" {
		result["vulnerability-alerts"] = config.VulnerabilityAlerts
	}
	if config.OrganizationProjects != "" {
		result["organization-projects"] = config.OrganizationProjects
	}

	// GitHub App-only permission scopes - repository-level
	if config.Administration != "" {
		result["administration"] = config.Administration
	}
	if config.Environments != "" {
		result["environments"] = config.Environments
	}
	if config.GitSigning != "" {
		result["git-signing"] = config.GitSigning
	}
	if config.Workflows != "" {
		result["workflows"] = config.Workflows
	}
	if config.RepositoryHooks != "" {
		result["repository-hooks"] = config.RepositoryHooks
	}
	if config.SingleFile != "" {
		result["single-file"] = config.SingleFile
	}
	if config.Codespaces != "" {
		result["codespaces"] = config.Codespaces
	}
	if config.RepositoryCustomProperties != "" {
		result["repository-custom-properties"] = config.RepositoryCustomProperties
	}

	// GitHub App-only permission scopes - organization-level
	if config.Members != "" {
		result["members"] = config.Members
	}
	if config.OrganizationAdministration != "" {
		result["organization-administration"] = config.OrganizationAdministration
	}
	if config.TeamDiscussions != "" {
		result["team-discussions"] = config.TeamDiscussions
	}
	if config.OrganizationHooks != "" {
		result["organization-hooks"] = config.OrganizationHooks
	}
	if config.OrganizationMembers != "" {
		result["organization-members"] = config.OrganizationMembers
	}
	if config.OrganizationPackages != "" {
		result["organization-packages"] = config.OrganizationPackages
	}
	if config.OrganizationSelfHostedRunners != "" {
		result["organization-self-hosted-runners"] = config.OrganizationSelfHostedRunners
	}
	if config.OrganizationCustomOrgRoles != "" {
		result["organization-custom-org-roles"] = config.OrganizationCustomOrgRoles
	}
	if config.OrganizationCustomProperties != "" {
		result["organization-custom-properties"] = config.OrganizationCustomProperties
	}
	if config.OrganizationCustomRepositoryRoles != "" {
		result["organization-custom-repository-roles"] = config.OrganizationCustomRepositoryRoles
	}
	if config.OrganizationAnnouncementBanners != "" {
		result["organization-announcement-banners"] = config.OrganizationAnnouncementBanners
	}
	if config.OrganizationEvents != "" {
		result["organization-events"] = config.OrganizationEvents
	}
	if config.OrganizationPlan != "" {
		result["organization-plan"] = config.OrganizationPlan
	}
	if config.OrganizationUserBlocking != "" {
		result["organization-user-blocking"] = config.OrganizationUserBlocking
	}
	if config.OrganizationPersonalAccessTokenReqs != "" {
		result["organization-personal-access-token-requests"] = config.OrganizationPersonalAccessTokenReqs
	}
	if config.OrganizationPersonalAccessTokens != "" {
		result["organization-personal-access-tokens"] = config.OrganizationPersonalAccessTokens
	}
	if config.OrganizationCopilot != "" {
		result["organization-copilot"] = config.OrganizationCopilot
	}
	if config.OrganizationCodespaces != "" {
		result["organization-codespaces"] = config.OrganizationCodespaces
	}

	// GitHub App-only permission scopes - user-level
	if config.EmailAddresses != "" {
		result["email-addresses"] = config.EmailAddresses
	}
	if config.CodespacesLifecycleAdmin != "" {
		result["codespaces-lifecycle-admin"] = config.CodespacesLifecycleAdmin
	}
	if config.CodespacesMetadata != "" {
		result["codespaces-metadata"] = config.CodespacesMetadata
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
