//go:build !integration

package envutil

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/logger"
)

// TestSpec_PublicAPI_GetIntFromEnv validates the documented behavior of
// GetIntFromEnv as described in the package README.md.
//
// Specification:
// - Returns defaultValue when the environment variable is not set.
// - Returns defaultValue and emits a warning when the value cannot be parsed as an integer.
// - Returns defaultValue and emits a warning when the value is outside [minValue, maxValue].
// - Logs the accepted value at debug level when log is non-nil.
// - Pass nil for log to suppress debug output.

// TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenNotSet validates that
// defaultValue is returned when the environment variable is not set.
func TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenNotSet(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_UNSET"
	os.Unsetenv(envVar)
	defer os.Unsetenv(envVar)

	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 5, result,
		"GetIntFromEnv should return defaultValue when env var is not set")
}

// TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenNotInteger validates that
// defaultValue is returned when the value cannot be parsed as an integer.
func TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenNotInteger(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_NAN"
	os.Setenv(envVar, "not-a-number")
	defer os.Unsetenv(envVar)

	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 5, result,
		"GetIntFromEnv should return defaultValue when value cannot be parsed as integer")
}

// TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenBelowMin validates that
// defaultValue is returned when the value is below minValue.
func TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenBelowMin(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_BELOW_MIN"
	os.Setenv(envVar, "0")
	defer os.Unsetenv(envVar)

	// minValue is 1, so 0 is outside [1, 20]
	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 5, result,
		"GetIntFromEnv should return defaultValue when value is below minValue")
}

// TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenAboveMax validates that
// defaultValue is returned when the value is above maxValue.
func TestSpec_PublicAPI_GetIntFromEnv_ReturnsDefault_WhenAboveMax(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_ABOVE_MAX"
	os.Setenv(envVar, "21")
	defer os.Unsetenv(envVar)

	// maxValue is 20, so 21 is outside [1, 20]
	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 5, result,
		"GetIntFromEnv should return defaultValue when value is above maxValue")
}

// TestSpec_PublicAPI_GetIntFromEnv_ReturnsValue_WhenInRange validates that
// the parsed value is returned when it is within [minValue, maxValue].
func TestSpec_PublicAPI_GetIntFromEnv_ReturnsValue_WhenInRange(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_IN_RANGE"
	os.Setenv(envVar, "10")
	defer os.Unsetenv(envVar)

	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 10, result,
		"GetIntFromEnv should return the env var value when within [minValue, maxValue]")
}

// TestSpec_PublicAPI_GetIntFromEnv_AcceptsInclusiveMinBoundary validates that
// minValue itself is accepted (inclusive lower bound).
func TestSpec_PublicAPI_GetIntFromEnv_AcceptsInclusiveMinBoundary(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_MIN_BOUNDARY"
	os.Setenv(envVar, "1")
	defer os.Unsetenv(envVar)

	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 1, result,
		"GetIntFromEnv should accept value equal to minValue (inclusive lower bound)")
}

// TestSpec_PublicAPI_GetIntFromEnv_AcceptsInclusiveMaxBoundary validates that
// maxValue itself is accepted (inclusive upper bound).
func TestSpec_PublicAPI_GetIntFromEnv_AcceptsInclusiveMaxBoundary(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_MAX_BOUNDARY"
	os.Setenv(envVar, "20")
	defer os.Unsetenv(envVar)

	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 20, result,
		"GetIntFromEnv should accept value equal to maxValue (inclusive upper bound)")
}

// TestSpec_PublicAPI_GetIntFromEnv_AcceptsNilLogger validates that passing nil
// for the log parameter suppresses debug output without panicking.
//
// Specification: "Pass nil for log to suppress debug output."
func TestSpec_PublicAPI_GetIntFromEnv_AcceptsNilLogger(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_NIL_LOGGER"
	os.Setenv(envVar, "10")
	defer os.Unsetenv(envVar)

	assert.NotPanics(t, func() {
		GetIntFromEnv(envVar, 5, 1, 20, nil)
	}, "GetIntFromEnv should not panic when nil logger is passed")
}

// TestSpec_PublicAPI_GetIntFromEnv_AcceptsNonNilLogger validates that a non-nil
// logger is accepted and the function returns the parsed value as documented.
//
// Specification: "Logs the accepted value at debug level when log is non-nil."
func TestSpec_PublicAPI_GetIntFromEnv_AcceptsNonNilLogger(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_NON_NIL_LOGGER"
	os.Setenv(envVar, "10")
	defer os.Unsetenv(envVar)

	log := logger.New("envutil:spec_test")

	var result int
	assert.NotPanics(t, func() {
		result = GetIntFromEnv(envVar, 5, 1, 20, log)
	}, "GetIntFromEnv should not panic when a non-nil logger is passed")
	assert.Equal(t, 10, result,
		"GetIntFromEnv should return the parsed value regardless of whether a logger is provided")
}

// TestSpec_PublicAPI_GetIntFromEnv_DocExample validates the usage example
// from the package README.md.
//
// Specification example:
//
//	concurrency := envutil.GetIntFromEnv("GH_AW_MAX_CONCURRENT_DOWNLOADS", 5, 1, 20, log)
func TestSpec_PublicAPI_GetIntFromEnv_DocExample(t *testing.T) {
	const envVar = "GH_AW_MAX_CONCURRENT_DOWNLOADS"
	saved := os.Getenv(envVar)
	defer func() {
		if saved != "" {
			os.Setenv(envVar, saved)
		} else {
			os.Unsetenv(envVar)
		}
	}()

	// When not set, should return default of 5
	os.Unsetenv(envVar)
	result := GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 5, result,
		"documented example: default 5 when env var not set")

	// When set to valid value, should return it
	os.Setenv(envVar, "8")
	result = GetIntFromEnv(envVar, 5, 1, 20, nil)
	assert.Equal(t, 8, result,
		"documented example: returns valid value within [1, 20]")
}

// TestSpec_PublicAPI_GetBoolFromEnv validates the documented behavior of
// GetBoolFromEnv as described in the package README.md.
func TestSpec_PublicAPI_GetBoolFromEnv_ReturnsDefault_WhenNotSet(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_BOOL_UNSET"
	os.Unsetenv(envVar)
	defer os.Unsetenv(envVar)

	result := GetBoolFromEnv(envVar, true, nil)
	assert.True(t, result,
		"GetBoolFromEnv should return defaultValue when env var is not set")
}

func TestSpec_PublicAPI_GetBoolFromEnv_ReturnsDefault_WhenInvalid(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_BOOL_INVALID"
	os.Setenv(envVar, "not-a-bool")
	defer os.Unsetenv(envVar)

	result := GetBoolFromEnv(envVar, true, nil)
	assert.True(t, result,
		"GetBoolFromEnv should return defaultValue when value cannot be parsed as boolean")
}

func TestSpec_PublicAPI_GetBoolFromEnv_ReturnsParsedValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "lowercase true", value: "true", want: true},
		{name: "uppercase true", value: "TRUE", want: true},
		{name: "numeric true", value: "1", want: true},
		{name: "lowercase false", value: "false", want: false},
		{name: "uppercase false", value: "FALSE", want: false},
		{name: "numeric false", value: "0", want: false},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVar := fmt.Sprintf("GH_AW_SPEC_TEST_BOOL_VALID_%d", index)
			require.NoError(t, os.Setenv(envVar, tt.value), "setting the test environment variable should succeed")
			t.Cleanup(func() {
				assert.NoError(t, os.Unsetenv(envVar), "unsetting the test environment variable should succeed")
			})

			result := GetBoolFromEnv(envVar, !tt.want, nil)
			assert.Equal(t, tt.want, result,
				"GetBoolFromEnv should follow strconv.ParseBool rules for %q", tt.value)
		})
	}
}

func TestSpec_PublicAPI_GetBoolFromEnv_AcceptsNonNilLogger(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_BOOL_LOGGER"
	os.Setenv(envVar, "true")
	defer os.Unsetenv(envVar)

	log := logger.New("envutil:spec_test")

	assert.NotPanics(t, func() {
		assert.True(t, GetBoolFromEnv(envVar, false, log))
	}, "GetBoolFromEnv should not panic when a non-nil logger is passed")
}

// TestSpec_PublicAPI_GetStringFromEnv validates the documented behavior of
// GetStringFromEnv as described in the package README.md.
func TestSpec_PublicAPI_GetStringFromEnv_ReturnsDefault_WhenNotSet(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_STRING_UNSET"
	os.Unsetenv(envVar)
	defer os.Unsetenv(envVar)

	result := GetStringFromEnv(envVar, "fallback", nil)
	assert.Equal(t, "fallback", result,
		"GetStringFromEnv should return defaultValue when env var is not set")
}

func TestSpec_PublicAPI_GetStringFromEnv_ReturnsDefault_WhenEmpty(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_STRING_EMPTY"
	os.Setenv(envVar, "")
	defer os.Unsetenv(envVar)

	result := GetStringFromEnv(envVar, "fallback", nil)
	assert.Equal(t, "fallback", result,
		"GetStringFromEnv should return defaultValue when env var is empty")
}

func TestSpec_PublicAPI_GetStringFromEnv_ReturnsValue(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_STRING_VALUE"
	os.Setenv(envVar, "value")
	defer os.Unsetenv(envVar)

	result := GetStringFromEnv(envVar, "fallback", nil)
	assert.Equal(t, "value", result,
		"GetStringFromEnv should return the env var value when it is non-empty")
}

func TestSpec_PublicAPI_GetStringFromEnv_AcceptsNonNilLogger(t *testing.T) {
	const envVar = "GH_AW_SPEC_TEST_STRING_LOGGER"
	os.Setenv(envVar, "token")
	defer os.Unsetenv(envVar)

	log := logger.New("envutil:spec_test")

	assert.NotPanics(t, func() {
		assert.Equal(t, "token", GetStringFromEnv(envVar, "", log))
	}, "GetStringFromEnv should not panic when a non-nil logger is passed")
}

// TestSpec_ThreadSafety_ConcurrentUse validates the README guarantee that all
// three helpers are safe for concurrent use and hold no shared mutable state.
func TestSpec_ThreadSafety_ConcurrentUse(t *testing.T) {
	const workers = 24

	type result struct {
		name string
		got  any
		want any
	}

	results := make(chan result, workers*3)
	var wg sync.WaitGroup
	for index := range workers {
		intVar := fmt.Sprintf("GH_AW_SPEC_TEST_CONCURRENT_INT_%d", index)
		boolVar := fmt.Sprintf("GH_AW_SPEC_TEST_CONCURRENT_BOOL_%d", index)
		stringVar := fmt.Sprintf("GH_AW_SPEC_TEST_CONCURRENT_STRING_%d", index)
		require.NoError(t, os.Setenv(intVar, strconv.Itoa(index)), "setting integer environment variable should succeed")
		require.NoError(t, os.Setenv(boolVar, "true"), "setting boolean environment variable should succeed")
		require.NoError(t, os.Setenv(stringVar, fmt.Sprintf("value-%d", index)), "setting string environment variable should succeed")
		t.Cleanup(func() {
			assert.NoError(t, os.Unsetenv(intVar), "unsetting integer environment variable should succeed")
			assert.NoError(t, os.Unsetenv(boolVar), "unsetting boolean environment variable should succeed")
			assert.NoError(t, os.Unsetenv(stringVar), "unsetting string environment variable should succeed")
		})

		wg.Add(3)
		go func() {
			defer wg.Done()
			results <- result{name: intVar, got: GetIntFromEnv(intVar, -1, 0, workers, nil), want: index}
		}()
		go func() {
			defer wg.Done()
			results <- result{name: boolVar, got: GetBoolFromEnv(boolVar, false, nil), want: true}
		}()
		go func() {
			defer wg.Done()
			results <- result{name: stringVar, got: GetStringFromEnv(stringVar, "fallback", nil), want: fmt.Sprintf("value-%d", index)}
		}()
	}

	wg.Wait()
	close(results)
	for result := range results {
		assert.Equal(t, result.want, result.got, "%s should be read correctly during concurrent use", result.name)
	}
}
