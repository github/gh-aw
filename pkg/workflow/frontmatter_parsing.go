package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ParseFrontmatterConfig creates a FrontmatterConfig from a raw frontmatter map
// This provides a single entry point for converting untyped frontmatter into
// a structured configuration with better error handling.
func ParseFrontmatterConfig(frontmatter map[string]any) (*FrontmatterConfig, error) {
	frontmatterTypesLog.Printf("Parsing frontmatter config with %d fields", len(frontmatter))
	var config FrontmatterConfig

	githubApp, err := extractTypedTopLevelGitHubApp(frontmatter)
	if err != nil {
		return nil, err
	}

	// Use JSON marshaling for the entire frontmatter conversion.
	// TemplatableInt32.UnmarshalJSON transparently handles both integer literals
	// (e.g. timeout-minutes: 30) and GitHub Actions expressions
	// (e.g. timeout-minutes: ${{ inputs.timeout }}) during unmarshaling.
	jsonBytes, err := json.Marshal(frontmatter)
	if err != nil {
		frontmatterTypesLog.Printf("Failed to marshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to marshal frontmatter to JSON: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &config); err != nil {
		frontmatterTypesLog.Printf("Failed to unmarshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to unmarshal frontmatter into config: %w", err)
	}
	if err := validateThreatDetectionSuppressions(config.ThreatDetectionSuppressions); err != nil {
		return nil, err
	}

	if err := validateRunsOnValue(config.RunsOn); err != nil {
		return nil, err
	}
	if err := validateRunsOnValue(config.RunsOnSlim); err != nil {
		return nil, err
	}
	if safeOutputsRaw, ok := frontmatter["safe-outputs"].(map[string]any); ok {
		if err := validateRunsOnValue(safeOutputsRaw["runs-on"]); err != nil {
			return nil, err
		}
		if threatRaw, ok := safeOutputsRaw["threat-detection"].(map[string]any); ok {
			if err := validateRunsOnValue(threatRaw["runs-on"]); err != nil {
				return nil, err
			}
		}
		if jobsRaw, ok := safeOutputsRaw["jobs"].(map[string]any); ok {
			for _, jobRaw := range jobsRaw {
				if job, ok := jobRaw.(map[string]any); ok {
					if err := validateRunsOnValue(job["runs-on"]); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	// Parse typed Runtimes field if runtimes exist
	if len(config.Runtimes) > 0 {
		runtimesTyped, err := parseRuntimesConfig(config.Runtimes)
		if err == nil {
			config.RuntimesTyped = runtimesTyped
			frontmatterTypesLog.Printf("Parsed typed runtimes config with %d runtimes", countRuntimes(runtimesTyped))
		}
	}

	// Parse typed Permissions field if permissions exist
	if len(config.Permissions) > 0 {
		permissionsTyped, err := parsePermissionsConfig(config.Permissions)
		if err == nil {
			config.PermissionsTyped = permissionsTyped
			frontmatterTypesLog.Print("Parsed typed permissions config")
		}
	}

	// permissions.contents: none (or the "none" shorthand) signals that the workflow
	// does not need its own repository content, so the automatic workflow-repository
	// checkout (and "Checkout PR branch" step) should be skipped. Other explicitly
	// configured checkout entries (e.g. a target-only sidecar checkout of a different
	// repository) are unaffected and are still checked out normally.
	if checkoutSkipDefaultFromPermissions(frontmatter["permissions"]) {
		config.CheckoutSkipDefault = true
		frontmatterTypesLog.Print("Skipping default checkout: permissions.contents is none")
	}

	// Parse checkout field - supports single object, array of objects, or false to disable
	if config.Checkout != nil {
		if checkoutValue, ok := config.Checkout.(bool); ok && !checkoutValue {
			config.CheckoutDisabled = true
			config.CheckoutExplicitlyDisabled = true
			frontmatterTypesLog.Print("Checkout disabled via checkout: false")
		} else {
			checkoutConfigs, err := ParseCheckoutConfigs(config.Checkout)
			if err == nil {
				config.CheckoutConfigs = checkoutConfigs
				frontmatterTypesLog.Printf("Parsed checkout config: %d entries", len(checkoutConfigs))
			}
		}
	}

	// Parse typed on.needs field if on exists
	if len(config.On) > 0 {
		onNeeds, err := parseOnNeedsConfig(config.On)
		if err == nil {
			config.OnNeeds = onNeeds
			frontmatterTypesLog.Printf("Parsed typed on.needs config with %d entries", len(onNeeds))
		}
	}

	// Parse typed on.stop-after field if on exists. Parse errors (e.g. wrong type) are
	// intentionally not fatal here: extractStopAfterFromOn re-validates the raw value
	// and returns the actual compile error at the point stop-after is consumed.
	if len(config.On) > 0 {
		stopAfter, err := parseOnStopAfterValue(config.On)
		if err == nil {
			config.OnStopAfter = stopAfter
			frontmatterTypesLog.Printf("Parsed typed on.stop-after config: %q", stopAfter)
		}
	}

	// Populate typed ExperimentConfigs from the raw frontmatter map so that both the
	// legacy bare-array form and the new object form are available as ExperimentConfig
	// structs without callers needing to type-assert config.Experiments entries.
	config.ExperimentConfigs = extractExperimentConfigsFromFrontmatter(frontmatter)
	config.ModelPolicyAllowed, config.ModelPolicyBlocked = extractModelPolicyFromFrontmatter(frontmatter)
	if rawSkills, ok := frontmatter["skills"].([]any); ok {
		config.SkillReferences = parseRawSkillReferences(rawSkills)
	}
	if rawPlugins, ok := frontmatter["plugins"].([]any); ok {
		config.PluginReferences = parseRawPluginReferences(rawPlugins)
	}
	if ambientFolders, err := extractAmbientFolders(frontmatter); err != nil {
		return nil, err
	} else if ambientFolders != nil {
		config.AmbientFolders, err = normalizeAmbientFolders(ambientFolders)
		if err != nil {
			return nil, err
		}
	}
	config.GitHubApp = githubApp

	frontmatterTypesLog.Printf("Successfully parsed frontmatter config: name=%s, engine=%v", config.Name, config.Engine)
	return &config, nil
}

func extractModelPolicyFromFrontmatter(frontmatter map[string]any) ([]string, []string) {
	modelsRaw, ok := frontmatter["models"].(map[string]any)
	if !ok {
		return nil, nil
	}
	return parseModelPolicyList(modelsRaw["allowed"]), parseModelPolicyList(modelsRaw["blocked"])
}

func parseModelPolicyList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if value != nil {
			frontmatterTypesLog.Printf("Skipping model policy list: expected array, got %T", value)
		}
		return nil
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			frontmatterTypesLog.Printf("Skipping model policy entry: expected string, got %T", v)
			continue
		}
		if s == "" {
			frontmatterTypesLog.Printf("Skipping model policy entry: empty string")
			continue
		}
		result = append(result, s)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseOnNeedsConfig(on map[string]any) ([]string, error) {
	return parseOnNeedsValues(on)
}

// parseRuntimesConfig converts a map[string]any to RuntimesConfig
func parseRuntimesConfig(runtimes map[string]any) (*RuntimesConfig, error) {
	config := &RuntimesConfig{}

	for runtimeID, configAny := range runtimes {
		configMap, ok := configAny.(map[string]any)
		if !ok {
			frontmatterTypesLog.Printf("Skipping runtime '%s': expected map, got %T", runtimeID, configAny)
			continue
		}

		// Extract version (optional)
		var version string
		if versionAny, hasVersion := configMap["version"]; hasVersion {
			// Convert version to string
			switch v := versionAny.(type) {
			case string:
				version = v
			case int:
				version = strconv.Itoa(v)
			case float64:
				if v == float64(int(v)) {
					version = strconv.Itoa(int(v))
				} else {
					version = fmt.Sprintf("%g", v)
				}
			default:
				continue
			}
		}

		// Extract if condition (optional)
		var ifCondition string
		if ifAny, hasIf := configMap["if"]; hasIf {
			if ifStr, ok := ifAny.(string); ok {
				ifCondition = ifStr
			}
		}

		// Extract action-repo and action-version overrides (optional)
		actionRepo, _ := configMap["action-repo"].(string)
		actionVersion, _ := configMap["action-version"].(string)

		// Extract run-install-scripts flag (optional)
		var runInstallScripts *bool
		if rsAny, hasRS := configMap["run-install-scripts"]; hasRS {
			if rsBool, ok := rsAny.(bool); ok {
				runInstallScripts = &rsBool
			}
		}

		// Extract cooldown flag (optional, default true when omitted)
		var cooldown *bool
		if cooldownAny, hasCooldown := configMap["cooldown"]; hasCooldown {
			if cooldownBool, ok := cooldownAny.(bool); ok {
				cooldown = &cooldownBool
			}
		}

		// Create runtime config with all fields
		runtimeConfig := &RuntimeConfig{
			Version:           version,
			If:                ifCondition,
			ActionRepo:        actionRepo,
			ActionVersion:     actionVersion,
			Cooldown:          cooldown,
			RunInstallScripts: runInstallScripts,
		}

		// Map to specific runtime field
		switch runtimeID {
		case "node":
			config.Node = runtimeConfig
		case "python":
			config.Python = runtimeConfig
		case "go":
			config.Go = runtimeConfig
		case "uv":
			config.UV = runtimeConfig
		case "bun":
			config.Bun = runtimeConfig
		case "deno":
			config.Deno = runtimeConfig
		case "dotnet":
			config.Dotnet = runtimeConfig
		case "elixir":
			config.Elixir = runtimeConfig
		case "gh-aw":
			config.GhAw = runtimeConfig
		case "haskell":
			config.Haskell = runtimeConfig
		case "java":
			config.Java = runtimeConfig
		case "ruby":
			config.Ruby = runtimeConfig
		}
	}

	return config, nil
}

// parsePermissionsConfig converts a map[string]any to PermissionsConfig
func parsePermissionsConfig(permissions map[string]any) (*PermissionsConfig, error) {
	config := &PermissionsConfig{}

	// Check if it's a shorthand permission (single string value)
	if len(permissions) == 1 {
		for key, value := range permissions {
			if strValue, ok := value.(string); ok {
				shorthandPerms := []string{"read-all", "write-all", "read", "write", "none"}
				for _, shorthand := range shorthandPerms {
					if key == shorthand || strValue == shorthand {
						config.Shorthand = shorthand
						return config, nil
					}
				}
			}
		}
	}

	// Parse detailed permissions
	for scope, level := range permissions {
		if levelStr, ok := level.(string); ok {
			switch scope {
			// GitHub Actions permission scopes
			case "actions":
				config.Actions = levelStr
			case "checks":
				config.Checks = levelStr
			case "code-quality":
				config.CodeQuality = levelStr
			case "contents":
				config.Contents = levelStr
			case "deployments":
				config.Deployments = levelStr
			case "id-token":
				config.IDToken = levelStr
			case "issues":
				config.Issues = levelStr
			case "discussions":
				config.Discussions = levelStr
			case "drives":
				config.Drives = levelStr
			case "packages":
				config.Packages = levelStr
			case "pages":
				config.Pages = levelStr
			case "pull-requests":
				config.PullRequests = levelStr
			case "repository-projects":
				config.RepositoryProjects = levelStr
			case "security-events":
				config.SecurityEvents = levelStr
			case "statuses":
				config.Statuses = levelStr
			case "vulnerability-alerts":
				config.VulnerabilityAlerts = levelStr
			case "organization-projects":
				config.OrganizationProjects = levelStr
			// GitHub App-only permission scopes
			case "administration":
				config.Administration = levelStr
			case "environments":
				config.Environments = levelStr
			case "git-signing":
				config.GitSigning = levelStr
			case "workflows":
				config.Workflows = levelStr
			case "repository-hooks":
				config.RepositoryHooks = levelStr
			case "single-file":
				config.SingleFile = levelStr
			case "codespaces":
				config.Codespaces = levelStr
			case "repository-custom-properties":
				config.RepositoryCustomProperties = levelStr
			case "members":
				config.Members = levelStr
			case "organization-administration":
				config.OrganizationAdministration = levelStr
			case "team-discussions":
				config.TeamDiscussions = levelStr
			case "organization-hooks":
				config.OrganizationHooks = levelStr
			case "organization-members":
				config.OrganizationMembers = levelStr
			case "organization-packages":
				config.OrganizationPackages = levelStr
			case "organization-self-hosted-runners":
				config.OrganizationSelfHostedRunners = levelStr
			case "organization-custom-org-roles":
				config.OrganizationCustomOrgRoles = levelStr
			case "organization-custom-properties":
				config.OrganizationCustomProperties = levelStr
			case "organization-custom-repository-roles":
				config.OrganizationCustomRepositoryRoles = levelStr
			case "organization-announcement-banners":
				config.OrganizationAnnouncementBanners = levelStr
			case "organization-events":
				config.OrganizationEvents = levelStr
			case "organization-plan":
				config.OrganizationPlan = levelStr
			case "organization-user-blocking":
				config.OrganizationUserBlocking = levelStr
			case "organization-personal-access-token-requests":
				config.OrganizationPersonalAccessTokenReqs = levelStr
			case "organization-personal-access-tokens":
				config.OrganizationPersonalAccessTokens = levelStr
			case "organization-copilot":
				config.OrganizationCopilot = levelStr
			case "organization-codespaces":
				config.OrganizationCodespaces = levelStr
			case "email-addresses":
				config.EmailAddresses = levelStr
			case "codespaces-lifecycle-admin":
				config.CodespacesLifecycleAdmin = levelStr
			case "codespaces-metadata":
				config.CodespacesMetadata = levelStr
			}
		}
	}

	return config, nil
}
