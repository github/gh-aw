package workqueue

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestWorkQueueStorageNames(t *testing.T) {
	if FileName != "work-queue.jsonl" || DefaultBranch != "gh-aw-work-queue" {
		t.Fatalf("unexpected work queue storage names: file=%q branch=%q", FileName, DefaultBranch)
	}
}

func TestWorkQueueWorkflowSchemas(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	entries, err := schemas.ReadDir("schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := schemas.ReadFile("schema/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		var resource any
		if err := json.Unmarshal(data, &resource); err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource(entry.Name(), resource); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		schema  string
		record  string
		invalid bool
	}{
		{"work", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null}`, false},
		{"work enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":123}`, false},
		{"zero enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":0}`, false},
		{"maximum enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":9007199254740991}`, false},
		{"negative enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":-1}`, true},
		{"unsafe enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":9007199254740992}`, true},
		{"fractional enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":0.5}`, true},
		{"string enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":"123"}`, true},
		{"null enqueue time", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":null}`, true},
		{"enqueue time only on work", "WorkQueueTransaction", `{"version":2,"kind":"Claim","work":"w","claim":"c","attempt":null,"enqueued":123}`, true},
		{"claim", "WorkQueueTransaction", `{"version":2,"kind":"Claim","work":"w","claim":"c","attempt":null}`, false},
		{"cancel claim", "WorkQueueTransaction", `{"version":2,"kind":"ClaimCancellation","work":"w","claim":"c","attempt":null}`, false},
		{"cancel work", "WorkQueueTransaction", `{"version":2,"kind":"WorkCancellation","work":"w","claim":null,"attempt":null}`, false},
		{"completion", "WorkQueueTransaction", `{"version":2,"kind":"Completion","work":"w","claim":"c","attempt":"a"}`, false},
		{"legacy requires upgrade", "WorkQueueTransaction", `{"kind":"Work","work":"w","claim":null,"attempt":null}`, true},
		{"version zero requires upgrade", "WorkQueueTransaction", `{"version":0,"kind":"Work","work":"w","claim":null,"attempt":null}`, true},
		{"version one requires upgrade", "WorkQueueTransaction", `{"version":1,"kind":"Work","work":"w","claim":null,"attempt":null}`, true},
		{"version one cannot carry enqueue time", "WorkQueueTransaction", `{"version":1,"kind":"Work","work":"w","claim":null,"attempt":null,"enqueued":123}`, true},
		{"unknown version", "WorkQueueTransaction", `{"version":3,"kind":"Work","work":"w","claim":null,"attempt":null}`, true},
		{"missing nullable field", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null}`, true},
		{"empty identity", "WorkQueueTransaction", `{"version":2,"kind":"Claim","work":"w","claim":"","attempt":null}`, true},
		{"completion without attempt", "WorkQueueTransaction", `{"version":2,"kind":"Completion","work":"w","claim":"c","attempt":null}`, true},
		{"unexpected authority", "WorkQueueTransaction", `{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null,"run_id":"run"}`, true},
		{"operator record", "WorkQueueTransaction", `{"kind":"Claim","work_id":"w","claim_id":"c","run_id":"run"}`, true},
		{"assignment", "WorkQueueAssignment", `{"work_id":"w","claim_id":"c","work":{"task":"test"}}`, false},
		{"empty assignment identity", "WorkQueueAssignment", `{"work_id":"","claim_id":"c","work":{}}`, true},
		{"assignment without payload", "WorkQueueAssignment", `{"work_id":"w","claim_id":"c"}`, true},
		{"assignment array payload", "WorkQueueAssignment", `{"work_id":"w","claim_id":"c","work":[]}`, true},
		{"finish completed", "WorkQueueFinishIntent", `{"outcome":"completed"}`, false},
		{"finish cancelled", "WorkQueueFinishIntent", `{"outcome":"cancelled"}`, false},
		{"invalid finish outcome", "WorkQueueFinishIntent", `{"outcome":"failed"}`, true},
		{"finish cannot select claim", "WorkQueueFinishIntent", `{"outcome":"completed","claim_id":"c"}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := compiler.Compile(test.schema + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var record any
			if err := json.Unmarshal([]byte(test.record), &record); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(record); (err != nil) != test.invalid {
				t.Fatalf("invalid=%t, validation error: %v", test.invalid, err)
			}
		})
	}
}
