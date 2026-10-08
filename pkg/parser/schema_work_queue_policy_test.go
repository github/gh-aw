package parser

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkQueuePolicySchema(t *testing.T) {
	for _, tool := range []any{true, nil, map[string]any{}, map[string]any{"worker": true}} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"work-queue": tool},
		}, "worker.md"))
	}

	for _, tool := range []any{false, map[string]any{"storage": "git"}, map[string]any{"storage": "issues"}, map[string]any{"storage": nil}, map[string]any{"scheduler": false}} {
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

func TestWorkQueueMemorySchema(t *testing.T) {
	memory := map[string]any{"path": "memory.json", "target-repo": "owner/repo", "base-revision": strings.Repeat("a", 40), "branch-prefix": "memory/runs", "schema": map[string]any{"type": "object"}}
	validate := func(config map[string]any) error {
		return ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"work-queue": map[string]any{"worker": true, "memory": config}},
		}, "worker.md")
	}
	require.NoError(t, validate(memory))
	for _, field := range []string{"path", "target-repo", "base-revision", "branch-prefix", "schema"} {
		invalid := maps.Clone(memory)
		delete(invalid, field)
		require.Error(t, validate(invalid))
	}
	for field, value := range map[string]any{"unknown": true, "max-bytes": 262145, "base-revision": "main", "name": "../tool", "schema": "object"} {
		invalid := maps.Clone(memory)
		invalid[field] = value
		require.Error(t, validate(invalid))
	}
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
	for _, test := range []struct {
		name     string
		mutation map[string]any
	}{
		{"unverified-mode", map[string]any{"mode": "job-success"}},
		{"unverified-effect", map[string]any{"effect-type": "unverified"}},
		{"dynamic-repository", map[string]any{"target-repo": "${{ inputs.repo }}"}},
		{"mapped-claim-selector", map[string]any{"field-map": map[string]any{"claim_handle": "content"}}},
		{"nested-source-field", map[string]any{"field-map": map[string]any{"body": "a.b"}}},
		{"fixed-claim-selector", map[string]any{"expected": map[string]any{"claim_handle": "foreign"}}},
		{"agent-authorization", map[string]any{"authorized": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := make(map[string]any, len(valid)+1)
			maps.Copy(candidate, valid)
			maps.Copy(candidate, test.mutation)
			require.Error(t, validate(candidate))
		})
	}
	rest := map[string]any{
		"mode": "prepared", "effect-type": "github_rest", "target-repo": "owner/repo",
		"field-map": map[string]any{"name": "check_name"},
		"request":   map[string]any{"method": "POST", "route": "/repos/{owner}/{repo}/check-runs", "permission": "checks"},
		"verifier":  map[string]any{"route": "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", "fields": map[string]any{"name": "name"}},
	}
	require.NoError(t, validate(rest))
	for _, field := range []string{"field-map", "expected"} {
		for _, reserved := range []string{"claim_handle", "claim_id", "work_id", "dispatch_id", "receipt_id", "__proto__", "constructor", "prototype"} {
			t.Run("native-"+field+"-"+reserved, func(t *testing.T) {
				candidate := make(map[string]any, len(rest))
				maps.Copy(candidate, rest)
				candidate[field] = map[string]any{reserved: "content"}
				require.Error(t, validate(candidate))
			})
		}
	}
	for _, id := range []string{"CheckCreated.v1", "v", strings.Repeat("a", 128)} {
		rest["verifier-id"] = id
		require.NoError(t, validate(rest))
	}
	for _, id := range []any{"", "_invalid", "read user", "${{ inputs.verifier }}", strings.Repeat("a", 129), nil, 1} {
		rest["verifier-id"] = id
		require.Error(t, validate(rest))
	}
	delete(rest, "verifier-id")
	valid["verifier-id"] = "IssueUpdated.v1"
	require.NoError(t, validate(valid))
	delete(valid, "verifier-id")
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
