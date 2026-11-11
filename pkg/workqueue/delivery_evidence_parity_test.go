package workqueue

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDeliveryEvidenceJavaScriptLedgerParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-runtime evidence test requires Node")
	}
	const script = `
const h = require("../../actions/setup/js/work_queue_test_helpers.cjs");
const q = require("../../actions/setup/js/work_queue_replay.cjs");
let log = [h.genesis()];
log.push(h.submission(log, ["durable"]));
const grant = h.grant(log);
log.push(grant.commit);
const dispatchId = grant.assignments[0].dispatch_id;
log = h.bind(log, dispatchId);
log.push(h.finish(log, dispatchId, "h1", "completed"));
const state = q.replayTransactions(log);
const claim = state.claims.get(grant.operations[0].claim_id);
const operation = { kind: "DeliveryEvidence", work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: "h1", completion_id: claim.terminal_commit_id, run_id: "200", run_attempt: 1, outputs: [{ index: 0, type: "create_issue", verifier: "builtin", receipt: { number: 123 } }], effects: [] };
log.push(h.operationCommit(log, "evidence", "delivery_evidence", [operation], h.workerActor(state, dispatchId), 45));
process.stdout.write(JSON.stringify({ ledger: q.serializeTransactionLog(log), checkpoint: q.serializeTransactionLog(q.prepareCheckpoint(log, "a".repeat(40), h.administrator, 60)), operation }));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript evidence fixture: %v\n%s", err, data)
	}
	var fixture struct {
		Ledger     string    `json:"ledger"`
		Checkpoint string    `json:"checkpoint"`
		Operation  Operation `json:"operation"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var identity struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.Unmarshal(fixture.Operation, &identity); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"ledger": fixture.Ledger, "JS-checkpoint": fixture.Checkpoint} {
		t.Run(name, func(t *testing.T) {
			commits, err := Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(state.DeliveryEvidence[identity.ClaimID], fixture.Operation) {
				t.Fatal("replay lost pending delivery receipts")
			}
			compacted, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 70)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Replay(compacted)
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(restored.DeliveryEvidence[identity.ClaimID], fixture.Operation) {
				t.Fatal("Go checkpoint lost pending delivery receipts")
			}
			assertRecoveryReplayParity(t, compacted, "")
		})
	}
}

func TestDeliveryEvidenceManagerRestartParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("restart evidence fixture requires Node")
	}
	const script = `
const {createHarness} = require("../../actions/setup/js/work_queue_delivery_evidence.test_helpers.cjs");
(async () => {
  const h = createHarness();
  try {
    await h.workerPass();
    const queue = require("../../actions/setup/js/work_queue_replay.cjs");
    const member = h.fixture.assignment.claims[0];
    const operation = h.projection.deliveryEvidence.get(member.claim_id);
    const pending = queue.serializeTransactionLog(h.transactions);
    const checkpoint = queue.serializeTransactionLog(queue.prepareCheckpoint(h.transactions, "a".repeat(40), h.transactions[0].actor, h.clock.now));
    h.restartForMaintenance();
    await h.maintenance();
    process.stdout.write(JSON.stringify({ pending, checkpoint, terminal: queue.serializeTransactionLog(h.transactions), operation, claim_id: member.claim_id, work_id: member.work_id, writes: h.state.writes }));
  } finally { h.dispose(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, node, "-e", script).Output()
	if err != nil {
		t.Fatalf("JavaScript manager restart fixture: %v\n%s", err, data)
	}
	var fixture struct {
		Pending    string    `json:"pending"`
		Checkpoint string    `json:"checkpoint"`
		Terminal   string    `json:"terminal"`
		Operation  Operation `json:"operation"`
		ClaimID    string    `json:"claim_id"`
		WorkID     string    `json:"work_id"`
		Writes     int       `json:"writes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode restart fixture: %v\n%s", err, data)
	}
	if fixture.Writes != 1 {
		t.Fatal("maintenance replayed the native issue write")
	}
	for name, text := range map[string]string{"pending": fixture.Pending, "checkpoint": fixture.Checkpoint, "terminal": fixture.Terminal} {
		t.Run(name, func(t *testing.T) {
			commits, err := Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			if name == "terminal" {
				if state.Works[fixture.WorkID].Barrier != "verified" || len(state.DeliveryEvidence) != 0 {
					t.Fatal("terminal delivery retained evidence or lost its verified barrier")
				}
			} else if !sameJSON(state.DeliveryEvidence[fixture.ClaimID], fixture.Operation) || state.Works[fixture.WorkID].Barrier != "pending" {
				t.Fatal("pending replay lost native delivery evidence")
			}
			compacted, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 2000)
			if err != nil {
				t.Fatal(err)
			}
			assertRecoveryReplayParity(t, compacted, "")
		})
	}
}
