package workqueue

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestSelectionSharedConformance(t *testing.T) {
	data, err := os.ReadFile("../../specs/dispatch-work-coordinator/selection-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Transactions []Transaction
		Cases        []struct {
			Name      string
			Selection json.RawMessage
			Expected  *string
		}
		Invalid []json.RawMessage
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			selection, err := ParseSelection(test.Selection)
			if err != nil {
				t.Fatal(err)
			}
			variants := [][]Transaction{fixture.Transactions, slices.Clone(fixture.Transactions)}
			slices.Reverse(variants[1])
			compacted, err := Compact(fixture.Transactions)
			if err != nil {
				t.Fatal(err)
			}
			variants = append(variants, compacted)
			for _, transactions := range variants {
				next, err := SelectNext(transactions, selection)
				if err != nil {
					t.Fatal(err)
				}
				if test.Expected == nil {
					if next != nil {
						t.Fatalf("expected no eligible work, got %s", next.WorkID)
					}
				} else if next == nil || next.WorkID != *test.Expected {
					t.Fatalf("expected %s, got %+v", *test.Expected, next)
				}
			}
		})
	}
	for _, invalid := range fixture.Invalid {
		if _, err := ParseSelection(invalid); err == nil {
			t.Errorf("accepted invalid selection: %s", invalid)
		}
	}
}

func TestClaimNextRetriesSelectionAgainstLatestState(t *testing.T) {
	branch, mock := newQueueAPI(t)
	ctx := context.Background()
	first, _, _ := fixture(t)
	id, payload, err := WorkID([]byte(`{"task":"later"}`))
	if err != nil {
		t.Fatal(err)
	}
	second := Transaction{Kind: "Work", WorkID: id, Work: payload}
	if _, _, err := branch.Update(ctx, appendTransaction(first)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := branch.Update(ctx, appendTransaction(second)); err != nil {
		t.Fatal(err)
	}
	// Compaction must not substitute hash/physical order for submission order.
	if _, _, err := branch.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		compacted, err := Compact(current)
		return compacted, true, err
	}); err != nil {
		t.Fatal(err)
	}
	mock.conflicts = 1
	result, err := branch.ClaimNext(ctx, Selection{}, "next-claim", "next-run")
	if err != nil || result == nil || result.Work.WorkID != first.WorkID || result.Work.State != "claimed" {
		t.Fatalf("FIFO claim failed: %+v, %v", result, err)
	}
	result, err = branch.ClaimNext(ctx, Selection{}, "second-claim", "second-run")
	if err != nil || result == nil || result.Work.WorkID != second.WorkID {
		t.Fatalf("second claim failed: %+v, %v", result, err)
	}
	writes := mock.refWrites
	result, err = branch.ClaimNext(ctx, Selection{}, "empty-claim", "empty-run")
	if err != nil || result != nil || mock.refWrites != writes {
		t.Fatalf("empty queue published a claim: %+v, %v", result, err)
	}
}

func TestLegacyFIFOUpgrade(t *testing.T) {
	log := []byte(`{"kind":"Work","work":"z","claim":null,"attempt":null}
{"version":1,"kind":"Work","work":"a","claim":null,"attempt":null}
{"kind":"Work","work":"z","claim":null,"attempt":null}
{"version":1,"kind":"Claim","work":"a","claim":"c","attempt":null}
`)
	transactions, err := Parse(log)
	if err != nil {
		t.Fatal(err)
	}

	compacted, err := Compact(transactions)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Serialize(compacted)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	next, err := SelectNext(roundtrip, Selection{})
	if err != nil || next == nil || next.WorkID != "z" || next.Sequence != 1 {
		t.Fatalf("migration lost FIFO: %+v, %v", next, err)
	}
	if transactions[1].Sequence != 2 || transactions[3].RunID != "legacy:c" {
		t.Fatalf("invalid legacy metadata: %+v", transactions)
	}
}

func TestClaimNextReselectsAfterConcurrentClaim(t *testing.T) {
	branch, mock := newQueueAPI(t)
	ctx := context.Background()
	first, _, _ := fixture(t)
	id, payload, err := WorkID([]byte(`{"task":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	second := Transaction{Kind: "Work", WorkID: id, Work: payload}
	for _, tx := range []Transaction{first, second} {
		if _, _, err := branch.Update(ctx, appendTransaction(tx)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := branch.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	competing, _, err := Apply(current, Transaction{Kind: "Claim", WorkID: first.WorkID, ClaimID: "remote", RunID: "remote-run"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Serialize(competing)
	if err != nil {
		t.Fatal(err)
	}
	mock.conflicts = 1
	mock.onConflict = func() {
		mock.logs["remote-tree"] = string(encoded)
		mock.commits["remote-head"] = queueCommit{Tree: "remote-tree", Parents: []string{mock.head}}
		mock.head = "remote-head"
	}
	result, err := branch.ClaimNext(ctx, Selection{}, "local", "local-run")
	if err != nil || result == nil || result.Work.WorkID != second.WorkID {
		t.Fatalf("did not reselect after concurrent claim: %+v, %v", result, err)
	}
	latest, err := branch.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := Replay(latest)
	if err != nil || projection.Stats.Claimed != 2 {
		t.Fatalf("lost concurrent claim: %+v, %v", projection, err)
	}
}
