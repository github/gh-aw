//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"
)

// TestPlaywrightAllowedDomainsSecretHandling tests that expressions in allowed_domains
// are properly extracted and replaced with environment variable references
func TestPlaywrightAllowedDomainsSecretHandling(t *testing.T) {
	tests := []struct {
		name                 string
		workflow             string
		expectEnvVarPrefix   string
		expectMCPConfigValue string
		expectRedaction      bool
		expectSecretName     string
	}{
		{
			name: "Single secret in allowed_domains",
			workflow: `---
on: issues
engine: copilot
tools:
  playwright:
    allowed_domains:
      - "${{ secrets.TEST_DOMAIN }}"
---

# Test workflow

Test secret in allowed_domains.
`,
			expectEnvVarPrefix:   "GH_AW_SECRETS_",
			expectMCPConfigValue: "__GH_AW_SECRETS_",
			expectRedaction:      true,
			expectSecretName:     "TEST_DOMAIN",
		},
		{
			name: "Multiple secrets in allowed_domains",
			workflow: `---
on: issues
engine: copilot
tools:
  playwright:
    allowed_domains:
      - "${{ secrets.API_KEY }}"
      - "example.com"
      - "${{ secrets.ANOTHER_SECRET }}"
---

# Test workflow

Test multiple secrets in allowed_domains.
`,
			expectEnvVarPrefix:   "GH_AW_SECRETS_",
			expectMCPConfigValue: "__GH_AW_SECRETS_",
			expectRedaction:      true,
			expectSecretName:     "API_KEY",
		},
		{
			name: "No secrets in allowed_domains",
			workflow: `---
on: issues
engine: copilot
tools:
  playwright:
    allowed_domains:
      - "example.com"
      - "test.org"
---

# Test workflow

Test no secrets in allowed_domains.
`,
			expectEnvVarPrefix:   "",
			expectMCPConfigValue: "example.com",
			expectRedaction:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpFile, err := os.CreateTemp("", "test-playwright-secrets-*.md")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			// Write content to file
			if _, err := tmpFile.WriteString(tt.workflow); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			// Create compiler and compile workflow
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)

			// Parse the workflow file to get WorkflowData
			workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse workflow file: %v", err)
			}

			// Generate YAML
			yamlContent, err := compiler.generateYAML(workflowData, tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			// Check if environment variable with correct prefix exists in Start MCP gateway step
			if tt.expectRedaction {
				if !strings.Contains(yamlContent, tt.expectEnvVarPrefix) {
					t.Errorf("Expected environment variable with prefix %s not found in Start MCP gateway step", tt.expectEnvVarPrefix)
				}
			} else {
				if strings.Contains(yamlContent, tt.expectEnvVarPrefix) && tt.expectEnvVarPrefix != "" {
					t.Errorf("Unexpected environment variable with prefix %s found when no secrets present", tt.expectEnvVarPrefix)
				}
			}

			// Check if MCP config uses environment variable reference
			if tt.expectRedaction {
				if !strings.Contains(yamlContent, tt.expectMCPConfigValue) {
					t.Errorf("Expected MCP config to contain %s but it didn't", tt.expectMCPConfigValue)
				}

				// Ensure the secret expression itself is NOT in the MCP config JSON
				// (it should only be in env vars and redaction step)
				mcpConfigStart := strings.Index(yamlContent, "cat > /home/runner/.copilot/mcp-config.json << EOF")
				if mcpConfigStart != -1 {
					mcpConfigEnd := strings.Index(yamlContent[mcpConfigStart:], "EOF\n")
					if mcpConfigEnd != -1 {
						mcpConfig := yamlContent[mcpConfigStart : mcpConfigStart+mcpConfigEnd]
						if strings.Contains(mcpConfig, "${{ secrets.") {
							t.Errorf("MCP config should not contain secret expressions, found secret in config")
						}
					}
				}
			}

			// Check if secret is in redaction list
			if tt.expectRedaction && tt.expectSecretName != "" {
				expectedRedactionEnv := "SECRET_" + tt.expectSecretName + ": ${{ secrets." + tt.expectSecretName + " }}"
				if !strings.Contains(yamlContent, expectedRedactionEnv) {
					t.Errorf("Expected secret %s to be in redaction step environment variables", tt.expectSecretName)
				}
			}
		})
	}
}

// TestExtractExpressionsFromPlaywrightArgs tests the helper function
func TestExtractExpressionsFromPlaywrightArgs(t *testing.T) {
	tests := []struct {
		name                string
		allowedDomains      []string
		customArgs          []string
		expectedExpressions int // Number of unique expressions expected
	}{
		{
			name: "Single expression in allowed_domains",
			allowedDomains: []string{
				"${{ secrets.TEST_DOMAIN }}",
			},
			customArgs:          nil,
			expectedExpressions: 1,
		},
		{
			name: "Multiple expressions",
			allowedDomains: []string{
				"${{ secrets.API_KEY }}",
				"example.com",
				"${{ secrets.ANOTHER_SECRET }}",
			},
			customArgs:          []string{"--arg", "${{ github.event.issue.number }}"},
			expectedExpressions: 3,
		},
		{
			name: "Expressions with whitespace",
			allowedDomains: []string{
				"${{secrets.TEST_SECRET}}",
				"${{  secrets.SPACED_SECRET  }}",
			},
			customArgs:          nil,
			expectedExpressions: 2,
		},
		{
			name: "No expressions",
			allowedDomains: []string{
				"example.com",
				"test.org",
			},
			customArgs:          []string{"--static-arg"},
			expectedExpressions: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expressions := extractExpressionsFromPlaywrightArgs(tt.allowedDomains, tt.customArgs)

			if len(expressions) != tt.expectedExpressions {
				t.Errorf("Expected %d expressions, got %d", tt.expectedExpressions, len(expressions))
			}

			// Verify all expressions are in the map with proper GH_AW_ prefix
			for envVar, originalExpr := range expressions {
				if !strings.HasPrefix(envVar, "GH_AW_") {
					t.Errorf("Expected env var to start with GH_AW_, got %s", envVar)
				}
				if !strings.HasPrefix(originalExpr, "${{") || !strings.HasSuffix(originalExpr, "}}") {
					t.Errorf("Expected original expression to be wrapped in ${{ }}, got %s", originalExpr)
				}
			}
		})
	}
}

// TestReplaceExpressionsInPlaywrightArgs tests the helper function
func TestReplaceExpressionsInPlaywrightArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expressions map[string]string
		validate    func(t *testing.T, result []string)
	}{
		{
			name: "Replace single expression",
			args: []string{
				"${{ secrets.TEST_DOMAIN }}",
			},
			expressions: map[string]string{
				"GH_AW_SECRETS_TEST_DOMAIN": "${{ secrets.TEST_DOMAIN }}",
			},
			validate: func(t *testing.T, result []string) {
				if len(result) != 1 {
					t.Errorf("Expected 1 result, got %d", len(result))
				}
				if !strings.Contains(result[0], "__GH_AW_") {
					t.Errorf("Expected result to contain __GH_AW_, got %s", result[0])
				}
				if strings.Contains(result[0], "${{ secrets.") {
					t.Errorf("Result should not contain secret expressions, got %s", result[0])
				}
			},
		},
		{
			name: "Replace multiple expressions",
			args: []string{
				"${{ secrets.API_KEY }}",
				"example.com",
				"${{ secrets.ANOTHER_SECRET }}",
			},
			expressions: map[string]string{
				"GH_AW_SECRETS_API_KEY":        "${{ secrets.API_KEY }}",
				"GH_AW_SECRETS_ANOTHER_SECRET": "${{ secrets.ANOTHER_SECRET }}",
			},
			validate: func(t *testing.T, result []string) {
				if len(result) != 3 {
					t.Errorf("Expected 3 results, got %d", len(result))
				}
				// Second element should be unchanged
				if result[1] != "example.com" {
					t.Errorf("Expected example.com to be unchanged, got %s", result[1])
				}
				// First and third should be replaced
				for i, r := range []int{0, 2} {
					if strings.Contains(result[r], "${{ secrets.") {
						t.Errorf("Result[%d] should not contain secret expressions, got %s", i, result[r])
					}
				}
			},
		},
		{
			name: "No expressions to replace",
			args: []string{
				"example.com",
				"test.org",
			},
			expressions: map[string]string{},
			validate: func(t *testing.T, result []string) {
				if len(result) != 2 {
					t.Errorf("Expected 2 results, got %d", len(result))
				}
				if result[0] != "example.com" || result[1] != "test.org" {
					t.Errorf("Expected unchanged results, got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceExpressionsInPlaywrightArgs(tt.args, tt.expressions)
			tt.validate(t, result)
		})
	}
}
