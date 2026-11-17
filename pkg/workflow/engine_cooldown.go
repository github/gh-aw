package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// resolveEngineCooldownDays applies the compiler-managed engine installation policy.
func resolveEngineCooldownDays(data *WorkflowData) int {
	if data != nil && data.EngineConfig != nil && data.EngineConfig.Cooldown != nil {
		return *data.EngineConfig.Cooldown
	}
	return npmDefaultCooldownDays
}

func applyEngineCooldownField(config *EngineConfig, obj map[string]any) {
	raw, exists := obj["cooldown"]
	if !exists {
		return
	}
	var days int
	valid := false
	switch value := raw.(type) {
	case int:
		days, valid = value, true
	case int64:
		days = int(value)
		valid = int64(days) == value
	case uint64:
		if value <= uint64(math.MaxInt) {
			days, valid = int(value), true
		}
	case float64:
		if value >= 0 && value < float64(math.MaxInt) && math.Trunc(value) == value {
			days, valid = int(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseInt(string(value), 10, 0)
		if err == nil {
			days, valid = int(parsed), true
		}
	}
	if !valid || days < 0 {
		config.cooldownError = NewValidationError("engine.cooldown", fmt.Sprint(raw),
			"expected a nonnegative integer number of days",
			"Set engine.cooldown to an integer such as 3; booleans, strings, and negative values are not supported.")
		return
	}
	config.Cooldown = &days
}

func validateEngineCooldown(config *EngineConfig) error {
	if config == nil {
		return nil
	}
	if config.cooldownError != nil {
		return config.cooldownError
	}
	if config.Cooldown != nil && (*config.Cooldown < 0 ||
		(*config.Cooldown == 0 && strings.Contains(config.Version, "${{"))) {
		return NewValidationError("engine.cooldown", strconv.Itoa(*config.Cooldown),
			"cooldown must be nonnegative; zero requires a statically known engine version",
			"Use a positive integer number of days, or use zero only with the compiler-pinned default or a literal engine.version; Actions expressions cannot be used with zero.")
	}
	return nil
}
