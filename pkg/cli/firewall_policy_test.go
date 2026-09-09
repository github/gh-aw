//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainMatchesRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		host     string
		rule     FirewallPolicyRule
		expected bool
	}{
		{
			name: "exact match without port",
			host: "github.com",
			rule: FirewallPolicyRule{
				Domains: []string{"github.com"},
			},
			expected: true,
		},
		{
			name: "exact match with port",
			host: "github.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{"github.com"},
			},
			expected: true,
		},
		{
			name: "wildcard match - subdomain",
			host: "api.github.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: true,
		},
		{
			name: "wildcard match - base domain",
			host: "github.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: true,
		},
		{
			name: "wildcard match - deep subdomain",
			host: "api.v2.github.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: true,
		},
		{
			name: "no match - different domain",
			host: "evil.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: false,
		},
		{
			name: "no match - suffix collision",
			host: "notgithub.com:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: false,
		},
		{
			name: "case insensitive match",
			host: "API.GitHub.COM:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com"},
			},
			expected: true,
		},
		{
			name: "multiple domains in rule",
			host: "npmjs.org:443",
			rule: FirewallPolicyRule{
				Domains: []string{".github.com", "npmjs.org"},
			},
			expected: true,
		},
		{
			name: "no match - empty domains",
			host: "example.com",
			rule: FirewallPolicyRule{
				Domains: []string{},
			},
			expected: false,
		},
		{
			name: "regex match - IP pattern",
			host: "192.168.1.1",
			rule: FirewallPolicyRule{
				ACLName: "dst_ipv4_regex",
				Domains: []string{`^192\.168\.`},
			},
			expected: true,
		},
		{
			name: "regex match - metachar detection",
			host: "test.example.com",
			rule: FirewallPolicyRule{
				ACLName: "some_acl",
				Domains: []string{`^.*\.example\.com$`},
			},
			expected: true,
		},
		{
			name: "regex no match",
			host: "other.com",
			rule: FirewallPolicyRule{
				ACLName: "dst_ipv4_regex",
				Domains: []string{`^192\.168\.`},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := domainMatchesRule(tt.host, tt.rule)
			assert.Equal(t, tt.expected, result, "domainMatchesRule(%q) should return %v", tt.host, tt.expected)
		})
	}
}

func TestFindMatchingRule(t *testing.T) {
	t.Parallel()
	rules := []FirewallPolicyRule{
		{
			ID:       "allow-github",
			Order:    1,
			Action:   "allow",
			ACLName:  "allowed_domains",
			Protocol: "both",
			Domains:  []string{".github.com"},
		},
		{
			ID:       "allow-npm",
			Order:    2,
			Action:   "allow",
			ACLName:  "npm_domains",
			Protocol: "both",
			Domains:  []string{"registry.npmjs.org"},
		},
		{
			ID:       "deny-all",
			Order:    3,
			Action:   "deny",
			ACLName:  "all",
			Protocol: "both",
			Domains:  []string{},
		},
	}

	t.Run("matches first rule - allowed HTTPS", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"}
		rule := findMatchingRule(entry, rules)
		require.NotNil(t, rule, "Should find a matching rule")
		assert.Equal(t, "allow-github", rule.ID, "Should match allow-github rule")
	})

	t.Run("matches second rule", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "registry.npmjs.org:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"}
		rule := findMatchingRule(entry, rules)
		require.NotNil(t, rule, "Should find a matching rule")
		assert.Equal(t, "allow-npm", rule.ID, "Should match allow-npm rule")
	})

	t.Run("aclName all catches unmatched denied traffic", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "evil.com:443", Method: "CONNECT", Status: 403, Decision: "NONE_NONE"}
		rule := findMatchingRule(entry, rules)
		require.NotNil(t, rule, "Should find the catch-all deny rule")
		assert.Equal(t, "deny-all", rule.ID, "Should match deny-all rule via aclName 'all'")
	})

	t.Run("aclName all skipped for allowed traffic", func(t *testing.T) {
		t.Parallel()
		// If a domain doesn't match specific rules but traffic was allowed,
		// the deny-all rule should NOT match (action mismatch)
		entry := AuditLogEntry{Host: "unknown.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"}
		rule := findMatchingRule(entry, rules)
		assert.Nil(t, rule, "deny-all rule should not match allowed traffic")
	})

	t.Run("first matching rule wins", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"}
		rule := findMatchingRule(entry, rules)
		require.NotNil(t, rule, "Should find a matching rule")
		assert.Equal(t, "allow-github", rule.ID, "First matching rule should win")
	})

	t.Run("observed-decision validation - allow rule skipped for denied traffic", func(t *testing.T) {
		t.Parallel()
		// Domain matches allow-github, but traffic was denied — allow rule shouldn't be credited
		entry := AuditLogEntry{Host: "api.github.com:443", Method: "CONNECT", Status: 403, Decision: "NONE_NONE"}
		rule := findMatchingRule(entry, rules)
		// Falls through to deny-all since allow-github action doesn't match observed denial
		require.NotNil(t, rule, "Should fall through to deny-all")
		assert.Equal(t, "deny-all", rule.ID, "Should match deny-all, not allow-github")
	})
}

func TestProtocolMatching(t *testing.T) {
	t.Parallel()
	rules := []FirewallPolicyRule{
		{
			ID:       "allow-https-only",
			Order:    1,
			Action:   "allow",
			ACLName:  "https_domains",
			Protocol: "https",
			Domains:  []string{".github.com"},
		},
		{
			ID:       "deny-all",
			Order:    2,
			Action:   "deny",
			ACLName:  "all",
			Protocol: "both",
			Domains:  []string{},
		},
	}

	t.Run("HTTPS rule matches CONNECT request", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"}
		rule := findMatchingRule(entry, rules)
		require.NotNil(t, rule, "Should match HTTPS rule")
		assert.Equal(t, "allow-https-only", rule.ID, "HTTPS rule should match CONNECT request")
	})

	t.Run("HTTPS rule skipped for HTTP request", func(t *testing.T) {
		t.Parallel()
		entry := AuditLogEntry{Host: "api.github.com:80", Method: "GET", Status: 403, Decision: "NONE_NONE"}
		rule := findMatchingRule(entry, rules)
		// HTTPS-only rule skipped for GET → falls through to deny-all
		require.NotNil(t, rule, "Should fall through to deny-all")
		assert.Equal(t, "deny-all", rule.ID, "HTTPS rule should not match HTTP GET request")
	})
}

func TestIsEntryHTTPS(t *testing.T) {
	t.Parallel()
	assert.True(t, isEntryHTTPS(AuditLogEntry{Method: "CONNECT"}), "CONNECT should be HTTPS")
	assert.True(t, isEntryHTTPS(AuditLogEntry{Method: "connect"}), "connect (lowercase) should be HTTPS")
	assert.False(t, isEntryHTTPS(AuditLogEntry{Method: "GET"}), "GET should not be HTTPS")
	assert.False(t, isEntryHTTPS(AuditLogEntry{Method: ""}), "Empty method should not be HTTPS")
}

func TestIsEntryAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		entry    AuditLogEntry
		expected bool
	}{
		{"status 200 is allowed", AuditLogEntry{Status: 200}, true},
		{"status 206 is allowed", AuditLogEntry{Status: 206}, true},
		{"status 304 is allowed", AuditLogEntry{Status: 304}, true},
		{"status 403 is denied", AuditLogEntry{Status: 403}, false},
		{"status 407 is denied", AuditLogEntry{Status: 407}, false},
		{"TCP_TUNNEL decision is allowed", AuditLogEntry{Status: 0, Decision: "TCP_TUNNEL:HIER_DIRECT"}, true},
		{"NONE_NONE decision is denied", AuditLogEntry{Status: 0, Decision: "NONE_NONE:HIER_NONE"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isEntryAllowed(tt.entry), "isEntryAllowed should return %v", tt.expected)
		})
	}
}

func TestContainsRegexMeta(t *testing.T) {
	t.Parallel()
	assert.True(t, containsRegexMeta(`^192\.168`), "Should detect caret")
	assert.True(t, containsRegexMeta(`foo.*bar`), "Should detect asterisk")
	assert.True(t, containsRegexMeta(`[0-9]+`), "Should detect brackets")
	assert.False(t, containsRegexMeta("github.com"), "Plain domain should not be regex")
	assert.False(t, containsRegexMeta(".github.com"), "Dot-prefix domain should not be regex")
}

func TestLoadPolicyManifest(t *testing.T) {
	t.Parallel()
	t.Run("valid manifest", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "policy-manifest.json")

		manifest := PolicyManifest{
			Version:     1,
			GeneratedAt: "2026-01-01T00:00:00Z",
			Rules: []FirewallPolicyRule{
				{ID: "rule-b", Order: 2, Action: "deny", Domains: []string{".evil.com"}, Description: "Block evil"},
				{ID: "rule-a", Order: 1, Action: "allow", Domains: []string{".github.com"}, Description: "Allow GitHub"},
			},
			SSLBumpEnabled: false,
			DLPEnabled:     false,
		}

		data, err := json.Marshal(manifest)
		require.NoError(t, err, "Should marshal manifest")
		require.NoError(t, os.WriteFile(manifestPath, data, 0644), "Should write manifest file")

		loaded, err := loadPolicyManifest(manifestPath)
		require.NoError(t, err, "Should load manifest without error")
		require.NotNil(t, loaded, "Loaded manifest should not be nil")

		assert.Equal(t, 1, loaded.Version, "Version should be 1")
		assert.Len(t, loaded.Rules, 2, "Should have 2 rules")
		// Rules should be sorted by order
		assert.Equal(t, "rule-a", loaded.Rules[0].ID, "First rule should be rule-a (order 1)")
		assert.Equal(t, "rule-b", loaded.Rules[1].ID, "Second rule should be rule-b (order 2)")
	})

	t.Run("manifest with hostAccessEnabled and allowHostPorts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "policy-manifest.json")

		ports := "8080,9090"
		manifest := PolicyManifest{
			Version:           1,
			Rules:             []FirewallPolicyRule{},
			HostAccessEnabled: true,
			AllowHostPorts:    &ports,
		}

		data, err := json.Marshal(manifest)
		require.NoError(t, err, "Should marshal manifest with extra fields")
		require.NoError(t, os.WriteFile(manifestPath, data, 0644))

		loaded, err := loadPolicyManifest(manifestPath)
		require.NoError(t, err, "Should load manifest")
		assert.True(t, loaded.HostAccessEnabled, "HostAccessEnabled should be true")
		require.NotNil(t, loaded.AllowHostPorts, "AllowHostPorts should not be nil")
		assert.Equal(t, "8080,9090", *loaded.AllowHostPorts, "AllowHostPorts should match")
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := loadPolicyManifest("/nonexistent/path/policy-manifest.json")
		assert.Error(t, err, "Should return error for missing file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "policy-manifest.json")
		require.NoError(t, os.WriteFile(manifestPath, []byte("not json"), 0644))

		_, err := loadPolicyManifest(manifestPath)
		assert.Error(t, err, "Should return error for invalid JSON")
	})
}

func TestParseAuditJSONL(t *testing.T) {
	t.Parallel()
	t.Run("valid JSONL", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "audit.jsonl")

		lines := `{"ts":1761074374.646,"client":"172.30.0.20","host":"api.github.com:443","dest":"140.82.114.22:443","method":"CONNECT","status":200,"decision":"TCP_TUNNEL","url":"api.github.com:443"}
{"ts":1761074375.100,"client":"172.30.0.20","host":"evil.com:443","dest":"1.2.3.4:443","method":"CONNECT","status":403,"decision":"NONE_NONE","url":"evil.com:443"}
{"ts":1761074376.200,"client":"172.30.0.20","host":"registry.npmjs.org:443","dest":"104.16.0.1:443","method":"CONNECT","status":200,"decision":"TCP_TUNNEL","url":"registry.npmjs.org:443"}
`
		require.NoError(t, os.WriteFile(jsonlPath, []byte(lines), 0644))

		entries, err := parseAuditJSONL(jsonlPath)
		require.NoError(t, err, "Should parse JSONL without error")
		assert.Len(t, entries, 3, "Should parse 3 entries")

		// Check first entry
		assert.InDelta(t, 1761074374.646, entries[0].Timestamp, 0.001, "Timestamp should match")
		assert.Equal(t, "api.github.com:443", entries[0].Host, "Host should match")
		assert.Equal(t, 200, entries[0].Status, "Status should match")

		// Check denied entry
		assert.Equal(t, "evil.com:443", entries[1].Host, "Second host should match")
		assert.Equal(t, 403, entries[1].Status, "Second status should be 403")
	})

	t.Run("empty lines skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "audit.jsonl")

		lines := `{"ts":1.0,"host":"a.com:443","status":200}

{"ts":2.0,"host":"b.com:443","status":200}
`
		require.NoError(t, os.WriteFile(jsonlPath, []byte(lines), 0644))

		entries, err := parseAuditJSONL(jsonlPath)
		require.NoError(t, err, "Should parse JSONL without error")
		assert.Len(t, entries, 2, "Should parse 2 entries, skipping empty line")
	})

	t.Run("malformed lines skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "audit.jsonl")

		lines := `{"ts":1.0,"host":"valid.com:443","status":200}
not valid json
{"ts":2.0,"host":"also-valid.com:443","status":200}
`
		require.NoError(t, os.WriteFile(jsonlPath, []byte(lines), 0644))

		entries, err := parseAuditJSONL(jsonlPath)
		require.NoError(t, err, "Should parse JSONL without error despite malformed line")
		assert.Len(t, entries, 2, "Should parse 2 valid entries")
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := parseAuditJSONL("/nonexistent/path/audit.jsonl")
		assert.Error(t, err, "Should return error for missing file")
	})
}

func TestEnrichWithPolicyRules(t *testing.T) {
	t.Parallel()
	manifest := &PolicyManifest{
		Version:     1,
		GeneratedAt: "2026-01-01T00:00:00Z",
		Rules: []FirewallPolicyRule{
			{
				ID:          "allow-github",
				Order:       1,
				Action:      "allow",
				ACLName:     "allowed_domains",
				Protocol:    "both",
				Domains:     []string{".github.com"},
				Description: "Allow GitHub and subdomains",
			},
			{
				ID:          "allow-npm",
				Order:       2,
				Action:      "allow",
				ACLName:     "npm_domains",
				Protocol:    "both",
				Domains:     []string{"registry.npmjs.org"},
				Description: "Allow npm registry",
			},
			{
				ID:          "deny-all",
				Order:       3,
				Action:      "deny",
				ACLName:     "all",
				Protocol:    "both",
				Domains:     []string{},
				Description: "Deny all other traffic",
			},
		},
		SSLBumpEnabled: false,
		DLPEnabled:     false,
	}

	entries := []AuditLogEntry{
		{Timestamp: 1761074374.646, Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
		{Timestamp: 1761074375.100, Host: "github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
		{Timestamp: 1761074376.200, Host: "registry.npmjs.org:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
		{Timestamp: 1761074377.300, Host: "evil.com:443", Method: "CONNECT", Status: 403, Decision: "NONE_NONE"},
	}

	analysis := enrichWithPolicyRules(entries, manifest)
	require.NotNil(t, analysis, "Analysis should not be nil")

	t.Run("summary statistics", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 4, analysis.TotalRequests, "Should have 4 total requests")
		assert.Equal(t, 3, analysis.AllowedCount, "Should have 3 allowed requests")
		assert.Equal(t, 1, analysis.DeniedCount, "Should have 1 denied request")
		assert.Equal(t, 4, analysis.UniqueDomains, "Should have 4 unique domains")
	})

	t.Run("policy summary", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, analysis.PolicySummary, "3 rules", "Policy summary should mention 3 rules")
		assert.Contains(t, analysis.PolicySummary, "SSL Bump disabled", "Should mention SSL Bump status")
		assert.Contains(t, analysis.PolicySummary, "DLP disabled", "Should mention DLP status")
	})

	t.Run("rule hits", func(t *testing.T) {
		t.Parallel()
		require.Len(t, analysis.RuleHits, 3, "Should have 3 rule hit entries")
		// Rules should be in order
		assert.Equal(t, "allow-github", analysis.RuleHits[0].Rule.ID, "First rule should be allow-github")
		assert.Equal(t, 2, analysis.RuleHits[0].Hits, "allow-github should have 2 hits")
		assert.Equal(t, "allow-npm", analysis.RuleHits[1].Rule.ID, "Second rule should be allow-npm")
		assert.Equal(t, 1, analysis.RuleHits[1].Hits, "allow-npm should have 1 hit")
		assert.Equal(t, "deny-all", analysis.RuleHits[2].Rule.ID, "Third rule should be deny-all")
		assert.Equal(t, 1, analysis.RuleHits[2].Hits, "deny-all should have 1 hit (evil.com)")
	})

	t.Run("denied requests attributed to deny-all rule", func(t *testing.T) {
		t.Parallel()
		require.Len(t, analysis.DeniedRequests, 1, "Should have 1 denied request")
		assert.Equal(t, "evil.com:443", analysis.DeniedRequests[0].Host, "Denied request host should match")
		assert.Equal(t, "deny", analysis.DeniedRequests[0].Action, "Action should be deny")
		assert.Equal(t, "deny-all", analysis.DeniedRequests[0].RuleID, "Should be attributed to deny-all rule")
	})

	t.Run("entries with empty host skipped", func(t *testing.T) {
		t.Parallel()
		emptyEntries := []AuditLogEntry{
			{Timestamp: 1.0, Host: "", Status: 200},
			{Timestamp: 2.0, Host: "-", Status: 200},
			{Timestamp: 3.0, Host: "valid.com:443", Method: "CONNECT", Status: 403, Decision: "NONE_NONE"},
		}
		result := enrichWithPolicyRules(emptyEntries, manifest)
		// valid.com is denied → matches deny-all via aclName "all"
		assert.Equal(t, 1, result.TotalRequests, "Only valid entries should be counted")
		require.Len(t, result.DeniedRequests, 1, "Should have 1 denied request")
		assert.Equal(t, "deny-all", result.DeniedRequests[0].RuleID, "Should match deny-all rule")
	})

	t.Run("error:transaction-end-before-headers entries filtered", func(t *testing.T) {
		t.Parallel()
		squidEntries := []AuditLogEntry{
			{Timestamp: 1.0, Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL", URL: "api.github.com:443"},
			{Timestamp: 2.0, Host: "api.github.com:443", Method: "CONNECT", Status: 0, Decision: "NONE_NONE", URL: "error:transaction-end-before-headers"},
		}
		result := enrichWithPolicyRules(squidEntries, manifest)
		assert.Equal(t, 1, result.TotalRequests, "Squid error entries should be filtered out")
	})

	t.Run("unattributed-allow for allowed traffic with no matching rule", func(t *testing.T) {
		t.Parallel()
		// Manifest without a catch-all rule — allowed traffic that doesn't match
		// any allow rule should be classified as (unattributed-allow), not (implicit-deny)
		limitedManifest := &PolicyManifest{
			Version: 1,
			Rules: []FirewallPolicyRule{
				{ID: "allow-github", Order: 1, Action: "allow", ACLName: "allowed_domains", Protocol: "both", Domains: []string{".github.com"}, Description: "Allow GitHub"},
			},
		}
		unattribEntries := []AuditLogEntry{
			// Allowed request that matches the allow-github rule
			{Timestamp: 1.0, Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
			// Allowed request that does NOT match any rule — should be (unattributed-allow)
			{Timestamp: 2.0, Host: "unknown.example.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
			// Denied request that does NOT match any rule — should be (implicit-deny)
			{Timestamp: 3.0, Host: "evil.com:443", Method: "CONNECT", Status: 403, Decision: "NONE_NONE"},
		}
		result := enrichWithPolicyRules(unattribEntries, limitedManifest)
		assert.Equal(t, 3, result.TotalRequests, "Should process all 3 entries")
		assert.Equal(t, 2, result.AllowedCount, "Should have 2 allowed (1 attributed + 1 unattributed)")
		assert.Equal(t, 1, result.DeniedCount, "Should have 1 denied (implicit)")
		require.Len(t, result.DeniedRequests, 1, "Should have 1 denied request")
		assert.Equal(t, "(implicit-deny)", result.DeniedRequests[0].RuleID, "Denied should be implicit-deny")
		assert.Equal(t, "evil.com:443", result.DeniedRequests[0].Host, "Denied host should match")
	})

	t.Run("unique domains case normalized", func(t *testing.T) {
		t.Parallel()
		caseEntries := []AuditLogEntry{
			{Timestamp: 1.0, Host: "API.GITHUB.COM:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
			{Timestamp: 2.0, Host: "api.github.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
			{Timestamp: 3.0, Host: "Api.GitHub.com:443", Method: "CONNECT", Status: 200, Decision: "TCP_TUNNEL"},
		}
		result := enrichWithPolicyRules(caseEntries, manifest)
		assert.Equal(t, 1, result.UniqueDomains, "Mixed-case hosts for same domain should count as 1 unique domain")
	})
}

func TestDetectFirewallAuditArtifacts(t *testing.T) {
	t.Parallel()
	t.Run("sandbox/firewall/audit path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL")
	})

	t.Run("agent artifact new structure (not yet flattened)", func(t *testing.T) {
		t.Parallel()
		// Simulates a directory populated by `gh run download` before flattenUnifiedArtifact
		// is called. actions/upload-artifact v4+ strips the /tmp/gh-aw/ common prefix, so
		// files land at agent/sandbox/firewall/audit/ inside the downloaded artifact dir.
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "agent", "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest in agent/sandbox/firewall/audit")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL in agent/sandbox/firewall/audit")
	})

	t.Run("agent artifact old structure with tmp/gh-aw prefix (not yet flattened)", func(t *testing.T) {
		t.Parallel()
		// Simulates older artifact structure where the full /tmp/gh-aw/ path was preserved
		// inside the agent artifact directory before the v4+ prefix-stripping behavior.
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "agent", "tmp", "gh-aw", "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest in agent/tmp/gh-aw/sandbox/firewall/audit")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL in agent/tmp/gh-aw/sandbox/firewall/audit")
	})

	t.Run("agent-artifacts legacy artifact name (not yet flattened)", func(t *testing.T) {
		t.Parallel()
		// Simulates the legacy "agent-artifacts" artifact name used before the rename to "agent".
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "agent-artifacts", "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest in agent-artifacts/sandbox/firewall/audit")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL in agent-artifacts/sandbox/firewall/audit")
	})

	t.Run("workflow_call prefixed agent artifact (not yet flattened)", func(t *testing.T) {
		t.Parallel()
		// Simulates the workflow_call artifact naming where a hash prefix is added:
		// e.g., "abc123-agent" instead of "agent".
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "abc123-agent", "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest in prefixed-agent/sandbox/firewall/audit")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL in prefixed-agent/sandbox/firewall/audit")
	})

	t.Run("firewall-audit-logs directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "firewall-audit-logs")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifestPath := filepath.Join(auditDir, "policy-manifest.json")
		auditPath := filepath.Join(auditDir, "audit.jsonl")
		require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(auditPath, []byte(""), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with valid run dir")
		assert.Equal(t, manifestPath, foundManifest, "Should find policy manifest in firewall-audit-logs")
		assert.Equal(t, auditPath, foundAudit, "Should find audit JSONL in firewall-audit-logs")
	})

	t.Run("no artifacts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error with empty run dir")
		assert.Empty(t, foundManifest, "Should not find manifest")
		assert.Empty(t, foundAudit, "Should not find audit JSONL")
	})

	t.Run("file named 'agent' does not panic or falsely match", func(t *testing.T) {
		t.Parallel()
		// If a plain file happens to be named "agent" in the run directory (e.g., a flattened
		// single-file artifact from a different upload), the lookup must skip it gracefully.
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent"), []byte("not a directory"), 0644))

		foundManifest, foundAudit, err := detectFirewallAuditArtifacts(dir)
		require.NoError(t, err, "Should not error when 'agent' is a file")
		assert.Empty(t, foundManifest, "Should not find manifest when 'agent' is a file")
		assert.Empty(t, foundAudit, "Should not find audit JSONL when 'agent' is a file")
	})

	t.Run("unreadable run directory returns error", func(t *testing.T) {
		t.Parallel()
		if os.Getuid() == 0 {
			t.Skip("root can read any directory; skipping permission test")
		}
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0000), "Should be able to remove read permission")
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		_, _, err := detectFirewallAuditArtifacts(dir)
		require.Error(t, err, "Should return an error when run dir is unreadable")
		require.ErrorContains(t, err, "detectFirewallAuditArtifacts", "Error should identify the function")
	})
}

func TestAnalyzeFirewallPolicy(t *testing.T) {
	t.Parallel()
	t.Run("full enrichment", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		// Write policy manifest
		manifest := PolicyManifest{
			Version:     1,
			GeneratedAt: "2026-01-01T00:00:00Z",
			Rules: []FirewallPolicyRule{
				{ID: "allow-github", Order: 1, Action: "allow", ACLName: "allowed_domains", Protocol: "both", Domains: []string{".github.com"}, Description: "Allow GitHub"},
				{ID: "deny-all", Order: 2, Action: "deny", ACLName: "all", Protocol: "both", Domains: []string{}, Description: "Block all other traffic"},
			},
		}
		manifestData, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(auditDir, "policy-manifest.json"), manifestData, 0644))

		// Write audit JSONL
		jsonl := `{"ts":1.0,"host":"api.github.com:443","method":"CONNECT","status":200,"decision":"TCP_TUNNEL"}
{"ts":2.0,"host":"evil.com:443","method":"CONNECT","status":403,"decision":"NONE_NONE"}
`
		require.NoError(t, os.WriteFile(filepath.Join(auditDir, "audit.jsonl"), []byte(jsonl), 0644))

		analysis, err := analyzeFirewallPolicy(dir, false)
		require.NoError(t, err, "Should analyze without error")
		require.NotNil(t, analysis, "Analysis should not be nil")

		assert.Equal(t, 2, analysis.TotalRequests, "Should have 2 total requests")
		assert.Equal(t, 1, analysis.AllowedCount, "Should have 1 allowed request")
		assert.Equal(t, 1, analysis.DeniedCount, "Should have 1 denied request")
		require.Len(t, analysis.DeniedRequests, 1, "Should have 1 denied request detail")
		assert.Equal(t, "deny-all", analysis.DeniedRequests[0].RuleID, "Denied request should be attributed to deny-all")
	})

	t.Run("manifest only - no audit.jsonl", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		auditDir := filepath.Join(dir, "sandbox", "firewall", "audit")
		require.NoError(t, os.MkdirAll(auditDir, 0755))

		manifest := PolicyManifest{
			Version:        1,
			Rules:          []FirewallPolicyRule{{ID: "r1", Order: 1, Action: "allow", Domains: []string{".example.com"}}},
			SSLBumpEnabled: true,
		}
		manifestData, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(auditDir, "policy-manifest.json"), manifestData, 0644))

		analysis, err := analyzeFirewallPolicy(dir, false)
		require.NoError(t, err, "Should not error with manifest only")
		require.NotNil(t, analysis, "Should return analysis with manifest-only data")
		assert.Contains(t, analysis.PolicySummary, "SSL Bump enabled", "Should reflect SSL Bump enabled")
	})

	t.Run("no artifacts returns nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		analysis, err := analyzeFirewallPolicy(dir, false)
		require.NoError(t, err, "Should not error when no artifacts found")
		assert.Nil(t, analysis, "Should return nil when no artifacts found")
	})
}

func TestDomainMatchesRegex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		domain   string
		patterns []string
		expected bool
	}{
		{
			name:     "valid pattern that matches",
			domain:   "api.github.com",
			patterns: []string{`^api\.github\.com$`},
			expected: true,
		},
		{
			name:     "valid pattern that does not match",
			domain:   "evil.com",
			patterns: []string{`^api\.github\.com$`},
			expected: false,
		},
		{
			name:     "invalid pattern is skipped and returns false",
			domain:   "github.com",
			patterns: []string{`[invalid`},
			expected: false,
		},
		{
			name:     "invalid pattern skipped, valid second pattern matches",
			domain:   "github.com",
			patterns: []string{`[invalid`, `^github\.com$`},
			expected: true,
		},
		{
			name:     "invalid pattern skipped, no other pattern matches",
			domain:   "github.com",
			patterns: []string{`[invalid`, `^other\.com$`},
			expected: false,
		},
		{
			name:     "empty patterns list returns false",
			domain:   "github.com",
			patterns: []string{},
			expected: false,
		},
		{
			name:     "wildcard regex pattern",
			domain:   "anything.example.com",
			patterns: []string{`^.*\.example\.com$`},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := domainMatchesRegex(tt.domain, tt.patterns)
			assert.Equal(t, tt.expected, result, "domainMatchesRegex(%q, %v) should return %v", tt.domain, tt.patterns, tt.expected)
		})
	}
}
