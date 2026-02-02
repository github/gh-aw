//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestProtocolSpecificDomainsIntegration tests protocol-specific domain filtering end-to-end
func TestProtocolSpecificDomainsIntegration(t *testing.T) {
	tests := []struct {
		name            string
		workflow        string
		expectedDomains []string // domains that should appear in --allow-domains
		checkAWFArgs    bool     // whether to check AWF arguments
	}{
		{
			name: "Copilot with protocol-specific domains",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - https://secure.example.com
    - http://legacy.example.com
    - example.org
---

# Test Workflow

Test protocol-specific domain filtering.
`,
			expectedDomains: []string{
				"https://secure.example.com",
				"http://legacy.example.com",
				"example.org",
				"api.github.com", // Copilot default
			},
			checkAWFArgs: true,
		},
		{
			name: "Claude with HTTPS-only wildcard domains",
			workflow: `---
on: push
permissions:
  contents: read
engine: claude
strict: false
network:
  allowed:
    - https://*.api.example.com
    - https://secure.example.com
---

# Test Workflow

Test HTTPS-only wildcard domains.
`,
			expectedDomains: []string{
				"https://*.api.example.com",
				"https://secure.example.com",
				"anthropic.com", // Claude default
			},
			checkAWFArgs: true,
		},
		{
			name: "Mixed protocol domains in safe-outputs",
			workflow: `---
on: push
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
network:
  allowed:
    - https://secure.example.com
    - http://legacy.example.com
safe-outputs:
  create-issue:
  allowed-domains:
    - https://secure.example.com
    - http://legacy.example.com
---

# Test Workflow

Test protocol-specific domains in safe-outputs.
`,
			expectedDomains: []string{
				"https://secure.example.com",
				"http://legacy.example.com",
			},
			checkAWFArgs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory and workflow file
			tmpDir := t.TempDir()
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(workflowPath, []byte(tt.workflow), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the compiled lock file
			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockYAML := string(lockContent)

			// Verify expected domains are present
			for _, domain := range tt.expectedDomains {
				if !strings.Contains(lockYAML, domain) {
					t.Errorf("Expected domain %q not found in compiled workflow", domain)
				}
			}

			// If checking AWF args, verify --allow-domains flag is present
			if tt.checkAWFArgs {
				if !strings.Contains(lockYAML, "--allow-domains") {
					t.Error("Expected --allow-domains flag in compiled workflow")
				}
			}

			// Verify protocol prefixes are preserved in the lock file
			for _, domain := range tt.expectedDomains {
				if strings.HasPrefix(domain, "https://") || strings.HasPrefix(domain, "http://") {
					// The domain with protocol should appear in the lock file
					if !strings.Contains(lockYAML, domain) {
						t.Errorf("Protocol-specific domain %q should be preserved in lock file", domain)
					}
				}
			}
		})
	}
}

// TestProtocolSpecificDomainsValidationIntegration tests that invalid protocols are rejected
func TestProtocolSpecificDomainsValidationIntegration(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		wantErr  bool
	}{
		{
			name: "Invalid protocol - FTP",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - ftp://example.com
---

# Test Workflow

Test invalid protocol rejection.
`,
			wantErr: true,
		},
		{
			name: "Invalid protocol - ws",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - ws://example.com
---

# Test Workflow

Test websocket protocol rejection.
`,
			wantErr: true,
		},
		{
			name: "Valid HTTPS protocol",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - https://example.com
---

# Test Workflow

Test valid HTTPS protocol.
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory and workflow file
			tmpDir := t.TempDir()
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(workflowPath, []byte(tt.workflow), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)

			if tt.wantErr && err == nil {
				t.Error("Expected compilation error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestBackwardCompatibilityNoProtocol tests that domains without protocols still work
func TestBackwardCompatibilityNoProtocol(t *testing.T) {
	workflow := `---
on: push
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - example.com
    - "*.example.org"
    - api.test.com
---

# Test Workflow

Test backward compatibility with domains without protocols.
`

	// Create temporary directory and workflow file
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflow), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockYAML := string(lockContent)

	// Verify domains without protocols are still present
	expectedDomains := []string{
		"example.com",
		"*.example.org",
		"api.test.com",
	}

	for _, domain := range expectedDomains {
		if !strings.Contains(lockYAML, domain) {
			t.Errorf("Expected domain %q not found in compiled workflow", domain)
		}
	}

	// Verify --allow-domains flag is present
	if !strings.Contains(lockYAML, "--allow-domains") {
		t.Error("Expected --allow-domains flag in compiled workflow")
	}
}
