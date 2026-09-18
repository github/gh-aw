package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/typeutil"
)

// parseMaxAICreditsValue parses max-ai-credits from either integer
// or numeric-string frontmatter values.
//
// A return value of 0 is a sentinel that means "not configured" (missing or
// invalid); explicit zero is not a valid user value. Negative values (-1) are
// passed through as-is and signal that budget enforcement and token steering
// should be disabled.
func parseMaxAICreditsValue(raw any) int64 {
	if parsed, ok := parseMaxAICLimitValue(raw); ok {
		return parsed
	}
	if raw != nil {
		engineLog.Printf("Ignoring invalid max-ai-credits value of type %T: %v", raw, raw)
	}
	return 0
}

// parseMaxRunsValue parses max-runs from either integer or numeric-string
// frontmatter values.
func parseMaxRunsValue(raw any) int {
	return parsePositiveIntValue(raw, "max-runs")
}

// parseMaxTurnCacheMissesValue parses max-turn-cache-misses from either integer or
// numeric-string frontmatter values.
func parseMaxTurnCacheMissesValue(raw any) int {
	return parsePositiveIntValue(raw, "max-turn-cache-misses")
}

// parsePositiveIntValue parses a strictly-positive integer from raw.
// Delegates to parseIntOrExpressionValue for a single int-validation path.
// GitHub Actions expression strings (e.g. "${{ inputs.value }}") are silently
// treated as 0 (not configured) because these fields are integer-only.
func parsePositiveIntValue(raw any, fieldName string) int {
	s := parseIntOrExpressionValue(raw, 1, fieldName)
	if s == "" || isExpression(s) {
		return 0
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return val
}

func parseMaxTurnsValue(raw any) string {
	return parseIntOrExpressionValue(raw, 1, "max-turns")
}

// parseHarnessMaxRetriesValue parses harness.max-retries from a raw frontmatter value
// that must be a non-negative integer (≥ 0) or a GitHub Actions expression template (${{ ... }}).
// It is intentionally distinct from parseMaxTurnsValue which rejects zero.
// Returns the canonical string representation, or "" when the value is absent/invalid.
func parseHarnessMaxRetriesValue(raw any) string {
	return parseIntOrExpressionValue(raw, 0, "harness.max-retries")
}

// parseHarnessWatchdogTimeoutValue parses harness.watchdog-timeout (seconds)
// and converts it to milliseconds for GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS.
// Accepts a positive integer (converted seconds→ms) or a GitHub Actions expression
// template (${{ ... }}), which is passed through unchanged and must already be in ms.
func parseHarnessWatchdogTimeoutValue(raw any) string {
	seconds := parseIntOrExpressionValue(raw, 1, "harness.watchdog-timeout")
	if seconds == "" {
		return ""
	}
	// GitHub Actions expressions do not support arithmetic operators; pass through
	// unchanged. Callers using an expression must supply a value already in ms.
	if _, ok := extractWrappedGitHubExpression(seconds); ok {
		return seconds
	}
	parsedSeconds, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil {
		engineLog.Printf("Ignoring invalid harness.watchdog-timeout value: %q", seconds)
		return ""
	}
	const maxInt64Div1000 = int64((1<<63)-1) / 1000
	if parsedSeconds > maxInt64Div1000 {
		engineLog.Printf("Ignoring out-of-range harness.watchdog-timeout value: %q", seconds)
		return ""
	}
	return strconv.FormatInt(parsedSeconds*1000, 10)
}

func parseIntOrExpressionValue(raw any, minValue int, fieldName string) string {
	if val, ok := typeutil.ParseIntValue(raw); ok && val >= minValue {
		return strconv.Itoa(val)
	}
	if rawStr, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(rawStr)
		if trimmed == "" {
			return ""
		}
		if parsed, err := strconv.Atoi(trimmed); err == nil && parsed >= minValue {
			return strconv.Itoa(parsed)
		}
		// Match the same GitHub Actions expression wrapper accepted by the schema.
		// The schema and GitHub Actions runtime are responsible for validating the
		// expression body itself; this helper only needs to preserve templated values.
		if isExpression(trimmed) {
			return trimmed
		}
		engineLog.Printf("Ignoring invalid %s value: %q", fieldName, rawStr)
	}
	return ""
}

func parseMaxToolDenialsValue(raw any) string {
	return parseIntOrExpressionValue(raw, 1, "max-tool-denials")
}

// parseAuthDefinition converts a raw auth config map (from engine.provider.auth) into
// an AuthDefinition. It is backward-compatible: a map with only a "secret" key produces
// an AuthDefinition with Strategy="" and Secret set (callers normalise Strategy to api-key).
func parseAuthDefinition(authObj map[string]any) *AuthDefinition {
	def := &AuthDefinition{}
	if err := decodeEngineConfig(authObj, def); err != nil {
		engineLog.Printf("Ignoring invalid engine provider auth configuration: %v", err)
	}
	return def
}

// parseEngineAuthConfig converts a raw engine.auth config map into EngineAuthConfig.
func parseEngineAuthConfig(authObj map[string]any) *EngineAuthConfig {
	auth := &EngineAuthConfig{}
	if err := decodeEngineConfig(authObj, auth); err != nil {
		engineLog.Printf("Ignoring invalid engine auth configuration: %v", err)
	}
	return auth
}

// parseRequestShape converts a raw request config map (from engine.provider.request) into
// a RequestShape.
func parseRequestShape(requestObj map[string]any) *RequestShape {
	shape := &RequestShape{}
	if err := decodeEngineConfig(requestObj, shape); err != nil {
		engineLog.Printf("Ignoring invalid engine provider request configuration: %v", err)
	}
	return shape
}

// decodeEngineConfig decodes config into target, which must be a non-nil pointer to a
// struct whose fields carry `json` tags describing the recognized keys. Unknown keys
// are ignored for backward compatibility, but recognized keys with an explicit JSON
// null value or an incompatible type are rejected. On error, target is left untouched
// (it is never partially populated from a malformed configuration).
func decodeEngineConfig(config map[string]any, target any) error {
	if err := rejectNullKnownFields(config, target); err != nil {
		return err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal configuration: %w", err)
	}
	// Decode into a fresh value of the same underlying type so a decode failure
	// partway through never leaves target with a partially-populated struct.
	fresh := reflect.New(reflect.TypeOf(target).Elem())
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(fresh.Interface()); err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	reflect.ValueOf(target).Elem().Set(fresh.Elem())
	return nil
}

// rejectNullKnownFields explicitly rejects an explicit JSON null for any key that maps
// to a recognized struct field (encoding/json otherwise silently accepts null for
// scalar and map fields, turning them into zero values without an error). It also
// checks one level of nested map values (e.g. RequestShape.Query / BodyInject) since
// those are declared as map[string]string and null entries would otherwise be dropped
// silently as well. Unknown keys are left untouched for compatibility.
func rejectNullKnownFields(config map[string]any, target any) error {
	t := reflect.TypeOf(target)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for _, field := range reflect.VisibleFields(t) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		rawVal, present := config[name]
		if !present {
			continue
		}
		if rawVal == nil {
			return fmt.Errorf("field %q must not be null", name)
		}
		if field.Type.Kind() != reflect.Map {
			continue
		}
		nested, ok := rawVal.(map[string]any)
		if !ok {
			continue
		}
		for key, val := range nested {
			if val == nil {
				return fmt.Errorf("field %q.%q must not be null", name, key)
			}
		}
	}
	return nil
}
