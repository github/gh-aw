package workflow

import (
	"github.com/github/gh-aw/pkg/typeutil"
)

// parseBaseSafeOutputConfig parses common fields (max, github-token, github-app, staged) from a config map.
// If defaultMax is provided (> 0), it will be set as the default value for config.Max
// before parsing the max field from configMap. Supports both integer values and GitHub
// Actions expression strings (e.g. "${{ inputs.max }}").
func (c *Compiler) parseBaseSafeOutputConfig(configMap map[string]any, config *BaseSafeOutputConfig, defaultMax int) {
	// Set default max if provided
	if defaultMax > 0 {
		safeOutputsConfigLog.Printf("Setting default max: %d", defaultMax)
		config.Max = defaultIntStr(defaultMax)
	}

	parseSafeOutputMax(configMap, config)

	parseSafeOutputCredentials(configMap, config)
	parseSafeOutputStagedAndSamples(configMap, config)
}

func parseSafeOutputMax(configMap map[string]any, config *BaseSafeOutputConfig) {
	// Parse max (this will override the default if present in configMap)
	if max, exists := configMap["max"]; exists {
		switch v := max.(type) {
		case string:
			// Accept GitHub Actions expression strings
			if isExpression(v) {
				safeOutputsConfigLog.Printf("Parsed max as GitHub Actions expression: %s", v)
				config.Max = &v
			}
		default:
			// Convert integer/float64/etc to string via typeutil.ParseIntValue
			if maxInt, ok := typeutil.ParseIntValue(max); ok {
				safeOutputsConfigLog.Printf("Parsed max as integer: %d", maxInt)
				s := defaultIntStr(maxInt)
				config.Max = s
			}
		}
	}
}

func parseSafeOutputCredentials(configMap map[string]any, config *BaseSafeOutputConfig) {
	// Parse github-token
	if githubToken, exists := configMap["github-token"]; exists {
		if githubTokenStr, ok := githubToken.(string); ok {
			safeOutputsConfigLog.Print("Parsed custom github-token from config")
			config.GitHubToken = githubTokenStr
		}
	}

	// Parse github-app (per-handler GitHub App credentials for token minting)
	if app, exists := configMap["github-app"]; exists {
		if appMap, ok := app.(map[string]any); ok {
			safeOutputsConfigLog.Print("Parsed custom github-app from config")
			config.GitHubApp = parseAppConfig(appMap)
		}
	}
}

func parseSafeOutputStagedAndSamples(configMap map[string]any, config *BaseSafeOutputConfig) {
	// Parse staged flag (per-handler staged mode)
	if err := preprocessBoolFieldAsString(configMap, "staged", safeOutputsConfigLog); err != nil {
		safeOutputsConfigLog.Printf("Invalid staged value: %v", err)
	} else if staged, exists := configMap["staged"]; exists {
		if stagedStr, ok := staged.(string); ok && stagedStr != "" {
			safeOutputsConfigLog.Printf("Parsed staged flag: %s", stagedStr)
			value := TemplatableBool(stagedStr)
			config.Staged = &value
		}
	}

	// Parse samples list (hidden feature: deterministic replay samples for --use-samples).
	// Accepts either a YAML list of objects, or a single object that is auto-wrapped
	// into a one-element list. The JSON schema rejects scalar/string shapes so we
	// don't need a defensive YAML-string branch here.
	if samples, exists := configMap["samples"]; exists {
		parsed := parseSamplesValue(samples)
		if len(parsed) > 0 {
			safeOutputsConfigLog.Printf("Parsed %d samples entries", len(parsed))
			config.Samples = parsed
		}
	}
}

// parseSamplesValue normalizes a `samples` frontmatter value into a list of
// objects. Accepted shapes:
//   - YAML list of mappings: returned as-is
//   - single YAML mapping: wrapped into a one-element list
//
// Any other shape returns an empty slice — schema validation rejects those
// shapes upstream and we keep this parser strict to match.
func parseSamplesValue(samples any) []map[string]any {
	switch v := samples.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			} else if mStr, ok := item.(map[string]string); ok {
				converted := make(map[string]any, len(mStr))
				for k, s := range mStr {
					converted[k] = s
				}
				out = append(out, converted)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{v}
	default:
		return nil
	}
}
