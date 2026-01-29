//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestIsValidFullSHA(t *testing.T) {
	tests := []struct {
		name  string
		sha   string
		valid bool
	}{
		{
			name:  "valid full SHA",
			sha:   "2d4c6ce24c55704d72ec674d1f5c357831435180",
			valid: true,
		},
		{
			name:  "valid full SHA - all lowercase hex",
			sha:   "abcdef0123456789abcdef0123456789abcdef01",
			valid: true,
		},
		{
			name:  "invalid - short SHA (7 chars)",
			sha:   "5c3428a",
			valid: false,
		},
		{
			name:  "invalid - short SHA (8 chars)",
			sha:   "5c3428ab",
			valid: false,
		},
		{
			name:  "invalid - uppercase letters",
			sha:   "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			valid: false,
		},
		{
			name:  "invalid - contains non-hex characters",
			sha:   "xyz123456789abcdef0123456789abcdef0123g",
			valid: false,
		},
		{
			name:  "invalid - too long",
			sha:   "2d4c6ce24c55704d72ec674d1f5c357831435180abc",
			valid: false,
		},
		{
			name:  "invalid - empty string",
			sha:   "",
			valid: false,
		},
		{
			name:  "invalid - spaces",
			sha:   "2d4c6ce24c55704d72ec674d1f5c3578314 35180",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidFullSHA(tt.sha)
			if result != tt.valid {
				t.Errorf("isValidFullSHA(%q) = %v, want %v", tt.sha, result, tt.valid)
			}
		})
	}
}

func TestValidateActionTag(t *testing.T) {
	tests := []struct {
		name        string
		value       any
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid full SHA",
			value:       "2d4c6ce24c55704d72ec674d1f5c357831435180",
			expectError: false,
		},
		{
			name:        "valid - empty string (falls back to version)",
			value:       "",
			expectError: false,
		},
		{
			name:        "valid - nil value",
			value:       nil,
			expectError: false,
		},
		{
			name:        "invalid - short SHA (7 chars)",
			value:       "5c3428a",
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
		{
			name:        "invalid - short SHA (8 chars)",
			value:       "abc123de",
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
		{
			name:        "invalid - version tag instead of SHA",
			value:       "v1.0.0",
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
		{
			name:        "invalid - not a string",
			value:       12345,
			expectError: true,
			errorMsg:    "action-tag must be a string",
		},
		{
			name:        "invalid - boolean",
			value:       true,
			expectError: true,
			errorMsg:    "action-tag must be a string",
		},
		{
			name:        "invalid - uppercase SHA",
			value:       "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateActionTag(tt.value)
			if tt.expectError {
				if err == nil {
					t.Errorf("validateActionTag(%v) expected error, got nil", tt.value)
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateActionTag(%v) error = %q, want error containing %q", tt.value, err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateActionTag(%v) unexpected error: %v", tt.value, err)
				}
			}
		})
	}
}

func TestValidateFeatures(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil data",
			data:        nil,
			expectError: false,
		},
		{
			name:        "nil features",
			data:        &WorkflowData{Features: nil},
			expectError: false,
		},
		{
			name: "valid action-tag",
			data: &WorkflowData{
				Features: map[string]any{
					"action-tag": "2d4c6ce24c55704d72ec674d1f5c357831435180",
				},
			},
			expectError: false,
		},
		{
			name: "invalid action-tag - short SHA",
			data: &WorkflowData{
				Features: map[string]any{
					"action-tag": "5c3428a",
				},
			},
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
		{
			name: "invalid action-tag - version tag",
			data: &WorkflowData{
				Features: map[string]any{
					"action-tag": "v2.0.0",
				},
			},
			expectError: true,
			errorMsg:    "action-tag must be a full 40-character commit SHA",
		},
		{
			name: "empty action-tag is allowed",
			data: &WorkflowData{
				Features: map[string]any{
					"action-tag": "",
				},
			},
			expectError: false,
		},
		{
			name: "other features should not cause errors",
			data: &WorkflowData{
				Features: map[string]any{
					"some-other-feature": "any-value",
					"firewall":           true,
				},
			},
			expectError: false,
		},
		{
			name: "valid action-tag with other features",
			data: &WorkflowData{
				Features: map[string]any{
					"action-tag": "2d4c6ce24c55704d72ec674d1f5c357831435180",
					"firewall":   true,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFeatures(tt.data)
			if tt.expectError {
				if err == nil {
					t.Errorf("validateFeatures() expected error, got nil")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateFeatures() error = %q, want error containing %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateFeatures() unexpected error: %v", err)
				}
			}
		})
	}
}
