//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckoutMissingPersistCredentialsFalse tests the checkoutMissingPersistCredentialsFalse helper
func TestCheckoutMissingPersistCredentialsFalse(t *testing.T) {
	tests := []struct {
		name     string
		step     map[string]any
		expected bool // true = insecure (missing persist-credentials: false)
	}{
		{
			name: "non-checkout step is safe",
			step: map[string]any{
				"name": "Run tests",
				"run":  "go test ./...",
			},
			expected: false,
		},
		{
			name: "checkout without with block is insecure",
			step: map[string]any{
				"uses": "actions/checkout@v4",
			},
			expected: true,
		},
		{
			name: "checkout with persist-credentials false is safe",
			step: map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"persist-credentials": false,
				},
			},
			expected: false,
		},
		{
			name: "checkout with persist-credentials true is insecure",
			step: map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"persist-credentials": true,
				},
			},
			expected: true,
		},
		{
			name: "checkout with persist-credentials string false is safe",
			step: map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"persist-credentials": "false",
				},
			},
			expected: false,
		},
		{
			name: "checkout with persist-credentials string true is insecure",
			step: map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"persist-credentials": "true",
				},
			},
			expected: true,
		},
		{
			name: "checkout without persist-credentials key in with block is insecure",
			step: map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"fetch-depth": 1,
				},
			},
			expected: true,
		},
		{
			name: "checkout without version tag is insecure",
			step: map[string]any{
				"uses": "actions/checkout",
				"with": map[string]any{
					"fetch-depth": 0,
				},
			},
			expected: true,
		},
		{
			name: "checkout without version tag with persist-credentials false is safe",
			step: map[string]any{
				"uses": "actions/checkout",
				"with": map[string]any{
					"persist-credentials": false,
				},
			},
			expected: false,
		},
		{
			name: "step with no uses key is safe",
			step: map[string]any{
				"name": "Print message",
				"run":  "echo hello",
			},
			expected: false,
		},
		{
			name: "checkout with SHA pin and persist-credentials false is safe",
			step: map[string]any{
				"uses": "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683",
				"with": map[string]any{
					"persist-credentials": false,
				},
			},
			expected: false,
		},
		{
			name: "checkout with SHA pin without persist-credentials is insecure",
			step: map[string]any{
				"uses": "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683",
			},
			expected: true,
		},
		{
			name: "different action starting with actions/checkout prefix but not checkout itself",
			step: map[string]any{
				"uses": "actions/checkout-extra@v1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkoutMissingPersistCredentialsFalse(tt.step)
			assert.Equal(t, tt.expected, result, "checkoutMissingPersistCredentialsFalse returned unexpected result")
		})
	}
}

// TestValidateCheckoutPersistCredentials_FrontmatterSteps tests the main frontmatter steps validation
func TestValidateCheckoutPersistCredentials_FrontmatterSteps(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		mergedSteps string
		strictMode  bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "no steps section - no error",
			frontmatter: map[string]any{
				"on": "push",
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "empty steps - no error",
			frontmatter: map[string]any{
				"on":    "push",
				"steps": []any{},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "checkout with persist-credentials false - no error",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Checkout",
						"uses": "actions/checkout@v4",
						"with": map[string]any{
							"persist-credentials": false,
						},
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "checkout without persist-credentials false in strict mode - error",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Checkout",
						"uses": "actions/checkout@v4",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: actions/checkout step(s) without 'persist-credentials: false'",
		},
		{
			name: "checkout without persist-credentials false in non-strict mode - warning only",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Checkout",
						"uses": "actions/checkout@v4",
					},
				},
			},
			strictMode:  false,
			expectError: false, // warning only in non-strict mode
		},
		{
			name: "non-checkout step - no error",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Run tests",
						"run":  "go test ./...",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "error includes step name",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "My Checkout Step",
						"uses": "actions/checkout@v4",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "'My Checkout Step'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			err := compiler.validateCheckoutPersistCredentials(tt.frontmatter, tt.mergedSteps)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				require.ErrorContains(t, err, tt.errorMsg, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

// TestValidateCheckoutPersistCredentials_MergedSteps tests imported (merged) steps validation
func TestValidateCheckoutPersistCredentials_MergedSteps(t *testing.T) {
	tests := []struct {
		name        string
		mergedSteps string
		strictMode  bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty merged steps - no error",
			mergedSteps: "",
			strictMode:  true,
			expectError: false,
		},
		{
			name: "imported checkout with persist-credentials false - no error",
			mergedSteps: `- name: Checkout
  uses: actions/checkout@v4
  with:
    persist-credentials: false
`,
			strictMode:  true,
			expectError: false,
		},
		{
			name: "imported checkout without persist-credentials in strict mode - error",
			mergedSteps: `- name: Imported Checkout
  uses: actions/checkout@v4
`,
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: actions/checkout step(s) without 'persist-credentials: false'",
		},
		{
			name: "imported checkout without persist-credentials in non-strict mode - warning only",
			mergedSteps: `- name: Imported Checkout
  uses: actions/checkout@v4
`,
			strictMode:  false,
			expectError: false,
		},
		{
			name: "imported non-checkout step - no error",
			mergedSteps: `- name: Setup node
  uses: actions/setup-node@v4
  with:
    node-version: '24'
`,
			strictMode:  true,
			expectError: false,
		},
		{
			name: "error message includes imported step name",
			mergedSteps: `- name: My Imported Checkout
  uses: actions/checkout@v4
`,
			strictMode:  true,
			expectError: true,
			errorMsg:    "'My Imported Checkout'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			err := compiler.validateCheckoutPersistCredentials(map[string]any{}, tt.mergedSteps)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				require.ErrorContains(t, err, tt.errorMsg, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

// TestValidateCheckoutPersistCredentials_WarningEmitted tests that a warning is emitted in non-strict mode
func TestValidateCheckoutPersistCredentials_WarningEmitted(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = false

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Checkout",
				"uses": "actions/checkout@v4",
			},
		},
	}

	initialWarnings := compiler.GetWarningCount()
	err := compiler.validateCheckoutPersistCredentials(frontmatter, "")
	require.NoError(t, err, "Should not return an error in non-strict mode")
	assert.Greater(t, compiler.GetWarningCount(), initialWarnings, "Warning count should be incremented")
}

// TestValidateCheckoutPersistCredentials_BothSourcesChecked tests that both frontmatter and
// imported steps are checked together
func TestValidateCheckoutPersistCredentials_BothSourcesChecked(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	// Main frontmatter has safe checkout
	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Safe Checkout",
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"persist-credentials": false,
				},
			},
		},
	}

	// Imported steps have insecure checkout
	mergedSteps := `- name: Insecure Imported Checkout
  uses: actions/checkout@v4
`

	err := compiler.validateCheckoutPersistCredentials(frontmatter, mergedSteps)
	require.Error(t, err, "Should error when imported step has insecure checkout")
	require.ErrorContains(t, err, "'Insecure Imported Checkout'", "Error should reference the insecure imported step")
	assert.NotContains(t, err.Error(), "'Safe Checkout'", "Error should not reference the safe step")
}

// TestValidateCheckoutPersistCredentials_MultipleOffenders tests reporting multiple insecure steps
func TestValidateCheckoutPersistCredentials_MultipleOffenders(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "First Checkout",
				"uses": "actions/checkout@v4",
			},
			map[string]any{
				"name": "Second Checkout",
				"uses": "actions/checkout@v4",
			},
		},
	}

	err := compiler.validateCheckoutPersistCredentials(frontmatter, "")
	require.Error(t, err, "Should error when multiple steps have insecure checkout")
	require.ErrorContains(t, err, "'First Checkout'", "Error should mention first step")
	require.ErrorContains(t, err, "'Second Checkout'", "Error should mention second step")
}

// TestStepDisplayName tests the stepDisplayName helper function
func TestStepDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		step     map[string]any
		expected string
	}{
		{
			name:     "step with name",
			step:     map[string]any{"name": "Checkout", "uses": "actions/checkout@v4"},
			expected: "'Checkout'",
		},
		{
			name:     "step without name but with uses",
			step:     map[string]any{"uses": "actions/checkout@v4"},
			expected: "'actions/checkout@v4'",
		},
		{
			name:     "step without name or uses",
			step:     map[string]any{"run": "echo hello"},
			expected: "'<unnamed step>'",
		},
		{
			name:     "step with empty name falls back to uses",
			step:     map[string]any{"name": "", "uses": "actions/checkout@v4"},
			expected: "'actions/checkout@v4'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stepDisplayName(tt.step)
			assert.Equal(t, tt.expected, result, "stepDisplayName returned unexpected value")
		})
	}
}

// TestCheckoutMissingPersistCredentialsFalse_ActionPin tests that action pin comments are handled
func TestCheckoutMissingPersistCredentialsFalse_ActionPin(t *testing.T) {
	// Action pin with SHA and comment should still be recognized as checkout
	step := map[string]any{
		"uses": "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2",
	}
	result := checkoutMissingPersistCredentialsFalse(step)
	assert.True(t, result, "Checkout with action pin comment but no persist-credentials should be flagged")

	// Action pin with SHA, comment, and persist-credentials: false should be safe
	stepSafe := map[string]any{
		"uses": "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2",
		"with": map[string]any{
			"persist-credentials": false,
		},
	}
	resultSafe := checkoutMissingPersistCredentialsFalse(stepSafe)
	assert.False(t, resultSafe, "Checkout with action pin comment and persist-credentials: false should be safe")
}

// TestValidateCheckoutPersistCredentials_GitLeakErrorMessage validates the error mentions token leak
func TestValidateCheckoutPersistCredentials_GitLeakErrorMessage(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"uses": "actions/checkout@v4",
			},
		},
	}

	err := compiler.validateCheckoutPersistCredentials(frontmatter, "")
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "git token") || strings.Contains(err.Error(), ".git/config"),
		"Error should mention git token leak",
	)
	require.ErrorContains(t, err, "persist-credentials: false", "Error should mention the fix")
}
