//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestGenerateOutputSchema(t *testing.T) {
	t.Run("generates schema for simple struct", func(t *testing.T) {
		type SimpleOutput struct {
			Name  string `json:"name" jsonschema:"Name of the item"`
			Count int    `json:"count" jsonschema:"Number of items"`
		}

		schema, err := GenerateOutputSchema[SimpleOutput]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that schema has type object
		if schema.Type != "object" {
			t.Errorf("Expected schema type to be 'object', got '%s'", schema.Type)
		}

		// Check that properties are defined
		if schema.Properties == nil {
			t.Fatal("Expected schema properties to be defined")
		}

		// Check that name property exists
		if _, ok := schema.Properties["name"]; !ok {
			t.Error("Expected 'name' property to be defined")
		}

		// Check that count property exists
		if _, ok := schema.Properties["count"]; !ok {
			t.Error("Expected 'count' property to be defined")
		}
	})

	t.Run("generates schema for struct with optional fields", func(t *testing.T) {
		type OutputWithOptional struct {
			Required string  `json:"required" jsonschema:"Required field"`
			Optional *string `json:"optional,omitempty" jsonschema:"Optional field"`
		}

		schema, err := GenerateOutputSchema[OutputWithOptional]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that properties are defined
		if schema.Properties == nil {
			t.Fatal("Expected schema properties to be defined")
		}

		// Check that both fields exist
		if _, ok := schema.Properties["required"]; !ok {
			t.Error("Expected 'required' property to be defined")
		}
		if _, ok := schema.Properties["optional"]; !ok {
			t.Error("Expected 'optional' property to be defined")
		}
	})

	t.Run("generates schema for nested struct", func(t *testing.T) {
		type NestedData struct {
			Value int `json:"value" jsonschema:"Nested value"`
		}

		type OutputWithNested struct {
			Name   string     `json:"name" jsonschema:"Name"`
			Nested NestedData `json:"nested" jsonschema:"Nested data"`
		}

		schema, err := GenerateOutputSchema[OutputWithNested]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that nested property exists
		nestedProp, ok := schema.Properties["nested"]
		if !ok {
			t.Fatal("Expected 'nested' property to be defined")
		}

		// Check that nested property has object type
		if nestedProp.Type != "object" {
			t.Errorf("Expected nested type to be 'object', got '%s'", nestedProp.Type)
		}

		// Check that nested properties are defined
		if nestedProp.Properties == nil {
			t.Fatal("Expected nested properties to be defined")
		}

		if _, ok := nestedProp.Properties["value"]; !ok {
			t.Error("Expected nested 'value' property to be defined")
		}
	})

	t.Run("generates schema for slice field", func(t *testing.T) {
		type OutputWithSlice struct {
			Items []string `json:"items" jsonschema:"List of items"`
		}

		schema, err := GenerateOutputSchema[OutputWithSlice]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that items property exists
		itemsProp, ok := schema.Properties["items"]
		if !ok {
			t.Fatal("Expected 'items' property to be defined")
		}

		// Check that items is an array type
		// In v0.4.0+, nullable slices use Types []string with ["null", "array"]
		// instead of Type string with "array"
		isArray := itemsProp.Type == "array" || sliceutil.Contains(itemsProp.Types, "array")
		if !isArray {
			t.Errorf("Expected items to be an array type, got Type='%s', Types=%v", itemsProp.Type, itemsProp.Types)
		}

		// Check that items has an items schema
		if itemsProp.Items == nil {
			t.Fatal("Expected items to have an items schema")
		}

		// Check that the items schema is for strings
		if itemsProp.Items.Type != "string" {
			t.Errorf("Expected items schema type to be 'string', got '%s'", itemsProp.Items.Type)
		}
	})

	t.Run("generates schema for WorkflowStatus", func(t *testing.T) {
		schema, err := GenerateOutputSchema[WorkflowStatus]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed for WorkflowStatus: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that all expected properties exist
		expectedProps := []string{"workflow", "engine_id", "compiled", "status", "time_remaining"}
		for _, prop := range expectedProps {
			if _, ok := schema.Properties[prop]; !ok {
				t.Errorf("Expected '%s' property to be defined", prop)
			}
		}
	})

	t.Run("generates schema for LogsData", func(t *testing.T) {
		schema, err := GenerateOutputSchema[LogsData]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed for LogsData: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that expected top-level properties exist
		expectedProps := []string{"summary", "runs", "logs_location"}
		for _, prop := range expectedProps {
			if _, ok := schema.Properties[prop]; !ok {
				t.Errorf("Expected '%s' property to be defined", prop)
			}
		}
	})

	t.Run("generates schema for AuditData", func(t *testing.T) {
		schema, err := GenerateOutputSchema[AuditData]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed for AuditData: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Check that expected top-level properties exist
		expectedProps := []string{"overview", "metrics", "downloaded_files"}
		for _, prop := range expectedProps {
			if _, ok := schema.Properties[prop]; !ok {
				t.Errorf("Expected '%s' property to be defined", prop)
			}
		}
	})
}

func TestAddSchemaDefault(t *testing.T) {
	t.Run("adds default to existing property", func(t *testing.T) {
		type TestStruct struct {
			Name  string `json:"name" jsonschema:"Name field"`
			Count int    `json:"count" jsonschema:"Count field"`
		}

		schema, err := GenerateOutputSchema[TestStruct]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		// Add defaults
		if err := AddSchemaDefault(schema, "name", "default_name"); err != nil {
			t.Fatalf("AddSchemaDefault failed: %v", err)
		}
		if err := AddSchemaDefault(schema, "count", 42); err != nil {
			t.Fatalf("AddSchemaDefault failed: %v", err)
		}

		// Verify defaults were added
		nameProp := schema.Properties["name"]
		if len(nameProp.Default) == 0 {
			t.Error("Expected name property to have a default")
		}
		var nameDefault string
		if err := json.Unmarshal(nameProp.Default, &nameDefault); err != nil {
			t.Errorf("Failed to unmarshal name default: %v", err)
		} else if nameDefault != "default_name" {
			t.Errorf("Expected name default to be 'default_name', got %v", nameDefault)
		}

		countProp := schema.Properties["count"]
		if len(countProp.Default) == 0 {
			t.Error("Expected count property to have a default")
		}
		var countDefault int
		if err := json.Unmarshal(countProp.Default, &countDefault); err != nil {
			t.Errorf("Failed to unmarshal count default: %v", err)
		} else if countDefault != 42 {
			t.Errorf("Expected count default to be 42, got %v", countDefault)
		}
	})

	t.Run("handles non-existent property gracefully", func(t *testing.T) {
		type TestStruct struct {
			Name string `json:"name" jsonschema:"Name field"`
		}

		schema, err := GenerateOutputSchema[TestStruct]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		// Try to add default to non-existent property - should not error
		if err := AddSchemaDefault(schema, "nonexistent", "value"); err != nil {
			t.Errorf("AddSchemaDefault should not error on non-existent property: %v", err)
		}
	})

	t.Run("handles nil schema gracefully", func(t *testing.T) {
		// Should not panic or error
		if err := AddSchemaDefault(nil, "field", "value"); err != nil {
			t.Errorf("AddSchemaDefault should not error on nil schema: %v", err)
		}
	})
}

func TestGenerateOutputSchemaWithDefaults(t *testing.T) {
	t.Run("manually adds default values to schema", func(t *testing.T) {
		type OutputWithDefaults struct {
			Name    string `json:"name" jsonschema:"Name of the item"`
			Count   int    `json:"count" jsonschema:"Number of items"`
			Enabled bool   `json:"enabled" jsonschema:"Whether enabled"`
		}

		schema, err := GenerateOutputSchema[OutputWithDefaults]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		if schema == nil {
			t.Fatal("Expected schema to be non-nil")
		}

		// Manually add default values
		if nameProp, ok := schema.Properties["name"]; ok {
			nameProp.Default = json.RawMessage(`"test"`)
		}
		if countProp, ok := schema.Properties["count"]; ok {
			countProp.Default = json.RawMessage(`100`)
		}
		if enabledProp, ok := schema.Properties["enabled"]; ok {
			enabledProp.Default = json.RawMessage(`true`)
		}

		// Check name property has default
		nameProp, ok := schema.Properties["name"]
		if !ok {
			t.Fatal("Expected 'name' property to be defined")
		}
		if len(nameProp.Default) == 0 {
			t.Error("Expected 'name' property to have a default value")
		} else {
			var nameDefault string
			if err := json.Unmarshal(nameProp.Default, &nameDefault); err != nil {
				t.Errorf("Failed to unmarshal name default: %v", err)
			} else if nameDefault != "test" {
				t.Errorf("Expected name default to be 'test', got %v", nameDefault)
			}
		}

		// Check count property has default
		countProp, ok := schema.Properties["count"]
		if !ok {
			t.Fatal("Expected 'count' property to be defined")
		}
		if len(countProp.Default) == 0 {
			t.Error("Expected 'count' property to have a default value")
		} else {
			var countDefault int
			if err := json.Unmarshal(countProp.Default, &countDefault); err != nil {
				t.Errorf("Failed to unmarshal count default: %v", err)
			} else if countDefault != 100 {
				t.Errorf("Expected count default to be 100, got %v", countDefault)
			}
		}

		// Check enabled property has default
		enabledProp, ok := schema.Properties["enabled"]
		if !ok {
			t.Fatal("Expected 'enabled' property to be defined")
		}
		if len(enabledProp.Default) == 0 {
			t.Error("Expected 'enabled' property to have a default value")
		} else {
			var enabledDefault bool
			if err := json.Unmarshal(enabledProp.Default, &enabledDefault); err != nil {
				t.Errorf("Failed to unmarshal enabled default: %v", err)
			} else if !enabledDefault {
				t.Errorf("Expected enabled default to be true, got %v", enabledDefault)
			}
		}
	})
}

func TestGeneratedSchemasValidateRealOutput(t *testing.T) {
	t.Run("validates LogsData schema against real data", func(t *testing.T) {
		// Generate schema for LogsData
		schema, err := GenerateOutputSchema[LogsData]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		// Resolve the schema to prepare it for validation
		resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
		if err != nil {
			t.Fatalf("Schema.Resolve failed: %v", err)
		}

		// Create realistic test data
		data := LogsData{
			Summary: LogsSummary{
				TotalRuns:     5,
				TotalDuration: "10m30s",
				TotalTokens:   15000,
				TotalCost:     0.45,
				TotalTurns:    25,
			},
			Runs: []RunData{
				{
					DatabaseID:    123456,
					Number:        1,
					WorkflowName:  "test-workflow",
					Agent:         "copilot",
					Status:        "completed",
					Conclusion:    "success",
					Duration:      "2m5s",
					TokenUsage:    3000,
					EstimatedCost: 0.09,
					Turns:         5,
				},
			},
			LogsLocation: "/path/to/logs",
		}

		// Marshal to JSON and then to map[string]any for validation
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var jsonValue map[string]any
		if err := json.Unmarshal(jsonBytes, &jsonValue); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Validate the data against the schema
		err = resolved.Validate(jsonValue)
		if err != nil {
			t.Errorf("Schema should validate real LogsData output: %v", err)
		}
	})

	t.Run("validates AuditData schema against real data", func(t *testing.T) {
		// Generate schema for AuditData
		schema, err := GenerateOutputSchema[AuditData]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		// Resolve the schema to prepare it for validation
		resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
		if err != nil {
			t.Fatalf("Schema.Resolve failed: %v", err)
		}

		// Create realistic test data
		data := AuditData{
			Overview: OverviewData{
				RunID:        789012,
				WorkflowName: "audit-workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Now(),
			},
			Metrics: MetricsData{
				TokenUsage:    5000,
				EstimatedCost: 0.15,
				Turns:         10,
				ErrorCount:    0,
				WarningCount:  2,
			},
			DownloadedFiles: []FileInfo{
				{
					Path:        "/tmp/test.log",
					Size:        1024,
					Description: "Test log file",
				},
			},
		}

		// Marshal to JSON and then to map[string]any for validation
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var jsonValue map[string]any
		if err := json.Unmarshal(jsonBytes, &jsonValue); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Validate the data against the schema
		err = resolved.Validate(jsonValue)
		if err != nil {
			t.Errorf("Schema should validate real AuditData output: %v", err)
		}
	})

	t.Run("validates WorkflowStatus schema against real data", func(t *testing.T) {
		// Generate schema for WorkflowStatus
		schema, err := GenerateOutputSchema[WorkflowStatus]()
		if err != nil {
			t.Fatalf("GenerateOutputSchema failed: %v", err)
		}

		// Resolve the schema to prepare it for validation
		resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
		if err != nil {
			t.Fatalf("Schema.Resolve failed: %v", err)
		}

		// Create realistic test data
		data := WorkflowStatus{
			Workflow:      "status-workflow",
			EngineID:      "copilot",
			Compiled:      "true",
			Status:        "active",
			TimeRemaining: "5m30s",
			Labels:        []string{"production", "automated"},
			On:            "push",
			RunStatus:     "completed",
			RunConclusion: "success",
		}

		// Marshal to JSON and then to map[string]any for validation
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var jsonValue map[string]any
		if err := json.Unmarshal(jsonBytes, &jsonValue); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Validate the data against the schema
		err = resolved.Validate(jsonValue)
		if err != nil {
			t.Errorf("Schema should validate real WorkflowStatus output: %v", err)
		}
	})
}
