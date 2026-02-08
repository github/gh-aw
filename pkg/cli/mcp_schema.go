package cli

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

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

// GenerateOutputSchema is deprecated. Use GenerateSchema instead.
// This function is kept for backward compatibility but will be removed in a future version.
func GenerateOutputSchema[T any]() (*jsonschema.Schema, error) {
	return GenerateSchema[T]()
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
		return nil
	}

	prop, ok := schema.Properties[propertyName]
	if !ok {
		return nil // Property doesn't exist, nothing to do
	}

	// Marshal the value to JSON
	defaultBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal default value for %s: %w", propertyName, err)
	}

	prop.Default = json.RawMessage(defaultBytes)
	return nil
}
