package workqueue

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const FileName = "dispatch-work-coordinator.jsonl"
const DefaultBranch = "gh-aw-dispatch-work-coordinator"

//go:embed schema/*.json
var schemas embed.FS
var transactionSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	kinds := []string{"Work", "Claim", "ClaimCancellation", "WorkCancellation", "Completion"}
	for _, kind := range kinds {
		name := kind + "Transaction.json"
		data, err := schemas.ReadFile("schema/" + name)
		if err != nil {
			return nil, err
		}
		var resource any
		if err := json.Unmarshal(data, &resource); err != nil {
			return nil, err
		}
		if err := compiler.AddResource(name, resource); err != nil {
			return nil, err
		}
	}
	result := make(map[string]*jsonschema.Schema)
	for _, kind := range kinds {
		schema, err := compiler.Compile(kind + "Transaction.json")
		if err != nil {
			return nil, err
		}
		result[kind] = schema
	}
	return result, nil
})

type Transaction struct {
	Kind      string          `json:"kind"`
	WorkID    string          `json:"work_id"`
	Work      json.RawMessage `json:"work,omitempty"`
	ClaimID   string          `json:"claim_id,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	AttemptID string          `json:"attempt_id,omitempty"`
	Outcome   string          `json:"outcome,omitempty"`
}

type WorkState struct {
	WorkID  string          `json:"work_id"`
	Work    json.RawMessage `json:"work"`
	State   string          `json:"state"`
	Winner  string          `json:"winner,omitempty"`
	Claims  []ClaimState    `json:"claims"`
	Outcome string          `json:"outcome,omitempty"`
}

type ClaimState struct {
	ClaimID string `json:"claim_id"`
	RunID   string `json:"run_id"`
	State   string `json:"state"`
}

type Stats struct {
	Work         int `json:"work"`
	Available    int `json:"available"`
	Claimed      int `json:"claimed"`
	Completed    int `json:"completed"`
	Cancelled    int `json:"cancelled"`
	Claims       int `json:"claims"`
	Transactions int `json:"transactions"`
}

type Projection struct {
	Works []WorkState `json:"works"`
	Stats Stats       `json:"stats"`
}

func WorkID(payload []byte) (string, json.RawMessage, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return "", nil, errors.New("work payload must be a JSON object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", nil, errors.New("work payload must be a JSON object")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), canonical, nil
}

func validateTransaction(tx Transaction) error {
	schemas, err := transactionSchemas()
	if err != nil {
		return err
	}
	if schema, ok := schemas[tx.Kind]; ok {
		data, err := json.Marshal(tx)
		if err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if err := schema.Validate(value); err != nil {
			return fmt.Errorf("invalid %s transaction: %w", tx.Kind, err)
		}
	} else {
		return fmt.Errorf("unknown transaction kind %q", tx.Kind)
	}
	if tx.WorkID == "" || (tx.ClaimID == "" && (tx.Kind == "Claim" || tx.Kind == "ClaimCancellation" || tx.Kind == "Completion")) {
		return errors.New("transaction identifiers cannot be empty")
	}
	if tx.Kind == "Claim" && tx.RunID == "" || tx.Kind == "Completion" && tx.AttemptID == "" {
		return errors.New("transaction provenance cannot be empty")
	}
	if tx.Kind == "Work" {
		id, _, err := WorkID(tx.Work)
		if err != nil || id != tx.WorkID {
			return errors.New("work_id does not match canonical work payload")
		}
	}
	return nil
}

func Parse(data []byte) ([]Transaction, error) {
	var transactions []Transaction
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		var tx Transaction
		if err := json.Unmarshal([]byte(line), &tx); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		canonical, err := json.Marshal(tx)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(canonical, &fields); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		for key := range raw {
			if _, ok := fields[key]; !ok {
				return nil, fmt.Errorf("line %d: unknown field %q", i+1, key)
			}
		}
		if err := validateTransaction(tx); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		transactions = append(transactions, tx)
	}
	return transactions, nil
}

func Serialize(transactions []Transaction) ([]byte, error) {
	var b strings.Builder
	for _, tx := range transactions {
		if err := validateTransaction(tx); err != nil {
			return nil, err
		}
		line, err := json.Marshal(tx)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

type replayFacts struct {
	works           map[string]Transaction
	claims          map[string]Transaction
	cancelledClaims map[string]struct{}
	cancelledWorks  map[string]struct{}
	completions     map[string]Transaction
}

func collectFacts(transactions []Transaction) (replayFacts, int, error) {
	f := replayFacts{
		works: map[string]Transaction{}, claims: map[string]Transaction{},
		cancelledClaims: map[string]struct{}{}, cancelledWorks: map[string]struct{}{},
		completions: map[string]Transaction{},
	}
	facts := map[string]struct{}{}
	for _, tx := range transactions {
		if err := validateTransaction(tx); err != nil {
			return f, 0, err
		}
		b, err := json.Marshal(tx)
		if err != nil {
			return f, 0, err
		}
		key := string(b)
		if _, exists := facts[key]; exists {
			continue
		}
		facts[key] = struct{}{}
		switch tx.Kind {
		case "Work":
			if _, exists := f.works[tx.WorkID]; exists {
				return f, 0, fmt.Errorf("conflicting Work %s", tx.WorkID)
			}
			f.works[tx.WorkID] = tx
		case "Claim":
			if _, exists := f.claims[tx.ClaimID]; exists {
				return f, 0, fmt.Errorf("conflicting Claim %s", tx.ClaimID)
			}
			f.claims[tx.ClaimID] = tx
		case "ClaimCancellation":
			if _, exists := f.cancelledClaims[tx.ClaimID]; exists {
				return f, 0, fmt.Errorf("conflicting cancellation %s", tx.ClaimID)
			}
			f.cancelledClaims[tx.ClaimID] = struct{}{}
		case "WorkCancellation":
			if _, exists := f.cancelledWorks[tx.WorkID]; exists {
				return f, 0, fmt.Errorf("conflicting cancellation %s", tx.WorkID)
			}
			f.cancelledWorks[tx.WorkID] = struct{}{}
		case "Completion":
			if _, exists := f.completions[tx.WorkID]; exists {
				return f, 0, fmt.Errorf("multiple completions for %s", tx.WorkID)
			}
			f.completions[tx.WorkID] = tx
		}
	}
	return f, len(facts), validateReferences(f, transactions)
}

func validateReferences(f replayFacts, transactions []Transaction) error {
	for _, tx := range transactions {
		if tx.Kind != "Work" {
			if _, ok := f.works[tx.WorkID]; !ok {
				return fmt.Errorf("%s references missing Work %s", tx.Kind, tx.WorkID)
			}
		}
		if tx.Kind == "ClaimCancellation" || tx.Kind == "Completion" {
			claim, ok := f.claims[tx.ClaimID]
			if !ok || claim.WorkID != tx.WorkID {
				return fmt.Errorf("%s references missing Claim %s", tx.Kind, tx.ClaimID)
			}
		}
	}
	return nil
}

func Replay(transactions []Transaction) (Projection, error) {
	result := Projection{Works: []WorkState{}}
	f, count, err := collectFacts(transactions)
	if err != nil {
		return result, err
	}
	result.Stats.Transactions = count
	for id, tx := range f.works {
		state := WorkState{WorkID: id, Work: tx.Work, State: "available", Claims: []ClaimState{}}
		for _, claim := range f.claims {
			if claim.WorkID != id {
				continue
			}
			item := ClaimState{ClaimID: claim.ClaimID, RunID: claim.RunID, State: "superseded"}
			if _, cancelled := f.cancelledClaims[claim.ClaimID]; cancelled {
				item.State = "cancelled"
			} else if state.Winner == "" || claim.ClaimID < state.Winner {
				state.Winner = claim.ClaimID
			}
			state.Claims = append(state.Claims, item)
		}
		slices.SortFunc(state.Claims, func(a, b ClaimState) int { return strings.Compare(a.ClaimID, b.ClaimID) })
		if completion, ok := f.completions[id]; ok {
			_, cancelled := f.cancelledWorks[id]
			if cancelled || state.Winner != completion.ClaimID {
				return result, fmt.Errorf("completion for %s is not owned by the effective Claim", id)
			}
			state.State, state.Outcome = "completed", completion.Outcome
		} else if _, cancelled := f.cancelledWorks[id]; cancelled {
			state.State, state.Winner = "cancelled", ""
		} else if state.Winner != "" {
			state.State = "claimed"
		}
		updatedClaims := make([]ClaimState, 0, len(state.Claims))
		for _, claim := range state.Claims {
			if claim.ClaimID == state.Winner {
				claim.State = "effective"
			}
			updatedClaims = append(updatedClaims, claim)
		}
		state.Claims = updatedClaims
		result.Works = append(result.Works, state)
		switch state.State {
		case "available":
			result.Stats.Available++
		case "claimed":
			result.Stats.Claimed++
		case "completed":
			result.Stats.Completed++
		case "cancelled":
			result.Stats.Cancelled++
		}
	}
	slices.SortFunc(result.Works, func(a, b WorkState) int { return strings.Compare(a.WorkID, b.WorkID) })
	result.Stats.Work, result.Stats.Claims = len(f.works), len(f.claims)
	return result, nil
}

func Apply(transactions []Transaction, tx Transaction) ([]Transaction, bool, error) {
	current, err := Replay(transactions)
	if err != nil {
		return nil, false, err
	}
	if err := validateTransaction(tx); err != nil {
		return nil, false, err
	}
	for _, existing := range transactions {
		a, err := json.Marshal(existing)
		if err != nil {
			return nil, false, err
		}
		b, err := json.Marshal(tx)
		if err != nil {
			return nil, false, err
		}
		if bytes.Equal(a, b) {
			return transactions, false, nil
		}
	}
	var state *WorkState
	for _, work := range current.Works {
		if work.WorkID == tx.WorkID {
			state = &work
			break
		}
	}
	if tx.Kind == "Work" {
		if state != nil {
			return nil, false, fmt.Errorf("work %s already exists", tx.WorkID)
		}
	} else if state == nil {
		return nil, false, fmt.Errorf("work %s does not exist", tx.WorkID)
	} else if state.State == "cancelled" || state.State == "completed" {
		return nil, false, fmt.Errorf("work %s is terminal", tx.WorkID)
	}
	if tx.Kind == "ClaimCancellation" || tx.Kind == "Completion" {
		found := false
		for _, claim := range state.Claims {
			if claim.ClaimID == tx.ClaimID {
				found = true
			}
		}
		if !found {
			return nil, false, fmt.Errorf("claim %s does not exist on work %s", tx.ClaimID, tx.WorkID)
		}
	}
	if tx.Kind == "Completion" && state.Winner != tx.ClaimID {
		return nil, false, fmt.Errorf("claim %s is not effective", tx.ClaimID)
	}
	next := append(append([]Transaction(nil), transactions...), tx)
	if _, err := Replay(next); err != nil {
		return nil, false, err
	}
	return next, true, nil
}

func Compact(transactions []Transaction) ([]Transaction, error) {
	if _, err := Replay(transactions); err != nil {
		return nil, err
	}
	unique := map[string]Transaction{}
	for _, tx := range transactions {
		data, err := json.Marshal(tx)
		if err != nil {
			return nil, err
		}
		unique[string(data)] = tx
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]Transaction, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return result, nil
}
