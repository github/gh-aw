package workqueue

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestWorkQueueStorageNames(t *testing.T) {
	if FileName != "work-queue.jsonl" || DefaultBranch != "work-queue" || Version != 3 {
		t.Fatal("native and runtime must use one current-only authority")
	}
}

func TestContractGenerationIsReproducible(t *testing.T) {
	command := exec.Command("python3", "../../specs/work-queue/generate_contract.py", "--check")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("contract generation/check: %v\n%s", err, output)
	}
}

func TestClosedAssignmentAndFinishSchemas(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"WorkQueueAssignment", "WorkQueueFinishIntent"} {
		data, err := schemas.ReadFile("schema/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		_ = json.Unmarshal(data, &schema)
		if err := compiler.AddResource(name+".json", schema); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		Schema string
		JSON   string
		Valid  bool
	}{
		{"WorkQueueFinishIntent", `{"outcome":"completed"}`, true},
		{"WorkQueueFinishIntent", `{"outcome":"cancelled","claim_handle":"h1"}`, true},
		{"WorkQueueFinishIntent", `{"outcome":"completed","claim_handle":null}`, false},
		{"WorkQueueFinishIntent", `{"outcome":"completed","claim_handle":""}`, false},
		{"WorkQueueFinishIntent", `{"outcome":"finished"}`, false},
		{"WorkQueueFinishIntent", `{"outcome":"completed","claim_id":"c1"}`, false},
		{"WorkQueueAssignment", `{"work_id":"w","claim_id":"c","work":{}}`, false},
		{"WorkQueueAssignment", `{"version":3,"dispatch_id":"d","request_id":"r","commit_id":"q","policy_epoch":"e","pool":"p","worker_profile":"x","claims":[{"handle":"h1","claim_id":"c1","work_id":"w1","work":{},"result_refs":[]}]}`, true},
		{"WorkQueueAssignment", `{"version":3,"dispatch_id":"d","request_id":"r","commit_id":"q","policy_epoch":"e","pool":"p","worker_profile":"x","claims":[]}`, false},
	}
	for _, test := range tests {
		var value any
		_ = json.Unmarshal([]byte(test.JSON), &value)
		schema, err := compiler.Compile(test.Schema + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); (err == nil) != test.Valid {
			t.Errorf("schema %s valid=%v record=%s error=%v", test.Schema, test.Valid, test.JSON, err)
		}
	}
}
