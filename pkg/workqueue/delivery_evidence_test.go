package workqueue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func deliveryEvidenceFixture(t *testing.T, keepOpen bool) ([]QueueCommit, Assignment, map[string]any) {
	t.Helper()
	commits, assignment := boundAssignment(t)
	member := assignment.Claims[0]
	actor := workerActor(assignment, member.Handle)
	request, err := NewRequest("evidence-completion", "finish", actor, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: member.Handle, Outcome: "completed", KeepOpen: keepOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, actor, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return commits, assignment, map[string]any{
		"kind": "DeliveryEvidence", "work_id": member.WorkID, "claim_id": member.ClaimID,
		"dispatch_id": assignment.DispatchID, "claim_handle": member.Handle,
		"completion_id": state.Works[member.WorkID].CompletionID, "run_id": "202", "run_attempt": 1,
		"outputs": []any{map[string]any{"index": 0, "type": "create_issue", "verifier": "builtin", "receipt": map[string]any{"number": 17}}},
		"effects": []any{map[string]any{"kind": "issue", "repository": testRepository, "outcome": "succeeded", "number": 17}},
	}
}

func rejectDeliveryEvidence(t *testing.T, commits []QueueCommit, actor Actor, value map[string]any, code string) {
	t.Helper()
	request, err := NewRequest("rejected-evidence", "delivery_evidence", actor, OperationsParameters{
		Operations: []Operation{mustOp(t, value)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil || code != "" && !strings.Contains(err.Error(), code) {
		t.Fatalf("expected rejected DeliveryEvidence (%s), got %v", code, err)
	}
}

func settleDeliveryEvidence(t *testing.T, commits []QueueCommit, assignment Assignment, failed bool) []QueueCommit {
	t.Helper()
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	member := assignment.Claims[0]
	evidence := terminalFor(state, assignment)
	operation := map[string]any{"work_id": member.WorkID, "claim_id": member.ClaimID, "completion_id": state.Works[member.WorkID].CompletionID}
	kind := "result"
	if failed {
		kind = "delivery_failure"
		evidence.Attempts, evidence.Effects = state.Policy.Pools[assignment.Pool].Reconciliation.MaxAttempts, "unknown"
		operation["kind"], operation["reason"], operation["disposition"] = "DeliveryFailure", "verification_exhausted", "unknown"
	} else {
		evidence.Kind, evidence.Source, evidence.Receipt = "delivery", "verified_receipts", "independent-readback"
		operation["kind"], operation["descriptor"] = "Result", map[string]any{"number": 17}
	}
	operation["evidence"] = evidence
	return testOperations(t, commits, testActor("reconciler"), "settle-evidence", kind, mustOp(t, operation))
}

func TestDeliveryEvidenceReplayAndImmutability(t *testing.T) {
	for _, keepOpen := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep_open=%t", keepOpen), func(t *testing.T) {
			commits, assignment, value := deliveryEvidenceFixture(t, keepOpen)
			actor := workerActor(assignment, assignment.Claims[0].Handle)
			operation := mustOp(t, value)
			commits = testOperations(t, commits, actor, "evidence", "delivery_evidence", operation)
			commits = testOperations(t, commits, actor, "identical-evidence", "delivery_evidence", operation)
			state, err := Replay(commits)
			if err != nil || len(state.DeliveryEvidence) != 1 || !sameJSON(state.DeliveryEvidence[assignment.Claims[0].ClaimID], operation) {
				t.Fatalf("original worker evidence was not retained idempotently: %v", err)
			}
			if state.Works[assignment.Claims[0].WorkID].Barrier != "pending" {
				t.Fatal("evidence alone verified delivery")
			}
			value["controls_digest"] = strings.Repeat("a", 64)
			rejectDeliveryEvidence(t, commits, actor, value, "delivery_conflict")
			delete(value, "controls_digest")
			for _, failed := range []bool{false, true} {
				t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
					settled := settleDeliveryEvidence(t, commits, assignment, failed)
					state, err := Replay(settled)
					if err != nil || len(state.DeliveryEvidence) != 0 {
						t.Fatalf("terminal barrier retained evidence: %v", err)
					}
					rejectDeliveryEvidence(t, settled, actor, value, "")
					checkpoint, err := CompactCheckpoint(settled, strings.Repeat("b", 40), testActor("administrator"), 4000)
					if err != nil {
						t.Fatal(err)
					}
					checkpointOperation, err := decodeCheckpointOperation(checkpoint[0])
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Contains(checkpointOperation.State, []byte(`"delivery_evidence"`)) {
						t.Fatal("empty evidence map changed checkpoint wire shape")
					}
					restored, err := Replay(checkpoint)
					if err != nil || restored.DeliveryEvidence == nil || len(restored.DeliveryEvidence) != 0 {
						t.Fatalf("terminal checkpoint did not restore an empty evidence map: %v", err)
					}
				})
			}
		})
	}
}

func TestDeliveryEvidenceRejectsForeignScope(t *testing.T) {
	commits, assignment, value := deliveryEvidenceFixture(t, false)
	actor := workerActor(assignment, assignment.Claims[0].Handle)
	for _, field := range []string{"work_id", "claim_id", "dispatch_id", "claim_handle", "completion_id", "run_id"} {
		t.Run(field, func(t *testing.T) {
			original := value[field]
			value[field] = "999"
			rejectDeliveryEvidence(t, commits, actor, value, "claim_scope_invalid")
			value[field] = original
		})
	}
	for _, role := range []string{"administrator", "producer", "dispatcher", "reconciler", "projector"} {
		t.Run(role, func(t *testing.T) {
			rejectDeliveryEvidence(t, commits, testActor(role), value, "actor_unauthorized")
		})
	}
	for _, field := range []string{"principal", "repository", "workflow", "run", "attempt", "dispatch", "handle"} {
		t.Run("actor/"+field, func(t *testing.T) {
			foreign := actor
			switch field {
			case "principal":
				foreign.Principal = "999"
			case "repository":
				foreign.Repository = "other/repo"
			case "workflow":
				foreign.Workflow = ".github/workflows/other.lock.yml"
			case "run":
				foreign.RunID = "999"
			case "attempt":
				foreign.RunAttempt = 2
			case "dispatch":
				foreign.DispatchID = "foreign"
			case "handle":
				foreign.ClaimHandle = assignment.Claims[1].Handle
			}
			rejectDeliveryEvidence(t, commits, foreign, value, "")
		})
	}
	t.Run("before Completion", func(t *testing.T) {
		rejectDeliveryEvidence(t, commits[:len(commits)-1], actor, value, "claim_ineffective")
	})
}

func TestDeliveryEvidenceMalformedInventory(t *testing.T) {
	commits, assignment, original := deliveryEvidenceFixture(t, false)
	actor := workerActor(assignment, assignment.Claims[0].Handle)
	tests := []struct {
		name   string
		change func(map[string]any, map[string]any, map[string]any)
	}{
		{"unknown field", func(v, o, e map[string]any) { v["unexpected"] = true }},
		{"missing outputs", func(v, o, e map[string]any) { delete(v, "outputs") }},
		{"null outputs", func(v, o, e map[string]any) { v["outputs"] = nil }},
		{"null effects", func(v, o, e map[string]any) { v["effects"] = nil }},
		{"too many outputs", func(v, o, e map[string]any) {
			outputs := make([]any, 129)
			for i := range outputs {
				outputs[i] = map[string]any{"index": i, "type": "noop", "verifier": "none"}
			}
			v["outputs"] = outputs
		}},
		{"too many effects", func(v, o, e map[string]any) {
			effects := make([]any, 257)
			for i := range effects {
				effects[i] = e
			}
			v["effects"] = effects
		}},
		{"duplicate indexes", func(v, o, e map[string]any) { v["outputs"] = []any{o, o} }},
		{"descending indexes", func(v, o, e map[string]any) {
			o["index"] = 1
			v["outputs"] = []any{o, map[string]any{"index": 0, "type": "noop", "verifier": "none"}}
		}},
		{"negative index", func(v, o, e map[string]any) { o["index"] = -1 }},
		{"index beyond maximum", func(v, o, e map[string]any) { o["index"] = 128 }},
		{"empty type", func(v, o, e map[string]any) { o["type"] = "" }},
		{"oversized type", func(v, o, e map[string]any) { o["type"] = strings.Repeat("é", 65) }},
		{"unknown output field", func(v, o, e map[string]any) { o["extra"] = true }},
		{"unknown verifier", func(v, o, e map[string]any) { o["verifier"] = "agent_assertion" }},
		{"none with receipt", func(v, o, e map[string]any) { o["verifier"] = "none" }},
		{"missing receipt", func(v, o, e map[string]any) { delete(o, "receipt") }},
		{"null receipt", func(v, o, e map[string]any) { o["receipt"] = nil }},
		{"array receipt", func(v, o, e map[string]any) { o["receipt"] = []any{} }},
		{"scalar receipt", func(v, o, e map[string]any) { o["receipt"] = "proof" }},
		{"null adapter", func(v, o, e map[string]any) { o["adapter"] = nil }},
		{"non JSON adapter", func(v, o, e map[string]any) { o["adapter"] = "{" }},
		{"duplicate adapter key", func(v, o, e map[string]any) { o["adapter"] = `{"a":1,"a":2}` }},
		{"fractional adapter number", func(v, o, e map[string]any) { o["adapter"] = `{"a":1.5}` }},
		{"unsafe adapter number", func(v, o, e map[string]any) { o["adapter"] = `{"a":9007199254740992}` }},
		{"adapter surrogate", func(v, o, e map[string]any) { o["adapter"] = `"\ud800"` }},
		{"trailing adapter JSON", func(v, o, e map[string]any) { o["adapter"] = `{} {}` }},
		{"oversized adapter", func(v, o, e map[string]any) { o["adapter"] = `"` + strings.Repeat("x", 8191) + `"` }},
		{"oversized UTF8 adapter", func(v, o, e map[string]any) { o["adapter"] = `"` + strings.Repeat("é", 4096) + `"` }},
		{"oversized operation", func(v, o, e map[string]any) {
			o["receipt"] = map[string]any{"padding": strings.Repeat("é", deliveryEvidenceMaxBytes/2)}
		}},
		{"missing effect kind", func(v, o, e map[string]any) { delete(e, "kind") }},
		{"empty effect repository", func(v, o, e map[string]any) { e["repository"] = "" }},
		{"invalid effect id", func(v, o, e map[string]any) { e["id"] = 17 }},
		{"invalid outcome", func(v, o, e map[string]any) { e["outcome"] = "failed" }},
		{"zero effect number", func(v, o, e map[string]any) { e["number"] = 0 }},
		{"invalid commit", func(v, o, e map[string]any) { e["commit"] = strings.Repeat("A", 40) }},
		{"unknown effect field", func(v, o, e map[string]any) { e["extra"] = true }},
		{"unknown expectation field", func(v, o, e map[string]any) { e["expected"] = map[string]any{"title": "unbounded"} }},
		{"null expectation", func(v, o, e map[string]any) { e["expected"] = nil }},
		{"invalid controls digest", func(v, o, e map[string]any) { v["controls_digest"] = strings.Repeat("A", 64) }},
		{"wrong attempt", func(v, o, e map[string]any) { v["run_attempt"] = 2 }},
		{"nondecimal run", func(v, o, e map[string]any) { v["run_id"] = "0202" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(mustOp(t, original), &value); err != nil {
				t.Fatal(err)
			}
			output := value["outputs"].([]any)[0].(map[string]any)
			effect := value["effects"].([]any)[0].(map[string]any)
			test.change(value, output, effect)
			rejectDeliveryEvidence(t, commits, actor, value, "")
		})
	}
}

func TestDeliveryEvidenceInventoryBoundaries(t *testing.T) {
	for _, boundary := range []string{"empty", "maximum inventories", "adapter UTF8", "canonical bytes", "opaque expected fields"} {
		t.Run(boundary, func(t *testing.T) {
			commits, assignment, value := deliveryEvidenceFixture(t, false)
			output := value["outputs"].([]any)[0].(map[string]any)
			switch boundary {
			case "empty":
				value["outputs"], value["effects"] = []any{}, []any{}
			case "maximum inventories":
				outputs, effects := make([]any, 128), make([]any, 256)
				for i := range outputs {
					outputs[i] = map[string]any{"index": i, "type": "noop", "verifier": "none"}
				}
				for i := range effects {
					effects[i] = map[string]any{"kind": "issue", "repository": testRepository, "outcome": "unknown"}
				}
				value["outputs"], value["effects"] = outputs, effects
			case "adapter UTF8":
				output["adapter"] = `"` + strings.Repeat("é", 4095) + `"`
			case "canonical bytes":
				receipt := map[string]any{"padding": ""}
				output["receipt"] = receipt
				receipt["padding"] = strings.Repeat("x", deliveryEvidenceMaxBytes-len(mustOp(t, value)))
				if len(mustOp(t, value)) != deliveryEvidenceMaxBytes {
					t.Fatal("fixture did not reach exact canonical bound")
				}
			case "opaque expected fields":
				effect := value["effects"].([]any)[0].(map[string]any)
				effect["expected"] = map[string]any{"ref": nil, "sha": 1, "labels": "opaque", "assignees": map[string]any{"key": true}}
			}
			actor := workerActor(assignment, assignment.Claims[0].Handle)
			commits = testOperations(t, commits, actor, "boundary-evidence", "delivery_evidence", mustOp(t, value))
			if _, err := Replay(commits); err != nil {
				t.Fatal(err)
			}
			if boundary == "canonical bytes" {
				receipt := output["receipt"].(map[string]any)
				receipt["padding"] = receipt["padding"].(string) + "x"
				rejectDeliveryEvidence(t, commits, actor, value, "resource_limit")
			}
		})
	}
}

func TestDeliveryEvidenceCheckpointRetentionAndLegacy(t *testing.T) {
	for _, keepOpen := range []bool{false, true} {
		for _, released := range []bool{false, true} {
			t.Run(fmt.Sprintf("keep_open=%t/released=%t", keepOpen, released), func(t *testing.T) {
				commits, assignment, value := deliveryEvidenceFixture(t, keepOpen)
				actor := workerActor(assignment, assignment.Claims[0].Handle)
				legacy, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 4000)
				if err != nil {
					t.Fatal(err)
				}
				legacyOperation, err := decodeCheckpointOperation(legacy[0])
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(legacyOperation.State, []byte(`"delivery_evidence"`)) {
					t.Fatal("legacy checkpoint gained an empty evidence key")
				}
				state, err := Replay(legacy)
				if err != nil || state.DeliveryEvidence == nil || len(state.DeliveryEvidence) != 0 {
					t.Fatalf("legacy checkpoint restore: %v", err)
				}
				commits = testOperations(t, legacy, actor, "pending-evidence", "delivery_evidence", mustOp(t, value))
				if released {
					for i := 1; i < len(assignment.Claims); i++ {
						commits = finishMember(t, commits, assignment, i, "cancelled")
					}
					state, err = Replay(commits)
					if err != nil {
						t.Fatal(err)
					}
					commits = testOperations(t, commits, testActor("reconciler"), "release-evidence", "release", mustOp(t, map[string]any{
						"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminalFor(state, assignment),
					}))
					rejectDeliveryEvidence(t, commits, actor, value, "run_binding_conflict")
				}
				checkpoint, err := CompactCheckpoint(commits, strings.Repeat("b", 40), testActor("administrator"), 4000)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := Replay(checkpoint)
				if err != nil || !sameJSON(restored.DeliveryEvidence[assignment.Claims[0].ClaimID], mustOp(t, value)) {
					t.Fatalf("pending evidence did not survive checkpoint: %v", err)
				}
			})
		}
	}
}

func TestDeliveryEvidenceCheckpointRejectsInvalidScopeAndInventory(t *testing.T) {
	commits, assignment, value := deliveryEvidenceFixture(t, false)
	actor := workerActor(assignment, assignment.Claims[0].Handle)
	commits = testOperations(t, commits, actor, "checkpoint-evidence", "delivery_evidence", mustOp(t, value))
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"kind", "work_id", "claim_id", "dispatch_id", "claim_handle", "completion_id", "run_id", "run_attempt", "receipt", "adapter", "map key", "claim state", "current claim", "terminal barrier", "missing run"} {
		t.Run(mutation, func(t *testing.T) {
			invalid := rewriteCheckpointRegression(t, checkpoint, func(snapshot *checkpointSnapshot, _ *QueueCommit, _ *CheckpointOperation) {
				member := assignment.Claims[0]
				var evidence map[string]any
				if err := json.Unmarshal(snapshot.DeliveryEvidence[member.ClaimID], &evidence); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "map key":
					snapshot.DeliveryEvidence["foreign"] = snapshot.DeliveryEvidence[member.ClaimID]
					delete(snapshot.DeliveryEvidence, member.ClaimID)
					return
				case "claim state":
					snapshot.Claims[member.ClaimID].State = "cancelled"
				case "current claim":
					snapshot.Works[member.WorkID].ClaimID = assignment.Claims[1].ClaimID
				case "terminal barrier":
					snapshot.Works[member.WorkID].Barrier = "verified"
				case "missing run":
					snapshot.Dispatches[assignment.DispatchID].Run = nil
				case "run_attempt":
					evidence[mutation] = 2
				case "receipt":
					evidence["outputs"].([]any)[0].(map[string]any)["receipt"] = nil
				case "adapter":
					evidence["outputs"].([]any)[0].(map[string]any)["adapter"] = `{"duplicate":1,"duplicate":2}`
				default:
					evidence[mutation] = "999"
				}
				snapshot.DeliveryEvidence[member.ClaimID] = mustOp(t, evidence)
			})
			if _, err := Replay(invalid); err == nil || !strings.Contains(err.Error(), "checkpoint_invalid") {
				t.Fatalf("checkpoint accepted invalid %s: %v", mutation, err)
			}
		})
	}
}
