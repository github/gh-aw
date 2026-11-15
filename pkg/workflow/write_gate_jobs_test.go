package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func writeGateTestData() *WorkflowData {
	return &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Jobs:         map[string]*SafeJobConfig{"publish-result": {}},
			CallWorkflow: &CallWorkflowConfig{Workflows: []string{"worker_one"}},
		},
		Jobs: map[string]any{"safe_outputs": map[string]any{"gate-steps": []any{map[string]any{"run": "echo gate"}}}},
	}
}

func TestWriteGatePublicationRegistry(t *testing.T) {
	publication := publicationJobNames(writeGateTestData().SafeOutputs)
	// Every compiler-owned job must be consciously classified; new writers
	// cannot silently inherit an exemption by simply missing from the registry.
	exempt := map[string]bool{
		"agent": true, "admission": true, "activation": true,
		"pre_activation": true, "pre-activation": true,
		"detection": true, "evals": true, "conclusion": true,
		"conclusion_admission": true, "graders": true, "unlock": true,
		"push_experiments_state": true,
		"safe-outputs":           true, // Compatibility alias, never an emitted write job.
	}
	for name := range constants.KnownBuiltInJobNames {
		_, registered := publication[name]
		require.NotEqual(t, registered, exempt[name], "classify %s as publication or explicitly exempt", name)
	}
	for name := range constants.MaintenanceJobDependencies {
		if constants.MaintenanceJobDependencies[name] == constants.ActivationJobName {
			require.NotContains(t, publication, name, "activation-owned state is not agent publication")
			continue
		}
		require.Contains(t, publication, name, "maintenance writer %s must follow the verdict", name)
	}
	require.Contains(t, publication, "publish_result")
	require.Contains(t, publication, "call-worker-one")
}

func TestWriteGateGuardsAllPublicationJobs(t *testing.T) {
	for _, requireAgentSuccess := range []bool{false, true} {
		data := writeGateTestData()
		data.SafeOutputs.RequireAgentSuccess = requireAgentSuccess
		compiler := NewCompiler()
		compiler.jobManager = NewJobManager()
		for _, name := range []string{"agent", "activation", "conclusion", "detection", "push_experiments_state"} {
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, If: "always()"}))
		}
		for name := range publicationJobNames(data.SafeOutputs) {
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, If: "always() || failure()"}))
		}
		require.NoError(t, compiler.guardPublicationWriteGate(data))
		for name := range publicationJobNames(data.SafeOutputs) {
			job, _ := compiler.jobManager.GetJob(name)
			if name == "safe_outputs" {
				require.NotContains(t, job.Needs, "safe_outputs")
				continue
			}
			require.True(t, slices.Contains(job.Needs, "safe_outputs"), name)
			condition := strings.NewReplacer(
				"!cancelled()", "('true' == 'true')",
				"always()", "('true' == 'true')",
				"failure()", "('true' == 'false')",
			).Replace(job.If)
			for _, result := range []string{"success", "failure", "cancelled", "skipped", "timed_out", ""} {
				for _, verdict := range []string{"allow", "partial", "deny", "unknown", ""} {
					for _, agentResult := range []string{"success", "failure"} {
						want := result == "success" && (verdict == "allow" || verdict == "partial") && (!requireAgentSuccess || agentResult == "success")
						require.Equal(t, want, evaluateTestCondition(t, condition, map[string]string{
							"needs.safe_outputs.result": result, "needs.safe_outputs.outputs.write_gate": verdict,
							"needs.agent.result": agentResult,
						}), "%s result=%s verdict=%s agent=%s", name, result, verdict, agentResult)
					}
				}
			}
		}
		for _, name := range []string{"agent", "activation", "conclusion", "detection", "push_experiments_state"} {
			job, _ := compiler.jobManager.GetJob(name)
			require.Equal(t, "always()", job.If, name)
		}
	}
}

func TestWriteGateRejectsPublicationDependencyCycle(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "safe_outputs", Needs: []string{"push_repo_memory"}}))
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "push_repo_memory"}))
	require.ErrorContains(t, compiler.guardPublicationWriteGate(writeGateTestData()), "cycle detected")
}

func TestWriteGateDoesNotChangeUngatedJobs(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	for name := range publicationJobNames(nil) {
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, If: "always()", Needs: []string{"agent"}}))
	}
	var before strings.Builder
	compiler.jobManager.WriteJobsYAML(&before)
	require.NoError(t, compiler.guardPublicationWriteGate(&WorkflowData{}))
	var after strings.Builder
	compiler.jobManager.WriteJobsYAML(&after)
	require.Equal(t, before.String(), after.String())
}

func TestWriteGateDefersMemoryWithoutDetection(t *testing.T) {
	for _, gated := range []bool{false, true} {
		data := writeGateTestData()
		if !gated {
			data.Jobs = nil
		}
		data.CacheMemoryConfig = &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}}
		data.DriveMemoryConfig = &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "default", DriveName: "memory"}}}
		var cacheSteps, cacheUploads, driveSteps, drivePersistence strings.Builder
		generateCacheMemorySteps(&cacheSteps, data)
		generateCacheMemoryArtifactUpload(&cacheUploads, data, getActionPin)
		generateDriveMemorySteps(&driveSteps, data, getActionPin)
		generateDriveMemoryPersistence(&drivePersistence, data, getActionPin)
		compiler := NewCompiler()
		cacheJob, err := compiler.buildUpdateCacheMemoryJob(data, false)
		require.NoError(t, err)
		driveJob, err := compiler.buildUpdateDriveMemoryJob(data, false)
		require.NoError(t, err)
		if gated {
			require.Contains(t, cacheSteps.String(), getActionPin("actions/cache/restore"))
			require.NotContains(t, cacheSteps.String(), "uses: "+getActionPin("actions/cache")+"\n")
			require.Contains(t, cacheUploads.String(), "Upload cache-memory")
			require.Contains(t, driveSteps.String(), "write: false")
			require.Contains(t, driveSteps.String(), "Capture drive-memory baseline")
			require.NotContains(t, drivePersistence.String(), getActionPin("actions/gh-drives-preview/commit"))
			require.Contains(t, drivePersistence.String(), "Upload drive-memory data")
			require.NotNil(t, cacheJob)
			require.NotNil(t, driveJob)
			require.NotContains(t, cacheJob.Needs, "detection")
			require.NotContains(t, driveJob.Needs, "detection")
			require.Contains(t, strings.Join(cacheJob.Steps, ""), "Save cache-memory to cache")
			require.Contains(t, strings.Join(driveJob.Steps, ""), "Commit drive-memory")
		} else {
			require.Contains(t, cacheSteps.String(), "uses: "+getActionPin("actions/cache")+"\n")
			require.Empty(t, cacheUploads.String())
			require.Contains(t, driveSteps.String(), "write: true")
			require.Contains(t, drivePersistence.String(), getActionPin("actions/gh-drives-preview/commit"))
			require.Nil(t, cacheJob)
			require.Nil(t, driveJob)
		}
	}
}

func TestWriteGatePublicationPathIncludesMemoryOnly(t *testing.T) {
	for _, data := range []*WorkflowData{
		{RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "default"}}}},
		{CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}}},
		{DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "default"}}}},
		{SafeOutputs: &SafeOutputsConfig{CallWorkflow: &CallWorkflowConfig{Workflows: []string{"worker"}}}},
	} {
		require.True(t, hasWritePublicationPath(data))
	}
	require.False(t, hasWritePublicationPath(&WorkflowData{ExperimentsStorage: ExperimentsStorageRepo, Experiments: map[string][]string{"model": {"a", "b"}}}))
	require.False(t, hasWritePublicationPath(&WorkflowData{CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default", RestoreOnly: true}}}}))
}
