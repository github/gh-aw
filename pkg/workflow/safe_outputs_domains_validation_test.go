//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSafeOutputsAllowedDomains(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty allowed domains",
			config:  &SafeOutputsConfig{AllowedDomains: []string{}},
			wantErr: false,
		},
		{
			name: "valid plain domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"github.com",
					"api.github.com",
					"example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "valid wildcard domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"*.github.com",
					"*.example.org",
				},
			},
			wantErr: false,
		},
		{
			name: "mixed valid domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"github.com",
					"*.githubusercontent.com",
					"api.example.com",
					"*.test.org",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid - empty domain",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{""},
			},
			wantErr: true,
			errMsg:  "domain cannot be empty",
		},
		{
			name: "invalid - wildcard only",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"*"},
			},
			wantErr: true,
			errMsg:  "wildcard-only domain '*' is not allowed",
		},
		{
			name: "invalid - multiple wildcards",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"*.*.github.com"},
			},
			wantErr: true,
			errMsg:  "contains multiple wildcards",
		},
		{
			name: "invalid - wildcard in middle",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github.*.com"},
			},
			wantErr: true,
			errMsg:  "wildcard must be at the start followed by a dot",
		},
		{
			name: "invalid - wildcard at end",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github.*"},
			},
			wantErr: true,
			errMsg:  "wildcard must be at the start followed by a dot",
		},
		{
			name: "invalid - trailing dot",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github.com."},
			},
			wantErr: true,
			errMsg:  "cannot end with a dot",
		},
		{
			name: "invalid - leading dot",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{".github.com"},
			},
			wantErr: true,
			errMsg:  "cannot start with a dot",
		},
		{
			name: "invalid - consecutive dots",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github..com"},
			},
			wantErr: true,
			errMsg:  "cannot contain consecutive dots",
		},
		{
			name: "invalid - special characters",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github@example.com"},
			},
			wantErr: true,
			errMsg:  "contains invalid character",
		},
		{
			name: "invalid - spaces",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"github .com"},
			},
			wantErr: true,
			errMsg:  "contains invalid character",
		},
		{
			name: "invalid - wildcard without base domain",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{"*."},
			},
			wantErr: true,
			errMsg:  "must have a domain after",
		},
		{
			name: "invalid - multiple domains in first entry",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"github.com",
					"*.example.com",
					"invalid domain",
				},
			},
			wantErr: true,
			errMsg:  "safe-outputs.allowed-domains[2]",
		},
		{
			name: "valid - complex subdomain",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"very.long.subdomain.example.com",
					"*.multi.level.example.org",
				},
			},
			wantErr: false,
		},
		{
			name: "valid - domains with numbers and hyphens",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"api-v2.github.com",
					"test123.example.com",
					"*.cdn-example.org",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			err := c.validateSafeOutputsAllowedDomains(tt.config)
			if tt.wantErr {
				require.Error(t, err, "Expected an error but got none")
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidateDomainPattern(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
		errMsg  string
	}{
		// Valid plain domains
		{
			name:    "valid - simple domain",
			domain:  "github.com",
			wantErr: false,
		},
		{
			name:    "valid - subdomain",
			domain:  "api.github.com",
			wantErr: false,
		},
		{
			name:    "valid - multiple subdomains",
			domain:  "api.v2.github.com",
			wantErr: false,
		},
		{
			name:    "valid - domain with numbers",
			domain:  "test123.example.com",
			wantErr: false,
		},
		{
			name:    "valid - domain with hyphens",
			domain:  "my-api.example-site.com",
			wantErr: false,
		},

		// Valid wildcard domains
		{
			name:    "valid - wildcard subdomain",
			domain:  "*.github.com",
			wantErr: false,
		},
		{
			name:    "valid - wildcard with multiple levels",
			domain:  "*.api.example.com",
			wantErr: false,
		},

		// Invalid patterns
		{
			name:    "invalid - empty",
			domain:  "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "invalid - wildcard only",
			domain:  "*",
			wantErr: true,
			errMsg:  "wildcard-only",
		},
		{
			name:    "invalid - multiple wildcards",
			domain:  "*.*.github.com",
			wantErr: true,
			errMsg:  "multiple wildcards",
		},
		{
			name:    "invalid - wildcard in middle",
			domain:  "api.*.github.com",
			wantErr: true,
			errMsg:  "wildcard must be at the start followed by a dot",
		},
		{
			name:    "invalid - wildcard at end",
			domain:  "github.*",
			wantErr: true,
			errMsg:  "wildcard must be at the start followed by a dot",
		},
		{
			name:    "invalid - trailing dot",
			domain:  "github.com.",
			wantErr: true,
			errMsg:  "cannot end with a dot",
		},
		{
			name:    "invalid - leading dot",
			domain:  ".github.com",
			wantErr: true,
			errMsg:  "cannot start with a dot",
		},
		{
			name:    "invalid - consecutive dots",
			domain:  "github..com",
			wantErr: true,
			errMsg:  "consecutive dots",
		},
		{
			name:    "invalid - underscore",
			domain:  "github_api.com",
			wantErr: true,
			errMsg:  "invalid character",
		},
		{
			name:    "invalid - special character @",
			domain:  "user@github.com",
			wantErr: true,
			errMsg:  "invalid character",
		},
		{
			name:    "invalid - space",
			domain:  "github .com",
			wantErr: true,
			errMsg:  "invalid character",
		},
		{
			name:    "invalid - wildcard without domain",
			domain:  "*.",
			wantErr: true,
			errMsg:  "must have a domain after",
		},
		{
			name:    "invalid - wildcard with dot after",
			domain:  "*..",
			wantErr: true,
			errMsg:  "invalid format",
		},

		// Edge cases
		{
			name:    "valid - single character domain (theoretical)",
			domain:  "a.b",
			wantErr: false,
		},
		{
			name:    "valid - long subdomain",
			domain:  "very-long-subdomain-name-with-many-hyphens.example.com",
			wantErr: false,
		},
		{
			name:    "valid - many levels",
			domain:  "a.b.c.d.e.f.example.com",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDomainPattern(tt.domain)
			if tt.wantErr {
				require.Error(t, err, "Expected an error for domain: %s", tt.domain)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for domain: %s, but got: %v", tt.domain, err)
			}
		})
	}
}

func TestValidateDomainPatternCoverage(t *testing.T) {
	// Test various error paths to ensure comprehensive coverage
	errorCases := []struct {
		domain      string
		description string
	}{
		{"", "empty domain"},
		{"*", "wildcard only"},
		{"*.*", "double wildcard"},
		{"*.*.*.com", "triple wildcard"},
		{"test.*.example.com", "wildcard in middle"},
		{"test.*", "wildcard at end"},
		{"example.com.", "trailing dot"},
		{".example.com", "leading dot"},
		{"example..com", "consecutive dots"},
		{"example@test.com", "@ character"},
		{"example test.com", "space character"},
		{"example_test.com", "underscore"},
		{"*..", "wildcard with double dot"},
		{"*.", "wildcard without base"},
		{"example!.com", "exclamation mark"},
		{"*.*.github.com", "multiple wildcards nested"},
	}

	for _, tc := range errorCases {
		t.Run(tc.description, func(t *testing.T) {
			err := validateDomainPattern(tc.domain)
			assert.Error(t, err, "Domain '%s' (%s) should produce an error", tc.domain, tc.description)
		})
	}

	// Test valid patterns for positive coverage
	validCases := []string{
		"example.com",
		"api.example.com",
		"*.example.com",
		"test-api.example.com",
		"api123.example.com",
		"*.api.example.org",
		"a.b.c.d.example.com",
	}

	for _, domain := range validCases {
		t.Run("valid-"+domain, func(t *testing.T) {
			err := validateDomainPattern(domain)
			assert.NoError(t, err, "Valid domain '%s' should not produce an error", domain)
		})
	}
}

// TestDomainPatternRegex tests the domain pattern regex directly
func TestDomainPatternRegex(t *testing.T) {
	tests := []struct {
		domain  string
		matches bool
	}{
		// Should match
		{"example.com", true},
		{"*.example.com", true},
		{"api.example.com", true},
		{"test-123.example.com", true},

		// Should not match
		{"", false},
		{"example.com.", false},
		{".example.com", false},
		{"example..com", false},
		{"*.*.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			matches := domainPattern.MatchString(tt.domain)
			assert.Equal(t, tt.matches, matches, "Domain pattern regex match for '%s'", tt.domain)
		})
	}
}

// TestDomainPatternRegexComprehensive provides comprehensive regex validation tests
func TestDomainPatternRegexComprehensive(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		matches bool
		reason  string
	}{
		// Valid plain domains
		{
			name:    "simple two-part domain",
			domain:  "example.com",
			matches: true,
			reason:  "basic domain structure",
		},
		{
			name:    "three-part domain",
			domain:  "api.example.com",
			matches: true,
			reason:  "subdomain structure",
		},
		{
			name:    "four-part domain",
			domain:  "v2.api.example.com",
			matches: true,
			reason:  "multiple subdomain levels",
		},
		{
			name:    "domain with numbers",
			domain:  "api123.example456.com",
			matches: true,
			reason:  "alphanumeric characters",
		},
		{
			name:    "domain with hyphens",
			domain:  "my-api.my-example.com",
			matches: true,
			reason:  "hyphens in labels",
		},
		{
			name:    "domain starting with number",
			domain:  "1api.example.com",
			matches: true,
			reason:  "label can start with number",
		},
		{
			name:    "domain ending with number",
			domain:  "api1.example1.com",
			matches: true,
			reason:  "label can end with number",
		},
		{
			name:    "single character labels",
			domain:  "a.b.c",
			matches: true,
			reason:  "minimum label length",
		},
		{
			name:    "maximum label length (63 chars)",
			domain:  "a123456789012345678901234567890123456789012345678901234567890bc.example.com",
			matches: true,
			reason:  "63-character label is valid",
		},
		{
			name:    "very deep nesting",
			domain:  "a.b.c.d.e.f.g.example.com",
			matches: true,
			reason:  "many subdomain levels",
		},

		// Valid wildcard domains
		{
			name:    "wildcard with two-part base",
			domain:  "*.example.com",
			matches: true,
			reason:  "wildcard at start",
		},
		{
			name:    "wildcard with three-part base",
			domain:  "*.api.example.com",
			matches: true,
			reason:  "wildcard with subdomain base",
		},
		{
			name:    "wildcard with hyphenated base",
			domain:  "*.my-example.com",
			matches: true,
			reason:  "wildcard with hyphen in base",
		},
		{
			name:    "wildcard with numeric base",
			domain:  "*.example123.com",
			matches: true,
			reason:  "wildcard with numbers in base",
		},

		// Invalid - empty and whitespace
		{
			name:    "empty string",
			domain:  "",
			matches: false,
			reason:  "empty domain not allowed",
		},
		{
			name:    "only whitespace",
			domain:  "   ",
			matches: false,
			reason:  "whitespace not allowed",
		},

		// Invalid - trailing/leading dots
		{
			name:    "trailing dot",
			domain:  "example.com.",
			matches: false,
			reason:  "FQDN format not allowed",
		},
		{
			name:    "leading dot",
			domain:  ".example.com",
			matches: false,
			reason:  "leading dot not allowed",
		},
		{
			name:    "double leading dot",
			domain:  "..example.com",
			matches: false,
			reason:  "multiple leading dots not allowed",
		},
		{
			name:    "wildcard with trailing dot",
			domain:  "*.example.com.",
			matches: false,
			reason:  "wildcard with trailing dot invalid",
		},

		// Invalid - consecutive dots
		{
			name:    "double dots in middle",
			domain:  "example..com",
			matches: false,
			reason:  "consecutive dots not allowed",
		},
		{
			name:    "triple dots",
			domain:  "example...com",
			matches: false,
			reason:  "multiple consecutive dots not allowed",
		},
		{
			name:    "dots at start and middle",
			domain:  ".example..com",
			matches: false,
			reason:  "multiple dot issues",
		},

		// Invalid - wildcard patterns
		{
			name:    "double wildcard",
			domain:  "*.*.example.com",
			matches: false,
			reason:  "multiple wildcards not allowed",
		},
		{
			name:    "triple wildcard",
			domain:  "*.*.*.example.com",
			matches: false,
			reason:  "multiple wildcards not allowed",
		},
		{
			name:    "wildcard in middle",
			domain:  "api.*.example.com",
			matches: false,
			reason:  "wildcard must be at start",
		},
		{
			name:    "wildcard at end",
			domain:  "api.example.*",
			matches: false,
			reason:  "wildcard at end not allowed",
		},
		{
			name:    "wildcard without dot",
			domain:  "*example.com",
			matches: false,
			reason:  "wildcard must be followed by dot",
		},
		{
			name:    "only wildcard",
			domain:  "*",
			matches: false,
			reason:  "standalone wildcard not allowed",
		},
		{
			name:    "wildcard with dot only",
			domain:  "*.",
			matches: false,
			reason:  "wildcard with no base domain",
		},

		// Invalid - special characters
		{
			name:    "underscore in domain",
			domain:  "example_api.com",
			matches: false,
			reason:  "underscore not allowed in hostname",
		},
		{
			name:    "space in domain",
			domain:  "example .com",
			matches: false,
			reason:  "space not allowed",
		},
		{
			name:    "at sign",
			domain:  "user@example.com",
			matches: false,
			reason:  "@ not allowed in domain",
		},
		{
			name:    "forward slash",
			domain:  "example.com/path",
			matches: false,
			reason:  "path not part of domain",
		},
		{
			name:    "colon (port)",
			domain:  "example.com:8080",
			matches: false,
			reason:  "port not part of domain",
		},
		{
			name:    "question mark",
			domain:  "example.com?query",
			matches: false,
			reason:  "query string not part of domain",
		},
		{
			name:    "hash",
			domain:  "example.com#anchor",
			matches: false,
			reason:  "anchor not part of domain",
		},
		{
			name:    "percent encoding",
			domain:  "example%20.com",
			matches: false,
			reason:  "percent encoding not allowed",
		},
		{
			name:    "exclamation mark",
			domain:  "example!.com",
			matches: false,
			reason:  "special characters not allowed",
		},

		// Invalid - hyphen rules
		{
			name:    "hyphen at start of label",
			domain:  "-example.com",
			matches: false,
			reason:  "label cannot start with hyphen",
		},
		{
			name:    "hyphen at end of label",
			domain:  "example-.com",
			matches: false,
			reason:  "label cannot end with hyphen",
		},
		{
			name:    "hyphen at start and end",
			domain:  "-example-.com",
			matches: false,
			reason:  "label cannot start/end with hyphen",
		},
		{
			name:    "only hyphen in label",
			domain:  "-.com",
			matches: false,
			reason:  "label cannot be only hyphen",
		},

		// Invalid - single label (no TLD)
		{
			name:    "single label domain",
			domain:  "localhost",
			matches: true, // The regex allows single-label domains
			reason:  "single label matches regex pattern",
		},
		{
			name:    "wildcard single label",
			domain:  "*.localhost",
			matches: true,
			reason:  "wildcard with single label base",
		},

		// Edge cases - length limits
		{
			name:    "label too long (64 chars)",
			domain:  "a1234567890123456789012345678901234567890123456789012345678901234.example.com",
			matches: false,
			reason:  "label exceeds 63 character limit",
		},
		{
			name:    "exactly 63 chars in first label",
			domain:  "a123456789012345678901234567890123456789012345678901234567890bc.example.com",
			matches: true,
			reason:  "63 chars is valid",
		},

		// Edge cases - case sensitivity (regex should handle both)
		{
			name:    "uppercase domain",
			domain:  "EXAMPLE.COM",
			matches: true,
			reason:  "uppercase letters allowed",
		},
		{
			name:    "mixed case",
			domain:  "Example.Com",
			matches: true,
			reason:  "mixed case allowed",
		},
		{
			name:    "wildcard uppercase",
			domain:  "*.EXAMPLE.COM",
			matches: true,
			reason:  "wildcard with uppercase",
		},

		// Edge cases - numbers
		{
			name:    "all numbers",
			domain:  "123.456.789",
			matches: true,
			reason:  "numeric domains allowed",
		},
		{
			name:    "IP-like format",
			domain:  "192.168.1.1",
			matches: true,
			reason:  "IP-like patterns match domain regex",
		},

		// Edge cases - multiple hyphens
		{
			name:    "multiple hyphens in middle",
			domain:  "my--api.example.com",
			matches: true,
			reason:  "multiple hyphens in middle allowed",
		},
		{
			name:    "many hyphens",
			domain:  "my-really-long-domain-name.example.com",
			matches: true,
			reason:  "many hyphens allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := domainPattern.MatchString(tt.domain)
			assert.Equal(t, tt.matches, matches,
				"Domain '%s' regex match - %s (expected %v, got %v)",
				tt.domain, tt.reason, tt.matches, matches)
		})
	}
}

// TestValidateSafeOutputsAllowedDomainsIntegration tests validation with realistic workflow configurations
func TestValidateSafeOutputsAllowedDomainsIntegration(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr bool
	}{
		{
			name: "typical configuration",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"api.github.com",
					"*.githubusercontent.com",
					"raw.githubusercontent.com",
				},
			},
			wantErr: false,
		},
		{
			name: "multi-repository configuration",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"*.github.com",
					"*.gitlab.com",
					"api.bitbucket.org",
				},
			},
			wantErr: false,
		},
		{
			name: "CDN and API domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"cdn.example.com",
					"*.cdn.example.com",
					"api-v2.example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "configuration with error in list",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"api.github.com",
					"*.invalid..com", // Double dot
					"valid.example.com",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			err := c.validateSafeOutputsAllowedDomains(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
