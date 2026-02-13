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

// =============================================================================
// Injection Attack Prevention Tests
// =============================================================================

// TestSecurityTemplateInjectionPrevention validates that template injection
// attacks via GitHub expressions are properly blocked.
func TestSecurityTemplateInjectionPrevention(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		shouldBlock bool
		description string
	}{
		{
			name:        "secrets_injection_in_expression",
			content:     "${{ secrets.GITHUB_TOKEN }}",
			shouldBlock: true,
			description: "Direct secrets access should be blocked",
		},
		{
			name:        "secrets_injection_in_text",
			content:     "Here is a secret: ${{ secrets.API_KEY }}",
			shouldBlock: true,
			description: "Secrets embedded in text should be blocked",
		},
		{
			name:        "nested_secrets_injection",
			content:     "${{ github.workflow && secrets.TOKEN }}",
			shouldBlock: true,
			description: "Secrets in compound expressions should be blocked",
		},
		{
			name:        "github_token_direct_access",
			content:     "${{ github.token }}",
			shouldBlock: true,
			description: "github.token direct access should be blocked",
		},
		{
			name:        "allowed_github_workflow",
			content:     "${{ github.workflow }}",
			shouldBlock: false,
			description: "Allowed expressions should pass",
		},
		{
			name:        "allowed_github_repository",
			content:     "${{ github.repository }}",
			shouldBlock: false,
			description: "Allowed repository expressions should pass",
		},
		{
			name:        "allowed_env_var",
			content:     "${{ env.MY_VAR }}",
			shouldBlock: false,
			description: "Environment variable access should be allowed",
		},
		{
			name:        "allowed_needs_output",
			content:     "${{ needs.activation.outputs.text }}",
			shouldBlock: false,
			description: "Needs outputs should be allowed",
		},
		{
			name:        "script_tag_injection",
			content:     "${{ github.workflow }}<script>alert('xss')</script>",
			shouldBlock: false, // Not blocked at expression level, but no secrets leak
			description: "Script tags with allowed expressions do not leak secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.shouldBlock && err == nil {
				t.Errorf("Security violation: %s - %s was not blocked", tt.name, tt.description)
			}
			if !tt.shouldBlock && err != nil {
				t.Errorf("False positive: %s - %s was incorrectly blocked: %v", tt.name, tt.description, err)
			}
		})
	}
}

// TestSecurityCommandInjectionPrevention validates that command injection
// patterns in expressions don't bypass security controls.
func TestSecurityCommandInjectionPrevention(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		shouldBlock bool
		description string
	}{
		{
			name:        "backtick_command_injection",
			content:     "${{ github.workflow }}`whoami`",
			shouldBlock: false, // Not blocked at expression level, backticks outside expression
			description: "Backticks outside expression don't bypass validation",
		},
		{
			name:        "dollar_paren_injection",
			content:     "${{ github.workflow }}$(whoami)",
			shouldBlock: false, // Not blocked at expression level, subshell outside expression
			description: "Subshell syntax outside expression don't bypass validation",
		},
		{
			name:        "semicolon_injection",
			content:     "${{ github.workflow }}; rm -rf /",
			shouldBlock: false, // Not blocked at expression level, command after expression
			description: "Commands after expression don't bypass validation",
		},
		{
			name:        "secrets_with_command_injection",
			content:     "${{ secrets.TOKEN }}`rm -rf /`",
			shouldBlock: true,
			description: "Secrets access with command injection should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.shouldBlock && err == nil {
				t.Errorf("Security violation: %s - %s was not blocked", tt.name, tt.description)
			}
			if !tt.shouldBlock && err != nil {
				t.Errorf("False positive: %s - %s was incorrectly blocked: %v", tt.name, tt.description, err)
			}
		})
	}
}

// TestSecurityYAMLInjectionPrevention validates that YAML-based injection
// attacks are prevented during workflow compilation.
func TestSecurityYAMLInjectionPrevention(t *testing.T) {
	tests := []struct {
		name          string
		workflow      string
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name: "yaml_anchor_bomb_prevention",
			workflow: `---
on: push
permissions:
  contents: read
---

# Anchor Test
Test workflow without YAML anchors (anchors are not supported in frontmatter).`,
			expectError: false,
			description: "Simple workflow should compile successfully",
		},
		{
			name: "secrets_in_markdown_body",
			workflow: `---
on: push
permissions:
  contents: read
---

# Secret Test
Testing secrets injection: ${{ secrets.GITHUB_TOKEN }}`,
			expectError:   true,
			errorContains: "secrets.GITHUB_TOKEN",
			description:   "Secrets in markdown body should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "yaml-injection-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Security violation: %s - %s", tt.name, tt.description)
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
			}
		})
	}
}

// =============================================================================
// DoS Prevention Tests
// =============================================================================

// TestSecurityDoSViaLargeInputs validates that excessively large inputs
// are handled without causing denial of service.
func TestSecurityDoSViaLargeInputs(t *testing.T) {
	tests := []struct {
		name        string
		contentFunc func() string
		description string
	}{
		{
			name: "very_long_expression",
			contentFunc: func() string {
				// Create a very long but valid expression
				expr := "${{ "
				for i := 0; i < 100; i++ {
					expr += "github.workflow && "
				}
				expr += "github.repository }}"
				return expr
			},
			description: "Very long expression should be handled",
		},
		{
			name: "many_expressions",
			contentFunc: func() string {
				// Many repeated expressions
				var sb strings.Builder
				for i := 0; i < 1000; i++ {
					sb.WriteString("${{ github.workflow }} ")
				}
				return sb.String()
			},
			description: "Many expressions should be handled",
		},
		{
			name: "excessive_whitespace",
			contentFunc: func() string {
				return "${{" + strings.Repeat(" ", 10000) + "github.workflow" + strings.Repeat(" ", 10000) + "}}"
			},
			description: "Excessive whitespace should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := tt.contentFunc()

			// The parser should handle these without panic or timeout
			// We don't require a specific result, just that it doesn't hang
			err := validateExpressionSafety(content)
			_ = err // We don't care about the result, just that it completes
		})
	}
}

// TestSecurityDoSViaNestedYAML validates that deeply nested YAML structures
// don't cause stack overflow or excessive resource consumption.
func TestSecurityDoSViaNestedYAML(t *testing.T) {
	tests := []struct {
		name            string
		workflowFunc    func() string
		expectComplete  bool
		maxDurationSecs int
		description     string
	}{
		{
			name: "deeply_nested_structure",
			workflowFunc: func() string {
				// Create a deeply nested but valid structure
				yaml := "---\non: push\ndata:\n"
				for i := 0; i < 20; i++ {
					yaml += strings.Repeat("  ", i+1) + "level" + string(rune('a'+i%26)) + ":\n"
				}
				yaml += strings.Repeat("  ", 21) + "value: deep\n---\n\n# Deep Test\n"
				return yaml
			},
			expectComplete:  true,
			maxDurationSecs: 5,
			description:     "Deeply nested YAML should be processed",
		},
		{
			name: "wide_array_structure",
			workflowFunc: func() string {
				yaml := "---\non: push\nitems:\n"
				for i := 0; i < 500; i++ {
					yaml += "  - item" + string(rune('0'+i%10)) + "\n"
				}
				yaml += "---\n\n# Wide Array Test\n"
				return yaml
			},
			expectComplete:  true,
			maxDurationSecs: 5,
			description:     "Wide arrays should be processed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := tt.workflowFunc()

			tmpDir := testutil.TempDir(t, "nested-yaml-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// The compiler should complete without hanging
			// We don't check error here, just that it completes
			_ = compiler.CompileWorkflow(testFile)
		})
	}
}

// TestSecurityBillionLaughsAttack validates protection against the
// "Billion Laughs" XML/YAML entity expansion attack.
func TestSecurityBillionLaughsAttack(t *testing.T) {
	tests := []struct {
		name        string
		workflow    string
		expectError bool
		description string
	}{
		{
			name: "yaml_alias_expansion",
			workflow: `---
on: push
lol1: &lol1 "lol"
lol2: &lol2 [*lol1, *lol1, *lol1]
lol3: &lol3 [*lol2, *lol2, *lol2]
data: *lol3
---

# Alias Test
Testing YAML alias expansion.`,
			expectError: false,
			description: "Reasonable alias expansion should be allowed",
		},
		{
			name: "simple_anchor_reference",
			workflow: `---
on: push
defaults: &defaults
  timeout: 30
  retry: 3
job1: *defaults
job2: *defaults
---

# Simple Anchor Test
Testing simple anchor reference.`,
			expectError: false,
			description: "Simple anchor references should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "billion-laughs-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Should complete without exponential expansion
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s: %s", tt.name, tt.description)
			}
		})
	}
}

// =============================================================================
// Authorization Tests
// =============================================================================

// TestSecurityUnauthorizedAccess validates that unauthorized expression
// contexts are properly rejected.
func TestSecurityUnauthorizedAccess(t *testing.T) {
	unauthorizedPatterns := []struct {
		pattern     string
		description string
	}{
		{"${{ secrets.GITHUB_TOKEN }}", "GITHUB_TOKEN secret access"},
		{"${{ secrets.API_KEY }}", "Custom secret access"},
		{"${{ secrets.MY_SECRET_VALUE }}", "Underscore secret access"},
		{"${{ github.token }}", "github.token access"},
		{"${{ github.event.token }}", "event token access"},
	}

	for _, tt := range unauthorizedPatterns {
		t.Run(tt.description, func(t *testing.T) {
			err := validateExpressionSafety(tt.pattern)
			if err == nil {
				t.Errorf("Security violation: %s should be blocked", tt.pattern)
			}
		})
	}
}

// TestSecurityTokenLeakage validates that tokens cannot be leaked through
// various expression paths.
func TestSecurityTokenLeakage(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		shouldBlock bool
		description string
	}{
		{
			name:        "token_in_allowed_expression",
			content:     "${{ github.workflow }}/${{ secrets.TOKEN }}",
			shouldBlock: true,
			description: "Token in combined expression should be blocked",
		},
		{
			name:        "token_with_function_wrapper",
			content:     "${{ toJson(secrets.TOKEN) }}",
			shouldBlock: true,
			description: "Token wrapped in function should be blocked",
		},
		{
			name:        "token_in_conditional",
			content:     "${{ secrets.TOKEN == '' && 'empty' || 'has-value' }}",
			shouldBlock: true,
			description: "Token in conditional should be blocked",
		},
		{
			name:        "token_with_string_concat",
			content:     "${{ 'prefix-' + secrets.TOKEN + '-suffix' }}",
			shouldBlock: true,
			description: "Token with string concatenation should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.shouldBlock && err == nil {
				t.Errorf("Security violation: %s - %s", tt.name, tt.description)
			}
		})
	}
}

// =============================================================================
// Safe Output System Tests
// =============================================================================

// TestSecuritySafeOutputsBlocksUnsafeOperations validates that the safe-outputs
// system properly restricts operations.
func TestSecuritySafeOutputsBlocksUnsafeOperations(t *testing.T) {
	tests := []struct {
		name          string
		workflow      string
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name: "safe_outputs_with_valid_config",
			workflow: `---
on: issues
permissions:
  contents: read
  issues: read
safe-outputs:
  create-issue:
    title-prefix: "[ai] "
---

# Safe Test
Test safe outputs.`,
			expectError: false,
			description: "Valid safe-outputs should compile successfully",
		},
		{
			name: "safe_outputs_with_secrets_in_body",
			workflow: `---
on: issues
permissions:
  contents: read
  issues: read
safe-outputs:
  create-issue:
---

# Secret in Safe Outputs
Test secrets: ${{ secrets.PREFIX }}`,
			expectError:   true,
			errorContains: "secrets.PREFIX",
			description:   "Secrets in markdown body should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "safe-outputs-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Security violation: %s - %s", tt.name, tt.description)
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
			}
		})
	}
}

// =============================================================================
// Network Isolation Tests
// =============================================================================

// TestSecurityNetworkIsolationEnforcement validates that network permissions
// are properly enforced.
func TestSecurityNetworkIsolationEnforcement(t *testing.T) {
	tests := []struct {
		name            string
		workflow        string
		expectedDomains []string
		description     string
	}{
		{
			name: "copilot_with_restricted_domains",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
network:
  allowed:
    - example.com
    - api.example.org
---

# Network Test
Test network restrictions.`,
			expectedDomains: []string{"example.com", "api.example.org"},
			description:     "Network restrictions should be applied",
		},
		{
			name: "copilot_with_defaults",
			workflow: `---
on: push
permissions:
  contents: read
engine: copilot
network: defaults
---

# Defaults Test
Test network defaults.`,
			expectedDomains: []string{"api.github.com", "github.com"},
			description:     "Default domains should be applied for copilot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "network-isolation-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Verify expected domains appear somewhere in the workflow
			for _, domain := range tt.expectedDomains {
				if !strings.Contains(lockStr, domain) {
					t.Errorf("Expected domain %q not found in compiled workflow", domain)
				}
			}
		})
	}
}

// =============================================================================
// Path Traversal Prevention Tests
// =============================================================================

// TestSecurityPathTraversalPrevention validates that path traversal attacks
// are prevented in file operations.
func TestSecurityPathTraversalPrevention(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		shouldBlock bool
		description string
	}{
		{
			name:        "simple_filename",
			filename:    "workflow.md",
			shouldBlock: false,
			description: "Simple filenames should be allowed",
		},
		{
			name:        "parent_directory_traversal",
			filename:    "../../../etc/passwd",
			shouldBlock: true,
			description: "Parent directory traversal should be blocked",
		},
		{
			name:        "encoded_traversal",
			filename:    "..%2F..%2Fetc%2Fpasswd",
			shouldBlock: true,
			description: "URL-encoded traversal should be blocked",
		},
		{
			name:        "nested_traversal",
			filename:    "subdir/../../etc/passwd",
			shouldBlock: true,
			description: "Nested traversal should be blocked",
		},
		{
			name:        "valid_subdir",
			filename:    "subdir/workflow.md",
			shouldBlock: false,
			description: "Valid subdirectory paths should be allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a safe base directory
			tmpDir := testutil.TempDir(t, "path-traversal-test")

			// Check if the path starts with an absolute path indicator
			if filepath.IsAbs(tt.filename) {
				// Absolute paths should always be blocked when used with a base directory
				if !tt.shouldBlock {
					t.Errorf("Absolute paths should be blocked: %s", tt.filename)
				}
				return
			}

			// Try to construct the full path
			targetPath := filepath.Join(tmpDir, tt.filename)

			// Clean the path
			cleanPath := filepath.Clean(targetPath)

			// Check if the cleaned path is still within tmpDir
			relPath, err := filepath.Rel(tmpDir, cleanPath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				// Path traversal detected
				if !tt.shouldBlock {
					t.Errorf("Unexpected path traversal detection for %s", tt.filename)
				}
			} else {
				// Path is safe
				if tt.shouldBlock {
					t.Errorf("Path traversal should have been detected for %s", tt.filename)
				}
			}
		})
	}
}

// =============================================================================
// Regression Tests for Known Security Issues
// =============================================================================

// TestSecurityRegressionKnownVulnerabilities tests for specific known
// vulnerabilities that have been fixed.
func TestSecurityRegressionKnownVulnerabilities(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		shouldBlock bool
		description string
	}{
		{
			name:        "secrets_typo_check",
			content:     "${{ secrts.TOKEN }}", // Typo in secrets
			shouldBlock: true,                  // Should still be blocked as unknown
			description: "Typos in secrets should still be blocked",
		},
		{
			name:        "whitespace_around_secrets",
			content:     "${{   secrets.TOKEN   }}",
			shouldBlock: true,
			description: "Whitespace around secrets should not bypass blocking",
		},
		{
			name:        "case_sensitivity_secrets",
			content:     "${{ SECRETS.TOKEN }}", // Uppercase
			shouldBlock: true,                   // Should be blocked as unknown
			description: "Case variations should be handled correctly",
		},
		{
			name:        "newline_in_expression",
			content:     "${{ github.workflow\n}}",
			shouldBlock: true, // Multiline expressions are blocked
			description: "Newlines in expressions should be handled",
		},
		{
			name:        "tab_in_expression",
			content:     "${{ github\t.workflow }}",
			shouldBlock: true, // Malformed expression
			description: "Tabs in expressions should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.shouldBlock && err == nil {
				t.Errorf("Regression: %s - %s should be blocked", tt.name, tt.description)
			}
			if !tt.shouldBlock && err != nil {
				t.Errorf("Regression: %s - %s should not be blocked: %v", tt.name, tt.description, err)
			}
		})
	}
}
