//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAutoUpdateWorkflow_Enabled(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     true,
		RepoSlug:    "owner/repo",
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed when enabled")

	outputPath := filepath.Join(dir, AutoUpdateWorkflowFileName)
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err, "agentic-auto-upgrade.yml should be written")

	content := string(data)
	assert.Contains(t, content, "name: Agentic Auto-Upgrade", "should include workflow name")
	assert.Contains(t, content, "cron:", "should include cron schedule")
	assert.Contains(t, content, "Weekly (auto-upgrade)", "should include schedule comment")
	assert.Contains(t, content, "workflow_dispatch:", "should include workflow_dispatch trigger")
	assert.Contains(t, content, "contents: read", "should grant contents: read for checkout")
	assert.Contains(t, content, "issues: write", "should grant issues: write")
	assert.Contains(t, content, "run_operation_update_upgrade.cjs", "should inline upgrade JS")
	assert.Contains(t, content, "GH_AW_OPERATION: upgrade", "should set upgrade operation")
	assert.Contains(t, content, "GH_AW_CMD_PREFIX: ./gh-aw", "should use dev CLI prefix by default")
	assert.Contains(t, content, "Checkout repository", "should include checkout step")
	assert.Contains(t, content, "Build gh-aw", "should include local gh-aw build step in dev mode")
	assert.Contains(t, content, "mainNotifyIssue", "should call mainNotifyIssue")
	assert.NotContains(t, content, "uses: ./.github/workflows/agentics-maintenance.yml", "should not use workflow_call")
	assert.NotContains(t, content, "actions: write", "should not grant actions: write")
	assert.NotContains(t, content, "contents: write", "should not grant contents: write")
	assert.NotContains(t, content, "pull-requests: write", "should not grant pull-requests: write")
}

func TestGenerateAutoUpdateWorkflow_Disabled(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     false,
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed when disabled")

	outputPath := filepath.Join(dir, AutoUpdateWorkflowFileName)
	_, err = os.Stat(outputPath)
	assert.True(t, os.IsNotExist(err), "agentic-auto-upgrade.yml should not be created when disabled")
}

func TestGenerateAutoUpdateWorkflow_UpgradeOptions(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir:    dir,
		Enabled:        true,
		UpgradeOptions: []string{"--pre-releases"},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), `GH_AW_UPGRADE_OPTIONS: '["--pre-releases"]'`)
}

func TestGenerateAutoUpdateWorkflow_DisabledDeletesExistingFile(t *testing.T) {
	dir := t.TempDir()

	// Create an existing file to simulate a previously-generated workflow.
	outputPath := filepath.Join(dir, AutoUpdateWorkflowFileName)
	require.NoError(t, os.WriteFile(outputPath, []byte("old content"), 0o644))

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     false,
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed when disabled")

	_, err = os.Stat(outputPath)
	assert.True(t, os.IsNotExist(err), "existing agentic-auto-upgrade.yml should be deleted when disabled")
}

func TestGenerateAutoUpdateWorkflow_CronIsDeterministic(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	opts := GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir1,
		Enabled:     true,
		RepoSlug:    "myorg/myrepo",
	}
	require.NoError(t, GenerateAutoUpdateWorkflow(opts))

	opts.WorkflowDir = dir2
	require.NoError(t, GenerateAutoUpdateWorkflow(opts))

	data1, err := os.ReadFile(filepath.Join(dir1, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	data2, err := os.ReadFile(filepath.Join(dir2, AutoUpdateWorkflowFileName))
	require.NoError(t, err)

	assert.Equal(t, string(data1), string(data2), "same repo slug should produce identical output")
}

func TestGenerateAutoUpdateWorkflow_DifferentReposDifferentCron(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Per-repo schedule scattering applies in all non-dev modes.
	// Use ActionModeAction since that is what released binaries actually detect.
	require.NoError(t, GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir1,
		Enabled:     true,
		RepoSlug:    "org1/repo-alpha",
		ActionMode:  ActionModeAction,
	}))
	require.NoError(t, GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir2,
		Enabled:     true,
		RepoSlug:    "org2/repo-beta",
		ActionMode:  ActionModeAction,
	}))

	data1, err := os.ReadFile(filepath.Join(dir1, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	data2, err := os.ReadFile(filepath.Join(dir2, AutoUpdateWorkflowFileName))
	require.NoError(t, err)

	// Extract cron lines and compare — different repos should (almost certainly) scatter differently.
	cron1 := extractCronLine(string(data1))
	cron2 := extractCronLine(string(data2))
	assert.NotEmpty(t, cron1, "cron should be non-empty for org1/repo-alpha")
	assert.NotEmpty(t, cron2, "cron should be non-empty for org2/repo-beta")
	// Schedules are scattered by hash — different repos should typically differ.
	// This is a best-effort check; hash collisions are possible but unlikely for these slugs.
	assert.NotEqual(t, cron1, cron2, "different repo slugs should produce different cron schedules in action mode")
}

func TestGenerateAutoUpdateWorkflow_NoRepoSlug(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     true,
		RepoSlug:    "",
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed with empty repo slug")

	content, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	assert.Contains(t, string(content), "cron:", "should still generate a cron schedule without repo slug")
}

func TestGenerateAutoUpdateWorkflow_CustomActionRefs(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir:     dir,
		Enabled:         true,
		RepoSlug:        "owner/repo",
		SetupActionRef:  "github/gh-aw/actions/setup@abc123",
		GitHubScriptPin: "actions/github-script@def456",
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed with custom action refs")

	content, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	assert.Contains(t, string(content), "github/gh-aw/actions/setup@abc123", "should use custom setup action ref")
	assert.Contains(t, string(content), "actions/github-script@def456", "should use custom github-script pin")
}

func TestGenerateAutoUpdateWorkflow_ReleaseModeUsesGhAwPrefix(t *testing.T) {
	dir := t.TempDir()

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     true,
		RepoSlug:    "owner/repo",
		ActionMode:  ActionModeRelease,
		Version:     "v1.2.3",
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed in release mode")

	content, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	assert.Contains(t, string(content), "GH_AW_CMD_PREFIX: gh aw", "should use gh aw prefix outside dev mode")
	assert.Contains(t, string(content), "name: Install gh-aw", "should install gh-aw in release mode")
	assert.NotContains(t, string(content), "Build gh-aw", "should not build gh-aw from source in release mode")
}

func TestGenerateAutoUpdateWorkflow_DevModeScheduleStable(t *testing.T) {
	// Dev mode must produce the same schedule regardless of whether --schedule-seed /
	// a repo slug is provided. This is the key stability property: agents and CI that
	// call `gh aw compile` without --schedule-seed should not cause agentic-auto-upgrade.yml
	// to differ from a build that passes --schedule-seed.
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	require.NoError(t, GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir1,
		Enabled:     true,
		RepoSlug:    "github/gh-aw", // simulates --schedule-seed github/gh-aw
		ActionMode:  ActionModeDev,
	}))
	require.NoError(t, GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir2,
		Enabled:     true,
		RepoSlug:    "", // simulates sandbox/CI where git remote detection yields empty slug
		ActionMode:  ActionModeDev,
	}))

	data1, err := os.ReadFile(filepath.Join(dir1, AutoUpdateWorkflowFileName))
	require.NoError(t, err)
	data2, err := os.ReadFile(filepath.Join(dir2, AutoUpdateWorkflowFileName))
	require.NoError(t, err)

	cron1 := extractCronLine(string(data1))
	cron2 := extractCronLine(string(data2))
	assert.NotEmpty(t, cron1, "cron should be present with slug in dev mode")
	assert.NotEmpty(t, cron2, "cron should be present without slug in dev mode")
	assert.Equal(t, cron1, cron2, "dev mode schedule must be identical regardless of repo slug")
}

func TestBuildAutoUpdateSeed(t *testing.T) {
	// Release mode: incorporates repo slug for per-repo scattering.
	assert.Equal(t, "owner/repo/agentic-auto-upgrade", buildAutoUpdateSeed("owner/repo", ActionModeRelease))
	assert.Equal(t, "agentic-auto-upgrade", buildAutoUpdateSeed("", ActionModeRelease))

	// Action mode (used by released binaries): also incorporates repo slug for per-repo scattering.
	assert.Equal(t, "owner/repo/agentic-auto-upgrade", buildAutoUpdateSeed("owner/repo", ActionModeAction))
	assert.Equal(t, "agentic-auto-upgrade", buildAutoUpdateSeed("", ActionModeAction))

	// Script mode: also incorporates repo slug for per-repo scattering.
	assert.Equal(t, "owner/repo/agentic-auto-upgrade", buildAutoUpdateSeed("owner/repo", ActionModeScript))
	assert.Equal(t, "agentic-auto-upgrade", buildAutoUpdateSeed("", ActionModeScript))

	// Dev mode: always uses the stable "dev/" prefix regardless of repo slug,
	// so that the schedule does not change depending on whether --schedule-seed
	// is passed or whether git remote detection succeeds.
	assert.Equal(t, "dev/agentic-auto-upgrade", buildAutoUpdateSeed("owner/repo", ActionModeDev))
	assert.Equal(t, "dev/agentic-auto-upgrade", buildAutoUpdateSeed("", ActionModeDev))
}

// extractCronLine returns the cron expression from the first `- cron:` line in the YAML.
func extractCronLine(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- cron:") {
			return trimmed
		}
	}
	return ""
}

func TestGenerateAutoUpdateWorkflow_CustomCron(t *testing.T) {
	dir := t.TempDir()
	const customCron = "0 9 * * 1"

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     true,
		RepoSlug:    "owner/repo",
		CustomCron:  customCron,
	})
	require.NoError(t, err, "GenerateAutoUpdateWorkflow should succeed with custom cron")

	content, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)

	assert.Contains(t, string(content), `cron: "0 9 * * 1"`, "should use the custom cron expression verbatim")
	assert.Contains(t, string(content), "Custom schedule (auto-upgrade)", "should use custom schedule comment for custom cron")
	assert.NotContains(t, string(content), "Weekly (auto-upgrade)", "should not use weekly comment for custom cron")
	assert.Contains(t, string(content), "auto_upgrade.cron is set in aw.json", "header should describe custom cron configuration")
}

func TestGenerateAutoUpdateWorkflow_CustomCronOverridesFuzzy(t *testing.T) {
	dir := t.TempDir()
	const customCron = "30 5 * * 1-5"

	err := GenerateAutoUpdateWorkflow(GenerateAutoUpdateWorkflowOptions{
		WorkflowDir: dir,
		Enabled:     true,
		RepoSlug:    "org/repo",
		ActionMode:  ActionModeAction,
		CustomCron:  customCron,
	})
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, AutoUpdateWorkflowFileName))
	require.NoError(t, err)

	cronLine := extractCronLine(string(content))
	assert.Contains(t, cronLine, customCron, "custom cron should override the per-repo fuzzy weekly schedule")
	assert.Contains(t, string(content), "Custom schedule (auto-upgrade)", "should use custom schedule comment")
	assert.NotContains(t, string(content), "Weekly (auto-upgrade)", "should not use weekly comment when custom cron is set")
}
