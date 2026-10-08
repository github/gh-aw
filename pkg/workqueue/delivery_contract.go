package workqueue

import (
	"encoding/json"
	"slices"
)

func closedDeliveryObject(value any, required, optional []string) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, field := range required {
		if _, exists := object[field]; !exists {
			return nil, false
		}
	}
	for field := range object {
		if !slices.Contains(required, field) && !slices.Contains(optional, field) {
			return nil, false
		}
	}
	return object, true
}

func validateDeliveryContract(payload json.RawMessage) error {
	value, err := decodeStrict(payload)
	if err != nil {
		return queueError("delivery_contract_required", "immutable Work requires a supported canonical effect_contract")
	}
	work, ok := value.(map[string]any)
	if !ok {
		return queueError("delivery_contract_required", "immutable Work requires a declared effect_contract")
	}
	if alias, ok := closedDeliveryObject(work["effect_contract"], []string{"kind"}, nil); ok {
		if alias["kind"] == "none" {
			return nil
		}
	}
	contract, ok := closedDeliveryObject(work["effect_contract"], []string{"version", "outputs"}, []string{"no_writes"})
	if !ok || contract["version"] != json.Number("1") {
		return queueError("delivery_contract_required", "effect_contract requires supported closed version 1 or kind none")
	}
	outputs, ok := contract["outputs"].([]any)
	if !ok || len(outputs) > 128 {
		return queueError("delivery_contract_required", "effect_contract requires at most 128 declared outputs")
	}
	noWrites := false
	if value, declared := contract["no_writes"]; declared {
		noWrites, ok = value.(bool)
		if !ok {
			return queueError("delivery_contract_required", "effect_contract no_writes must be boolean")
		}
	}
	if len(outputs) == 0 && !noWrites || len(outputs) != 0 && noWrites {
		return queueError("delivery_contract_required", "empty effect_contract outputs require explicit no_writes without contradictory outputs")
	}
	seen := make(map[string]struct{}, len(outputs))
	for _, value := range outputs {
		if err := validateDeliveryOutput(value, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateDeliveryOutput(value any, seen map[string]struct{}) error {
	output, ok := closedDeliveryObject(value, []string{"type", "min", "max"}, []string{"verification"})
	if !ok {
		return queueError("delivery_contract_required", "effect_contract output requires closed type and min/max bounds")
	}
	name, ok := output["type"].(string)
	if _, exists := seen[name]; !ok || name == "" || exists {
		return queueError("delivery_contract_required", "effect_contract output types must be nonempty and unique")
	}
	seen[name] = struct{}{}
	minimum, minOK := output["min"].(json.Number)
	maximum, maxOK := output["max"].(json.Number)
	if !minOK || !maxOK {
		return queueError("delivery_contract_required", "effect_contract output bounds must be canonical integers")
	}
	minCount, minErr := minimum.Int64()
	maxCount, maxErr := maximum.Int64()
	if minErr != nil || maxErr != nil || minCount < 0 || maxCount > 128 || minCount > maxCount {
		return queueError("delivery_contract_required", "effect_contract output bounds must satisfy 0 <= min <= max <= 128")
	}
	if value, declared := output["verification"]; declared {
		verification, ok := closedDeliveryObject(value, []string{"verifier_id", "expected"}, nil)
		if !ok {
			return queueError("delivery_contract_required", "effect_contract verification requires closed verifier_id and expected")
		}
		id, ok := verification["verifier_id"].(string)
		if !ok || !reasonPattern.MatchString(id) {
			return queueError("delivery_contract_required", "effect_contract verifier_id must be a bounded ASCII code")
		}
		if _, ok := verification["expected"].(map[string]any); !ok {
			return queueError("delivery_contract_required", "effect_contract expected verification intent must be a canonical object")
		}
	}
	return nil
}
