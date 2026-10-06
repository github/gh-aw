package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkQueueCustomAdapterDeclarationAndPreparationIsolation(t *testing.T) {
	adapter := &WorkQueueClaimAdapter{
		Mode: "prepared", EffectType: "update_issue", TargetRepo: "owner/repo",
		FieldMap: map[string]string{"body": "content", "item_number": "number"},
	}
	data := &WorkflowData{
		Name: "Claim adapter", Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		Env: "env:\n  PRIVATE_TOKEN: ${{ secrets.WRITE_TOKEN }}\n  GITHUB_TOKEN: ${{ secrets.WRITE_TOKEN }}\n",
		SafeOutputs: &SafeOutputsConfig{
			ClaimAdapters: map[string]*WorkQueueClaimAdapter{"custom": adapter},
			Jobs: map[string]*SafeJobConfig{"custom": {Steps: []any{map[string]any{
				"run": `node prepare.cjs "$GH_AW_CLAIM_INPUT" "$GH_AW_CLAIM_OUTPUT"`,
			}}}},
		},
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	compiler := NewCompiler()
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Len(t, names, 16)
	require.Equal(t, "work_queue_prepare_custom_0", names[0])
	require.Equal(t, "work_queue_prepare_custom_15", names[15])
	job := compiler.jobManager.jobs[names[0]]
	require.Contains(t, job.Permissions, "contents: read")
	require.NotContains(t, job.Permissions, "write")
	require.Equal(t, `""`, job.Env["PRIVATE_TOKEN"])
	require.Equal(t, `""`, job.Env["GITHUB_TOKEN"])
	require.Empty(t, job.Strategy)
	steps := strings.Join(job.Steps, "")
	require.Contains(t, steps, "work_queue_prepare_claim_adapter.cjs")
	require.Contains(t, steps, "steps.claim_adapter_context.outputs.active == 'true'")
	require.Contains(t, steps, "work-queue-claim-adapter-custom-0")
	require.Contains(t, steps, `GH_AW_CLAIM_ADAPTER_INDEX: "0"`)
	require.Contains(t, steps, "/claims/")
	require.NotContains(t, steps, "claim_authorized")
	download := strings.Join(compiler.buildSafeOutputsDownloadSteps(data, ""), "")
	require.Contains(t, download, "Download isolated Claim adapter preparations")
	require.Contains(t, download, "artifact-ids: ${{ needs.work_queue_prepare_custom_0.outputs.artifact_id }}")
	require.Contains(t, download, "artifact-ids: ${{ needs.work_queue_prepare_custom_15.outputs.artifact_id }}")
	require.NotContains(t, download, "pattern: work-queue-claim-adapter-*")
	require.Contains(t, download, "/claim-adapters/custom/0/claims/")
	require.Equal(t, "${{ steps.claim_adapter_artifact.outputs.artifact-id }}", job.Outputs["artifact_id"])
	for index, name := range names {
		isolated := compiler.jobManager.jobs[name]
		require.Len(t, isolated.Outputs, 1)
		require.Empty(t, isolated.Strategy)
		require.Contains(t, strings.Join(isolated.Steps, ""), fmt.Sprintf("GH_AW_CLAIM_ADAPTER_INDEX: %q", fmt.Sprint(index)))
	}
	data.SafeOutputs.Jobs["custom"].Permissions = map[string]string{"contents": "write"}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "cannot obtain")
	data.SafeOutputs.Jobs["custom"].Permissions = nil
	data.SafeOutputs.Jobs["custom"].Env = map[string]string{"TOKEN": "${{ secrets.WRITE_TOKEN }}"}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "cannot expose write credentials")
}

func TestWorkQueueDeclaredRESTAdapterRequiresCompleteIndependentVerifier(t *testing.T) {
	adapter := &WorkQueueClaimAdapter{
		Mode: "prepared", EffectType: "github_rest", TargetRepo: "owner/repo",
		FieldMap: map[string]string{"name": "check_name", "head_sha": "revision"},
		Expected: map[string]any{"status": "completed", "conclusion": "success"},
		Request:  &WorkQueueRestRequest{Method: "POST", Route: "/repos/{owner}/{repo}/check-runs", Permission: "checks"},
		Verifier: &WorkQueueRestVerifier{
			Route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", ResourceKind: "check_run",
			Fields: map[string]string{"name": "name", "head_sha": "head_sha", "status": "status", "conclusion": "conclusion"},
		},
	}
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		SafeOutputs: &SafeOutputsConfig{
			ClaimAdapters: map[string]*WorkQueueClaimAdapter{"custom": adapter},
			Jobs:          map[string]*SafeJobConfig{"custom": {Steps: []any{map[string]any{"run": "node prepare.cjs"}}}},
		},
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	compiler := NewCompiler()
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Len(t, names, 16)
	first := strings.Join(compiler.jobManager.jobs[names[0]].Steps, "")
	require.Contains(t, first, "github_rest")
	require.Contains(t, first, "receipt_id")
	require.NotContains(t, compiler.jobManager.jobs[names[0]].Permissions, "write")
	delete(adapter.Verifier.Fields, "head_sha")
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "every REST effect field")
	adapter.Verifier.Fields["head_sha"] = "head_sha"
	adapter.FieldMap["headers"] = "credentials"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "reserved REST effect field")
	delete(adapter.FieldMap, "headers")
	adapter.Verifier.Route = "https://foreign.example/check-runs/{receipt_id}"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "repository-relative")
}

func TestWorkQueueDeclaredGitTreeAdapterAndPermissions(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"jobs": map[string]any{"code": map[string]any{"steps": []any{map[string]any{"run": "node prepare-code.cjs"}}}},
		"claim-adapters": map[string]any{"code": map[string]any{
			"mode": "prepared", "effect-type": "git_tree", "target-repo": "owner/repo",
			"field-map": map[string]any{"files": "prepared_files", "title": "title"},
			"git-tree": map[string]any{
				"base-revision": strings.Repeat("a", 40), "branch-prefix": "automation/code",
				"pull-request": true, "base-branch": "main",
			},
		}},
	}})
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: config}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Equal(t, strings.Repeat("a", 40), config.ClaimAdapters["code"].GitTree.BaseRevision)
	adapterOnly := &SafeOutputsConfig{ClaimAdapters: config.ClaimAdapters}
	permissions := computePermissionsForSafeOutputs(adapterOnly, false)
	level, _ := permissions.Get(PermissionContents)
	require.Equal(t, PermissionWrite, level)
	level, _ = permissions.Get(PermissionPullRequests)
	require.Equal(t, PermissionWrite, level)
	level, _ = permissions.Get(PermissionIssues)
	require.NotEqual(t, PermissionWrite, level)
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Len(t, names, 16)
	require.NotContains(t, compiler.jobManager.jobs[names[0]].Permissions, "write")
	require.Contains(t, strings.Join(compiler.jobManager.jobs[names[0]].Steps, ""), "git_tree")
	adapter := config.ClaimAdapters["code"]
	adapter.GitTree.BaseRevision = "main"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "immutable base revision")
	adapter.GitTree.BaseRevision = strings.Repeat("a", 40)
	adapter.GitTree.BaseBranch = ""
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "fixed base branch")
	adapter.GitTree.PullRequest = false
	delete(adapter.FieldMap, "title")
	require.NoError(t, validateWorkQueueConfiguration(data))
	permissions = computePermissionsForSafeOutputs(adapterOnly, false)
	level, _ = permissions.Get(PermissionContents)
	require.Equal(t, PermissionWrite, level)
	level, _ = permissions.Get(PermissionPullRequests)
	require.NotEqual(t, PermissionWrite, level)
	data.Tools = nil
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "require a declared work-queue worker")
	data.Tools = map[string]any{"work-queue": true}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "unassigned dispatchers")
}

func TestWorkQueueDeclaredGraphQLAdapterRequiresCompleteNativeVerifier(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"jobs": map[string]any{"discussion": map[string]any{"steps": []any{map[string]any{"run": "node prepare-discussion.cjs"}}}},
		"claim-adapters": map[string]any{"discussion": map[string]any{
			"mode": "prepared", "effect-type": "github_graphql", "target-repo": "owner/repo",
			"field-map": map[string]any{"title": "title", "body": "body", "categoryId": "category"},
			"graphql": map[string]any{
				"mutation": "createDiscussion", "input-type": "CreateDiscussionInput", "response-field": "discussion",
				"resource-type": "Discussion", "resource-kind": "discussion", "repository-input": "repositoryId",
				"repository-field": "repository.nameWithOwner", "number-field": "number", "permission": "discussions",
				"fields": map[string]any{"title": "title", "body": "body", "categoryId": "category.id"},
			},
		}},
	}})
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: config}
	require.NoError(t, validateWorkQueueConfiguration(data))
	adapter := config.ClaimAdapters["discussion"]
	require.Equal(t, "createDiscussion", adapter.GraphQL.Mutation)
	permissions := computePermissionsForSafeOutputs(&SafeOutputsConfig{ClaimAdapters: config.ClaimAdapters}, false)
	level, _ := permissions.Get(PermissionDiscussions)
	require.Equal(t, PermissionWrite, level)
	level, _ = permissions.Get(PermissionIssues)
	require.NotEqual(t, PermissionWrite, level)
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Len(t, names, 16)
	require.Contains(t, strings.Join(compiler.jobManager.jobs[names[0]].Steps, ""), "github_graphql")
	require.NotContains(t, compiler.jobManager.jobs[names[0]].Permissions, "write")
	delete(adapter.GraphQL.Fields, "body")
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "every GraphQL effect field")
	adapter.GraphQL.Fields["body"] = "body"
	for _, field := range []string{"repositoryId", "query", "headers", "__proto__", "constructor"} {
		adapter.FieldMap[field] = "value"
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "GraphQL")
		delete(adapter.FieldMap, field)
	}
	adapter.GraphQL.Mutation = "createDiscussion) { deleteRepository"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "fixed native operation")
	adapter.GraphQL.Mutation = "createDiscussion"
	adapter.GraphQL.Fields["categoryId"] = "repository"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "conflicting scalar")
}

func TestWorkQueueCustomAdapterRuntimeAndOrdinaryBehavior(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"scripts": map[string]any{"custom": map[string]any{"script": "return async input => ({success: true});"}},
		"claim-adapters": map[string]any{"custom": map[string]any{
			"mode": "script", "effect-type": "update_issue", "target-repo": "owner/repo",
			"field-map": map[string]any{"body": "content"}, "expected": map[string]any{"item_number": 42},
		}},
	}})
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: config}
	require.NoError(t, validateWorkQueueConfiguration(data))
	var runtime []string
	compiler.addHandlerManagerConfigEnvVar(&runtime, data)
	require.Contains(t, strings.Join(runtime, ""), "claim_adapters")
	require.Contains(t, strings.Join(runtime, ""), "effect-type")
	require.Equal(t, []string{"custom"}, workQueuePreparedAdapterNames(data))
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Contains(t, strings.Join(compiler.jobManager.jobs[names[0]].Steps, ""), "work_queue_prepare_claim_script.cjs")
	ordinary := &WorkflowData{SafeOutputs: &SafeOutputsConfig{Scripts: config.Scripts}}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Empty(t, workQueuePreparedAdapterNames(ordinary))
}

func TestWorkQueueActionAdapterIsPreparedAndHasNoCredentialedFallback(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"actions": map[string]any{"custom": map[string]any{
			"uses": "actions/github-script@v9.0.0",
			"inputs": map[string]any{"script": map[string]any{
				"default": "${{ 'require(\"fs\").writeFileSync(process.env.GH_AW_CLAIM_OUTPUT, JSON.stringify({prepared:true}))' }}",
			}},
		}},
		"claim-adapters": map[string]any{"custom": map[string]any{
			"mode": "prepared", "effect-type": "update_issue", "target-repo": "owner/repo",
			"field-map": map[string]any{"body": "content"}, "expected": map[string]any{"item_number": 42},
		}},
	}})
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: config}
	require.NoError(t, validateWorkQueueConfiguration(data))
	names, err := compiler.buildWorkQueuePreparedAdapterJobs(data, false)
	require.NoError(t, err)
	require.Len(t, names, 16)
	job := compiler.jobManager.jobs[names[0]]
	require.Contains(t, strings.Join(job.Steps, ""), "actions/github-script@")
	require.Contains(t, strings.Join(job.Steps, ""), "GH_AW_CLAIM_OUTPUT")
	require.NotContains(t, job.Permissions, "write")
	state := safeOutputsHandlerOutputsAndActionState{outputs: make(map[string]string)}
	compiler.appendCustomActionSteps(data, "", &state)
	require.Empty(t, state.steps)
}
