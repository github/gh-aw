package workqueue

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestStructuralRunAndObservationBounds(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"Actor", "Resource", "ClaimOperation"} {
		data, err := schemas.ReadFile("schema/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource(name+".json", schema); err != nil {
			t.Fatal(err)
		}
	}
	resource := func(id string) map[string]any {
		return map[string]any{
			"kind": "issue", "host": "github.com", "repository": "owner/repo",
			"repository_id": "1", "resource_id": id, "number": "7",
		}
	}
	actor := func(attempt int) map[string]any {
		return map[string]any{
			"role": "dispatcher", "principal": "1001", "repository": "owner/repo",
			"workflow": ".github/workflows/dispatcher.lock.yml",
			"run_id":   "202", "run_attempt": attempt,
		}
	}
	claim := func(count int, duplicate bool) map[string]any {
		references := make([]any, count)
		for i := range references {
			references[i] = fmt.Sprintf("observation-%d", i)
		}
		if duplicate {
			references[len(references)-1] = references[0]
		}
		return map[string]any{
			"kind": "Claim", "work_id": "work", "claim_id": "claim",
			"dispatch_id": "dispatch", "handle": "h1", "observations": references,
		}
	}
	tests := []struct {
		name   string
		schema string
		value  map[string]any
		valid  bool
	}{
		{"positive-id", "Resource", resource("1"), true},
		{"zero-id", "Resource", resource("0"), false},
		{"leading-zero-id", "Resource", resource("01"), false},
		{"maximum-decimal-id", "Resource", resource(strings.Repeat("9", 256)), true},
		{"over-bound-decimal-id", "Resource", resource(strings.Repeat("9", 257)), false},
		{"original-source-attempt", "Actor", actor(1), true},
		{"logical-source-rerun", "Actor", actor(2), true},
		{"maximum-source-attempt", "Actor", actor(4096), true},
		{"over-bound-source-attempt", "Actor", actor(4097), false},
		{"empty-observations", "ClaimOperation", claim(0, false), true},
		{"maximum-unique-observations", "ClaimOperation", claim(64, false), true},
		{"over-bound-observations", "ClaimOperation", claim(65, false), false},
		{"duplicate-observations", "ClaimOperation", claim(64, true), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := compiler.Compile(test.schema + ".json")
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(test.value); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
