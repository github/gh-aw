package workqueue

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestIndependentFrozenResourceScopeSchema(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/resource-scope.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition any
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const location = "https://github.com/github/gh-aw/specs/work-queue/resource-scope.schema.json"
	if err := compiler.AddResource(location, definition); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("../../specs/work-queue/fixtures/resource-scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name        string          `json:"name"`
			Scope       json.RawMessage `json:"scope"`
			SchemaValid *bool           `json:"scope_schema_valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Cases {
		if len(test.Scope) == 0 {
			continue
		}
		t.Run(test.Name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal(test.Scope, &value); err != nil {
				t.Fatal(err)
			}
			valid := test.SchemaValid == nil || *test.SchemaValid
			if err := schema.Validate(value); (err == nil) != valid {
				t.Fatalf("independent scope schema expectation valid=%t: %v", valid, err)
			}
		})
	}
}
