// This file provides network firewall validation functions for agentic workflow compilation.
//
// This file contains domain-specific validation functions for network firewall configuration:
//   - validateNetworkFirewallConfig() - Validates firewall configuration dependencies
//   - validateNetworkAllowedDomains() - Validates the allowed domains in network configuration
//   - validateDomainPattern() - Validates a single domain pattern
//   - isEcosystemIdentifier() - Checks if a string is an ecosystem identifier
//
// These validation functions are organized in a dedicated file following the validation
// architecture pattern where domain-specific validation belongs in domain validation files.
// See validation.go for the complete validation architecture documentation.

package workflow

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var networkFirewallValidationLog = logger.New("workflow:network_firewall_validation")

// validateNetworkFirewallConfig validates network firewall configuration dependencies
// Returns an error if the configuration is invalid
func validateNetworkFirewallConfig(networkPermissions *NetworkPermissions) error {
	if networkPermissions == nil {
		return nil
	}

	firewallConfig := networkPermissions.Firewall
	if firewallConfig == nil {
		return nil
	}

	networkFirewallValidationLog.Print("Validating network firewall configuration")

	// Validate allow-urls requires ssl-bump
	if len(firewallConfig.AllowURLs) > 0 && !firewallConfig.SSLBump {
		networkFirewallValidationLog.Printf("Validation error: allow-urls specified without ssl-bump: %d URLs", len(firewallConfig.AllowURLs))
		return NewValidationError(
			"network.firewall.allow-urls",
			"requires ssl-bump: true",
			"allow-urls requires ssl-bump: true to function. SSL Bump enables HTTPS content inspection, which is necessary for URL path filtering",
			"Enable SSL Bump in your firewall configuration:\n\nnetwork:\n  firewall:\n    ssl-bump: true\n    allow-urls:\n      - \"https://github.com/githubnext/*\"\n\nSee: "+string(constants.DocsNetworkURL),
		)
	}

	if len(firewallConfig.AllowURLs) > 0 {
		networkFirewallValidationLog.Printf("Validated allow-urls: %d URLs with ssl-bump enabled", len(firewallConfig.AllowURLs))
	}

	return nil
}

// validateNetworkAllowedDomains validates the allowed domains in network configuration
func (c *Compiler) validateNetworkAllowedDomains(network *NetworkPermissions) error { //nolint:largefunc // Domain validation preserves detailed diagnostics.
	if network == nil || len(network.Allowed) == 0 {
		return nil
	}

	if networkFirewallValidationLog.Enabled() {
		networkFirewallValidationLog.Printf("Validating %d network allowed domains", len(network.Allowed))
	}

	// collector is lazily initialized on the first validation error to avoid a heap
	// allocation on the common path where all domains are valid.
	var collector *ErrorCollector

	for i, domain := range network.Allowed {
		// "*" means allow all traffic - skip validation
		if domain == "*" {
			networkFirewallValidationLog.Print("Skipping allow-all wildcard '*'")
			continue
		}

		// Check if this looks like an ecosystem identifier (single lowercase word with optional hyphens)
		if isEcosystemIdentifier(domain) {
			// Validate it's a known ecosystem identifier using a direct map lookup to avoid allocations
			if isKnownEcosystemIdentifier(domain) {
				if networkFirewallValidationLog.Enabled() {
					networkFirewallValidationLog.Printf("Skipping known ecosystem identifier: %s", domain)
				}
				continue
			}
			// Unknown ecosystem identifier - error
			if networkFirewallValidationLog.Enabled() {
				networkFirewallValidationLog.Printf("Validation error: unknown ecosystem identifier: %s", domain)
			}
			wrappedErr := fmt.Errorf("network.allowed[%d]: %w", i, NewValidationError(
				"network.allowed",
				domain,
				fmt.Sprintf("'%s' is not a valid ecosystem identifier", domain),
				"Use a valid ecosystem identifier or a domain name containing a dot (e.g., 'example.com').\n\nValid ecosystem identifiers: "+strings.Join(getValidEcosystemIdentifiers(), ", "),
			))
			if collector == nil {
				collector = NewErrorCollector(c.failFast)
			}
			if returnErr := collector.Add(wrappedErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
			continue
		}

		if err := validateDomainPattern(domain); err != nil {
			wrappedErr := fmt.Errorf("network.allowed[%d]: %w", i, err)
			if collector == nil {
				collector = NewErrorCollector(c.failFast)
			}
			if returnErr := collector.Add(wrappedErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
		}
	}

	if collector != nil {
		if err := collector.Error(); err != nil {
			if networkFirewallValidationLog.Enabled() {
				networkFirewallValidationLog.Printf("Network allowed domains validation failed: %v", err)
			}
			return err
		}
	}

	networkFirewallValidationLog.Print("Network allowed domains validation passed")
	return nil
}

func (c *Compiler) validateHostedWebPolicy(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.NetworkPermissions == nil || (workflowData.NetworkPermissions.HostedWeb == nil && !workflowData.NetworkPermissions.ExplicitlyDefined) {
		return nil
	}
	if workflowData.NetworkPermissions.InvalidHostedWeb {
		return errors.New("network.hosted-web must be false or an object policy")
	}

	policy := workflowData.NetworkPermissions.HostedWeb
	runtimeID, err := c.resolveHostedWebRuntimeID(workflowData)
	if err != nil {
		return fmt.Errorf("network.hosted-web: failed to resolve engine runtime: %w", err)
	}
	if runtimeID != "claude" && runtimeID != "codex" {
		if policy == nil {
			return nil
		}
		return errors.New("network.hosted-web is only supported by the claude and codex engines")
	}
	firewallConfig := getFirewallConfig(workflowData)
	if !awfSupportsHostedWeb(firewallConfig) {
		awfTag := getAWFImageTag(firewallConfig)
		networkFirewallValidationLog.Printf("Rejecting network.hosted-web: AWF tag %q predates %s", awfTag, constants.AWFHostedWebMinVersion)
		return NewValidationError(
			"network.hosted-web",
			awfTag,
			fmt.Sprintf("requires AWF %s or newer; pinned version %q rejects apiProxy.hostedWeb", constants.AWFHostedWebMinVersion, awfTag),
			fmt.Sprintf("Set network.firewall.version or sandbox.agent.version to %s or newer:\n\nnetwork:\n  firewall:\n    version: %s", constants.AWFHostedWebMinVersion, constants.AWFHostedWebMinVersion),
		)
	}
	if policy == nil {
		return nil
	}
	if !policy.Enabled {
		if len(policy.Allowed) > 0 || len(policy.Blocked) > 0 || policy.MaxUses != 0 {
			return errors.New("network.hosted-web: false cannot be combined with allowed, blocked, or max-uses")
		}
		return nil
	}
	if len(policy.Allowed) == 0 && len(policy.Blocked) == 0 {
		return errors.New("network.hosted-web requires exactly one non-empty allowed or blocked list")
	}
	if len(policy.Allowed) > 0 && len(policy.Blocked) > 0 {
		return errors.New("network.hosted-web: allowed and blocked cannot both be set")
	}
	if policy.MaxUses < 0 {
		return errors.New("network.hosted-web.max-uses must be a positive integer")
	}
	if err := validateHostedWebDomains("allowed", policy.Allowed); err != nil {
		return err
	}
	if err := validateHostedWebDomains("blocked", policy.Blocked); err != nil {
		return err
	}
	return nil
}

func (c *Compiler) resolveHostedWebRuntimeID(workflowData *WorkflowData) (string, error) {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return "", nil
	}
	engineID := workflowData.EngineConfig.ID
	if workflowData.EngineConfig.IsInlineDefinition {
		return hostedWebRuntimeID(engineID, ""), nil
	}
	if c != nil && c.engineCatalog != nil {
		resolved, err := c.engineCatalog.Resolve(engineID, workflowData.EngineConfig)
		if err != nil {
			return "", err
		}
		if resolved != nil && resolved.Runtime != nil {
			return c.hostedWebRuntimeIDForResolvedEngine(engineID, resolved.Runtime.GetID()), nil
		}
	}
	return hostedWebRuntimeID(engineID, ""), nil
}

func (c *Compiler) hostedWebRuntimeIDForResolvedEngine(engineID, resolvedRuntimeID string) string {
	runtimeID := strings.ToLower(resolvedRuntimeID)
	if c.engineCatalog.Get(engineID) != nil {
		return runtimeID
	}
	if hostedWebRuntimeID(engineID, "") == runtimeID {
		return runtimeID
	}
	// EngineCatalog.Resolve has a broad historical prefix fallback; avoid treating
	// unrelated unregistered names (for example "codexbridge") as hosted-web-capable.
	return strings.ToLower(engineID)
}

func validateHostedWebDomains(field string, domains []string) error {
	seen := map[string]struct{}{}
	for _, domain := range domains {
		if _, ok := seen[domain]; ok {
			return fmt.Errorf("network.hosted-web.%s contains duplicate domain %q", field, domain)
		}
		seen[domain] = struct{}{}
		if err := validateHostedWebDomain(domain); err != nil {
			return err
		}
	}
	return nil
}

var hostedWebDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validateHostedWebDomain(domain string) error {
	if len(domain) > 253 {
		return fmt.Errorf("network.hosted-web domain %q must be 253 characters or fewer", domain)
	}
	if !hostedWebDomainPattern.MatchString(domain) || net.ParseIP(domain) != nil || domain == "localhost" {
		return fmt.Errorf("network.hosted-web domain %q must be a lowercase DNS hostname with at least two labels", domain)
	}
	return nil
}

// isEcosystemIdentifierPattern matches valid ecosystem identifiers like "defaults", "node", "dev-tools"
var isEcosystemIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// isEcosystemIdentifier checks if a domain string is actually an ecosystem identifier
func isEcosystemIdentifier(domain string) bool {
	// Ecosystem identifiers are simple lowercase alphanumeric identifiers with optional hyphens
	// like "defaults", "node", "python", "dev-tools", "default-safe-outputs".
	// They don't contain dots, protocol prefixes, spaces, wildcards, or other special characters.
	return isEcosystemIdentifierPattern.MatchString(domain)
}

// isKnownEcosystemIdentifier reports whether id is a recognised ecosystem identifier.
// It checks the base ecosystemDomains map and the compoundEcosystems map directly,
// avoiding the allocations that getEcosystemDomains incurs.
func isKnownEcosystemIdentifier(id string) bool {
	ecosystemDomains := getLoadedEcosystemDomains()
	if _, ok := ecosystemDomains[id]; ok {
		return true
	}
	_, ok := compoundEcosystems[id]
	return ok
}

// getValidEcosystemIdentifiers returns a sorted list of all valid ecosystem identifiers,
// including both the base identifiers from ecosystemDomains and compound identifiers.
func getValidEcosystemIdentifiers() []string {
	ecosystemDomains := getLoadedEcosystemDomains()
	ids := make([]string, 0, typeutil.SafeAllocationCapacity(len(ecosystemDomains), len(compoundEcosystems)))
	for id := range ecosystemDomains {
		ids = append(ids, id)
	}
	for id := range compoundEcosystems {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// domainPattern validates domain patterns including wildcards
// Valid patterns:
// - Plain domains: github.com, api.github.com
// - Wildcard domains: *.github.com
// Invalid patterns:
// - Multiple wildcards: *.*.github.com
// - Wildcard not at start: github.*.com
// - Empty or malformed domains
var domainPattern = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// validateDomainPattern validates a single domain pattern
func validateDomainPattern(domain string) error { //nolint:largefunc // Domain validation preserves detailed diagnostics.
	// Check for empty domain
	if domain == "" {
		return NewValidationError(
			"domain",
			"",
			"domain cannot be empty",
			"Provide a valid domain name. Examples:\n  - Plain domain: 'github.com'\n  - Wildcard: '*.github.com'\n  - With protocol: 'https://api.github.com'",
		)
	}

	// Check for invalid protocol prefixes
	// Only http:// and https:// are allowed
	if strings.Contains(domain, "://") {
		if !strings.HasPrefix(domain, "https://") && !strings.HasPrefix(domain, "http://") {
			return NewValidationError(
				"domain",
				domain,
				"domain pattern has invalid protocol, only 'http://' and 'https://' are allowed",
				"Remove the invalid protocol or use 'http://' or 'https://'. Examples:\n  - 'https://api.github.com'\n  - 'http://example.com'\n  - 'github.com' (no protocol)",
			)
		}
	}

	// Strip protocol prefix if present (http:// or https://)
	// This allows protocol-specific domain filtering
	domainWithoutProtocol := domain
	if after, ok := strings.CutPrefix(domain, "https://"); ok {
		domainWithoutProtocol = after
	} else if after, ok := strings.CutPrefix(domain, "http://"); ok {
		domainWithoutProtocol = after
	}

	// Check for wildcard-only pattern
	if domainWithoutProtocol == "*" {
		return NewValidationError(
			"domain",
			domain,
			"wildcard-only domain '*' is not allowed",
			"Use a specific wildcard pattern with a base domain. Examples:\n  - '*.example.com'\n  - '*.github.com'\n  - 'https://*.api.example.com'",
		)
	}

	// Check for wildcard without base domain (must be done before regex)
	if domainWithoutProtocol == "*." {
		return NewValidationError(
			"domain",
			domain,
			"wildcard pattern must have a domain after '*.'",
			"Add a base domain after the wildcard. Examples:\n  - '*.example.com'\n  - '*.github.com'\n  - 'https://*.api.example.com'",
		)
	}

	// Check for multiple wildcards
	if strings.Count(domainWithoutProtocol, "*") > 1 {
		return NewValidationError(
			"domain",
			domain,
			"domain pattern contains multiple wildcards, only one wildcard at the start is allowed",
			"Use a single wildcard at the start of the domain. Examples:\n  - '*.example.com' ✓\n  - '*.*.example.com' ✗ (multiple wildcards)\n  - 'https://*.github.com' ✓",
		)
	}

	// Check for wildcard not at the start (in the domain part)
	if strings.Contains(domainWithoutProtocol, "*") && !strings.HasPrefix(domainWithoutProtocol, "*.") {
		return NewValidationError(
			"domain",
			domain,
			"wildcard must be at the start followed by a dot",
			"Move the wildcard to the beginning of the domain. Examples:\n  - '*.example.com' ✓\n  - 'example.*.com' ✗ (wildcard in middle)\n  - 'https://*.github.com' ✓",
		)
	}

	// Additional validation for wildcard patterns
	if strings.HasPrefix(domainWithoutProtocol, "*.") {
		baseDomain := domainWithoutProtocol[2:] // Remove "*."
		if baseDomain == "" {
			return NewValidationError(
				"domain",
				domain,
				"wildcard pattern must have a domain after '*.'",
				"Add a base domain after the wildcard. Examples:\n  - '*.example.com'\n  - '*.github.com'\n  - 'https://*.api.example.com'",
			)
		}
		// Ensure the base domain doesn't start with a dot
		if strings.HasPrefix(baseDomain, ".") {
			return NewValidationError(
				"domain",
				domain,
				"wildcard pattern has invalid format (extra dot after wildcard)",
				"Use correct wildcard format. Examples:\n  - '*.example.com' ✓\n  - '*.*.example.com' ✗ (extra dot)\n  - 'https://*.github.com' ✓",
			)
		}
	}

	// Validate domain pattern format (without protocol)
	if !domainPattern.MatchString(domainWithoutProtocol) {
		// Provide specific error messages for common issues
		if strings.HasSuffix(domainWithoutProtocol, ".") {
			return NewValidationError(
				"domain",
				domain,
				"domain pattern cannot end with a dot",
				"Remove the trailing dot from the domain. Examples:\n  - 'example.com' ✓\n  - 'example.com.' ✗\n  - '*.github.com' ✓",
			)
		}
		if strings.Contains(domainWithoutProtocol, "..") {
			return NewValidationError(
				"domain",
				domain,
				"domain pattern cannot contain consecutive dots",
				"Remove extra dots from the domain. Examples:\n  - 'api.example.com' ✓\n  - 'api..example.com' ✗\n  - 'sub.api.example.com' ✓",
			)
		}
		if strings.HasPrefix(domainWithoutProtocol, ".") && !strings.HasPrefix(domainWithoutProtocol, "*.") {
			return NewValidationError(
				"domain",
				domain,
				"domain pattern cannot start with a dot (except for wildcard patterns)",
				"Remove the leading dot or use a wildcard. Examples:\n  - 'example.com' ✓\n  - '.example.com' ✗\n  - '*.example.com' ✓",
			)
		}
		// Check for invalid characters (in the domain part, not protocol)
		for _, char := range domainWithoutProtocol {
			if (char < 'a' || char > 'z') &&
				(char < 'A' || char > 'Z') &&
				(char < '0' || char > '9') &&
				char != '-' && char != '.' && char != '*' {
				return NewValidationError(
					"domain",
					domain,
					fmt.Sprintf("domain pattern contains invalid character '%c'", char),
					"Use only alphanumeric characters, hyphens, dots, and wildcards. Examples:\n  - 'api-v2.example.com' ✓\n  - 'api_v2.example.com' ✗ (underscore not allowed)\n  - '*.github.com' ✓",
				)
			}
		}
		return NewValidationError(
			"domain",
			domain,
			"domain pattern is not a valid domain format",
			"Use a valid domain format. Examples:\n  - Plain: 'github.com', 'api.example.com'\n  - Wildcard: '*.github.com', '*.example.com'\n  - With protocol: 'https://api.github.com'",
		)
	}

	return nil
}
