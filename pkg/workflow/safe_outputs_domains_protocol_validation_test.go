//go:build !integration

package workflow

import (
	"testing"
)

// TestValidateDomainPatternWithProtocol tests domain validation with protocol prefixes
func TestValidateDomainPatternWithProtocol(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		// Valid domains with HTTPS protocol
		{
			name:    "HTTPS domain",
			domain:  "https://example.com",
			wantErr: false,
		},
		{
			name:    "HTTPS wildcard domain",
			domain:  "https://*.example.com",
			wantErr: false,
		},
		{
			name:    "HTTPS subdomain",
			domain:  "https://api.example.com",
			wantErr: false,
		},

		// Valid domains with HTTP protocol
		{
			name:    "HTTP domain",
			domain:  "http://example.com",
			wantErr: false,
		},
		{
			name:    "HTTP wildcard domain",
			domain:  "http://*.example.com",
			wantErr: false,
		},
		{
			name:    "HTTP subdomain",
			domain:  "http://api.example.com",
			wantErr: false,
		},

		// Valid domains without protocol (backward compatibility)
		{
			name:    "Plain domain",
			domain:  "example.com",
			wantErr: false,
		},
		{
			name:    "Wildcard domain",
			domain:  "*.example.com",
			wantErr: false,
		},

		// Invalid patterns
		{
			name:    "Empty domain",
			domain:  "",
			wantErr: true,
		},
		{
			name:    "Protocol only",
			domain:  "https://",
			wantErr: true,
		},
		{
			name:    "HTTPS wildcard only",
			domain:  "https://*",
			wantErr: true,
		},
		{
			name:    "HTTP wildcard only",
			domain:  "http://*",
			wantErr: true,
		},
		{
			name:    "HTTPS wildcard without base domain",
			domain:  "https://*.",
			wantErr: true,
		},
		{
			name:    "Invalid protocol",
			domain:  "ftp://example.com",
			wantErr: true,
		},
		{
			name:    "Multiple wildcards with HTTPS",
			domain:  "https://*.*.example.com",
			wantErr: true,
		},
		{
			name:    "Wildcard in wrong position with HTTPS",
			domain:  "https://example.*.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDomainPattern(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDomainPattern(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

// TestValidateSafeOutputsAllowedDomainsWithProtocol tests safe-outputs domain validation with protocols
func TestValidateSafeOutputsAllowedDomainsWithProtocol(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr bool
	}{
		{
			name: "Mixed protocol domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"https://secure.example.com",
					"http://legacy.example.com",
					"example.org",
				},
			},
			wantErr: false,
		},
		{
			name: "HTTPS wildcard domains",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"https://*.example.com",
					"https://api.example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid protocol in list",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"https://valid.example.com",
					"ftp://invalid.example.com",
				},
			},
			wantErr: true,
		},
		{
			name: "HTTPS with invalid domain",
			config: &SafeOutputsConfig{
				AllowedDomains: []string{
					"https://",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			err := c.validateSafeOutputsAllowedDomains(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSafeOutputsAllowedDomains() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
