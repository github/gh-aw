package dispatchcoordinator

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// Claim is an immutable assertion of ownership of a Work item. The producer
// must bind RunID to the originating workflow run, not to agent-supplied input.
type Claim struct {
	ID       string
	WorkID   string
	Claimant string
	RunID    string
}

// ClaimCancellation withdraws one Claim without withdrawing competing Claims.
type ClaimCancellation struct {
	ID      string
	WorkID  string
	ClaimID string
}

// Arbitration is the projection of the currently known Claim facts for WorkID.
// Superseded contains only live competing Claims, ordered by ID.
type Arbitration struct {
	WorkID     string
	Effective  *Claim
	Superseded []Claim
}

// ArbitrateClaims selects the lexicographically smallest live Claim ID for a
// Work item. Fact order, timestamps, runner order and duplicate identical facts
// cannot affect the result. A cancellation takes effect regardless of whether
// its Claim has arrived yet. Conflicting uses of a fact ID are rejected.
//
// Callers must refresh the complete fact set before each decision. This pure
// projection does not persist facts or authorize external effects: safe_outputs
// must revalidate against shared state under an atomic publish boundary.
func ArbitrateClaims(workID string, claims []Claim, cancellations []ClaimCancellation) (Arbitration, error) {
	result := Arbitration{WorkID: workID}
	if workID == "" {
		return result, errors.New("dispatch work ID is required")
	}

	byID := make(map[string]Claim, len(claims))
	for _, claim := range claims {
		if claim.ID == "" || claim.WorkID == "" || claim.Claimant == "" || claim.RunID == "" {
			return result, errors.New("dispatch claim requires ID, work ID, claimant and run ID")
		}
		if previous, exists := byID[claim.ID]; exists && previous != claim {
			return result, fmt.Errorf("conflicting dispatch claim ID %q", claim.ID)
		}
		byID[claim.ID] = claim
	}

	cancelByID := make(map[string]ClaimCancellation, len(cancellations))
	cancelled := make(map[string]struct{}, len(cancellations))
	for _, cancellation := range cancellations {
		if cancellation.ID == "" || cancellation.WorkID == "" || cancellation.ClaimID == "" {
			return result, errors.New("dispatch claim cancellation requires ID, work ID and claim ID")
		}
		if previous, exists := cancelByID[cancellation.ID]; exists && previous != cancellation {
			return result, fmt.Errorf("conflicting dispatch claim cancellation ID %q", cancellation.ID)
		}
		cancelByID[cancellation.ID] = cancellation
		if claim, exists := byID[cancellation.ClaimID]; exists && claim.WorkID != cancellation.WorkID {
			return result, fmt.Errorf("dispatch claim cancellation %q has mismatched work ID", cancellation.ID)
		}
		if cancellation.WorkID == workID {
			cancelled[cancellation.ClaimID] = struct{}{}
		}
	}

	live := make([]Claim, 0, len(byID))
	for _, claim := range byID {
		_, withdrawn := cancelled[claim.ID]
		if claim.WorkID == workID && !withdrawn {
			live = append(live, claim)
		}
	}
	slices.SortFunc(live, func(a, b Claim) int { return cmp.Compare(a.ID, b.ID) })
	if len(live) > 0 {
		winner := live[0]
		result.Effective = &winner
		result.Superseded = live[1:]
	}
	return result, nil
}
