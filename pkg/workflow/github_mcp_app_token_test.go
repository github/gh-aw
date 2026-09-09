//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitHubMCPAppTokenConfiguration tests that app configuration is correctly parsed for GitHub tool
func TestGitHubMCPAppTokenConfiguration(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read  # read permission for testing
strict: false  # disable strict mode for testing
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      repositories:
        - "repo1"
        - "repo2"
---

# Test Workflow

Test workflow with GitHub MCP Server app configuration.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.ParsedTools, "ParsedTools should not be nil")
	require.NotNil(t, workflowData.ParsedTools.GitHub, "GitHub tool should be parsed")
	require.NotNil(t, workflowData.ParsedTools.GitHub.GitHubApp, "App configuration should be parsed")

	// Verify app configuration
	assert.Equal(t, "${{ vars.APP_ID }}", workflowData.ParsedTools.GitHub.GitHubApp.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.ParsedTools.GitHub.GitHubApp.PrivateKey)
	assert.Equal(t, []string{"repo1", "repo2"}, workflowData.ParsedTools.GitHub.GitHubApp.Repositories)
}

// TestGitHubMCPAppTokenMintingStep tests that token minting step is generated
func TestGitHubMCPAppTokenMintingStep(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read  # read permission for testing
strict: false  # disable strict mode for testing
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with GitHub MCP app token minting.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile the workflow
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file (same name with .lock.yml extension)
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// Verify token minting step is present in the agent job
	assert.Contains(t, lockContent, "Generate GitHub App token", "Token minting step should be present")
	assert.Contains(t, lockContent, "actions/create-github-app-token", "Should use create-github-app-token action")
	assert.Contains(t, lockContent, "id: github-mcp-app-token", "Should use github-mcp-app-token as step ID")
	assert.Contains(t, lockContent, "client-id: ${{ vars.APP_ID }}", "Should use configured app ID")
	assert.Contains(t, lockContent, "private-key: ${{ secrets.APP_PRIVATE_KEY }}", "Should use configured private key")

	// Verify permissions are passed to the app token minting
	assert.Contains(t, lockContent, "permission-contents: read", "Should include contents read permission")
	assert.Contains(t, lockContent, "permission-issues: read", "Should include issues read permission")

	// Verify the activation job does NOT expose github_mcp_app_token as a job output
	// (masked values are silently dropped by the runner when used as job outputs)
	assert.NotContains(t, lockContent, "github_mcp_app_token: ${{ steps.github-mcp-app-token.outputs.token }}", "Activation job must not expose github_mcp_app_token output")

	// Explicit token invalidation step should not be generated in the agent job.
	// actions/create-github-app-token handles revocation in its post step.
	assert.NotContains(t, lockContent, "Invalidate GitHub App token", "Token invalidation step should not be present")

	// Verify the app token is consumed from the step output within the agent job
	assert.Contains(t, lockContent, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.github-mcp-app-token.outputs.token }}", "Should use agent job step token for GitHub MCP Server")
}

// TestGitHubMCPAppTokenAndGitHubTokenMutuallyExclusive tests that setting both app and github-token is rejected
func TestGitHubMCPAppTokenAndGitHubTokenMutuallyExclusive(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
tools:
  github:
    mode: local
    github-token: ${{ secrets.CUSTOM_GITHUB_TOKEN }}
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test that setting both app and github-token is an error.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile the workflow - should fail because both app and github-token are set
	err = compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Expected error when both app and github-token are set")
	require.ErrorContains(t, err, "'tools.github.github-app' and 'tools.github.github-token' cannot both be set", "Error should mention mutual exclusion")
}

// TestGitHubMCPAppTokenWithRemoteMode tests that app token works with remote mode
func TestGitHubMCPAppTokenWithRemoteMode(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
tools:
  github:
    mode: remote
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
engine: claude
---

# Test Workflow

Test app token with remote GitHub MCP Server.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile the workflow
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file (same name with .lock.yml extension)
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// Verify token minting step is present in the agent job
	assert.Contains(t, lockContent, "Generate GitHub App token", "Token minting step should be present")
	assert.Contains(t, lockContent, "id: github-mcp-app-token", "Should use github-mcp-app-token as step ID")

	// Verify the activation job does NOT expose the token as a job output
	assert.NotContains(t, lockContent, "github_mcp_app_token: ${{ steps.github-mcp-app-token.outputs.token }}", "Activation job must not expose github_mcp_app_token output")

	// Verify the app token from the agent job step is used
	// The token should be referenced via steps.github-mcp-app-token.outputs.token
	if strings.Contains(lockContent, `"Authorization": "Bearer ${{ steps.github-mcp-app-token.outputs.token }}"`) {
		// Success - app token from step is used in Authorization header
		t.Log("App token from agent job step correctly used in remote mode Authorization header")
	} else {
		// Also check for the env var reference pattern used by Claude engine
		assert.Contains(t, lockContent, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.github-mcp-app-token.outputs.token }}", "Should use agent job step token for GitHub MCP Server in remote mode")
	}
}

// TestGitHubMCPAppTokenOrgWide tests org-wide GitHub MCP token with wildcard
func TestGitHubMCPAppTokenOrgWide(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read
strict: false
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      repositories:
        - "*"
---

# Test Workflow

Test org-wide GitHub MCP app token.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile the workflow
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file (same name with .lock.yml extension)
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// Verify token minting step is present
	assert.Contains(t, lockContent, "Generate GitHub App token", "Token minting step should be present")

	// Verify repositories field is NOT present (org-wide access)
	assert.NotContains(t, lockContent, "repositories:", "Should not include repositories field for org-wide access")

	// Verify other fields are still present
	assert.Contains(t, lockContent, "owner:", "Should include owner field")
	assert.Contains(t, lockContent, "client-id:", "Should include client-id field")
}

// TestGitHubMCPAppTokenWithLockdownDetectionStep tests that determine-automatic-lockdown
// step IS generated even when a GitHub App is configured.
// Repo-scoping from a GitHub App token does not substitute for author-integrity filtering
// inside a repository; public repos still need automatic min-integrity: approved protection.
func TestGitHubMCPAppTokenWithLockdownDetectionStep(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read
strict: false
safe-outputs:
  noop: {}
  report-incomplete: {}
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      repositories:
        - "repo1"
        - "repo2"
---

# Test Workflow

Test that determine-automatic-lockdown is generated even when app is configured.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// The automatic lockdown detection step MUST be present even when app is configured.
	// GitHub App repo-scoping does not replace author-integrity filtering for public repos.
	assert.Contains(t, lockContent, "Determine automatic lockdown mode", "determine-automatic-lockdown step should be generated even when app is configured")
	assert.Contains(t, lockContent, "id: determine-automatic-lockdown", "determine-automatic-lockdown step ID should be present")

	// Guard policy env vars must reference the lockdown step outputs
	assert.Contains(t, lockContent, "GITHUB_MCP_GUARD_MIN_INTEGRITY: ${{ steps.determine-automatic-lockdown.outputs.min_integrity }}", "Guard min-integrity env var should reference lockdown step output")
	assert.Contains(t, lockContent, "GITHUB_MCP_GUARD_REPOS: ${{ steps.determine-automatic-lockdown.outputs.repos }}", "Guard repos env var should reference lockdown step output")
	githubMCPIndex := strings.Index(lockContent, `"github": {`)
	require.NotEqual(t, -1, githubMCPIndex, "GitHub MCP server should be configured")
	githubMCPConfig := lockContent[githubMCPIndex:]
	assert.Contains(t, githubMCPConfig, `"allow-only"`, "GitHub MCP server should include an automatic allow-only guard policy")
	assert.Contains(t, githubMCPConfig, `"min-integrity": "$GITHUB_MCP_GUARD_MIN_INTEGRITY"`, "GitHub guard policy should use automatic min-integrity output")
	assert.Contains(t, githubMCPConfig, `"repos": "$GITHUB_MCP_GUARD_REPOS"`, "GitHub guard policy should use automatic repos output")

	safeOutputsMCPIndex := strings.Index(lockContent, `"safeoutputs": {`)
	require.NotEqual(t, -1, safeOutputsMCPIndex, "safeoutputs MCP server should be configured")
	safeOutputsMCPConfig := lockContent[safeOutputsMCPIndex:]
	assert.Contains(t, safeOutputsMCPConfig, `"write-sink"`, "safeoutputs MCP server should include a write-sink guard policy")
	assert.Contains(t, safeOutputsMCPConfig, `"accept": [`, "safeoutputs write-sink policy should include accept list")
	assert.Contains(t, safeOutputsMCPConfig, `"*"`, "safeoutputs write-sink policy should accept all runtime safe-output sink labels")
	assert.Contains(t, safeOutputsMCPConfig, `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"`, "safeoutputs write-sink policy should use runtime sink visibility")

	// App token should still be minted (now in agent job) and consumed via step output
	assert.Contains(t, lockContent, "id: github-mcp-app-token", "GitHub App token step should still be generated")
	assert.Contains(t, lockContent, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.github-mcp-app-token.outputs.token }}", "App token from agent job step should be used for MCP server")
}

// TestGitHubMCPAppTokenWithDependabotToolset tests that permission-vulnerability-alerts is included
// when the dependabot toolset is configured with a GitHub App.
// The correct GitHub App permission for Dependabot alerts is "vulnerability_alerts"
// (see https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app),
// which maps to "permission-vulnerability-alerts" in actions/create-github-app-token.
func TestGitHubMCPAppTokenWithDependabotToolset(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  security-events: read
  vulnerability-alerts: read
strict: false
tools:
  github:
    mode: local
    toolsets: [dependabot]
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test that permission-vulnerability-alerts is emitted in the App token minting step.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// Verify the vulnerability-alerts permission is passed to the App token minting step
	// This is the correct GitHub App permission name for Dependabot alerts
	assert.Contains(t, lockContent, "permission-vulnerability-alerts: read", "Should include vulnerability-alerts read permission in App token")
	// Verify that security-events is also still passed through
	assert.Contains(t, lockContent, "permission-security-events: read", "Should also include security-events read permission in App token")
	// Verify the token minting step is present
	assert.Contains(t, lockContent, "id: github-mcp-app-token", "GitHub App token step should be generated")
	// Verify that vulnerability-alerts DOES appear in job-level permissions block.
	// It is now a valid GITHUB_TOKEN permission scope.
	var workflow map[string]any
	require.NoError(t, goyaml.Unmarshal(content, &workflow), "Lock file should be valid YAML")
	jobs, ok := workflow["jobs"].(map[string]any)
	require.True(t, ok, "Should have jobs section")
	foundVulnAlerts := false
	for _, jobConfig := range jobs {
		jobMap, ok := jobConfig.(map[string]any)
		if !ok {
			continue
		}
		perms, hasPerms := jobMap["permissions"]
		if !hasPerms {
			continue
		}
		permsMap, ok := perms.(map[string]any)
		if !ok {
			continue
		}
		if _, found := permsMap["vulnerability-alerts"]; found {
			foundVulnAlerts = true
		}
	}
	assert.True(t, foundVulnAlerts, "vulnerability-alerts should appear in at least one job-level permissions block (it is a GITHUB_TOKEN scope)")
}

// TestGitHubMCPAppTokenWithExtraPermissions tests that extra permissions under
// tools.github.github-app.permissions are merged into the minted token (nested wins).
// This allows org-level permissions (e.g. members: read) that are not valid GitHub
// Actions scopes but are supported by GitHub Apps.
func TestGitHubMCPAppTokenWithExtraPermissions(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read
strict: false
tools:
  github:
    mode: local
    toolsets: [orgs, users]
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      repositories: ["*"]
      permissions:
        members: read
        organization-administration: read
        secret-scanning-alerts: read
---

# Test Workflow

Test extra org-level permissions in GitHub App token.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// Verify the token minting step is present
	assert.Contains(t, lockContent, "id: github-mcp-app-token", "GitHub App token step should be generated")

	// Verify that job-level permissions are still included
	assert.Contains(t, lockContent, "permission-contents: read", "Should include job-level contents permission")
	assert.Contains(t, lockContent, "permission-issues: read", "Should include job-level issues permission")

	// Verify that the extra org-level permissions from github-app.permissions are included
	assert.Contains(t, lockContent, "permission-members: read", "Should include extra members permission from github-app.permissions")
	assert.Contains(t, lockContent, "permission-organization-administration: read", "Should include extra organization-administration permission from github-app.permissions")
	assert.Contains(t, lockContent, "permission-secret-scanning-alerts: read", "Should include extra secret-scanning-alerts permission from github-app.permissions")
}

// TestGitHubMCPAppTokenWithSecretScanningAlertsNoneOmitted tests that
// secret-scanning-alerts: none under tools.github.github-app.permissions is omitted
// from create-github-app-token inputs because the action does not accept "none"
// for that permission.
func TestGitHubMCPAppTokenWithSecretScanningAlertsNoneOmitted(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
strict: false
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      repositories: ["*"]
      permissions:
        members: read
        secret-scanning-alerts: none
---

# Test Workflow

Test extra secret-scanning-alerts none behavior in GitHub App token.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	assert.Contains(t, lockContent, "permission-members: read", "Should include other app permissions")
	assert.NotContains(t, lockContent, "permission-secret-scanning-alerts:", "secret-scanning-alerts: none should be omitted")
}

// TestGitHubMCPAppTokenExtraPermissionsOverrideJobLevel tests that extra permissions
// under tools.github.github-app.permissions can suppress a scope
// that was set at job level by overriding it with 'none' (nested wins).
func TestGitHubMCPAppTokenExtraPermissionsOverrideJobLevel(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
  issues: read
  vulnerability-alerts: read
strict: false
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      permissions:
        vulnerability-alerts: none
---

# Test Workflow

Test that nested permissions override job-level scopes (nested wins).
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// The nested permission (none) should win over the job-level permission (read)
	assert.Contains(t, lockContent, "permission-vulnerability-alerts: none", "Nested vulnerability-alerts: none should override job-level: read")
	assert.NotContains(t, lockContent, "permission-vulnerability-alerts: read", "Job-level vulnerability-alerts: read should be overridden by nested none")

	// Other job-level permissions should still be present
	assert.Contains(t, lockContent, "permission-contents: read", "Unaffected job-level contents permission should still be present")
	assert.Contains(t, lockContent, "permission-issues: read", "Unaffected job-level issues permission should still be present")
}

// TestGitHubMCPAppTokenExtraPermissionsWriteRejected tests that the compiler
// rejects a workflow where tools.github.github-app.permissions contains a "write"
// value, since write access is not allowed for GitHub App-only scopes in this section.
func TestGitHubMCPAppTokenExtraPermissionsWriteRejected(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
strict: false
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      permissions:
        members: write
---

# Test Workflow

Test that write is rejected in tools.github.github-app.permissions.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Compiler should reject write in tools.github.github-app.permissions")
	require.ErrorContains(t, err, "Invalid permission levels in tools.github.github-app.permissions", "Error should mention invalid permission levels")
	require.ErrorContains(t, err, `"write" is not allowed`, "Error should mention that write is not allowed")
	require.ErrorContains(t, err, "members", "Error should mention the offending scope")
}

// TestCheckoutAppTokensMintedInAgentJob verifies that checkout-related GitHub App token
// minting steps (create-github-app-token) appear directly in the agent job.
// Previously, checkout tokens were minted in the activation job and passed via job outputs,
// but the GitHub Actions runner silently drops masked values in job outputs (runner v2.308+),
// causing actions/checkout to fail with "Input required and not supplied: token".
// The fix mints checkout tokens in the agent job itself (same as github-mcp-app-token),
// so the token is accessible as steps.checkout-app-token-{index}.outputs.token.
func TestCheckoutAppTokensMintedInAgentJob(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
	}{
		{
			name: "checkout.github-app token minted in agent job",
			markdown: `---
on: issues
permissions:
  contents: read
strict: false
checkout:
  repository: myorg/private-repo
  path: private
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

Test workflow - checkout app token must be minted in agent job.
`,
		},
		{
			name: "top-level github-app fallback for checkout minted in agent job",
			markdown: `---
on: issues
permissions:
  contents: read
strict: false
github-app:
  app-id: ${{ vars.APP_ID }}
  private-key: ${{ secrets.APP_PRIVATE_KEY }}
checkout:
  repository: myorg/private-repo
  path: private
---

Test workflow - top-level github-app checkout token must be minted in agent job.
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion("1.0.0"))
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(testFile, []byte(tt.markdown), 0644)
			require.NoError(t, err, "Failed to write test file")

			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Workflow should compile successfully")

			lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
			content, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Failed to read lock file")
			lockContent := string(content)

			// Extract the agent job section (from "  agent:" to the next top-level job).
			// The token minting step must be inside the agent job, not the activation job.
			agentJobSection := extractJobSection(lockContent, "agent")
			require.NotEmpty(t, agentJobSection, "Agent job should be present")

			// The token minting step must appear inside the agent job section
			assert.Contains(t, agentJobSection, "id: checkout-app-token-0",
				"Checkout app token minting step must be inside the agent job")
			assert.Contains(t, agentJobSection, "create-github-app-token",
				"Checkout app token minting step must use create-github-app-token action")

			// The token must be referenced via the step output (same-job), not via activation outputs.
			// Activation job outputs of masked values are silently dropped by the runner (v2.308+).
			assert.Contains(t, agentJobSection, "steps.checkout-app-token-0.outputs.token",
				"Token must be referenced via step output within the same job")
			assert.NotContains(t, lockContent, "needs.activation.outputs.checkout_app_token_0",
				"Token must not be passed via activation job outputs (masked values are dropped by runner)")

			// The activation job must NOT expose checkout_app_token as a job output or contain
			// the minting step.
			assert.NotContains(t, lockContent, "checkout_app_token_0: ${{ steps.checkout-app-token-0.outputs.token }}",
				"Activation job must not expose checkout app token as a job output")
			activationJobSection := extractJobSection(lockContent, "activation")
			if activationJobSection != "" {
				assert.NotContains(t, activationJobSection, "id: checkout-app-token-0",
					"Checkout app token minting step must NOT be in the activation job")
			}
		})
	}
}

// TestGitHubMCPAppTokenMintedInAgentJob verifies that the GitHub MCP App token
// (tools.github.github-app) IS minted directly in the agent job.
// This is required because actions/create-github-app-token calls ::add-mask:: on the
// produced token, and the GitHub Actions runner silently drops masked values when used
// as job outputs (runner v2.308+). By minting within the agent job the token is
// available as steps.github-mcp-app-token.outputs.token.
func TestGitHubMCPAppTokenMintedInAgentJob(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	markdown := `---
on: issues
permissions:
  contents: read
  issues: read
strict: false
tools:
  github:
    mode: local
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

Test workflow - MCP app token must be minted in agent job.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow should compile successfully")

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	lockContent := string(content)

	// The minting step must be present somewhere in the compiled workflow
	assert.Contains(t, lockContent, "create-github-app-token",
		"GitHub MCP App token minting step must be present in the compiled workflow")
	assert.Contains(t, lockContent, "id: github-mcp-app-token",
		"GitHub MCP App token step must use github-mcp-app-token step ID")

	// The activation job must NOT expose github_mcp_app_token as a job output.
	// Masked values are silently dropped by the runner when passed as job outputs (runner v2.308+).
	assert.NotContains(t, lockContent, "github_mcp_app_token: ${{ steps.github-mcp-app-token.outputs.token }}",
		"Activation job must not expose github_mcp_app_token as a job output")

	// The token must be referenced via the step output, not via activation job outputs.
	assert.NotContains(t, lockContent, "needs.activation.outputs.github_mcp_app_token",
		"Token must not be referenced via activation job outputs (masked values are dropped by runner)")
	assert.Contains(t, lockContent, "steps.github-mcp-app-token.outputs.token",
		"Token must be referenced via step output within the same job")
}
