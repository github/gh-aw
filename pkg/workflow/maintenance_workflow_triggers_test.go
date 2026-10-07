//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMaintenanceWorkflow_PushTrigger(t *testing.T) {
	const jobSectionSearchRange = 500

	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					Expires: 48,
				},
			},
		},
	}

	t.Run("dev mode includes push trigger on main for workflow md files", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			ActionTag:        "",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		yaml := string(content)

		if !strings.Contains(yaml, "  push:") {
			t.Error("Dev mode workflow should include push trigger")
		}
		if !strings.Contains(yaml, "      - main") {
			t.Error("Dev mode push trigger should target main branch (fallback when slug is empty)")
		}
		if !strings.Contains(yaml, "      - '.github/workflows/*.md'") {
			t.Error("Dev mode push trigger should target .github/workflows/*.md paths")
		}
	})

	t.Run("dev mode uses custom default branch from buildMaintenanceWorkflowYAML", func(t *testing.T) {
		// Call buildMaintenanceWorkflowYAML directly to test the branch substitution
		// without needing a live GitHub API call (FetchDefaultBranch falls back to "main" with no slug)
		yaml, err := buildMaintenanceWorkflowYAML(context.Background(), buildMaintenanceWorkflowYAMLOptions{
			cronSchedule:   "37 */2 * * *",
			scheduleDesc:   "Every 2 hours",
			minExpiresDays: 1,
			runsOnValue:    "ubuntu-slim",
			actionMode:     ActionModeDev,
			version:        "v1.0.0",
			defaultBranch:  "develop",
		})
		if err != nil {
			t.Fatalf("Expected maintenance workflow YAML: %v", err)
		}
		if !strings.Contains(yaml, "      - develop") {
			t.Errorf("Push trigger should use the provided default branch 'develop', got:\n%s", yaml[:min(500, len(yaml))])
		}
		if strings.Contains(yaml, "      - main") {
			t.Errorf("Push trigger should not contain hardcoded 'main' when 'develop' is specified")
		}
	})

	t.Run("release mode does not include push trigger", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeRelease,
			ActionTag:        "",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		yaml := string(content)

		if strings.Contains(yaml, "  push:") {
			t.Error("Release mode workflow should NOT include push trigger")
		}
	})

	t.Run("close-expired jobs and secret-validation exclude push events", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			ActionTag:        "",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		yaml := string(content)
		pushExclusionCondition := "github.event_name != 'push'"

		scheduleOnlyJobs := []string{
			"close-expired-discussions:",
			"close-expired-issues:",
			"close-expired-pull-requests:",
			"secret-validation:",
		}
		for _, job := range scheduleOnlyJobs {
			jobIdx := strings.Index(yaml, "\n  "+job)
			if jobIdx == -1 {
				t.Errorf("Job %q not found in generated workflow", job)
				continue
			}
			jobSection := yaml[jobIdx : jobIdx+jobSectionSearchRange]
			if !strings.Contains(jobSection, pushExclusionCondition) {
				t.Errorf("Job %q should exclude push events (%q) but condition is:\n%s", job, pushExclusionCondition, jobSection)
			}
		}
	})

	t.Run("compile-workflows runs on push events (no push exclusion)", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			ActionTag:        "",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		yaml := string(content)

		compileIdx := strings.Index(yaml, "\n  compile-workflows:")
		if compileIdx == -1 {
			t.Fatal("Job compile-workflows not found in generated workflow")
		}
		jobSection := yaml[compileIdx : compileIdx+jobSectionSearchRange]
		if strings.Contains(jobSection, "github.event_name != 'push'") {
			t.Errorf("Job compile-workflows should NOT exclude push events, but condition is:\n%s", jobSection)
		}
		if !strings.Contains(jobSection, "cancel-in-progress: true") {
			t.Errorf("Job compile-workflows should have cancel-in-progress concurrency, but got:\n%s", jobSection)
		}
		if !strings.Contains(jobSection, "github.workflow }}-compile-workflows-${{ github.repository") {
			t.Errorf("Job compile-workflows should have a scoped concurrency group, but got:\n%s", jobSection)
		}
		if !strings.Contains(yaml, "compile --validate --no-emit --verbose") {
			t.Errorf("Workflow should run pre-compile validation with --no-emit, but did not. Generated YAML:\n%s", yaml)
		}
		if strings.Contains(yaml, "compile --validate --validate-images --verbose") {
			t.Errorf("Workflow should not require --validate-images in compile-workflows, but generated YAML includes it:\n%s", yaml)
		}
		if strings.Contains(yaml, "        env:\n        with:\n") {
			t.Errorf("Workflow should not emit an empty env block in compile-workflows, but generated YAML includes one:\n%s", yaml)
		}
	})

	t.Run("compile-workflows can create pull requests with custom token secret", func(t *testing.T) {
		const compileJobSectionSearchRange = 650
		tmpDir := t.TempDir()
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				Compile: &MaintenanceCompileConfig{
					CreatePullRequestGitHubToken: "MAINTENANCE_TOKEN",
				},
			},
		}
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			ActionTag:        "",
			RepoConfig:       repoConfig,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		yaml := string(content)

		compileIdx := strings.Index(yaml, "\n  compile-workflows:")
		if compileIdx == -1 {
			t.Fatal("Job compile-workflows not found in generated workflow")
		}
		jobSection := yaml[compileIdx : compileIdx+compileJobSectionSearchRange]
		if !strings.Contains(jobSection, "contents: read") {
			t.Errorf("compile-workflows should keep contents: read permission, got:\n%s", jobSection)
		}
		if !strings.Contains(jobSection, "issues: write") {
			t.Errorf("compile-workflows should keep issues: write permission, got:\n%s", jobSection)
		}
		if strings.Contains(jobSection, "pull-requests: write") {
			t.Errorf("compile-workflows should not request pull-requests: write in PR mode, got:\n%s", jobSection)
		}
		if strings.Contains(jobSection, "contents: write") {
			t.Errorf("compile-workflows should not request contents: write in PR mode, got:\n%s", jobSection)
		}
		if !strings.Contains(yaml, "GH_AW_MAINTENANCE_GITHUB_TOKEN: ${{ secrets.MAINTENANCE_TOKEN }}") {
			t.Errorf("workflow should use configured maintenance github token secret, got:\n%s", yaml)
		}
		if !strings.Contains(yaml, "github-token: ${{ env.GH_AW_MAINTENANCE_GITHUB_TOKEN }}") {
			t.Errorf("workflow should pass maintenance token to github-script, got:\n%s", yaml)
		}
		if strings.Contains(yaml, "GH_AW_WORKFLOW_RECOMPILE_CREATE_PULL_REQUEST") {
			t.Errorf("workflow should not emit a separate PR mode env var, got:\n%s", yaml)
		}
	})
}

func TestGenerateMaintenanceWorkflow_ActionTag(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					Expires: 48,
				},
			},
		},
	}

	t.Run("release mode with action-tag uses remote ref", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeRelease,
			ActionTag:        "v0.47.4",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		if !strings.Contains(string(content), "github/gh-aw/actions/setup@v0.47.4") {
			t.Errorf("Expected remote ref with action-tag v0.47.4, got:\n%s", string(content))
		}
		if strings.Contains(string(content), "uses: ./actions/setup") {
			t.Errorf("Expected no local path in release mode with action-tag, got:\n%s", string(content))
		}
	})

	t.Run("release mode with action-tag and resolver uses SHA-pinned ref", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Set up an action resolver with a cached SHA for the setup action
		cache := NewActionCache(tmpDir)
		cache.Set("github/gh-aw/actions/setup", "v0.47.4", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		resolver := NewActionResolver(cache)

		workflowDataListWithResolver := []*WorkflowData{
			{
				Name:              "test-workflow",
				ActionResolver:    resolver,
				ActionPinWarnings: make(map[string]bool),
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						Expires: 48,
					},
				},
			},
		}

		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataListWithResolver,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeRelease,
			ActionTag:        "v0.47.4",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		expectedRef := "github/gh-aw/actions/setup@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa # v0.47.4"
		if !strings.Contains(string(content), expectedRef) {
			t.Errorf("Expected SHA-pinned ref %q, got:\n%s", expectedRef, string(content))
		}
		if strings.Contains(string(content), "uses: ./actions/setup") {
			t.Errorf("Expected no local path in release mode with action-tag, got:\n%s", string(content))
		}
	})

	t.Run("dev mode ignores action-tag and uses local path", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: workflowDataList,
			WorkflowDir:      tmpDir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			ActionTag:        "v0.47.4",
			RepoConfig:       nil,
			RepoSlug:         "",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
		if err != nil {
			t.Fatalf("Expected maintenance workflow to be generated: %v", err)
		}
		if !strings.Contains(string(content), "uses: ./actions/setup") {
			t.Errorf("Expected local path in dev mode, got:\n%s", string(content))
		}
	})
}
