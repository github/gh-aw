//go:build !integration

package envutil

import (
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/logger"
)

func TestGetIntFromEnv(t *testing.T) {
	// Save original env value
	const testEnvVar = "GH_AW_TEST_INT_VALUE"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	tests := []struct {
		name         string
		envValue     string
		defaultValue int
		minValue     int
		maxValue     int
		expected     int
	}{
		{
			name:         "default when env var not set",
			envValue:     "",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10,
		},
		{
			name:         "valid value within range",
			envValue:     "50",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     50,
		},
		{
			name:         "valid value at minimum",
			envValue:     "1",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     1,
		},
		{
			name:         "valid value at maximum",
			envValue:     "100",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     100,
		},
		{
			name:         "invalid non-numeric value",
			envValue:     "invalid",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10,
		},
		{
			name:         "invalid value below minimum",
			envValue:     "0",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10,
		},
		{
			name:         "invalid negative value",
			envValue:     "-5",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10,
		},
		{
			name:         "invalid value above maximum",
			envValue:     "101",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10,
		},
		{
			name:         "different valid range",
			envValue:     "25",
			defaultValue: 5,
			minValue:     10,
			maxValue:     50,
			expected:     25,
		},
		{
			name:         "different valid range - out of bounds",
			envValue:     "5",
			defaultValue: 20,
			minValue:     10,
			maxValue:     50,
			expected:     20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv(testEnvVar, tt.envValue)
			} else {
				os.Unsetenv(testEnvVar)
			}

			// Test the function
			log := logger.New("test:GetIntFromEnv")
			result := GetIntFromEnv(testEnvVar, tt.defaultValue, tt.minValue, tt.maxValue, log)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetIntFromEnv_WithoutLogger(t *testing.T) {
	// Test that the function works without a logger (nil logger)
	const testEnvVar = "GH_AW_TEST_INT_NO_LOG"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	os.Setenv(testEnvVar, "42")
	result := GetIntFromEnv(testEnvVar, 10, 1, 100, nil)
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

// Additional edge case tests

func TestGetIntFromEnv_EdgeCases(t *testing.T) {
	const testEnvVar = "GH_AW_TEST_INT_EDGE"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	tests := []struct {
		name         string
		envValue     string
		defaultValue int
		minValue     int
		maxValue     int
		expected     int
	}{
		{
			name:         "very large valid value",
			envValue:     "999999",
			defaultValue: 10,
			minValue:     1,
			maxValue:     1000000,
			expected:     999999,
		},
		{
			name:         "whitespace in value",
			envValue:     " 50 ",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10, // strconv.Atoi doesn't trim whitespace
		},
		{
			name:         "leading zeros",
			envValue:     "0050",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     50,
		},
		{
			name:         "plus sign prefix",
			envValue:     "+50",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     50,
		},
		{
			name:         "float value",
			envValue:     "50.5",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10, // strconv.Atoi fails on floats
		},
		{
			name:         "scientific notation",
			envValue:     "5e2",
			defaultValue: 10,
			minValue:     1,
			maxValue:     1000,
			expected:     10, // strconv.Atoi doesn't parse scientific notation
		},
		{
			name:         "hex value",
			envValue:     "0x32",
			defaultValue: 10,
			minValue:     1,
			maxValue:     100,
			expected:     10, // strconv.Atoi doesn't parse hex
		},
		{
			name:         "zero range - min equals max",
			envValue:     "5",
			defaultValue: 1,
			minValue:     5,
			maxValue:     5,
			expected:     5,
		},
		{
			name:         "negative range",
			envValue:     "-10",
			defaultValue: 0,
			minValue:     -20,
			maxValue:     -5,
			expected:     -10,
		},
		{
			name:         "large negative value",
			envValue:     "-999999",
			defaultValue: 0,
			minValue:     -1000000,
			maxValue:     1000000,
			expected:     -999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(testEnvVar, tt.envValue)
			result := GetIntFromEnv(testEnvVar, tt.defaultValue, tt.minValue, tt.maxValue, nil)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetIntFromEnv_Idempotent(t *testing.T) {
	// Test that calling GetIntFromEnv multiple times with same input returns same result
	const testEnvVar = "GH_AW_TEST_INT_IDEMPOTENT"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	os.Setenv(testEnvVar, "42")

	result1 := GetIntFromEnv(testEnvVar, 10, 1, 100, nil)
	result2 := GetIntFromEnv(testEnvVar, 10, 1, 100, nil)
	result3 := GetIntFromEnv(testEnvVar, 10, 1, 100, nil)

	if result1 != result2 || result2 != result3 {
		t.Errorf("GetIntFromEnv is not idempotent: got %d, %d, %d", result1, result2, result3)
	}

	if result1 != 42 {
		t.Errorf("Expected 42, got %d", result1)
	}
}

func TestGetIntFromEnv_BoundaryValidation(t *testing.T) {
	// Test that min/max boundaries are properly enforced
	const testEnvVar = "GH_AW_TEST_INT_BOUNDARY"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	tests := []struct {
		name     string
		envValue string
		minValue int
		maxValue int
		expected int
	}{
		{
			name:     "exactly at minimum",
			envValue: "10",
			minValue: 10,
			maxValue: 100,
			expected: 10,
		},
		{
			name:     "exactly at maximum",
			envValue: "100",
			minValue: 10,
			maxValue: 100,
			expected: 100,
		},
		{
			name:     "one below minimum",
			envValue: "9",
			minValue: 10,
			maxValue: 100,
			expected: 50, // default
		},
		{
			name:     "one above maximum",
			envValue: "101",
			minValue: 10,
			maxValue: 100,
			expected: 50, // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(testEnvVar, tt.envValue)
			result := GetIntFromEnv(testEnvVar, 50, tt.minValue, tt.maxValue, nil)

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetIntFromEnv_EmptyString(t *testing.T) {
	// Test explicit empty string vs unset variable
	const testEnvVar = "GH_AW_TEST_INT_EMPTY"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	// Test with explicitly set empty string
	os.Setenv(testEnvVar, "")
	result1 := GetIntFromEnv(testEnvVar, 42, 1, 100, nil)

	// Test with unset variable
	os.Unsetenv(testEnvVar)
	result2 := GetIntFromEnv(testEnvVar, 42, 1, 100, nil)

	// Both should return the default value
	if result1 != 42 || result2 != 42 {
		t.Errorf("Expected both to return 42, got %d and %d", result1, result2)
	}
}

// Benchmark tests

func BenchmarkGetIntFromEnv_ValidValue(b *testing.B) {
	const testEnvVar = "GH_AW_BENCHMARK_VALID"
	os.Setenv(testEnvVar, "50")
	defer os.Unsetenv(testEnvVar)

	log := logger.New("benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIntFromEnv(testEnvVar, 10, 1, 100, log)
	}
}

func BenchmarkGetIntFromEnv_DefaultValue(b *testing.B) {
	const testEnvVar = "GH_AW_BENCHMARK_DEFAULT"
	os.Unsetenv(testEnvVar)

	log := logger.New("benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIntFromEnv(testEnvVar, 10, 1, 100, log)
	}
}

func BenchmarkGetIntFromEnv_InvalidValue(b *testing.B) {
	const testEnvVar = "GH_AW_BENCHMARK_INVALID"
	os.Setenv(testEnvVar, "invalid")
	defer os.Unsetenv(testEnvVar)

	log := logger.New("benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIntFromEnv(testEnvVar, 10, 1, 100, log)
	}
}

func BenchmarkGetIntFromEnv_NoLogger(b *testing.B) {
	const testEnvVar = "GH_AW_BENCHMARK_NOLOG"
	os.Setenv(testEnvVar, "50")
	defer os.Unsetenv(testEnvVar)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIntFromEnv(testEnvVar, 10, 1, 100, nil)
	}
}
