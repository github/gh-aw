package workflow

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var aicLimitLog = logger.New("workflow:aic_limit_parser")

// parseFrontmatterAICLimit preserves the existing typed threshold API while
// accepting credit-specific suffixes and the daily guardrail's object form.
func parseFrontmatterAICLimit(data json.RawMessage, allowObject bool) (*TemplatableInt32, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	if allowObject {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err == nil {
			const objectValueError = "object 'value' must be a positive integer, a K/M suffix string, or a GitHub Actions expression. Example: max-daily-ai-credits: {value: 20K}; use scalar max-daily-ai-credits: -1 to disable the guardrail"
			value, ok := object["value"]
			if !ok || string(value) == "null" {
				return nil, errors.New(objectValueError)
			}
			limit, err := parseFrontmatterAICLimit(value, false)
			if err != nil || limit == nil || limit.String() == "-1" {
				return nil, errors.New(objectValueError)
			}
			return limit, nil
		}
	}
	var number int64
	if err := json.Unmarshal(data, &number); err == nil && (number > 0 || number == -1) {
		limit := TemplatableInt32(strconv.FormatInt(number, 10))
		return &limit, nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		if value == "-1" || isExpression(value) {
			limit := TemplatableInt32(value)
			return &limit, nil
		}
		if normalized, ok := normalizePositiveAICLimit(value); ok {
			limit := TemplatableInt32(normalized)
			return &limit, nil
		}
	}
	return nil, errors.New("expected a positive integer, a K/M suffix string, a GitHub Actions expression, or -1")
}

// normalizePositiveAICLimit converts positive integer-like values
// into a canonical base-10 string.
//
// Supported inputs:
//   - positive integers
//   - positive numeric strings with optional K/M suffixes
//
// K/M suffix strings are expanded to plain base-10 (for example, "100M"
// becomes "100000000").
//
// It returns the normalized base-10 value and true when parsing succeeds.
// It returns an empty string and false when the value is not a valid positive
// AI Credits limit.
func normalizePositiveAICLimit(raw any) (string, bool) {
	if val, ok := typeutil.ParseIntValue(raw); ok && val > 0 {
		return strconv.Itoa(val), true
	}

	rawStr, ok := raw.(string)
	if !ok {
		aicLimitLog.Printf("Rejecting AI Credits limit: unsupported type %T", raw)
		return "", false
	}

	trimmed := strings.TrimSpace(rawStr)
	if trimmed == "" {
		return "", false
	}

	normalized, ok := typeutil.NormalizeInt64KMSuffix(trimmed)
	if !ok {
		aicLimitLog.Print("Rejecting AI Credits limit: not a valid positive value")
		return "", false
	}
	aicLimitLog.Print("Normalized AI Credits limit")
	return normalized, true
}

// parseMaxAICLimitValue parses an AI Credits limit from either an
// integer, -1 string sentinel, or positive K/M-suffixed string.
//
// It returns the parsed limit value and a success boolean. A false success
// value means the input was not supported.
func parseMaxAICLimitValue(raw any) (int64, bool) {
	if val, ok := typeutil.ParseIntValue(raw); ok && val != 0 {
		return int64(val), true
	}

	rawStr, ok := raw.(string)
	if !ok {
		return 0, false
	}

	trimmed := strings.TrimSpace(rawStr)
	if trimmed == "-1" {
		aicLimitLog.Print("Parsed AI Credits limit sentinel -1 (unlimited)")
		return -1, true
	}

	parsed, ok := typeutil.ParseInt64KMSuffix(trimmed)
	if !ok {
		aicLimitLog.Printf("Rejecting AI Credits limit: %q is not a supported value", trimmed)
		return 0, false
	}
	return parsed, true
}
