package workqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
)

func validatePayloadNumbers(value any) error {
	switch value := value.(type) {
	case json.Number:
		number, err := strconv.ParseFloat(string(value), 64)
		if err != nil || math.Abs(number) > float64(MaxSequence) {
			return errors.New("work payload numbers must be finite and within the JavaScript-safe range; encode larger values as strings")
		}
	case []any:
		for _, item := range value {
			if err := validatePayloadNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := validatePayloadNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalTransaction(tx Transaction) ([]byte, error) {
	data, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type parsedRecord struct {
	transaction     Transaction
	missingSequence bool
}

func parseRecord(data []byte) (parsedRecord, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return parsedRecord{}, err
	}
	upgraded, err := upgradeMessage(raw)
	if err != nil {
		return parsedRecord{}, err
	}
	_, canonical := raw["work_id"]
	if !canonical {
		data, err = json.Marshal(upgraded)
		if err != nil {
			return parsedRecord{}, err
		}
	}
	var tx Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return parsedRecord{}, err
	}
	schemas, err := transactionSchemas()
	if err != nil {
		return parsedRecord{}, err
	}
	schema, ok := schemas[tx.Kind]
	if !ok {
		return parsedRecord{}, fmt.Errorf("unknown transaction kind %q", tx.Kind)
	}
	if err := schema.Validate(upgraded); err != nil {
		return parsedRecord{}, fmt.Errorf("invalid %s transaction: %w", tx.Kind, err)
	}
	if err := validateTransaction(tx); err != nil {
		return parsedRecord{}, err
	}
	return parsedRecord{transaction: tx, missingSequence: tx.Kind == "Work" && (!canonical || tx.Sequence == 0)}, nil
}

func assignHistoricalSequences(records []parsedRecord) ([]Transaction, error) {
	sequences := map[string]int64{}
	var maximum int64
	for _, record := range records {
		tx := record.transaction
		if tx.Kind == "Work" && !record.missingSequence {
			sequences[tx.WorkID] = tx.Sequence
			maximum = max(maximum, tx.Sequence)
		}
	}
	transactions := make([]Transaction, 0, len(records))
	for _, record := range records {
		tx := record.transaction
		if record.missingSequence {
			sequence, exists := sequences[tx.WorkID]
			if !exists {
				if maximum == MaxSequence {
					return nil, errors.New("work queue sequence exhausted")
				}
				maximum++
				sequence = maximum
				sequences[tx.WorkID] = sequence
			}
			tx.Sequence = sequence
		}
		tx.Version = CurrentVersion
		transactions = append(transactions, tx)
	}
	return transactions, nil
}

// Historical FIFO order can only be recovered from the first physical Work
// record. Persist it before any canonical sorting or duplicate removal.
func normalizeTransactions(transactions []Transaction) ([]Transaction, error) {
	result := slices.Clone(transactions)
	sequences := map[string]int64{}
	var maximum int64
	for _, tx := range result {
		if err := validateTransaction(tx); err != nil {
			return nil, err
		}
		if tx.Kind == "Work" && tx.Version == CurrentVersion {
			sequences[tx.WorkID] = tx.Sequence
			maximum = max(maximum, tx.Sequence)
		}
	}
	for i, tx := range result {
		if tx.Version != 0 {
			continue
		}
		tx.Version = CurrentVersion
		if tx.Kind == "Work" {
			sequence, ok := sequences[tx.WorkID]
			if !ok {
				if maximum == MaxSequence {
					return nil, errors.New("work queue sequence exhausted")
				}
				maximum++
				sequence = maximum
				sequences[tx.WorkID] = sequence
			}
			tx.Sequence = sequence
		}
		result[i] = tx
	}
	return result, nil
}

func nextSequence(transactions []Transaction) (int64, error) {
	var maximum int64
	for _, tx := range transactions {
		maximum = max(maximum, tx.Sequence)
	}
	if maximum >= MaxSequence {
		return 0, errors.New("work queue sequence exhausted")
	}
	return maximum + 1, nil
}

func upgradeMessage(raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, errors.New("transaction must be an object")
	}
	if _, legacy := raw["work_id"]; legacy {
		return raw, nil
	}
	version, exists := raw["version"]
	if exists && version != float64(0) && version != float64(1) {
		return nil, errors.New("unsupported dispatch coordinator transaction version")
	}
	if err := validateLegacyFields(raw, exists); err != nil {
		return nil, err
	}
	work, ok := raw["work"].(string)
	if !ok || work == "" {
		return nil, errors.New("legacy work identity must be a non-empty string")
	}
	kind, ok := raw["kind"].(string)
	if !ok {
		return nil, errors.New("legacy transaction kind is invalid")
	}
	result := map[string]any{"kind": kind, "work_id": work}
	switch kind {
	case "Work", "WorkCancellation":
		if raw["claim"] != nil || raw["attempt"] != nil {
			return nil, errors.New("legacy Work transaction must not include claim or attempt")
		}
		if kind == "Work" {
			result["work"] = map[string]any{"legacy_work_id": work}
			result["sequence"] = 1
		}
	case "Claim", "ClaimCancellation", "Completion":
		if err := upgradeLegacyClaim(raw, result, kind); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown transaction kind %q", kind)
	}
	result["version"] = CurrentVersion
	return result, nil
}

func validateLegacyFields(raw map[string]any, versioned bool) error {
	if len(raw) != 4 && (!versioned || len(raw) != 5) {
		return errors.New("legacy transaction must contain exactly kind, work, claim, attempt, and optional version")
	}
	for field := range raw {
		if field != "version" && field != "kind" && field != "work" && field != "claim" && field != "attempt" {
			return errors.New("legacy transaction contains an unknown field")
		}
	}
	if _, ok := raw["claim"]; !ok {
		return errors.New("legacy transaction is missing claim")
	}
	if _, ok := raw["attempt"]; !ok {
		return errors.New("legacy transaction is missing attempt")
	}
	return nil
}

func upgradeLegacyClaim(raw, result map[string]any, kind string) error {
	claim, ok := raw["claim"].(string)
	if !ok || claim == "" {
		return errors.New("legacy claim identity is invalid")
	}
	result["claim_id"] = claim
	if kind == "Claim" {
		result["run_id"] = "legacy:" + claim
	}
	if kind == "Completion" {
		attempt, ok := raw["attempt"].(string)
		if !ok || attempt == "" {
			return errors.New("legacy completion attempt is invalid")
		}
		result["attempt_id"] = attempt
	} else if raw["attempt"] != nil {
		return errors.New("legacy claim must not include attempt")
	}
	return nil
}
