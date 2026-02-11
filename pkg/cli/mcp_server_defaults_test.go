//go:build !integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestMCPToolElicitationDefaults verifies that MCP tools have appropriate
// elicitation defaults configured according to SEP-1024.
func TestMCPToolElicitationDefaults(t *testing.T) {
	t.Run("compile tool has strict default", func(t *testing.T) {
		type compileArgs struct {
			Workflows  []string `json:"workflows,omitempty" jsonschema:"Workflow files to compile (empty for all)"`
			Strict     bool     `json:"strict,omitempty" jsonschema:"Override frontmatter to enforce strict mode validation for all workflows"`
			Zizmor     bool     `json:"zizmor,omitempty" jsonschema:"Run zizmor security scanner on generated .lock.yml files"`
			Poutine    bool     `json:"poutine,omitempty" jsonschema:"Run poutine security scanner on generated .lock.yml files"`
			Actionlint bool     `json:"actionlint,omitempty" jsonschema:"Run actionlint linter on generated .lock.yml files"`
			Fix        bool     `json:"fix,omitempty" jsonschema:"Apply automatic codemod fixes to workflows before compiling"`
		}

		schema, err := GenerateOutputSchema[compileArgs]()
		if err != nil {
			t.Fatalf("Failed to generate schema: %v", err)
		}

		// Add default as done in createMCPServer
		if err := AddSchemaDefault(schema, "strict", true); err != nil {
			t.Fatalf("Failed to add default: %v", err)
		}

		// Verify the default was added
		strictProp, ok := schema.Properties["strict"]
		if !ok {
			t.Fatal("Expected 'strict' property to exist")
		}

		if len(strictProp.Default) == 0 {
			t.Error("Expected 'strict' property to have a default value")
		}

		var strictDefault bool
		if err := json.Unmarshal(strictProp.Default, &strictDefault); err != nil {
			t.Fatalf("Failed to unmarshal strict default: %v", err)
		}

		if !strictDefault {
			t.Errorf("Expected strict default to be true, got %v", strictDefault)
		}
	})

	t.Run("logs tool has count, timeout, and max_tokens defaults", func(t *testing.T) {
		type logsArgs struct {
			WorkflowName string `json:"workflow_name,omitempty" jsonschema:"Name of the workflow to download logs for (empty for all)"`
			Count        int    `json:"count,omitempty" jsonschema:"Number of workflow runs to download"`
			Timeout      int    `json:"timeout,omitempty" jsonschema:"Maximum time in seconds to spend downloading logs"`
			MaxTokens    int    `json:"max_tokens,omitempty" jsonschema:"Maximum number of tokens in output before triggering guardrail"`
		}

		schema, err := GenerateOutputSchema[logsArgs]()
		if err != nil {
			t.Fatalf("Failed to generate schema: %v", err)
		}

		// Add defaults as done in createMCPServer
		if err := AddSchemaDefault(schema, "count", 100); err != nil {
			t.Fatalf("Failed to add count default: %v", err)
		}
		if err := AddSchemaDefault(schema, "timeout", 50); err != nil {
			t.Fatalf("Failed to add timeout default: %v", err)
		}
		if err := AddSchemaDefault(schema, "max_tokens", 12000); err != nil {
			t.Fatalf("Failed to add max_tokens default: %v", err)
		}

		// Verify count default
		countProp, ok := schema.Properties["count"]
		if !ok {
			t.Fatal("Expected 'count' property to exist")
		}
		if len(countProp.Default) == 0 {
			t.Error("Expected 'count' property to have a default value")
		}
		var countDefault int
		if err := json.Unmarshal(countProp.Default, &countDefault); err != nil {
			t.Fatalf("Failed to unmarshal count default: %v", err)
		}
		if countDefault != 100 {
			t.Errorf("Expected count default to be 100, got %v", countDefault)
		}

		// Verify timeout default
		timeoutProp, ok := schema.Properties["timeout"]
		if !ok {
			t.Fatal("Expected 'timeout' property to exist")
		}
		if len(timeoutProp.Default) == 0 {
			t.Error("Expected 'timeout' property to have a default value")
		}
		var timeoutDefault int
		if err := json.Unmarshal(timeoutProp.Default, &timeoutDefault); err != nil {
			t.Fatalf("Failed to unmarshal timeout default: %v", err)
		}
		if timeoutDefault != 50 {
			t.Errorf("Expected timeout default to be 50, got %v", timeoutDefault)
		}

		// Verify max_tokens default
		maxTokensProp, ok := schema.Properties["max_tokens"]
		if !ok {
			t.Fatal("Expected 'max_tokens' property to exist")
		}
		if len(maxTokensProp.Default) == 0 {
			t.Error("Expected 'max_tokens' property to have a default value")
		}
		var maxTokensDefault int
		if err := json.Unmarshal(maxTokensProp.Default, &maxTokensDefault); err != nil {
			t.Fatalf("Failed to unmarshal max_tokens default: %v", err)
		}
		if maxTokensDefault != 12000 {
			t.Errorf("Expected max_tokens default to be 12000, got %v", maxTokensDefault)
		}
	})

	t.Run("schema with defaults is valid JSON Schema", func(t *testing.T) {
		type testArgs struct {
			Name  string `json:"name" jsonschema:"Name field"`
			Count int    `json:"count" jsonschema:"Count field"`
		}

		schema, err := GenerateOutputSchema[testArgs]()
		if err != nil {
			t.Fatalf("Failed to generate schema: %v", err)
		}

		if err := AddSchemaDefault(schema, "count", 42); err != nil {
			t.Fatalf("Failed to add default: %v", err)
		}

		// Verify schema can be marshaled and is valid
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			t.Fatalf("Failed to marshal schema: %v", err)
		}

		// Verify it unmarshals back to a valid schema
		var unmarshaled jsonschema.Schema
		if err := json.Unmarshal(schemaBytes, &unmarshaled); err != nil {
			t.Fatalf("Failed to unmarshal schema: %v", err)
		}

		// Verify the default survived the round trip
		countProp := unmarshaled.Properties["count"]
		if len(countProp.Default) == 0 {
			t.Error("Default value was lost during marshal/unmarshal")
		}
	})
}
