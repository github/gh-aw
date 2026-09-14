//go:build !integration

package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// ========================================
// Repo Memory and Cache Memory Job Tests
// ========================================

// TestJobsWithRepoMemoryDependencies tests push_repo_memory job positioning
// This tests the job creation logic when repo-memory config is present
func TestJobsWithRepoMemoryDependencies(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	// Create workflow data with repo-memory config
	data := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:         "test-memory",
					BranchName: "memory-branch",
					FileGlob:   []string{"data/**"},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[bot] ",
			},
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	// Build activation and agent jobs first
	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	// Build safe outputs jobs (creates detection job when threat detection is enabled)
	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	// Build push_repo_memory job
	threatDetectionEnabledForSafeJobs := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
	pushRepoMemoryJob, err := compiler.buildPushRepoMemoryJob(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		t.Fatalf("buildPushRepoMemoryJob() error: %v", err)
	}

	// Verify job was created
	if pushRepoMemoryJob == nil {
		t.Fatal("Expected push_repo_memory job to be created")
	}

	// Detection is a separate job — push_repo_memory should depend on it when enabled
	if threatDetectionEnabledForSafeJobs {
		hasDetectionDep := slices.Contains(pushRepoMemoryJob.Needs, string(constants.DetectionJobName))
		if !hasDetectionDep {
			t.Error("push_repo_memory should depend on detection job (detection is now a separate job)")
		}
	}
	if !slices.Contains(pushRepoMemoryJob.Needs, string(constants.SafeOutputsJobName)) {
		t.Error("push_repo_memory should depend on safe_outputs so temporary ID mappings are finalized first")
	}
	pushSteps := strings.Join(pushRepoMemoryJob.Steps, "\n")
	if !strings.Contains(pushSteps, "Download safe-output temporary ID map") {
		t.Error("push_repo_memory should download the temporary ID map artifact from safe_outputs")
	}
	if !strings.Contains(pushSteps, "GH_AW_TEMPORARY_ID_MAP_FILE: /tmp/gh-aw/safe-outputs-items/temporary-id-map.json") {
		t.Error("push_repo_memory should receive the temporary ID map artifact path")
	}

	// Verify job name
	if pushRepoMemoryJob.Name != "push_repo_memory" {
		t.Errorf("Expected job name 'push_repo_memory', got %q", pushRepoMemoryJob.Name)
	}
}

func TestJobsWithRepoMemoryWithoutConsolidatedSafeOutputs(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{{ID: "test-memory", BranchName: "memory-branch"}},
		},
		SafeOutputs: &SafeOutputsConfig{
			UploadAssets: &UploadAssetsConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)
	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)
	if err := compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md"); err != nil {
		t.Fatalf("buildSafeOutputsJobs() error: %v", err)
	}

	pushRepoMemoryJob, err := compiler.buildPushRepoMemoryJob(data, false)
	if err != nil {
		t.Fatalf("buildPushRepoMemoryJob() error: %v", err)
	}
	if slices.Contains(pushRepoMemoryJob.Needs, string(constants.SafeOutputsJobName)) {
		t.Error("push_repo_memory should not depend on a consolidated safe_outputs job that was not created")
	}
	if strings.Contains(strings.Join(pushRepoMemoryJob.Steps, "\n"), "GH_AW_TEMPORARY_ID_MAP") {
		t.Error("push_repo_memory should not reference a temporary ID map from a consolidated safe_outputs job that was not created")
	}
}

// TestJobsWithCacheMemoryDependencies tests update_cache_memory job positioning
// This tests the job creation logic when cache-memory config is present
func TestJobsWithCacheMemoryDependencies(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	// Create workflow data with cache-memory config
	data := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{
					ID:  "test-cache",
					Key: "test-key",
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[bot] ",
			},
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	// Build activation and agent jobs first
	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	// Build safe outputs jobs (creates detection job when threat detection is enabled)
	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	// Build update_cache_memory job (only created with threat detection)
	threatDetectionEnabledForSafeJobs := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
	if threatDetectionEnabledForSafeJobs {
		updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, threatDetectionEnabledForSafeJobs)
		if err != nil {
			t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
		}

		// Verify job was created
		if updateCacheMemoryJob == nil {
			t.Fatal("Expected update_cache_memory job to be created when threat detection is enabled")
		}

		// Verify dependencies — detection is a separate job, so should depend on it
		hasDetectionDep := slices.Contains(updateCacheMemoryJob.Needs, string(constants.DetectionJobName))
		if !hasDetectionDep {
			t.Error("update_cache_memory should depend on detection job (detection is now a separate job)")
		}
		// Should depend on agent job
		hasAgentDep := slices.Contains(updateCacheMemoryJob.Needs, string(constants.AgentJobName))
		if !hasAgentDep {
			t.Error("Expected update_cache_memory to depend on agent job")
		}

		// Verify job name
		if updateCacheMemoryJob.Name != "update_cache_memory" {
			t.Errorf("Expected job name 'update_cache_memory', got %q", updateCacheMemoryJob.Name)
		}
	}
}

// TestUpdateCacheMemoryJobHasWorkflowIDEnv verifies that the update_cache_memory job
// includes GH_AW_WORKFLOW_ID_SANITIZED in its env block so cache keys match the agent job.
func TestUpdateCacheMemoryJobHasWorkflowIDEnv(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:       "Test Workflow",
		WorkflowID: "daily-repo-status",
		AI:         "copilot",
		RunsOn:     "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, true)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
	}
	if updateCacheMemoryJob == nil {
		t.Fatal("Expected update_cache_memory job to be created")
	}

	// GH_AW_WORKFLOW_ID_SANITIZED must be present so the save key matches the restore key
	sanitizedID, ok := updateCacheMemoryJob.Env["GH_AW_WORKFLOW_ID_SANITIZED"]
	if !ok {
		t.Error("update_cache_memory job is missing GH_AW_WORKFLOW_ID_SANITIZED env var; cache keys will not match")
	}
	// "daily-repo-status" -> lowercase + hyphens removed -> "dailyrepostatus"
	if sanitizedID != "dailyrepostatus" {
		t.Errorf("GH_AW_WORKFLOW_ID_SANITIZED = %q, want %q", sanitizedID, "dailyrepostatus")
	}
}

// TestUpdateCacheMemoryJobConditionRequiresAgentSuccess verifies that the update_cache_memory
// job condition requires the agent job to have succeeded (not just not be skipped).
// This ensures cache is not updated when the agent job fails.
func TestUpdateCacheMemoryJobConditionRequiresAgentSuccess(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, true)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
	}
	if updateCacheMemoryJob == nil {
		t.Fatal("Expected update_cache_memory job to be created")
	}

	// The job condition must require the agent to have succeeded.
	// It must NOT use != 'skipped' (which allows the job to run when agent fails).
	if !strings.Contains(updateCacheMemoryJob.If, "needs.agent.result == 'success'") {
		t.Errorf("update_cache_memory job condition should require agent == 'success' to prevent running when agent fails, got: %q", updateCacheMemoryJob.If)
	}
}

// TestUpdateCacheMemoryJobHasActionsWritePermission verifies that the update_cache_memory job
// has actions: write permission so GitHub's cache-reservation backend allows cache saves.
// Without this permission, cache saves fail with "token has no writable scopes".
func TestUpdateCacheMemoryJobHasActionsWritePermission(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, true)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
	}
	if updateCacheMemoryJob == nil {
		t.Fatal("Expected update_cache_memory job to be created")
	}

	// Must have actions: write so cache saves are not rejected with "token has no writable scopes".
	if !strings.Contains(updateCacheMemoryJob.Permissions, "actions: write") {
		t.Errorf("update_cache_memory job must have 'actions: write' permission for cache saves, got: %q", updateCacheMemoryJob.Permissions)
	}
}

func TestUpdateCacheMemoryJobHasActionsWritePermissionInDevMode(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, true)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
	}
	if updateCacheMemoryJob == nil {
		t.Fatal("Expected update_cache_memory job to be created")
	}

	if !strings.Contains(updateCacheMemoryJob.Permissions, "actions: write") {
		t.Errorf("dev-mode update_cache_memory job must preserve 'actions: write', got: %q", updateCacheMemoryJob.Permissions)
	}
	if !strings.Contains(updateCacheMemoryJob.Permissions, "contents: read") {
		t.Errorf("dev-mode update_cache_memory job must include 'contents: read', got: %q", updateCacheMemoryJob.Permissions)
	}
}

func TestUpdateCacheMemoryJobSkippedForRestoreOnlyCaches(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default", RestoreOnly: true},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, true)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
	}
	if updateCacheMemoryJob != nil {
		t.Fatalf("Expected update_cache_memory job to be omitted for restore-only caches, got permissions: %q", updateCacheMemoryJob.Permissions)
	}
}
