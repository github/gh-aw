package workqueue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// IssuesSettings describes optional Issues projection, not queue authority.
// Nil Settings.Issues means disabled; true or {} uses the default label and title prefix.
type IssuesSettings struct {
	Label             string `json:"label"`
	TitlePrefix       string `json:"title_prefix"`
	OrphanedAfterDays int    `json:"orphaned_after_days"`
}

func parseIssuesSettings(data []byte) (*IssuesSettings, error) {
	switch string(bytes.TrimSpace(data)) {
	case "false":
		return nil, nil
	case "true":
		return &IssuesSettings{Label: "work", TitlePrefix: "[work queue]", OrphanedAfterDays: 30}, nil
	}
	fields, err := settingsObject(data, "work_queue.issues", []string{"label", "title_prefix", "orphaned_after_days"})
	if err != nil {
		return nil, fmt.Errorf("work_queue.issues: use true, false, or an object with optional literal label, title_prefix, and orphaned_after_days: %w", err)
	}
	issues := &IssuesSettings{Label: "work", TitlePrefix: "[work queue]", OrphanedAfterDays: 30}
	if data, exists := fields["label"]; exists {
		var label string
		if json.Unmarshal(data, &label) != nil || strings.TrimSpace(label) == "" || len(label) > 33 ||
			strings.Contains(label, "${{") || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return nil, settingsError("work_queue.issues.label", "must be a nonblank literal of at most 33 UTF-8 bytes without control characters or expressions")
		}
		issues.Label = label
	}
	if data, exists := fields["title_prefix"]; exists {
		var prefix string
		if json.Unmarshal(data, &prefix) != nil || strings.TrimSpace(prefix) == "" || len(prefix) > 80 ||
			strings.Contains(prefix, "${{") || strings.IndexFunc(prefix, unicode.IsControl) >= 0 {
			return nil, settingsError("work_queue.issues.title_prefix", "must be a nonblank literal of at most 80 UTF-8 bytes without control characters or expressions")
		}
		issues.TitlePrefix = prefix
	}
	if data, exists := fields["orphaned_after_days"]; exists {
		days, err := settingsInteger(data, "work_queue.issues.orphaned_after_days", 3650)
		if err != nil {
			return nil, err
		}
		issues.OrphanedAfterDays = days
	}
	return issues, nil
}

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
