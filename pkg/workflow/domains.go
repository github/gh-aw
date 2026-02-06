package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var domainsLog = logger.New("workflow:domains")

//go:embed data/ecosystem_domains.json
var ecosystemDomainsJSON []byte

// ecosystemDomains holds the loaded domain data
var ecosystemDomains map[string][]string

// CopilotDefaultDomains are the default domains required for GitHub Copilot CLI authentication and operation
var CopilotDefaultDomains = []string{
	"api.business.githubcopilot.com",
	"api.enterprise.githubcopilot.com",
	"api.github.com",
	"api.githubcopilot.com",
	"api.individual.githubcopilot.com",
	"github.com",
	"host.docker.internal",
	"raw.githubusercontent.com",
	"registry.npmjs.org",
	"telemetry.enterprise.githubcopilot.com",
}

// CodexDefaultDomains are the minimal default domains required for Codex CLI operation
var CodexDefaultDomains = []string{
	"172.30.0.1", // AWF gateway IP - Codex resolves host.docker.internal to this IP for Rust DNS compatibility
	"api.openai.com",
	"host.docker.internal",
	"openai.com",
}

// ClaudeDefaultDomains are the default domains required for Claude Code CLI authentication and operation
var ClaudeDefaultDomains = []string{
	"*.githubusercontent.com",
	"anthropic.com",
	"api.anthropic.com",
	"api.github.com",
	"api.snapcraft.io",
	"archive.ubuntu.com",
	"azure.archive.ubuntu.com",
	"cdn.playwright.dev",
	"codeload.github.com",
	"crl.geotrust.com",
	"crl.globalsign.com",
	"crl.identrust.com",
	"crl.sectigo.com",
	"crl.thawte.com",
	"crl.usertrust.com",
	"crl.verisign.com",
	"crl3.digicert.com",
	"crl4.digicert.com",
	"crls.ssl.com",
	"files.pythonhosted.org",
	"ghcr.io",
	"github-cloud.githubusercontent.com",
	"github-cloud.s3.amazonaws.com",
	"github.com",
	"host.docker.internal",
	"json-schema.org",
	"json.schemastore.org",
	"keyserver.ubuntu.com",
	"lfs.github.com",
	"objects.githubusercontent.com",
	"ocsp.digicert.com",
	"ocsp.geotrust.com",
	"ocsp.globalsign.com",
	"ocsp.identrust.com",
	"ocsp.sectigo.com",
	"ocsp.ssl.com",
	"ocsp.thawte.com",
	"ocsp.usertrust.com",
	"ocsp.verisign.com",
	"packagecloud.io",
	"packages.cloud.google.com",
	"packages.microsoft.com",
	"playwright.download.prss.microsoft.com",
	"ppa.launchpad.net",
	"pypi.org",
	"raw.githubusercontent.com",
	"registry.npmjs.org",
	"s.symcb.com",
	"s.symcd.com",
	"security.ubuntu.com",
	"sentry.io",
	"statsig.anthropic.com",
	"ts-crl.ws.symantec.com",
	"ts-ocsp.ws.symantec.com",
}

// init loads the ecosystem domains from the embedded JSON
func init() {
	domainsLog.Print("Loading ecosystem domains from embedded JSON")

	if err := json.Unmarshal(ecosystemDomainsJSON, &ecosystemDomains); err != nil {
		panic(fmt.Sprintf("failed to load ecosystem domains from JSON: %v", err))
	}

	domainsLog.Printf("Loaded %d ecosystem categories", len(ecosystemDomains))
}

// getEcosystemDomains returns the domains for a given ecosystem category
// The returned list is sorted and contains unique entries
func getEcosystemDomains(category string) []string {
	domains, exists := ecosystemDomains[category]
	if !exists {
		return []string{}
	}
	// Return a sorted copy to avoid external modification
	result := make([]string, len(domains))
	copy(result, domains)
	SortStrings(result)
	return result
}

// runtimeToEcosystem maps runtime IDs to their corresponding ecosystem categories in ecosystem_domains.json
// Some runtimes share ecosystems (e.g., bun and deno use node ecosystem domains)
var runtimeToEcosystem = map[string]string{
	"node":    "node",
	"python":  "python",
	"go":      "go",
	"java":    "java",
	"ruby":    "ruby",
	"dotnet":  "dotnet",
	"haskell": "haskell",
	"bun":     "node",   // bun.sh is in the node ecosystem
	"deno":    "node",   // deno.land is in the node ecosystem
	"uv":      "python", // uv is a Python package manager
}

// getDomainsFromRuntimes extracts ecosystem domains based on the specified runtimes
// Returns a deduplicated list of domains for all specified runtimes
func getDomainsFromRuntimes(runtimes map[string]any) []string {
	if len(runtimes) == 0 {
		return []string{}
	}

	domainMap := make(map[string]bool)

	for runtimeID := range runtimes {
		// Look up the ecosystem for this runtime
		ecosystem, exists := runtimeToEcosystem[runtimeID]
		if !exists {
			domainsLog.Printf("No ecosystem mapping for runtime '%s'", runtimeID)
			continue
		}

		// Get domains for this ecosystem
		domains := getEcosystemDomains(ecosystem)
		if len(domains) > 0 {
			domainsLog.Printf("Runtime '%s' mapped to ecosystem '%s' with %d domains", runtimeID, ecosystem, len(domains))
			for _, d := range domains {
				domainMap[d] = true
			}
		}
	}

	// Convert map to sorted slice
	result := make([]string, 0, len(domainMap))
	for domain := range domainMap {
		result = append(result, domain)
	}
	SortStrings(result)

	return result
}

// GetAllowedDomains returns the allowed domains from network permissions.
//
// # Behavior based on network permissions configuration:
//
//  1. No network permissions (nil):
//     Returns default ecosystem domains for backwards compatibility.
//
//  2. Allowed list with "defaults" only:
//     network: defaults  OR  network: { allowed: [defaults] }
//     Returns default ecosystem domains.
//
//  3. Allowed list with multiple ecosystems:
//     network:
//     allowed:
//     - defaults
//     - github
//     Processes the Allowed list, expanding all ecosystem identifiers and merging them.
//
//  4. Allowed list with custom domains:
//     network:
//     allowed:
//     - example.com
//     - python
//     Processes the Allowed list, expanding ecosystem identifiers.
//
//  5. Empty Allowed list (deny-all):
//     network: {}  OR  network: { allowed: [] }
//     Returns empty slice (no network access).
//
// The returned list is sorted and deduplicated.
//
// # Supported ecosystem identifiers:
//   - "defaults": basic infrastructure (certs, JSON schema, Ubuntu, package mirrors)
//   - "containers": container registries (Docker, GHCR, etc.)
//   - "dotnet": .NET and NuGet ecosystem
//   - "dart": Dart/Flutter ecosystem
//   - "github": GitHub domains (*.githubusercontent.com, github.githubassets.com, etc.)
//   - "go": Go ecosystem
//   - "terraform": HashiCorp/Terraform
//   - "haskell": Haskell ecosystem
//   - "java": Java/Maven/Gradle
//   - "linux-distros": Linux distribution package repositories
//   - "node": Node.js/NPM/Yarn
//   - "perl": Perl/CPAN
//   - "php": PHP/Composer
//   - "playwright": Playwright testing framework
//   - "python": Python/PyPI/Conda
//   - "ruby": Ruby/RubyGems
//   - "rust": Rust/Cargo/Crates
//   - "swift": Swift/CocoaPods
//   - "github-actions": GitHub Actions blob storage domains
func GetAllowedDomains(network *NetworkPermissions) []string {
	if network == nil {
		domainsLog.Print("No network permissions specified, using defaults")
		return getEcosystemDomains("defaults") // Default allow-list for backwards compatibility
	}

	// Handle empty allowed list (deny-all case)
	if len(network.Allowed) == 0 {
		domainsLog.Print("Empty allowed list, denying all network access")
		return []string{} // Return empty slice, not nil
	}

	domainsLog.Printf("Processing %d allowed domains/ecosystems", len(network.Allowed))

	// Process the allowed list, expanding ecosystem identifiers if present
	// Use a map to deduplicate domains
	domainMap := make(map[string]bool)
	for _, domain := range network.Allowed {
		// Try to get domains for this ecosystem category
		ecosystemDomains := getEcosystemDomains(domain)
		if len(ecosystemDomains) > 0 {
			// This was an ecosystem identifier, expand it
			domainsLog.Printf("Expanded ecosystem '%s' to %d domains", domain, len(ecosystemDomains))
			for _, d := range ecosystemDomains {
				domainMap[d] = true
			}
		} else {
			// Add the domain as-is (regular domain name)
			domainMap[domain] = true
		}
	}

	// Convert map to sorted slice
	expandedDomains := make([]string, 0, len(domainMap))
	for domain := range domainMap {
		expandedDomains = append(expandedDomains, domain)
	}
	SortStrings(expandedDomains)

	return expandedDomains
}

// GetDomainEcosystem returns the ecosystem identifier for a given domain, or empty string if not found
func GetDomainEcosystem(domain string) string {
	// Check each ecosystem for domain match
	for ecosystem := range ecosystemDomains {
		domains := getEcosystemDomains(ecosystem)
		for _, ecosystemDomain := range domains {
			if matchesDomain(domain, ecosystemDomain) {
				return ecosystem
			}
		}
	}

	return "" // No ecosystem found
}

// matchesDomain checks if a domain matches a pattern (supports wildcards)
func matchesDomain(domain, pattern string) bool {
	// Exact match
	if domain == pattern {
		return true
	}

	// Wildcard match
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:] // Remove "*."
		return strings.HasSuffix(domain, "."+suffix) || domain == suffix
	}

	return false
}

// extractHTTPMCPDomains extracts domain names from HTTP MCP server URLs in tools configuration
// Returns a slice of domain names (e.g., ["mcp.tavily.com", "api.example.com"])
func extractHTTPMCPDomains(tools map[string]any) []string {
	if tools == nil {
		return []string{}
	}

	domains := []string{}

	// Iterate through tools to find HTTP MCP servers
	for toolName, toolConfig := range tools {
		configMap, ok := toolConfig.(map[string]any)
		if !ok {
			// Tool has no explicit config (e.g., github: null means local mode)
			continue
		}

		// Special handling for GitHub MCP in remote mode
		// When mode: remote is set, the URL is implicitly the hosted GitHub Copilot MCP server
		if toolName == "github" {
			if modeField, hasMode := configMap["mode"]; hasMode {
				if modeStr, ok := modeField.(string); ok && modeStr == "remote" {
					domainsLog.Printf("Detected GitHub MCP remote mode, adding %s to domains", constants.GitHubCopilotMCPDomain)
					domains = append(domains, constants.GitHubCopilotMCPDomain)
					continue
				}
			}
		}

		// Check if this is an HTTP MCP server
		mcpType, hasType := configMap["type"].(string)
		url, hasURL := configMap["url"].(string)

		// HTTP MCP servers have either type: http or just a url field
		isHTTPMCP := (hasType && mcpType == "http") || (!hasType && hasURL)

		if isHTTPMCP && hasURL {
			// Extract domain from URL (e.g., "https://mcp.tavily.com/mcp/" -> "mcp.tavily.com")
			domain := stringutil.ExtractDomainFromURL(url)
			if domain != "" {
				domainsLog.Printf("Extracted HTTP MCP domain '%s' from tool '%s'", domain, toolName)
				domains = append(domains, domain)
			}
		}
	}

	return domains
}

// mergeDomainsWithNetwork combines default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func mergeDomainsWithNetwork(defaultDomains []string, network *NetworkPermissions) string {
	return mergeDomainsWithNetworkAndTools(defaultDomains, network, nil)
}

// mergeDomainsWithNetworkAndTools combines default domains with NetworkPermissions allowed domains and HTTP MCP server domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func mergeDomainsWithNetworkAndTools(defaultDomains []string, network *NetworkPermissions, tools map[string]any) string {
	return mergeDomainsWithNetworkToolsAndRuntimes(defaultDomains, network, tools, nil)
}

// mergeDomainsWithNetworkToolsAndRuntimes combines default domains with NetworkPermissions, HTTP MCP server domains, and runtime ecosystem domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func mergeDomainsWithNetworkToolsAndRuntimes(defaultDomains []string, network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	domainMap := make(map[string]bool)

	// Add default domains
	for _, domain := range defaultDomains {
		domainMap[domain] = true
	}

	// Add NetworkPermissions domains (if specified)
	if network != nil && len(network.Allowed) > 0 {
		// Expand ecosystem identifiers and add individual domains
		expandedDomains := GetAllowedDomains(network)
		for _, domain := range expandedDomains {
			domainMap[domain] = true
		}
	}

	// Add HTTP MCP server domains (if tools are specified)
	if tools != nil {
		mcpDomains := extractHTTPMCPDomains(tools)
		for _, domain := range mcpDomains {
			domainMap[domain] = true
		}
	}

	// Add runtime ecosystem domains (if runtimes are specified)
	if runtimes != nil {
		runtimeDomains := getDomainsFromRuntimes(runtimes)
		for _, domain := range runtimeDomains {
			domainMap[domain] = true
		}
	}

	// Convert to sorted slice for consistent output
	domains := make([]string, 0, len(domainMap))
	for domain := range domainMap {
		domains = append(domains, domain)
	}
	SortStrings(domains)

	// Join with commas for AWF --allow-domains flag
	return strings.Join(domains, ",")
}

// GetCopilotAllowedDomains merges Copilot default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCopilotAllowedDomains(network *NetworkPermissions) string {
	return GetCopilotAllowedDomainsWithSafeInputs(network, false)
}

// GetCopilotAllowedDomainsWithSafeInputs merges Copilot default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
// The hasSafeInputs parameter is maintained for backward compatibility but is no longer used
// since host.docker.internal is now in CopilotDefaultDomains
func GetCopilotAllowedDomainsWithSafeInputs(network *NetworkPermissions, hasSafeInputs bool) string {
	return mergeDomainsWithNetwork(CopilotDefaultDomains, network)
}

// GetCopilotAllowedDomainsWithTools merges Copilot default domains with NetworkPermissions allowed domains and HTTP MCP server domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCopilotAllowedDomainsWithTools(network *NetworkPermissions, tools map[string]any) string {
	return mergeDomainsWithNetworkAndTools(CopilotDefaultDomains, network, tools)
}

// GetCopilotAllowedDomainsWithToolsAndRuntimes merges Copilot default domains with NetworkPermissions, HTTP MCP server domains, and runtime ecosystem domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCopilotAllowedDomainsWithToolsAndRuntimes(network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	return mergeDomainsWithNetworkToolsAndRuntimes(CopilotDefaultDomains, network, tools, runtimes)
}

// GetCodexAllowedDomains merges Codex default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCodexAllowedDomains(network *NetworkPermissions) string {
	return mergeDomainsWithNetwork(CodexDefaultDomains, network)
}

// GetCodexAllowedDomainsWithTools merges Codex default domains with NetworkPermissions allowed domains and HTTP MCP server domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCodexAllowedDomainsWithTools(network *NetworkPermissions, tools map[string]any) string {
	return mergeDomainsWithNetworkAndTools(CodexDefaultDomains, network, tools)
}

// GetCodexAllowedDomainsWithToolsAndRuntimes merges Codex default domains with NetworkPermissions, HTTP MCP server domains, and runtime ecosystem domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetCodexAllowedDomainsWithToolsAndRuntimes(network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	return mergeDomainsWithNetworkToolsAndRuntimes(CodexDefaultDomains, network, tools, runtimes)
}

// GetClaudeAllowedDomains merges Claude default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetClaudeAllowedDomains(network *NetworkPermissions) string {
	return GetClaudeAllowedDomainsWithSafeInputs(network, false)
}

// GetClaudeAllowedDomainsWithSafeInputs merges Claude default domains with NetworkPermissions allowed domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
// The hasSafeInputs parameter is maintained for backward compatibility but is no longer used
// since host.docker.internal is now in ClaudeDefaultDomains
func GetClaudeAllowedDomainsWithSafeInputs(network *NetworkPermissions, hasSafeInputs bool) string {
	return mergeDomainsWithNetwork(ClaudeDefaultDomains, network)
}

// GetClaudeAllowedDomainsWithTools merges Claude default domains with NetworkPermissions allowed domains and HTTP MCP server domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetClaudeAllowedDomainsWithTools(network *NetworkPermissions, tools map[string]any) string {
	return mergeDomainsWithNetworkAndTools(ClaudeDefaultDomains, network, tools)
}

// GetClaudeAllowedDomainsWithToolsAndRuntimes merges Claude default domains with NetworkPermissions, HTTP MCP server domains, and runtime ecosystem domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func GetClaudeAllowedDomainsWithToolsAndRuntimes(network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	return mergeDomainsWithNetworkToolsAndRuntimes(ClaudeDefaultDomains, network, tools, runtimes)
}

// GetBlockedDomains returns the blocked domains from network permissions
// Returns empty slice if no network permissions configured or no domains blocked
// The returned list is sorted and deduplicated
// Supports ecosystem identifiers (same as allowed domains)
func GetBlockedDomains(network *NetworkPermissions) []string {
	if network == nil {
		domainsLog.Print("No network permissions specified, no blocked domains")
		return []string{}
	}

	// Handle empty blocked list
	if len(network.Blocked) == 0 {
		domainsLog.Print("Empty blocked list, no domains blocked")
		return []string{}
	}

	domainsLog.Printf("Processing %d blocked domains/ecosystems", len(network.Blocked))

	// Process the blocked list, expanding ecosystem identifiers if present
	// Use a map to deduplicate domains
	domainMap := make(map[string]bool)
	for _, domain := range network.Blocked {
		// Try to get domains for this ecosystem category
		ecosystemDomains := getEcosystemDomains(domain)
		if len(ecosystemDomains) > 0 {
			// This was an ecosystem identifier, expand it
			domainsLog.Printf("Expanded ecosystem '%s' to %d domains", domain, len(ecosystemDomains))
			for _, d := range ecosystemDomains {
				domainMap[d] = true
			}
		} else {
			// Add the domain as-is (regular domain name)
			domainMap[domain] = true
		}
	}

	// Convert map to sorted slice
	expandedDomains := make([]string, 0, len(domainMap))
	for domain := range domainMap {
		expandedDomains = append(expandedDomains, domain)
	}
	SortStrings(expandedDomains)

	return expandedDomains
}

// formatBlockedDomains formats blocked domains as a comma-separated string suitable for AWF's --block-domains flag
// Returns empty string if no blocked domains
func formatBlockedDomains(network *NetworkPermissions) string {
	if network == nil {
		return ""
	}

	blockedDomains := GetBlockedDomains(network)
	if len(blockedDomains) == 0 {
		return ""
	}

	return strings.Join(blockedDomains, ",")
}

// computeAllowedDomainsForSanitization computes the allowed domains for sanitization
// based on the engine and network configuration, matching what's provided to the firewall
func (c *Compiler) computeAllowedDomainsForSanitization(data *WorkflowData) string {
	// Determine which engine is being used
	var engineID string
	if data.EngineConfig != nil {
		engineID = data.EngineConfig.ID
	} else if data.AI != "" {
		engineID = data.AI
	}

	// Compute domains based on engine type
	// For Copilot with firewall support, use GetCopilotAllowedDomains which merges
	// Copilot defaults with network permissions
	// For Codex with firewall support, use GetCodexAllowedDomains which merges
	// Codex defaults with network permissions
	// For Claude with firewall support, use GetClaudeAllowedDomains which merges
	// Claude defaults with network permissions
	// For other engines, use GetAllowedDomains which uses network permissions only
	switch engineID {
	case "copilot":
		return GetCopilotAllowedDomains(data.NetworkPermissions)
	case "codex":
		return GetCodexAllowedDomains(data.NetworkPermissions)
	case "claude":
		return GetClaudeAllowedDomains(data.NetworkPermissions)
	default:
		// For other engines, use network permissions only
		domains := GetAllowedDomains(data.NetworkPermissions)
		return strings.Join(domains, ",")
	}
}
