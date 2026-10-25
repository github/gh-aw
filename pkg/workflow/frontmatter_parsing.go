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
	githubApp, err := extractTypedTopLevelGitHubApp(frontmatter)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeFrontmatterConfig(frontmatter)
	if err != nil {
		return nil, err
	}
	config, err := decodeFrontmatterConfig(normalized)
	if err != nil {
		return nil, err
	}
	if err := validateParsedFrontmatterConfig(config, frontmatter); err != nil {
		return nil, err
	}
	parseTypedFrontmatterSections(config, frontmatter)
	if err := parseFrontmatterCheckout(config); err != nil {
		return nil, err
	}
	parseFrontmatterTriggers(config)
	if err := parseFrontmatterReferences(config, frontmatter); err != nil {
		return nil, err
	}
	config.GitHubApp = githubApp
	frontmatterTypesLog.Printf("Successfully parsed frontmatter config: name=%s, engine=%v", config.Name, config.Engine)
	return config, nil
}

func decodeFrontmatterConfig(frontmatter map[string]any) (*FrontmatterConfig, error) {
	// Use JSON marshaling so TemplatableInt32 accepts integers and GitHub Actions expressions.
	jsonBytes, err := json.Marshal(frontmatter)
	if err != nil {
		frontmatterTypesLog.Printf("Failed to marshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to marshal frontmatter to JSON: %w", err)
	}
	var config FrontmatterConfig
	type alias FrontmatterConfig
	// Credit limits have a broader scalar syntax than ordinary templatable ints.
	aux := struct {
		MaxAICredits      json.RawMessage `json:"max-ai-credits"`
		MaxDailyAICredits json.RawMessage `json:"max-daily-ai-credits"`
		*alias
	}{alias: (*alias)(&config)}
	if err := json.Unmarshal(jsonBytes, &aux); err != nil {
		frontmatterTypesLog.Printf("Failed to unmarshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to unmarshal frontmatter into config: %w", err)
	}
	config.MaxAICredits, err = parseFrontmatterAICLimit(aux.MaxAICredits, false)
	if err != nil {
		return nil, fmt.Errorf("invalid max-ai-credits: %w", err)
	}
	config.MaxDailyAICredits, err = parseFrontmatterAICLimit(aux.MaxDailyAICredits, true)
	if err != nil {
		return nil, fmt.Errorf("invalid max-daily-ai-credits: %w", err)
	}
	return &config, nil
}

func validateParsedFrontmatterConfig(config *FrontmatterConfig, frontmatter map[string]any) error {
	if err := validateFrontmatterCacheShape(config.Cache); err != nil {
		return err
	}
	if err := validateThreatDetectionSuppressions(config.ThreatDetectionSuppressions); err != nil {
		return err
	}
	if err := validateRunsOnValue(config.RunsOn); err != nil {
		return err
	}
	if err := validateRunsOnValue(config.RunsOnSlim); err != nil {
		return err
	}
	return validateSafeOutputRunsOn(frontmatter["safe-outputs"])
}

func validateFrontmatterCacheShape(cache any) error {
	switch value := cache.(type) {
	case nil, map[string]any:
		return nil
	case []any:
		for i, entry := range value {
			if _, ok := entry.(map[string]any); !ok {
				return fmt.Errorf("invalid cache entry at index %d: expected a cache object, got %T", i, entry)
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid cache: expected a cache object or array of cache objects, got %T", cache)
	}
}

func validateSafeOutputRunsOn(value any) error {
	safeOutputs, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if err := validateRunsOnValue(safeOutputs["runs-on"]); err != nil {
		return err
	}
	if threat, ok := safeOutputs["threat-detection"].(map[string]any); ok {
		if err := validateRunsOnValue(threat["runs-on"]); err != nil {
			return err
		}
	}
	if jobs, ok := safeOutputs["jobs"].(map[string]any); ok {
		for _, value := range jobs {
			if job, ok := value.(map[string]any); ok {
				if err := validateRunsOnValue(job["runs-on"]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func parseTypedFrontmatterSections(config *FrontmatterConfig, frontmatter map[string]any) {
	if len(config.Runtimes) > 0 {
		if runtimes, err := parseRuntimesConfig(config.Runtimes); err == nil {
			config.RuntimesTyped = runtimes
			frontmatterTypesLog.Printf("Parsed typed runtimes config with %d runtimes", countRuntimes(runtimes))
		}
	}
	if len(config.Permissions) > 0 {
		if permissions, err := parsePermissionsConfig(config.Permissions); err == nil {
			config.PermissionsTyped = permissions
			frontmatterTypesLog.Print("Parsed typed permissions config")
		}
	}
	if checkoutSkipDefaultFromPermissions(frontmatter["permissions"]) {
		config.CheckoutSkipDefault = true
		frontmatterTypesLog.Print("Skipping default checkout: permissions.contents is none")
	}
}

func parseFrontmatterCheckout(config *FrontmatterConfig) error {
	if checkoutValue, ok := config.Checkout.(bool); ok && !checkoutValue {
		config.CheckoutDisabled = true
		config.CheckoutExplicitlyDisabled = true
		frontmatterTypesLog.Print("Checkout disabled via checkout: false")
		return nil
	}
	if config.Checkout == nil {
		return nil
	}
	checkoutConfigs, err := ParseCheckoutConfigs(config.Checkout)
	if err != nil {
		return fmt.Errorf("invalid checkout configuration: %w", err)
	}
	config.CheckoutConfigs = checkoutConfigs
	frontmatterTypesLog.Printf("Parsed checkout config: %d entries", len(checkoutConfigs))
	return nil
}

func parseFrontmatterTriggers(config *FrontmatterConfig) {
	if len(config.On) == 0 {
		return
	}
	if onNeeds, err := parseOnNeedsConfig(config.On); err == nil {
		config.OnNeeds = onNeeds
		frontmatterTypesLog.Printf("Parsed typed on.needs config with %d entries", len(onNeeds))
	}
	// Invalid stop-after values are revalidated by extractStopAfterFromOn where consumed.
	if stopAfter, err := parseOnStopAfterValue(config.On); err == nil {
		config.OnStopAfter = stopAfter
		frontmatterTypesLog.Printf("Parsed typed on.stop-after config: %q", stopAfter)
	}
}

func parseFrontmatterReferences(config *FrontmatterConfig, frontmatter map[string]any) error {
	config.ExperimentConfigs = extractExperimentConfigsFromFrontmatter(frontmatter)
	config.ModelPolicyAllowed, config.ModelPolicyBlocked = extractModelPolicyFromFrontmatter(frontmatter)
	if rawSkills, ok := frontmatter["skills"].([]any); ok {
		config.SkillReferences = parseRawSkillReferences(rawSkills)
	}
	if rawPlugins, ok := frontmatter["plugins"].([]any); ok {
		config.PluginReferences = parseRawPluginReferences(rawPlugins)
	}
	ambientFolders, err := extractAmbientFolders(frontmatter)
	if err != nil {
		return err
	}
	if ambientFolders != nil {
		config.AmbientFolders, err = normalizeAmbientFolders(ambientFolders)
	}
	return err
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
		if runtimeConfig := parseRuntimeConfig(configMap); runtimeConfig != nil {
			setRuntimeConfig(config, runtimeID, runtimeConfig)
		}
	}
	return config, nil
}

func parseRuntimeConfig(config map[string]any) *RuntimeConfig {
	version, valid := parseRuntimeVersion(config["version"])
	if _, exists := config["version"]; exists && !valid {
		return nil
	}
	return &RuntimeConfig{
		Version: version, If: stringFromMap(config, "if"),
		ActionRepo:        stringFromMap(config, "action-repo"),
		ActionVersion:     stringFromMap(config, "action-version"),
		Cooldown:          boolPointerFromMap(config, "cooldown"),
		RunInstallScripts: boolPointerFromMap(config, "run-install-scripts"),
	}
}

func parseRuntimeVersion(value any) (string, bool) {
	switch value := value.(type) {
	case nil:
		return "", true
	case string:
		return value, true
	case int:
		return strconv.Itoa(value), true
	case float64:
		if value == float64(int(value)) {
			return strconv.Itoa(int(value)), true
		}
		return fmt.Sprintf("%g", value), true
	default:
		return "", false
	}
}

func stringFromMap(config map[string]any, key string) string {
	value, ok := config[key].(string)
	if ok {
		return value
	}
	return ""
}

func boolPointerFromMap(config map[string]any, key string) *bool {
	value, ok := config[key].(bool)
	if !ok {
		return nil
	}
	return &value
}

func setRuntimeConfig(config *RuntimesConfig, runtimeID string, runtime *RuntimeConfig) {
	switch runtimeID {
	case "node":
		config.Node = runtime
	case "python":
		config.Python = runtime
	case "go":
		config.Go = runtime
	case "uv":
		config.UV = runtime
	case "bun":
		config.Bun = runtime
	case "deno":
		config.Deno = runtime
	case "dotnet":
		config.Dotnet = runtime
	case "elixir":
		config.Elixir = runtime
	case "gh-aw":
		config.GhAw = runtime
	case "haskell":
		config.Haskell = runtime
	case "java":
		config.Java = runtime
	case "ruby":
		config.Ruby = runtime
	}
}

// parsePermissionsConfig converts a map[string]any to PermissionsConfig
func parsePermissionsConfig(permissions map[string]any) (*PermissionsConfig, error) {
	config := &PermissionsConfig{}
	if shorthand := parsePermissionShorthand(permissions); shorthand != "" {
		config.Shorthand = shorthand
		return config, nil
	}
	for scope, level := range permissions {
		levelValue, ok := level.(string)
		if !ok {
			continue
		}
		setActionsPermission(config, scope, levelValue)
		setGitHubAppPermission(config, scope, levelValue)
	}
	return config, nil
}

func parsePermissionShorthand(permissions map[string]any) string {
	if len(permissions) != 1 {
		return ""
	}
	for key, value := range permissions {
		level, ok := value.(string)
		if !ok {
			return ""
		}
		for _, shorthand := range []string{"read-all", "write-all", "read", "write", "none"} {
			if key == shorthand || level == shorthand {
				return shorthand
			}
		}
	}
	return ""
}

func setActionsPermission(config *PermissionsConfig, scope, level string) {
	switch scope {
	case "actions":
		config.Actions = level
	case "checks":
		config.Checks = level
	case "code-quality":
		config.CodeQuality = level
	case "contents":
		config.Contents = level
	case "deployments":
		config.Deployments = level
	case "id-token":
		config.IDToken = level
	case "issues":
		config.Issues = level
	case "discussions":
		config.Discussions = level
	case "drives":
		config.Drives = level
	case "packages":
		config.Packages = level
	case "pages":
		config.Pages = level
	case "pull-requests":
		config.PullRequests = level
	case "repository-projects":
		config.RepositoryProjects = level
	case "security-events":
		config.SecurityEvents = level
	case "statuses":
		config.Statuses = level
	case "vulnerability-alerts":
		config.VulnerabilityAlerts = level
	case "organization-projects":
		config.OrganizationProjects = level
	}
}

func setGitHubAppPermission(config *PermissionsConfig, scope, level string) {
	if setRepositoryAppPermission(config, scope, level) ||
		setOrganizationAppPermission(config, scope, level) {
		return
	}
	switch scope {
	case "email-addresses":
		config.EmailAddresses = level
	case "codespaces-lifecycle-admin":
		config.CodespacesLifecycleAdmin = level
	case "codespaces-metadata":
		config.CodespacesMetadata = level
	}
}

func setRepositoryAppPermission(config *PermissionsConfig, scope, level string) bool {
	switch scope {
	case "administration":
		config.Administration = level
	case "environments":
		config.Environments = level
	case "git-signing":
		config.GitSigning = level
	case "workflows":
		config.Workflows = level
	case "repository-hooks":
		config.RepositoryHooks = level
	case "single-file":
		config.SingleFile = level
	case "codespaces":
		config.Codespaces = level
	case "repository-custom-properties":
		config.RepositoryCustomProperties = level
	default:
		return false
	}
	return true
}

func setOrganizationAppPermission(config *PermissionsConfig, scope, level string) bool {
	switch scope {
	case "members":
		config.Members = level
	case "organization-administration":
		config.OrganizationAdministration = level
	case "team-discussions":
		config.TeamDiscussions = level
	case "organization-hooks":
		config.OrganizationHooks = level
	case "organization-members":
		config.OrganizationMembers = level
	case "organization-packages":
		config.OrganizationPackages = level
	case "organization-self-hosted-runners":
		config.OrganizationSelfHostedRunners = level
	case "organization-custom-org-roles":
		config.OrganizationCustomOrgRoles = level
	case "organization-custom-properties":
		config.OrganizationCustomProperties = level
	case "organization-custom-repository-roles":
		config.OrganizationCustomRepositoryRoles = level
	case "organization-announcement-banners":
		config.OrganizationAnnouncementBanners = level
	case "organization-events":
		config.OrganizationEvents = level
	case "organization-plan":
		config.OrganizationPlan = level
	case "organization-user-blocking":
		config.OrganizationUserBlocking = level
	case "organization-personal-access-token-requests":
		config.OrganizationPersonalAccessTokenReqs = level
	case "organization-personal-access-tokens":
		config.OrganizationPersonalAccessTokens = level
	case "organization-copilot":
		config.OrganizationCopilot = level
	case "organization-codespaces":
		config.OrganizationCodespaces = level
	default:
		return false
	}
	return true
}
