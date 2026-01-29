//go:build !integration

package workflow

import (
	"math"
	"testing"
)

// TestConvertToIntEdgeCases provides comprehensive coverage for ConvertToInt edge cases
func TestConvertToIntEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		// Valid string numbers
		{"valid string", "123", 123},
		{"string zero", "0", 0},
		{"negative string", "-42", -42},
		{"positive string with plus sign", "+123", 123}, // strconv.Atoi accepts "+" prefix

		// Invalid strings
		{"invalid string - letters", "abc", 0},
		{"invalid string - mixed", "12abc34", 0},
		{"invalid string - multiple decimals", "12.5.6", 0},
		{"empty string", "", 0},

		// Scientific notation strings
		// Note: strconv.Atoi doesn't parse scientific notation, so these return 0
		{"scientific notation string - positive exp", "1.5e3", 0},
		{"scientific notation string - negative exp", "1e-3", 0},
		{"scientific notation string - uppercase", "1E3", 0},

		// Hexadecimal strings
		// Note: strconv.Atoi doesn't parse hex, so these return 0
		{"hex string lowercase", "0xff", 0},
		{"hex string uppercase", "0xFF", 0},
		{"hex string without prefix", "ff", 0},

		// Whitespace in strings
		// Note: strconv.Atoi doesn't trim whitespace, so these return 0
		{"string with leading whitespace", " 123", 0},
		{"string with trailing whitespace", "123 ", 0},
		{"string with surrounding whitespace", " 123 ", 0},
		{"string with newline", "123\n", 0},
		{"string with tab", "\t123", 0},

		// Integer types
		{"int positive", 123, 123},
		{"int negative", -456, -456},
		{"int zero", 0, 0},
		{"int max int32", int(2147483647), 2147483647},
		{"int min int32", int(-2147483648), -2147483648},

		// Int64 types
		{"int64 positive", int64(789), 789},
		{"int64 negative", int64(-789), -789},
		{"int64 zero", int64(0), 0},

		// Float64 types - truncation behavior
		{"float64 clean conversion", 60.0, 60},
		{"float64 truncation 60.7", 60.7, 60},
		{"float64 truncation 60.3", 60.3, 60},
		{"float64 negative truncation", -60.3, -60},
		{"float64 negative truncation 60.7", -60.7, -60},
		{"float64 near zero truncation", 0.9, 0},
		{"float64 negative near zero", -0.9, 0},
		{"float64 very small positive", 0.001, 0},
		{"float64 very small negative", -0.001, 0},

		// Invalid/unsupported types
		{"nil", nil, 0},
		{"bool true", true, 0},
		{"bool false", false, 0},
		{"empty slice", []int{}, 0},
		{"slice with elements", []int{1, 2, 3}, 0},
		{"empty map", map[string]int{}, 0},
		{"map with elements", map[string]int{"key": 1}, 0},
		{"struct", struct{ X int }{X: 1}, 0},
		{"pointer to int", func() any { v := 5; return &v }(), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToInt(tt.input)
			if result != tt.expected {
				t.Errorf("ConvertToInt(%v) = %d; want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// TestConvertToIntOverflow tests behavior with very large numbers
func TestConvertToIntOverflow(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		validate func(t *testing.T, result int)
	}{
		{
			name:  "very large float64",
			input: float64(1e18),
			validate: func(t *testing.T, result int) {
				// Should convert to int without panic
				// The exact value depends on platform int size
				if result == 0 {
					t.Errorf("Expected non-zero result for 1e18")
				}
			},
		},
		{
			name:  "MaxFloat64 converted to int",
			input: math.MaxFloat64,
			validate: func(t *testing.T, result int) {
				// This will overflow but should not panic
				// Result is undefined but should not crash
				t.Logf("MaxFloat64 converted to int: %d", result)
			},
		},
		{
			name:  "positive infinity",
			input: math.Inf(1),
			validate: func(t *testing.T, result int) {
				// Should not panic, result is undefined
				t.Logf("Inf(1) converted to int: %d", result)
			},
		},
		{
			name:  "negative infinity",
			input: math.Inf(-1),
			validate: func(t *testing.T, result int) {
				// Should not panic, result is undefined
				t.Logf("Inf(-1) converted to int: %d", result)
			},
		},
		{
			name:  "NaN",
			input: math.NaN(),
			validate: func(t *testing.T, result int) {
				// Should not panic, result is undefined
				t.Logf("NaN converted to int: %d", result)
			},
		},
		{
			name:  "very large string number",
			input: "99999999999999999999999999999999",
			validate: func(t *testing.T, result int) {
				// strconv.Atoi should return 0 for overflow
				if result != 0 {
					t.Errorf("Expected 0 for overflow string, got %d", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ConvertToInt panicked with %v: %v", tt.input, r)
				}
			}()

			result := ConvertToInt(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestParseIntValueEdgeCases provides comprehensive coverage for parseIntValue edge cases
func TestParseIntValueEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
		ok       bool
	}{
		// Integer types
		{"int positive", 42, 42, true},
		{"int negative", -42, -42, true},
		{"int zero", 0, 0, true},

		// Int64 types
		{"int64 positive", int64(100), 100, true},
		{"int64 negative", int64(-100), -100, true},
		{"int64 zero", int64(0), 0, true},

		// Uint64 types
		{"uint64 positive", uint64(200), 200, true},
		{"uint64 zero", uint64(0), 0, true},

		// Float64 types - truncation behavior
		{"float64 clean", 60.0, 60, true},
		{"float64 truncation", 60.7, 60, true},
		{"float64 negative truncation", -5.5, -5, true},
		{"float64 near zero", 0.9, 0, true},
		{"float64 zero", 0.0, 0, true},

		// Unsupported types - parseIntValue does NOT support strings
		{"string value", "42", 0, false},
		{"nil value", nil, 0, false},
		{"bool value", true, 0, false},
		{"slice value", []int{1, 2}, 0, false},
		{"map value", map[string]int{"a": 1}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := parseIntValue(tt.input)
			if ok != tt.ok {
				t.Errorf("parseIntValue(%v) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if result != tt.expected {
				t.Errorf("parseIntValue(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParseIntValueOverflow tests behavior with extreme values
func TestParseIntValueOverflow(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		validate func(t *testing.T, result int, ok bool)
	}{
		{
			name:  "MaxFloat64",
			input: math.MaxFloat64,
			validate: func(t *testing.T, result int, ok bool) {
				if !ok {
					t.Errorf("Expected ok=true for MaxFloat64")
				}
				// Result is undefined but should not panic
				t.Logf("MaxFloat64 parsed to: %d", result)
			},
		},
		{
			name:  "positive infinity",
			input: math.Inf(1),
			validate: func(t *testing.T, result int, ok bool) {
				if !ok {
					t.Errorf("Expected ok=true for Inf(1)")
				}
				t.Logf("Inf(1) parsed to: %d", result)
			},
		},
		{
			name:  "negative infinity",
			input: math.Inf(-1),
			validate: func(t *testing.T, result int, ok bool) {
				if !ok {
					t.Errorf("Expected ok=true for Inf(-1)")
				}
				t.Logf("Inf(-1) parsed to: %d", result)
			},
		},
		{
			name:  "NaN",
			input: math.NaN(),
			validate: func(t *testing.T, result int, ok bool) {
				if !ok {
					t.Errorf("Expected ok=true for NaN")
				}
				t.Logf("NaN parsed to: %d", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parseIntValue panicked with %v: %v", tt.input, r)
				}
			}()

			result, ok := parseIntValue(tt.input)
			tt.validate(t, result, ok)
		})
	}
}

// TestConvertToFloatEdgeCases provides comprehensive coverage for ConvertToFloat edge cases
func TestConvertToFloatEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
	}{
		// Valid float values
		{"float64 positive", 123.45, 123.45},
		{"float64 negative", -123.45, -123.45},
		{"float64 zero", 0.0, 0.0},

		// Integer to float conversion
		{"int positive", 100, 100.0},
		{"int negative", -100, -100.0},
		{"int zero", 0, 0.0},

		// Int64 to float conversion
		{"int64 positive", int64(200), 200.0},
		{"int64 negative", int64(-200), -200.0},

		// String to float conversion
		{"string positive", "99.99", 99.99},
		{"string negative", "-99.99", -99.99},
		{"string integer", "50", 50.0},
		{"string zero", "0", 0.0},

		// Scientific notation strings - strconv.ParseFloat supports these
		{"scientific notation positive exp", "1.5e3", 1500.0},
		{"scientific notation negative exp", "1.5e-3", 0.0015},
		{"scientific notation uppercase", "1.5E3", 1500.0},
		{"scientific notation no decimal", "1e3", 1000.0},

		// Invalid strings
		{"invalid string", "not a number", 0.0},
		{"empty string", "", 0.0},
		{"multiple decimals", "1.2.3", 0.0},

		// Whitespace in strings
		// Note: strconv.ParseFloat does NOT accept whitespace, returns error
		{"string with whitespace", " 123.45 ", 0.0}, // ParseFloat returns error for whitespace

		// Invalid/unsupported types
		{"nil", nil, 0.0},
		{"bool true", true, 0.0},
		{"bool false", false, 0.0},
		{"slice", []int{1, 2, 3}, 0.0},
		{"map", map[string]int{"key": 1}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToFloat(tt.input)
			if result != tt.expected {
				t.Errorf("ConvertToFloat(%v) = %f; want %f", tt.input, result, tt.expected)
			}
		})
	}
}

// TestConvertToFloatSpecialValues tests behavior with special float values
func TestConvertToFloatSpecialValues(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		validate func(t *testing.T, result float64)
	}{
		{
			name:  "positive infinity",
			input: math.Inf(1),
			validate: func(t *testing.T, result float64) {
				if !math.IsInf(result, 1) {
					t.Errorf("Expected +Inf, got %f", result)
				}
			},
		},
		{
			name:  "negative infinity",
			input: math.Inf(-1),
			validate: func(t *testing.T, result float64) {
				if !math.IsInf(result, -1) {
					t.Errorf("Expected -Inf, got %f", result)
				}
			},
		},
		{
			name:  "NaN",
			input: math.NaN(),
			validate: func(t *testing.T, result float64) {
				if !math.IsNaN(result) {
					t.Errorf("Expected NaN, got %f", result)
				}
			},
		},
		{
			name:  "MaxFloat64",
			input: math.MaxFloat64,
			validate: func(t *testing.T, result float64) {
				if result != math.MaxFloat64 {
					t.Errorf("Expected MaxFloat64, got %f", result)
				}
			},
		},
		{
			name:  "SmallestNonzeroFloat64",
			input: math.SmallestNonzeroFloat64,
			validate: func(t *testing.T, result float64) {
				if result != math.SmallestNonzeroFloat64 {
					t.Errorf("Expected SmallestNonzeroFloat64, got %e", result)
				}
			},
		},
		{
			name:  "Inf string",
			input: "Inf",
			validate: func(t *testing.T, result float64) {
				if !math.IsInf(result, 1) {
					t.Errorf("Expected +Inf from string, got %f", result)
				}
			},
		},
		{
			name:  "-Inf string",
			input: "-Inf",
			validate: func(t *testing.T, result float64) {
				if !math.IsInf(result, -1) {
					t.Errorf("Expected -Inf from string, got %f", result)
				}
			},
		},
		{
			name:  "NaN string",
			input: "NaN",
			validate: func(t *testing.T, result float64) {
				if !math.IsNaN(result) {
					t.Errorf("Expected NaN from string, got %f", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ConvertToFloat panicked with %v: %v", tt.input, r)
				}
			}()

			result := ConvertToFloat(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestPolymorphicTypeHandling tests handling of polymorphic fields like category and version
func TestPolymorphicTypeHandling(t *testing.T) {
	// This tests the common case where YAML/JSON can provide values as different types

	t.Run("ConvertToInt with polymorphic inputs", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected int
		}{
			// String versions that should fail to convert to int
			{"version string main", "main", 0},
			{"version string 3.11", "3.11", 0},
			{"version string v1.0.0", "v1.0.0", 0},

			// Integer values
			{"integer 123", 123, 123},
			{"integer 0", 0, 0},
			{"integer -1", -1, -1},

			// Float values (common from JSON parsing)
			{"float 3.11", 3.11, 3},
			{"float 20.0", 20.0, 20},
			{"float -5.5", -5.5, -5},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := ConvertToInt(tt.input)
				if result != tt.expected {
					t.Errorf("ConvertToInt(%v) = %d; want %d", tt.input, result, tt.expected)
				}
			})
		}
	})

	t.Run("ConvertToFloat with polymorphic inputs", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected float64
		}{
			// String versions
			{"version string 3.11", "3.11", 3.11},
			{"version string main", "main", 0.0},

			// Integer values
			{"integer 123", 123, 123.0},
			{"integer 0", 0, 0.0},

			// Float values
			{"float 3.11", 3.11, 3.11},
			{"float 20.0", 20.0, 20.0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := ConvertToFloat(tt.input)
				if result != tt.expected {
					t.Errorf("ConvertToFloat(%v) = %f; want %f", tt.input, result, tt.expected)
				}
			})
		}
	})
}

// TestTypeSafetyNoPanics ensures no panics occur with any input type
func TestTypeSafetyNoPanics(t *testing.T) {
	// Collection of unusual/edge case inputs that should not cause panics
	inputs := []any{
		nil,
		true,
		false,
		int8(127),
		int16(32767),
		int32(2147483647),
		int64(9223372036854775807),
		uint(42),
		uint8(255),
		uint16(65535),
		uint32(4294967295),
		uint64(18446744073709551615),
		float32(3.14),
		float64(3.14159),
		complex64(1 + 2i),
		complex128(1 + 2i),
		"string",
		"",
		[]byte("bytes"),
		[]int{1, 2, 3},
		[]string{"a", "b"},
		map[string]any{"key": "value"},
		map[int]int{1: 2},
		struct{ X int }{X: 1},
		func() {},
		make(chan int),
	}

	t.Run("ConvertToInt no panics", func(t *testing.T) {
		for i, input := range inputs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("ConvertToInt panicked on input %d (%T): %v", i, input, r)
					}
				}()
				_ = ConvertToInt(input)
			}()
		}
	})

	t.Run("ConvertToFloat no panics", func(t *testing.T) {
		for i, input := range inputs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("ConvertToFloat panicked on input %d (%T): %v", i, input, r)
					}
				}()
				_ = ConvertToFloat(input)
			}()
		}
	})

	t.Run("parseIntValue no panics", func(t *testing.T) {
		for i, input := range inputs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("parseIntValue panicked on input %d (%T): %v", i, input, r)
					}
				}()
				_, _ = parseIntValue(input)
			}()
		}
	})
}

// TestZeroValueReturns ensures invalid inputs return zero values
func TestZeroValueReturns(t *testing.T) {
	invalidInputs := []any{
		nil,
		true,
		false,
		[]int{1, 2, 3},
		map[string]int{"key": 1},
		struct{ X int }{X: 5},
		func() {},
		make(chan int),
		"not a number",
		"abc123",
		"",
	}

	t.Run("ConvertToInt returns zero for invalid inputs", func(t *testing.T) {
		for _, input := range invalidInputs {
			result := ConvertToInt(input)
			if result != 0 {
				t.Errorf("ConvertToInt(%v) = %d; want 0", input, result)
			}
		}
	})

	t.Run("ConvertToFloat returns zero for invalid inputs", func(t *testing.T) {
		for _, input := range invalidInputs {
			result := ConvertToFloat(input)
			if result != 0.0 {
				t.Errorf("ConvertToFloat(%v) = %f; want 0.0", input, result)
			}
		}
	})

	t.Run("parseIntValue returns zero and false for invalid inputs", func(t *testing.T) {
		for _, input := range invalidInputs {
			result, ok := parseIntValue(input)
			if ok {
				t.Errorf("parseIntValue(%v) ok = true; want false", input)
			}
			if result != 0 {
				t.Errorf("parseIntValue(%v) = %d; want 0", input, result)
			}
		}
	})
}

// TestFloatToIntTruncationBehavior documents the specific truncation behavior
func TestFloatToIntTruncationBehavior(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected int
	}{
		// Positive numbers truncate towards zero
		{"0.1 truncates to 0", 0.1, 0},
		{"0.5 truncates to 0", 0.5, 0},
		{"0.9 truncates to 0", 0.9, 0},
		{"1.1 truncates to 1", 1.1, 1},
		{"1.9 truncates to 1", 1.9, 1},
		{"99.99 truncates to 99", 99.99, 99},

		// Negative numbers truncate towards zero (not floor)
		{"-0.1 truncates to 0", -0.1, 0},
		{"-0.5 truncates to 0", -0.5, 0},
		{"-0.9 truncates to 0", -0.9, 0},
		{"-1.1 truncates to -1", -1.1, -1},
		{"-1.9 truncates to -1", -1.9, -1},
		{"-99.99 truncates to -99", -99.99, -99},

		// Exact values
		{"1.0 stays 1", 1.0, 1},
		{"-1.0 stays -1", -1.0, -1},
		{"0.0 stays 0", 0.0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test ConvertToInt
			result := ConvertToInt(tt.input)
			if result != tt.expected {
				t.Errorf("ConvertToInt(%v) = %d; want %d", tt.input, result, tt.expected)
			}

			// Test parseIntValue for consistency
			parsed, ok := parseIntValue(tt.input)
			if !ok {
				t.Errorf("parseIntValue(%v) ok = false; want true", tt.input)
			}
			if parsed != tt.expected {
				t.Errorf("parseIntValue(%v) = %d; want %d", tt.input, parsed, tt.expected)
			}
		})
	}
}
