package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsDomainsValidationLog = logger.New("workflow:safe_outputs_domains_validation")

// validateNetworkAllowedDomains validates the allowed domains in network configuration
func (c *Compiler) validateNetworkAllowedDomains(network *NetworkPermissions) error {
	if network == nil || len(network.Allowed) == 0 {
		return nil
	}

	safeOutputsDomainsValidationLog.Printf("Validating %d network allowed domains", len(network.Allowed))

	collector := NewErrorCollector(c.failFast)

	for i, domain := range network.Allowed {
		// Skip ecosystem identifiers - they don't need domain pattern validation
		if isEcosystemIdentifier(domain) {
			continue
		}

		if err := validateDomainPattern(domain); err != nil {
			wrappedErr := fmt.Errorf("network.allowed[%d]: %w", i, err)
			if returnErr := collector.Add(wrappedErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
		}
	}

	return collector.Error()
}

// isEcosystemIdentifier checks if a domain string is actually an ecosystem identifier
func isEcosystemIdentifier(domain string) bool {
	// Ecosystem identifiers don't contain dots and don't have protocol prefixes
	// They are simple identifiers like "defaults", "node", "python", etc.
	return !strings.Contains(domain, ".") && !strings.Contains(domain, "://")
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

// validateSafeOutputsAllowedDomains validates the allowed-domains configuration in safe-outputs
func (c *Compiler) validateSafeOutputsAllowedDomains(config *SafeOutputsConfig) error {
	if config == nil || len(config.AllowedDomains) == 0 {
		return nil
	}

	safeOutputsDomainsValidationLog.Printf("Validating %d allowed domains", len(config.AllowedDomains))

	collector := NewErrorCollector(c.failFast)

	for i, domain := range config.AllowedDomains {
		if err := validateDomainPattern(domain); err != nil {
			wrappedErr := fmt.Errorf("safe-outputs.allowed-domains[%d]: %w", i, err)
			if returnErr := collector.Add(wrappedErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
		}
	}

	return collector.Error()
}

// validateDomainPattern validates a single domain pattern
func validateDomainPattern(domain string) error {
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
	if strings.HasPrefix(domain, "https://") {
		domainWithoutProtocol = strings.TrimPrefix(domain, "https://")
	} else if strings.HasPrefix(domain, "http://") {
		domainWithoutProtocol = strings.TrimPrefix(domain, "http://")
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
