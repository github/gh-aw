//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateIntRange tests the validateIntRange helper function with boundary values
func TestValidateIntRange(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		min       int
		max       int
		fieldName string
		wantError bool
		errorText string
	}{
		{
			name:      "value at minimum",
			value:     1,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: false,
		},
		{
			name:      "value at maximum",
			value:     100,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: false,
		},
		{
			name:      "value in middle of range",
			value:     50,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: false,
		},
		{
			name:      "value below minimum",
			value:     0,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 1 and 100, got 0",
		},
		{
			name:      "value above maximum",
			value:     101,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 1 and 100, got 101",
		},
		{
			name:      "negative value below minimum",
			value:     -1,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 1 and 100, got -1",
		},
		{
			name:      "zero when minimum is zero",
			value:     0,
			min:       0,
			max:       100,
			fieldName: "test-field",
			wantError: false,
		},
		{
			name:      "large negative value",
			value:     -9999,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 1 and 100, got -9999",
		},
		{
			name:      "large positive value exceeding maximum",
			value:     999999,
			min:       1,
			max:       100,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 1 and 100, got 999999",
		},
		{
			name:      "single value range (min equals max)",
			value:     42,
			min:       42,
			max:       42,
			fieldName: "test-field",
			wantError: false,
		},
		{
			name:      "single value range - below",
			value:     41,
			min:       42,
			max:       42,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 42 and 42, got 41",
		},
		{
			name:      "single value range - above",
			value:     43,
			min:       42,
			max:       42,
			fieldName: "test-field",
			wantError: true,
			errorText: "test-field must be between 42 and 42, got 43",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIntRange(tt.value, tt.min, tt.max, tt.fieldName)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorText) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorText, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestValidateIntRangeWithRealWorldValues tests validateIntRange with actual constraint values
func TestValidateIntRangeWithRealWorldValues(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		min       int
		max       int
		fieldName string
		wantError bool
	}{
		// Port validation (1-65535)
		{
			name:      "port - minimum valid",
			value:     1,
			min:       1,
			max:       65535,
			fieldName: "port",
			wantError: false,
		},
		{
			name:      "port - maximum valid",
			value:     65535,
			min:       1,
			max:       65535,
			fieldName: "port",
			wantError: false,
		},
		{
			name:      "port - zero invalid",
			value:     0,
			min:       1,
			max:       65535,
			fieldName: "port",
			wantError: true,
		},
		{
			name:      "port - above maximum",
			value:     65536,
			min:       1,
			max:       65535,
			fieldName: "port",
			wantError: true,
		},

		// Max-file-size validation (1-104857600)
		{
			name:      "max-file-size - minimum valid",
			value:     1,
			min:       1,
			max:       104857600,
			fieldName: "max-file-size",
			wantError: false,
		},
		{
			name:      "max-file-size - maximum valid",
			value:     104857600,
			min:       1,
			max:       104857600,
			fieldName: "max-file-size",
			wantError: false,
		},
		{
			name:      "max-file-size - zero invalid",
			value:     0,
			min:       1,
			max:       104857600,
			fieldName: "max-file-size",
			wantError: true,
		},
		{
			name:      "max-file-size - above maximum",
			value:     104857601,
			min:       1,
			max:       104857600,
			fieldName: "max-file-size",
			wantError: true,
		},

		// Max-file-count validation (1-1000)
		{
			name:      "max-file-count - minimum valid",
			value:     1,
			min:       1,
			max:       1000,
			fieldName: "max-file-count",
			wantError: false,
		},
		{
			name:      "max-file-count - maximum valid",
			value:     1000,
			min:       1,
			max:       1000,
			fieldName: "max-file-count",
			wantError: false,
		},
		{
			name:      "max-file-count - zero invalid",
			value:     0,
			min:       1,
			max:       1000,
			fieldName: "max-file-count",
			wantError: true,
		},
		{
			name:      "max-file-count - above maximum",
			value:     1001,
			min:       1,
			max:       1000,
			fieldName: "max-file-count",
			wantError: true,
		},

		// Retention-days validation (1-90)
		{
			name:      "retention-days - minimum valid",
			value:     1,
			min:       1,
			max:       90,
			fieldName: "retention-days",
			wantError: false,
		},
		{
			name:      "retention-days - maximum valid",
			value:     90,
			min:       1,
			max:       90,
			fieldName: "retention-days",
			wantError: false,
		},
		{
			name:      "retention-days - zero invalid",
			value:     0,
			min:       1,
			max:       90,
			fieldName: "retention-days",
			wantError: true,
		},
		{
			name:      "retention-days - above maximum",
			value:     91,
			min:       1,
			max:       90,
			fieldName: "retention-days",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIntRange(tt.value, tt.min, tt.max, tt.fieldName)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error for %s=%d, got nil", tt.fieldName, tt.value)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for %s=%d, got: %v", tt.fieldName, tt.value, err)
				}
			}
		})
	}
}

func TestValidateRequired(t *testing.T) {
	t.Run("valid non-empty value", func(t *testing.T) {
		err := ValidateRequired("title", "my title")
		assert.NoError(t, err)
	})

	t.Run("empty value fails", func(t *testing.T) {
		err := ValidateRequired("title", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "field is required")
		assert.Contains(t, err.Error(), "Provide a non-empty value")
	})

	t.Run("whitespace-only value fails", func(t *testing.T) {
		err := ValidateRequired("title", "   ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestValidateMaxLength(t *testing.T) {
	t.Run("value within limit", func(t *testing.T) {
		err := ValidateMaxLength("title", "short", 100)
		assert.NoError(t, err)
	})

	t.Run("value at limit", func(t *testing.T) {
		err := ValidateMaxLength("title", "12345", 5)
		assert.NoError(t, err)
	})

	t.Run("value exceeds limit", func(t *testing.T) {
		err := ValidateMaxLength("title", "too long value", 5)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum length")
		assert.Contains(t, err.Error(), "Shorten")
	})
}

func TestValidateMinLength(t *testing.T) {
	t.Run("value meets minimum", func(t *testing.T) {
		err := ValidateMinLength("title", "hello", 3)
		assert.NoError(t, err)
	})

	t.Run("value below minimum", func(t *testing.T) {
		err := ValidateMinLength("title", "hi", 5)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shorter than minimum length")
		assert.Contains(t, err.Error(), "at least 5 characters")
	})
}

func TestValidateInList(t *testing.T) {
	allowedValues := []string{"open", "closed", "draft"}

	t.Run("value in list", func(t *testing.T) {
		err := ValidateInList("status", "open", allowedValues)
		assert.NoError(t, err)
	})

	t.Run("value not in list", func(t *testing.T) {
		err := ValidateInList("status", "invalid", allowedValues)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in allowed list")
		assert.Contains(t, err.Error(), "open, closed, draft")
	})
}

func TestValidatePositiveInt(t *testing.T) {
	t.Run("positive integer", func(t *testing.T) {
		err := ValidatePositiveInt("count", 5)
		assert.NoError(t, err)
	})

	t.Run("zero fails", func(t *testing.T) {
		err := ValidatePositiveInt("count", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a positive integer")
	})

	t.Run("negative fails", func(t *testing.T) {
		err := ValidatePositiveInt("count", -1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a positive integer")
	})
}

func TestValidateNonNegativeInt(t *testing.T) {
	t.Run("positive integer", func(t *testing.T) {
		err := ValidateNonNegativeInt("count", 5)
		assert.NoError(t, err)
	})

	t.Run("zero is valid", func(t *testing.T) {
		err := ValidateNonNegativeInt("count", 0)
		assert.NoError(t, err)
	})

	t.Run("negative fails", func(t *testing.T) {
		err := ValidateNonNegativeInt("count", -1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a non-negative integer")
	})
}

// TestIsEmptyOrNil tests the isEmptyOrNil helper function
func TestIsEmptyOrNil(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		// Nil values
		{"nil value", nil, true},

		// String values
		{"empty string", "", true},
		{"whitespace string", "   ", true},
		{"non-empty string", "hello", false},

		// Integer values
		{"zero int", 0, true},
		{"positive int", 5, false},
		{"negative int", -1, false},
		{"zero int64", int64(0), true},
		{"positive int64", int64(5), false},

		// Unsigned integer values
		{"zero uint", uint(0), true},
		{"positive uint", uint(5), false},
		{"zero uint64", uint64(0), true},
		{"positive uint64", uint64(5), false},

		// Float values
		{"zero float32", float32(0), true},
		{"positive float32", float32(5.5), false},
		{"zero float64", float64(0), true},
		{"positive float64", float64(5.5), false},

		// Boolean values
		{"false bool", false, true},
		{"true bool", true, false},

		// Slice values
		{"empty slice", []any{}, true},
		{"non-empty slice", []any{1, 2}, false},

		// Map values
		{"empty map", map[string]any{}, true},
		{"non-empty map", map[string]any{"key": "value"}, false},

		// Other types
		{"struct value", struct{ field string }{"value"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmptyOrNil(tt.value)
			assert.Equal(t, tt.expected, result, "isEmptyOrNil(%v) = %v, want %v", tt.value, result, tt.expected)
		})
	}
}

// TestGetMapFieldAsString tests the getMapFieldAsString helper function
func TestGetMapFieldAsString(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]any
		key        string
		defaultVal string
		expected   string
	}{
		{
			name:       "extract existing string",
			m:          map[string]any{"title": "Test Title"},
			key:        "title",
			defaultVal: "",
			expected:   "Test Title",
		},
		{
			name:       "missing key returns default",
			m:          map[string]any{"other": "value"},
			key:        "title",
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "non-string value returns default",
			m:          map[string]any{"title": 123},
			key:        "title",
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "nil map returns default",
			m:          nil,
			key:        "title",
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "empty string value",
			m:          map[string]any{"title": ""},
			key:        "title",
			defaultVal: "default",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMapFieldAsString(tt.m, tt.key, tt.defaultVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetMapFieldAsMap tests the getMapFieldAsMap helper function
func TestGetMapFieldAsMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		key      string
		expected map[string]any
	}{
		{
			name: "extract existing nested map",
			m: map[string]any{
				"network": map[string]any{
					"allowed-domains": "example.com",
				},
			},
			key:      "network",
			expected: map[string]any{"allowed-domains": "example.com"},
		},
		{
			name:     "missing key returns nil",
			m:        map[string]any{"other": "value"},
			key:      "network",
			expected: nil,
		},
		{
			name:     "non-map value returns nil",
			m:        map[string]any{"network": "not a map"},
			key:      "network",
			expected: nil,
		},
		{
			name:     "nil map returns nil",
			m:        nil,
			key:      "network",
			expected: nil,
		},
		{
			name:     "empty nested map",
			m:        map[string]any{"network": map[string]any{}},
			key:      "network",
			expected: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMapFieldAsMap(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetMapFieldAsBool tests the getMapFieldAsBool helper function
func TestGetMapFieldAsBool(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]any
		key        string
		defaultVal bool
		expected   bool
	}{
		{
			name:       "extract true value",
			m:          map[string]any{"enabled": true},
			key:        "enabled",
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "extract false value",
			m:          map[string]any{"enabled": false},
			key:        "enabled",
			defaultVal: true,
			expected:   false,
		},
		{
			name:       "missing key returns default",
			m:          map[string]any{"other": true},
			key:        "enabled",
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "non-bool value returns default",
			m:          map[string]any{"enabled": "true"},
			key:        "enabled",
			defaultVal: false,
			expected:   false,
		},
		{
			name:       "nil map returns default",
			m:          nil,
			key:        "enabled",
			defaultVal: true,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMapFieldAsBool(tt.m, tt.key, tt.defaultVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetMapFieldAsInt tests the getMapFieldAsInt helper function
func TestGetMapFieldAsInt(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]any
		key        string
		defaultVal int
		expected   int
	}{
		{
			name:       "extract int value",
			m:          map[string]any{"max-size": 100},
			key:        "max-size",
			defaultVal: 0,
			expected:   100,
		},
		{
			name:       "extract int64 value",
			m:          map[string]any{"max-size": int64(200)},
			key:        "max-size",
			defaultVal: 0,
			expected:   200,
		},
		{
			name:       "extract float64 value",
			m:          map[string]any{"max-size": float64(300)},
			key:        "max-size",
			defaultVal: 0,
			expected:   300,
		},
		{
			name:       "extract uint64 value",
			m:          map[string]any{"max-size": uint64(400)},
			key:        "max-size",
			defaultVal: 0,
			expected:   400,
		},
		{
			name:       "missing key returns default",
			m:          map[string]any{"other": 100},
			key:        "max-size",
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "non-numeric value returns default",
			m:          map[string]any{"max-size": "100"},
			key:        "max-size",
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "nil map returns default",
			m:          nil,
			key:        "max-size",
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "zero value",
			m:          map[string]any{"max-size": 0},
			key:        "max-size",
			defaultVal: 100,
			expected:   0,
		},
		{
			name:       "negative value",
			m:          map[string]any{"max-size": -10},
			key:        "max-size",
			defaultVal: 100,
			expected:   -10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMapFieldAsInt(tt.m, tt.key, tt.defaultVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDirExists tests the dirExists helper function
func TestDirExists(t *testing.T) {
	t.Run("empty path returns false", func(t *testing.T) {
		result := dirExists("")
		assert.False(t, result, "empty path should return false")
	})

	t.Run("non-existent path returns false", func(t *testing.T) {
		result := dirExists("/nonexistent/path/to/directory")
		assert.False(t, result, "non-existent path should return false")
	})

	t.Run("file path returns false", func(t *testing.T) {
		// validation_helpers.go should exist and be a file, not a directory
		result := dirExists("validation_helpers.go")
		assert.False(t, result, "file path should return false")
	})

	t.Run("directory path returns true", func(t *testing.T) {
		// Current directory should exist
		result := dirExists(".")
		assert.True(t, result, "current directory should return true")
	})

	t.Run("parent directory returns true", func(t *testing.T) {
		// Parent directory should exist
		result := dirExists("..")
		assert.True(t, result, "parent directory should return true")
	})
}
