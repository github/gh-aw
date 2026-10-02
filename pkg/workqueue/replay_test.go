package workqueue

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Transaction, Transaction, Transaction) {
	t.Helper()
	id, work, err := WorkID([]byte(`{"task":"test","priority":1}`))
	if err != nil {
		t.Fatal(err)
	}
	return Transaction{Kind: "Work", WorkID: id, Work: work},
		Transaction{Kind: "Claim", WorkID: id, ClaimID: "a", RunID: "run-a"},
		Transaction{Kind: "Claim", WorkID: id, ClaimID: "b", RunID: "run-b"}
}

func TestReplayOrderAndCompaction(t *testing.T) {
	work, a, b := fixture(t)
	cancel := Transaction{Kind: "ClaimCancellation", WorkID: work.WorkID, ClaimID: a.ClaimID}
	first := []Transaction{work, b, a, cancel, b}
	second := []Transaction{cancel, a, b, work}
	left, err := Replay(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Replay(second)
	if err != nil || !reflect.DeepEqual(left, right) {
		t.Fatalf("order-dependent replay: %v, %v, %v", left, right, err)
	}
	if left.Works[0].Winner != "b" || left.Stats.Transactions != 4 {
		t.Fatalf("incorrect projection: %+v", left)
	}
	compacted, err := Compact(first)
	if err != nil || len(compacted) != 4 {
		t.Fatalf("incorrect compact: %v, %v", compacted, err)
	}
	after, err := Replay(compacted)
	if err != nil || !reflect.DeepEqual(left, after) {
		t.Fatalf("compaction changed projection: %v, %v", after, err)
	}
	next := Transaction{Kind: "Completion", WorkID: work.WorkID, ClaimID: "b", AttemptID: "attempt"}
	originalExtension, _, err := Apply(first, next)
	if err != nil {
		t.Fatal(err)
	}
	compactedExtension, _, err := Apply(compacted, next)
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := Replay(originalExtension)
	p2, _ := Replay(compactedExtension)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatal("compaction changed future semantics")
	}
}

func TestApplyTerminalAndIdempotency(t *testing.T) {
	work, a, b := fixture(t)
	log, changed, err := Apply(nil, work)
	if err != nil || !changed {
		t.Fatalf("submit: %v", err)
	}
	if _, changed, err := Apply(log, work); err != nil || changed {
		t.Fatalf("idempotent submission: %v, %t", err, changed)
	}
	log, _, err = Apply(log, b)
	if err != nil {
		t.Fatal(err)
	}
	log, _, err = Apply(log, a)
	if err != nil {
		t.Fatal(err)
	}
	wrong := Transaction{Kind: "Completion", WorkID: work.WorkID, ClaimID: "b", AttemptID: "try"}
	if _, _, err := Apply(log, wrong); err == nil {
		t.Fatal("superseded claim completed work")
	}
	done := Transaction{Kind: "Completion", WorkID: work.WorkID, ClaimID: "a", AttemptID: "try"}
	log, _, err = Apply(log, done)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Apply(log, Transaction{Kind: "ClaimCancellation", WorkID: work.WorkID, ClaimID: "b"}); err == nil {
		t.Fatal("terminal work accepted mutation")
	}
	if _, _, err := Apply(log, Transaction{Kind: "WorkCancellation", WorkID: work.WorkID}); err == nil {
		t.Fatal("completed work cancelled")
	}
	if _, changed, err := Apply(log, done); err != nil || changed {
		t.Fatalf("duplicate completion: %v, %t", err, changed)
	}
}

func TestReplayRejectsInvalidFactsAndMessages(t *testing.T) {
	work, a, b := fixture(t)
	cases := [][]Transaction{
		{a}, {work, {Kind: "Completion", WorkID: work.WorkID, ClaimID: "missing", AttemptID: "1"}},
		{work, a, b, {Kind: "Completion", WorkID: work.WorkID, ClaimID: "b", AttemptID: "1"}},
		{work, work, {Kind: "WorkCancellation", WorkID: work.WorkID}, {Kind: "Completion", WorkID: work.WorkID, ClaimID: "a", AttemptID: "1"}, a},
		{work, {Kind: "Claim", WorkID: work.WorkID, ClaimID: "a", RunID: "different"}, a},
	}
	for i, input := range cases {
		if _, err := Replay(input); err == nil {
			t.Errorf("case %d accepted invalid facts", i)
		}
	}
	valid, err := Serialize([]Transaction{work, a})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(valid); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`{"kind":"Work","work_id":"x","work":{},"extra":true}`,
		`{"kind":"Claim","work_id":"x","claim_id":"a"}`,
		`{"kind":"Unknown","work_id":"x"}`,
		`{"kind":"Work","work_id":"x","work":[]}`,
	} {
		if _, err := Parse([]byte(line)); err == nil {
			t.Errorf("accepted invalid message: %s", line)
		}
	}
	completion, err := json.Marshal(Transaction{
		Kind: "Completion", WorkID: work.WorkID, ClaimID: a.ClaimID, AttemptID: "attempt",
	})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(completion), "}") + `,"outcome":""}`
	log, err := Parse(append(valid, append([]byte(line), '\n')...))
	if err != nil || len(log) != 3 || log[2].Outcome != "" {
		t.Fatalf("schema-valid empty outcome did not parse: %v, %v", log, err)
	}
	var value map[string]any
	if err := json.Unmarshal(work.Work, &value); err != nil {
		t.Fatal(err)
	}
	id, _, err := WorkID([]byte(`{"priority":1,"task":"test"}`))
	if err != nil || !strings.EqualFold(id, work.WorkID) {
		t.Fatal("unstable canonical payload identity")
	}
}
