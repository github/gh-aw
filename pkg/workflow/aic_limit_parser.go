package workflow

import (
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var aicLimitLog = logger.New("workflow:aic_limit_parser")

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
		aicLimitLog.Printf("Rejecting AI Credits limit: %q is not a valid positive value", trimmed)
		return "", false
	}
	aicLimitLog.Printf("Normalized AI Credits limit %q to %s", trimmed, normalized)
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
