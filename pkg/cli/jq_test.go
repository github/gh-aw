//go:build !integration

package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestApplyJqFilter(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	tests := []struct {
		name      string
		jsonInput string
		jqFilter  string
		wantErr   bool
		validate  func(t *testing.T, output string)
	}{
		{
			name:      "simple filter - identity",
			jsonInput: `{"name":"test"}`,
			jqFilter:  ".",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				if !strings.Contains(output, "test") {
					t.Errorf("Expected output to contain 'test', got %q", output)
				}
			},
		},
		{
			name:      "simple filter - get first element",
			jsonInput: `[{"name":"a"},{"name":"b"}]`,
			jqFilter:  ".[0]",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				if output == "" {
					t.Error("Expected non-empty output")
				}
			},
		},
		{
			name:      "filter - count array length",
			jsonInput: `[{"name":"a"},{"name":"b"},{"name":"c"}]`,
			jqFilter:  "length",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				if output != "3\n" {
					t.Errorf("Expected '3\\n', got %q", output)
				}
			},
		},
		{
			name:      "filter - map and select",
			jsonInput: `[{"name":"a","type":"x"},{"name":"b","type":"y"},{"name":"c","type":"x"}]`,
			jqFilter:  `[.[] | select(.type == "x") | .name]`,
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				if output == "" {
					t.Error("Expected non-empty output")
				}
			},
		},
		{
			name:      "filter - extract specific field",
			jsonInput: `{"name":"value","id":123}`,
			jqFilter:  ".name",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				if !strings.Contains(output, "value") {
					t.Errorf("Expected output to contain 'value', got %q", output)
				}
			},
		},
		{
			name:      "filter - empty input",
			jsonInput: `{}`,
			jqFilter:  ".",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				if output != "{}" {
					t.Errorf("Expected '{}', got %q", output)
				}
			},
		},
		{
			name:      "filter - array transformation",
			jsonInput: `[1,2,3]`,
			jqFilter:  "map(. * 2)",
			wantErr:   false,
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "2") && !strings.Contains(output, "4") && !strings.Contains(output, "6") {
					t.Error("Expected transformed array output")
				}
			},
		},
		{
			name:      "invalid filter - syntax error",
			jsonInput: `[{"name":"a"}]`,
			jqFilter:  ".[invalid",
			wantErr:   true,
			validate:  nil,
		},
		{
			name:      "invalid JSON input",
			jsonInput: `{invalid json}`,
			jqFilter:  ".",
			wantErr:   true,
			validate:  nil,
		},
		{
			name:      "empty filter",
			jsonInput: `{"data":"test"}`,
			jqFilter:  "",
			wantErr:   true,
			validate:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ApplyJqFilter(tt.jsonInput, tt.jqFilter)
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyJqFilter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, output)
			}
		})
	}
}

func TestApplyJqFilter_JqNotAvailable(t *testing.T) {
	// This test verifies the error message when jq is not available
	// We can't easily mock exec.LookPath, so we'll just verify the function structure

	// If jq is available, skip this test
	if _, err := exec.LookPath("jq"); err == nil {
		t.Skip("Skipping test: jq is available, cannot test 'not found' scenario")
	}

	_, err := ApplyJqFilter(`[]`, ".")
	if err == nil {
		t.Error("Expected error when jq is not available")
	}
	if err != nil && err.Error() != "jq not found in PATH" {
		t.Errorf("Expected 'jq not found in PATH' error, got: %v", err)
	}
}

// TestApplyJqFilter_SecurityValidation tests security validation of jq filters
func TestApplyJqFilter_SecurityValidation(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	tests := []struct {
		name        string
		jqFilter    string
		expectError bool
		errorSubstr string
	}{
		// Dangerous functions - should be blocked
		{
			name:        "block input function",
			jqFilter:    `input`,
			expectError: true,
			errorSubstr: "dangerous function 'input'",
		},
		{
			name:        "block debug function",
			jqFilter:    `debug`,
			expectError: true,
			errorSubstr: "dangerous function 'debug'",
		},
		{
			name:        "block $__loc__ variable",
			jqFilter:    `$__loc__`,
			expectError: true,
			errorSubstr: "dangerous function '$__loc__'",
		},
		{
			name:        "block input in complex filter",
			jqFilter:    `. | input | .name`,
			expectError: true,
			errorSubstr: "dangerous function 'input'",
		},
		{
			name:        "block case-insensitive input",
			jqFilter:    `INPUT`,
			expectError: true,
			errorSubstr: "dangerous function 'input'",
		},
		// DoS patterns - should be blocked
		{
			name:        "block unbounded recurse",
			jqFilter:    `recurse(.)`,
			expectError: true,
			errorSubstr: "potentially dangerous pattern",
		},
		{
			name:        "block unbounded recurse with expression",
			jqFilter:    `recurse(.foo)`,
			expectError: true,
			errorSubstr: "potentially dangerous pattern",
		},
		{
			name:        "block infinite while loop",
			jqFilter:    `while(true; . + 1)`,
			expectError: true,
			errorSubstr: "potentially dangerous pattern",
		},
		{
			name:        "block infinite until loop",
			jqFilter:    `until(false; . + 1)`,
			expectError: true,
			errorSubstr: "potentially dangerous pattern",
		},
		// Length validation
		{
			name:        "block excessively long filter",
			jqFilter:    strings.Repeat("a", 10001),
			expectError: true,
			errorSubstr: "too long",
		},
		// Safe filters - should pass
		{
			name:        "allow identity filter",
			jqFilter:    ".",
			expectError: false,
		},
		{
			name:        "allow select function",
			jqFilter:    `select(.type == "test")`,
			expectError: false,
		},
		{
			name:        "allow length function",
			jqFilter:    "length",
			expectError: false,
		},
		{
			name:        "allow keys function",
			jqFilter:    "keys",
			expectError: false,
		},
		{
			name:        "allow object construction",
			jqFilter:    `{name: .name, id: .value}`,
			expectError: false,
		},
		{
			name:        "allow bounded recurse with condition",
			jqFilter:    `recurse(. * 2; . < 100)`,
			expectError: false,
		},
		{
			name:        "allow reasonable chaining",
			jqFilter:    `. | .name`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a simple JSON input for testing
			jsonInput := `{"name":"test","value":123,"active":true}`

			// Special case for recurse test - needs numeric input
			if strings.Contains(tt.jqFilter, "recurse") {
				jsonInput = `2`
			}

			_, err := ApplyJqFilter(jsonInput, tt.jqFilter)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', but got no error", tt.errorSubstr)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorSubstr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for safe filter, but got: %v", err)
				}
			}
		})
	}
}

// TestApplyJqFilter_TimeoutProtection tests timeout protection against slow/hanging filters
func TestApplyJqFilter_TimeoutProtection(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	// Note: We can't easily test timeout without a filter that actually hangs,
	// which would require a very complex or malicious filter that passes validation
	// but still takes too long. This test documents the timeout feature exists.

	t.Run("timeout mechanism exists", func(t *testing.T) {
		// Verify that valid filters complete quickly (well under 30s)
		jsonInput := `[1,2,3,4,5]`
		filter := `map(. * 2)`

		_, err := ApplyJqFilter(jsonInput, filter)
		if err != nil {
			t.Errorf("Fast filter should not timeout, got error: %v", err)
		}
	})
}

// TestApplyJqFilter_NoBreakingChanges verifies existing legitimate filters still work
func TestApplyJqFilter_NoBreakingChanges(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	// These are real-world filters used in the codebase
	tests := []struct {
		name      string
		jsonInput string
		jqFilter  string
		validate  func(t *testing.T, output string)
	}{
		{
			name:      "extract workflow names from status",
			jsonInput: `[{"workflow":"test1"},{"workflow":"test2"}]`,
			jqFilter:  ".[].workflow",
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "test1") || !strings.Contains(output, "test2") {
					t.Errorf("Should extract workflow names, got: %s", output)
				}
			},
		},
		{
			name:      "count array length from status",
			jsonInput: `[{"workflow":"test1"},{"workflow":"test2"}]`,
			jqFilter:  "length",
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				if output != "2" {
					t.Errorf("Should count 2 items, got: %s", output)
				}
			},
		},
		{
			name:      "extract nested fields from audit",
			jsonInput: `{"overview":{"run_id":123456}}`,
			jqFilter:  ".overview.run_id",
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				if output != "123456" {
					t.Errorf("Should extract run_id, got: %s", output)
				}
			},
		},
		{
			name:      "map and transform arrays",
			jsonInput: `{"jobs":[{"name":"a"},{"name":"b"}]}`,
			jqFilter:  ".jobs | map(.name)",
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "a") || !strings.Contains(output, "b") {
					t.Errorf("Should map job names, got: %s", output)
				}
			},
		},
		{
			name:      "select with conditions",
			jsonInput: `[{"name":"a","type":"x"},{"name":"b","type":"y"}]`,
			jqFilter:  `[.[] | select(.type == "x")]`,
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "a") || strings.Contains(output, "b") {
					t.Errorf("Should select only type x, got: %s", output)
				}
			},
		},
		{
			name:      "complex object construction",
			jsonInput: `{"overview":{"run_id":123},"metrics":{"tokens":500}}`,
			jqFilter:  `{run_id: .overview.run_id, tokens: .metrics.tokens}`,
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "123") || !strings.Contains(output, "500") {
					t.Errorf("Should construct object with both fields, got: %s", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ApplyJqFilter(tt.jsonInput, tt.jqFilter)
			if err != nil {
				t.Fatalf("Legitimate filter should not be blocked: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, output)
			}
		})
	}
}
