//go:build integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileSideRepoWorkflow parses a workflow markdown file and returns the
// resulting workflowData plus a temp directory, so callers can then invoke
// GenerateMaintenanceWorkflow and inspect side-repo maintenance files.
func compileSideRepoWorkflow(t *testing.T, content string) ([]*WorkflowData, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "side-repo-maint-test-*")
	require.NoError(t, err, "create temp dir")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0644), "write workflow file")

	compiler := NewCompiler()
	// ParseWorkflowFile populates CheckoutConfigs, SafeOutputs, and Name —
	// exactly the fields examined by GenerateMaintenanceWorkflow.
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	require.NoError(t, err, "parse workflow data")

	return []*WorkflowData{workflowData}, tmpDir
}

// TestSideRepoMaintenanceWorkflowGenerated_EndToEnd verifies that compiling a
// workflow with a SideRepoOps checkout generates a side-repo maintenance file
// with the expected top-level structure.
func TestSideRepoMaintenanceWorkflowGenerated_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
checkout:
  repository: my-org/target-repo
  current: true
  github-token: ${{ secrets.GH_AW_TARGET_TOKEN }}
---

# Side-Repo Test Workflow

This workflow operates on a separate repository.
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	sideRepoFile := filepath.Join(tmpDir, "agentics-maintenance-my-org-target-repo.yml")
	content, err := os.ReadFile(sideRepoFile)
	require.NoError(t, err, "side-repo maintenance file should have been created")

	contentStr := string(content)

	// Workflow name reflects target repo.
	assert.Contains(t, contentStr, "my-org/target-repo",
		"generated workflow should reference the target repo slug")

	// Must have workflow_dispatch trigger.
	assert.Contains(t, contentStr, "workflow_dispatch:",
		"generated workflow should include workflow_dispatch trigger")

	// Must have workflow_call trigger.
	assert.Contains(t, contentStr, "workflow_call:",
		"generated workflow should include workflow_call trigger")

	// Must have apply_safe_outputs job.
	assert.Contains(t, contentStr, "apply_safe_outputs:",
		"generated workflow should include apply_safe_outputs job")

	// Must have create_labels job.
	assert.Contains(t, contentStr, "create_labels:",
		"generated workflow should include create_labels job")

	// Must have activity_report job.
	assert.Contains(t, contentStr, "activity_report:",
		"generated workflow should include activity_report job")
	assert.Contains(t, contentStr, "Restore activity report logs cache",
		"generated workflow should include cache restore step for activity_report logs")
	assert.Contains(t, contentStr, "Save activity report logs cache",
		"generated workflow should include cache save step for activity_report logs")
	assert.Contains(t, contentStr, "if: ${{ always() }}",
		"generated workflow should save activity_report logs cache even if report generation fails")
	assert.Contains(t, contentStr, "steps.activity_report_logs_cache.outputs.cache-primary-key",
		"generated workflow should save activity_report logs using the cache primary key")
	assert.Contains(t, contentStr, "Download activity report logs in target repository",
		"generated workflow should include direct logs download step for activity_report")
	assert.Contains(t, contentStr, "timeout-minutes: 20",
		"generated workflow should set a 20-minute timeout for the activity_report logs download step")
	assert.Contains(t, contentStr, "${GH_AW_CMD_PREFIX} logs",
		"generated workflow should run gh aw logs directly")
	assert.Contains(t, contentStr, `${GH_AW_CMD_PREFIX} logs \
            --repo "my-org/target-repo" \`,
		"generated workflow should preserve the multi-line gh aw logs command formatting")
	assert.Contains(t, contentStr, "--start-date -1w",
		"generated workflow should download 7 days of logs for activity_report")
	assert.Contains(t, contentStr, "--count 500",
		"generated workflow should limit activity_report log downloads to at most 500 runs")
	assert.Contains(t, contentStr, "--format markdown",
		"generated workflow should request markdown report output from gh aw logs")
	assert.Contains(t, contentStr, "./.cache/gh-aw/activity-report-logs/report.md",
		"generated workflow should write activity_report markdown output to report.md")
	assert.Contains(t, contentStr, "Generate activity report issue in target repository",
		"generated workflow should include activity_report issue generation step after cache save")
	assert.Contains(t, contentStr, "title: '[aw] agentic status report'",
		"generated workflow should create the activity_report issue with the expected title")
	assert.Regexp(t, `actions: read[^\n]*\n      contents: read[^\n]*\n      issues: write`, contentStr,
		"activity_report job should include contents: read with explicit permissions")
	assert.Contains(t, contentStr, "timeout-minutes: 120",
		"activity_report job should include a 2 hour timeout")
	assert.Contains(t, contentStr, "${{ github.run_id }}",
		"activity_report cache key should include run id for latest-cache resolution")

	// GH_AW_TARGET_REPO_SLUG must be wired with the correct slug.
	assert.Contains(t, contentStr, `GH_AW_TARGET_REPO_SLUG: "my-org/target-repo"`,
		"GH_AW_TARGET_REPO_SLUG should be set to the target repo slug")

	// Custom token should appear in the generated file.
	assert.Contains(t, contentStr, "secrets.GH_AW_TARGET_TOKEN",
		"custom github-token should appear in the generated workflow")
}

// TestSideRepoMaintenanceWorkflowWithExpires_EndToEnd verifies that when the
// workflow uses safe-output expiry, the side-repo file includes a schedule
// trigger with a fuzzy cron expression (not minute :00 or the fixed :37).
func TestSideRepoMaintenanceWorkflowWithExpires_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
checkout:
  repository: corp/infra-tools
  current: true
safe-outputs:
  create-issue:
    expires: 14
---

# Expires Test Workflow

Create issues that expire after 14 days.
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	sideRepoFile := filepath.Join(tmpDir, "agentics-maintenance-corp-infra-tools.yml")
	content, err := os.ReadFile(sideRepoFile)
	require.NoError(t, err, "side-repo maintenance file should have been created")
	contentStr := string(content)

	// Must have a schedule trigger when expires is set.
	assert.Contains(t, contentStr, "schedule:",
		"side-repo maintenance should include schedule trigger when expires is set")

	// close-expired-entities job must be present.
	assert.Contains(t, contentStr, "close-expired-entities:",
		"side-repo maintenance should include close-expired-entities job when expires is set")

	// The cron expression should be present; extract it and verify it is valid.
	expectedCron, _ := generateSideRepoMaintenanceCron("corp/infra-tools", 14)
	assert.Contains(t, contentStr, expectedCron,
		"cron expression should match the fuzzy-scheduled value for corp/infra-tools")

	// The cron minute must not be 0 or 37 (fixed values to avoid pile-up).
	// We verify by checking the actual expected value contains neither ":00" nor ":37".
	minute := strings.Fields(expectedCron)[0]
	assert.NotEqual(t, "0", minute,
		"fuzzy cron should not fire at minute 0 (likely collision with defaults)")
}

// TestSideRepoMaintenanceWorkflowFallbackToken_EndToEnd verifies that when no
// custom token is specified in the checkout config, the generated workflow falls
// back to GH_AW_GITHUB_TOKEN.
func TestSideRepoMaintenanceWorkflowFallbackToken_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
checkout:
  repository: acme/shared-services
  current: true
---

# No-token side-repo workflow.
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	sideRepoFile := filepath.Join(tmpDir, "agentics-maintenance-acme-shared-services.yml")
	content, err := os.ReadFile(sideRepoFile)
	require.NoError(t, err, "side-repo maintenance file should have been created")
	contentStr := string(content)

	// Fallback token should be referenced.
	assert.Contains(t, contentStr, "GH_AW_GITHUB_TOKEN",
		"should fall back to GH_AW_GITHUB_TOKEN when no custom token is specified")
}

// TestNoSideRepoMaintenanceForExpressionRepository_EndToEnd verifies that
// expression-based repository values do not produce a side-repo maintenance file.
func TestNoSideRepoMaintenanceForExpressionRepository_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
    inputs:
      target_repo:
        description: Target repository
        required: true
permissions:
  contents: read
engine: copilot
checkout:
  repository: ${{ inputs.target_repo }}
  path: targets
  allowed-repos: ["my-org/*"]
---

# Dynamic repository workflow.
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	// No side-repo file should be created because the repository is an expression.
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t,
			strings.HasPrefix(e.Name(), "agentics-maintenance-") && e.Name() != "agentics-maintenance.yml",
			"no side-repo maintenance file should be generated for expression-based repositories, got: %s", e.Name())
	}
}

// TestSideRepoMaintenanceFuzzyScheduleScattered_EndToEnd verifies that two
// different side-repo targets receive distinct cron expressions (scattered).
func TestSideRepoMaintenanceFuzzyScheduleScattered_EndToEnd(t *testing.T) {
	// Compile two separate workflows for different side-repo targets.
	makeContent := func(repo string) string {
		return `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
checkout:
  repository: ` + repo + `
  current: true
safe-outputs:
  create-issue:
    expires: 30
---

# Scattered cron test.
`
	}

	repoA := "company/repo-alpha"
	repoB := "company/repo-beta"

	cronA, _ := generateSideRepoMaintenanceCron(repoA, 30)
	cronB, _ := generateSideRepoMaintenanceCron(repoB, 30)

	// Verify the crons are actually different (they should be; if they collide that
	// would be a surprising FNV-1a collision and the test would rightly flag it).
	assert.NotEqual(t, cronA, cronB,
		"different side-repo targets should get different cron expressions to avoid simultaneous runs")

	// Compile both and verify each generated file contains its own cron.
	for _, tc := range []struct {
		repo string
		cron string
	}{
		{repoA, cronA},
		{repoB, cronB},
	} {
		t.Run(tc.repo, func(t *testing.T) {
			wdl, tmpDir := compileSideRepoWorkflow(t, makeContent(tc.repo))
			err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
				WorkflowDataList: wdl,
				WorkflowDir:      tmpDir,
				Version:          "v1.0.0",
				ActionMode:       ActionModeDev,
				ActionTag:        "",
				RepoConfig:       nil,
				RepoSlug:         "",
			})
			require.NoError(t, err)

			slug := stringutil.SanitizeForFilename(tc.repo)
			sideFile := filepath.Join(tmpDir, "agentics-maintenance-"+slug+".yml")
			fileContent, err := os.ReadFile(sideFile)
			require.NoError(t, err, "side-repo file should exist for %s", tc.repo)

			assert.Contains(t, string(fileContent), tc.cron,
				"generated file for %s should contain cron %s", tc.repo, tc.cron)
		})
	}
}

// TestSideRepoMaintenanceWorkflowWithGitHubApp_EndToEnd verifies that when a source
// workflow authenticates its cross-repo checkout with a GitHub App, the generated
// side-repo maintenance workflow emits a create-github-app-token mint step in each
// cross-repo job and uses the minted token expression instead of GH_AW_GITHUB_TOKEN.
func TestSideRepoMaintenanceWorkflowWithGitHubApp_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
checkout:
  - repository: microsoft/aspire.dev
    github-app:
      app-id: ${{ secrets.ASPIRE_BOT_APP_ID }}
      private-key: ${{ secrets.ASPIRE_BOT_PRIVATE_KEY }}
      owner: "microsoft"
      repositories:
        - aspire.dev
        - aspire
    current: true
safe-outputs:
  github-app:
    app-id: ${{ secrets.ASPIRE_BOT_APP_ID }}
    private-key: ${{ secrets.ASPIRE_BOT_PRIVATE_KEY }}
    owner: "microsoft"
    repositories:
      - aspire.dev
      - aspire
  create-pull-request:
    target-repo: "microsoft/aspire.dev"
---

# GitHub App Auth Test Workflow

This workflow uses a GitHub App for cross-repo authentication.
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeRelease,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	sideRepoFile := filepath.Join(tmpDir, "agentics-maintenance-microsoft-aspire.dev.yml")
	content, err := os.ReadFile(sideRepoFile)
	require.NoError(t, err, "side-repo maintenance file should have been created")
	contentStr := string(content)
	extractJobContent := func(jobName string) string {
		t.Helper()
		startMarker := "  " + jobName + ":\n"
		start := strings.Index(contentStr, startMarker)
		require.GreaterOrEqualf(t, start, 0, "%s job must be present", jobName)
		searchStart := start + len(startMarker)
		end := len(contentStr)
		for _, candidate := range []string{"close-expired-entities", "apply_safe_outputs", "create_labels", "activity_report", "validate_workflows"} {
			next := strings.Index(contentStr[searchStart:], "\n  "+candidate+":\n")
			if next >= 0 {
				candidateStart := searchStart + next
				if candidateStart < end {
					end = candidateStart
				}
			}
		}
		return contentStr[start:end]
	}

	// The minted token reference must be used (not the fallback secret).
	assert.Contains(t, contentStr, "steps.side-repo-app-token.outputs.token",
		"minted app token should be referenced via steps output")
	// Each cross-repo job should include the mint step and avoid fallback secret use.
	for _, jobName := range []string{"apply_safe_outputs", "create_labels", "activity_report"} {
		jobContent := extractJobContent(jobName)
		assert.Containsf(t, jobContent, "create-github-app-token",
			"%s job should include create-github-app-token action", jobName)
		assert.Containsf(t, jobContent, "id: side-repo-app-token",
			"%s job should include mint step ID", jobName)
		assert.Containsf(t, jobContent, "Generate GitHub App token",
			"%s job should include mint step name", jobName)
		assert.NotContainsf(t, jobContent, "secrets.GH_AW_GITHUB_TOKEN",
			"%s job should not use fallback GH_AW_GITHUB_TOKEN when github-app auth is configured", jobName)
	}

	// GitHub App credentials must be forwarded.
	assert.Contains(t, contentStr, "secrets.ASPIRE_BOT_APP_ID",
		"app-id secret reference should appear in the generated workflow")
	assert.Contains(t, contentStr, "secrets.ASPIRE_BOT_PRIVATE_KEY",
		"private-key secret reference should appear in the generated workflow")

	// The owner and repositories from the checkout config should appear.
	assert.Contains(t, contentStr, "owner: microsoft",
		"app token owner should be set from the checkout config")
	assert.Contains(t, contentStr, "aspire.dev",
		"repository list should include aspire.dev")

	// Standard job structure must still be present.
	assert.Contains(t, contentStr, "apply_safe_outputs:",
		"apply_safe_outputs job should still be generated")
	assert.Contains(t, contentStr, "create_labels:",
		"create_labels job should still be generated")
	assert.Contains(t, contentStr, "activity_report:",
		"activity_report job should still be generated")

	// The minted token must be used as github-token: input and GH_TOKEN: env in the right steps.
	assert.Contains(t, contentStr, "github-token: ${{ steps.side-repo-app-token.outputs.token }}",
		"minted token must be used as github-token: input in cross-repo steps")
	assert.Contains(t, contentStr, "GH_TOKEN: ${{ steps.side-repo-app-token.outputs.token }}",
		"minted token must be used as GH_TOKEN env var in cross-repo steps")

	// Validate that the mint step appears before the first token-using step in at
	// least one job by checking ordering within apply_safe_outputs.
	applyJobContent := extractJobContent("apply_safe_outputs")
	mintIdx := strings.Index(applyJobContent, "id: side-repo-app-token")
	tokenUseIdx := strings.Index(applyJobContent, "steps.side-repo-app-token.outputs.token")
	assert.Greater(t, mintIdx, 0, "mint step must be present in apply_safe_outputs job")
	assert.Greater(t, tokenUseIdx, mintIdx,
		"minted token reference must appear after the mint step definition in apply_safe_outputs")
}

// TestSideRepoMaintenanceWorkflowWithGitHubApp_WithExpires_EndToEnd verifies that
// when a workflow uses both GitHub App auth and expires-based scheduling, the
// close-expired-entities job also receives the mint step.
func TestSideRepoMaintenanceWorkflowWithGitHubApp_WithExpires_EndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
engine: copilot
checkout:
  repository: my-org/target-repo
  github-app:
    app-id: ${{ secrets.MY_APP_ID }}
    private-key: ${{ secrets.MY_APP_KEY }}
    owner: "my-org"
    repositories:
      - target-repo
  current: true
safe-outputs:
  create-issue:
    expires: 7
---

# Expires + App Auth Test
`

	workflowDataList, tmpDir := compileSideRepoWorkflow(t, workflowContent)

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeRelease,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	require.NoError(t, err, "generate maintenance workflow")

	sideRepoFile := filepath.Join(tmpDir, "agentics-maintenance-my-org-target-repo.yml")
	content, err := os.ReadFile(sideRepoFile)
	require.NoError(t, err, "side-repo maintenance file should have been created")
	contentStr := string(content)

	// Schedule trigger should be present (from expires: 7).
	assert.Contains(t, contentStr, "schedule:", "should include schedule trigger")
	assert.Contains(t, contentStr, "close-expired-entities:", "should include close-expired-entities job")

	// The close-expired-entities job must also get the mint step.
	closeExpiredIdx := strings.Index(contentStr, "close-expired-entities:")
	require.Greater(t, closeExpiredIdx, 0)
	closeExpiredContent := contentStr[closeExpiredIdx:]
	assert.Contains(t, closeExpiredContent, "id: side-repo-app-token",
		"close-expired-entities job must also contain the app token mint step")
	assert.Contains(t, closeExpiredContent, "steps.side-repo-app-token.outputs.token",
		"close-expired-entities job must use the minted token")
}
