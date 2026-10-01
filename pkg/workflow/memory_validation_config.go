package workflow

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"

	"github.com/github/gh-aw/pkg/logger"
)

var memoryValidationLog = logger.New("workflow:memory_validation_config")

const (
	defaultMemoryValidationTimeoutMinutes = 1
	maxMemoryValidationTimeoutMinutes     = 5
)

type MemoryValidationConfig struct {
	Script         string `yaml:"script,omitempty"`
	TimeoutMinutes int    `yaml:"timeout-minutes,omitempty"`
}

func parseMemoryValidationConfig(configMap map[string]any, fieldPath string) (*MemoryValidationConfig, error) {
	raw, ok := configMap["validation"]
	if !ok {
		if script, ok := configMap["validation-script"].(string); ok {
			memoryValidationLog.Printf("Using legacy validation-script field at %s", fieldPath)
			return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: script}, fieldPath)
		}
		if script, ok := configMap["custom-validation"].(string); ok {
			memoryValidationLog.Printf("Using legacy custom-validation field at %s", fieldPath)
			return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: script}, fieldPath)
		}
		return nil, nil
	}

	switch value := raw.(type) {
	case string:
		return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: value}, fieldPath)
	case map[string]any:
		if _, exists := value["timeout"]; exists {
			memoryValidationLog.Printf("Rejecting deprecated timeout field at %s", fieldPath)
			return nil, fmt.Errorf("%s.timeout has been renamed to %s.timeout-minutes. Example:\n%s:\n  timeout-minutes: 1", fieldPath, fieldPath, fieldPath)
		}
		config := &MemoryValidationConfig{}
		if script, ok := value["script"].(string); ok {
			config.Script = script
		}
		if timeout, exists := value["timeout-minutes"]; exists {
			parsed, err := parseMemoryValidationTimeoutMinutes(timeout, fieldPath+".timeout-minutes")
			if err != nil {
				return nil, err
			}
			config.TimeoutMinutes = parsed
		}
		return normalizeMemoryValidationConfig(config, fieldPath)
	default:
		return nil, fmt.Errorf("%s must be an object with script and optional timeout-minutes, or a script string. Example:\n%s:\n  script: \"throw new Error('invalid state')\"\n  timeout-minutes: 1", fieldPath, fieldPath)
	}
}

func parseMemoryValidationTimeoutMinutes(value any, fieldPath string) (int, error) {
	switch v := value.(type) {
	case int:
		return validateMemoryValidationTimeoutMinutes(v, fieldPath)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
		}
		if v < 1 || v > maxMemoryValidationTimeoutMinutes {
			return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(int(v), fieldPath)
	case uint64:
		if v > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(int(v), fieldPath)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(parsed, fieldPath)
	default:
		return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
	}
}

func validateMemoryValidationTimeoutMinutes(timeout int, fieldPath string) (int, error) {
	if timeout < 1 || timeout > maxMemoryValidationTimeoutMinutes {
		return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
	}
	return timeout, nil
}

func normalizeMemoryValidationConfig(config *MemoryValidationConfig, fieldPath string) (*MemoryValidationConfig, error) {
	if config == nil {
		return nil, nil
	}
	if config.Script == "" {
		return nil, fmt.Errorf("%s.script must not be empty. Example:\n%s:\n  script: \"throw new Error('invalid state')\"", fieldPath, fieldPath)
	}
	if config.TimeoutMinutes == 0 {
		memoryValidationLog.Printf("Applying default timeout of %d minute(s) at %s", defaultMemoryValidationTimeoutMinutes, fieldPath)
		config.TimeoutMinutes = defaultMemoryValidationTimeoutMinutes
	}
	return config, nil
}

func memoryValidationTimeoutSeconds(config *MemoryValidationConfig) int {
	return config.TimeoutMinutes * 60
}

func memoryValidationScriptBase64(config *MemoryValidationConfig) string {
	if config == nil || config.Script == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(config.Script))
}

func memoryValidationStepID(prefix, memoryID string) string {
	return fmt.Sprintf("%s_%x", prefix, memoryID)
}
