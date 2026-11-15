package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
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

func TestWriteGatePreservesOriginalDependencySuccess(t *testing.T) {
	for _, test := range []struct {
		name         string
		condition    string
		implicit     bool
		explicitMode string
	}{
		{"empty", "", true, ""},
		{"simple", "vars.PUBLISH == 'true'", true, ""},
		{"wrapped simple", "${{ vars.PUBLISH == 'true' }}", true, ""},
		{"quoted status text", "vars.MODE == 'always()'", true, ""},
		{"always", "always()", false, "always"},
		{"case insensitive always", "ALWAYS ()", false, "always"},
		{"failure", "failure()", false, "failure"},
		{"success", "success()", false, "success"},
		{"not cancelled", "!cancelled()", false, "always"},
		{"cancelled", "cancelled()", false, "cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "safe_outputs"}))
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "extra"}))
			writer := &Job{Name: "publish_result", If: test.condition, Needs: []string{"extra"}}
			require.NoError(t, compiler.jobManager.AddJob(writer))
			require.NoError(t, compiler.guardPublicationWriteGate(writeGateTestData()))
			require.NotContains(t, writer.If, "${{", "a wrapped original condition must not become an embedded interpolation")
			if test.implicit {
				require.Contains(t, writer.If, "success()", "the added status check must not remove implicit dependency success")
			}
			for _, result := range []string{"success", "failure", "cancelled", "skipped"} {
				replacements := []string{
					"!cancelled()", "('true' == 'true')",
					"cancelled()", "('true' == 'false')",
					"success()", "needs.extra.result == 'success'",
					"failure()", "needs.extra.result == 'failure'",
				}
				if test.explicitMode == "always" {
					replacements = append(replacements, "always()", "('true' == 'true')", "ALWAYS ()", "('true' == 'true')")
				}
				condition := strings.NewReplacer(replacements...).Replace(writer.If)
				for _, verdict := range []string{"allow", "partial", "deny", ""} {
					want := verdict == "allow" || verdict == "partial"
					switch test.explicitMode {
					case "always":
					case "failure":
						want = want && result == "failure"
					case "cancelled":
						want = false
					default:
						want = want && result == "success"
					}
					require.Equal(t, want, evaluateTestCondition(t, condition, map[string]string{
						"needs.extra.result": result, "needs.safe_outputs.result": "success",
						"needs.safe_outputs.outputs.write_gate": verdict,
						"vars.PUBLISH":                          "true", "vars.MODE": "always()",
					}), "extra=%s verdict=%s", result, verdict)
				}
				if strings.Contains(test.condition, "vars.") {
					require.False(t, evaluateTestCondition(t, condition, map[string]string{
						"needs.extra.result": result, "needs.safe_outputs.result": "success",
						"needs.safe_outputs.outputs.write_gate": "allow",
						"vars.PUBLISH":                          "false", "vars.MODE": "never()",
					}), "the original false predicate must still block publication")
				}
			}
		})
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
	require.False(t, hasWritePublicationPath(&WorkflowData{SafeOutputs: &SafeOutputsConfig{
		NoOp: &NoOpConfig{}, MissingTool: &MissingToolConfig{}, MissingData: &MissingDataConfig{}, ReportIncomplete: &ReportIncompleteConfig{},
	}}), "conclusion-owned diagnostics do not create a gated publication path")
	require.True(t, hasWritePublicationPath(&WorkflowData{SafeOutputs: &SafeOutputsConfig{
		Steps: []any{map[string]any{"run": "echo publish"}},
	}}), "authored safe-output publication steps require the gate")
}

func TestWriteGateCompiledPublicationGraph(t *testing.T) {
	for _, detection := range []string{"false", "true", "{ continue-on-error: false }"} {
		t.Run(detection, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(directory, ".github", "workflows"), 0700))
			createWorker(t, filepath.Join(directory, ".github", "workflows"), "worker-one", "")
			source := fmt.Sprintf(`---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
  copilot-requests: none
tools:
  repo-memory:
    branch-name: memory/test
  cache-memory:
    key: memory-test
  drive-memory:
    drive-name: memory-test
jobs:
  safe_outputs:
    gate-steps:
      - run: echo gate
safe-outputs:
  require-agent-success: true
  threat-detection: %s
  add-comment:
  upload-asset:
  create-code-scanning-alert:
  upload-code-coverage:
  call-workflow: [worker-one]
  jobs:
    publish-result:
      if: always() || failure()
      runs-on: ubuntu-latest
      steps:
        - run: echo publish
---
Publish only after the gate.
`, detection)
			compiler := NewCompiler()
			path := filepath.Join(directory, ".github", "workflows", "workflow.md")
			data, err := compiler.ParseWorkflowString(source, path)
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			var compiled map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
			jobs := compiled["jobs"].(map[string]any)
			for _, name := range []string{"publish_result", "push_repo_memory", "update_cache_memory", "update_drive_memory", "upload_assets", "upload_code_scanning_sarif", "upload_code_coverage", "call-worker-one"} {
				job, exists := jobs[name].(map[string]any)
				require.True(t, exists, name)
				require.Contains(t, parseNeedsField(job["needs"]), "safe_outputs", name)
				condition := job["if"].(string)
				require.Contains(t, condition, "needs.safe_outputs.result == 'success'", name)
				require.Contains(t, condition, "needs.safe_outputs.outputs.write_gate == 'allow'", name)
				require.Contains(t, condition, "needs.safe_outputs.outputs.write_gate == 'partial'", name)
			}
			if detection == "false" {
				require.Equal(t, "read", jobs["agent"].(map[string]any)["cache-mode"])
				require.NotContains(t, parseNeedsField(jobs["update_cache_memory"].(map[string]any)["needs"]), "detection")
			}
			conclusion := jobs["conclusion"].(map[string]any)
			steps, err := yaml.Marshal(conclusion["steps"])
			require.NoError(t, err)
			require.Contains(t, string(steps), "GH_AW_WRITE_GATE_WORKFLOW_SHA")
			require.Contains(t, string(steps), "github.workflow_sha")
			require.Contains(t, string(steps), "Upload write gate verdict")
			require.Contains(t, output, `"write_gate":{"version":1,"record_job":"conclusion","artifact_immutable":true`)
		})
	}
}

func TestWriteGateReplayConfigPreservesOriginalRestrictions(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(`---
on: workflow_dispatch
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    target-repo: owner/restricted
    allowed-repos: [owner/restricted]
    staged: true
    github-token: ${{ secrets.ORIGINAL_TOKEN }}
  merge-pull-request:
    allowed-base-branches: [agent-work/*]
    protected-files:
      exclude: [AGENTS.md]
---
Restricted publication.
`, "workflow.md")
	require.NoError(t, err)
	config, err := compiler.buildWriteGateReplayConfig(data)
	require.NoError(t, err)
	require.NotNil(t, config)
	issue := config["create_issue"].(map[string]any)
	require.Equal(t, float64(1), issue["max"])
	require.Equal(t, "owner/restricted", issue["target-repo"])
	require.Equal(t, []any{"owner/restricted"}, issue["allowed_repos"])
	require.Equal(t, true, issue["staged"])
	require.NotContains(t, issue, "github-token")
	require.Equal(t, "${{ secrets.ORIGINAL_TOKEN }}", data.SafeOutputs.CreateIssues.GitHubToken)
	merge := config["merge_pull_request"].(map[string]any)
	require.NotEmpty(t, merge["protected_files"])
	require.NotContains(t, merge["protected_files"], "AGENTS.md")
	original := compiler.buildHandlerManagerConfig(data)
	delete(original["create_issue"].(map[string]any), "github-token")
	before, err := json.Marshal(original)
	require.NoError(t, err)
	after, err := json.Marshal(config)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

func TestWriteGateReplayConfigRefusesUnresolvedRestrictions(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(`---
on: workflow_dispatch
engine: copilot
safe-outputs:
  create-issue:
    target-repo: ${{ vars.ORIGINAL_DESTINATION }}
    max: ${{ vars.ORIGINAL_LIMIT }}
---
Runtime-resolved publication.
`, "workflow.md")
	require.NoError(t, err)
	config, err := compiler.buildWriteGateReplayConfig(data)
	require.NoError(t, err)
	require.Nil(t, config, "today's variables must never replace the original resolved policy")
}

func TestWriteGateStaticReplayRefusesImplicitContext(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}}
	require.False(t, isStaticWriteGateReplayType("add_comment", data))
	require.False(t, isStaticWriteGateReplayType("merge_pull_request", data))
	require.False(t, isStaticWriteGateReplayType("create_issue", data), "default mention authors depend on the original context")
	no := false
	data.SafeOutputs.Mentions = &MentionsConfig{Enabled: &no}
	require.True(t, isStaticWriteGateReplayType("create_issue", data))
	data.SafeOutputs.CreateIssues.Group = strPtr("true")
	require.False(t, isStaticWriteGateReplayType("create_issue", data), "grouping needs the original resolved target")
	require.False(t, isStaticWriteGateReplayType("call_workflow", data))
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: data.SafeOutputs}
	config, err := NewCompiler().buildWriteGateReplayConfig(worker)
	require.NoError(t, err)
	require.Nil(t, config, "claim-constrained writes require the original runtime scope snapshot")
}
