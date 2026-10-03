package workqueue

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestVersionTwoCodemodPreservesFacts(t *testing.T) {
	work, a, b := fixture(t)
	work.Sequence = 7
	id, payload, err := WorkID([]byte(`{"task":"cancelled","nested":{"number":1.5}}`))
	if err != nil {
		t.Fatal(err)
	}
	legacy := []Transaction{
		work, a, b,
		{Kind: "ClaimCancellation", WorkID: work.WorkID, ClaimID: b.ClaimID},
		{Kind: "Completion", WorkID: work.WorkID, ClaimID: a.ClaimID, AttemptID: "attempt", Outcome: "completed"},
		{Kind: "Work", WorkID: id, Work: payload, Sequence: 9},
		{Kind: "WorkCancellation", WorkID: id},
	}
	var log bytes.Buffer
	expected := make([]Transaction, 0, len(legacy))
	for i, tx := range legacy {
		tx.Version = 2
		legacy[i] = tx
		line, err := json.Marshal(tx)
		if err != nil {
			t.Fatal(err)
		}
		log.Write(line)
		log.WriteByte('\n')
		tx.Version = CurrentVersion
		expected = append(expected, tx)
	}
	upgraded, err := Parse(log.Bytes())
	if err != nil || !reflect.DeepEqual(upgraded, expected) {
		t.Fatalf("codemod changed facts: %+v, %v", upgraded, err)
	}
	before, err := Replay(legacy)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Replay(upgraded)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("codemod changed replay: %+v, %v", after, err)
	}
	if _, changed, err := Apply(upgraded, legacy[4]); err != nil || changed {
		t.Fatalf("version-2 duplicate completion changed state: %t, %v", changed, err)
	}
	if legacy[0].Version != 2 || CurrentVersion != 3 {
		t.Fatal("upgrade mutated input or emitted the wrong version")
	}
}

func TestVersionThreeRejectsIncompleteOrFutureMessages(t *testing.T) {
	for _, input := range []string{
		`{"version":2,"kind":"Work","work_id":"w","work":{}}`,
		`{"version":2,"kind":"Claim","work_id":"w","claim_id":"c"}`,
		`{"version":4,"kind":"Work","work_id":"w","work":{},"sequence":1}`,
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted invalid upgrade: %s", input)
		}
	}
}

func TestWorkSequencesAreUniqueAndCannotOverflow(t *testing.T) {
	duplicateSequenceLog := []byte(
		"{\"version\":3,\"kind\":\"Work\",\"work_id\":\"work-a\",\"work\":{},\"sequence\":1}\n" +
			"{\"version\":3,\"kind\":\"Work\",\"work_id\":\"work-b\",\"work\":{},\"sequence\":1}\n",
	)
	if _, err := Parse(duplicateSequenceLog); err == nil {
		t.Fatal("expected duplicate Work sequence to be rejected")
	}

	maxSequenceWork := Transaction{
		Version: CurrentVersion, Kind: "Work", WorkID: "work-max",
		Work: json.RawMessage(`{}`), Sequence: MaxSequence,
	}
	if _, err := nextSequence([]Transaction{maxSequenceWork}); err == nil {
		t.Fatal("expected sequence exhaustion error")
	}

	allocator := newSequenceAllocator()
	if err := allocator.record("work-max", MaxSequence); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.allocate("work-next"); err == nil {
		t.Fatal("expected allocation after MaxSequence to fail")
	}
}
