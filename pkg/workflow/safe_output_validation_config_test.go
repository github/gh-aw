//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"
)

func TestGetValidationConfigJSON(t *testing.T) {
	// Test with nil (all types)
	jsonStr, err := GetValidationConfigJSON(nil)
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

func TestGetValidationConfigJSONFiltered(t *testing.T) {
	// Test with filtered types
	enabledTypes := []string{"create_issue", "add_comment"}
	jsonStr, err := GetValidationConfigJSON(enabledTypes)
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
	jsonStr, err := GetValidationConfigJSON([]string{})
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

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' {
			return true
		}
	}
	return false
}

func TestGetValidationConfigForType(t *testing.T) {
	tests := []struct {
		name       string
		typeName   string
		wantFound  bool
		wantMax    int
		wantFields []string
	}{
		{
			name:       "create_issue type",
			typeName:   "create_issue",
			wantFound:  true,
			wantMax:    1,
			wantFields: []string{"title", "body", "labels", "parent", "temporary_id"},
		},
		{
			name:       "link_sub_issue type",
			typeName:   "link_sub_issue",
			wantFound:  true,
			wantMax:    5,
			wantFields: []string{"parent_issue_number", "sub_issue_number"},
		},
		{
			name:      "unknown type",
			typeName:  "unknown_type",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, found := GetValidationConfigForType(tt.typeName)
			if found != tt.wantFound {
				t.Errorf("GetValidationConfigForType() found = %v, want %v", found, tt.wantFound)
			}
			if found {
				if config.DefaultMax != tt.wantMax {
					t.Errorf("DefaultMax = %v, want %v", config.DefaultMax, tt.wantMax)
				}
				for _, fieldName := range tt.wantFields {
					if _, ok := config.Fields[fieldName]; !ok {
						t.Errorf("Field %q not found in config", fieldName)
					}
				}
			}
		})
	}
}

func TestGetDefaultMaxForType(t *testing.T) {
	tests := []struct {
		typeName string
		want     int
	}{
		{"create_issue", 1},
		{"add_labels", 5},
		{"missing_tool", 20},
		{"create_code_scanning_alert", 40},
		{"link_sub_issue", 5},
		{"unknown_type", 1}, // Default fallback
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			got := GetDefaultMaxForType(tt.typeName)
			if got != tt.want {
				t.Errorf("GetDefaultMaxForType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
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

func TestValidationConfigConsistency(t *testing.T) {
	// Verify that all types with customValidation have valid validation rules
	validCustomValidations := map[string]bool{
		"requiresOneOf:status,title,body":        true,
		"requiresOneOf:title,body":               true,
		"requiresOneOf:issue_number,pull_number": true,
		"startLineLessOrEqualLine":               true,
		"parentAndSubDifferent":                  true,
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

func TestMissingToolFieldOptional(t *testing.T) {
	// Test that the 'tool' field in missing_tool is optional
	config, found := GetValidationConfigForType("missing_tool")
	if !found {
		t.Fatal("missing_tool config not found")
	}

	// Verify 'tool' field exists and is optional
	toolField, ok := config.Fields["tool"]
	if !ok {
		t.Fatal("tool field not found in missing_tool config")
	}

	if toolField.Required {
		t.Error("tool field should be optional (Required: false) to match the tool description")
	}

	// Verify 'reason' field is required
	reasonField, ok := config.Fields["reason"]
	if !ok {
		t.Fatal("reason field not found in missing_tool config")
	}

	if !reasonField.Required {
		t.Error("reason field should be required")
	}

	// Verify 'alternatives' field is optional (no Required field or Required: false)
	alternativesField, ok := config.Fields["alternatives"]
	if !ok {
		t.Fatal("alternatives field not found in missing_tool config")
	}

	if alternativesField.Required {
		t.Error("alternatives field should be optional")
	}
}
