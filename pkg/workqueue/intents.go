package workqueue

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

func prepareIntent(transactions []Transaction, tx Transaction) (Transaction, bool, error) {
	if tx.Version != 0 {
		return tx, false, nil
	}
	if err := validateTransaction(tx); err != nil {
		return tx, false, err
	}
	tx.Version = CurrentVersion
	if tx.Kind != "Work" {
		return tx, false, nil
	}
	for _, existing := range transactions {
		if existing.Kind != "Work" || existing.WorkID != tx.WorkID {
			continue
		}
		var a, b any
		if err := json.Unmarshal(existing.Work, &a); err != nil {
			return tx, false, err
		}
		if err := json.Unmarshal(tx.Work, &b); err != nil {
			return tx, false, err
		}
		tx.Sequence = existing.Sequence
		return tx, reflect.DeepEqual(a, b), nil
	}
	sequence, err := nextSequence(transactions)
	tx.Sequence = sequence
	return tx, false, err
}

func validateIntentState(projection Projection, tx Transaction) error {
	var state *WorkState
	for _, work := range projection.Works {
		if work.WorkID == tx.WorkID {
			state = &work
			break
		}
	}
	if tx.Kind == "Work" {
		if state != nil {
			return fmt.Errorf("work %s already exists", tx.WorkID)
		}
		return nil
	}
	if state == nil {
		return fmt.Errorf("work %s does not exist", tx.WorkID)
	}
	if state.State == "cancelled" || state.State == "completed" {
		return fmt.Errorf("work %s is terminal", tx.WorkID)
	}
	if tx.Kind == "ClaimCancellation" || tx.Kind == "Completion" {
		if !slices.ContainsFunc(state.Claims, func(claim ClaimState) bool { return claim.ClaimID == tx.ClaimID }) {
			return fmt.Errorf("claim %s does not exist on work %s", tx.ClaimID, tx.WorkID)
		}
	}
	if tx.Kind == "Completion" && state.Winner != tx.ClaimID {
		return fmt.Errorf("claim %s is not effective", tx.ClaimID)
	}
	return nil
}

func projectedClaims(state WorkState) []ClaimState {
	claims := make([]ClaimState, 0, len(state.Claims))
	for _, claim := range state.Claims {
		if state.State == "cancelled" {
			claim.State = "cancelled"
		} else if claim.ClaimID == state.Winner {
			claim.State = "effective"
		}
		claims = append(claims, claim)
	}
	return claims
}
