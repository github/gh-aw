package workqueue

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestIndependentEmbeddedEffectContractSchemaFixtures(t *testing.T) {
	schemaData, err := os.ReadFile("../../specs/work-queue/effect-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition any
	if err := json.Unmarshal(schemaData, &definition); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const location = "https://github.com/github/gh-aw/specs/work-queue/effect-contract.schema.json"
	if err := compiler.AddResource(location, definition); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../specs/work-queue/fixtures/effect-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name        string          `json:"name"`
			Contract    json.RawMessage `json:"contract"`
			SchemaValid bool            `json:"schema_valid"`
			Valid       bool            `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 20 {
		t.Fatal("all independent embedded intent fixtures must be consumed")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]json.RawMessage{"effect_contract": test.Contract})
			if err != nil {
				t.Fatal(err)
			}
			if err := validateDeliveryContract(payload); (err == nil) != test.Valid {
				t.Fatalf("native contract gate differs from independent semantic expectation %t: %v", test.Valid, err)
			}
			var value any
			if err := json.Unmarshal(test.Contract, &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); (err == nil) != test.SchemaValid {
				t.Fatalf("embedded schema validity=%t: %v", test.SchemaValid, err)
			}
			if test.SchemaValid {
				var contract struct {
					Outputs []struct {
						Type string `json:"type"`
						Min  int    `json:"min"`
						Max  int    `json:"max"`
					} `json:"outputs"`
				}
				if err := json.Unmarshal(test.Contract, &contract); err != nil {
					t.Fatal(err)
				}
				types := map[string]bool{}
				valid := true
				for _, output := range contract.Outputs {
					if types[output.Type] || output.Min > output.Max {
						valid = false
					}
					types[output.Type] = true
				}
				if valid != test.Valid {
					t.Fatal("range/unique-type controls differ from independent semantic expectation")
				}
			} else if test.Valid {
				t.Fatal("an invalid structure cannot be a supported intent")
			}
		})
	}
}
