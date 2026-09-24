//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpgradeCommand_OnExistingRepository verifies that the upgrade command runs
// successfully against the actual project repository. It uses --no-fix to skip
// codemods, action updates, and compilation, and --skip-extension-upgrade to
// avoid a network call for the extension upgrade check.
func TestUpgradeCommand_OnExistingRepository(t *testing.T) {
	cmd := exec.Command(globalBinaryPath, "upgrade", "--no-fix", "--skip-extension-upgrade")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("Upgrade output: %s", outputStr)

	require.NoError(t, err, "upgrade command should succeed on existing repository, output: %s", outputStr)
	assert.Contains(t, outputStr, "Upgrade complete", "Should report upgrade complete")
}

func TestUpgradeCompilesAndPreservesAWDirectory(t *testing.T) {
	const noImplicitExpiry = `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`

	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	initGit := exec.Command("git", "init", "--quiet")
	initGit.Dir = setup.tempDir
	require.NoError(t, initGit.Run(), "git init should succeed")

	awDir := filepath.Join(setup.tempDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(filepath.Join(awDir, "logs"), 0o755), "should create .github/aw/logs")
	require.NoError(t, os.WriteFile(filepath.Join(awDir, "actions-lock.json"), []byte("{}\n"), 0o644), "should create actions-lock.json")

	initCmd := exec.Command(setup.binaryPath, "init")
	initCmd.Dir = setup.tempDir
	initOutput, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "init command should succeed with empty .github/aw directory, output: %s", string(initOutput))
	_, err := os.Stat(filepath.Join(awDir, "actions-lock.json"))
	require.NoError(t, err, "expected actions-lock.json to be preserved after init")
	_, err = os.Stat(filepath.Join(awDir, "logs"))
	require.NoError(t, err, "expected .github/aw/logs to be preserved after init")

	skillPath := filepath.Join(setup.tempDir, ".github", "skills", "agentic-workflows", "SKILL.md")
	_, err = os.Stat(skillPath)
	require.NoError(t, err, "expected dispatcher skill file to exist after init")

	workflowPath := filepath.Join(setup.tempDir, ".github", "workflows", "example.md")
	workflowContent := `---
name: Example Agentic Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
strict: true
timeout-minutes: 5
safe-outputs:
  add-comment:
---

Say hello.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644), "should create sample workflow")

	upgradeCmd := exec.Command(setup.binaryPath, "upgrade", "--approve", "--no-actions", "--skip-extension-upgrade")
	upgradeCmd.Dir = setup.tempDir
	upgradeOutput, upgradeErr := upgradeCmd.CombinedOutput()
	upgradeOutputStr := string(upgradeOutput)
	require.NoError(t, upgradeErr, "upgrade command should succeed with empty .github/aw directory, output: %s", upgradeOutputStr)
	assert.Contains(t, upgradeOutputStr, "Upgrade complete", "Should report upgrade complete")
	_, err = os.Stat(filepath.Join(awDir, "actions-lock.json"))
	require.NoError(t, err, "expected actions-lock.json to be preserved after upgrade")
	_, err = os.Stat(filepath.Join(awDir, "logs"))
	require.NoError(t, err, "expected .github/aw/logs to be preserved after upgrade")

	lockContent, err := os.ReadFile(filepath.Join(setup.tempDir, ".github", "workflows", "example.lock.yml"))
	require.NoError(t, err, "expected upgrade to compile the workflow")
	assert.Contains(t, string(lockContent), noImplicitExpiry)

	_, err = os.Stat(filepath.Join(setup.tempDir, ".github", "workflows", "agentics-maintenance.yml"))
	require.True(t, os.IsNotExist(err), "expected maintenance workflow to not be generated for implicit expiry")
}

func TestUpgradeNoFixSkipsCompilation(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	initGit := exec.Command("git", "init", "--quiet")
	initGit.Dir = setup.tempDir
	require.NoError(t, initGit.Run(), "git init should succeed")

	initCmd := exec.Command(setup.binaryPath, "init")
	initCmd.Dir = setup.tempDir
	initOutput, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "init command should succeed, output: %s", initOutput)

	workflowPath := filepath.Join(setup.tempDir, ".github", "workflows", "example.md")
	workflowContent := `---
name: Example Agentic Workflow
on:
  workflow_dispatch:
engine: copilot
---

Say hello.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	noFixCmd := exec.Command(setup.binaryPath, "upgrade", "--no-fix", "--skip-extension-upgrade")
	noFixCmd.Dir = setup.tempDir
	noFixOutput, noFixErr := noFixCmd.CombinedOutput()
	require.NoError(t, noFixErr, "upgrade --no-fix should succeed, output: %s", noFixOutput)

	_, err := os.Stat(filepath.Join(setup.tempDir, ".github", "workflows", "example.lock.yml"))
	require.True(t, os.IsNotExist(err), "--no-fix should skip compilation")
}
