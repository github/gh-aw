package workqueue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"unicode"
	"unicode/utf8"
)

// Settings contains scheduling and optional Issues projection preferences,
// never queue authorization or routes.
// ParseSettings accepts the raw work_queue property from .github/workflows/aw.json.
type Settings struct {
	Concurrency  int                     `json:"concurrency"`
	PendingLimit int                     `json:"pending_limit"`
	Retry        SettingsRetry           `json:"retry"`
	Pools        map[string]PoolSettings `json:"pools"`
	Issues       *IssuesSettings         `json:"issues,omitempty"`
}

type SettingsRetry struct {
	MaxAttempts int `json:"max_attempts"`
}

// PoolSettings overrides scheduling for one pool. PerAccountLimit 0 means unlimited.
type PoolSettings struct {
	Concurrency     int           `json:"concurrency"`
	PerAccountLimit int           `json:"per_account_limit,omitempty"`
	Retry           SettingsRetry `json:"retry"`
}

func defaultSettings() Settings {
	return Settings{
		Concurrency: 16, PendingLimit: 4096,
		Retry: SettingsRetry{MaxAttempts: 3},
		Pools: map[string]PoolSettings{},
	}
}

// ParseSettings resolves omitted preferences to the ordinary singleton queue defaults.
// A missing section is nil/empty bytes; explicit null or malformed input is an error.
func ParseSettings(data []byte) (Settings, error) {
	settings := defaultSettings()
	if len(data) == 0 {
		return settings, nil
	}
	object, err := settingsObject(data, "work_queue", []string{"concurrency", "pending_limit", "retry", "pools", "issues"})
	if err != nil {
		return Settings{}, err
	}
	if err := decodeSettingsScalars(object, &settings); err != nil {
		return Settings{}, err
	}
	if value, ok := object["retry"]; ok {
		settings.Retry, err = parseSettingsRetry(value, "work_queue.retry", settings.Retry)
		if err != nil {
			return Settings{}, err
		}
	}
	if value, ok := object["pools"]; ok {
		settings.Pools, err = parseSettingsPools(value, settings)
		if err != nil {
			return Settings{}, err
		}
	}
	if value, exists := object["issues"]; exists {
		settings.Issues, err = parseIssuesSettings(value)
		if err != nil {
			return Settings{}, err
		}
	}
	if _, err := decodeStrict(data); err != nil {
		return Settings{}, settingsError("work_queue", fmt.Sprintf("must be valid scheduling JSON: %v", err))
	}
	return settings, nil
}

func settingsError(property, message string) error {
	return fmt.Errorf("%s: %s; update .github/workflows/aw.json", property, message)
}

func settingsObject(data []byte, property string, allowed []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, settingsError(property, "must be a JSON object, not null, an array, or a scalar")
	}
	result := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, settingsError(property, "must be valid JSON")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, settingsError(property+"."+key, "duplicate property is not allowed")
		}
		if allowed != nil && !slices.Contains(allowed, key) {
			return nil, settingsError(property+"."+key, "unknown scheduling property; worker routes and credentials are managed by AW")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, settingsError(property+"."+key, "must be valid JSON")
		}
		result[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, settingsError(property, "must be valid JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, settingsError(property, "must contain exactly one JSON object")
	}
	return result, nil
}

func settingsInteger(data []byte, property string, maximum int) (int, error) {
	var value int
	if json.Unmarshal(data, &value) != nil || value < 1 || value > maximum {
		return 0, settingsError(property, fmt.Sprintf("must be an integer in 1..%d", maximum))
	}
	return value, nil
}

func parseSettingsRetry(data []byte, property string, defaults SettingsRetry) (SettingsRetry, error) {
	fields, err := settingsObject(data, property, []string{"max_attempts"})
	if err != nil {
		return SettingsRetry{}, err
	}
	if raw, ok := fields["max_attempts"]; ok {
		defaults.MaxAttempts, err = settingsInteger(raw, property+".max_attempts", 16)
		if err != nil {
			return SettingsRetry{}, err
		}
	}
	return defaults, nil
}

func sortedSettingsKeys[T any](value map[string]T) []string {
	keys := slices.Collect(maps.Keys(value))
	slices.Sort(keys)
	return keys
}

// ParseRepositorySettings extracts only work_queue; other aw.json properties are
// owned by the repository configuration parser.
func ParseRepositorySettings(data []byte) (Settings, error) {
	if data == nil {
		return ParseSettings(nil)
	}
	object, err := settingsObject(data, "aw.json", nil)
	if err != nil {
		return Settings{}, fmt.Errorf("work_queue: cannot read scheduling configuration: %w", err)
	}
	return ParseSettings(object["work_queue"])
}

func validIdentity(value string, allowEmpty bool, maximum int) bool {
	if (!allowEmpty && value == "") || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
