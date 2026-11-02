package connectors

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Schema returns the structural JSON Schema for editor/tooling integration.
// LoadSpec/Lower additionally enforce the semantic security invariants, such as
// credential phase matching and referenced operation/resource existence.
func Schema(kind string) ([]byte, error) {
	var schema *jsonschema.Schema
	var err error
	options := &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[Value](): {Ref: "#/$defs/value"},
	}}
	switch kind {
	case "ConnectorSpec":
		schema, err = jsonschema.For[ConnectorSpec](options)
	case "ConnectorConnection":
		schema, err = jsonschema.For[Connection](nil)
	case "ConnectorBinding":
		schema, err = jsonschema.For[Binding](nil)
	default:
		return nil, fmt.Errorf("unknown connector schema kind %q", kind)
	}
	if err != nil {
		return nil, fmt.Errorf("generate connector schema: %w", err)
	}
	schema.Schema = "https://json-schema.org/draft/2020-12/schema"
	if kind == "ConnectorSpec" {
		value, err := valueSchema()
		if err != nil {
			return nil, err
		}
		schema.Defs = map[string]*jsonschema.Schema{"value": value}
	}
	schema.Properties["apiVersion"].Enum = []any{APIVersion}
	schema.Properties["kind"].Enum = []any{kind}
	return json.MarshalIndent(schema, "", "  ")
}

func valueSchema() (*jsonschema.Schema, error) {
	const definition = `{
	  "type": "object",
	  "additionalProperties": false,
	  "properties": {
	    "input": {"type": "string", "minLength": 1},
	    "literal": {"not": {"type": "null"}},
	    "object": {"type": "object", "additionalProperties": {"$ref": "#/$defs/value"}},
	    "array": {"type": "array", "items": {"$ref": "#/$defs/value"}},
	    "encoding": {"enum": ["jira-adf", "confluence-storage"]}
	  },
	  "oneOf": [
	    {"required": ["input"]},
	    {"required": ["literal"]},
	    {"required": ["object"]},
	    {"required": ["array"]}
	  ],
	  "dependentRequired": {"encoding": ["input"]}
	}`
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(definition), &schema); err != nil {
		return nil, fmt.Errorf("decode connector value schema: %w", err)
	}
	return &schema, nil
}
