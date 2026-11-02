package connectors

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// Schema returns the structural JSON Schema for editor/tooling integration.
// LoadSpec/Lower additionally enforce the semantic security invariants, such as
// credential phase matching and referenced operation/resource existence.
func Schema(kind string) ([]byte, error) {
	var schema *jsonschema.Schema
	var err error
	switch kind {
	case "ConnectorSpec":
		schema, err = jsonschema.For[ConnectorSpec](nil)
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
	schema.Properties["apiVersion"].Enum = []any{APIVersion}
	schema.Properties["kind"].Enum = []any{kind}
	return json.MarshalIndent(schema, "", "  ")
}
