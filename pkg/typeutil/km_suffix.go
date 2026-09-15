package typeutil

import (
	"math"
	"strconv"
	"strings"
)

// ParseInt64KMSuffix parses a positive base-10 integer string with an optional
// K/k (×1,000) or M/m (×1,000,000) suffix.
func ParseInt64KMSuffix(raw string) (int64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}

	multiplier := int64(1)
	switch last := trimmed[len(trimmed)-1]; last {
	case 'k', 'K':
		multiplier = 1_000
		trimmed = trimmed[:len(trimmed)-1]
	case 'm', 'M':
		multiplier = 1_000_000
		trimmed = trimmed[:len(trimmed)-1]
	}

	if trimmed == "" {
		return 0, false
	}

	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed <= 0 {
		typeutilLog.Printf("Rejected K/M-suffixed value %q: not a positive base-10 integer", raw)
		return 0, false
	}
	if parsed > math.MaxInt64/multiplier {
		typeutilLog.Printf("Rejected K/M-suffixed value %q: would overflow int64 (multiplier=%d)", raw, multiplier)
		return 0, false
	}
	return parsed * multiplier, true
}

// NormalizeInt64KMSuffix returns a canonical base-10 string for a positive
// integer string with an optional K/k or M/m suffix.
func NormalizeInt64KMSuffix(raw string) (string, bool) {
	parsed, ok := ParseInt64KMSuffix(raw)
	if !ok {
		return "", false
	}
	return strconv.FormatInt(parsed, 10), true
}
