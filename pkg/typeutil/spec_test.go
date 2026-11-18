//go:build !integration

package typeutil

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSpec_PublicAPI_ParseIntValue validates the documented behavior of
// ParseIntValue as described in the package README.md.
//
// Specification: Strictly parses numeric types (int, int64, uint64, float64)
// to int. Returns (value, true) on success and (0, false) for unrecognized or
// non-numeric types, uint64 overflow, and float64 NaN, infinity, or int overflow.
func TestSpec_PublicAPI_ParseIntValue(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		wantValue int
		wantOK    bool
	}{
		{
			name:      "int input returns (value, true)",
			input:     42,
			wantValue: 42,
			wantOK:    true,
		},
		{
			name:      "int64 input returns (value, true)",
			input:     int64(99),
			wantValue: 99,
			wantOK:    true,
		},
		{
			name:      "uint64 input returns (value, true)",
			input:     uint64(7),
			wantValue: 7,
			wantOK:    true,
		},
		{
			name:      "float64 integer value returns (value, true)",
			input:     float64(10),
			wantValue: 10,
			wantOK:    true,
		},
		{
			name:      "float64 fractional value truncates toward zero",
			input:     float64(3.14),
			wantValue: 3,
			wantOK:    true,
		},
		{
			name:      "float64 NaN returns (0, false)",
			input:     math.NaN(),
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "float64 positive infinity returns (0, false)",
			input:     math.Inf(1),
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "float64 negative infinity returns (0, false)",
			input:     math.Inf(-1),
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "float64 positive overflow returns (0, false)",
			input:     math.MaxFloat64,
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "float64 negative overflow returns (0, false)",
			input:     -math.MaxFloat64,
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "uint64 overflow returns (0, false)",
			input:     ^uint64(0),
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "string input returns (0, false)",
			input:     "42",
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "nil input returns (0, false)",
			input:     nil,
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "bool input returns (0, false)",
			input:     true,
			wantValue: 0,
			wantOK:    false,
		},
		{
			name:      "zero int returns (0, true)",
			input:     0,
			wantValue: 0,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := ParseIntValue(tt.input)
			assert.Equal(t, tt.wantValue, gotValue,
				"ParseIntValue(%v) value mismatch", tt.input)
			assert.Equal(t, tt.wantOK, gotOK,
				"ParseIntValue(%v) ok flag mismatch", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ParseBool validates the documented behavior of ParseBool
// as described in the package README.md.
//
// Specification: Extracts a boolean value from a map[string]any by key.
// Returns false if the map is nil, the key is absent, or the value is not a bool.
func TestSpec_PublicAPI_ParseBool(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		key      string
		expected bool
	}{
		{
			name:     "true bool value returns true",
			m:        map[string]any{"enabled": true},
			key:      "enabled",
			expected: true,
		},
		{
			name:     "false bool value returns false",
			m:        map[string]any{"enabled": false},
			key:      "enabled",
			expected: false,
		},
		{
			name:     "nil map returns false",
			m:        nil,
			key:      "enabled",
			expected: false,
		},
		{
			name:     "absent key returns false",
			m:        map[string]any{"other": true},
			key:      "enabled",
			expected: false,
		},
		{
			name:     "non-bool value returns false",
			m:        map[string]any{"enabled": "yes"},
			key:      "enabled",
			expected: false,
		},
		{
			name:     "integer value returns false",
			m:        map[string]any{"enabled": 1},
			key:      "enabled",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseBool(tt.m, tt.key)
			assert.Equal(t, tt.expected, result,
				"ParseBool(map, %q) should match documented behavior", tt.key)
		})
	}
}

// TestSpec_KMSuffix_ParseInt64KMSuffix validates the documented behavior of
// ParseInt64KMSuffix as described in the package README.md.
//
// Specification: Parses a positive base-10 integer string with an optional
// K/k (×1,000) or M/m (×1,000,000) suffix. Returns (value, true) on success and
// (0, false) for empty input, non-positive values, non-numeric strings, or
// overflow.
func TestSpec_KMSuffix_ParseInt64KMSuffix(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValue int64
		wantOK    bool
	}{
		{name: "K suffix multiplies by 1,000", input: "128K", wantValue: 128000, wantOK: true},
		{name: "M suffix multiplies by 1,000,000", input: "2M", wantValue: 2000000, wantOK: true},
		{name: "plain integer parses as-is", input: "512", wantValue: 512, wantOK: true},
		{name: "lowercase k suffix accepted", input: "3k", wantValue: 3000, wantOK: true},
		{name: "lowercase m suffix accepted", input: "1m", wantValue: 1000000, wantOK: true},
		{name: "zero is non-positive returns (0, false)", input: "0", wantValue: 0, wantOK: false},
		{name: "negative value returns (0, false)", input: "-5", wantValue: 0, wantOK: false},
		{name: "empty input returns (0, false)", input: "", wantValue: 0, wantOK: false},
		{name: "non-numeric string returns (0, false)", input: "abc", wantValue: 0, wantOK: false},
		{name: "overflow returns (0, false)", input: "99999999999999999999M", wantValue: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := ParseInt64KMSuffix(tt.input)
			assert.Equal(t, tt.wantOK, gotOK,
				"ParseInt64KMSuffix(%q) ok flag mismatch", tt.input)
			assert.Equal(t, tt.wantValue, gotValue,
				"ParseInt64KMSuffix(%q) value mismatch", tt.input)
		})
	}
}

// TestSpec_KMSuffix_NormalizeInt64KMSuffix validates the documented behavior of
// NormalizeInt64KMSuffix as described in the package README.md.
//
// Specification: Returns a canonical base-10 string for a positive integer
// string with an optional K/k or M/m suffix. Delegates to ParseInt64KMSuffix
// and formats with strconv.FormatInt.
func TestSpec_KMSuffix_NormalizeInt64KMSuffix(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValue string
		wantOK    bool
	}{
		{name: "K suffix normalizes to base-10", input: "128K", wantValue: "128000", wantOK: true},
		{name: "lowercase m suffix normalizes to base-10", input: "2m", wantValue: "2000000", wantOK: true},
		{name: "plain integer is returned canonically", input: "512", wantValue: "512", wantOK: true},
		{name: "non-positive value returns (\"\", false)", input: "0", wantValue: "", wantOK: false},
		{name: "non-numeric string returns (\"\", false)", input: "abc", wantValue: "", wantOK: false},
		{name: "empty input returns (\"\", false)", input: "", wantValue: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := NormalizeInt64KMSuffix(tt.input)
			assert.Equal(t, tt.wantOK, gotOK,
				"NormalizeInt64KMSuffix(%q) ok flag mismatch", tt.input)
			assert.Equal(t, tt.wantValue, gotValue,
				"NormalizeInt64KMSuffix(%q) value mismatch", tt.input)
		})
	}
}

// TestSpec_SafeOverflow_SafeUint64ToInt validates the documented behavior of
// SafeUint64ToInt as described in the package README.md.
//
// Specification: Converts uint64 to int, returning 0 if the value would
// overflow int.
func TestSpec_SafeOverflow_SafeUint64ToInt(t *testing.T) {
	t.Run("normal value converts correctly", func(t *testing.T) {
		result := SafeUint64ToInt(uint64(100))
		assert.Equal(t, 100, result,
			"SafeUint64ToInt(100) should return 100")
	})

	t.Run("zero converts to zero", func(t *testing.T) {
		result := SafeUint64ToInt(uint64(0))
		assert.Equal(t, 0, result,
			"SafeUint64ToInt(0) should return 0")
	})

	t.Run("overflow value returns 0 (documented defensive behavior)", func(t *testing.T) {
		// uint64 max overflows int on all supported platforms
		result := SafeUint64ToInt(math.MaxUint64)
		assert.Equal(t, 0, result,
			"SafeUint64ToInt(MaxUint64) should return 0 to prevent overflow panic")
	})
}

// TestSpec_SafeOverflow_SafeUintToInt validates the documented behavior of
// SafeUintToInt as described in the package README.md.
//
// Specification: Converts uint to int, returning 0 if the value would overflow
// int. Thin wrapper around SafeUint64ToInt.
func TestSpec_SafeOverflow_SafeUintToInt(t *testing.T) {
	t.Run("normal value converts correctly", func(t *testing.T) {
		result := SafeUintToInt(uint(42))
		assert.Equal(t, 42, result,
			"SafeUintToInt(42) should return 42")
	})

	t.Run("zero converts to zero", func(t *testing.T) {
		result := SafeUintToInt(uint(0))
		assert.Equal(t, 0, result,
			"SafeUintToInt(0) should return 0")
	})
}

// TestSpec_PublicAPI_ConvertToInt validates the documented behavior of
// ConvertToInt as described in the package README.md.
//
// Specification: Leniently converts any value to int, returning 0 on failure.
// Also handles string inputs via strconv.Atoi, making it suitable for
// heterogeneous sources.
func TestSpec_PublicAPI_ConvertToInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{
			name:     "int input returns value",
			input:    55,
			expected: 55,
		},
		{
			name:     "int64 input returns value",
			input:    int64(77),
			expected: 77,
		},
		{
			name:     "float64 input returns int value",
			input:    float64(3),
			expected: 3,
		},
		{
			name:     "numeric string returns parsed value (documented behavior)",
			input:    "42",
			expected: 42,
		},
		{
			name:     "non-numeric string returns 0",
			input:    "not-a-number",
			expected: 0,
		},
		{
			name:     "nil returns 0",
			input:    nil,
			expected: 0,
		},
		{
			name:     "bool returns 0",
			input:    true,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToInt(tt.input)
			assert.Equal(t, tt.expected, result,
				"ConvertToInt(%v) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ConvertToFloat validates the documented behavior of
// ConvertToFloat as described in the package README.md.
//
// Specification: Safely converts any value (float64, int, int64, string) to
// float64, returning 0 on failure.
func TestSpec_PublicAPI_ConvertToFloat(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
	}{
		{
			name:     "float64 input returns value",
			input:    float64(3.14),
			expected: 3.14,
		},
		{
			name:     "int input returns float value",
			input:    10,
			expected: 10.0,
		},
		{
			name:     "int64 input returns float value",
			input:    int64(20),
			expected: 20.0,
		},
		{
			name:     "numeric string returns parsed value",
			input:    "2.5",
			expected: 2.5,
		},
		{
			name:     "non-numeric string returns 0",
			input:    "not-a-float",
			expected: 0,
		},
		{
			name:     "nil returns 0",
			input:    nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToFloat(tt.input)
			assert.InDelta(t, tt.expected, result, 1e-9,
				"ConvertToFloat(%v) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_LookupMap validates the documented behavior of LookupMap
// as described in the package README.md.
//
// Specification: "Safe map extraction from map[string]any by key". Returns the
// nested map with ok=true when the key holds a map[string]any; otherwise the
// zero value with ok=false (including a nil map or a wrong-typed value).
func TestSpec_PublicAPI_LookupMap(t *testing.T) {
	nested := map[string]any{"inner": "value"}

	tests := []struct {
		name      string
		m         map[string]any
		key       string
		wantValue map[string]any
		wantOK    bool
	}{
		{
			name:      "key holds a map returns (map, true)",
			m:         map[string]any{"cfg": nested},
			key:       "cfg",
			wantValue: nested,
			wantOK:    true,
		},
		{
			name:      "nil map returns (nil, false)",
			m:         nil,
			key:       "cfg",
			wantValue: nil,
			wantOK:    false,
		},
		{
			name:      "absent key returns (nil, false)",
			m:         map[string]any{"other": nested},
			key:       "cfg",
			wantValue: nil,
			wantOK:    false,
		},
		{
			name:      "non-map value returns (nil, false)",
			m:         map[string]any{"cfg": "not-a-map"},
			key:       "cfg",
			wantValue: nil,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := LookupMap(tt.m, tt.key)
			assert.Equal(t, tt.wantOK, gotOK,
				"LookupMap(map, %q) ok flag mismatch", tt.key)
			assert.Equal(t, tt.wantValue, gotValue,
				"LookupMap(map, %q) value mismatch", tt.key)
		})
	}
}

// TestSpec_PublicAPI_LookupString validates the documented behavior of
// LookupString as described in the package README.md.
//
// Specification: "Safe string extraction from map[string]any by key". Returns
// the string with ok=true when the key holds a string; otherwise ("", false)
// (including a nil map, an absent key, or a wrong-typed value).
func TestSpec_PublicAPI_LookupString(t *testing.T) {
	tests := []struct {
		name      string
		m         map[string]any
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "key holds a string returns (value, true)",
			m:         map[string]any{"name": "gh-aw"},
			key:       "name",
			wantValue: "gh-aw",
			wantOK:    true,
		},
		{
			name:      "nil map returns (\"\", false)",
			m:         nil,
			key:       "name",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "absent key returns (\"\", false)",
			m:         map[string]any{"other": "x"},
			key:       "name",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "non-string value returns (\"\", false)",
			m:         map[string]any{"name": 42},
			key:       "name",
			wantValue: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := LookupString(tt.m, tt.key)
			assert.Equal(t, tt.wantOK, gotOK,
				"LookupString(map, %q) ok flag mismatch", tt.key)
			assert.Equal(t, tt.wantValue, gotValue,
				"LookupString(map, %q) value mismatch", tt.key)
		})
	}
}

// TestSpec_PublicAPI_LookupStringPath validates the documented behavior of
// LookupStringPath as described in the package README.md.
//
// Specification: "Safe nested string extraction by key path". Walks the nested
// maps following the key path and returns the terminal string with ok=true;
// returns ("", false) if any step in the path is missing or has an invalid type.
func TestSpec_PublicAPI_LookupStringPath(t *testing.T) {
	doc := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "deep",
			},
		},
		"top": "shallow",
	}

	tests := []struct {
		name      string
		m         map[string]any
		path      []string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "single-key path to string returns (value, true)",
			m:         doc,
			path:      []string{"top"},
			wantValue: "shallow",
			wantOK:    true,
		},
		{
			name:      "nested path to string returns (value, true)",
			m:         doc,
			path:      []string{"a", "b", "c"},
			wantValue: "deep",
			wantOK:    true,
		},
		{
			name:      "missing intermediate key returns (\"\", false)",
			m:         doc,
			path:      []string{"a", "x", "c"},
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "path terminating at a map returns (\"\", false)",
			m:         doc,
			path:      []string{"a", "b"},
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "empty path returns (\"\", false)",
			m:         doc,
			path:      []string{},
			wantValue: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := LookupStringPath(tt.m, tt.path...)
			assert.Equal(t, tt.wantOK, gotOK,
				"LookupStringPath(map, %v) ok flag mismatch", tt.path)
			assert.Equal(t, tt.wantValue, gotValue,
				"LookupStringPath(map, %v) value mismatch", tt.path)
		})
	}
}

// TestSpec_PublicAPI_SafeAllocationCapacity validates the documented behavior
// of SafeAllocationCapacity as described in the package README.md.
//
// Specification: Returns the sum of non-negative capacity hints when it fits
// in int, and returns zero for a negative part or an overflowing sum.
func TestSpec_PublicAPI_SafeAllocationCapacity(t *testing.T) {
	tests := []struct {
		name     string
		parts    []int
		expected int
	}{
		{
			name:     "documented capacity hints are summed",
			parts:    []int{2, 3, 5},
			expected: 10,
		},
		{
			name:     "negative part returns zero",
			parts:    []int{4, -1},
			expected: 0,
		},
		{
			name:     "overflowing sum returns zero",
			parts:    []int{math.MaxInt, 1},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SafeAllocationCapacity(tt.parts...)
			assert.Equal(t, tt.expected, result,
				"SafeAllocationCapacity(%v) should match documented behavior", tt.parts)
		})
	}
}
