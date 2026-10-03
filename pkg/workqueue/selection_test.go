package workqueue

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestOldestAvailableSharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/selection-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name  string `json:"name"`
		Works []struct {
			Task     string `json:"task"`
			Enqueued int64  `json:"enqueued"`
			State    string `json:"state"`
		} `json:"works"`
		Expected []string `json:"expected"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var log []Transaction
			ids := make(map[string]string)
			for _, item := range fixture.Works {
				payload, err := json.Marshal(map[string]string{"task": item.Task})
				if err != nil {
					t.Fatal(err)
				}
				work, err := NewWork(payload)
				if err != nil {
					t.Fatal(err)
				}
				work.Enqueued = item.Enqueued
				ids[item.Task] = work.WorkID
				log = append(log, work)
				claimID := "claim-" + item.Task
				if slices.Contains([]string{"claimed", "completed", "released"}, item.State) {
					log = append(log, Transaction{Kind: "Claim", WorkID: work.WorkID, ClaimID: claimID, RunID: "run-" + item.Task})
				}
				switch item.State {
				case "completed":
					log = append(log, Transaction{Kind: "Completion", WorkID: work.WorkID, ClaimID: claimID, AttemptID: "attempt-" + item.Task})
				case "cancelled":
					log = append(log, Transaction{Kind: "WorkCancellation", WorkID: work.WorkID})
				case "released":
					log = append(log, Transaction{Kind: "ClaimCancellation", WorkID: work.WorkID, ClaimID: claimID})
				}
			}
			expected := make([]string, 0, len(fixture.Expected))
			for _, task := range fixture.Expected {
				expected = append(expected, ids[task])
			}
			reversed := slices.Clone(log)
			slices.Reverse(reversed)
			compacted, err := Compact(log)
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := Serialize(log)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := Parse(serialized)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range [][]Transaction{log, reversed, append(slices.Clone(log), log...), compacted, parsed} {
				projection, err := Replay(input)
				if err != nil || !reflect.DeepEqual(projection.Available, expected) {
					t.Fatalf("available=%v, expected=%v, error=%v", projection.Available, expected, err)
				}
				selected, err := OldestAvailable(input)
				first := ""
				if len(expected) > 0 {
					first = expected[0]
				}
				if err != nil || selected != first {
					t.Fatalf("selected=%q, expected=%q, error=%v", selected, first, err)
				}
			}
		})
	}
}

func TestWorkEnqueueTimePreserved(t *testing.T) {
	before := time.Now().UnixMilli()
	work, err := NewWork([]byte(`{"task":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if work.Enqueued < before || work.Enqueued > time.Now().UnixMilli() {
		t.Fatalf("enqueue time was not captured at submission: %d", work.Enqueued)
	}
	first := work
	work.Enqueued++
	log, changed, err := Apply([]Transaction{first}, work)
	if err != nil || changed || !reflect.DeepEqual(log, []Transaction{first}) {
		t.Fatalf("resubmission changed age: %+v, changed=%t, error=%v", log, changed, err)
	}
	if _, err := Replay([]Transaction{first, work}); err == nil {
		t.Fatal("conflicting durable enqueue metadata was accepted")
	}
}

func TestEnqueueTimeValidation(t *testing.T) {
	work, _, _ := fixture(t)
	for _, enqueued := range []int64{-1, MaxEnqueued + 1} {
		work.Enqueued = enqueued
		if _, err := Replay([]Transaction{work}); err == nil {
			t.Errorf("accepted invalid enqueue time %d", enqueued)
		}
	}
	work.Enqueued = MaxEnqueued
	if _, err := Replay([]Transaction{work}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`null`, `"1"`, `0.5`, `9007199254740992`, `-1`} {
		work.Enqueued = 0
		encoded, err := json.Marshal(work)
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded[:len(encoded)-1], []byte(`,"enqueued":`+value+`}`)...)
		if _, err := Parse(encoded); err == nil {
			t.Errorf("parsed invalid enqueue time %s", value)
		}
	}
}

func TestQueueIdentityTieBreak(t *testing.T) {
	works := []WorkState{{WorkID: "\U00010000"}, {WorkID: "\uE000"}, {WorkID: "2"}, {WorkID: "10"}, {WorkID: "__proto__"}}
	slices.SortFunc(works, compareQueueWork)
	expected := []string{"10", "2", "__proto__", "\uE000", "\U00010000"}
	for i, work := range works {
		if work.WorkID != expected[i] {
			t.Fatalf("identity order differs from JavaScript: %+v", works)
		}
	}
}
