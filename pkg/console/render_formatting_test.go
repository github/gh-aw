//go:build !integration

package console

import (
	"reflect"
	"testing"
	"time"
)

func TestFormatFieldValue_Pointers(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "nil pointer",
			value:    (*int)(nil),
			expected: "-",
		},
		{
			name: "single pointer to int",
			value: func() *int {
				v := 42
				return &v
			}(),
			expected: "42",
		},
		{
			name: "double pointer to int",
			value: func() **int {
				v := 42
				p1 := &v
				return &p1
			}(),
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValue(val)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValue_NumericTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"int zero", int(0), "0"},
		{"int positive", int(42), "42"},
		{"int negative", int(-42), "-42"},
		{"int8", int8(127), "127"},
		{"int16", int16(32767), "32767"},
		{"int32", int32(2147483647), "2147483647"},
		{"int64", int64(9223372036854775807), "9223372036854775807"},
		{"uint zero", uint(0), "0"},
		{"uint positive", uint(42), "42"},
		{"uint8", uint8(255), "255"},
		{"uint16", uint16(65535), "65535"},
		{"uint32", uint32(4294967295), "4294967295"},
		{"uint64", uint64(18446744073709551615), "18446744073709551615"},
		{"float32 zero", float32(0.0), "0"},
		{"float32 positive", float32(3.14), "3.14"},
		{"float64 zero", float64(0.0), "0"},
		{"float64 positive", float64(3.14159265359), "3.14159265359"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValue(val)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValue_TimeType(t *testing.T) {
	// Test time.Time formatting
	testTime := time.Date(2025, 10, 28, 14, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "valid time.Time",
			value:    testTime,
			expected: "2025-10-28 14:30:45",
		},
		{
			name:     "zero time.Time",
			value:    time.Time{},
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValue(val)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValue_UnexportedFields(t *testing.T) {
	// Test handling of unexported fields (can't use Interface())
	type testStruct struct {
		exported   int
		unexported int
	}

	s := testStruct{exported: 42, unexported: 99}
	val := reflect.ValueOf(s)

	// Test unexported field formatting
	unexportedField := val.FieldByName("unexported")
	result := formatFieldValue(unexportedField)
	if result != "99" {
		t.Errorf("unexported int field: got %q, want %q", result, "99")
	}
}

func TestFormatFieldValue_BoolType(t *testing.T) {
	tests := []struct {
		name     string
		value    bool
		expected string
	}{
		{"true", true, "true"},
		{"false", false, "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValue(val)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValue_InvalidValue(t *testing.T) {
	// Test invalid reflect.Value
	var val reflect.Value
	result := formatFieldValue(val)
	if result != "-" {
		t.Errorf("invalid value: got %q, want %q", result, "-")
	}
}

func TestFormatFieldValueWithTag_NumberFormat(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"int - small", 500, "500"},
		{"int - 1k", 1000, "1.00k"},
		{"int - 1.5k", 1500, "1.50k"},
		{"int - 1M", 1000000, "1.00M"},
		{"int - 5M", 5000000, "5.00M"},
		{"int64", int64(250000), "250k"},
		{"int32", int32(1500), "1.50k"},
		{"uint", uint(2000), "2.00k"},
		{"uint64", uint64(3500000), "3.50M"},
		{"uint32", uint32(750), "750"},
	}

	tag := consoleTag{format: "number"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValueWithTag(val, tag)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValueWithTag_CostFormat(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"float64 positive", float64(1.234), "$1.234"},
		{"float64 small", float64(0.001), "$0.001"},
		{"float64 large", float64(99.999), "$99.999"},
		{"float64 zero", float64(0.0), "0"},
		{"float32 positive", float32(5.67), "$5.670"},
		{"float32 zero", float32(0.0), "0"},
	}

	tag := consoleTag{format: "cost"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValueWithTag(val, tag)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValueWithTag_DefaultValue(t *testing.T) {
	tag := consoleTag{defaultVal: "N/A"}

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"zero int uses default", 0, "N/A"},
		{"empty string uses default", "", "N/A"},
		{"non-zero int ignores default", 42, "42"},
		{"non-empty string ignores default", "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValueWithTag(val, tag)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValueWithTag_EmptyValueWithNumberFormat(t *testing.T) {
	// Test that "-" is returned for empty values even with format tag
	tag := consoleTag{format: "number"}

	val := reflect.ValueOf("")
	result := formatFieldValueWithTag(val, tag)
	if result != "-" {
		t.Errorf("empty string with number format: got %q, want %q", result, "-")
	}
}

func TestFormatFieldValueWithTag_FilesizeFormat(t *testing.T) {
	tag := consoleTag{format: "filesize"}

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"bytes", int(512), "512 B"},
		{"kilobytes", int(1024), "1.0 KB"},
		{"megabytes", int(1048576), "1.0 MB"},
		{"gigabytes", int64(1073741824), "1.0 GB"},
		{"int64 bytes", int64(256), "256 B"},
		{"uint bytes", uint(128), "128 B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValueWithTag(val, tag)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValueWithTag_NoFormat(t *testing.T) {
	// Test with no format tag - should behave like formatFieldValue
	tag := consoleTag{}

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"int", 42, "42"},
		{"string", "hello", "hello"},
		{"float", 3.14, "3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValueWithTag(val, tag)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}
