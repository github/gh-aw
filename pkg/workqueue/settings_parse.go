package workqueue

import (
	"encoding/json"
	"fmt"
)

func decodeSettingsScalars(fields map[string]json.RawMessage, settings *Settings) error {
	if value, ok := fields["mode"]; ok {
		var mode string
		if json.Unmarshal(value, &mode) != nil ||
			(mode != "weighted-priority" && mode != "strict-priority") {
			return settingsError("work_queue.mode", "must be weighted-priority or strict-priority")
		}
		settings.Mode = mode
	}
	for _, field := range []string{"concurrency", "pending_limit"} {
		if value, ok := fields[field]; ok {
			target := &settings.Concurrency
			if field == "pending_limit" {
				target = &settings.PendingLimit
			}
			number, err := settingsInteger(value, "work_queue."+field, 4096)
			if err != nil {
				return err
			}
			*target = number
		}
	}
	return nil
}

func parseSettingsClassWeights(data []byte) ([]int, error) {
	var values []json.RawMessage
	if json.Unmarshal(data, &values) != nil || len(values) != 5 {
		return nil, settingsError("work_queue.class_weights", "must contain exactly five integer weights in 1..1000")
	}
	weights := make([]int, 0, len(values))
	for index, raw := range values {
		weight, err := settingsInteger(raw, fmt.Sprintf("work_queue.class_weights[%d]", index), 1000)
		if err != nil {
			return nil, err
		}
		weights = append(weights, weight)
	}
	return weights, nil
}

func parseSettingsAccountingWeights(data []byte) (map[string]int, error) {
	values, err := settingsObject(data, "work_queue.accounting_weights", nil)
	if err != nil {
		return nil, err
	}
	if len(values) > 1024 {
		return nil, settingsError("work_queue.accounting_weights", "must contain at most 1024 keys")
	}
	weights := map[string]int{"": 1}
	for _, key := range sortedSettingsKeys(values) {
		property := "work_queue.accounting_weights." + key
		if !validKey(key) {
			return nil, settingsError(property, "must be an accounting key of at most 128 UTF-8 bytes without control characters")
		}
		weight, err := settingsInteger(values[key], property, 1000)
		if err != nil {
			return nil, err
		}
		if key == "" && weight != 1 {
			return nil, settingsError(property, "the default accounting key must have weight 1")
		}
		weights[key] = weight
	}
	if len(weights) > 1024 {
		return nil, settingsError("work_queue.accounting_weights", "must contain at most 1024 keys including the default key")
	}
	return weights, nil
}

func parseSettingsPools(data []byte, settings Settings) (map[string]PoolSettings, error) {
	values, err := settingsObject(data, "work_queue.pools", nil)
	if err != nil {
		return nil, err
	}
	if len(values) > 64 {
		return nil, settingsError("work_queue.pools", "must contain at most 64 scheduling pools")
	}
	pools := map[string]PoolSettings{}
	for _, name := range sortedSettingsKeys(values) {
		property := "work_queue.pools." + name
		if !validIdentity(name, false, 256) {
			return nil, settingsError(property, "pool name must be a nonempty identity of at most 256 UTF-8 bytes")
		}
		pool, err := parsePoolSettings(values[name], property, PoolSettings{
			Concurrency: settings.Concurrency, Retry: settings.Retry,
		})
		if err != nil {
			return nil, err
		}
		pools[name] = pool
	}
	return pools, nil
}

func parsePoolSettings(data []byte, property string, defaults PoolSettings) (PoolSettings, error) {
	fields, err := settingsObject(data, property, []string{"concurrency", "per_account_limit", "retry"})
	if err != nil {
		return PoolSettings{}, err
	}
	for _, field := range []string{"concurrency", "per_account_limit"} {
		if raw, ok := fields[field]; ok {
			target := &defaults.Concurrency
			if field == "per_account_limit" {
				target = &defaults.PerAccountLimit
			}
			value, err := settingsInteger(raw, property+"."+field, 4096)
			if err != nil {
				return PoolSettings{}, err
			}
			*target = value
		}
	}
	if raw, ok := fields["retry"]; ok {
		defaults.Retry, err = parseSettingsRetry(raw, property+".retry", defaults.Retry)
		if err != nil {
			return PoolSettings{}, err
		}
	}
	return defaults, nil
}
