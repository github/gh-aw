//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileVulnerabilityAlertsPermissionIncluded compiles the canonical
// test-vulnerability-alerts-permission.md workflow file and verifies that
// the `vulnerability-alerts` scope appears correctly in the compiled lock file.
//
// Since vulnerability-alerts is now a native GITHUB_TOKEN permission scope, it
// SHOULD appear in job-level permissions blocks. It is also forwarded as a
// `permission-vulnerability-alerts` input to actions/create-github-app-token
// when a GitHub App is configured.
func TestCompileVulnerabilityAlertsPermissionIncluded(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Copy the canonical workflow file into the test's .github/workflows dir
	srcPath := filepath.Join(projectRoot, "pkg/cli/workflows/test-vulnerability-alerts-permission.md")
	dstPath := filepath.Join(setup.workflowsDir, "test-vulnerability-alerts-permission.md")

	srcContent, err := os.ReadFile(srcPath)
	require.NoError(t, err, "Failed to read source workflow file %s", srcPath)
	require.NoError(t, os.WriteFile(dstPath, srcContent, 0644), "Failed to write workflow to test dir")

	// Compile the workflow using the pre-built binary
	cmd := exec.Command(setup.binaryPath, "compile", dstPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI compile command failed:\n%s", string(output))

	// Read the compiled lock file
	lockFilePath := filepath.Join(setup.workflowsDir, "test-vulnerability-alerts-permission.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")
	lockContentStr := string(lockContent)

	// The App token minting step must receive `permission-vulnerability-alerts: read` as input.
	assert.Contains(t, lockContentStr, "permission-vulnerability-alerts: read",
		"App token minting step should include permission-vulnerability-alerts: read")
	assert.Contains(t, lockContentStr, "id: github-mcp-app-token",
		"GitHub App token minting step should be generated")

	// vulnerability-alerts is now a valid GITHUB_TOKEN scope and SHOULD appear in
	// job-level permissions blocks.
	var workflow map[string]any
	require.NoError(t, goyaml.Unmarshal(lockContent, &workflow),
		"Lock file should be valid YAML")

	jobs, ok := workflow["jobs"].(map[string]any)
	require.True(t, ok, "Lock file should have a jobs section")

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
	assert.True(t, foundVulnAlerts,
		"vulnerability-alerts should appear in at least one job-level permissions block (it is a GITHUB_TOKEN scope)")

	// vulnerability-alerts: read should appear in both job-level permissions
	// and in the App token step inputs (permission-vulnerability-alerts: read).
	occurrences := strings.Count(lockContentStr, "vulnerability-alerts: read")
	appTokenOccurrences := strings.Count(lockContentStr, "permission-vulnerability-alerts: read")
	assert.GreaterOrEqual(t, occurrences, appTokenOccurrences,
		"vulnerability-alerts: read should appear at least as often as permission-vulnerability-alerts: read")
	assert.Greater(t, occurrences, 0,
		"vulnerability-alerts: read should appear at least once in the lock file")
}

func TestCompileDefaultsToStrictMode(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	workflowPath := filepath.Join(setup.workflowsDir, "strict-default.md")
	workflow := `---
on: push
permissions:
  contents: write
engine: copilot
---

# Strict default
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0644))

	cmd := exec.Command(setup.binaryPath, "compile", workflowPath)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "compile without --strict should reject write permissions")
	assert.Contains(t, string(output), "strict mode: write permission 'contents: write' is not allowed",
		"compile without --strict should enable strict validation by default")
}

func TestCompileWorkspaceStrictModeOverridesFrontmatter(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	require.NoError(t, os.WriteFile(filepath.Join(setup.workflowsDir, "aw.json"), []byte(`{"strict":true}`), 0644))
	workflowPath := filepath.Join(setup.workflowsDir, "workspace-strict.md")
	workflow := `---
on: push
strict: false
permissions:
  contents: write
engine: copilot
---

# Workspace strict
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0644))

	cmd := exec.Command(setup.binaryPath, "compile", workflowPath)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "workspace strict mode should reject a workflow opt-out")
	assert.Contains(t, string(output), "strict mode: write permission 'contents: write' is not allowed")
}
