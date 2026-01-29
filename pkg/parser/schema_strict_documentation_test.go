//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestStrictFieldSchemaDocumentation verifies that the strict field in the schema
// contains comprehensive documentation about all enforcement areas and CLI usage
func TestStrictFieldSchemaDocumentation(t *testing.T) {
	// Read the main workflow schema
	schemaPath := "schemas/main_workflow_schema.json"
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("Failed to read schema file: %v", err)
	}

	// Parse the schema
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("Failed to parse schema JSON: %v", err)
	}

	// Get the properties section
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Schema properties section not found or invalid")
	}

	// Get the strict field
	strictField, ok := properties["strict"].(map[string]interface{})
	if !ok {
		t.Fatal("Strict field not found in schema properties")
	}

	// Get the description
	description, ok := strictField["description"].(string)
	if !ok {
		t.Fatal("Strict field description not found or not a string")
	}

	// Verify that description contains key elements
	requiredElements := []string{
		"Write Permissions",
		"Network Configuration",
		"Action Pinning",
		"MCP Network",
		"Deprecated Fields",
		"gh aw compile --strict",
		"CLI flag takes precedence",
		"safe-outputs",
	}

	for _, element := range requiredElements {
		if !strings.Contains(description, element) {
			t.Errorf("Strict field description missing required element: %q\nDescription: %s", element, description)
		}
	}

	// Verify description contains documentation link
	if !strings.Contains(description, "https://") {
		t.Error("Strict field description should contain a documentation link")
	}

	// Verify type is boolean
	fieldType, ok := strictField["type"].(string)
	if !ok || fieldType != "boolean" {
		t.Errorf("Strict field type should be 'boolean', got: %v", fieldType)
	}

	t.Logf("✓ Strict field description is comprehensive (%d chars)", len(description))
}
