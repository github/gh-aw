//go:build !integration

package typeutil

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseIntValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		expected int
		ok       bool
	}{
		{"int value", 42, 42, true},
		{"int64 value", int64(100), 100, true},
		{"uint64 value", uint64(200), 200, true},
		{"float64 value", float64(3.14), 3, true},
		{"float64 positive out of range", 1e300, 0, false},
		{"float64 negative out of range", -1e300, 0, false},
		{"float64 NaN", math.NaN(), 0, false},
		{"float64 positive infinity", math.Inf(1), 0, false},
		{"float64 negative infinity", math.Inf(-1), 0, false},
		{"string value (not supported)", "42", 0, false},
		{"int32 value (not supported)", int32(5), 0, false},
		{"nil value", nil, 0, false},
		{"bool value (not supported)", true, 0, false},
		{"overflow: uint64 max returns 0", ^uint64(0), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, ok := ParseIntValue(tt.value)
			assert.Equal(t, tt.ok, ok, "ParseIntValue(%v) ok flag mismatch", tt.value)
			assert.Equal(t, tt.expected, result, "ParseIntValue(%v) result mismatch", tt.value)
		})
	}
}

func TestSafeUint64ToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    uint64
		expected int
	}{
		{"zero", 0, 0},
		{"small value", 42, 42},
		{"large value within int range", 1000000, 1000000},
		{"max int value", uint64(^uint(0) >> 1), int(^uint(0) >> 1)},
		{"overflow: max uint64 returns 0", ^uint64(0), 0},
		{"overflow: max int + 1 returns 0", uint64(^uint(0)>>1) + 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SafeUint64ToInt(tt.value)
			assert.Equal(t, tt.expected, result, "SafeUint64ToInt(%d) result mismatch", tt.value)
		})
	}
}

func TestSafeUintToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    uint
		expected int
	}{
		{"zero", 0, 0},
		{"small value", 100, 100},
		{"large value within range", uint(math.MaxInt32), math.MaxInt32},
		{"overflow: max int + 1 returns 0", uint(math.MaxInt) + 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SafeUintToInt(tt.value)
			assert.Equal(t, tt.expected, result, "SafeUintToInt(%d) result mismatch", tt.value)
		})
	}
}

func TestConvertToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"int", 42, 42},
		{"int64", int64(100), 100},
		{"float64 clean", 60.0, 60},
		{"float64 truncation", 60.7, 60},
		{"float64 negative truncation", -2.7, -2},
		{"string number", "123", 123},
		{"invalid string", "abc", 0},
		{"string with surrounding whitespace", " 42 ", 0},
		{"empty string", "", 0},
		{"uint64 unsupported", uint64(7), 0},
		{"nil", nil, 0},
		{"bool", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertToInt(tt.input)
			assert.Equal(t, tt.expected, result, "ConvertToInt(%v) result mismatch", tt.input)
		})
	}
}

func TestConvertToFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected float64
		isNaN    bool
		isInf    bool
	}{
		{"float64", 123.45, 123.45, false, false},
		{"int", 100, 100.0, false, false},
		{"int64", int64(200), 200.0, false, false},
		{"string", "99.99", 99.99, false, false},
		{"scientific notation string", "1e3", 1000, false, false},
		{"NaN string", "NaN", 0, true, false},
		{"infinity string", "Inf", math.Inf(1), false, true},
		{"invalid string", "not a number", 0.0, false, false},
		{"unsupported float32", float32(1.5), 0.0, false, false},
		{"nil", nil, 0.0, false, false},
		{"bool", true, 0.0, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertToFloat(tt.input)
			if tt.isNaN {
				assert.True(t, math.IsNaN(result), "ConvertToFloat(%v) should return NaN", tt.input)
			} else if tt.isInf {
				assert.True(t, math.IsInf(result, 1), "ConvertToFloat(%v) should return positive infinity", tt.input)
			} else {
				assert.InDelta(t, tt.expected, result, 0, "ConvertToFloat(%v) result mismatch", tt.input)
			}
		})
	}
}
