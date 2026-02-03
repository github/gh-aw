//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestRoleMembershipUsesGitHubToken tests that the role membership check
// explicitly uses the GitHub Actions token (GITHUB_TOKEN) and not any other secret
func TestRoleMembershipUsesGitHubToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "role-membership-token-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  issues:
    types: [opened]
roles: [admin, maintainer]
---

# Test Workflow
Test that role membership check uses GITHUB_TOKEN.`

	workflowPath := filepath.Join(tmpDir, "role-membership-token.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	outputPath := filepath.Join(tmpDir, "role-membership-token.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)

	// Verify that the check_membership step exists
	if !strings.Contains(compiledStr, "id: check_membership") {
		t.Fatalf("Expected check_membership step to exist in compiled workflow")
	}

	// Verify that the check_membership step uses github-token
	if !strings.Contains(compiledStr, "github-token: ${{ secrets.GITHUB_TOKEN }}") {
		t.Errorf("Expected check_membership step to explicitly use 'github-token: ${{ secrets.GITHUB_TOKEN }}'")
	}

	// Verify it does NOT use any custom tokens like GH_AW_GITHUB_TOKEN, GH_AW_AGENT_TOKEN, etc.
	customTokens := []string{
		"GH_AW_GITHUB_TOKEN",
		"GH_AW_AGENT_TOKEN",
		"COPILOT_GITHUB_TOKEN",
		"COPILOT_TOKEN",
		"GH_AW_GITHUB_MCP_SERVER_TOKEN",
	}

	// Extract the check_membership job section for more precise checking
	checkMembershipSection := ""
	lines := strings.Split(compiledStr, "\n")
	inCheckMembership := false
	for i, line := range lines {
		if strings.Contains(line, "id: check_membership") {
			inCheckMembership = true
			// Include lines before the step for context
			if i > 5 {
				checkMembershipSection = strings.Join(lines[i-5:], "\n")
			}
		}
		if inCheckMembership && i < len(lines)-1 {
			// Stop when we reach the next step or job
			if strings.HasPrefix(line, "      - name:") && !strings.Contains(line, "Check team membership") {
				break
			}
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && i > 0 {
				break
			}
		}
	}

	if checkMembershipSection == "" {
		// If we couldn't extract it, use the full compiled workflow for checks
		checkMembershipSection = compiledStr
	}

	for _, customToken := range customTokens {
		if strings.Contains(checkMembershipSection, customToken) {
			t.Errorf("check_membership step should NOT use custom token '%s', only GITHUB_TOKEN", customToken)
		}
	}
}

// TestRoleMembershipTokenWithBots tests that the role membership check uses GITHUB_TOKEN even with bots configured
func TestRoleMembershipTokenWithBots(t *testing.T) {
	tmpDir := testutil.TempDir(t, "role-membership-token-bots-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  pull_request:
    types: [opened]
roles: [write]
bots: ["dependabot[bot]"]
---

# Test Workflow
Test that role membership check uses GITHUB_TOKEN with bots.`

	workflowPath := filepath.Join(tmpDir, "role-membership-token-bots.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	outputPath := filepath.Join(tmpDir, "role-membership-token-bots.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)

	// Verify that the check_membership step explicitly uses github-token: GITHUB_TOKEN
	if !strings.Contains(compiledStr, "github-token: ${{ secrets.GITHUB_TOKEN }}") {
		t.Errorf("Expected check_membership step to explicitly use 'github-token: ${{ secrets.GITHUB_TOKEN }}'")
	}
}
