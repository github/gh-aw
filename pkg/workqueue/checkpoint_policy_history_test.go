package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckpointPolicyPoolHistoryParity(t *testing.T) {
	history := testGenesis(t, func(policy *Policy) {
		policy.Authorization = "aw"
		policy.Producers = map[string]ProducerRule{}
		pool := policy.Pools["default"]
		profile := pool.Profiles["default"]
		profile.Principal = ""
		profile.LogicalContract = strings.Repeat("a", 64)
		pool.Profiles["default"] = profile
		policy.Pools["default"] = pool
	})
	state, err := Replay(history)
	if err != nil {
		t.Fatal(err)
	}
	install := mustOp(t, PoolInstallOperation{Kind: "PoolInstall", Pool: "added", Config: state.Policy.Pools["default"]})
	history = testOperations(t, history, testActor("dispatcher"), "install-added", "pool_install", install)
	state, err = Replay(history)
	if err != nil {
		t.Fatal(err)
	}
	history = testOperations(t, history, testActor("administrator"), "next-policy", "policy",
		mustOp(t, map[string]any{"kind": "Policy", "epoch": "epoch2", "policy": *state.Policy}))
	prior := strings.Repeat("c", 40)
	checkpoint, err := CompactCheckpoint(history, prior, testActor("administrator"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	checkEvents := func(commits []QueueCommit) {
		t.Helper()
		op, err := decodeCheckpointOperation(commits[0])
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _, err := decodeCheckpointSnapshot(op.State)
		if err != nil {
			t.Fatal(err)
		}
		for index, original := range history {
			if !sameJSON(snapshot.Requests[index].Events, original.Operations) {
				t.Fatal("checkpoint lost complete Policy or PoolInstall configuration")
			}
		}
	}
	checkEvents(checkpoint)
	nested, err := CompactCheckpoint(checkpoint, strings.Repeat("d", 40), testActor("administrator"), 5001)
	if err != nil {
		t.Fatal(err)
	}
	checkEvents(nested)
	var js struct {
		Checkpoint []QueueCommit `json:"checkpoint"`
	}
	checkpointRegressionJS(t, "--checkpoint", map[string]any{
		"transactions": history, "prior_git_sha": prior, "actor": testActor("administrator"), "at": 5000,
	}, &js)
	if !sameJSON(js.Checkpoint[0].Request, checkpoint[0].Request) {
		t.Fatal("Go and JavaScript disagree on retained Policy history")
	}
	checkEvents(js.Checkpoint)
	for _, legacyTimestamp := range []bool{false, true} {
		legacy, err := compactCheckpointForHostWithPolicyHistory(history, prior, testActor("administrator"), 5000, legacyTimestamp, "", true)
		if err != nil {
			t.Fatal(err)
		}
		var op CheckpointOperation
		if err := json.Unmarshal(legacy[0].Operations[0], &op); err != nil {
			t.Fatal(err)
		}
		recovered, err := reconstructCheckpoint(history, prior, testActor("administrator"), 5000, op.StateSHA256)
		if err != nil || !sameJSON(recovered, legacy) {
			t.Fatalf("cannot reconstruct pre-Policy-history checkpoint: %v", err)
		}
	}
}
