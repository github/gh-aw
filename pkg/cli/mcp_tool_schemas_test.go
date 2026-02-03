//go:build !integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/sliceutil"
)

// TestMCPToolOutputSchemas verifies that output schemas are correctly generated for MCP tools
func TestMCPToolOutputSchemas(t *testing.T) {
	t.Run("logs schema can be generated (for future use)", func(t *testing.T) {
		// The logs tool currently doesn't use output schemas, but we verify
		// the helper can generate them for when they're needed in the future
		schema, err := GenerateOutputSchema[LogsData]()
		if err != nil {
			t.Fatalf("Failed to generate schema for LogsData: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected non-nil schema for LogsData")
		}

		// Check that it's an object schema
		if schema.Type != "object" {
			t.Errorf("Expected schema type 'object', got '%s'", schema.Type)
		}

		// Check that it has the expected properties
		expectedProps := []string{"summary", "runs", "logs_location"}
		for _, prop := range expectedProps {
			if _, ok := schema.Properties[prop]; !ok {
				t.Errorf("Expected property '%s' in logs schema", prop)
			}
		}

		// Verify it can be marshaled to JSON (for MCP transport)
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("Failed to marshal logs schema to JSON: %v", err)
		}

		if len(schemaJSON) == 0 {
			t.Error("Expected non-empty JSON schema")
		}

		t.Logf("Logs schema JSON length: %d bytes (ready for future use)", len(schemaJSON))
	})

	t.Run("audit schema can be generated (for future use)", func(t *testing.T) {
		// The audit tool currently doesn't use output schemas (output can be filtered with jq),
		// but we verify the helper can generate them for when they're needed in the future
		schema, err := GenerateOutputSchema[AuditData]()
		if err != nil {
			t.Fatalf("Failed to generate schema for AuditData: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected non-nil schema for AuditData")
		}

		// Check that it's an object schema
		if schema.Type != "object" {
			t.Errorf("Expected schema type 'object', got '%s'", schema.Type)
		}

		// Check that it has the expected properties
		expectedProps := []string{"overview", "metrics", "downloaded_files"}
		for _, prop := range expectedProps {
			if _, ok := schema.Properties[prop]; !ok {
				t.Errorf("Expected property '%s' in audit schema", prop)
			}
		}

		// Verify it can be marshaled to JSON (for MCP transport)
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("Failed to marshal audit schema to JSON: %v", err)
		}

		if len(schemaJSON) == 0 {
			t.Error("Expected non-empty JSON schema")
		}

		t.Logf("Audit schema JSON length: %d bytes (ready for future use)", len(schemaJSON))
	})

	t.Run("status tool array schema can be generated", func(t *testing.T) {
		// Even though status tool doesn't use the schema (MCP requires objects),
		// verify the helper can generate a schema for the array type
		schema, err := GenerateOutputSchema[[]WorkflowStatus]()
		if err != nil {
			t.Fatalf("Failed to generate schema for []WorkflowStatus: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected non-nil schema for []WorkflowStatus")
		}

		// This will be an array schema
		// In v0.4.0+, nullable arrays use Types []string with ["null", "array"]
		// instead of Type string with "array"
		isArray := schema.Type == "array" || sliceutil.Contains(schema.Types, "array")
		if !isArray {
			t.Errorf("Expected schema to be an array type, got Type='%s', Types=%v", schema.Type, schema.Types)
		}

		t.Log("Note: Status tool cannot use this schema in MCP because output schemas must be objects")
	})
}
