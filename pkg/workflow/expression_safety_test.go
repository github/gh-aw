//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateExpressionSafety(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectError    bool
		expectedErrors []string
	}{
		{
			name:        "no_expressions",
			content:     "This is a simple markdown with no expressions",
			expectError: false,
		},
		{
			name:        "allowed_github_workflow",
			content:     "The workflow name is ${{ github.workflow }}",
			expectError: false,
		},
		{
			name:        "allowed_github_repository",
			content:     "Repository: ${{ github.repository }}",
			expectError: false,
		},
		{
			name:        "allowed_github_run_id",
			content:     "Run ID: ${{ github.run_id }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_issue_number",
			content:     "Issue number: ${{ github.event.issue.number }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_discussion_number",
			content:     "Discussion number: ${{ github.event.discussion.number }}",
			expectError: false,
		},
		{
			name:        "allowed_needs_task_outputs_text",
			content:     "Task output: ${{ needs.activation.outputs.text }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_inputs",
			content:     "User input: ${{ github.event.inputs.name }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_inputs_underscore",
			content:     "Branch input: ${{ github.event.inputs.target_branch }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_inputs_hyphen",
			content:     "Deploy input: ${{ github.event.inputs.deploy-environment }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_conclusion",
			content:     "Workflow conclusion: ${{ github.event.workflow_run.conclusion }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_html_url",
			content:     "Run URL: ${{ github.event.workflow_run.html_url }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_head_sha",
			content:     "Head SHA: ${{ github.event.workflow_run.head_sha }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_run_number",
			content:     "Run number: ${{ github.event.workflow_run.run_number }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_event",
			content:     "Triggering event: ${{ github.event.workflow_run.event }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_workflow_run_status",
			content:     "Run status: ${{ github.event.workflow_run.status }}",
			expectError: false,
		},
		{
			name:        "allowed_github_event_repository_default_branch",
			content:     "Default branch: ${{ github.event.repository.default_branch }}",
			expectError: false,
		},
		{
			name:        "multiple_allowed_expressions",
			content:     "Workflow: ${{ github.workflow }}, Repository: ${{ github.repository }}, Output: ${{ needs.activation.outputs.text }}",
			expectError: false,
		},
		{
			name:           "unauthorized_github_token",
			content:        "Using token: ${{ secrets.GITHUB_TOKEN }}",
			expectError:    true,
			expectedErrors: []string{"secrets.GITHUB_TOKEN"},
		},
		{
			name:        "authorized_github_actor",
			content:     "Actor: ${{ github.actor }}",
			expectError: false,
		},
		{
			name:        "authorized_env_variable",
			content:     "Environment: ${{ env.MY_VAR }}",
			expectError: false,
		},
		{
			name:        "unauthorized_steps_output",
			content:     "Step output: ${{ steps.my-step.outputs.result }}",
			expectError: false,
			// Note: steps outputs are allowed, but this is a test case to ensure it
			expectedErrors: []string{"steps.my-step.outputs.result"},
		},
		{
			name:           "mixed_authorized_and_unauthorized",
			content:        "Valid: ${{ github.workflow }}, Invalid: ${{ secrets.API_KEY }}",
			expectError:    true,
			expectedErrors: []string{"secrets.API_KEY"},
		},
		{
			name:           "multiple_unauthorized_expressions",
			content:        "Token: ${{ secrets.GITHUB_TOKEN }}, Valid: ${{ github.actor }}, Env: ${{ env.TEST }}",
			expectError:    true,
			expectedErrors: []string{"secrets.GITHUB_TOKEN"},
		},
		{
			name:        "expressions_with_whitespace",
			content:     "Spaced: ${{   github.workflow   }}, Normal: ${{github.repository}}",
			expectError: false,
		},
		{
			name:           "expressions_with_unauthorized_whitespace",
			content:        "Invalid spaced: ${{   secrets.TOKEN   }}",
			expectError:    true,
			expectedErrors: []string{"secrets.TOKEN"},
		},
		{
			name:        "expressions_in_code_blocks",
			content:     "Code example: `${{ github.workflow }}` and ```${{ github.repository }}```",
			expectError: false,
		},
		{
			name:           "unauthorized_in_code_blocks",
			content:        "Code example: `${{ secrets.TOKEN }}` should still be caught",
			expectError:    true,
			expectedErrors: []string{"secrets.TOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectError && err != nil {
				// Check that all expected unauthorized expressions are mentioned in the error
				errorMsg := err.Error()
				for _, expectedError := range tt.expectedErrors {
					if !strings.Contains(errorMsg, expectedError) {
						t.Errorf("Expected error message to contain '%s', but got: %s", expectedError, errorMsg)
					}
				}
			}
		})
	}
}

func TestValidateExpressionSafetyEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name:        "empty_expression",
			content:     "Empty: ${{ }}",
			expectError: true,
			description: "Empty expressions should be considered unauthorized",
		},
		{
			name:        "malformed_expression_single_brace",
			content:     "Malformed: ${ github.workflow }",
			expectError: false,
			description: "Malformed expressions (single brace) should be ignored",
		},
		{
			name:        "malformed_expression_no_closing",
			content:     "Malformed: ${{ github.workflow",
			expectError: false,
			description: "Malformed expressions (no closing) should be ignored",
		},
		{
			name:        "nested_expressions",
			content:     "Nested: ${{ ${{ github.workflow }} }}",
			expectError: true,
			description: "Nested expressions should be caught",
		},
		{
			name:        "expression_with_functions",
			content:     "Function: ${{ toJson(github.workflow) }}",
			expectError: true,
			description: "Expressions with functions should be unauthorized unless the base expression is allowed",
		},
		{
			name:        "multiline_expression",
			content:     "Multi:\n${{ github.workflow\n}}",
			expectError: true,
			description: "Should NOT handle expressions spanning multiple lines - though this is unusual",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s but got none", tt.description)
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for %s but got: %v", tt.description, err)
			}
		})
	}
}

// TestValidateExpressionSafetyWithParser tests the new parser functionality in expression safety
func TestValidateExpressionSafetyWithParser(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "allowed AND expression",
			content: `${{ github.workflow && github.repository }}`,
			wantErr: false,
		},
		{
			name:    "allowed OR expression",
			content: `${{ github.workflow || github.repository }}`,
			wantErr: false,
		},
		{
			name:    "allowed NOT expression",
			content: `${{ !github.workflow }}`,
			wantErr: false,
		},
		{
			name:    "complex allowed expression with parentheses",
			content: `${{ (github.workflow && github.repository) || github.run_id }}`,
			wantErr: false,
		},
		{
			name:        "mixed allowed and unauthorized",
			content:     `${{ github.workflow && secrets.TOKEN }}`,
			wantErr:     true,
			errContains: "secrets.TOKEN",
		},
		{
			name:        "unauthorized in complex expression",
			content:     `${{ (github.workflow || secrets.TOKEN) && github.repository }}`,
			wantErr:     true,
			errContains: "secrets.TOKEN",
		},
		{
			name:    "nested complex allowed expression",
			content: `${{ ((github.workflow && github.repository) || github.run_id) && github.actor }}`,
			wantErr: false,
		},
		{
			name:        "NOT with unauthorized expression",
			content:     `${{ !secrets.TOKEN }}`,
			wantErr:     true,
			errContains: "secrets.TOKEN",
		},
		{
			name:    "unparseable but allowed literal",
			content: `${{ github.workflow }}`,
			wantErr: false,
		},
		{
			name:        "unparseable and unauthorized literal",
			content:     `${{ secrets.INVALID_TOKEN }}`,
			wantErr:     true,
			errContains: "secrets.INVALID_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateExpressionSafety() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateExpressionSafety() error = %v, should contain %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("validateExpressionSafety() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUnauthorizedExpressionFuzzyMatchSuggestions(t *testing.T) {
	tests := []struct {
		name                string
		content             string
		expectedExpression  string
		expectedSuggestions []string
	}{
		{
			name:               "typo in github.actor",
			content:            "User: ${{ github.actr }}",
			expectedExpression: "github.actr",
			expectedSuggestions: []string{
				"github.actor",
			},
		},
		{
			name:               "typo in github.event.issue.number",
			content:            "Issue: ${{ github.event.issue.numbre }}",
			expectedExpression: "github.event.issue.numbre",
			expectedSuggestions: []string{
				"github.event.issue.number",
			},
		},
		{
			name:               "typo in github.repository",
			content:            "Repo: ${{ github.repositry }}",
			expectedExpression: "github.repositry",
			expectedSuggestions: []string{
				"github.repository",
			},
		},
		{
			name:               "typo in github.workflow",
			content:            "Workflow: ${{ github.workfow }}",
			expectedExpression: "github.workfow",
			expectedSuggestions: []string{
				"github.workflow",
			},
		},
		{
			name:               "typo in github.run_id",
			content:            "Run: ${{ github.run_di }}",
			expectedExpression: "github.run_di",
			expectedSuggestions: []string{
				"github.run_id",
			},
		},
		{
			name:                "no close match for secrets",
			content:             "Secret: ${{ secrets.TOKEN }}",
			expectedExpression:  "secrets.TOKEN",
			expectedSuggestions: []string{}, // No close match expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if err == nil {
				t.Fatal("Expected error for unauthorized expression")
			}

			errMsg := err.Error()

			// Verify the unauthorized expression is in the error message
			if !strings.Contains(errMsg, tt.expectedExpression) {
				t.Errorf("Error message should contain unauthorized expression '%s', got: %s",
					tt.expectedExpression, errMsg)
			}

			// Verify fuzzy match suggestions are present (if expected)
			for _, suggestion := range tt.expectedSuggestions {
				if !strings.Contains(errMsg, suggestion) {
					t.Errorf("Error message should contain suggestion '%s', got: %s",
						suggestion, errMsg)
				}
				// Also verify the "did you mean" format
				if !strings.Contains(errMsg, "did you mean:") {
					t.Errorf("Error message should contain 'did you mean:' for suggestions, got: %s",
						errMsg)
				}
			}

			// If no suggestions expected, verify "did you mean" is not present for that expression
			if len(tt.expectedSuggestions) == 0 {
				// Check if the pattern "- <expression> (did you mean:" appears in the error message
				pattern := fmt.Sprintf("- %s (did you mean:", tt.expectedExpression)
				if strings.Contains(errMsg, pattern) {
					t.Errorf("Error message for '%s' should NOT contain 'did you mean:', got: %s",
						tt.expectedExpression, errMsg)
				}
			}
		})
	}
}

// TestValidateExpressionForDangerousProps tests that dangerous JavaScript property names
// are blocked at compile time to prevent prototype pollution attacks (PR #14826)
func TestValidateExpressionForDangerousProps(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		expectError bool
		errorProp   string // The dangerous property that should be mentioned in error
	}{
		// Test constructor blocking
		{
			name:        "block_constructor_simple",
			expression:  "github.constructor",
			expectError: true,
			errorProp:   "constructor",
		},
		{
			name:        "block_constructor_in_chain",
			expression:  "github.event.constructor",
			expectError: true,
			errorProp:   "constructor",
		},
		{
			name:        "block_constructor_with_inputs",
			expression:  "inputs.constructor",
			expectError: true,
			errorProp:   "constructor",
		},
		{
			name:        "block_constructor_with_needs",
			expression:  "needs.job.constructor",
			expectError: true,
			errorProp:   "constructor",
		},

		// Test __proto__ blocking
		{
			name:        "block_proto_simple",
			expression:  "github.__proto__",
			expectError: true,
			errorProp:   "__proto__",
		},
		{
			name:        "block_proto_in_chain",
			expression:  "github.event.__proto__",
			expectError: true,
			errorProp:   "__proto__",
		},
		{
			name:        "block_proto_with_inputs",
			expression:  "inputs.__proto__",
			expectError: true,
			errorProp:   "__proto__",
		},

		// Test prototype blocking
		{
			name:        "block_prototype_simple",
			expression:  "github.prototype",
			expectError: true,
			errorProp:   "prototype",
		},
		{
			name:        "block_prototype_in_chain",
			expression:  "github.event.prototype",
			expectError: true,
			errorProp:   "prototype",
		},
		{
			name:        "block_prototype_with_inputs",
			expression:  "inputs.prototype",
			expectError: true,
			errorProp:   "prototype",
		},

		// Test __defineGetter__ blocking
		{
			name:        "block_defineGetter",
			expression:  "github.__defineGetter__",
			expectError: true,
			errorProp:   "__defineGetter__",
		},

		// Test __defineSetter__ blocking
		{
			name:        "block_defineSetter",
			expression:  "github.__defineSetter__",
			expectError: true,
			errorProp:   "__defineSetter__",
		},

		// Test __lookupGetter__ blocking
		{
			name:        "block_lookupGetter",
			expression:  "github.__lookupGetter__",
			expectError: true,
			errorProp:   "__lookupGetter__",
		},

		// Test __lookupSetter__ blocking
		{
			name:        "block_lookupSetter",
			expression:  "github.__lookupSetter__",
			expectError: true,
			errorProp:   "__lookupSetter__",
		},

		// Test hasOwnProperty blocking
		{
			name:        "block_hasOwnProperty",
			expression:  "github.hasOwnProperty",
			expectError: true,
			errorProp:   "hasOwnProperty",
		},

		// Test isPrototypeOf blocking
		{
			name:        "block_isPrototypeOf",
			expression:  "github.isPrototypeOf",
			expectError: true,
			errorProp:   "isPrototypeOf",
		},

		// Test propertyIsEnumerable blocking
		{
			name:        "block_propertyIsEnumerable",
			expression:  "github.propertyIsEnumerable",
			expectError: true,
			errorProp:   "propertyIsEnumerable",
		},

		// Test toString blocking
		{
			name:        "block_toString",
			expression:  "github.toString",
			expectError: true,
			errorProp:   "toString",
		},

		// Test valueOf blocking
		{
			name:        "block_valueOf",
			expression:  "github.valueOf",
			expectError: true,
			errorProp:   "valueOf",
		},

		// Test toLocaleString blocking
		{
			name:        "block_toLocaleString",
			expression:  "github.toLocaleString",
			expectError: true,
			errorProp:   "toLocaleString",
		},

		// Test blocking in array access patterns (bracket notation)
		{
			name:        "block_constructor_in_array_access",
			expression:  "github.event.release.assets[0].constructor",
			expectError: true,
			errorProp:   "constructor",
		},
		{
			name:        "block_proto_in_array_access",
			expression:  "github.event.release.assets[0].__proto__",
			expectError: true,
			errorProp:   "__proto__",
		},

		// Test that safe expressions are allowed
		{
			name:        "allow_safe_github_actor",
			expression:  "github.actor",
			expectError: false,
		},
		{
			name:        "allow_safe_github_repository",
			expression:  "github.repository",
			expectError: false,
		},
		{
			name:        "allow_safe_github_event_issue_number",
			expression:  "github.event.issue.number",
			expectError: false,
		},
		{
			name:        "allow_safe_needs_outputs",
			expression:  "needs.job-id.outputs.result",
			expectError: false,
		},
		{
			name:        "allow_safe_steps_outputs",
			expression:  "steps.step-id.outputs.value",
			expectError: false,
		},
		{
			name:        "allow_safe_inputs",
			expression:  "inputs.repository",
			expectError: false,
		},
		{
			name:        "allow_safe_env",
			expression:  "env.MY_VAR",
			expectError: false,
		},
		{
			name:        "allow_safe_array_access",
			expression:  "github.event.release.assets[0].id",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionForDangerousProps(tt.expression)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for expression '%s', but got nil", tt.expression)
					return
				}

				// Verify error message contains the dangerous property name
				errMsg := err.Error()
				if !strings.Contains(errMsg, tt.errorProp) {
					t.Errorf("Error message should mention dangerous property '%s', got: %s",
						tt.errorProp, errMsg)
				}

				// Verify error message mentions the expression
				if !strings.Contains(errMsg, tt.expression) {
					t.Errorf("Error message should mention expression '%s', got: %s",
						tt.expression, errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for safe expression '%s', but got: %v",
						tt.expression, err)
				}
			}
		})
	}
}

// TestValidateExpressionSafetyWithDangerousProps tests that dangerous properties
// are blocked in full workflow markdown content
func TestValidateExpressionSafetyWithDangerousProps(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorProp   string
	}{
		{
			name:        "block_constructor_in_markdown",
			content:     "The value is ${{ github.constructor }}",
			expectError: true,
			errorProp:   "constructor",
		},
		{
			name:        "block_proto_in_markdown",
			content:     "The value is ${{ inputs.__proto__ }}",
			expectError: true,
			errorProp:   "__proto__",
		},
		{
			name:        "block_prototype_in_markdown",
			content:     "The value is ${{ github.event.prototype }}",
			expectError: true,
			errorProp:   "prototype",
		},
		{
			name:        "block_toString_in_markdown",
			content:     "The value is ${{ github.toString }}",
			expectError: true,
			errorProp:   "toString",
		},
		{
			name:        "allow_safe_expressions_in_markdown",
			content:     "Issue number: ${{ github.event.issue.number }}, Actor: ${{ github.actor }}",
			expectError: false,
		},
		{
			name:        "block_multiple_dangerous_props",
			content:     "First: ${{ github.constructor }}, Second: ${{ inputs.__proto__ }}",
			expectError: true,
			errorProp:   "constructor", // Should catch at least one
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for content with dangerous property, but got nil")
					return
				}

				errMsg := err.Error()
				if !strings.Contains(errMsg, tt.errorProp) {
					t.Errorf("Error message should mention dangerous property '%s', got: %s",
						tt.errorProp, errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for safe content, but got: %v", err)
				}
			}
		})
	}
}
