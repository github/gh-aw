package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

func TestWorkOperatorCheckedCancellationIntent(t *testing.T) {
	state, commits, ids := workOperatorFixture(t, false)
	targets := []workOperatorTarget{{WorkID: ids[0]}}
	for _, reason := range []string{"worker_contract_obsolete", "operator_cancelled"} {
		params, kind, err := workOperatorIntent(state, slices.Clone(targets), reason, 0, "new-retirement")
		if err != nil || kind != "cancel_work" {
			t.Fatalf("cancellation intent: %s %v", kind, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(params.Operations[0], &fields); err != nil {
			t.Fatal(err)
		}
		if reason == "worker_contract_obsolete" {
			if fields["version"] != float64(1) {
				t.Fatal("operator obsolete cancellation omitted its checked version")
			}
			actor := state.Requests["submit"].Actor
			request, err := workqueue.NewRequest("new-retirement", kind, actor, params)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := workqueue.BuildCandidate(commits, actor, request, 3000); err == nil ||
				!strings.HasPrefix(err.Error(), "deployment_conflict:") {
				t.Fatalf("fresh operator intent bypassed checked eligibility: %v", err)
			}
		} else if _, exists := fields["version"]; exists {
			t.Fatal("ordinary operator reason changed its legacy shape")
		}
	}
	params, _, err := workOperatorIntent(state, targets, "worker_contract_obsolete", 1, "priority")
	if err != nil {
		t.Fatal(err)
	}
	var priority map[string]any
	_ = json.Unmarshal(params.Operations[0], &priority)
	if _, exists := priority["version"]; exists {
		t.Fatal("cancellation version leaked into WorkPriority")
	}
}

func TestWorkOperatorLegacyRetirementReceiptRecovery(t *testing.T) {
	state, commits, ids := workOperatorFixture(t, false)
	actor := state.Requests["submit"].Actor
	operation, err := workqueue.Op(map[string]any{
		"kind": "WorkCancellation", "work_id": ids[0], "reason": "worker_contract_obsolete",
	})
	if err != nil {
		t.Fatal(err)
	}
	params := workqueue.OperationsParameters{Operations: []workqueue.Operation{operation}}
	request, err := workqueue.NewRequest("legacy-retirement", "cancel_work", actor, params)
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = workqueue.BuildCandidate(commits, actor, request, 3000)
	if err != nil {
		t.Fatal(err)
	}
	checkWorkRetirementRecovery(t, commits, ids[0], request, params)
	for _, sha := range []string{strings.Repeat("c", 40), strings.Repeat("d", 40)} {
		commits, err = workqueue.CompactCheckpoint(commits, sha, actor, 4000)
		if err != nil {
			t.Fatal(err)
		}
		checkWorkRetirementRecovery(t, commits, ids[0], request, params)
	}
}

func checkWorkRetirementRecovery(t *testing.T, commits []workqueue.QueueCommit, workID string, original workqueue.Request, params workqueue.OperationsParameters) {
	t.Helper()
	state, err := workqueue.Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	targets := []workOperatorTarget{{WorkID: workID}}
	actor := state.Requests[original.ID].Actor
	recovered, kind, err := workOperatorIntent(state, targets, "worker_contract_obsolete", 0, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := workqueue.NewRequest(original.ID, kind, actor, recovered)
	if err != nil || request.Fingerprint != original.Fingerprint {
		t.Fatalf("legacy retry changed its accepted fingerprint: %v", err)
	}
	if string(recovered.Operations[0]) != string(params.Operations[0]) {
		t.Fatal("legacy retry retrofitted checked cancellation metadata")
	}
	if _, _, err := workOperatorIntent(state, targets, "different_reason", 0, original.ID); err == nil {
		t.Fatal("legacy retry changed its accepted reason")
	}
	ack, _, _, err := workqueue.BuildCandidate(commits, actor, request, 5000)
	if err != nil || len(ack) != len(commits) {
		t.Fatalf("legacy recovery attempted a new publication: %v", err)
	}
}

func TestWorkRecoverCheckedCancellationMarker(t *testing.T) {
	state, _, ids := workOperatorFixture(t, false)
	actor := state.Requests["submit"].Actor
	operation, err := workqueue.Op(map[string]any{
		"kind": "WorkCancellation", "version": 1, "work_id": ids[0], "reason": "worker_contract_obsolete",
	})
	if err != nil {
		t.Fatal(err)
	}
	params := workqueue.OperationsParameters{Operations: []workqueue.Operation{operation}}
	request, err := workqueue.NewRequest("checked-retirement", "cancel_work", actor, params)
	if err != nil {
		t.Fatal(err)
	}
	request.Parameters = json.RawMessage("null")
	recovered, err := workRecoverClearIntent(workqueue.QueueCommit{Request: request, Actor: actor, Operations: params.Operations}, "worker_contract_obsolete")
	if err != nil || string(recovered.Operations[0]) != string(operation) {
		t.Fatalf("checkpoint recovery removed checked version: %v", err)
	}
}
