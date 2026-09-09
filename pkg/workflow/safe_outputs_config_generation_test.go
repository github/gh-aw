//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateSafeOutputsConfigDispatchWorkflow tests that generateSafeOutputsConfig correctly
// includes dispatch_workflow configuration with workflow_files mapping.
func TestGenerateSafeOutputsConfigDispatchWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Failed to create workflows directory")

	ciWorkflow := `name: CI
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "ci.lock.yml"), []byte(ciWorkflow), 0644),
		"Failed to write ci workflow")

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
				Workflows:            []string{"ci"},
				WorkflowFiles: map[string]string{
					"ci": ".lock.yml",
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	dispatchConfig, ok := parsed["dispatch_workflow"].(map[string]any)
	require.True(t, ok, "Expected dispatch_workflow key in config")

	assert.InDelta(t, float64(2), dispatchConfig["max"], 0.0001, "Max should be 2")

	workflowFiles, ok := dispatchConfig["workflow_files"].(map[string]any)
	require.True(t, ok, "Expected workflow_files in dispatch_workflow config")
	assert.Equal(t, ".lock.yml", workflowFiles["ci"], "ci should map to .lock.yml")
}

// TestGenerateSafeOutputsConfigNeutralizesUnresolvableNeedsExpression verifies that a
// templated expression referencing a job listed in safe-outputs.needs is neutralized in the
// agent job's copy of the safe-outputs config, since that job is only ever wired as a
// dependency of the safe_outputs handler job (see buildSafeOutputsJobNeeds), never of the
// agent job itself. Leaving the raw needs.<job> expression in the agent job's config would
// produce an actionlint "undefined property" error (see github/gh-aw#53909 /
// pr-sous-chef.lock.yml:837).
func TestGenerateSafeOutputsConfigNeutralizesUnresolvableNeedsExpression(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Needs: []string{"approval_allowlist"},
			ApproveWorkflowRun: &ApproveWorkflowRunConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("8")},
				AllowedPullRequests:  []string{"${{ needs.approval_allowlist.outputs.eligible_pull_request_numbers }}"},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")
	assert.NotContains(t, result, "needs.approval_allowlist",
		"agent job's safe-outputs config must not reference a job it does not depend on")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	approveConfig, ok := parsed["approve_workflow_run"].(map[string]any)
	require.True(t, ok, "Expected approve_workflow_run key in config")
	assert.Equal(t, []any{}, approveConfig["allowed_pull_requests"],
		"allowed_pull_requests should be neutralized to an empty array in the agent job's config")
}

func TestGenerateSafeOutputsConfigNeutralizesAllUnresolvableNeedsForms(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Needs: []string{"approval_allowlist"},
			ApproveWorkflowRun: &ApproveWorkflowRunConfig{
				AllowedPullRequests: []string{`${{ needs['approval_allowlist'].outputs.eligible_pull_request_numbers }}`},
			},
			AddComments: &AddCommentsConfig{
				AllowedCommentIDs: []string{"literal", "${{ needs.approval_allowlist.outputs.comment_ids }}"},
			},
			DataEnabled:          true,
			DataSchemaExpression: "${{ fromJSON(needs.approval_allowlist.outputs.data_schema) }}",
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	approveConfig := parsed["approve_workflow_run"].(map[string]any)
	assert.Equal(t, []any{}, approveConfig["allowed_pull_requests"])
	addCommentConfig := parsed["add_comment"].(map[string]any)
	assert.Equal(t, []any{}, addCommentConfig["allows_comment_ids"])
	assert.Empty(t, addCommentConfig["data_schema"])
}

func TestGenerateSafeOutputsConfigPreservesResolvableNeedsExpressions(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Needs: []string{"approval_allowlist"},
			AddComments: &AddCommentsConfig{
				AllowedCommentIDs: []string{"${{ needs.prepare.outputs.comment_ids }}"},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	assert.Contains(t, result, "needs.prepare.outputs.comment_ids")
}

func TestSanitizeAgentSafeOutputsConfigNoOp(t *testing.T) {
	config := map[string]any{
		"add_comment": map[string]any{
			"allowed_comment_ids": []string{"${{ needs.prepare.outputs.comment_ids }}"},
		},
	}

	sanitizeAgentSafeOutputsConfig(config, nil)

	assert.Equal(t, []string{"${{ needs.prepare.outputs.comment_ids }}"},
		config["add_comment"].(map[string]any)["allowed_comment_ids"])
}

func TestSanitizeAgentSafeOutputsConfigStringExpression(t *testing.T) {
	config := map[string]any{
		"safe_output": map[string]any{
			"data_schema": "${{ fromJSON(needs.approval_allowlist.outputs.data_schema) }}",
		},
	}

	sanitizeAgentSafeOutputsConfig(config, []string{"approval_allowlist"})

	assert.Empty(t, config["safe_output"].(map[string]any)["data_schema"])
}

func TestGenerateSafeOutputsConfigCommentMemoryToolsOnly(t *testing.T) {
	data := &WorkflowData{
		CommentMemoryConfig: &CommentMemoryConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			MemoryID:             "default",
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	require.NotEmpty(t, result)
	require.NotNil(t, data.SafeOutputs)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Contains(t, parsed, commentMemoryHandlerKey)
}

// TestGenerateSafeOutputsConfigActions tests that generateSafeOutputsConfig includes custom
// action tool names as enabled keys so both MCP server implementations register them.
func TestGenerateSafeOutputsConfigActions(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"upload_report": {
					Uses:        "actions/upload-artifact@v4",
					Description: "Upload the report",
				},
				"publish-results": {
					Uses:        "owner/action@v1",
					Description: "Publish results",
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	// Each action tool should appear as a truthy key in config.json so the MCP server
	// registers it. Names are normalized (hyphens converted to underscores).
	uploadVal, hasUploadReport := parsed["upload_report"]
	assert.True(t, hasUploadReport, "Expected upload_report key in config")
	assert.True(t, uploadVal.(bool), "upload_report value should be true")

	publishVal, hasPublishResults := parsed["publish_results"]
	assert.True(t, hasPublishResults, "Expected publish_results key in config (hyphen normalized to underscore)")
	assert.True(t, publishVal.(bool), "publish_results value should be true")
}

// TestGenerateSafeOutputsConfigActionsCollisionReturnsError tests that a custom action
// whose normalized name collides with an existing built-in handler key returns an error.
func TestGenerateSafeOutputsConfigActionsCollisionReturnsError(t *testing.T) {
	trueVal := "true"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			// add_labels is a built-in handler that produces a real config object.
			AddLabels: &AddLabelsConfig{
				SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
					Allowed: []string{"bug"},
				},
			},
			// A custom action whose normalized name matches the built-in "add_labels" key.
			Actions: map[string]*SafeOutputActionConfig{
				"add-labels": {
					Uses:        "owner/some-action@v1",
					Description: "Should trigger a collision error",
				},
			},
			// Ensure at least one handler is set to make config non-empty.
			NoOp: &NoOpConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: &trueVal}},
		},
	}

	_, err := generateSafeOutputsConfig(data)
	require.Error(t, err, "Expected an error when a custom action name collides with a built-in handler key")
	require.ErrorContains(t, err, "add-labels", "Error should mention the conflicting action name")
	require.ErrorContains(t, err, "add_labels", "Error should mention the conflicting normalized name")
}

// TestGenerateSafeOutputsConfigMissingToolWithIssue tests the missing_tool config.
// The legacy create_missing_tool_issue sub-key is no longer generated; only missing_tool is present.
func TestGenerateSafeOutputsConfigMissingToolWithIssue(t *testing.T) {
	trueVal := "true"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			MissingTool: &MissingToolConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
				CreateIssue:          &trueVal,
				TitlePrefix:          "[Missing Tool] ",
				Labels:               []string{"bug"},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	_, hasMissingTool := parsed["missing_tool"]
	assert.True(t, hasMissingTool, "Expected missing_tool key in config")

	// create_missing_tool_issue is no longer generated as a separate top-level key;
	// the missing_tool handler registry entry covers this functionality.
	_, hasCreateMissingIssue := parsed["create_missing_tool_issue"]
	assert.False(t, hasCreateMissingIssue, "create_missing_tool_issue should not be a separate key")
}

// TestGenerateSafeOutputsConfigMentions tests the mentions configuration generation.
func TestGenerateSafeOutputsConfigMentions(t *testing.T) {
	enabled := true
	allowedCollaborators := false
	max := "5"

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Mentions: &MentionsConfig{
				Enabled:              &enabled,
				AllowedCollaborators: &allowedCollaborators,
				Max:                  &max,
				Allowed:              []string{"user1", "user2"},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	mentions, ok := parsed["mentions"].(map[string]any)
	require.True(t, ok, "Expected mentions key in config")
	assert.True(t, mentions["enabled"].(bool), "enabled should be true")
	assert.False(t, mentions["allowedCollaborators"].(bool), "allowedCollaborators should be false")
	assert.InDelta(t, float64(5), mentions["max"], 0.0001, "max should be 5")
}

func TestGenerateSafeOutputsConfigMentionsTemplatableMax(t *testing.T) {
	max := "${{ inputs.max-mentions }}"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Mentions: &MentionsConfig{Max: &max},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	mentions, ok := parsed["mentions"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, max, mentions["max"])
}

func TestGenerateSafeOutputsConfigNormalizeClosingKeywordsPerType(t *testing.T) {
	enabled := true
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					NormalizeClosingKeywords: &enabled,
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")
	createPR, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "create_pull_request config should be present")
	assert.Equal(t, true, createPR["normalize_closing_keywords"], "create_pull_request.normalize_closing_keywords should be true")
	_, hasTopLevel := parsed["normalize_closing_keywords"]
	assert.False(t, hasTopLevel, "top-level normalize_closing_keywords should not be emitted")
}

// TestPopulateDispatchWorkflowFilesNoSafeOutputs tests that the function handles nil SafeOutputs gracefully.
func TestPopulateDispatchWorkflowFilesNoSafeOutputs(t *testing.T) {
	data := &WorkflowData{SafeOutputs: nil}
	// Should not panic
	populateDispatchWorkflowFiles(data, "/some/path")
}

func TestGenerateSafeOutputsConfigAddsDataFlagsForBodyHandlers(t *testing.T) {
	cfg := &SafeOutputsConfig{
		DataEnabled: true,
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
		},
	}
	data := &WorkflowData{SafeOutputs: cfg}
	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	addComment, ok := parsed["add_comment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, addComment["data_enabled"])
}

func TestGenerateSafeOutputsConfigForwardsAllowedCommentIDs(t *testing.T) {
	cfg := &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			Target:               "*",
			AllowedCommentIDs:    []string{"${{ needs.prepare.outputs.comment_ids }}"},
		},
	}
	data := &WorkflowData{SafeOutputs: cfg}
	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	addComment, ok := parsed["add_comment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "*", addComment["target"])
	assert.Equal(t, "${{ needs.prepare.outputs.comment_ids }}", addComment["allows_comment_ids"])
}

func TestGenerateSafeOutputsConfigAddsRuntimeDataSchemaExpression(t *testing.T) {
	cfg := &SafeOutputsConfig{
		DataEnabled:          true,
		DataSchemaExpression: "${{ fromJSON(needs.schema.outputs.data_schema) }}",
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
		},
	}
	data := &WorkflowData{SafeOutputs: cfg}
	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	addComment, ok := parsed["add_comment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "${{ fromJSON(needs.schema.outputs.data_schema) }}", addComment["data_schema"])
}

// TestPopulateDispatchWorkflowFilesNoWorkflows tests that the function handles empty Workflows list gracefully.
func TestPopulateDispatchWorkflowFilesNoWorkflows(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				Workflows: []string{},
			},
		},
	}
	// Should not panic or modify anything
	populateDispatchWorkflowFiles(data, "/some/path")
	assert.Nil(t, data.SafeOutputs.DispatchWorkflow.WorkflowFiles, "WorkflowFiles should remain nil")
}

// TestPopulateDispatchWorkflowFilesFindsLockFile tests that .lock.yml is preferred over .yml.
func TestPopulateDispatchWorkflowFilesFindsLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Failed to create workflows dir")

	// Create both .yml and .lock.yml files
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "deploy.yml"), []byte("name: deploy\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "deploy.lock.yml"), []byte("name: deploy\n"), 0644))

	markdownPath := filepath.Join(tmpDir, ".github", "aw", "test.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(markdownPath), 0755))

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				Workflows: []string{"deploy"},
			},
		},
	}

	populateDispatchWorkflowFiles(data, markdownPath)

	require.NotNil(t, data.SafeOutputs.DispatchWorkflow.WorkflowFiles, "WorkflowFiles should be populated")
	assert.Equal(t, ".lock.yml", data.SafeOutputs.DispatchWorkflow.WorkflowFiles["deploy"],
		"Should prefer .lock.yml over .yml")
}

// TestGenerateCustomJobToolDefinition tests that generateCustomJobToolDefinition produces
// valid MCP tool definitions from SafeJobConfig input definitions.
func TestGenerateCustomJobToolDefinition(t *testing.T) {
	tests := []struct {
		name      string
		jobName   string
		jobConfig *SafeJobConfig
		check     func(t *testing.T, result map[string]any)
	}{
		{
			name:    "basic string input",
			jobName: "my_job",
			jobConfig: &SafeJobConfig{
				Description: "A test job",
				Inputs: map[string]*InputDefinition{
					"title": {
						Type:        "string",
						Description: "The title",
						Required:    true,
					},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				assert.Equal(t, "my_job", result["name"], "name should match job name")
				assert.Equal(t, "A test job", result["description"], "description should be included")
				schema, ok := result["inputSchema"].(map[string]any)
				require.True(t, ok, "inputSchema should be a map")
				assert.Equal(t, "object", schema["type"], "schema type should be object")
				assert.False(t, schema["additionalProperties"].(bool), "additionalProperties should be false")
				props, ok := schema["properties"].(map[string]any)
				require.True(t, ok, "properties should be a map")
				titleProp, ok := props["title"].(map[string]any)
				require.True(t, ok, "title property should exist")
				assert.Equal(t, "string", titleProp["type"], "title type should be string")
				assert.Equal(t, "The title", titleProp["description"], "title description should be set")
				required, ok := schema["required"].([]string)
				require.True(t, ok, "required should be a []string")
				assert.Contains(t, required, "title", "title should be required")
			},
		},
		{
			name:    "boolean input",
			jobName: "bool_job",
			jobConfig: &SafeJobConfig{
				Inputs: map[string]*InputDefinition{
					"flag": {
						Type:     "boolean",
						Required: false,
					},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				schema := result["inputSchema"].(map[string]any)
				props := schema["properties"].(map[string]any)
				flagProp := props["flag"].(map[string]any)
				assert.Equal(t, "boolean", flagProp["type"], "flag type should be boolean")
				assert.Nil(t, schema["required"], "required should be absent when no required fields")
			},
		},
		{
			name:    "number input",
			jobName: "num_job",
			jobConfig: &SafeJobConfig{
				Inputs: map[string]*InputDefinition{
					"count": {
						Type:     "number",
						Required: true,
					},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				schema := result["inputSchema"].(map[string]any)
				props := schema["properties"].(map[string]any)
				countProp := props["count"].(map[string]any)
				assert.Equal(t, "number", countProp["type"], "count type should be number")
			},
		},
		{
			name:    "choice input with enum",
			jobName: "choice_job",
			jobConfig: &SafeJobConfig{
				Inputs: map[string]*InputDefinition{
					"color": {
						Type:    "choice",
						Options: []string{"red", "green", "blue"},
					},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				schema := result["inputSchema"].(map[string]any)
				props := schema["properties"].(map[string]any)
				colorProp := props["color"].(map[string]any)
				assert.Equal(t, "string", colorProp["type"], "choice type should map to string")
				assert.Equal(t, []string{"red", "green", "blue"}, colorProp["enum"], "enum options should be set")
			},
		},
		{
			name:    "no inputs",
			jobName: "empty_job",
			jobConfig: &SafeJobConfig{
				Description: "No inputs",
			},
			check: func(t *testing.T, result map[string]any) {
				assert.Equal(t, "empty_job", result["name"], "name should match")
				schema := result["inputSchema"].(map[string]any)
				props := schema["properties"].(map[string]any)
				assert.Empty(t, props, "properties should be empty")
				assert.Nil(t, schema["required"], "required should be absent")
			},
		},
		{
			name:    "no description uses default",
			jobName: "nodesc_job",
			jobConfig: &SafeJobConfig{
				Inputs: map[string]*InputDefinition{
					"x": {Type: "string"},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				desc, hasDesc := result["description"]
				assert.True(t, hasDesc, "description should be present (default is added)")
				assert.Contains(t, desc.(string), "nodesc_job", "default description should include job name")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateCustomJobToolDefinition(tt.jobName, tt.jobConfig)
			require.NotNil(t, result, "result should not be nil")
			tt.check(t, result)
		})
	}
}

// TestGenerateCustomJobToolDefinitionJSONSerializable verifies that the output of
// generateCustomJobToolDefinition can be marshaled to valid JSON.
func TestGenerateCustomJobToolDefinitionJSONSerializable(t *testing.T) {
	jobConfig := &SafeJobConfig{
		Description: "Run deployment",
		Inputs: map[string]*InputDefinition{
			"env": {
				Type:        "choice",
				Description: "Target environment",
				Required:    true,
				Options:     []string{"staging", "production"},
			},
			"dry_run": {
				Type:     "boolean",
				Required: false,
			},
		},
	}

	result := generateCustomJobToolDefinition("deploy", jobConfig)
	data, err := json.Marshal(result)
	require.NoError(t, err, "result should be JSON serializable")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed), "JSON should be parseable back")
	assert.Equal(t, "deploy", parsed["name"], "name should round-trip through JSON")
}

// TestGenerateSafeOutputsConfigAddLabelsBlocked tests that the blocked field is included
// in config.json for add_labels.
func TestGenerateSafeOutputsConfigAddLabelsBlocked(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					Target:         "*",
					TargetRepoSlug: "microsoft/vscode",
				},
				SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
					Allowed: []string{"bug", "enhancement"},
					Blocked: []string{"[*]*", "~spam", "stale", "triage-needed"},
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	addLabelsConfig, ok := parsed["add_labels"].(map[string]any)
	require.True(t, ok, "Expected add_labels key in config")

	blocked, ok := addLabelsConfig["blocked"]
	require.True(t, ok, "Expected blocked field in add_labels config")
	blockedSlice, ok := blocked.([]any)
	require.True(t, ok, "Blocked should be an array")
	assert.Len(t, blockedSlice, 4, "Should have 4 blocked patterns")
	assert.Equal(t, "[*]*", blockedSlice[0], "First blocked pattern should match")
	assert.Equal(t, "~spam", blockedSlice[1], "Second blocked pattern should match")
	assert.Equal(t, "stale", blockedSlice[2], "Third blocked pattern should match")
	assert.Equal(t, "triage-needed", blockedSlice[3], "Fourth blocked pattern should match")
}

// TestGenerateSafeOutputsConfigAddLabelsCreateIfMissing tests that the create-if-missing
// field is included in config.json for add_labels when set, and omitted when nil.
func TestGenerateSafeOutputsConfigAddLabelsCreateIfMissing(t *testing.T) {
	trueVal := true
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
				CreateIfMissing:      &trueVal,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	addLabelsConfig, ok := parsed["add_labels"].(map[string]any)
	require.True(t, ok, "Expected add_labels key in config")

	createIfMissing, ok := addLabelsConfig["create_if_missing"]
	require.True(t, ok, "Expected create_if_missing field in add_labels config")
	assert.Equal(t, true, createIfMissing, "create_if_missing should be true")

	// When CreateIfMissing is nil (default), the field should be omitted entirely.
	dataDefault := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
			},
		},
	}
	resultDefault, err := generateSafeOutputsConfig(dataDefault)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")

	var parsedDefault map[string]any
	require.NoError(t, json.Unmarshal([]byte(resultDefault), &parsedDefault), "Result must be valid JSON")
	addLabelsConfigDefault, ok := parsedDefault["add_labels"].(map[string]any)
	require.True(t, ok, "Expected add_labels key in config")
	_, ok = addLabelsConfigDefault["create_if_missing"]
	assert.False(t, ok, "create_if_missing should be omitted when not configured")
}

// TestGenerateSafeOutputsConfigSafeJobMax tests that the max field is emitted in config.json
// for custom safe-jobs so the output collector can enforce it.
func TestGenerateSafeOutputsConfigSafeJobMax(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"emit-finding": {
					Description: "Emit a quality finding",
					Max:         25,
					Inputs: map[string]*InputDefinition{
						"message": {Type: "string", Required: true},
					},
				},
				"single-output": {
					Description: "Max not set — should be omitted from config",
					Inputs: map[string]*InputDefinition{
						"body": {Type: "string"},
					},
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	findingCfg, ok := parsed["emit-finding"].(map[string]any)
	require.True(t, ok, "emit-finding should be present in config")
	assert.InDelta(t, float64(25), findingCfg["max"], 0.0001, "max should be 25")

	singleCfg, ok := parsed["single-output"].(map[string]any)
	require.True(t, ok, "single-output should be present in config")
	_, hasMax := singleCfg["max"]
	assert.False(t, hasMax, "max should be absent when not configured (lets runtime default to 1)")
}

// TestGenerateSafeOutputsConfigCreatePullRequestTargetRepo tests that target-repo
// and related cross-repo fields are included in config.json for create_pull_request.
func TestGenerateSafeOutputsConfigCreatePullRequestTargetRepo(t *testing.T) {
	falseVal := false
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "caido/proxy-frontend",
				AllowedRepos:         []string{"caido/other-repo"},
				BaseBranch:           "dev",
				Draft:                strPtr("true"),
				Reviewers:            []string{"corb3nik"},
				TeamReviewers:        []string{"platform-reviewers"},
				TitlePrefix:          "[refactor] ",
				FallbackAsIssue:      &falseVal,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	assert.Equal(t, "caido/proxy-frontend", prConfig["target-repo"], "target-repo should be set")

	allowedRepos, ok := prConfig["allowed_repos"].([]any)
	require.True(t, ok, "allowed_repos should be an array")
	assert.Len(t, allowedRepos, 1, "Should have 1 allowed repo")
	assert.Equal(t, "caido/other-repo", allowedRepos[0], "allowed_repos should match")

	assert.Equal(t, "dev", prConfig["base_branch"], "base_branch should be set")
	assert.True(t, prConfig["draft"].(bool), "draft should be true")

	reviewers, ok := prConfig["reviewers"].([]any)
	require.True(t, ok, "reviewers should be an array")
	assert.Len(t, reviewers, 1, "Should have 1 reviewer")
	assert.Equal(t, "corb3nik", reviewers[0], "reviewer should match")

	teamReviewers, ok := prConfig["team_reviewers"].([]any)
	require.True(t, ok, "team_reviewers should be an array")
	assert.Len(t, teamReviewers, 1, "Should have 1 team reviewer")
	assert.Equal(t, "platform-reviewers", teamReviewers[0], "team reviewer should match")

	assert.Equal(t, "[refactor] ", prConfig["title_prefix"], "title_prefix should be set")
	assert.False(t, prConfig["fallback_as_issue"].(bool), "fallback_as_issue should be false")
}

// TestGenerateSafeOutputsConfigCreatePullRequestBackwardCompat tests that config without
// target-repo still works correctly (backward compatibility).
func TestGenerateSafeOutputsConfigCreatePullRequestBackwardCompat(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig:          BaseSafeOutputConfig{Max: strPtr("2")},
				SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"bug"}},
				AllowEmpty:                    strPtr("true"),
				AutoMerge:                     strPtr("true"),
				Expires:                       24,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	assert.InDelta(t, float64(2), prConfig["max"], 0.0001, "max should be 2")
	assert.True(t, prConfig["allow_empty"].(bool), "allow_empty should be true")
	assert.True(t, prConfig["auto_merge"].(bool), "auto_merge should be true")
	assert.InDelta(t, float64(24), prConfig["expires"], 0.0001, "expires should be 24")

	// target-repo and allowed_repos should not be present when not configured
	_, hasTargetRepo := prConfig["target-repo"]
	assert.False(t, hasTargetRepo, "target-repo should not be present when not configured")
	_, hasAllowedRepos := prConfig["allowed_repos"]
	assert.False(t, hasAllowedRepos, "allowed_repos should not be present when not configured")
}

func TestGenerateSafeOutputsConfigCreatePullRequestAutoMergeMethod(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				AutoMerge:            strPtr("rebase"),
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")
	assert.Equal(t, "rebase", prConfig["auto_merge"], "auto_merge should preserve explicit merge method")
}

func TestGenerateSafeOutputsConfigInjectsCurrentCheckoutPatchWorkspacePath(t *testing.T) {
	data := &WorkflowData{
		CheckoutConfigs: []*CheckoutConfig{
			{
				Repository: "caido/proxy-frontend",
				Path:       "./proxy-frontend",
				Current:    true,
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "caido/proxy-frontend",
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				TargetRepoSlug: "caido/proxy-frontend",
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")
	assert.Equal(t, "proxy-frontend", prConfig["patch_workspace_path"])
	assert.Equal(t, "caido/proxy-frontend", prConfig["current_checkout_repo"])

	pushConfig, ok := parsed["push_to_pull_request_branch"].(map[string]any)
	require.True(t, ok, "Expected push_to_pull_request_branch key in config")
	assert.Equal(t, "proxy-frontend", pushConfig["patch_workspace_path"])
	assert.Equal(t, "caido/proxy-frontend", pushConfig["current_checkout_repo"])
}

func TestGenerateSafeOutputsConfigSkipsPatchWorkspacePathForExplicitTargetRepoWhenCurrentRepoIsWorkflowRepo(t *testing.T) {
	data := &WorkflowData{
		CheckoutConfigs: []*CheckoutConfig{
			{
				Path:    "./proxy-frontend",
				Current: true,
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "caido/proxy-frontend",
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				TargetRepoSlug: "caido/proxy-frontend",
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")
	assert.NotContains(t, prConfig, "patch_workspace_path")
	assert.NotContains(t, prConfig, "current_checkout_repo")

	pushConfig, ok := parsed["push_to_pull_request_branch"].(map[string]any)
	require.True(t, ok, "Expected push_to_pull_request_branch key in config")
	assert.NotContains(t, pushConfig, "patch_workspace_path")
	assert.NotContains(t, pushConfig, "current_checkout_repo")
}

func TestGenerateSafeOutputsConfigCreatePullRequestIncludesEngineManifests(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "claude"},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	protectedFiles := parseStringSliceAny(prConfig["protected_files"], nil)
	assert.Contains(t, protectedFiles, "CLAUDE.md", "CLAUDE.md should be protected for Claude engine workflows")
	assert.Contains(t, protectedFiles, "AGENTS.md", "AGENTS.md should be protected for Claude engine workflows")
	assert.Contains(t, protectedFiles, "DESIGN.md", "DESIGN.md should be protected by default")

	protectedPathPrefixes := parseStringSliceAny(prConfig["protected_path_prefixes"], nil)
	assert.NotContains(t, protectedPathPrefixes, ".claude/", ".claude/ is covered by the general dot-folder rule, not explicit prefix list")
	assert.NotContains(t, protectedPathPrefixes, ".githooks/", ".githooks/ is covered by the general dot-folder rule, not explicit prefix list")
	assert.NotContains(t, protectedPathPrefixes, ".husky/", ".husky/ is covered by the general dot-folder rule, not explicit prefix list")
}

func TestGenerateSafeOutputsConfigCreatePullRequestAppliesProtectedFilesExclude(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "claude"},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig:  BaseSafeOutputConfig{Max: strPtr("1")},
				ProtectedFilesExclude: []string{"CLAUDE.md", ".claude/"},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	protectedFiles := parseStringSliceAny(prConfig["protected_files"], nil)
	assert.NotContains(t, protectedFiles, "CLAUDE.md", "CLAUDE.md should be excluded from protected_files")
	assert.Contains(t, protectedFiles, "AGENTS.md", "AGENTS.md should remain in protected_files")

	protectedPathPrefixes := parseStringSliceAny(prConfig["protected_path_prefixes"], nil)
	assert.NotContains(t, protectedPathPrefixes, ".claude/", ".claude/ should be absent from protected_path_prefixes (covered by general dot-folder rule)")
	// .github/ is also covered by the general dot-folder rule, not the explicit prefix list
	assert.NotContains(t, protectedPathPrefixes, ".github/", ".github/ should be absent from protected_path_prefixes (covered by general dot-folder rule)")
}

// TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssue tests that auto_close_issue
// is correctly serialized into config.json for create_pull_request.
func TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssue(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				AutoCloseIssue:       strPtr("false"),
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	assert.False(t, prConfig["auto_close_issue"].(bool), "auto_close_issue should be false")
}

// TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssueExpression tests that
// auto_close_issue supports GitHub Actions expression strings.
func TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssueExpression(t *testing.T) {
	expr := "${{ inputs.auto-close-issue }}"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				AutoCloseIssue:       &expr,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	assert.Equal(t, expr, prConfig["auto_close_issue"], "auto_close_issue should be an expression string")
}

// TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssueOmittedByDefault tests that
// auto_close_issue is omitted when not configured (backward compatibility).
func TestGenerateSafeOutputsConfigCreatePullRequestAutoCloseIssueOmittedByDefault(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	prConfig, ok := parsed["create_pull_request"].(map[string]any)
	require.True(t, ok, "Expected create_pull_request key in config")

	_, hasAutoCloseIssue := prConfig["auto_close_issue"]
	assert.False(t, hasAutoCloseIssue, "auto_close_issue should be absent when not configured")
}

// TestGenerateSafeOutputsConfigRepoMemory tests that generateSafeOutputsConfig includes
// push_repo_memory configuration with the expected memories entries when RepoMemoryConfig is present.
func TestGenerateSafeOutputsConfigRepoMemory(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:           "default",
					MaxFileSize:  5120,
					MaxPatchSize: 20480,
					MaxFileCount: 50,
				},
				{
					ID:           "notes",
					MaxFileSize:  2048,
					MaxPatchSize: 8192,
					MaxFileCount: 20,
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	pushRepoMemory, ok := parsed["push_repo_memory"].(map[string]any)
	require.True(t, ok, "Expected push_repo_memory key in config")

	memories, ok := pushRepoMemory["memories"].([]any)
	require.True(t, ok, "Expected memories to be an array")
	require.Len(t, memories, 2, "Expected 2 memory entries")

	// Check first memory entry
	mem0, ok := memories[0].(map[string]any)
	require.True(t, ok, "First memory entry should be a map")
	assert.Equal(t, "default", mem0["id"], "First memory id should match")
	assert.Equal(t, "/tmp/gh-aw/repo-memory/default", mem0["dir"], "First memory dir should be correct")
	assert.InDelta(t, float64(5120), mem0["max_file_size"], 0.0001, "First memory max_file_size should match")
	assert.InDelta(t, float64(20480), mem0["max_patch_size"], 0.0001, "First memory max_patch_size should match")
	assert.InDelta(t, float64(50), mem0["max_file_count"], 0.0001, "First memory max_file_count should match")

	// Check second memory entry
	mem1, ok := memories[1].(map[string]any)
	require.True(t, ok, "Second memory entry should be a map")
	assert.Equal(t, "notes", mem1["id"], "Second memory id should match")
	assert.Equal(t, "/tmp/gh-aw/repo-memory/notes", mem1["dir"], "Second memory dir should be correct")
	assert.InDelta(t, float64(2048), mem1["max_file_size"], 0.0001, "Second memory max_file_size should match")
}

// TestGenerateSafeOutputsConfigRepoMemoryFilters tests that generateSafeOutputsConfig
// passes allowed-extensions/file-glob through to the push_repo_memory preflight config,
// so the MCP tool preflight can apply the same eligibility filtering as the agent-side
// filter step and the push job (regression: preflight was previously unfiltered).
func TestGenerateSafeOutputsConfigRepoMemoryFilters(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:                "default",
					MaxFileSize:       5120,
					MaxPatchSize:      20480,
					MaxFileCount:      50,
					AllowedExtensions: []string{".json", ".md"},
					FileGlob:          []string{"*.json", "metrics/**"},
				},
				{
					ID:           "notes",
					MaxFileSize:  2048,
					MaxPatchSize: 8192,
					MaxFileCount: 20,
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	pushRepoMemory, ok := parsed["push_repo_memory"].(map[string]any)
	require.True(t, ok, "Expected push_repo_memory key in config")
	memories, ok := pushRepoMemory["memories"].([]any)
	require.True(t, ok, "Expected memories to be an array")
	require.Len(t, memories, 2, "Expected 2 memory entries")

	mem0, ok := memories[0].(map[string]any)
	require.True(t, ok, "First memory entry should be a map")
	allowedExts, ok := mem0["allowed_extensions"].([]any)
	require.True(t, ok, "First memory should carry allowed_extensions")
	assert.Equal(t, []any{".json", ".md"}, allowedExts, "allowed_extensions should be passed through")
	assert.Equal(t, "*.json metrics/**", mem0["file_glob"], "file_glob should be joined with spaces")

	mem1, ok := memories[1].(map[string]any)
	require.True(t, ok, "Second memory entry should be a map")
	_, hasAllowedExts := mem1["allowed_extensions"]
	assert.False(t, hasAllowedExts, "Second memory should not carry allowed_extensions when unset")
	_, hasFileGlob := mem1["file_glob"]
	assert.False(t, hasFileGlob, "Second memory should not carry file_glob when unset")
}

// TestGenerateSafeOutputsConfigNoRepoMemory tests that push_repo_memory is absent
// from the config when RepoMemoryConfig is not present.
func TestGenerateSafeOutputsConfigNoRepoMemory(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		RepoMemoryConfig: nil,
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	_, hasPushRepoMemory := parsed["push_repo_memory"]
	assert.False(t, hasPushRepoMemory, "push_repo_memory should not be present when RepoMemoryConfig is nil")
}

// TestGenerateSafeOutputsConfigEmptyRepoMemory tests that push_repo_memory is absent
// from the config when RepoMemoryConfig has no memories.
func TestGenerateSafeOutputsConfigEmptyRepoMemory(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			// Include a non-nil handler so the config is non-empty
			CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
		},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	_, hasPushRepoMemory := parsed["push_repo_memory"]
	assert.False(t, hasPushRepoMemory, "push_repo_memory should not be present when Memories slice is empty")
}

// TestGenerateSafeOutputsConfigReplyToPullRequestReviewComment verifies that
// reply_to_pull_request_review_comment appears in config.json when configured.
// Previously this key was missing from generateSafeOutputsConfig, causing the
// safe-outputs MCP server to skip the tool at runtime.
func TestGenerateSafeOutputsConfigReplyToPullRequestReviewComment(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ReplyToPullRequestReviewComment: &ReplyToPullRequestReviewCommentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("25")},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	replyConfig, ok := parsed["reply_to_pull_request_review_comment"].(map[string]any)
	require.True(t, ok, "Expected reply_to_pull_request_review_comment key in config.json")
	assert.InDelta(t, float64(25), replyConfig["max"], 0.0001, "max should be 25")
}

// TestGenerateSafeOutputsConfigReplyToPullRequestReviewCommentWithTarget verifies that
// target, target-repo, allowed_repos, and footer are forwarded to config.json.
func TestGenerateSafeOutputsConfigReplyToPullRequestReviewCommentWithTarget(t *testing.T) {
	footerTrue := "true"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ReplyToPullRequestReviewComment: &ReplyToPullRequestReviewCommentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max:    strPtr("10"),
					Footer: &footerTrue,
				},
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					Target:         "pull_request",
					TargetRepoSlug: "org/other-repo",
					AllowedRepos:   []string{"org/other-repo"},
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	replyConfig, ok := parsed["reply_to_pull_request_review_comment"].(map[string]any)
	require.True(t, ok, "Expected reply_to_pull_request_review_comment key in config.json")
	assert.InDelta(t, float64(10), replyConfig["max"], 0.0001, "max should be 10")
	assert.Equal(t, "pull_request", replyConfig["target"], "target should be set")
	assert.Equal(t, "org/other-repo", replyConfig["target-repo"], "target-repo should be set")

	allowedRepos, ok := replyConfig["allowed_repos"].([]any)
	require.True(t, ok, "allowed_repos should be an array")
	assert.Len(t, allowedRepos, 1, "Should have 1 allowed repo")
	assert.Equal(t, "org/other-repo", allowedRepos[0], "allowed_repos entry should match")

	assert.True(t, replyConfig["footer"].(bool), "footer should be true")
}

// TestGenerateSafeOutputsConfigClosePullRequest tests that generateSafeOutputsConfig correctly
// includes close_pull_request configuration in config.json.
func TestGenerateSafeOutputsConfigClosePullRequest(t *testing.T) {
	maxVal := "3"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ClosePullRequests: &ClosePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max:         &maxVal,
					GitHubToken: "${{ secrets.MY_TOKEN }}",
				},
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					Target:         "*",
					TargetRepoSlug: "org/repo",
					AllowedRepos:   []string{"org/other-repo"},
				},
				SafeOutputFilterConfig: SafeOutputFilterConfig{
					RequiredLabels:      []string{"ready-to-close"},
					RequiredTitlePrefix: "[my-prefix]",
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	closePRConfig, ok := parsed["close_pull_request"].(map[string]any)
	require.True(t, ok, "Expected close_pull_request key in config.json")

	assert.InDelta(t, float64(3), closePRConfig["max"], 0.0001, "max should be 3")
	assert.Equal(t, "*", closePRConfig["target"], "target should be set")
	assert.Equal(t, "org/repo", closePRConfig["target-repo"], "target-repo should be set")
	assert.Equal(t, "${{ secrets.MY_TOKEN }}", closePRConfig["github-token"], "github-token should be set")
	assert.Equal(t, "[my-prefix]", closePRConfig["required_title_prefix"], "required_title_prefix should be set")

	allowedRepos, ok := closePRConfig["allowed_repos"].([]any)
	require.True(t, ok, "allowed_repos should be an array")
	assert.Len(t, allowedRepos, 1, "Should have 1 allowed repo")
	assert.Equal(t, "org/other-repo", allowedRepos[0], "allowed_repos entry should match")

	requiredLabels, ok := closePRConfig["required_labels"].([]any)
	require.True(t, ok, "required_labels should be an array")
	assert.Len(t, requiredLabels, 1, "Should have 1 required label")
	assert.Equal(t, "ready-to-close", requiredLabels[0], "required_labels entry should match")
}

// TestGenerateSafeOutputsConfigClosePullRequestStaged tests that staged is included in config.json.
func TestGenerateSafeOutputsConfigClosePullRequestStaged(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			ClosePullRequests: &ClosePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Staged: templatableBoolPtr("true"),
				},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	closePRConfig, ok := parsed["close_pull_request"].(map[string]any)
	require.True(t, ok, "Expected close_pull_request key in config.json")

	assert.True(t, closePRConfig["staged"].(bool), "staged should be true")
	assert.Nil(t, closePRConfig["github-token"], "github-token should not be set when empty")
}

// TestGenerateSafeOutputsConfigDeduplicateByTitleBool tests that deduplicate_by_title
// with a boolean value is correctly serialized into config.json for create_issue.
func TestGenerateSafeOutputsConfigDeduplicateByTitleBool(t *testing.T) {
	trueVal := TemplatableBoolOrInt("true")
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				DeduplicateByTitle:   &trueVal,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	ciConfig, ok := parsed["create_issue"].(map[string]any)
	require.True(t, ok, "Expected create_issue key in config")

	assert.Equal(t, true, ciConfig["deduplicate_by_title"], "deduplicate_by_title should be true (JSON boolean)")
}

// TestGenerateSafeOutputsConfigDeduplicateByTitleFalse tests that deduplicate_by_title
// with an explicit false value is correctly serialized into config.json for create_issue.
func TestGenerateSafeOutputsConfigDeduplicateByTitleFalse(t *testing.T) {
	falseVal := TemplatableBoolOrInt("false")
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				DeduplicateByTitle:   &falseVal,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	ciConfig, ok := parsed["create_issue"].(map[string]any)
	require.True(t, ok, "Expected create_issue key in config")

	assert.Equal(t, false, ciConfig["deduplicate_by_title"], "deduplicate_by_title should be false (JSON boolean)")
}

// TestGenerateSafeOutputsConfigDeduplicateByTitleInt tests that deduplicate_by_title
// with an integer value is correctly serialized into config.json for create_issue.
func TestGenerateSafeOutputsConfigDeduplicateByTitleInt(t *testing.T) {
	intVal := TemplatableBoolOrInt("2")
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				DeduplicateByTitle:   &intVal,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	ciConfig, ok := parsed["create_issue"].(map[string]any)
	require.True(t, ok, "Expected create_issue key in config")

	// JSON numbers unmarshal as float64 in map[string]any
	assert.InDelta(t, float64(2), ciConfig["deduplicate_by_title"], 0.0001, "deduplicate_by_title should be 2 (JSON number)")
}

// TestGenerateSafeOutputsConfigDeduplicateByTitleExpression tests that deduplicate_by_title
// with a GitHub Actions expression is correctly serialized as a JSON string into config.json.
func TestGenerateSafeOutputsConfigDeduplicateByTitleExpression(t *testing.T) {
	expr := TemplatableBoolOrInt("${{ inputs.dedup }}")
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				DeduplicateByTitle:   &expr,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	ciConfig, ok := parsed["create_issue"].(map[string]any)
	require.True(t, ok, "Expected create_issue key in config")

	assert.Equal(t, "${{ inputs.dedup }}", ciConfig["deduplicate_by_title"], "deduplicate_by_title should be the expression string")
}

// TestGenerateSafeOutputsConfigDeduplicateByTitleNil tests that deduplicate_by_title
// is omitted from config.json when not set.
func TestGenerateSafeOutputsConfigDeduplicateByTitleNil(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				DeduplicateByTitle:   nil,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	ciConfig, ok := parsed["create_issue"].(map[string]any)
	require.True(t, ok, "Expected create_issue key in config")

	_, hasDedup := ciConfig["deduplicate_by_title"]
	assert.False(t, hasDedup, "deduplicate_by_title should not be present when nil")
}

// TestGenerateSafeOutputsConfigMaxBotMentions tests that max-bot-mentions is correctly
// propagated as "max_bot_mentions" into config.json for the MCP server.
func TestGenerateSafeOutputsConfigMaxBotMentions(t *testing.T) {
	maxBotMentions := "5"
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
			MaxBotMentions: &maxBotMentions,
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	maxBotMentionsVal, ok := parsed["max_bot_mentions"]
	require.True(t, ok, "Expected max_bot_mentions key in config")
	assert.EqualValues(t, 5, maxBotMentionsVal, "max_bot_mentions should be 5")
}

// TestGenerateSafeOutputsConfigMaxBotMentionsAbsent tests that max_bot_mentions is
// omitted from config.json when not configured.
func TestGenerateSafeOutputsConfigMaxBotMentionsAbsent(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
	require.NotEmpty(t, result, "Expected non-empty config")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "Result must be valid JSON")

	_, hasMaxBotMentions := parsed["max_bot_mentions"]
	assert.False(t, hasMaxBotMentions, "max_bot_mentions should not be present when not configured")
}
