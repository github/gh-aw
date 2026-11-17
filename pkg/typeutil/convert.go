// Package typeutil provides helpers for converting untyped values and extracting
// typed data from map[string]any structures.
//
// The package includes numeric conversion utilities (in this file) and
// map-extraction helpers (in lookup.go), plus related parsing helpers used by
// the workflow engine.
//
// This file specifically focuses on safe conversion of heterogeneous any values
// that represent a single numeric quantity.
//
// # Key Functions
//
// Strict Conversions (return (value, ok) to distinguish zero from missing/invalid):
//   - ParseIntValue() - Strictly parse numeric types to int; returns (value, ok). Use when
//     the caller needs to distinguish "missing/invalid" from a zero value, or when string
//     inputs are not expected (e.g. YAML config field parsing).
//
// Safe Conversions (return 0 on overflow or invalid input):
//   - SafeUint64ToInt() - Convert uint64 to int, returning 0 on overflow
//   - SafeUintToInt() - Convert uint to int, returning 0 on overflow
//
// Lenient Conversions (also handle strings, return 0 on failure):
//   - ConvertToInt() - Leniently convert any value (int/int64/float64/string) to int,
//     returning 0 on failure. Use for heterogeneous sources such as JSON metrics,
//     log-parsed data, or user-provided strings where a zero default is acceptable.
//   - ConvertToFloat() - Safely convert any value (float64/int/int64/string) to float64,
//     returning 0 on failure.
package typeutil

import (
	"math"
	"strconv"

	"github.com/github/gh-aw/pkg/logger"
)

var typeutilLog = logger.New("typeutil:convert")

// ParseIntValue strictly parses numeric types (int, int64, uint64, float64) to int,
// returning (value, true) on success and (0, false) for any unrecognized or
// non-numeric type. Float values that are NaN or outside the int range are invalid.
//
// Use this when the caller needs to distinguish a missing/invalid value from a
// legitimate zero, or when string inputs are not expected (e.g. YAML config field
// parsing where the YAML library has already produced a typed numeric value).
//
// For lenient conversion that also handles string inputs and returns 0 on failure,
// use ConvertToInt instead.
func ParseIntValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		// Check for overflow before converting uint64 to int
		const maxInt = int(^uint(0) >> 1)
		if v > uint64(maxInt) {
			typeutilLog.Printf("uint64 value %d exceeds max int value, returning 0", v)
			return 0, false
		}
		return int(v), true
	case float64:
		intLimit := math.Ldexp(1, strconv.IntSize-1)
		if math.IsNaN(v) || v < -intLimit || v >= intLimit {
			return 0, false
		}
		intVal := int(v)
		// Warn if truncation occurs (value has fractional part)
		if v != float64(intVal) {
			typeutilLog.Printf("Float value %.2f truncated to integer %d", v, intVal)
		}
		return intVal, true
	default:
		return 0, false
	}
}

// SafeUint64ToInt converts uint64 to int, returning 0 if overflow would occur.
func SafeUint64ToInt(u uint64) int {
	if u > math.MaxInt {
		return 0 // Return 0 (engine default) if value would overflow
	}
	return int(u)
}

// SafeUintToInt converts uint to int, returning 0 if overflow would occur.
// This is a thin wrapper around SafeUint64ToInt that widens the uint argument first.
func SafeUintToInt(u uint) int { return SafeUint64ToInt(uint64(u)) }

// ConvertToInt leniently converts any value to int, returning 0 on failure.
//
// Unlike ParseIntValue, this function also handles string inputs via strconv.Atoi,
// making it suitable for heterogeneous sources such as JSON metrics, log-parsed data,
// or user-provided configuration where a zero default on failure is acceptable and
// the caller does not need to distinguish "invalid" from a genuine zero.
//
// For strict numeric-only parsing where the caller must distinguish missing/invalid
// values from zero, use ParseIntValue instead.
func ConvertToInt(val any) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		intVal := int(v)
		// Warn if truncation occurs (value has fractional part)
		if v != float64(intVal) {
			typeutilLog.Printf("Float value %.2f truncated to integer %d", v, intVal)
		}
		return intVal
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

// ConvertToFloat safely converts any value to float64, returning 0 on failure.
//
// Supported input types: float64, int, int64, and string (parsed via strconv.ParseFloat).
// Returns 0 for any other type or for strings that cannot be parsed as a float.
func ConvertToFloat(val any) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}
