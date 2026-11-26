//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetIssueStatusConfig(t *testing.T) {
	compiler := NewCompiler()
	assert.Nil(t, compiler.parseSetIssueStatusConfig(map[string]any{}))
	config := compiler.parseSetIssueStatusConfig(map[string]any{"set-issue-status": nil})
	require.NotNil(t, config)
	assert.False(t, config.AllowClose)
	assert.False(t, config.AllowReopen)

	config = compiler.parseSetIssueStatusConfig(map[string]any{"set-issue-status": map[string]any{
		"allowed": []any{"In progress", 12}, "allow-close": true, "allow-reopen": true,
		"target": "*", "target-repo": "owner/repo", "allowed-repos": []any{"other/repo"},
		"required-labels": []any{"triaged"}, "required-title-prefix": "[work]", "max": 7,
	}})
	require.NotNil(t, config)
	assert.Equal(t, []any{"In progress", uint64(12)}, config.Allowed)
	assert.True(t, config.AllowClose)
	assert.True(t, config.AllowReopen)
	assert.Equal(t, "*", config.Target)
	assert.Equal(t, "owner/repo", config.TargetRepoSlug)
	assert.Equal(t, []string{"other/repo"}, config.AllowedRepos)
	assert.Equal(t, []string{"triaged"}, config.RequiredLabels)
	assert.Equal(t, "[work]", config.RequiredTitlePrefix)
	assert.Equal(t, "7", *config.Max)

	built := issueHandlerRegistry["set_issue_status"](&SafeOutputsConfig{SetIssueStatus: config})
	assert.Equal(t, true, built["allow_close"])
	assert.Equal(t, true, built["allow_reopen"])
	assert.Equal(t, config.Allowed, built["allowed"])
	assert.Equal(t, config.RequiredLabels, built["required_labels"])
	assert.Equal(t, config.AllowedRepos, built["allowed_repos"])
	assert.Equal(t, "owner/repo", built["target-repo"])
	assert.Nil(t, issueHandlerRegistry["set_issue_status"](&SafeOutputsConfig{}))
	defaults := issueHandlerRegistry["set_issue_status"](&SafeOutputsConfig{SetIssueStatus: &SetIssueStatusConfig{}})
	assert.Equal(t, false, defaults["allow_close"])
	assert.Equal(t, false, defaults["allow_reopen"])
	assert.NotContains(t, defaults, "allowed")
}

func TestSetIssueStatusRegistration(t *testing.T) {
	config := &SetIssueStatusConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")}}
	outputs := &SafeOutputsConfig{SetIssueStatus: config}
	assert.True(t, HasSafeOutputsEnabled(outputs))
	assert.True(t, hasNonBuiltinSafeOutputsEnabled(outputs))
	assert.Contains(t, computeEnabledToolNames(&WorkflowData{SafeOutputs: outputs}), "set_issue_status")
	assert.Equal(t, "set_issue_status", safeOutputFieldMapping["SetIssueStatus"])
	descriptor, ok := getSafeOutputHandlerByKey("set-issue-status")
	require.True(t, ok)
	assert.Equal(t, "SetIssueStatus", descriptor.StructField)
	assert.NotNil(t, descriptor.NewConfig())
	assert.Equal(t, NewPermissionsIssuesWrite(), descriptor.PermissionBuilder(outputs))
	config.Staged = templatableBoolPtr("true")
	assert.Nil(t, descriptor.PermissionBuilder(outputs))
	config.Staged = nil

	var prompt string
	for _, section := range buildSafeOutputsSections(outputs, nil, nil) {
		prompt += section.Content
	}
	assert.Contains(t, prompt, "set_issue_status(max:3)")
	description := enhanceToolDescription("set_issue_status", "Set native status.", outputs)
	assert.Contains(t, description, "Closing an open issue")
	assert.Contains(t, description, "Reopening a closed issue")
	assert.Contains(t, description, "Maximum 3")

	for _, target := range []string{"", "triggering", "*", "42", "${{ inputs.issue }}"} {
		config.Target = target
		assert.NoError(t, validateSafeOutputsTarget(outputs), target)
	}
	config.Target = "invalid"
	assert.ErrorContains(t, validateSafeOutputsTarget(outputs), "set-issue-status")
	config.Target = ""
	config.Max = strPtr("0")
	assert.Error(t, validateSafeOutputsMax(outputs))
}

func TestCreateIssuePlanningStatusConfig(t *testing.T) {
	compiler := NewCompiler()
	for _, status := range []any{"In progress", 12} {
		config := compiler.parseCreateIssuesConfig(map[string]any{"create-issue": map[string]any{
			"planning-status": status, "allowed-statuses": []any{"In progress", 12},
		}})
		require.NotNil(t, config)
		require.NotNil(t, config.PlanningStatus)
		require.Len(t, config.AllowedStatuses, 2)
		built := issueHandlerRegistry["create_issue"](&SafeOutputsConfig{CreateIssues: config})
		assert.Equal(t, config.PlanningStatus, built["planning_status"])
		assert.Equal(t, config.AllowedStatuses, built["allowed_statuses"])
		description := enhanceToolDescription("create_issue", "Create issue.", &SafeOutputsConfig{CreateIssues: config})
		assert.Contains(t, description, "Default native planning status:")
		assert.Contains(t, description, "backlog, planned, or started")
	}
	built := issueHandlerRegistry["create_issue"](&SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}})
	assert.NotContains(t, built, "planning_status")
	assert.NotContains(t, built, "allowed_statuses")
}

func TestSetIssueStatusToolContract(t *testing.T) {
	var tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Required   []string                  `json:"required"`
			Properties map[string]map[string]any `json:"properties"`
		} `json:"inputSchema"`
	}
	require.NoError(t, decodeSafeOutputsToolsJSON(&tools))
	found := map[string]bool{}
	for _, tool := range tools {
		switch tool.Name {
		case "set_issue_status":
			found[tool.Name] = true
			assert.Equal(t, []string{"status"}, tool.InputSchema.Required)
			assert.Equal(t, []any{"string", "integer"}, tool.InputSchema.Properties["status"]["type"])
			assert.Equal(t, "integer", tool.InputSchema.Properties["duplicate_of"]["type"])
			assert.NotContains(t, tool.InputSchema.Properties, "suggest")
		case "create_issue":
			found[tool.Name] = true
			assert.Equal(t, []any{"string", "integer"}, tool.InputSchema.Properties["planning_status"]["type"])
		case "update_issue":
			found[tool.Name] = true
			assert.Equal(t, []any{"open", "closed"}, tool.InputSchema.Properties["status"]["enum"])
		}
	}
	assert.Len(t, found, 3)
	assert.True(t, ValidationConfig["set_issue_status"].Fields["status"].StringOrInteger)
	assert.True(t, ValidationConfig["create_issue"].Fields["planning_status"].StringOrInteger)
}

func TestCompileSetIssueStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: issues
engine: copilot
safe-outputs:
  set-issue-status:
    allowed: [Backlog, 12]
    target: "*"
    required-labels: [triaged]
    github-app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-issue:
    planning-status: Backlog
    allowed-statuses: [Backlog, 12]
---
Triage issues using native planning status.
`), 0600))
	compiler := NewCompiler(WithVersion("1.0.0"))
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.NotNil(t, data.SafeOutputs.SetIssueStatus)
	assert.NotNil(t, data.SafeOutputs.SetIssueStatus.GitHubApp)
	compiled, _, _, err := compiler.generateYAML(data, path)
	require.NoError(t, err)
	assert.Contains(t, compiled, "set_issue_status")
	assert.Contains(t, compiled, "issues: write")
	assert.Contains(t, compiled, "planning_status")
	var config map[string]any
	bytes, err := json.Marshal(issueHandlerRegistry["set_issue_status"](data.SafeOutputs))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(bytes, &config))
	assert.Equal(t, false, config["allow_close"])
	assert.Equal(t, false, config["allow_reopen"])
}

func TestPublicAgentOutputIssuePlanningStatus(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "schemas", "agent-output.json"))
	require.NoError(t, err)
	schema, err := parser.CompileSchema(string(content), "https://github.com/github/gh-aw/schemas/agent-output.json")
	require.NoError(t, err)
	for _, test := range []struct {
		name  string
		item  string
		valid bool
	}{
		{"set-name", `{"type":"set_issue_status","status":"In progress"}`, true},
		{"set-id", `{"type":"set_issue_status","status":12,"issue_number":"aw_1","repo":"owner/repo"}`, true},
		{"set-duplicate", `{"type":"set_issue_status","status":"Duplicate","duplicate_of":42}`, true},
		{"set-missing", `{"type":"set_issue_status"}`, false},
		{"set-zero", `{"type":"set_issue_status","status":0}`, false},
		{"set-unsafe-id", `{"type":"set_issue_status","status":9007199254740992}`, false},
		{"set-fraction", `{"type":"set_issue_status","status":1.5}`, false},
		{"set-null", `{"type":"set_issue_status","status":null}`, false},
		{"set-blank", `{"type":"set_issue_status","status":"  "}`, false},
		{"set-suggestion", `{"type":"set_issue_status","status":"Done","suggest":true}`, false},
		{"set-invalid-duplicate", `{"type":"set_issue_status","status":"Duplicate","duplicate_of":-1}`, false},
		{"create-name", `{"type":"create_issue","title":"Issue","body":"Issue body","planning_status":"Backlog"}`, true},
		{"create-id", `{"type":"create_issue","title":"Issue","body":"Issue body","planning_status":12}`, true},
		{"create-omitted", `{"type":"create_issue","title":"Issue","body":"Issue body"}`, true},
		{"create-blank", `{"type":"create_issue","title":"Issue","body":"Issue body","planning_status":""}`, false},
		{"create-unsafe-id", `{"type":"create_issue","title":"Issue","body":"Issue body","planning_status":9007199254740992}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var data any
			require.NoError(t, json.Unmarshal([]byte(`{"items":[`+test.item+`],"errors":[]}`), &data))
			err := schema.Validate(data)
			if test.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
