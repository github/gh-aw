//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateIntegrationTestSetup holds the setup state for update integration tests
type updateIntegrationTestSetup struct {
	tempDir      string
	originalWd   string
	binaryPath   string
	workflowsDir string
	cleanup      func()
}

// setupUpdateIntegrationTest creates a minimal test environment for update command:
// - temporary directory
// - git init (required by update command)
// - pre-built gh-aw binary
// - .github/workflows directory
func setupUpdateIntegrationTest(t *testing.T) *updateIntegrationTestSetup {
	t.Helper()

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gh-aw-update-integration-*")
	require.NoError(t, err, "Failed to create temp directory")

	// Save current working directory and change to temp directory
	originalWd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current working directory")

	err = os.Chdir(tempDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Initialize git repository (required by update command for merge detection)
	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = tempDir
	output, err := gitInitCmd.CombinedOutput()
	require.NoError(t, err, "Failed to run git init: %s", string(output))

	// Configure git user for commits
	gitConfigName := exec.Command("git", "config", "user.name", "Test User")
	gitConfigName.Dir = tempDir
	_ = gitConfigName.Run()

	gitConfigEmail := exec.Command("git", "config", "user.email", "test@example.com")
	gitConfigEmail.Dir = tempDir
	_ = gitConfigEmail.Run()

	// Copy the pre-built binary
	binaryPath := filepath.Join(tempDir, "gh-aw")
	err = fileutil.CopyFile(globalBinaryPath, binaryPath)
	require.NoError(t, err, "Failed to copy gh-aw binary to temp directory")

	err = os.Chmod(binaryPath, 0755)
	require.NoError(t, err, "Failed to make binary executable")

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Failed to create workflows directory")

	cleanup := func() {
		_ = os.Chdir(originalWd)
		_ = os.RemoveAll(tempDir)
	}

	return &updateIntegrationTestSetup{
		tempDir:      tempDir,
		originalWd:   originalWd,
		binaryPath:   binaryPath,
		workflowsDir: workflowsDir,
		cleanup:      cleanup,
	}
}

// skipWithoutGitHubAuth skips the test if GitHub authentication is not available
func skipWithoutGitHubAuth(t *testing.T) {
	t.Helper()
	authCmd := exec.Command("gh", "auth", "status")
	if err := authCmd.Run(); err != nil {
		t.Skip("Skipping test: GitHub authentication not available (gh auth status failed)")
	}
}

// --- Ref Resolution Integration Tests ---

// TestResolveLatestRef_TagIntegration verifies that a semantic version tag
// resolves to the latest release (within the same major version by default).
func TestResolveLatestRef_TagIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	// Use a well-known public repo with releases
	// actions/checkout has many tagged releases (v3.x, v4.x, etc.)
	latestRef, err := resolveLatestRef(context.Background(), "actions/checkout", "v4.0.0", false, true, 0)
	require.NoError(t, err, "Should resolve latest release for actions/checkout v4.x")

	// The resolved ref should be a newer v4.x tag
	assert.True(t, isSemanticVersionTag(latestRef), "Resolved ref should be a semantic version tag, got: %s", latestRef)
	assert.True(t, strings.HasPrefix(latestRef, "v4."), "Should stay within v4.x major version, got: %s", latestRef)
}

// TestResolveLatestRef_TagMajorUpdateIntegration verifies that --major allows cross-major updates.
func TestResolveLatestRef_TagMajorUpdateIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	// With allowMajor=true, it should resolve to the latest release across all major versions
	latestRef, err := resolveLatestRef(context.Background(), "actions/checkout", "v3.0.0", true, true, 0)
	require.NoError(t, err, "Should resolve latest release with major updates allowed")

	assert.True(t, isSemanticVersionTag(latestRef), "Resolved ref should be a semantic version tag, got: %s", latestRef)
	// With major updates allowed, it might return v4.x or later
}

// TestResolveLatestRef_BranchIntegration verifies that a branch name resolves
// to the latest commit SHA for that branch.
func TestResolveLatestRef_BranchIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	// Use a well-known branch on a public repo
	latestRef, err := resolveLatestRef(context.Background(), "actions/checkout", "main", false, true, 0)
	require.NoError(t, err, "Should resolve latest commit for branch 'main'")

	// The result should be a 40-char commit SHA
	assert.True(t, IsCommitSHA(latestRef), "Branch resolution should return a commit SHA, got: %s", latestRef)
}

// TestResolveLatestRef_CommitSHAIntegration verifies that a commit SHA resolves
// to the latest commit from the default branch.
func TestResolveLatestRef_CommitSHAIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	// Use a known commit SHA from actions/checkout
	// This is an older commit — the resolution should return the latest commit on the default branch
	oldSHA := "f43a0e5ff2bd294095638e18286ca9a3d1956744" // Known old commit

	latestRef, err := resolveLatestRef(context.Background(), "actions/checkout", oldSHA, false, true, 0)
	require.NoError(t, err, "Should resolve latest commit from default branch")

	// The result should be a 40-char commit SHA (the latest on main)
	assert.True(t, IsCommitSHA(latestRef), "SHA resolution should return a commit SHA, got: %s", latestRef)
	// The result should be different from the old SHA if there have been newer commits
	// (actions/checkout is actively maintained, so this should be true)
	assert.NotEqual(t, oldSHA, latestRef, "Should resolve to a newer commit than the old SHA")
}

// --- Update Command Integration Tests ---

// TestUpdateCommand_NoMergeFlag verifies that --no-merge flag is recognized.
func TestUpdateCommand_NoMergeFlag(t *testing.T) {
	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	// Run the update command with --no-merge flag — should be accepted
	cmd := exec.Command(setup.binaryPath, "update", "--no-merge", "--verbose")
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// The --no-merge flag should be recognized (not "unknown flag").
	// When no source workflows exist, the command succeeds with an info message.
	require.NoError(t, err, "Should succeed (no source workflows = info message, not error), output: %s", outputStr)
	assert.NotContains(t, outputStr, "unknown flag", "The --no-merge flag should be recognized")
	assert.Contains(t, outputStr, "no workflows found", "Should report no workflows found")
}

// TestUpdateCommand_NoRedirectFlag verifies that --no-redirect flag is recognized.
func TestUpdateCommand_NoRedirectFlag(t *testing.T) {
	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	cmd := exec.Command(setup.binaryPath, "update", "--no-redirect", "--verbose")
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	require.NoError(t, err, "Should succeed (no source workflows = info message, not error), output: %s", outputStr)
	assert.NotContains(t, outputStr, "unknown flag", "The --no-redirect flag should be recognized")
	assert.Contains(t, outputStr, "no workflows found", "Should report no workflows found")
}

// TestUpdateCommand_RemovedFlags verifies that old flags are no longer accepted.
func TestUpdateCommand_RemovedFlags(t *testing.T) {
	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	removedFlags := []string{"--merge", "--no-actions", "--audit", "--dry-run", "--json"}

	for _, flag := range removedFlags {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(setup.binaryPath, "update", flag)
			cmd.Dir = setup.tempDir
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			assert.Error(t, err, "Should reject removed flag %s", flag)
			assert.Contains(t, outputStr, "unknown flag", "Should report %s as unknown flag, output: %s", flag, outputStr)
		})
	}
}

// TestUpdateCommand_HelpText verifies the update command help text is correct.
func TestUpdateCommand_HelpText(t *testing.T) {
	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	cmd := exec.Command(setup.binaryPath, "update", "--help")
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Help should not return error")

	outputStr := string(output)

	// Should mention merge behavior
	assert.Contains(t, outputStr, "no-merge", "Help should document --no-merge flag")
	assert.Contains(t, outputStr, "no-redirect", "Help should document --no-redirect flag")
	assert.Contains(t, outputStr, "no-security-scanner", "Help should document --no-security-scanner flag")
	assert.Contains(t, outputStr, "repo", "Help should document --repo flag")
	assert.Contains(t, outputStr, "org", "Help should document --org flag")
	assert.Contains(t, outputStr, "repos", "Help should document --repos flag")
	assert.Contains(t, outputStr, "--pr", "Help should document the draft pull request flag")
	assert.Contains(t, outputStr, "3-way merge", "Help should explain merge behavior")

	// Should reference upgrade for other features
	assert.Contains(t, outputStr, "upgrade", "Help should reference 'gh aw upgrade' for other features")

	// Should NOT mention removed flags
	assert.NotContains(t, outputStr, "--audit", "Help should not mention removed --audit flag")
	assert.NotContains(t, outputStr, "--dry-run", "Help should not mention removed --dry-run flag")
}

// TestUpdateCommand_RepoFlag verifies that --repo is recognized.
func TestUpdateCommand_RepoFlag(t *testing.T) {
	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	// Use an invalid repo slug to avoid network calls while still validating flag parsing.
	cmd := exec.Command(setup.binaryPath, "update", "--repo", "not-a-valid-slug", "--verbose")
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	assert.Error(t, err, "Command should fail for invalid repo slug")
	assert.NotContains(t, outputStr, "unknown flag", "The --repo flag should be recognized")
}

// TestUpdateCommand_AfterManifestAddIntegration verifies a manifest package can be
// added and then updated in place.
func TestUpdateCommand_AfterManifestAddIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	addCmd := exec.Command(setup.binaryPath, "add", "githubnext/agentic-ops@v-1", "--verbose")
	addCmd.Dir = setup.tempDir
	addOutput, addErr := addCmd.CombinedOutput()
	addOutputStr := string(addOutput)
	require.NoError(t, addErr, "add command should succeed: %s", addOutputStr)

	entries, err := os.ReadDir(setup.workflowsDir)
	require.NoError(t, err, "should read workflows directory")
	require.NotEmpty(t, entries, "add should create at least one workflow")

	var workflowPath string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		workflowPath = filepath.Join(setup.workflowsDir, entry.Name())
		break
	}
	require.NotEmpty(t, workflowPath, "expected at least one markdown workflow file")

	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "should read added workflow")
	assert.Contains(t, string(content), "source: githubnext/agentic-ops@", "workflow source should be manifest-scoped")

	updateCmd := exec.Command(setup.binaryPath, "update", "--verbose")
	updateCmd.Dir = setup.tempDir
	updateOutput, updateErr := updateCmd.CombinedOutput()
	updateOutputStr := string(updateOutput)
	require.NoError(t, updateErr, "update command should succeed after manifest add: %s", updateOutputStr)
	assert.NotContains(t, updateOutputStr, "no workflows found", "update should detect added workflows")
}

// --- Merge Behavior Integration Tests ---

// TestUpdateCommand_MergeIsDefault verifies that merge is the default behavior
// when a workflow has local modifications.
func TestUpdateCommand_MergeIsDefault(t *testing.T) {
	skipWithoutGitHubAuth(t)

	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	// Create a workflow with a source field that can be fetched
	workflowContent := `---
source: github/gh-aw/.github/workflows/smoke-test-push.md@main
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

Local modification that should be preserved during merge.
`
	workflowPath := filepath.Join(setup.workflowsDir, "test-update.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644))

	// Commit the workflow to git so hasLocalModifications can detect changes
	gitAdd := exec.Command("git", "add", "-A")
	gitAdd.Dir = setup.tempDir
	require.NoError(t, gitAdd.Run(), "Failed to git add")

	gitCommit := exec.Command("git", "commit", "-m", "initial commit")
	gitCommit.Dir = setup.tempDir
	require.NoError(t, gitCommit.Run(), "Failed to git commit")

	// Make a local modification
	modifiedContent := strings.Replace(workflowContent,
		"Local modification that should be preserved during merge.",
		"This is my custom local addition.\n\nLocal modification that should be preserved during merge.",
		1)
	require.NoError(t, os.WriteFile(workflowPath, []byte(modifiedContent), 0644))

	// Run update with default merge behavior (no --no-merge flag)
	cmd := exec.Command(setup.binaryPath, "update", "test-update", "--verbose")
	cmd.Dir = setup.tempDir
	output, _ := cmd.CombinedOutput()
	outputStr := string(output)

	// The command should produce output (may succeed or fail depending on
	// source repo accessibility, but must attempt merge, not override).
	t.Logf("Update output: %s", outputStr)
	assert.NotEmpty(t, outputStr, "Update command should produce output")

	// Verify it does NOT report "override mode" — merge is the default
	assert.NotContains(t, outputStr, "Using override mode",
		"Default behavior should use merge, not override")

	// Read the resulting workflow file to verify it still exists
	updatedContent, err := os.ReadFile(filepath.Join(setup.workflowsDir, "test-update.md"))
	require.NoError(t, err, "Workflow file should still exist after update")
	assert.Contains(t, string(updatedContent), "Local modification",
		"Local content should be preserved when merge is the default")
}

// --- ParseSourceSpec Integration Tests ---

// TestParseSourceSpec_Integration verifies source spec parsing for various formats.
func TestParseSourceSpec_Integration(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		expectedRepo string
		expectedPath string
		expectedRef  string
	}{
		{
			name:         "standard format with tag",
			source:       "github/gh-aw/.github/workflows/workflow.md@v1.2.3",
			expectedRepo: "github/gh-aw",
			expectedPath: ".github/workflows/workflow.md",
			expectedRef:  "v1.2.3",
		},
		{
			name:         "standard format with branch",
			source:       "githubnext/agentics/workflows/repo-assist.md@main",
			expectedRepo: "githubnext/agentics",
			expectedPath: "workflows/repo-assist.md",
			expectedRef:  "main",
		},
		{
			name:         "standard format with commit SHA",
			source:       "githubnext/agentics/workflows/repo-assist.md@6c79ed2ea350161ad5dcc9624cf510f134c6a9e3",
			expectedRepo: "githubnext/agentics",
			expectedPath: "workflows/repo-assist.md",
			expectedRef:  "6c79ed2ea350161ad5dcc9624cf510f134c6a9e3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseSourceSpec(tt.source)
			require.NoError(t, err, "Should parse source spec: %s", tt.source)

			assert.Equal(t, tt.expectedRepo, spec.Repo, "Repo should match")
			assert.Equal(t, tt.expectedPath, spec.Path, "Path should match")
			assert.Equal(t, tt.expectedRef, spec.Ref, "Ref should match")
		})
	}
}

// TestIsCommitSHA_Integration tests commit SHA identification with real-world examples.
func TestIsCommitSHA_Integration(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{"real commit SHA", "6c79ed2ea350161ad5dcc9624cf510f134c6a9e3", true},
		{"another SHA", "f43a0e5ff2bd294095638e18286ca9a3d1956744", true},
		{"v-prefixed tag", "v1.2.3", false},
		{"branch name", "main", false},
		{"short SHA", "6c79ed2", false},
		{"39 chars", "6c79ed2ea350161ad5dcc9624cf510f134c6a9e", false},
		{"41 chars", "6c79ed2ea350161ad5dcc9624cf510f134c6a9e39", false},
		{"non-hex 40 chars", "ghijklmnopqrstuvwxyz12345678901234567890", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsCommitSHA(tt.ref), "IsCommitSHA(%q)", tt.ref)
		})
	}
}

// --- Helper Function Tests ---

// TestGetRepoDefaultBranch_Integration verifies fetching the default branch via GitHub API.
func TestGetRepoDefaultBranch_Integration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	branch, err := getRepoDefaultBranch(context.Background(), "actions/checkout")
	require.NoError(t, err, "Should fetch default branch for actions/checkout")
	assert.Equal(t, "main", branch, "actions/checkout default branch should be 'main'")
}

// TestGetLatestBranchCommitInfo_Integration verifies fetching the latest commit info for a branch.
func TestGetLatestBranchCommitInfo_Integration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	info, err := getLatestBranchCommitInfo(context.Background(), "actions/checkout", "main")
	require.NoError(t, err, "Should fetch latest commit info for actions/checkout main branch")
	assert.True(t, IsCommitSHA(info.SHA), "Result should be a 40-char commit SHA, got: %s", info.SHA)
	assert.False(t, info.CommittedAt.IsZero(), "CommittedAt should be populated")
}

// --- Org Dry-Run Integration Tests ---

// TestUpdateCommand_OrgDryRunIntegration verifies that --org mode runs in dry-run
// (the default, without --create-pull-request or --create-issue) for two known
// repositories: github/gh-aw and github/gh-aw-firewall. The test validates that
// the command completes successfully and produces well-formed output regardless
// of whether the repos have pending updates.
func TestUpdateCommand_OrgDryRunIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	// Run update --org github targeting only gh-aw and gh-aw-firewall.
	// No --create-pull-request or --create-issue flags → dry-run mode (default).
	cmd := exec.Command(setup.binaryPath, "update",
		"--org", "github",
		"--repos", "gh-aw,gh-aw-firewall",
	)
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("Output:\n%s", outputStr)

	require.NoError(t, err, "org dry-run should succeed: %s", outputStr)

	// --org and --repos flags must be recognised.
	assert.NotContains(t, outputStr, "unknown flag", "All flags should be recognized")

	// The output must indicate one of the three valid terminal states:
	//   1. Dry-run preview with pending updates listed.
	//   2. All repos already up-to-date.
	//   3. No repos with source-managed workflows found in the filtered set.
	dryRunPreview := strings.Contains(outputStr, "Dry-run preview")
	alreadyUpToDate := strings.Contains(outputStr, "up to date")
	noReposFound := strings.Contains(outputStr, "No repositories")
	assert.True(t, dryRunPreview || alreadyUpToDate || noReposFound,
		"Output should indicate dry-run preview, up-to-date status, or no repos found; got: %s", outputStr)
}

// TestUpdateCommand_OrgSingleRepoDryRunIntegration verifies that --org mode can
// target a single repository using --repos gh-aw and still complete in dry-run
// mode without requiring write operations.
func TestUpdateCommand_OrgSingleRepoDryRunIntegration(t *testing.T) {
	skipWithoutGitHubAuth(t)

	setup := setupUpdateIntegrationTest(t)
	defer setup.cleanup()

	cmd := exec.Command(setup.binaryPath, "update",
		"--org", "github",
		"--repos", "gh-aw",
	)
	cmd.Dir = setup.tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("Output:\n%s", outputStr)

	require.NoError(t, err, "org single-repo dry-run should succeed: %s", outputStr)
	assert.NotContains(t, outputStr, "unknown flag", "All flags should be recognised")

	dryRunPreview := strings.Contains(outputStr, "Dry-run preview")
	alreadyUpToDate := strings.Contains(outputStr, "up to date")
	noReposFound := strings.Contains(outputStr, "No repositories")
	assert.True(t, dryRunPreview || alreadyUpToDate || noReposFound,
		"Output should indicate dry-run preview, up-to-date status, or no repos found; got: %s", outputStr)
}
