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
	Mode              string                  `json:"mode"`
	ClassWeights      []int                   `json:"class_weights"`
	AccountingWeights map[string]int          `json:"accounting_weights"`
	Concurrency       int                     `json:"concurrency"`
	PendingLimit      int                     `json:"pending_limit"`
	Retry             SettingsRetry           `json:"retry"`
	Pools             map[string]PoolSettings `json:"pools"`
	Issues            *IssuesSettings         `json:"issues,omitempty"`
}

type SettingsRetry struct {
	MaxAttempts    int `json:"max_attempts"`
	BackoffSeconds int `json:"backoff_seconds"`
}

// PoolSettings overrides scheduling while inheriting host-approved worker routes.
type PoolSettings struct {
	Concurrency     int           `json:"concurrency"`
	PerAccountLimit int           `json:"per_account_limit,omitempty"`
	Retry           SettingsRetry `json:"retry"`
}

func defaultSettings() Settings {
	return Settings{
		Mode: "weighted-priority", ClassWeights: []int{8, 4, 2, 1, 1},
		AccountingWeights: map[string]int{"": 1}, Concurrency: 16, PendingLimit: 4096,
		Retry: SettingsRetry{MaxAttempts: 3, BackoffSeconds: 30},
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
	object, err := settingsObject(data, "work_queue", []string{
		"mode", "class_weights", "accounting_weights", "concurrency", "pending_limit", "retry", "pools", "issues",
	})
	if err != nil {
		return Settings{}, err
	}
	if err := decodeSettingsScalars(object, &settings); err != nil {
		return Settings{}, err
	}
	if value, ok := object["class_weights"]; ok {
		settings.ClassWeights, err = parseSettingsClassWeights(value)
		if err != nil {
			return Settings{}, err
		}
	}
	if value, ok := object["accounting_weights"]; ok {
		settings.AccountingWeights, err = parseSettingsAccountingWeights(value)
		if err != nil {
			return Settings{}, err
		}
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
			return nil, settingsError(property+"."+key, "unknown scheduling property; identities, routes, trust, credentials and producers are managed by AW")
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
	fields, err := settingsObject(data, property, []string{"max_attempts", "backoff_seconds"})
	if err != nil {
		return SettingsRetry{}, err
	}
	for _, field := range []string{"max_attempts", "backoff_seconds"} {
		if raw, ok := fields[field]; ok {
			maximum := 16
			target := &defaults.MaxAttempts
			if field == "backoff_seconds" {
				maximum, target = 3600, &defaults.BackoffSeconds
			}
			*target, err = settingsInteger(raw, property+"."+field, maximum)
			if err != nil {
				return SettingsRetry{}, err
			}
		}
	}
	return defaults, nil
}

func sortedSettingsKeys[T any](value map[string]T) []string {
	keys := slices.Collect(maps.Keys(value))
	slices.Sort(keys)
	return keys
}

// Apply copies only scheduling preferences onto host-approved policy routes.
// Issues preferences remain available to the host and never enroll projectors.
// Additional scheduling pools inherit the approved default pool; no profile
// authority is authored.
// Full policy validation remains the host's responsibility after binding templates.
func (settings Settings) Apply(policy Policy) (Policy, error) {
	data, err := json.Marshal(settings)
	if err != nil {
		return Policy{}, settingsError("work_queue", "settings cannot be encoded")
	}
	settings, err = ParseSettings(data)
	if err != nil {
		return Policy{}, err
	}
	if len(policy.Pools) == 0 {
		return Policy{}, settingsError("work_queue.pools", "AW must approve at least one worker pool before scheduling settings can apply")
	}
	policy.Pools = maps.Clone(policy.Pools)
	policy.Producers = maps.Clone(policy.Producers)
	for principal, rule := range policy.Producers {
		rule.Pools = slices.Clone(rule.Pools)
		rule.Priorities = slices.Clone(rule.Priorities)
		rule.FairnessKeys = slices.Clone(rule.FairnessKeys)
		policy.Producers[principal] = rule
	}
	policy.Projectors = slices.Clone(policy.Projectors)
	for index := range policy.Projectors {
		policy.Projectors[index].Pools = slices.Clone(policy.Projectors[index].Pools)
		policy.Projectors[index].Repositories = slices.Clone(policy.Projectors[index].Repositories)
		policy.Projectors[index].BackingIssues = slices.Clone(policy.Projectors[index].BackingIssues)
	}
	policy.Mode = settings.Mode
	policy.ClassWeights = slices.Clone(settings.ClassWeights)
	policy.AccountingWeights = maps.Clone(settings.AccountingWeights)
	policy.Limits.PendingNodes = settings.PendingLimit
	for _, name := range sortedSettingsKeys(settings.Pools) {
		if _, exists := policy.Pools[name]; exists {
			continue
		}
		template, exists := policy.Pools["default"]
		if !exists {
			return Policy{}, settingsError("work_queue.pools."+name, "additional scheduling pools require an AW-approved default pool to inherit worker routes")
		}
		policy.Pools[name] = template
	}
	if len(policy.Pools) > 64 {
		return Policy{}, settingsError("work_queue.pools", "resolved policy must contain at most 64 scheduling pools")
	}
	for _, name := range sortedSettingsKeys(policy.Pools) {
		pool := policy.Pools[name]
		preferences := PoolSettings{Concurrency: settings.Concurrency, Retry: settings.Retry}
		if override, exists := settings.Pools[name]; exists {
			preferences = override
		}
		pool.LogicalLimit, pool.NativeLimit = preferences.Concurrency, preferences.Concurrency
		pool.PerAccountLimit = preferences.PerAccountLimit
		pool.Retry = RetryPolicy{MaxAttempts: preferences.Retry.MaxAttempts, BackoffMS: int64(preferences.Retry.BackoffSeconds) * 1000}
		pool.Profiles = maps.Clone(pool.Profiles)
		pool.AllowedRepositories = slices.Clone(pool.AllowedRepositories)
		for profileName, profile := range pool.Profiles {
			profile.MaxClaims, profile.ShareKeys = 1, false
			pool.Profiles[profileName] = profile
		}
		policy.Pools[name] = pool
	}
	return policy, nil
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
