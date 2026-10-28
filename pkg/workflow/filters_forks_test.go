//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerForkFilterCompilation(t *testing.T) {
	tests := []struct {
		name     string
		forks    string
		expected string
	}{
		{"wildcard", `["*"]`, ""},
		{"empty", "[]", "github.event.pull_request.head.repo.id == github.repository_id"},
		{"exact", `"trusted/repo"`, "github.event.pull_request.head.repo.full_name == 'trusted/repo'"},
		{"patterns", "\n      - trusted/*\n      - partner/repo", "startsWith(github.event.pull_request.head.repo.full_name, 'trusted/') || github.event.pull_request.head.repo.full_name == 'partner/repo'"},
	}
	for _, event := range []string{"pull_request", "pull_request_target"} {
		for _, tt := range tests {
			t.Run(event+"/"+tt.name, func(t *testing.T) {
				names := ""
				if event == "pull_request" {
					names = "\n    names: [community-contribution]"
				}
				source := fmt.Sprintf(`---
if: ${{ github.event.repository.fork == false }}
on:
  permissions: {}
  %s:
    types: [labeled]
    forks: %s%s
    branches: [main]
  workflow_dispatch:
engine: copilot
checkout: false
permissions:
  contents: read
network:
  allowed: [defaults]
---
Review the contribution.
`, event, tt.forks, names)
				compiled := compileTriggerFilterFixture(t, source)
				on := compiled["on"].(map[string]any)
				assert.Equal(t, map[string]any{"types": []any{"labeled"}, "branches": []any{"main"}}, on[event])
				assert.Contains(t, on, "workflow_dispatch")
				jobs := compiled["jobs"].(map[string]any)
				condition := jobs["activation"].(map[string]any)["if"].(string)
				assert.Contains(t, condition, "github.event.repository.fork == false")
				if tt.expected != "" {
					assert.Contains(t, condition, "github.event_name != '"+event+"'")
					assert.Contains(t, condition, tt.expected)
				} else {
					assert.NotContains(t, condition, "github.event.pull_request.head.repo")
				}
				if names != "" {
					assert.Contains(t, condition, "github.event.label.name == 'community-contribution'")
				}
			})
		}
	}
}

func compileTriggerFilterFixture(t *testing.T, source string) map[string]any {
	t.Helper()
	workflowFile := filepath.Join(testutil.TempDir(t, "trigger-filters"), "filters.md")
	require.NoError(t, os.WriteFile(workflowFile, []byte(source), 0644))
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowFile))
	content, err := os.ReadFile(filepath.Join(filepath.Dir(workflowFile), "filters.lock.yml"))
	require.NoError(t, err)
	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	require.NoError(t, compiler.validateGitHubActionsSchemaFromParsed(compiled))
	return compiled
}

func TestApplyPullRequestForkFilter(t *testing.T) {
	const sameRepo = "github.event_name != 'pull_request' || github.event.pull_request.head.repo.id == github.repository_id"
	tests := []struct {
		name     string
		pr       map[string]any
		expected string
	}{
		{"default", map[string]any{}, sameRepo},
		{"empty patterns", map[string]any{"forks": []any{}}, sameRepo},
		{"wildcard string", map[string]any{"forks": "*"}, ""},
		{"wildcard list", map[string]any{"forks": []any{"trusted/repo", "*"}}, ""},
		{
			"repository pattern",
			map[string]any{"forks": "trusted/*"},
			sameRepo + " || startsWith(github.event.pull_request.head.repo.full_name, 'trusted/')",
		},
		{
			"trusted associations",
			map[string]any{"forks": map[string]any{"author-association": []any{"OWNER", "MEMBER", "COLLABORATOR"}}},
			sameRepo + ` || contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.pull_request.author_association)`,
		},
		{
			"member excluded",
			map[string]any{"forks": map[string]any{"author-association": []any{"OWNER", "COLLABORATOR"}}},
			sameRepo + ` || contains(fromJSON('["OWNER","COLLABORATOR"]'), github.event.pull_request.author_association)`,
		},
		{
			"empty associations",
			map[string]any{"forks": map[string]any{"author-association": []any{}}},
			sameRepo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, event := range []string{"pull_request", "pull_request_target"} {
				for _, existing := range []string{"", "github.actor != 'dependabot[bot]'"} {
					data := &WorkflowData{If: existing}
					NewCompiler().applyPullRequestForkFilter(data, map[string]any{
						"on": map[string]any{event: tt.pr, "push": nil, "workflow_dispatch": nil},
					})
					filter := strings.ReplaceAll(tt.expected, "'pull_request'", "'"+event+"'")
					if _, hasForks := tt.pr["forks"]; event == "pull_request_target" && !hasForks {
						filter = ""
					}
					expected := filter
					if existing != "" {
						expected = existing
						if filter != "" {
							expected = "(" + existing + ") && (" + filter + ")"
						}
					}
					assert.Equal(t, expected, data.If, "event: %s", event)
				}
			}
		})
	}
}

func TestPullRequestForkAuthorAssociationCompilation(t *testing.T) {
	workflowFile := filepath.Join(testutil.TempDir(t, "fork-associations"), "forks.md")
	require.NoError(t, os.WriteFile(workflowFile, []byte(`---
on:
  pull_request:
    forks:
      author-association: [OWNER, MEMBER, COLLABORATOR]
    types: [opened, synchronize]
    branches: [main]
  push:
    branches: [main]
  workflow_dispatch:
if: github.actor != 'dependabot[bot]'
engine: copilot
---
# Trusted fork authors
`), 0644))
	require.NoError(t, NewCompiler().CompileWorkflow(workflowFile))
	content, err := os.ReadFile(filepath.Join(filepath.Dir(workflowFile), "forks.lock.yml"))
	require.NoError(t, err)

	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	on := compiled["on"].(map[string]any)
	pr := on["pull_request"].(map[string]any)
	assert.NotContains(t, pr, "forks")
	assert.NotContains(t, pr, "author-association")
	assert.Equal(t, []any{"opened", "synchronize"}, pr["types"])
	assert.Equal(t, []any{"main"}, pr["branches"])
	assert.Contains(t, on, "push")
	assert.Contains(t, on, "workflow_dispatch")
	assert.Contains(t, string(content), "# author-association:")

	jobs := compiled["jobs"].(map[string]any)
	activation := jobs["activation"].(map[string]any)
	condition := activation["if"].(string)
	assert.Contains(t, condition, "github.actor != 'dependabot[bot]'")
	assert.Contains(t, condition, "github.event_name != 'pull_request' || github.event.pull_request.head.repo.id == github.repository_id")
	assert.Contains(t, condition, `|| contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.pull_request.author_association)`)
	assert.Contains(t, string(content), "github.event.pull_request.base.sha || github.sha")
}

func TestApplyPullRequestForkFilterMultipleEvents(t *testing.T) {
	data := &WorkflowData{}
	NewCompiler().applyPullRequestForkFilter(data, map[string]any{
		"on": map[string]any{
			"pull_request":        map[string]any{"forks": "pr-org/*"},
			"pull_request_target": map[string]any{"forks": "target-org/*"},
		},
	})
	assert.Equal(t,
		"(github.event_name != 'pull_request' || github.event.pull_request.head.repo.id == github.repository_id || startsWith(github.event.pull_request.head.repo.full_name, 'pr-org/')) && "+
			"(github.event_name != 'pull_request_target' || github.event.pull_request.head.repo.id == github.repository_id || startsWith(github.event.pull_request.head.repo.full_name, 'target-org/'))",
		data.If)
}
