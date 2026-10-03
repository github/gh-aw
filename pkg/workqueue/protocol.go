package workqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

type sequenceAllocator struct {
	sequences map[string]int64
	owners    map[int64]string
	maximum   int64
}

func newSequenceAllocator() *sequenceAllocator {
	return &sequenceAllocator{
		sequences: make(map[string]int64),
		owners:    make(map[int64]string),
	}
}

func (allocator *sequenceAllocator) record(workID string, sequence int64) error {
	if sequence < 1 || sequence > MaxSequence {
		return errors.New("work sequence must be a positive safe integer")
	}
	if existing, ok := allocator.sequences[workID]; ok {
		if existing != sequence {
			return fmt.Errorf("work %s has conflicting sequences", workID)
		}
		return nil
	}
	if owner, ok := allocator.owners[sequence]; ok && owner != workID {
		return fmt.Errorf("work sequence %d is shared by Work %s and %s", sequence, owner, workID)
	}
	allocator.sequences[workID] = sequence
	allocator.owners[sequence] = workID
	allocator.maximum = max(allocator.maximum, sequence)
	return nil
}

func (allocator *sequenceAllocator) allocate(workID string) (int64, error) {
	if sequence, ok := allocator.sequences[workID]; ok {
		return sequence, nil
	}
	sequence, err := allocator.next()
	if err != nil {
		return 0, err
	}
	if err := allocator.record(workID, sequence); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (allocator *sequenceAllocator) next() (int64, error) {
	if allocator.maximum >= MaxSequence {
		return 0, errors.New("work queue sequence exhausted")
	}
	allocator.maximum++
	return allocator.maximum, nil
}

var versionCodemods = [...]struct{ from, to int }{{2, 3}}

func upgradeTransactionVersion(tx Transaction) Transaction {
	for _, codemod := range versionCodemods {
		if tx.Version == codemod.from {
			tx.Version = codemod.to
		}
	}
	return tx
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
	tx = upgradeTransactionVersion(tx)
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
	allocator := newSequenceAllocator()
	for _, record := range records {
		tx := record.transaction
		if tx.Kind == "Work" && !record.missingSequence {
			if err := allocator.record(tx.WorkID, tx.Sequence); err != nil {
				return nil, err
			}
		}
	}
	transactions := make([]Transaction, 0, len(records))
	for _, record := range records {
		tx := record.transaction
		if record.missingSequence {
			sequence, err := allocator.allocate(tx.WorkID)
			if err != nil {
				return nil, err
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
	allocator := newSequenceAllocator()
	for i, tx := range result {
		tx = upgradeTransactionVersion(tx)
		result[i] = tx
		if err := validateTransaction(tx); err != nil {
			return nil, err
		}
		if tx.Kind == "Work" && tx.Version == CurrentVersion {
			if err := allocator.record(tx.WorkID, tx.Sequence); err != nil {
				return nil, err
			}
		}
	}
	for i, tx := range result {
		if tx.Version != 0 {
			continue
		}
		tx.Version = CurrentVersion
		if tx.Kind == "Work" {
			sequence, err := allocator.allocate(tx.WorkID)
			if err != nil {
				return nil, err
			}
			tx.Sequence = sequence
		}
		result[i] = tx
	}
	return result, nil
}

func nextSequence(transactions []Transaction) (int64, error) {
	allocator := newSequenceAllocator()
	for _, tx := range transactions {
		if tx.Kind == "Work" && tx.Sequence > 0 {
			if err := allocator.record(tx.WorkID, tx.Sequence); err != nil {
				return 0, err
			}
		}
	}
	return allocator.next()
}

func upgradeMessage(raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, errors.New("transaction must be an object")
	}
	if _, canonical := raw["work_id"]; canonical {
		return upgradeCanonicalMessage(raw), nil
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

func upgradeCanonicalMessage(raw map[string]any) map[string]any {
	upgraded := maps.Clone(raw)
	for _, codemod := range versionCodemods {
		if upgraded["version"] == float64(codemod.from) {
			upgraded["version"] = float64(codemod.to)
		}
	}
	if upgraded["version"] == float64(0) {
		upgraded["version"] = float64(CurrentVersion)
	}
	return upgraded
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
