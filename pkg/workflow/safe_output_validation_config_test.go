//go:build !integration

package workflow

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestGetValidationConfigJSON(t *testing.T) {
	// Test with nil (all types)
	jsonStr, err := GetValidationConfigJSONWithDataSchema(nil, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]TypeValidationConfig
	err = json.Unmarshal([]byte(jsonStr), &parsed)
	if err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	// Verify all expected types are present
	expectedTypes := []string{
		"create_issue",
		"create_agent_session",
		"approve_workflow_run",
		"add_comment",
		"create_pull_request",
		"add_labels",
		"add_reviewer",
		"assign_milestone",
		"assign_to_agent",
		"assign_to_user",
		"update_issue",
		"update_pull_request",
		"push_to_pull_request_branch",
		"create_pull_request_review_comment",
		"submit_pull_request_review",
		"create_discussion",
		"close_discussion",
		"close_issue",
		"close_pull_request",
		"missing_tool",
		"update_release",
		"upload_asset",
		"noop",
		"create_code_scanning_alert",
		"link_sub_issue",
		"update_discussion",
		"remove_labels",
		"replace_label",
		"unassign_from_user",
		"hide_comment",
		"missing_data",
		"autofix_code_scanning_alert",
		"mark_pull_request_as_ready_for_review",
		"report_incomplete",
	}

	for _, typeName := range expectedTypes {
		if _, ok := parsed[typeName]; !ok {
			t.Errorf("Expected type %q not found in validation config", typeName)
		}
	}

	// Verify JSON is indented (contains newlines)
	if !containsNewline(jsonStr) {
		t.Error("Expected indented JSON output with newlines")
	}
}

func TestApproveWorkflowRunValidationConfig(t *testing.T) {
	config, ok := ValidationConfig["approve_workflow_run"]
	if !ok {
		t.Fatal("approve_workflow_run not found in ValidationConfig")
	}
	if config.DefaultMax != 1 {
		t.Errorf("approve_workflow_run DefaultMax = %d, want 1", config.DefaultMax)
	}
	if runID := config.Fields["run_id"]; !runID.Required || !runID.PositiveInteger {
		t.Errorf("approve_workflow_run run_id = %+v, want required positive integer", runID)
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"approve_workflow_run"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSONWithDataSchema() error = %v", err)
	}
	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}
	parsedConfig, ok := parsed["approve_workflow_run"]
	if len(parsed) != 1 || !ok || parsedConfig.DefaultMax != 1 {
		t.Errorf("approve_workflow_run validation config = %#v, want defaultMax 1", parsedConfig)
	}
	if runID := parsedConfig.Fields["run_id"]; !runID.Required || !runID.PositiveInteger {
		t.Errorf("approve_workflow_run generated run_id = %+v, want required positive integer", runID)
	}
}

// TestCallWorkflowValidationConfigPreservesWorkflowName is a regression test for
// samples-mode call-workflow replay dropping the workflow name (github/gh-aw#55176):
// without a "call_workflow" entry in ValidationConfig, collect_ndjson_output.cjs
// fell back to validateItemWithSafeJobConfig, which drops every field except
// "type" because the call_workflow safe-outputs config has no "inputs" key. This
// test verifies the compiler emits a validation config that declares
// "workflow_name" (required) and "inputs" for call_workflow, mirroring
// dispatch_workflow, so the field survives ingestion.
func TestCallWorkflowValidationConfigPreservesWorkflowName(t *testing.T) {
	config, ok := ValidationConfig["call_workflow"]
	if !ok {
		t.Fatal("call_workflow not found in ValidationConfig")
	}
	if config.DefaultMax != 1 {
		t.Errorf("call_workflow DefaultMax = %d, want 1", config.DefaultMax)
	}
	workflowName, ok := config.Fields["workflow_name"]
	if !ok || !workflowName.Required || workflowName.Type != "string" {
		t.Errorf("call_workflow workflow_name = %+v, want required string", workflowName)
	}
	inputs, ok := config.Fields["inputs"]
	if !ok || inputs.Type != "object" {
		t.Errorf("call_workflow inputs = %+v, want object", inputs)
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"call_workflow"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSONWithDataSchema() error = %v", err)
	}
	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}
	parsedConfig, ok := parsed["call_workflow"]
	if len(parsed) != 1 || !ok || parsedConfig.DefaultMax != 1 {
		t.Errorf("call_workflow validation config = %#v, want defaultMax 1", parsedConfig)
	}
	if workflowName := parsedConfig.Fields["workflow_name"]; !workflowName.Required || workflowName.Type != "string" {
		t.Errorf("generated call_workflow workflow_name = %+v, want required string", workflowName)
	}
}

func TestDismissPullRequestReviewValidationConfigSupportsAutoReviewID(t *testing.T) {
	config, ok := ValidationConfig["dismiss_pull_request_review"]
	if !ok {
		t.Fatal("dismiss_pull_request_review not found in ValidationConfig")
	}
	if reviewID := config.Fields["review_id"]; !reviewID.OptionalPositiveInteger || !reviewID.AllowAuto || reviewID.Required || reviewID.PositiveInteger {
		t.Errorf("dismiss_pull_request_review review_id = %+v, want optional positive integer or auto", reviewID)
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"dismiss_pull_request_review"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSONWithDataSchema() error = %v", err)
	}
	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}
	if reviewID := parsed["dismiss_pull_request_review"].Fields["review_id"]; !reviewID.OptionalPositiveInteger || !reviewID.AllowAuto {
		t.Errorf("generated dismiss_pull_request_review review_id = %+v, want optional positive integer or auto", reviewID)
	}
}

func TestUpdatePullRequestValidationConfigSupportsReplaceIsland(t *testing.T) {
	config, ok := ValidationConfig["update_pull_request"]
	if !ok {
		t.Fatal("update_pull_request not found in ValidationConfig")
	}
	operation := config.Fields["operation"]
	if !slices.Contains(operation.Enum, "replace-island") {
		t.Fatalf("update_pull_request operation enum = %v, want replace-island", operation.Enum)
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"update_pull_request"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSONWithDataSchema() error = %v", err)
	}
	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}
	parsedOperation := parsed["update_pull_request"].Fields["operation"]
	if !slices.Contains(parsedOperation.Enum, "replace-island") {
		t.Fatalf("generated update_pull_request operation enum = %v, want replace-island", parsedOperation.Enum)
	}
}

func TestGetValidationConfigJSONFiltered(t *testing.T) {
	// Test with filtered types
	enabledTypes := []string{"create_issue", "add_comment"}
	jsonStr, err := GetValidationConfigJSONWithDataSchema(enabledTypes, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]TypeValidationConfig
	err = json.Unmarshal([]byte(jsonStr), &parsed)
	if err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	// Verify only enabled types are present
	if len(parsed) != 2 {
		t.Errorf("Expected 2 types, got %d", len(parsed))
	}

	if _, ok := parsed["create_issue"]; !ok {
		t.Error("Expected create_issue to be present")
	}
	if _, ok := parsed["add_comment"]; !ok {
		t.Error("Expected add_comment to be present")
	}

	// Verify other types are NOT present
	if _, ok := parsed["create_discussion"]; ok {
		t.Error("Did not expect create_discussion to be present")
	}
}

func TestGetValidationConfigJSONEmpty(t *testing.T) {
	// Test with empty slice (should return all types, same as nil)
	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	var parsed map[string]TypeValidationConfig
	err = json.Unmarshal([]byte(jsonStr), &parsed)
	if err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	// Empty slice should return all types
	if len(parsed) != len(ValidationConfig) {
		t.Errorf("Expected %d types with empty slice, got %d", len(ValidationConfig), len(parsed))
	}
}

func TestGetValidationConfigJSONWithMentions(t *testing.T) {
	mentions := map[string]any{
		"enabled":              true,
		"allowedCollaborators": false,
		"allowContext":         false,
		"allowed":              []string{"copilot", "github-actions"},
		"max":                  5,
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"add_comment"}, mentions, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	if _, ok := parsed["add_comment"]; !ok {
		t.Error("Expected add_comment type entry to be present")
	}

	raw, ok := parsed["mentions"]
	if !ok {
		t.Fatal("Expected top-level mentions key to be present in validation JSON")
	}
	mentionsParsed, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("Expected mentions to be an object, got %T", raw)
	}

	allowed, ok := mentionsParsed["allowed"].([]any)
	if !ok {
		t.Fatalf("Expected mentions.allowed to be an array, got %T", mentionsParsed["allowed"])
	}
	if len(allowed) != 2 || allowed[0] != "copilot" || allowed[1] != "github-actions" {
		t.Errorf("Unexpected mentions.allowed contents: %v", allowed)
	}
	if enabled, _ := mentionsParsed["enabled"].(bool); !enabled {
		t.Errorf("Expected mentions.enabled to be true, got %v", mentionsParsed["enabled"])
	}
	if allowTeam, _ := mentionsParsed["allowedCollaborators"].(bool); allowTeam {
		t.Errorf("Expected mentions.allowedCollaborators to be false, got %v", mentionsParsed["allowedCollaborators"])
	}

	// A second call without mentions must not include the key (cache safety).
	plainJSON, err := GetValidationConfigJSONWithDataSchema([]string{"add_comment"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}
	var plainParsed map[string]any
	if err := json.Unmarshal([]byte(plainJSON), &plainParsed); err != nil {
		t.Fatalf("Failed to parse plain validation config JSON: %v", err)
	}
	if _, ok := plainParsed["mentions"]; ok {
		t.Error("Did not expect mentions key in JSON produced without mentions argument")
	}
}

func TestGetValidationConfigJSONWithDataSchema(t *testing.T) {
	dataSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}

	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"add_comment", "close_issue"}, nil, true, dataSchema)
	if err != nil {
		t.Fatalf("GetValidationConfigJSONWithDataSchema() error = %v", err)
	}

	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	if parsed["add_comment"].DataSchema == nil {
		t.Fatal("expected add_comment dataSchema to be present")
	}
	if !parsed["add_comment"].DataEnabled {
		t.Fatal("expected add_comment dataEnabled to be true")
	}
	if parsed["close_issue"].DataSchema != nil {
		t.Fatal("did not expect close_issue dataSchema to be present")
	}
	if parsed["close_issue"].DataEnabled {
		t.Fatal("did not expect close_issue dataEnabled to be true")
	}
}

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' {
			return true
		}
	}
	return false
}

func TestFieldValidationMarshaling(t *testing.T) {
	// Test that FieldValidation marshals correctly with omitempty
	field := FieldValidation{
		Required:  true,
		Type:      "string",
		MaxLength: 128,
		Sanitize:  true,
	}

	data, err := json.Marshal(field)
	if err != nil {
		t.Fatalf("Failed to marshal FieldValidation: %v", err)
	}

	// Verify omitempty works - should not include false/zero values
	jsonStr := string(data)
	if jsonStr == "" {
		t.Error("Empty JSON output")
	}

	// Parse it back
	var parsed FieldValidation
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal FieldValidation: %v", err)
	}

	if parsed.Required != field.Required {
		t.Errorf("Required mismatch: got %v, want %v", parsed.Required, field.Required)
	}
	if parsed.Type != field.Type {
		t.Errorf("Type mismatch: got %v, want %v", parsed.Type, field.Type)
	}
	if parsed.MaxLength != field.MaxLength {
		t.Errorf("MaxLength mismatch: got %v, want %v", parsed.MaxLength, field.MaxLength)
	}
}

func TestIssueIntentRationaleMaxLength(t *testing.T) {
	for _, typeName := range []string{"set_issue_type", "set_issue_field", "close_issue", "assign_to_user", "assign_to_agent"} {
		if got := ValidationConfig[typeName].Fields["rationale"].MaxLength; got != 280 {
			t.Fatalf("%s rationale maxLength = %d, want 280", typeName, got)
		}
	}
}

func TestUpdateDiscussionValidationConfig(t *testing.T) {
	// Verify update_discussion accepts label-only updates (regression test for
	// https://github.com/github/gh-aw/issues/24979 where label-only updates were
	// rejected with "requires at least one of: 'title', 'body' fields").
	config, ok := ValidationConfig["update_discussion"]
	if !ok {
		t.Fatal("update_discussion not found in ValidationConfig")
	}

	// customValidation must include labels so label-only messages pass
	if config.CustomValidation != "requiresOneOf:title,body,labels" {
		t.Errorf("update_discussion customValidation = %q, want %q", config.CustomValidation, "requiresOneOf:title,body,labels")
	}

	// labels field must be defined so label values are validated
	if _, ok := config.Fields["labels"]; !ok {
		t.Error("update_discussion Fields is missing the 'labels' field")
	}
}

func TestUpdatePullRequestValidationConfig(t *testing.T) {
	config, ok := ValidationConfig["update_pull_request"]
	if !ok {
		t.Fatal("update_pull_request not found in ValidationConfig")
	}

	if config.CustomValidation != "requiresOneOf:title,body,update_branch" {
		t.Errorf("update_pull_request customValidation = %q, want %q", config.CustomValidation, "requiresOneOf:title,body,update_branch")
	}

	if _, ok := config.Fields["update_branch"]; !ok {
		t.Error("update_pull_request Fields is missing the 'update_branch' field")
	}
}

func TestUpdateProjectAcceptsProjectURLOrTemporaryID(t *testing.T) {
	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"update_project"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	project := parsed["update_project"].Fields["project"]
	for _, expected := range []string{
		"https://[^/]+/(orgs|users)/[^/]+/projects/\\d+",
		"#?aw_[A-Za-z0-9_]{3,12}",
	} {
		if !strings.Contains(project.Pattern, expected) {
			t.Fatalf("update_project.project pattern %q does not contain %q", project.Pattern, expected)
		}
	}
}

func TestUpdateIssueValidationConfig(t *testing.T) {
	config, ok := ValidationConfig["update_issue"]
	if !ok {
		t.Fatal("update_issue not found in ValidationConfig")
	}

	if config.CustomValidation != "requiresOneOf:status,title,body,labels,assignees,milestone" {
		t.Errorf("update_issue customValidation = %q, want %q", config.CustomValidation, "requiresOneOf:status,title,body,labels,assignees,milestone")
	}

	if _, ok := config.Fields["labels"]; !ok {
		t.Error("update_issue Fields is missing the 'labels' field")
	}
}

func TestIssueIntentValidationFields(t *testing.T) {
	for _, typeName := range []string{"set_issue_type", "set_issue_field", "close_issue", "assign_to_user", "assign_to_agent"} {
		config, ok := ValidationConfig[typeName]
		if !ok {
			t.Fatalf("%s not found in ValidationConfig", typeName)
		}

		if _, ok := config.Fields["rationale"]; !ok {
			t.Fatalf("%s Fields is missing 'rationale'", typeName)
		}
		if _, ok := config.Fields["confidence"]; !ok {
			t.Fatalf("%s Fields is missing 'confidence'", typeName)
		}
		if _, ok := config.Fields["suggest"]; !ok {
			t.Fatalf("%s Fields is missing 'suggest'", typeName)
		}

		confidence := config.Fields["confidence"]
		if len(confidence.Enum) != 3 || confidence.Enum[0] != "LOW" || confidence.Enum[1] != "MEDIUM" || confidence.Enum[2] != "HIGH" {
			t.Fatalf("%s confidence enum = %v, want [LOW MEDIUM HIGH]", typeName, confidence.Enum)
		}
		if !confidence.StripOnError {
			t.Fatalf("%s confidence StripOnError = false, want true", typeName)
		}

		rationale := config.Fields["rationale"]
		if !rationale.StripOnError {
			t.Fatalf("%s rationale StripOnError = false, want true", typeName)
		}
	}
}

func TestIssueIntentLabelValidationFields(t *testing.T) {
	for _, typeName := range []string{"add_labels", "remove_labels", "update_issue"} {
		config, ok := ValidationConfig[typeName]
		if !ok {
			t.Fatalf("%s not found in ValidationConfig", typeName)
		}

		labels, ok := config.Fields["labels"]
		if !ok {
			t.Fatalf("%s Fields is missing 'labels'", typeName)
		}
		if labels.Type != "array" {
			t.Fatalf("%s labels type = %q, want %q", typeName, labels.Type, "array")
		}
	}
}

func TestStripOnErrorOnlyOnOptionalFields(t *testing.T) {
	for typeName, config := range ValidationConfig {
		for fieldName, field := range config.Fields {
			if field.StripOnError && field.Required {
				t.Fatalf("%s.%s cannot set both StripOnError and Required", typeName, fieldName)
			}
		}
	}
}

func TestValidationConfigConsistency(t *testing.T) {
	// Verify that all types with customValidation have valid validation rules
	validCustomValidations := map[string]bool{
		"requiresOneOf:status,title,body,labels,assignees,milestone":            true,
		"requiresOneOf:summary,description":                                     true,
		"requiresOneOf:title,body":                                              true,
		"requiresOneOf:title,body,update_branch":                                true,
		"requiresOneOf:title,body,labels":                                       true,
		"requiresOneOf:issue_number,pull_number":                                true,
		"requiresOneOf:milestone_number,milestone_title":                        true,
		"requiresOneOf:field_name,field_node_id":                                true,
		"requiresOneOf:reviewers,team_reviewers":                                true,
		"requiresOneOf:title,body,state,area_path,iteration_path,assignee,tags": true,
		"startLineLessOrEqualLine":                                              true,
		"parentAndSubDifferent":                                                 true,
	}

	for typeName, config := range ValidationConfig {
		if config.CustomValidation != "" {
			if !validCustomValidations[config.CustomValidation] {
				t.Errorf("Type %q has unknown customValidation: %q", typeName, config.CustomValidation)
			}
		}

		// Verify all types have at least one field
		if len(config.Fields) == 0 {
			t.Errorf("Type %q has no fields defined", typeName)
		}

		// Verify defaultMax is positive
		if config.DefaultMax <= 0 {
			t.Errorf("Type %q has invalid defaultMax: %d", typeName, config.DefaultMax)
		}
	}
}

func TestValidationConfigCoversToolInputSchemas(t *testing.T) {
	var tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools); err != nil {
		t.Fatalf("failed to parse safe outputs tool schema: %v", err)
	}

	metadataFields := map[string]bool{"secrecy": true, "integrity": true}
	for _, tool := range tools {
		typeName := strings.ReplaceAll(tool.Name, "-", "_")
		config, ok := ValidationConfig[typeName]
		if !ok {
			t.Errorf("%s tool is missing from ValidationConfig", tool.Name)
			continue
		}
		for fieldName := range tool.InputSchema.Properties {
			if metadataFields[fieldName] {
				continue
			}
			if _, ok := config.Fields[fieldName]; !ok {
				t.Errorf("%s tool input %q is missing from ValidationConfig", tool.Name, fieldName)
			}
		}
	}
}

func TestCreateDiscussionBodyMinLength(t *testing.T) {
	config, ok := ValidationConfig["create_discussion"]
	if !ok {
		t.Fatal("create_discussion not found in ValidationConfig")
	}

	bodyField, ok := config.Fields["body"]
	if !ok {
		t.Fatal("body field not found in create_discussion validation config")
	}

	if bodyField.MinLength != MinDiscussionBodyLength {
		t.Errorf("create_discussion body MinLength = %d, want %d", bodyField.MinLength, MinDiscussionBodyLength)
	}
}

func TestCreateIssueBodyMinLength(t *testing.T) {
	config, ok := ValidationConfig["create_issue"]
	if !ok {
		t.Fatal("create_issue not found in ValidationConfig")
	}

	bodyField, ok := config.Fields["body"]
	if !ok {
		t.Fatal("body field not found in create_issue validation config")
	}

	if bodyField.MinLength != MinIssueBodyLength {
		t.Errorf("create_issue body MinLength = %d, want %d", bodyField.MinLength, MinIssueBodyLength)
	}
}

func TestFieldValidationMinLengthMarshaling(t *testing.T) {
	field := FieldValidation{
		Required:  true,
		Type:      "string",
		MaxLength: 65000,
		MinLength: 64,
		Sanitize:  true,
	}

	data, err := json.Marshal(field)
	if err != nil {
		t.Fatalf("Failed to marshal FieldValidation with MinLength: %v", err)
	}

	var parsed FieldValidation
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal FieldValidation with MinLength: %v", err)
	}

	if parsed.MinLength != field.MinLength {
		t.Errorf("MinLength mismatch: got %v, want %v", parsed.MinLength, field.MinLength)
	}
}

func TestCreatePullRequestBaseValidationMaxLength(t *testing.T) {
	config, ok := ValidationConfig["create_pull_request"]
	if !ok {
		t.Fatal("create_pull_request not found in ValidationConfig")
	}

	baseField, ok := config.Fields["base"]
	if !ok {
		t.Fatal("base field not found in create_pull_request validation config")
	}

	if baseField.MaxLength != 128 {
		t.Errorf("base field MaxLength = %d, want 128", baseField.MaxLength)
	}
}

func TestAssignMilestoneValidationConfig(t *testing.T) {
	config, ok := ValidationConfig["assign_milestone"]
	if !ok {
		t.Fatal("assign_milestone not found in ValidationConfig")
	}

	if config.CustomValidation != "requiresOneOf:milestone_number,milestone_title" {
		t.Errorf("assign_milestone customValidation = %q, want %q", config.CustomValidation, "requiresOneOf:milestone_number,milestone_title")
	}

	if _, ok := config.Fields["milestone_number"]; !ok {
		t.Error("assign_milestone Fields is missing the 'milestone_number' field")
	}
	if _, ok := config.Fields["milestone_title"]; !ok {
		t.Error("assign_milestone Fields is missing the 'milestone_title' field")
	}
}

func TestAssignMilestoneValidationConfigJSON(t *testing.T) {
	jsonStr, err := GetValidationConfigJSONWithDataSchema([]string{"assign_milestone"}, nil, false, nil)
	if err != nil {
		t.Fatalf("GetValidationConfigJSON() error = %v", err)
	}

	var parsed map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse validation config JSON: %v", err)
	}

	cfg, ok := parsed["assign_milestone"]
	if !ok {
		t.Fatal("assign_milestone not found in serialized validation config")
	}
	if cfg.CustomValidation != "requiresOneOf:milestone_number,milestone_title" {
		t.Errorf("assign_milestone customValidation in JSON = %q, want %q", cfg.CustomValidation, "requiresOneOf:milestone_number,milestone_title")
	}
}

func TestBuildValidationConfigCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"nil input", nil, ""},
		{"empty input", []string{}, ""},
		{"single type", []string{"comment"}, "comment"},
		{"already sorted", []string{"comment", "issue"}, "comment,issue"},
		{"reverse order produces same key", []string{"issue", "comment"}, "comment,issue"},
		{"multiple types sorted", []string{"z_type", "a_type", "m_type"}, "a_type,m_type,z_type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildValidationConfigCacheKey(tc.input)
			if got != tc.expected {
				t.Errorf("buildValidationConfigCacheKey(%v) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
