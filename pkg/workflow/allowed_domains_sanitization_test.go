//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

// extractQuotedCSV returns the comma-separated domain list embedded inside
// the first pair of double-quotes in line. Used to enable exact-entry checks
// (avoiding substring false-positives like "corp.example.com" matching "copilot.corp.example.com").
func extractQuotedCSV(line string) string {
	start := strings.Index(line, `"`)
	if start < 0 {
		return line
	}
	rest := line[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// TestAllowedDomainsFromNetworkConfig tests that GH_AW_ALLOWED_DOMAINS is computed
// from network configuration for sanitization
func TestAllowedDomainsFromNetworkConfig(t *testing.T) {
	tests := []struct {
		name             string
		workflow         string
		expectedDomains  []string // domains that should be in GH_AW_ALLOWED_DOMAINS
		unexpectedDomain string   // domain that should NOT be in GH_AW_ALLOWED_DOMAINS
	}{
		{
			name: "Copilot with network permissions",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
network:
  allowed:
    - example.com
    - test.org
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with network permissions.
`,
			expectedDomains: []string{
				"example.com",
				"test.org",
			},
			unexpectedDomain: "registry.npmjs.org",
		},
		{
			name: "Claude with network permissions",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
network:
  allowed:
    - example.com
    - test.org
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with network permissions.
`,
			expectedDomains: []string{
				"example.com",
				"test.org",
			},
			unexpectedDomain: "",
		},
		{
			name: "Copilot with defaults network mode",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network: defaults
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with defaults network.
`,
			expectedDomains:  []string{"json-schema.org", "archive.ubuntu.com"},
			unexpectedDomain: "api.githubcopilot.com",
		},
		{
			name: "Copilot without network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow without network config.
`,
			expectedDomains:  []string{},
			unexpectedDomain: "api.githubcopilot.com",
		},
		{
			name: "Claude with ecosystem identifier",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
network:
  allowed:
    - python
    - node
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with ecosystem identifiers.
`,
			expectedDomains: []string{
				// Python ecosystem
				"pypi.org",
				"files.pythonhosted.org",
				// Node ecosystem
				"npmjs.org",
				"registry.npmjs.org",
			},
			unexpectedDomain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test
			tmpDir := testutil.TempDir(t, "allowed-domains-test")

			// Create a test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Check if GH_AW_ALLOWED_DOMAINS is set in the Ingest agent output step
			if !strings.Contains(lockStr, "GH_AW_ALLOWED_DOMAINS:") {
				t.Error("Expected GH_AW_ALLOWED_DOMAINS environment variable in lock file")
			}

			// Extract the GH_AW_ALLOWED_DOMAINS value
			lines := strings.Split(lockStr, "\n")
			var domainsLine string
			for _, line := range lines {
				if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
					domainsLine = line
					break
				}
			}

			if domainsLine == "" {
				t.Fatal("GH_AW_ALLOWED_DOMAINS not found in lock file")
			}

			// Check that expected domains are present
			for _, expectedDomain := range tt.expectedDomains {
				if !strings.Contains(domainsLine, expectedDomain) {
					t.Errorf("Expected domain '%s' not found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", expectedDomain, domainsLine)
				}
			}

			// Check that unexpected domain is NOT present
			if tt.unexpectedDomain != "" {
				if strings.Contains(domainsLine, tt.unexpectedDomain) {
					t.Errorf("Unexpected domain '%s' found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", tt.unexpectedDomain, domainsLine)
				}
			}
		})
	}
}

// TestManualAllowedDomainsUnionWithNetworkConfig tests that manually configured allowed-domains
// unions with network configuration (not overrides it)
func TestManualAllowedDomainsUnionWithNetworkConfig(t *testing.T) {
	tests := []struct {
		name             string
		workflow         string
		expectedDomains  []string
		unexpectedDomain string
	}{
		{
			name: "Manual allowed-domains unions with network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
network:
  allowed:
    - example.com
    - python
safe-outputs:
  create-issue:
  allowed-domains:
    - manual-domain.com
    - override.org
---

# Test Workflow

Test that manual allowed-domains unions with network config.
`,
			expectedDomains: []string{
				"manual-domain.com",
				"override.org",
				"example.com", // from network.allowed - still present (union)
			},
			// No domain should be absent
			unexpectedDomain: "",
		},
		{
			name: "Empty allowed-domains uses network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
network:
  allowed:
    - example.com
safe-outputs:
  create-issue:
---

# Test Workflow

Test that empty allowed-domains falls back to network config.
`,
			expectedDomains: []string{
				"example.com",
			},
			unexpectedDomain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test
			tmpDir := testutil.TempDir(t, "manual-domains-test")

			// Create a test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Check if GH_AW_ALLOWED_DOMAINS is set
			if !strings.Contains(lockStr, "GH_AW_ALLOWED_DOMAINS:") {
				t.Error("Expected GH_AW_ALLOWED_DOMAINS environment variable in lock file")
			}

			// Extract the GH_AW_ALLOWED_DOMAINS value
			lines := strings.Split(lockStr, "\n")
			var domainsLine string
			for _, line := range lines {
				if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
					domainsLine = line
					break
				}
			}

			if domainsLine == "" {
				t.Fatal("GH_AW_ALLOWED_DOMAINS not found in lock file")
			}

			// Check that expected domains are present
			for _, expectedDomain := range tt.expectedDomains {
				if !strings.Contains(domainsLine, expectedDomain) {
					t.Errorf("Expected domain '%s' not found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", expectedDomain, domainsLine)
				}
			}

			// Check that unexpected domain is NOT present
			if tt.unexpectedDomain != "" {
				if strings.Contains(domainsLine, tt.unexpectedDomain) {
					t.Errorf("Unexpected domain '%s' found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", tt.unexpectedDomain, domainsLine)
				}
			}
		})
	}
}

// TestComputeAllowedDomainsForSanitization tests the computeAllowedDomainsForSanitization function
func TestComputeAllowedDomainsForSanitization(t *testing.T) {
	tests := []struct {
		name              string
		engineID          string
		apiTarget         string
		networkPerms      *NetworkPermissions
		expectedDomains   []string
		unexpectedDomains []string
	}{
		{
			name:     "Copilot with custom domains",
			engineID: "copilot",
			networkPerms: &NetworkPermissions{
				Allowed: []string{"example.com", "test.org"},
			},
			expectedDomains: []string{
				"example.com",
				"test.org",
			},
		},
		{
			name:     "Claude with custom domains",
			engineID: "claude",
			networkPerms: &NetworkPermissions{
				Allowed: []string{"example.com", "test.org"},
			},
			expectedDomains: []string{
				"example.com",
				"test.org",
			},
		},
		{
			name:            "Copilot with nil network",
			engineID:        "copilot",
			networkPerms:    nil,
			expectedDomains: []string{},
		},
		{
			name:            "Claude with nil network",
			engineID:        "claude",
			networkPerms:    nil,
			expectedDomains: []string{},
		},
		{
			name:     "Codex with custom domains",
			engineID: "codex",
			networkPerms: &NetworkPermissions{
				Allowed: []string{"example.com"},
			},
			expectedDomains: []string{
				"example.com",
			},
		},
		{
			name:         "Copilot with GHES api-target includes api and base domains",
			engineID:     "copilot",
			apiTarget:    "api.acme.ghe.com",
			networkPerms: nil,
			expectedDomains: []string{
				"api.acme.ghe.com", // GHES API domain
				"acme.ghe.com",     // GHES base domain (derived from api-target)
			},
		},
		{
			name:         "non-api prefix api-target only adds the configured hostname",
			engineID:     "copilot",
			apiTarget:    "copilot.corp.example.com",
			networkPerms: nil,
			expectedDomains: []string{
				"copilot.corp.example.com", // configured hostname
			},
			unexpectedDomains: []string{
				"corp.example.com", // base hostname should NOT be added for non-api. prefix
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a compiler and workflow data
			compiler := NewCompiler()
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        tt.engineID,
					APITarget: tt.apiTarget,
				},
				NetworkPermissions: tt.networkPerms,
			}

			// Call the function
			domainsStr, err := compiler.computeAllowedDomainsForSanitization(data)
			require.NoError(t, err, "computeAllowedDomainsForSanitization should not return an error for valid test data")
			if len(tt.expectedDomains) == 0 {
				require.Empty(t, domainsStr, "expected no domains without network configuration")
			}

			// Verify expected domains are present (substring match is fine here since domain names
			// in a CSV string that are exact entries won't appear as substrings of other entries
			// when checking expected ones – we only need exact match for the negative "not present" check)
			for _, expectedDomain := range tt.expectedDomains {
				if !strings.Contains(domainsStr, expectedDomain) {
					t.Errorf("Expected domain '%s' not found in result: %s", expectedDomain, domainsStr)
				}
			}

			// Verify unexpected domains are absent using exact membership (not substring)
			// to avoid false positives where "corp.example.com" matches "copilot.corp.example.com"
			parts := strings.Split(domainsStr, ",")
			for _, unexpectedDomain := range tt.unexpectedDomains {
				if slices.Contains(parts, unexpectedDomain) {
					t.Errorf("Unexpected domain '%s' found in result: %s", unexpectedDomain, domainsStr)
				}
			}
		})
	}
}

// TestAPITargetDomainsInCompiledWorkflow is a regression test verifying that when engine.api-target
// is configured, both --allow-domains (AWF firewall flag) and GH_AW_ALLOWED_DOMAINS (sanitization
// env var) in the compiled lock file contain the api-target hostname and its derived base hostname.
func TestAPITargetDomainsInCompiledWorkflow(t *testing.T) {
	tests := []struct {
		name              string
		workflow          string
		expectedDomains   []string
		unexpectedDomains []string
	}{
		{
			name: "GHES api-target adds api and base domains to allow-domains and GH_AW_ALLOWED_DOMAINS",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  api-target: api.acme.ghe.com
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with GHES api-target.
`,
			expectedDomains: []string{
				"api.acme.ghe.com", // GHES API domain
				"acme.ghe.com",     // GHES base domain derived from api-target
			},
		},
		{
			name: "non-api prefix api-target only adds the configured hostname",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  api-target: copilot.corp.example.com
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with non-api prefix api-target.
`,
			expectedDomains: []string{
				"copilot.corp.example.com", // configured hostname
			},
			unexpectedDomains: []string{
				"corp.example.com", // base hostname should NOT be added for non-api. prefix
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "api-target-domains-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockStr := string(lockContent)

			// Check allowDomains in AWF JSON config contains expected domains.
			// Network settings are now expressed via --config JSON file instead of
			// --allow-domains CLI flag (see BuildAWFConfigJSON).
			// The JSON is shell-escaped in the lock file, so try both the unescaped
			// ("allowDomains":[) and escaped (\"allowDomains\":[) forms.
			allowDomainsPrefix := `"allowDomains":[`
			allowDomainsPrefixEscaped := `\"allowDomains\":[`
			allowDomainsIdx := strings.Index(lockStr, allowDomainsPrefix)
			if allowDomainsIdx < 0 {
				allowDomainsPrefix = allowDomainsPrefixEscaped
				allowDomainsIdx = strings.Index(lockStr, allowDomainsPrefixEscaped)
			}
			if allowDomainsIdx < 0 {
				t.Fatal("allowDomains key not found in compiled lock file")
			}
			// Extract the JSON array content for more targeted checking.
			arrayStart := allowDomainsIdx + len(allowDomainsPrefix)
			allowDomainsEnd := strings.Index(lockStr[arrayStart:], "]")
			if allowDomainsEnd < 0 {
				allowDomainsEnd = len(lockStr) - arrayStart
			}
			allowDomainsSection := lockStr[arrayStart : arrayStart+allowDomainsEnd]

			// containsJSONDomain checks for a domain as a JSON string value, handling both
			// escaped (\"domain\") and unescaped ("domain") forms in the lock file.
			containsJSONDomain := func(section, domain string) bool {
				return strings.Contains(section, `"`+domain+`"`) ||
					strings.Contains(section, `\"`+domain+`\"`)
			}

			for _, domain := range tt.expectedDomains {
				if !containsJSONDomain(allowDomainsSection, domain) {
					t.Errorf("Expected domain %q not found in allowDomains.\nSection: %s", domain, allowDomainsSection)
				}
			}
			// Use exact JSON string matching for "not present" checks to avoid false positives
			// (e.g. "corp.example.com" would substring-match "copilot.corp.example.com").
			for _, domain := range tt.unexpectedDomains {
				if containsJSONDomain(allowDomainsSection, domain) {
					t.Errorf("Unexpected domain %q found in allowDomains.\nSection: %s", domain, allowDomainsSection)
				}
			}

			// Check GH_AW_ALLOWED_DOMAINS env var contains expected domains
			lines := strings.Split(lockStr, "\n")
			var domainsLine string
			for _, line := range lines {
				if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
					domainsLine = line
					break
				}
			}
			if domainsLine == "" {
				t.Fatal("GH_AW_ALLOWED_DOMAINS not found in compiled lock file")
			}

			for _, domain := range tt.expectedDomains {
				if !strings.Contains(domainsLine, domain) {
					t.Errorf("Expected domain %q not found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", domain, domainsLine)
				}
			}
			// Use exact CSV membership for "not present" checks
			allowedDomainsEnvCSV := extractQuotedCSV(domainsLine)
			allowedEnvParts := strings.Split(allowedDomainsEnvCSV, ",")
			for _, domain := range tt.unexpectedDomains {
				if slices.Contains(allowedEnvParts, domain) {
					t.Errorf("Unexpected domain %q found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", domain, domainsLine)
				}
			}
		})
	}
}

// TestGitHubCopilotBaseURLInCompiledWorkflow verifies that when GITHUB_COPILOT_BASE_URL is set
// in engine.env (without an explicit engine.api-target), the compiled lock file contains
// --copilot-api-target and includes the extracted hostname in both --allow-domains and
// GH_AW_ALLOWED_DOMAINS — matching the OPENAI_BASE_URL/ANTHROPIC_BASE_URL pattern for other engines.
func TestGitHubCopilotBaseURLInCompiledWorkflow(t *testing.T) {
	workflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  env:
    GITHUB_COPILOT_BASE_URL: "https://copilot-proxy.corp.example.com"
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with GITHUB_COPILOT_BASE_URL in engine.env.
`

	tmpDir := testutil.TempDir(t, "copilot-base-url-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// The copilot API target should be derived from the env var and present in the
	// AWF JSON config (apiProxy.targets.copilot.host) rather than as a --copilot-api-target
	// CLI flag, since network/proxy settings are now expressed via --config JSON file.
	copilotHostUnescaped := `"copilot":{"host":"copilot-proxy.corp.example.com"}`
	copilotHostEscaped := `\"copilot\":{\"host\":\"copilot-proxy.corp.example.com\"}`
	if !strings.Contains(lockStr, copilotHostUnescaped) && !strings.Contains(lockStr, copilotHostEscaped) {
		t.Error("Expected copilot API target to be derived from GITHUB_COPILOT_BASE_URL in AWF config JSON")
	}

	// Extracted hostname should appear in the allowDomains list inside the AWF JSON config.
	// The AWF JSON config embeds the allowDomains array as a comma-separated list.
	// We search for the allowDomains key followed by its opening "[" and verify the hostname
	// appears before the closing "]" of that specific array.
	allowDomainsPrefix := `"allowDomains":[`
	allowDomainsPrefixEscaped := `\"allowDomains\":[`
	allowDomainsIdx := strings.Index(lockStr, allowDomainsPrefix)
	if allowDomainsIdx < 0 {
		allowDomainsPrefix = allowDomainsPrefixEscaped
		allowDomainsIdx = strings.Index(lockStr, allowDomainsPrefixEscaped)
	}
	if allowDomainsIdx < 0 {
		t.Fatal("allowDomains key not found in compiled lock file")
	}
	// Start searching for "]" from after the opening "[".
	arrayStart := allowDomainsIdx + len(allowDomainsPrefix)
	allowDomainsEnd := strings.Index(lockStr[arrayStart:], "]")
	if allowDomainsEnd < 0 {
		allowDomainsEnd = len(lockStr) - arrayStart
	}
	allowDomainsSection := lockStr[arrayStart : arrayStart+allowDomainsEnd]
	if !strings.Contains(allowDomainsSection, "copilot-proxy.corp.example.com") {
		t.Errorf("Expected hostname from GITHUB_COPILOT_BASE_URL in allowDomains.\nSection: %s", allowDomainsSection)
	}

	// Extracted hostname should appear in GH_AW_ALLOWED_DOMAINS
	lines := strings.Split(lockStr, "\n")
	var domainsLine string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
			domainsLine = line
			break
		}
	}
	if domainsLine == "" {
		t.Fatal("GH_AW_ALLOWED_DOMAINS not found in compiled lock file")
	}
	if !strings.Contains(domainsLine, "copilot-proxy.corp.example.com") {
		t.Errorf("Expected hostname from GITHUB_COPILOT_BASE_URL in GH_AW_ALLOWED_DOMAINS.\nLine: %s", domainsLine)
	}
}

// TestAPITargetDomainsInThreatDetectionStep is a regression test verifying that when engine.api-target
// is configured, the threat detection AWF invocation in the compiled lock file also receives
// --copilot-api-target and includes the GHE domains in its --allow-domains list.
// Regression test for: Threat detection AWF run missing --copilot-api-target on data residency.
func TestAPITargetDomainsInThreatDetectionStep(t *testing.T) {
	workflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  api-target: api.contoso-aw.ghe.com
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with GHE data residency api-target and threat detection.
`

	tmpDir := testutil.TempDir(t, "api-target-threat-detection-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Verify copilot api-target appears at least twice in the AWF JSON config:
	// once for the main agent AWF run and once for the threat detection AWF run.
	// API proxy settings are now expressed via --config JSON file instead of
	// --copilot-api-target CLI flag (see BuildAWFConfigJSON).
	// The JSON is shell-escaped in the lock file, so try both unescaped and escaped forms.
	apiTargetUnescaped := `"copilot":{"host":"api.contoso-aw.ghe.com"}`
	apiTargetEscaped := `\"copilot\":{\"host\":\"api.contoso-aw.ghe.com\"}`
	apiTargetCount := strings.Count(lockStr, apiTargetUnescaped)
	if apiTargetCount == 0 {
		apiTargetCount = strings.Count(lockStr, apiTargetEscaped)
	}
	if apiTargetCount < 2 {
		t.Errorf("Expected copilot api-target to appear in both the main agent and threat detection AWF JSON configs (at least 2 times), but found %d occurrence(s).", apiTargetCount)
	}

	// Find all allowDomains occurrences in AWF JSON config and verify each contains the GHE domains.
	// api.contoso-aw.ghe.com triggers base-domain derivation, so both the API domain
	// and the base domain (contoso-aw.ghe.com) must appear in each AWF invocation.
	// The JSON is shell-escaped in the lock file, so try both unescaped and escaped key forms.
	requiredDomains := []string{"api.contoso-aw.ghe.com", "contoso-aw.ghe.com"}
	allowDomainsPrefix := `"allowDomains":[`
	allowDomainsPrefixEscaped := `\"allowDomains\":[`
	// Use whichever prefix form is present in the lock file.
	if strings.Index(lockStr, allowDomainsPrefix) < 0 {
		allowDomainsPrefix = allowDomainsPrefixEscaped
	}
	remaining := lockStr
	occurrenceIdx := 0
	for {
		idx := strings.Index(remaining, allowDomainsPrefix)
		if idx < 0 {
			break
		}
		occurrenceIdx++
		arrayStart := idx + len(allowDomainsPrefix)
		arrayEnd := strings.Index(remaining[arrayStart:], "]")
		if arrayEnd < 0 {
			arrayEnd = len(remaining) - arrayStart
		}
		section := remaining[arrayStart : arrayStart+arrayEnd]
		for _, domain := range requiredDomains {
			// Handle both escaped (\"domain\") and unescaped ("domain") forms.
			if !strings.Contains(section, `"`+domain+`"`) && !strings.Contains(section, `\"`+domain+`\"`) {
				t.Errorf("allowDomains occurrence #%d is missing GHE domain %q.\nSection: %s", occurrenceIdx, domain, section)
			}
		}
		remaining = remaining[arrayStart+arrayEnd:]
	}

	if occurrenceIdx < 2 {
		t.Errorf("Expected at least 2 allowDomains occurrences (main agent + threat detection), found %d", occurrenceIdx)
	}
}

func TestCopilotProviderBaseURLInThreatDetectionStep(t *testing.T) {
	workflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ secrets.PROVIDER_BASE_URL }}
network:
  allowed:
    - defaults
    - llm.corp.example.com
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow

Test workflow with COPILOT_PROVIDER_BASE_URL in engine.env and provider host in network.allowed.
`

	tmpDir := testutil.TempDir(t, "copilot-provider-threat-detection-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	requiredDomain := "llm.corp.example.com"
	allowDomainsPrefix := `"allowDomains":[`
	allowDomainsPrefixEscaped := `\"allowDomains\":[`
	if !strings.Contains(lockStr, allowDomainsPrefix) {
		allowDomainsPrefix = allowDomainsPrefixEscaped
	}

	remaining := lockStr
	occurrenceIdx := 0
	for {
		idx := strings.Index(remaining, allowDomainsPrefix)
		if idx < 0 {
			break
		}
		occurrenceIdx++
		arrayStart := idx + len(allowDomainsPrefix)
		arrayEnd := strings.Index(remaining[arrayStart:], "]")
		if arrayEnd < 0 {
			arrayEnd = len(remaining) - arrayStart
		}
		section := remaining[arrayStart : arrayStart+arrayEnd]
		if !strings.Contains(section, `"`+requiredDomain+`"`) && !strings.Contains(section, `\"`+requiredDomain+`\"`) {
			t.Errorf("allowDomains occurrence #%d is missing BYOK provider domain %q.\nSection: %s", occurrenceIdx, requiredDomain, section)
		}
		remaining = remaining[arrayStart+arrayEnd:]
	}

	if occurrenceIdx < 2 {
		t.Errorf("Expected at least 2 allowDomains occurrences (main agent + threat detection), found %d", occurrenceIdx)
	}

	lines := strings.Split(lockStr, "\n")
	var domainsLine string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
			domainsLine = line
			break
		}
	}
	if domainsLine == "" {
		t.Fatal("GH_AW_ALLOWED_DOMAINS not found in compiled lock file")
	}
	if !strings.Contains(domainsLine, requiredDomain) {
		t.Errorf("Expected BYOK provider hostname in GH_AW_ALLOWED_DOMAINS.\nLine: %s", domainsLine)
	}
}

// TestAllowedDomainsUnionWithNetworkConfig tests that safe-outputs.allowed-domains
// is unioned with network.allowed and always includes localhost and github.com
func TestAllowedDomainsUnionWithNetworkConfig(t *testing.T) {
	tests := []struct {
		name            string
		workflow        string
		expectedDomains []string
	}{
		{
			name: "allowed-domains unioned with network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
network:
  allowed:
    - example.com
safe-outputs:
  create-issue:
  allowed-domains:
    - extra-domain.com
---

# Test Workflow

Test allowed-domains union with network config.
`,
			expectedDomains: []string{
				"extra-domain.com", // from allowed-domains
				"example.com",      // from network.allowed
				"localhost",        // always included
				"github.com",       // always included
			},
		},
		{
			name: "allowed-domains supports ecosystem identifiers",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
  allowed-domains:
    - dev-tools
    - python
---

# Test Workflow

Test allowed-domains with ecosystem identifiers.
`,
			expectedDomains: []string{
				"codecov.io", // from dev-tools ecosystem
				"snyk.io",    // from dev-tools ecosystem
				"pypi.org",   // from python ecosystem
				"localhost",  // always included
				"github.com", // always included
			},
		},
		{
			name: "allowed-domains does not override network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
network:
  allowed:
    - network-domain.com
safe-outputs:
  create-issue:
  allowed-domains:
    - url-domain.com
---

# Test Workflow

Test that allowed-domains does not override network config.
`,
			expectedDomains: []string{
				"url-domain.com",     // from allowed-domains
				"network-domain.com", // from network.allowed - still present (union)
				"localhost",          // always included
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "allowed-domains-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockStr := string(lockContent)

			if !strings.Contains(lockStr, "GH_AW_ALLOWED_DOMAINS:") {
				t.Error("Expected GH_AW_ALLOWED_DOMAINS environment variable in lock file")
			}

			lines := strings.Split(lockStr, "\n")
			var domainsLine string
			for _, line := range lines {
				if strings.Contains(line, "GH_AW_ALLOWED_DOMAINS:") {
					domainsLine = line
					break
				}
			}

			if domainsLine == "" {
				t.Fatal("GH_AW_ALLOWED_DOMAINS not found in lock file")
			}

			for _, expectedDomain := range tt.expectedDomains {
				if !strings.Contains(domainsLine, expectedDomain) {
					t.Errorf("Expected domain %q not found in GH_AW_ALLOWED_DOMAINS.\nLine: %s", expectedDomain, domainsLine)
				}
			}
		})
	}
}
