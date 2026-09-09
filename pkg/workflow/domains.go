package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/typeutil"
)

var domainsLog = logger.New("workflow:domains")

//go:embed data/ecosystem_domains.json
var domainSetsJSON []byte

type domainSets struct {
	Ecosystems           map[string][]string `json:"ecosystems"`
	EngineDefaults       map[string][]string `json:"engine-defaults"`
	PiProviderDomains    map[string]string   `json:"pi-provider-domains"`
	SanitizationDefaults []string            `json:"sanitization-defaults"`
}

var loadDomainSets = sync.OnceValues(func() (domainSets, error) {
	domainsLog.Print("Loading domain sets from embedded JSON")

	var sets domainSets
	if err := json.Unmarshal(domainSetsJSON, &sets); err != nil {
		return domainSets{}, fmt.Errorf("failed to load domain sets from JSON: %w", err)
	}

	// Pre-sort all domain lists once so lookup functions only need to copy, not sort.
	for key := range sets.Ecosystems {
		sort.Strings(sets.Ecosystems[key])
	}
	for key := range sets.EngineDefaults {
		sort.Strings(sets.EngineDefaults[key])
	}
	sort.Strings(sets.SanitizationDefaults)

	domainsLog.Printf("Loaded %d ecosystem categories and %d engine default domain sets", len(sets.Ecosystems), len(sets.EngineDefaults))
	return sets, nil
})

func getLoadedDomainSets() domainSets {
	sets, err := loadDomainSets()
	if err != nil {
		domainsLog.Printf("Failed to load domain sets: %v", err)
		return domainSets{}
	}
	return sets
}

func getLoadedEcosystemDomains() map[string][]string {
	sets := getLoadedDomainSets()
	domains := make(map[string][]string, typeutil.SafeAllocationCapacity(len(sets.Ecosystems), len(sets.EngineDefaults)))
	maps.Copy(domains, sets.Ecosystems)
	maps.Copy(domains, sets.EngineDefaults)
	return domains
}

// Runtime engine default domain lists intentionally exclude package registries (npm, PyPI, ...).
//
// Engine CLIs and SDKs are installed by dedicated GitHub Actions steps that run on the
// runner *before* the AWF-wrapped agent step, so package registries are not needed inside
// the sandbox for installation. Likewise, containerized stdio MCP servers (`npx`, `uvx`)
// are launched by the MCP gateway on the Docker bridge network, outside the agent's
// firewall namespace.
//
// Allowing registries by default would let an agent reach npm/PyPI even when the workflow
// declares `network: {}` or `network: { allowed: [defaults, github] }`, contradicting the
// documented behavior that package ecosystems require explicit opt-in
// (`network: { allowed: [node] }`, `[python]`, or a matching `runtimes:` entry).
//
// This invariant is enforced for runtime engine defaults by
// TestEngineDefaultDomainsDoNotOverlapEcosystems in domains_package_registry_test.go, which
// fails if those default domain lists overlap with the full "node" or "python" ecosystem
// domain sets in data/ecosystem_domains.json — not just the registries known when this comment
// was written. If you need to add a package-registry domain to a runtime engine default and
// that test starts failing, the domain belongs behind an explicit ecosystem/runtime opt-in
// instead, not in the unconditional default list. Copilot threat detection is the exception:
// its dedicated detection allow-list includes registry.npmjs.org for read-only lockfile
// validation, and that list is not part of normal agent engine defaults.

// engineDefaultDomainSets centralizes every engine allow-list. These sets are
// user-selectable domain lists: workflows opt into them through network.allowed
// the same way they opt into ecosystem domain lists.
//
// Runtime engine default domain lists intentionally exclude package registries
// (npm, PyPI, and similar). The threat-detection set is the exception because
// Copilot threat detection uses the npm registry. See the package-registry
// invariant above.
var engineDefaultDomainSets = getLoadedDomainSets().EngineDefaults

// GetEngineDefaultDomainSets returns copies of the named engine domain sets for
// analysis, reporting, and network.allowed expansion.
func GetEngineDefaultDomainSets() map[string][]string {
	sets := make(map[string][]string, len(engineDefaultDomainSets))
	for name, domains := range engineDefaultDomainSets {
		sets[name] = copyEngineDefaultDomainSet(domains)
	}
	return sets
}

func copyEngineDefaultDomainSet(domains []string) []string {
	return append([]string(nil), domains...)
}

// CopilotDefaultDomains are the default domains required for GitHub Copilot CLI authentication and operation.
//
// This list is limited to the shared gateway/GitHub transport baseline: the MCP/API gateway
// (host.docker.internal), the GitHub API and web hosts, and the Copilot routing hub used for
// inference. Plan-specific Copilot API hosts (business/enterprise/individual) and Copilot
// telemetry are *not* part of the default set: agents route inference through the AWF api-proxy,
// so those vendor hosts require an explicit `network: { allowed: [copilot-vendor] }` opt-in.
var CopilotDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["copilot"])

// CodexDefaultDomains are the minimal default domains required for Codex CLI operation.
var CodexDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["codex"])

// ClaudeDefaultDomains are the default domains required for Claude Code CLI authentication and operation.
var ClaudeDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["claude"])

// GeminiDefaultDomains are the default domains required for Google Gemini CLI authentication and operation.
var GeminiDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["gemini"])

// PiBaseDefaultDomains are the base domains required for the Pi CLI to operate,
// independent of the chosen LLM provider. When a model uses provider/model format,
// provider-specific API domains are added on top via GetDefaultDomainsForEngine.
var PiBaseDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["pi-base"])

// piProviderDomains maps provider prefixes to their API domains. It covers the
// same set of providers that Pi can route through via the AWF LLM gateway.
// Note: "google" is intentionally omitted — Pi backend resolution only supports
// copilot, anthropic, openai, and codex; adding google here without backend
// support would produce an inconsistent routing configuration.
var piProviderDomains = getLoadedDomainSets().PiProviderDomains

// PiDefaultDomains are the static default domains for backward compatibility when
// no model provider prefix is given. When a provider/model format is used, the
// dynamic path (GetDefaultDomainsForEngine) resolves provider-specific domains instead.
var PiDefaultDomains = copyEngineDefaultDomainSet(engineDefaultDomainSets["pi"])

// extractProviderFromModel parses "provider/model" format and returns the
// lowercase provider prefix. Returns ("", nil) when no model is given or the
// format contains no slash (no provider prefix detected). Returns an error when
// the format is explicitly malformed – a leading slash like "/gpt-4.1" means
// the provider prefix is intentionally empty, which is always invalid.
// Behavior-defined engines and Pi use this same "provider/model" convention.
func extractProviderFromModel(model string) (string, error) {
	if model == "" {
		return "", nil
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) < 2 {
		// No slash: no "provider/model" format; no provider to extract.
		return "", nil
	}
	provider := strings.ToLower(parts[0])
	if provider == "" { //nolint:tolowerequalfold
		return "", fmt.Errorf("invalid engine.model %q: provider prefix is empty; use provider/model format (for example: openai/gpt-4.1, anthropic/claude-sonnet-4)", model)
	}
	return provider, nil
}

// getPiDefaultDomains returns the default domains for Pi based on the model provider.
// It starts with PiBaseDefaultDomains and adds the provider-specific API domain when
// the model uses provider/model format (e.g. "copilot/claude-sonnet-4-20250514").
// When no provider prefix is present the default Copilot API domain is included for
// backward compatibility.
// Returns an error if the model string is malformed (e.g. a leading slash).
func getPiDefaultDomains(model string) ([]string, error) {
	provider, err := extractProviderFromModel(model)
	if err != nil {
		return nil, err
	}
	domains := make([]string, 0, typeutil.SafeAllocationCapacity(len(PiBaseDefaultDomains), 1))
	domains = append(domains, PiBaseDefaultDomains...)

	if domain, ok := piProviderDomains[provider]; ok {
		domains = append(domains, domain)
	} else if provider == "" {
		// No provider prefix → default to Copilot routing for backward compatibility.
		domains = append(domains, piProviderDomains["copilot"])
	}

	return domains, nil
}

// compoundEcosystems defines ecosystem identifiers that expand to the union of multiple
// component ecosystems. These are resolved at lookup time, so they stay in sync with
// any future changes to the component ecosystems.
var compoundEcosystems = map[string][]string{
	// default-safe-outputs: the recommended baseline for URL redaction in safe-outputs.
	// Covers common infrastructure certificate/OCSP hosts (via "defaults"), popular
	// developer-tool and CI/CD service domains (via "dev-tools"), GitHub domains (via "github"),
	// and loopback/localhost addresses (via "local").
	"default-safe-outputs": {"defaults", "dev-tools", "github", "local"},
}

// getEcosystemDomains returns the domains for a given ecosystem category.
// Supports compound ecosystem identifiers (see compoundEcosystems).
// The returned list is sorted and contains unique entries.
func getEcosystemDomains(category string) []string {
	// Check for compound ecosystem first
	if components, ok := compoundEcosystems[category]; ok {
		domainMap := make(map[string]struct{})
		for _, component := range components {
			for _, d := range getEcosystemDomains(component) {
				domainMap[d] = struct{}{}
			}
		}
		result := sliceutil.SortedKeys(domainMap)
		return result
	}

	ecosystemDomains := getLoadedDomainSets().Ecosystems
	domains, exists := ecosystemDomains[category]
	if !exists {
		domains, exists = engineDefaultDomainSets[category]
		if !exists {
			return []string{}
		}
	}
	// Return a copy to avoid external modification. The underlying list is already
	// sorted once at init() time so no per-call sort.Strings is needed.
	result := make([]string, len(domains))
	copy(result, domains)
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
	"gh-aw":   "gh-aw",
	"bun":     "node",   // bun.sh is in the node ecosystem
	"deno":    "node",   // deno.land is in the node ecosystem
	"uv":      "python", // uv is a Python package manager
	"clojure": "clojure",
	"dart":    "dart",
	"elixir":  "elixir",
	"kotlin":  "kotlin",
	"php":     "php",
	"scala":   "scala",
	"swift":   "swift",
	"zig":     "zig",
}

// getDomainsFromRuntimes extracts ecosystem domains based on the specified runtimes
// Returns a deduplicated list of domains for all specified runtimes
func getDomainsFromRuntimes(runtimes map[string]any) []string {
	if len(runtimes) == 0 {
		return []string{}
	}

	domainMap := make(map[string]struct{})

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
				domainMap[d] = struct{}{}
			}
		}
	}

	return sliceutil.SortedKeys(domainMap)
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
//   - "chrome": headless Chrome/Puppeteer browser testing (*.google.com, *.googleapis.com, *.gvt1.com)
//   - "clojure": Clojure/Clojars
//   - "containers": container registries (Docker, GHCR, etc.)
//   - "copilot-vendor": plan-specific Copilot API hosts and Copilot telemetry (opt-in)
//   - "dart": Dart/Flutter ecosystem
//   - "deno": Deno runtime (deno.land, jsr.io, googleapis.deno.dev, fresh.deno.dev)
//   - "dotnet": .NET and NuGet ecosystem
//   - "elixir": Elixir/Hex
//   - "github": GitHub domains (*.githubusercontent.com, github.githubassets.com, etc.)
//   - "github-actions": GitHub Actions blob storage domains
//   - "go": Go ecosystem
//   - "haskell": Haskell ecosystem
//   - "java": Java/Maven/Gradle
//   - "kotlin": Kotlin/JetBrains
//   - "lean": Lean 4/Lake/Reservoir
//   - "linux-distros": Linux distribution package repositories
//   - "node": Node.js/NPM/Yarn
//   - "perl": Perl/CPAN
//   - "php": PHP/Composer
//   - "playwright": Playwright testing framework
//   - "python": Python/PyPI/Conda
//   - "python-native": Python/PyPI/Conda + Rust crates (for packages with native extensions built with pyo3/maturin)
//   - "ruby": Ruby/RubyGems
//   - "rust": Rust/Cargo/Crates
//   - "scala": Scala/SBT
//   - "swift": Swift/CocoaPods
//   - "terraform": HashiCorp/Terraform
//   - "zig": Zig
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
	domainMap := make(map[string]struct{})
	for _, domain := range network.Allowed {
		// Try to get domains for this ecosystem category
		ecosystemDomains := getEcosystemDomains(domain)
		if len(ecosystemDomains) > 0 {
			// This was an ecosystem identifier, expand it
			domainsLog.Printf("Expanded ecosystem '%s' to %d domains", domain, len(ecosystemDomains))
			for _, d := range ecosystemDomains {
				domainMap[d] = struct{}{}
			}
		} else {
			// Add the domain as-is (regular domain name)
			domainMap[domain] = struct{}{}
		}
	}

	return sliceutil.SortedKeys(domainMap)
}

// ecosystemPriority defines the order in which ecosystems are checked by GetDomainEcosystem.
// More specific sub-ecosystems are listed before their parent ecosystems so that domains
// shared between multiple ecosystems resolve deterministically to the most specific one.
// For example, "node-cdns" is listed before "node" so that cdn.jsdelivr.net returns "node-cdns".
// All known ecosystems are enumerated here; any ecosystem not in this list is checked last
// in sorted order (for forward-compatibility with new entries).
var ecosystemPriority = []string{
	"node-cdns", // before "node" — more specific CDN sub-ecosystem
	"rust",      // before "python" — crates.io/index.crates.io/static.crates.io are native Rust domains
	"clojure",
	"containers",
	"copilot-vendor",
	"dart",
	"defaults",
	"dev-tools",
	"deno", // before "node" — deno-specific domains take precedence over the broader node set
	"dotnet",
	"elixir",
	"fonts", // before "chrome" — fonts.googleapis.com is a fonts domain, not a chrome domain
	"github",
	"github-actions",
	"go",
	"haskell",
	"java", // before "chrome" — maven.google.com and dl.google.com are Java domains, not chrome domains
	"chrome",
	"kotlin",
	"latex",
	"lean",
	"linux-distros",
	"local",
	"node",
	"perl",
	"php",
	"playwright",
	"python",
	"python-native", // superset of "python" — adds crates.io for pyo3/maturin native extensions
	"ruby",
	"scala",
	"swift",
	"terraform",
	"zig",
	"default-safe-outputs", // compound: defaults + dev-tools + github + local
}

// GetDomainEcosystem returns the ecosystem identifier for a given domain, or empty string if not found.
// Ecosystems are checked in ecosystemPriority order so that the result is deterministic even when
// a domain appears in multiple ecosystems (e.g. cdn.jsdelivr.net is in both "node" and "node-cdns").
func GetDomainEcosystem(domain string) string {
	checked := make(map[string]struct{}, len(ecosystemPriority))

	// Check ecosystems in priority order first
	for _, ecosystem := range ecosystemPriority {
		checked[ecosystem] = struct{}{}
		domains := getEcosystemDomains(ecosystem)
		for _, ecosystemDomain := range domains {
			if matchesDomain(domain, ecosystemDomain) {
				return ecosystem
			}
		}
	}

	// Fall back to any domain sets not in the priority list, sorted for determinism
	remainingSet := make(map[string]struct{})
	for ecosystem := range getLoadedDomainSets().Ecosystems {
		if _, ok := checked[ecosystem]; !ok {
			remainingSet[ecosystem] = struct{}{}
		}
	}
	for ecosystem := range engineDefaultDomainSets {
		if _, ok := checked[ecosystem]; !ok {
			remainingSet[ecosystem] = struct{}{}
		}
	}
	remaining := make([]string, 0, len(remainingSet))
	for ecosystem := range remainingSet {
		remaining = append(remaining, ecosystem)
	}
	sort.Strings(remaining)
	for _, ecosystem := range remaining {
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

// extractPlaywrightDomains returns Playwright domains when Playwright tool is configured
// Returns a slice of domain names required for Playwright browser downloads
// These domains are needed when Playwright CLI installs browser binaries.
func extractPlaywrightDomains(tools map[string]any) []string {
	if tools == nil {
		return []string{}
	}

	// Check if Playwright tool is configured
	if _, hasPlaywright := tools["playwright"]; hasPlaywright {
		domains := getEcosystemDomains("playwright")
		domainsLog.Printf("Detected Playwright tool, adding %d domains for browser downloads", len(domains))
		return domains
	}

	return []string{}
}

// mergeDomainsWithNetworkToolsAndRuntimes combines explicit base domains with NetworkPermissions, HTTP MCP server domains, and runtime ecosystem domains
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag
func mergeDomainsWithNetworkToolsAndRuntimes(baseDomains []string, network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	domainMap := make(map[string]struct{})

	// Add base domains
	for _, domain := range baseDomains {
		domainMap[domain] = struct{}{}
	}

	// Add NetworkPermissions domains (if specified)
	if network != nil && len(network.Allowed) > 0 {
		// Expand ecosystem identifiers and add individual domains
		expandedDomains := GetAllowedDomains(network)
		for _, domain := range expandedDomains {
			domainMap[domain] = struct{}{}
		}
	}

	// Add HTTP MCP server domains (if tools are specified)
	if tools != nil {
		mcpDomains := extractHTTPMCPDomains(tools)
		for _, domain := range mcpDomains {
			domainMap[domain] = struct{}{}
		}
	}

	// Add Playwright ecosystem domains (if Playwright tool is specified)
	// This ensures browser binaries can be downloaded when Playwright initializes
	if tools != nil {
		playwrightDomains := extractPlaywrightDomains(tools)
		for _, domain := range playwrightDomains {
			domainMap[domain] = struct{}{}
		}
	}

	// Add runtime ecosystem domains (if runtimes are specified)
	if runtimes != nil {
		runtimeDomains := getDomainsFromRuntimes(runtimes)
		for _, domain := range runtimeDomains {
			domainMap[domain] = struct{}{}
		}
	}

	domains := sliceutil.SortedKeys(domainMap)

	// Join with commas for AWF --allow-domains flag
	return strings.Join(domains, ",")
}

// resolveEngineNetworkDomains resolves the default domain list declared by an engine
// definition's behaviors.network block. The declared defaults are always included; the
// provider-specific API domain is appended based on the model's "provider/" prefix
// (falling back to network.default-provider when the model carries no prefix).
// Returns an error if the model string is malformed (e.g. a leading slash).
func resolveEngineNetworkDomains(network *EngineNetworkDefinition, model string) ([]string, error) {
	if network == nil {
		return nil, nil
	}
	provider, err := extractProviderFromModel(model)
	if err != nil {
		return nil, err
	}
	if provider == "" {
		provider = network.DefaultProvider
	}
	domains := make([]string, 0, typeutil.SafeAllocationCapacity(len(network.Defaults), 1))
	domains = append(domains, network.Defaults...)
	if domain, ok := network.ProviderDomains[provider]; ok {
		domains = append(domains, domain)
	}
	return domains, nil
}

// engineDeclaredNetworkDomains returns the declarative default domains for a
// behavior-defined engine registered in the global engine registry, or nil when the
// engine is unknown or declares no behaviors.network block.
func engineDeclaredNetworkDomains(engineID string, model string) ([]string, error) {
	engine, err := GetGlobalEngineRegistry().GetEngine(strings.ToLower(engineID))
	if err != nil {
		return nil, nil
	}
	behaviorEngine, ok := engine.(*BehaviorDefinedEngine)
	if !ok {
		return nil, nil
	}
	behavior := behaviorEngine.behavior()
	if behavior == nil {
		return nil, nil
	}
	return resolveEngineNetworkDomains(behavior.Network, model)
}

// engineDefaultDomains maps each engine to its static default required domains.
// Engines with model-specific defaults (for example, Pi and behavior-defined engines)
// are resolved dynamically instead of being stored directly in this map.
var engineDefaultDomains = map[constants.EngineName][]string{
	constants.CopilotEngine: CopilotDefaultDomains,
	constants.ClaudeEngine:  ClaudeDefaultDomains,
	constants.CodexEngine:   CodexDefaultDomains,
	constants.GeminiEngine:  GeminiDefaultDomains,
}

// GetDefaultDomainsForEngine returns the engine's default required domains.
// Pi and behavior-defined engine domains are model/provider-specific, so they are
// resolved dynamically from the model's provider prefix rather than the static
// engineDefaultDomains map.
// Falls back to an empty default domain list for unknown engines.
// Returns an error if the model string is malformed (e.g. a leading slash).
func GetDefaultDomainsForEngine(engine constants.EngineName, model string) ([]string, error) {
	if engine == constants.PiEngine {
		return getPiDefaultDomains(model)
	}

	if domains, ok := engineDefaultDomains[engine]; ok {
		return domains, nil
	}

	return engineDeclaredNetworkDomains(string(engine), model)
}

// GetAllowedDomainsForEngineWithModel merges NetworkPermissions, HTTP MCP server
// domains, and runtime ecosystem domains. Agent engine domain sets are not added
// automatically; workflows must reference them explicitly in network.allowed.
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's
// --allow-domains flag.
func GetAllowedDomainsForEngineWithModel(engine constants.EngineName, model string, network *NetworkPermissions, tools map[string]any, runtimes map[string]any) (string, error) {
	if _, err := GetDefaultDomainsForEngine(engine, model); err != nil {
		return "", err
	}
	return mergeDomainsWithNetworkToolsAndRuntimes(nil, network, tools, runtimes), nil
}

// mustGetAllowedDomainsForEngineWithModel is like GetAllowedDomainsForEngineWithModel but
// panics if the model is malformed. It is intended for call sites where the model has
// already been validated and an error represents an internal invariant violation (BUG).
func mustGetAllowedDomainsForEngineWithModel(engine constants.EngineName, model string, network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	result, err := GetAllowedDomainsForEngineWithModel(engine, model, network, tools, runtimes)
	if err != nil {
		panic(fmt.Sprintf("BUG: invalid model %q reached domain computation (should have been caught by validation): %v", model, err))
	}
	return result
}

// GetAllowedDomainsForEngine merges NetworkPermissions,
// HTTP MCP server domains, and runtime ecosystem domains.
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag.
func GetAllowedDomainsForEngine(engine constants.EngineName, network *NetworkPermissions, tools map[string]any, runtimes map[string]any) string {
	result, _ := GetAllowedDomainsForEngineWithModel(engine, "", network, tools, runtimes)
	return result
}

// GetThreatDetectionAllowedDomains returns the minimal set of domains allowed for a Copilot
// detection run. The "threat-detection" engine domain set includes the Copilot API endpoints
// needed for read-only threat analysis plus registry.npmjs.org
// for read-only npm package validation (e.g. verifying lockfile integrity hashes). It intentionally
// excludes raw.githubusercontent.com (not needed when MCP servers are disabled and the CLI binary
// is pre-installed). npm registry access is read-only metadata lookup only — installs are not
// permitted during detection runs.
// Any additional user-specified network.allowed entries are merged in (typically empty for detection).
// Returns a deduplicated, sorted, comma-separated string suitable for AWF's --allow-domains flag.
func GetThreatDetectionAllowedDomains(network *NetworkPermissions) string {
	detectionDomains := copyEngineDefaultDomainSet(engineDefaultDomainSets["threat-detection"])
	// Pass nil tools and runtimes: detection runs with no npm/runtime ecosystem, so
	// ecosystem domain expansion is intentionally skipped.
	return mergeDomainsWithNetworkToolsAndRuntimes(detectionDomains, network, nil, nil)
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
	domainMap := make(map[string]struct{})
	for _, domain := range network.Blocked {
		// Try to get domains for this ecosystem category
		ecosystemDomains := getEcosystemDomains(domain)
		if len(ecosystemDomains) > 0 {
			// This was an ecosystem identifier, expand it
			domainsLog.Printf("Expanded ecosystem '%s' to %d domains", domain, len(ecosystemDomains))
			for _, d := range ecosystemDomains {
				domainMap[d] = struct{}{}
			}
		} else {
			// Add the domain as-is (regular domain name)
			domainMap[domain] = struct{}{}
		}
	}

	return sliceutil.SortedKeys(domainMap)
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

// GetAPITargetDomains returns the set of domains to add to the allow-list when engine.api-target is set.
// For a GHES instance with api-target "api.acme.ghe.com", this returns both the API domain
// ("api.acme.ghe.com") and the base hostname ("acme.ghe.com") so that both the GitHub web UI
// and API requests pass through the firewall without manual lock file edits.
// Returns nil for empty apiTarget.
func GetAPITargetDomains(apiTarget string) []string {
	if apiTarget == "" {
		return nil
	}

	domains := []string{apiTarget}

	// Derive the base hostname by stripping the first subdomain label, but only for
	// API-style hostnames that start with "api.".
	// e.g., "api.acme.ghe.com" → "acme.ghe.com"
	// Only add the base hostname if it still looks like a multi-label hostname (contains a dot).
	if strings.HasPrefix(apiTarget, "api.") {
		if idx := strings.Index(apiTarget, "."); idx > 0 {
			baseHost := apiTarget[idx+1:]
			if strings.Contains(baseHost, ".") && baseHost != apiTarget {
				domains = append(domains, baseHost)
			}
		}
	}

	return domains
}

// mergeAPITargetDomains merges the api-target domains into an existing comma-separated domain string.
// When engine.api-target is set, both the API hostname and its base hostname are added to the allow-list.
// Returns the original string unchanged when apiTarget is empty.
func mergeAPITargetDomains(domainsStr string, apiTarget string) string {
	extraDomains := GetAPITargetDomains(apiTarget)
	if len(extraDomains) == 0 {
		return domainsStr
	}

	domainMap := make(map[string]struct{})
	for d := range strings.SplitSeq(domainsStr, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			domainMap[d] = struct{}{}
		}
	}
	for _, d := range extraDomains {
		domainMap[d] = struct{}{}
	}

	return strings.Join(sliceutil.SortedKeys(domainMap), ",")
}

// computeAllowedDomainsForSanitization computes the allowed domains for sanitization
// based on the network configuration, matching what's provided to the firewall.
// The result is cached in data.CachedAllowedDomainsStr after the first call so that
// repeated calls (e.g. from the activation job, safe-outputs steps, and agent run step)
// do not recompute the same domain list.
// Additionally, results are cached on the Compiler keyed by markdown path with the
// current FrontmatterHash so repeated compilations of an unchanged workflow skip the
// full domain computation without unbounded hash-key growth in watch mode.
// Returns an error if the engine's model is malformed (e.g. a leading slash).
func (c *Compiler) computeAllowedDomainsForSanitization(data *WorkflowData) (string, error) {
	// Return cached result if available (engine/network/tools/runtimes do not change during compilation).
	// CachedAllowedDomainsComputed is used as the sentinel so that a legitimately empty domain
	// list is not confused with "not yet computed".
	if data.CachedAllowedDomainsComputed {
		return data.CachedAllowedDomainsStr, nil
	}

	// Check the Compiler-level cache keyed by markdown path.
	// A cached entry is reusable only when the current frontmatter hash matches.
	if c.markdownPath != "" && data.FrontmatterHash != "" {
		if cached, ok := c.allowedDomainsCache[c.markdownPath]; ok && cached.frontmatterHash == data.FrontmatterHash {
			data.CachedAllowedDomainsStr = cached.domains
			data.CachedAllowedDomainsComputed = true
			return cached.domains, nil
		}
	}

	engineID, err := validateDomainEngineModel(data)
	if err != nil {
		return "", err
	}

	// Compute domains from explicit network config, tools, and runtimes to match
	// what's provided to the actual firewall at runtime. Engine domain sets are
	// only included when explicitly referenced in network.allowed.
	base := mergeDomainsWithNetworkToolsAndRuntimes(nil, data.NetworkPermissions, data.Tools, data.Runtimes)

	// Add Copilot BYOK/API target domains so GH_AW_ALLOWED_DOMAINS stays in sync with
	// the runtime firewall allow-list for both standard and BYOK Copilot runs.
	for _, copilotTarget := range GetCopilotAllowlistTargets(data) {
		base = mergeAPITargetDomains(base, copilotTarget)
	}

	// Add Gemini API target domains.
	// Resolved from GEMINI_API_BASE_URL in engine.env or default generativelanguage.googleapis.com.
	if geminiAPITarget := GetGeminiAPITarget(data, engineID); geminiAPITarget != "" {
		base = mergeAPITargetDomains(base, geminiAPITarget)
	}

	// Cache the result for subsequent calls during the same compilation.
	// Set the boolean sentinel first so that an empty result is also treated as cached.
	data.CachedAllowedDomainsComputed = true
	data.CachedAllowedDomainsStr = base

	// Populate the Compiler-level cache so subsequent compilations of the same
	// workflow path and unchanged frontmatter skip this computation entirely.
	if c.markdownPath != "" && data.FrontmatterHash != "" {
		c.allowedDomainsCache[c.markdownPath] = allowedDomain{
			frontmatterHash: data.FrontmatterHash,
			domains:         base,
		}
	}

	return base, nil
}

func validateDomainEngineModel(data *WorkflowData) (string, error) {
	var engineID string
	if data.EngineConfig != nil {
		engineID = data.EngineConfig.ID
	} else if data.AI != "" {
		engineID = data.AI
	}
	if engineID == "" {
		return "", nil
	}
	model := ""
	if data.EngineConfig != nil {
		model = data.Model
	}
	_, err := GetDefaultDomainsForEngine(constants.EngineName(engineID), model)
	return engineID, err
}

// expandAllowedDomains expands a list of domain entries (which may include ecosystem
// identifiers like "python", "node", "dev-tools") into a deduplicated, sorted list of
// concrete domain strings. This uses the same expansion logic as network.allowed.
func expandAllowedDomains(entries []string) []string {
	domainMap := make(map[string]struct{})
	for _, entry := range entries {
		ecosystemDomains := getEcosystemDomains(entry)
		if len(ecosystemDomains) > 0 {
			for _, d := range ecosystemDomains {
				domainMap[d] = struct{}{}
			}
		} else {
			domainMap[entry] = struct{}{}
		}
	}
	return sliceutil.SortedKeys(domainMap)
}

// computeExpandedAllowedDomainsForSanitization computes the allowed domains for URL sanitization,
// unioning the engine/network base set with the safe-outputs.allowed-domains entries.
// It always includes the sanitization defaults in the result.
// The allowed-domains entries support ecosystem identifiers (same syntax as network.allowed).
// Returns an error if the engine's model is malformed (e.g. a leading slash).
func (c *Compiler) computeExpandedAllowedDomainsForSanitization(data *WorkflowData) (string, error) {
	// Start from the base set (network.allowed + tools + runtimes)
	base, err := c.computeAllowedDomainsForSanitization(data)
	if err != nil {
		return "", err
	}

	domainMap := make(map[string]struct{})

	// Seed from the base computation
	if base != "" {
		for d := range strings.SplitSeq(base, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				domainMap[d] = struct{}{}
			}
		}
	}

	// Union with allowed-domains (expanded)
	if data.SafeOutputs != nil && len(data.SafeOutputs.AllowedDomains) > 0 {
		for _, d := range expandAllowedDomains(data.SafeOutputs.AllowedDomains) {
			domainMap[d] = struct{}{}
		}
	}

	for _, domain := range getLoadedDomainSets().SanitizationDefaults {
		domainMap[domain] = struct{}{}
	}

	// Produce a sorted, comma-separated result
	return strings.Join(sliceutil.SortedKeys(domainMap), ","), nil
}
