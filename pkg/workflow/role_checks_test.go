//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRateLimitProgrammaticEventsMatchJavaScript(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "check_rate_limit.cjs"))
	if err != nil {
		t.Fatal(err)
	}

	match := regexp.MustCompile(`const PROGRAMMATIC_EVENTS = (\[[^;]+\]);`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("PROGRAMMATIC_EVENTS not found in check_rate_limit.cjs")
	}

	var javascriptEvents []string
	if err := json.Unmarshal(match[1], &javascriptEvents); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, rateLimitProgrammaticEvents, javascriptEvents)
}

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

	// Extract the check_membership step section for more precise checking.
	// We collect lines starting from the step with "id: check_membership" until we
	// encounter the next step (another "- name:" line) or a new top-level job.
	checkMembershipSection := ""
	lines := strings.Split(compiledStr, "\n")
	inCheckMembership := false
	var sectionLines []string
	for _, line := range lines {
		if strings.Contains(line, "id: check_membership") {
			inCheckMembership = true
		}
		if inCheckMembership {
			// Stop when we reach the next step (a "- name:" that is NOT the membership step itself)
			if strings.HasPrefix(line, "      - name:") && !strings.Contains(line, "Check team membership") && len(sectionLines) > 0 {
				break
			}
			// Stop when we reach a new top-level job (line with exactly 2 spaces indent)
			if len(sectionLines) > 0 && len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' {
				break
			}
			sectionLines = append(sectionLines, line)
		}
	}
	if len(sectionLines) > 0 {
		checkMembershipSection = strings.Join(sectionLines, "\n")
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

func TestRoleMembershipSupportsSingleRoleString(t *testing.T) {
	tmpDir := testutil.TempDir(t, "role-membership-single-role-string-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  pull_request:
    types: [opened]
  roles: write
---

# Test Workflow
Test that on.roles supports a single string permission value.`

	workflowPath := filepath.Join(tmpDir, "role-membership-single-role-string.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Expected workflow with on.roles as a single string to compile successfully: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "role-membership-single-role-string.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)
	assert.Contains(t, compiledStr, "id: check_membership", "Compiled workflow should include membership checks for role-gated triggers")
	assert.Contains(t, compiledStr, `GH_AW_REQUIRED_ROLES: "write"`, "Compiled workflow should require the single role provided as a string")
	assert.NotContains(t, compiledStr, `GH_AW_REQUIRED_ROLES: "admin,maintainer,write"`, "Compiled workflow should not fall back to default role list when a single role string is provided")
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
			name: "no recognized triggers falls back to all programmatic triggers",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push":     map[string]any{},
					"schedule": "daily",
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
		{
			name:        "missing on section",
			frontmatter: map[string]any{},
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

func TestExtractRateLimitConfig(t *testing.T) {
	c := &Compiler{}

	t.Run("extracts user-rate-limit config", func(t *testing.T) {
		cfg := c.extractRateLimitConfig(map[string]any{
			"user-rate-limit": map[string]any{
				"max-runs-per-window": 3,
				"window":              45,
				"events":              []any{"issue_comment"},
				"ignored-roles":       []any{"admin"},
			},
		})

		if assert.NotNil(t, cfg) {
			assert.Equal(t, 3, cfg.Max)
			assert.Equal(t, 45, cfg.Window)
			assert.Equal(t, []string{"issue_comment"}, cfg.Events)
			assert.Equal(t, []string{"admin"}, cfg.IgnoredRoles)
		}
	})

	t.Run("ignores legacy max-runs alias", func(t *testing.T) {
		cfg := c.extractRateLimitConfig(map[string]any{
			"user-rate-limit": map[string]any{
				"max-runs": 4,
			},
		})

		if assert.NotNil(t, cfg) {
			assert.Zero(t, cfg.Max)
		}
	})

	t.Run("ignores legacy max alias", func(t *testing.T) {
		cfg := c.extractRateLimitConfig(map[string]any{
			"user-rate-limit": map[string]any{
				"max": 2,
			},
		})

		if assert.NotNil(t, cfg) {
			assert.Zero(t, cfg.Max)
		}
	})

	t.Run("legacy rate-limit key is ignored", func(t *testing.T) {
		cfg := c.extractRateLimitConfig(map[string]any{
			"rate-limit": map[string]any{
				"max-runs": 3,
				"window":   45,
			},
		})

		assert.Nil(t, cfg)
	})
}

// TestCommentAuthorAssociationConditionInPreActivation verifies that the compiler adds
// an explicit author_association guard to the pre_activation job's if: condition when the
// workflow is triggered by issue_comment or pull_request_review_comment events and permission
// checks are enabled (i.e. roles is NOT set to "all").  This addresses the RGS-004 static
// analysis finding.
func TestCommentAuthorAssociationConditionInPreActivation(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    string
		wantAssocCheck bool
		wantBotNames   []string // bot actor logins expected in the pre_activation if: condition
	}{
		{
			name: "issue_comment trigger with default roles gets author_association check",
			frontmatter: `---
on:
  issue_comment:
    types: [created]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: true,
		},
		{
			name: "slash_command trigger compiles to issue_comment and gets check",
			frontmatter: `---
on:
  slash_command:
    name: test
    events: [issue_comment]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: true,
		},
		{
			name: "pull_request_review_comment trigger gets author_association check",
			frontmatter: `---
on:
  pull_request_review_comment:
    types: [created]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: true,
		},
		{
			name: "issue_comment trigger with roles:all does NOT get author_association check",
			frontmatter: `---
on:
  roles: all
  issue_comment:
    types: [created]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: false,
		},
		{
			name: "push trigger only does NOT get author_association check",
			frontmatter: `---
on:
  push:
    branches: [main]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: false,
		},
		{
			name: "workflow_dispatch-only trigger does NOT get author_association check",
			frontmatter: `---
on:
  workflow_dispatch:
  roles: [write]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: false,
		},
		{
			name: "issue_comment trigger with on.bots allows bot actor in pre_activation if",
			frontmatter: `---
on:
  issue_comment:
    types: [created]
  bots:
    - dependabot[bot]
    - renovate[bot]
engine: copilot
---

Test workflow
`,
			wantAssocCheck: true,
			wantBotNames:   []string{"dependabot[bot]", "renovate[bot]"},
		},
		{
			name: "issue_comment trigger with expression bot disables static guard so runtime check always runs",
			frontmatter: `---
on:
  issue_comment:
    types: [created]
  bots:
    - ${{ vars.TRUSTED_BOT }}
engine: copilot
---

Test workflow
`,
			// The static guard must be absent; check_membership handles the bot at runtime.
			wantAssocCheck: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "comment-auth-test")
			compiler := NewCompiler()

			workflowPath := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			err = compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			outputPath := filepath.Join(tmpDir, "test-workflow.lock.yml")
			compiledContent, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("Failed to read compiled workflow: %v", err)
			}

			compiledStr := string(compiledContent)

			// Extract the pre_activation job section so we check only the job-level if:,
			// not unrelated occurrences in other jobs or comments.
			preActivationSection := extractJobSection(compiledStr, "pre_activation")

			// When there is no pre_activation job (e.g. roles:all or workflow_dispatch-only),
			// the author_association guard is clearly absent — handle both outcomes here.
			if preActivationSection == "" {
				if tt.wantAssocCheck {
					t.Errorf("Expected pre_activation job section to be present and contain author_association check, but the section was not found")
				}
				// wantAssocCheck == false and no pre_activation section → test passes.
				return
			}

			// Look for the author_association guard within the pre_activation job section.
			// The job-level if: may be rendered as a block scalar (if: >\n  <expression>),
			// so we check the whole section rather than a single line. Any occurrence of
			// author_association in this section comes from the job-level if: expression.
			hasCheck := strings.Contains(preActivationSection, "author_association")
			if tt.wantAssocCheck && !hasCheck {
				t.Errorf("Expected pre_activation job if: to contain author_association check, but it was absent.\nFull pre_activation section:\n%s", preActivationSection)
			}
			if !tt.wantAssocCheck && hasCheck {
				t.Errorf("Expected pre_activation job if: to NOT contain author_association check, but it was present.\nFull pre_activation section:\n%s", preActivationSection)
			}

			// For the bot test case, also verify bot actor exemptions appear in the job if:.
			for _, botName := range tt.wantBotNames {
				if !strings.Contains(preActivationSection, botName) {
					t.Errorf("Expected pre_activation job if: to contain bot actor exemption for %q, but it was absent.\nFull pre_activation section:\n%s", botName, preActivationSection)
				}
			}
		})
	}
}

// TestCommentAuthorAssociationImportedExpressionBot verifies that when a shared agentic workflow
// contributes an expression-based bot (e.g. "${{ vars.TRUSTED_BOT }}") via imports, the static
// author_association guard is disabled and check_membership is always reached at runtime.
func TestCommentAuthorAssociationImportedExpressionBot(t *testing.T) {
	tmpDir := testutil.TempDir(t, "comment-auth-import-test")
	compiler := NewCompiler()

	// Shared agentic workflow: defines a bot with a GHA expression using the supported
	// on.bots path. This should disable the static author_association guard because the
	// bot identity is only known at runtime.
	sharedContent := `---
on:
 bots:
   - "${{ vars.TRUSTED_BOT }}"
---
`
	sharedPath := filepath.Join(tmpDir, "shared-bots.md")
	err := os.WriteFile(sharedPath, []byte(sharedContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	// Main workflow imports the shared workflow; its own on: has issue_comment.
	mainContent := `---
on:
  issue_comment:
    types: [created]
engine: copilot
imports:
  - shared-bots.md
---

Test workflow
`
	mainPath := filepath.Join(tmpDir, "main-workflow.md")
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write main workflow file: %v", err)
	}

	err = compiler.CompileWorkflow(mainPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "main-workflow.lock.yml")
	compiled, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	preActivationSection := extractJobSection(string(compiled), "pre_activation")
	if preActivationSection == "" {
		t.Fatal("Expected pre_activation job section to be present")
	}

	// The static guard must be absent: the expression bot cannot be evaluated at compile time,
	// so check_membership must always run to handle authorization at runtime.
	if strings.Contains(preActivationSection, "author_association") {
		t.Errorf("Expected pre_activation job if: to NOT contain author_association check (expression bot from import), but it was present.\nFull pre_activation section:\n%s", preActivationSection)
	}
}
