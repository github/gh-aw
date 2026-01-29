//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestSecretsExpressionPattern tests the regex pattern directly
func TestSecretsExpressionPattern(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		matches bool
	}{
		// Valid patterns
		{"simple secret", "${{ secrets.TOKEN }}", true},
		{"with underscore", "${{ secrets.MY_TOKEN }}", true},
		{"with numbers", "${{ secrets.TOKEN_V2 }}", true},
		{"with spaces", "${{  secrets.TOKEN  }}", true},
		{"two fallbacks", "${{ secrets.TOKEN1 || secrets.TOKEN2 }}", true},
		{"three fallbacks", "${{ secrets.TOKEN1 || secrets.TOKEN2 || secrets.TOKEN3 }}", true},
		{"underscore prefix", "${{ secrets._PRIVATE }}", true},
		{"many spaces", "${{   secrets.TOKEN   ||   secrets.FALLBACK   }}", true},
		{"lowercase letters in name", "${{ secrets.myToken }}", true},
		{"mixed case name", "${{ secrets.MyToken }}", true},

		// Invalid patterns
		{"plaintext", "my-secret", false},
		{"env context", "${{ env.TOKEN }}", false},
		{"vars context", "${{ vars.TOKEN }}", false},
		{"github context", "${{ github.token }}", false},
		{"inputs context", "${{ inputs.TOKEN }}", false},
		{"mixed contexts", "${{ secrets.TOKEN || env.FALLBACK }}", false},
		{"mixed with vars", "${{ secrets.TOKEN || vars.BACKUP }}", false},
		{"missing opening", "secrets.TOKEN }}", false},
		{"missing closing", "${{ secrets.TOKEN", false},
		{"number prefix", "${{ secrets.123TOKEN }}", false},
		{"hyphen in name", "${{ secrets.MY-TOKEN }}", false},
		{"space in name", "${{ secrets.MY TOKEN }}", false},
		{"special char @", "${{ secrets.MY@TOKEN }}", false},
		{"special char $", "${{ secrets.MY$TOKEN }}", false},
		{"empty", "", false},
		{"only braces", "${{ }}", false},
		{"empty secret name", "${{ secrets. }}", false},
		{"case sensitive context", "${{ Secrets.TOKEN }}", false},
		{"uppercase SECRETS", "${{ SECRETS.TOKEN }}", false},
		{"text before", "****** secrets.TOKEN }}", false},
		{"text after", "${{ secrets.TOKEN }} extra", false},
		{"just secret context", "secrets.TOKEN", false},
		{"partial expression", "${{ secrets", false},
		{"dot only", "${{ secrets.TOKEN. }}", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := secretsExpressionPattern.MatchString(tt.value)
			if matches != tt.matches {
				t.Errorf("Pattern match = %v, want %v for value: %q", matches, tt.matches, tt.value)
			}
		})
	}
}

// TestValidateSecretsExpressionErrorMessages tests that error messages are descriptive
// but do NOT include sensitive values to prevent clear-text logging
func TestValidateSecretsExpressionErrorMessages(t *testing.T) {
	tests := []struct {
		name              string
		value             string
		expectedInErrs    []string
		notExpectedInErrs []string
	}{
		{
			name:              "plaintext does NOT show value in error",
			value:             "plaintext",
			expectedInErrs:    []string{"invalid secrets expression", "must be a GitHub Actions expression"},
			notExpectedInErrs: []string{"plaintext"},
		},
		{
			name:              "env context does NOT show value in error",
			value:             "${{ env.TOKEN }}",
			expectedInErrs:    []string{"invalid secrets expression"},
			notExpectedInErrs: []string{"${{ env.TOKEN }}"},
		},
		{
			name:              "hardcoded value NOT in error (security fix)",
			value:             "hardcoded",
			expectedInErrs:    []string{"invalid secrets expression"},
			notExpectedInErrs: []string{"hardcoded"},
		},
		{
			name:           "example format in error",
			value:          "bad",
			expectedInErrs: []string{"${{ secrets.MY_SECRET }}"},
		},
		{
			name:           "fallback example in error",
			value:          "bad",
			expectedInErrs: []string{"${{ secrets.SECRET1 || secrets.SECRET2 }}"},
		},
		{
			name:              "mixed context error does NOT show value",
			value:             "${{ secrets.TOKEN || env.FALLBACK }}",
			expectedInErrs:    []string{"invalid secrets expression"},
			notExpectedInErrs: []string{"${{ secrets.TOKEN || env.FALLBACK }}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretsExpression(tt.value)
			if err == nil {
				t.Fatalf("Expected error, got nil")
			}
			errMsg := err.Error()
			for _, expected := range tt.expectedInErrs {
				if !strings.Contains(errMsg, expected) {
					t.Errorf("Expected error to contain %q, got: %s", expected, errMsg)
				}
			}
			for _, notExpected := range tt.notExpectedInErrs {
				if strings.Contains(errMsg, notExpected) {
					t.Errorf("Expected error NOT to contain sensitive value %q, but it does. Got: %s", notExpected, errMsg)
				}
			}
		})
	}
}

// TestValidateSecretsExpressionWithVariousValues tests validation with different values
func TestValidateSecretsExpressionWithVariousValues(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectError bool
	}{
		{"valid simple", "${{ secrets.GITHUB_TOKEN }}", false},
		{"valid with underscore", "${{ secrets.MY_TOKEN }}", false},
		{"valid with fallback", "${{ secrets.TOKEN1 || secrets.TOKEN2 }}", false},
		{"invalid plaintext", "plaintext", true},
		{"invalid env", "${{ env.TOKEN }}", true},
		{"invalid vars", "${{ vars.TOKEN }}", true},
		{"invalid mixed", "${{ secrets.TOKEN || env.FALLBACK }}", true},
		{"invalid empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretsExpression(tt.value)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for value %q, got nil", tt.value)
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error for value %q, got: %v", tt.value, err)
			}
			// Error should be descriptive and not contain the actual value
			if err != nil {
				if !strings.Contains(err.Error(), "invalid secrets expression") {
					t.Errorf("Error should contain descriptive message, got: %s", err.Error())
				}
				// Security: Ensure the actual invalid value is not in the error message
				if tt.value != "" && strings.Contains(err.Error(), tt.value) {
					t.Errorf("Error should NOT contain the actual value %q, but got: %s", tt.value, err.Error())
				}
			}
		})
	}
}

// TestSecretsValidationEdgeCases tests edge cases and boundary conditions
func TestSecretsValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectError bool
		description string
	}{
		{
			name:        "very long secret name",
			value:       "${{ secrets.THIS_IS_A_VERY_LONG_SECRET_NAME_WITH_MANY_UNDERSCORES_123456789 }}",
			expectError: false,
			description: "Should accept long secret names",
		},
		{
			name:        "many fallbacks",
			value:       "${{ secrets.TOKEN1 || secrets.TOKEN2 || secrets.TOKEN3 || secrets.TOKEN4 || secrets.TOKEN5 }}",
			expectError: false,
			description: "Should accept multiple fallbacks",
		},
		{
			name:        "minimal valid expression",
			value:       "${{ secrets.T }}",
			expectError: false,
			description: "Should accept single character secret names",
		},
		{
			name:        "underscore only name",
			value:       "${{ secrets._ }}",
			expectError: false,
			description: "Should accept underscore-only names",
		},
		{
			name:        "mixed with spaces in fallback",
			value:       "${{ secrets.TOKEN1  ||  secrets.TOKEN2 }}",
			expectError: false,
			description: "Should accept extra spaces in fallback",
		},
		{
			name:        "almost valid but trailing dot",
			value:       "${{ secrets.TOKEN. }}",
			expectError: true,
			description: "Should reject trailing dot",
		},
		{
			name:        "unicode in secret name",
			value:       "${{ secrets.TOKEN_🔑 }}",
			expectError: true,
			description: "Should reject unicode characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretsExpression(tt.value)
			if tt.expectError && err == nil {
				t.Errorf("%s: Expected error, got nil", tt.description)
			} else if !tt.expectError && err != nil {
				t.Errorf("%s: Expected no error, got: %v", tt.description, err)
			}
		})
	}
}
