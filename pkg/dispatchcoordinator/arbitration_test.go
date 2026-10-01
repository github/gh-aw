package dispatchcoordinator

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestArbitrateClaimsCompetingAndPermutation(t *testing.T) {
	claims := []Claim{
		{ID: "b", WorkID: "work", Claimant: "runner-b", RunID: "2"},
		{ID: "a", WorkID: "work", Claimant: "runner-a", RunID: "1"},
		{ID: "c", WorkID: "work", Claimant: "runner-c", RunID: "3"},
		{ID: "other", WorkID: "different", Claimant: "other", RunID: "4"},
	}
	want, err := ArbitrateClaims("work", claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want.Effective == nil || want.Effective.ID != "a" || len(want.Superseded) != 2 ||
		want.Superseded[0].ID != "b" || want.Superseded[1].ID != "c" {
		t.Fatalf("unexpected arbitration: %+v", want)
	}
	for seed := range 100 {
		shuffled := append([]Claim(nil), claims...)
		rand.New(rand.NewSource(int64(seed))).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got, err := ArbitrateClaims("work", shuffled, nil)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: got %+v, %v; want %+v", seed, got, err, want)
		}
	}
}

func TestArbitrateClaimsCancellationAndDeduplication(t *testing.T) {
	a := Claim{ID: "a", WorkID: "work", Claimant: "runner-a", RunID: "1"}
	b := Claim{ID: "b", WorkID: "work", Claimant: "runner-b", RunID: "2"}
	cancel := ClaimCancellation{ID: "cancel-a", WorkID: "work", ClaimID: "a"}
	for _, cancellations := range [][]ClaimCancellation{{cancel}, {cancel, cancel}} {
		got, err := ArbitrateClaims("work", []Claim{b, a, a}, cancellations)
		if err != nil || got.Effective == nil || got.Effective.ID != "b" || len(got.Superseded) != 0 {
			t.Fatalf("cancellation: got %+v, %v", got, err)
		}
	}

	// An out-of-order cancellation must also withdraw a Claim when it arrives.
	got, err := ArbitrateClaims("work", []Claim{a}, []ClaimCancellation{cancel})
	if err != nil || got.Effective != nil {
		t.Fatalf("out-of-order cancellation: got %+v, %v", got, err)
	}
	got, err = ArbitrateClaims("work", nil, []ClaimCancellation{cancel})
	if err != nil || got.Effective != nil {
		t.Fatalf("unresolved cancellation: got %+v, %v", got, err)
	}
}

func TestArbitrateClaimsRejectsConflictingFacts(t *testing.T) {
	a := Claim{ID: "a", WorkID: "work", Claimant: "runner-a", RunID: "1"}
	tests := []struct {
		name          string
		workID        string
		claims        []Claim
		cancellations []ClaimCancellation
	}{
		{"missing work ID", "", nil, nil},
		{"missing provenance", "work", []Claim{{ID: "a", WorkID: "work", Claimant: "runner-a"}}, nil},
		{"claim ID collision", "work", []Claim{a, {ID: "a", WorkID: "work", Claimant: "other", RunID: "2"}}, nil},
		{"cross-work claim ID collision", "work", []Claim{a, {ID: "a", WorkID: "other", Claimant: "runner-a", RunID: "1"}}, nil},
		{"cancellation ID collision", "work", []Claim{a}, []ClaimCancellation{
			{ID: "cancel", WorkID: "work", ClaimID: "a"},
			{ID: "cancel", WorkID: "work", ClaimID: "b"},
		}},
		{"cross-work cancellation", "work", []Claim{a}, []ClaimCancellation{{ID: "cancel", WorkID: "other", ClaimID: "a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ArbitrateClaims(tt.workID, tt.claims, tt.cancellations); err == nil {
				t.Fatal("expected invalid fact set to fail closed")
			}
		})
	}
}
