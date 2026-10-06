package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkQueuePolicySchema(t *testing.T) {
	for _, tool := range []any{true, nil, map[string]any{"worker": true}, map[string]any{"storage": "git"}} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"work-queue": tool},
		}, "worker.md"))
	}
	for _, tool := range []any{false, map[string]any{"storage": "issues"}, map[string]any{"scheduler": false}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"work-queue": tool},
		}, "worker.md"))
	}
	for _, policy := range []any{false, map[string]any{"mode": "disabled"}, map[string]any{"class-weights": []any{1}}, map[string]any{"outstanding": map[string]any{"claims": 0}}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "work-queue-policy": policy,
		}, "worker.md"))
	}
	require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on": "workflow_dispatch", "work-queue-policy": map[string]any{
			"mode": "weighted-priority", "class-weights": []any{8, 4, 2, 1, 1},
			"accounting-weights": map[string]any{"project-a": 2},
			"outstanding":        map[string]any{"claims": 8, "dispatches": 4},
		},
	}, "worker.md"))
}

func TestWorkQueueClaimAdapterSchema(t *testing.T) {
	validate := func(adapter map[string]any) error {
		return ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "safe-outputs": map[string]any{
				"claim-adapters": map[string]any{"custom": adapter},
			},
		}, "worker.md")
	}
	valid := map[string]any{"mode": "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": map[string]any{"body": "content"}}
	require.NoError(t, validate(valid))
	for _, mutation := range []map[string]any{
		{"mode": "job-success"}, {"effect-type": "unverified"}, {"target-repo": "${{ inputs.repo }}"},
		{"field-map": map[string]any{"claim_handle": "content"}}, {"field-map": map[string]any{"body": "a.b"}},
		{"expected": map[string]any{"claim_handle": "foreign"}}, {"authorized": true},
	} {
		candidate := make(map[string]any, len(valid)+1)
		for key, value := range valid {
			candidate[key] = value
		}
		for key, value := range mutation {
			candidate[key] = value
		}
		require.Error(t, validate(candidate))
	}
	rest := map[string]any{
		"mode": "prepared", "effect-type": "github_rest", "target-repo": "owner/repo",
		"field-map": map[string]any{"name": "check_name"},
		"request":   map[string]any{"method": "POST", "route": "/repos/{owner}/{repo}/check-runs", "permission": "checks"},
		"verifier":  map[string]any{"route": "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", "fields": map[string]any{"name": "name"}},
	}
	require.NoError(t, validate(rest))
	for _, field := range []string{"request", "verifier"} {
		candidate := make(map[string]any)
		for key, value := range rest {
			if key != field {
				candidate[key] = value
			}
		}
		require.Error(t, validate(candidate))
	}
	code := map[string]any{
		"mode": "script", "effect-type": "git_tree", "target-repo": "owner/repo",
		"field-map": map[string]any{"files": "prepared_files", "title": "title"},
		"git-tree":  map[string]any{"base-revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "branch-prefix": "automation/code", "pull-request": true, "base-branch": "main"},
	}
	require.NoError(t, validate(code))
	code["git-tree"] = map[string]any{"base-revision": "main", "branch-prefix": "automation/code"}
	require.Error(t, validate(code))
	code["git-tree"] = map[string]any{"base-revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "branch-prefix": "automation/code", "pull-request": true}
	require.Error(t, validate(code))
	graph := map[string]any{
		"mode": "prepared", "effect-type": "github_graphql", "target-repo": "owner/repo",
		"field-map": map[string]any{"title": "title", "body": "body", "categoryId": "category"},
		"graphql": map[string]any{
			"mutation": "createDiscussion", "input-type": "CreateDiscussionInput", "response-field": "discussion",
			"resource-type": "Discussion", "resource-kind": "discussion", "repository-input": "repositoryId",
			"repository-field": "repository.nameWithOwner", "number-field": "number", "permission": "discussions",
			"fields": map[string]any{"title": "title", "body": "body", "categoryId": "category.id"},
		},
	}
	require.NoError(t, validate(graph))
	definition := graph["graphql"].(map[string]any)
	for _, field := range []string{"mutation", "input-type", "response-field", "resource-type", "resource-kind", "repository-input", "repository-field", "permission", "fields"} {
		original := definition[field]
		delete(definition, field)
		require.Error(t, validate(graph), field)
		definition[field] = original
	}
	definition["mutation"] = "createDiscussion) { deleteRepository"
	require.Error(t, validate(graph))
}
