package workqueue

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedCheckpointConformance(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}

	var fixture struct {
		PriorGitSHA       string        `json:"prior_git_sha"`
		History           []QueueCommit `json:"history"`
		Checkpoint        []QueueCommit `json:"checkpoint"`
		GenesisCheckpoint []QueueCommit `json:"genesis_checkpoint"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	before, err := Replay(fixture.History)
	if err != nil {
		t.Fatal(err)
	}
	checkpointData, err := Serialize(fixture.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := Parse(checkpointData)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Replay(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(before.Works, after.Works) || !sameJSON(before.Clocks, after.Clocks) ||
		!sameJSON(before.Claims, after.Claims) || !sameJSON(before.Dispatches, after.Dispatches) ||
		!sameJSON(before.Observations, after.Observations) || len(after.Requests) != len(before.Requests)+1 {
		t.Fatal("cross-runtime checkpoint lost replayed scheduling or lifecycle authority")
	}
	if _, ok := after.Requests[fixture.History[1].Request.ID]; !ok {
		t.Fatal("historical request identity was lost")
	}
	for _, original := range fixture.History {
		restored := after.Requests[original.Request.ID]
		if restored.Request.Fingerprint != original.Request.Fingerprint {
			t.Fatal("historical request fingerprint was lost")
		}
		if original.Request.Kind == "submit" || original.Request.Kind == "dispatch_next" {
			if !sameJSON(restored.Request.Parameters, original.Request.Parameters) {
				t.Fatal("historical request parameters changed meaning")
			}
		} else if string(restored.Request.Parameters) != "null" {
			t.Fatal("compacted request must not claim empty parameters")
		}
	}
	again, err := CompactCheckpoint(fixture.History, fixture.PriorGitSHA, checkpoint[0].Actor, checkpoint[0].At)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(again[0].Request, checkpoint[0].Request) || again[0].ID != checkpoint[0].ID {
		t.Fatal("Go and JS derived different stable checkpoint identities")
	}
	genesis, err := CompactCheckpoint(fixture.History[:1], fixture.PriorGitSHA, checkpoint[0].Actor, 0)
	if err != nil || len(fixture.GenesisCheckpoint) != 1 ||
		!sameJSON(genesis[0].Request, fixture.GenesisCheckpoint[0].Request) {
		t.Fatal("Go and JS differ for an untouched scheduling clock")
	}
	control, err := Op(map[string]any{"kind": "Control", "control": "grants_paused", "value": true, "reason": "pause"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest("post-checkpoint-pause", "control", checkpoint[0].Actor, OperationsParameters{Operations: []Operation{control}})
	if err != nil {
		t.Fatal(err)
	}
	next, published, _, err := BuildCandidate(checkpoint, checkpoint[0].Actor, request, 101)
	if err != nil || published == nil {
		t.Fatalf("cannot extend checkpoint with fresh request: %v", err)
	}
	extended, err := Replay(next)
	if err != nil || !extended.GrantsPaused || extended.Stats.Transactions != len(fixture.History)+2 ||
		!sameJSON(after.Clocks, extended.Clocks) || len(extended.Requests) != len(after.Requests)+1 {
		t.Fatal("checkpoint extension lost historical scheduling debt or stable request identity")
	}
	nested, err := CompactCheckpoint(next, strings.Repeat("b", 40), fixture.Checkpoint[0].Actor, 102)
	if err != nil {
		t.Fatal(err)
	}
	nestedState, err := Replay(nested)
	if err != nil || nestedState.Stats.Transactions != len(fixture.History)+3 ||
		!nestedState.GrantsPaused || len(nestedState.Requests) != len(extended.Requests)+1 {
		t.Fatal("nested checkpoint lost earlier causal history or later request")
	}
	malformed := checkpoint[0]
	malformed.Operations = append([]Operation(nil), malformed.Operations...)
	var op CheckpointOperation
	if err := json.Unmarshal(malformed.Operations[0], &op); err != nil {
		t.Fatal(err)
	}
	replacement := "0"
	if op.StateSHA256[0] == '0' {
		replacement = "1"
	}
	op.StateSHA256 = replacement + op.StateSHA256[1:]
	malformed.Operations[0], err = Op(op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Replay([]QueueCommit{malformed}); err == nil {
		t.Fatal("checkpoint accepted altered state digest")
	}
	forged := checkpoint[0]
	forged.Operations = append([]Operation(nil), forged.Operations...)
	if err := json.Unmarshal(forged.Operations[0], &op); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(op.State, &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, claim := range snapshot["claims"].(map[string]any) {
		claim.(map[string]any)["work_id"] = "missing-work"
		break
	}
	op.State, err = Op(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	op.StateSHA256 = hashBytes(op.State)
	forged.Operations[0], err = Op(op)
	if err != nil {
		t.Fatal(err)
	}
	forged.Request, err = NewRequest(forged.Request.ID, "checkpoint", forged.Actor, CheckpointParameters{
		PriorGitSHA: op.PriorGitSHA, PriorTip: op.PriorTip,
		HistorySHA256: op.HistorySHA256, StateSHA256: op.StateSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Replay([]QueueCommit{forged}); err == nil {
		t.Fatal("checkpoint accepted a forged claim with matching state digest")
	}
}

func TestCheckpointPreservesPendingDeliveryDeadline(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	before, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("c", 40), testActor("administrator"), commits[len(commits)-1].At)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Replay(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	id := assignment.Claims[0].WorkID
	if before.Works[id].completionAt != after.Works[id].completionAt ||
		after.Works[id].Barrier != "pending" || !sameJSON(before.Dispatches, after.Dispatches) {
		t.Fatal("checkpoint reset pending delivery deadline or native reservation")
	}
}

func TestCheckpointPreservesInspectionProvenance(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PriorGitSHA string        `json:"prior_git_sha"`
		History     []QueueCommit `json:"history"`
		Checkpoint  []QueueCommit `json:"checkpoint"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var claim ClaimOperation
	var requestID string
	for _, commit := range fixture.History {
		for _, operation := range commit.Operations {
			if err := json.Unmarshal(operation, &claim); err == nil && claim.Kind == "Claim" {
				requestID = commit.Request.ID
				break
			}
		}
		if requestID != "" {
			break
		}
	}
	if requestID == "" {
		t.Fatal("checkpoint fixture has no Claim")
	}
	before, err := ExplainBeforeClaim(fixture.History, claim.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	beforeTrace, err := TraceQueue(fixture.History, TraceOptions{ClaimID: claim.ClaimID, Limit: 256}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := CompactCheckpoint(fixture.History, fixture.PriorGitSHA, fixture.Checkpoint[0].Actor, fixture.Checkpoint[0].At)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ExplainBeforeClaim(checkpoint, claim.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	before.Tip = checkpoint[0].ID
	if !sameJSON(before, after) {
		t.Fatalf("checkpoint changed historical Claim explanation: %+v != %+v", before, after)
	}
	request, err := ExplainRequest(checkpoint, requestID)
	if err != nil || len(request.Claims) == 0 {
		t.Fatalf("checkpoint lost historical request explanation: %+v %v", request, err)
	}
	afterTrace, err := TraceQueue(checkpoint, TraceOptions{ClaimID: claim.ClaimID, Limit: 256}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	beforeTrace.Tip = checkpoint[0].ID
	if !sameJSON(beforeTrace, afterTrace) {
		t.Fatalf("checkpoint changed historical trace: %+v != %+v", beforeTrace, afterTrace)
	}
}
