//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

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
				// Copilot defaults should also be included
				"api.github.com",
				"github.com",
				"raw.githubusercontent.com",
				"registry.npmjs.org",
			},
			unexpectedDomain: "",
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
				// Claude now has its own default domains with AWF support
				"api.github.com",
				"anthropic.com",
				"api.anthropic.com",
			},
			// No unexpected domains - Claude has its own defaults
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
			expectedDomains: []string{
				// Should have Copilot defaults
				"api.github.com",
				"github.com",
				"raw.githubusercontent.com",
				// Note: network: defaults for Copilot doesn't expand ecosystem domains
				// in GetCopilotAllowedDomains - it only merges when network.allowed has values
			},
			unexpectedDomain: "",
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
			expectedDomains: []string{
				// Should have Copilot defaults
				"api.github.com",
				"github.com",
				"raw.githubusercontent.com",
				// Note: nil network for Copilot only returns Copilot defaults
			},
			unexpectedDomain: "",
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

// TestManualAllowedDomainsHasPriority tests that manually configured allowed-domains
// takes precedence over network configuration
func TestManualAllowedDomainsHasPriority(t *testing.T) {
	tests := []struct {
		name             string
		workflow         string
		expectedDomains  []string
		unexpectedDomain string
	}{
		{
			name: "Manual allowed-domains overrides network config",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
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

Test that manual allowed-domains takes precedence.
`,
			expectedDomains: []string{
				"manual-domain.com",
				"override.org",
			},
			// Network domains and Copilot defaults should NOT be included
			unexpectedDomain: "example.com",
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
				"api.github.com", // Copilot default
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
		name            string
		engineID        string
		networkPerms    *NetworkPermissions
		expectedDomains []string
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
				"api.github.com", // Copilot default
				"github.com",     // Copilot default
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
			name:         "Copilot with nil network",
			engineID:     "copilot",
			networkPerms: nil,
			expectedDomains: []string{
				"api.github.com",            // Copilot default
				"github.com",                // Copilot default
				"raw.githubusercontent.com", // Copilot default
				// Note: When network is nil, GetCopilotAllowedDomains only returns Copilot defaults
				// It does NOT include ecosystem defaults
			},
		},
		{
			name:         "Claude with nil network",
			engineID:     "claude",
			networkPerms: nil,
			expectedDomains: []string{
				"json-schema.org",    // ecosystem default
				"archive.ubuntu.com", // ecosystem default
			},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a compiler and workflow data
			compiler := NewCompiler()
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: tt.engineID,
				},
				NetworkPermissions: tt.networkPerms,
			}

			// Call the function
			domainsStr := compiler.computeAllowedDomainsForSanitization(data)

			// Verify expected domains are present
			for _, expectedDomain := range tt.expectedDomains {
				if !strings.Contains(domainsStr, expectedDomain) {
					t.Errorf("Expected domain '%s' not found in result: %s", expectedDomain, domainsStr)
				}
			}
		})
	}
}
