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
