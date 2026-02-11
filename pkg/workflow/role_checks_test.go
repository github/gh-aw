//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
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

func TestInferEventsFromTriggers(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    []string
	}{
		{
			name: "infer from map with multiple triggers",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues":            map[string]any{"types": []any{"opened"}},
					"issue_comment":     map[string]any{"types": []any{"created"}},
					"workflow_dispatch": nil,
				},
			},
			expected: []string{"issue_comment", "issues", "workflow_dispatch"},
		},
		{
			name: "infer only programmatic triggers",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push":     map[string]any{},
					"issues":   map[string]any{},
					"schedule": "daily",
				},
			},
			expected: []string{"issues"},
		},
		{
			name: "no triggers",
			frontmatter: map[string]any{
				"on": map[string]any{},
			},
			expected: nil,
		},
		{
			name:        "missing on section",
			frontmatter: map[string]any{},
			expected:    nil,
		},
		{
			name: "all programmatic triggers",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch":           nil,
					"repository_dispatch":         nil,
					"issues":                      map[string]any{},
					"issue_comment":               map[string]any{},
					"pull_request":                map[string]any{},
					"pull_request_review":         map[string]any{},
					"pull_request_review_comment": map[string]any{},
					"discussion":                  map[string]any{},
					"discussion_comment":          map[string]any{},
				},
			},
			expected: []string{
				"discussion",
				"discussion_comment",
				"issue_comment",
				"issues",
				"pull_request",
				"pull_request_review",
				"pull_request_review_comment",
				"repository_dispatch",
				"workflow_dispatch",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.inferEventsFromTriggers(tt.frontmatter)
			// Events should be sorted alphabetically
			assert.Equal(t, tt.expected, result, "Inferred events should match expected (in sorted order)")
		})
	}
}
