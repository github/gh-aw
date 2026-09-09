package cli

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/google/jsonschema-go/jsonschema"
)

var mcpSchemaLog = logger.New("cli:mcp_schema")

// GenerateSchema generates a JSON schema from a Go struct type.
// This is used for both MCP tool input parameters (InputSchema) and output data types.
// The schema conforms to JSON Schema draft 2020-12 and draft-07.
//
// Schema generation rules:
//   - json tags define property names
//   - jsonschema tags define descriptions
//   - omitempty/omitzero mark optional fields
//   - Pointer types include null in their type array
//   - Slices allow null values (jsonschema-go v0.4.0+)
//   - PropertyOrder maintains deterministic field ordering (v0.4.0+)
//
// MCP Requirements:
//   - Tool input/output schemas must be objects (not arrays or primitives)
//   - All properties should have descriptions for better LLM understanding
//   - Required vs optional fields must be correctly specified
//
// Example:
//
//	type MyArgs struct {
//	    Name string `json:"name" jsonschema:"Name of the user"`
//	    Age  int    `json:"age,omitempty" jsonschema:"Age in years"`
//	}
//	schema, err := GenerateSchema[MyArgs]()
func GenerateSchema[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](nil)
}

// GenerateOutputSchema generates a JSON schema for a structured command output type.
func GenerateOutputSchema[T any]() (*jsonschema.Schema, error) {
	return GenerateSchema[T]()
}

// MarshalOutputSchema serializes a generated output schema using the CLI's
// standard indented JSON format.
func MarshalOutputSchema(schema *jsonschema.Schema) ([]byte, error) {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal output schema to JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// GenerateNamedOutputSchema generates and serializes a known CLI output schema.
func GenerateNamedOutputSchema(name string) ([]byte, error) {
	var (
		schema *jsonschema.Schema
		err    error
	)
	switch name {
	case "audit":
		schema, err = generateAuditOutputSchema()
	case "logs":
		schema, err = generateLogsOutputSchema()
	default:
		return nil, fmt.Errorf("unsupported schema: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to generate %s schema: %w", name, err)
	}
	return MarshalOutputSchema(schema)
}

func generateAuditOutputSchema() (*jsonschema.Schema, error) {
	auditData, err := GenerateOutputSchema[AuditData]()
	if err != nil {
		return nil, err
	}
	auditDiff, err := GenerateOutputSchema[AuditDiff]()
	if err != nil {
		return nil, err
	}
	auditDiffs, err := GenerateOutputSchema[[]AuditDiff]()
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{auditData, auditDiff, auditDiffs}}, nil
}

func generateLogsOutputSchema() (*jsonschema.Schema, error) {
	logsData, err := GenerateOutputSchema[LogsData]()
	if err != nil {
		return nil, err
	}
	crossRun, err := GenerateOutputSchema[CrossRunAuditReport]()
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{logsData, crossRun}}, nil
}

func generateSchemaWithDefaults[T any](defaults map[string]any) (*jsonschema.Schema, error) {
	schema, err := GenerateSchema[T]()
	if err != nil {
		return nil, err
	}

	for propertyName, value := range defaults {
		if err := AddSchemaDefault(schema, propertyName, value); err != nil {
			mcpSchemaLog.Printf("Failed to add default for %s: %v", propertyName, err)
		}
	}

	return schema, nil
}

// AddSchemaDefault adds a default value to a property in a JSON schema.
// This is useful for elicitation defaults (SEP-1024) that improve UX by
// suggesting sensible starting values to MCP clients.
//
// The value must be JSON-marshallable and appropriate for the property type.
//
// Example:
//
//	schema, err := GenerateSchema[MyArgs]()
//	AddSchemaDefault(schema, "count", 100)          // number default
//	AddSchemaDefault(schema, "enabled", true)       // boolean default
//	AddSchemaDefault(schema, "name", "default")     // string default
func AddSchemaDefault(schema *jsonschema.Schema, propertyName string, value any) error {
	if schema == nil || schema.Properties == nil {
		// Defaults are meaningful only for object schemas with generated properties.
		return nil
	}

	prop, ok := schema.Properties[propertyName]
	if !ok {
		return fmt.Errorf("schema property %q not found", propertyName)
	}

	// Marshal the value to JSON
	defaultBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal default value for %s: %w", propertyName, err)
	}

	mcpSchemaLog.Printf("Setting default value for schema property: %s", propertyName)
	prop.Default = json.RawMessage(defaultBytes)
	return nil
}
