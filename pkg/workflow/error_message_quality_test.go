//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorMessageQuality validates that improved error messages contain:
// 1. What's wrong
// 2. What's expected
// 3. Example of correct usage
func TestErrorMessageQuality(t *testing.T) {
	tests := []struct {
		name             string
		testFunc         func() error
		shouldContain    []string
		shouldNotBeVague bool
	}{
		{
			name: "manual-approval type error includes example",
			testFunc: func() error {
				c := NewCompiler()
				frontmatter := map[string]any{
					"on": map[string]any{
						"manual-approval": 123, // Wrong type
					},
				}
				_, err := c.extractManualApprovalFromOn(frontmatter)
				return err
			},
			shouldContain: []string{
				"must be a string",
				"got",
				"Example:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "invalid on section format includes example",
			testFunc: func() error {
				c := NewCompiler()
				frontmatter := map[string]any{
					"on": []string{"invalid"}, // Wrong type
				}
				_, err := c.extractManualApprovalFromOn(frontmatter)
				return err
			},
			shouldContain: []string{
				"invalid",
				"Expected",
				"Example:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "invalid engine includes valid options and example",
			testFunc: func() error {
				c := NewCompiler()
				return c.validateEngine("invalid-engine")
			},
			shouldContain: []string{
				"invalid engine",
				"Valid engines",
				"copilot",
				"claude",
				"Example:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "MCP missing required property includes example",
			testFunc: func() error {
				tools := map[string]any{
					"http-tool": map[string]any{
						"type": "http",
						// Missing url
					},
				}
				return ValidateMCPConfigs(tools)
			},
			shouldContain: []string{
				"missing required property",
				"url",
				"Example:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "MCP invalid type includes valid options and example",
			testFunc: func() error {
				tools := map[string]any{
					"bad-tool": map[string]any{
						"type":    "invalid",
						"command": "test",
					},
				}
				return ValidateMCPConfigs(tools)
			},
			shouldContain: []string{
				"must be one of",
				"stdio",
				"http",
				"Example:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "MCP both command and container includes guidance",
			testFunc: func() error {
				tools := map[string]any{
					"conflict-tool": map[string]any{
						"type":      "stdio",
						"command":   "node",
						"container": "my-image",
					},
				}
				return ValidateMCPConfigs(tools)
			},
			shouldContain: []string{
				"cannot specify both",
				"Choose one",
				"Example",
			},
			shouldNotBeVague: true,
		},
		{
			name: "docker image not found includes example",
			testFunc: func() error {
				// This would normally try to pull the image, but we're testing the error format
				// We'll skip actual Docker validation in tests
				return nil // Skip this test as it requires Docker
			},
			shouldContain:    nil,
			shouldNotBeVague: false,
		},
		{
			name: "invalid secret name includes format and example",
			testFunc: func() error {
				secrets := []string{"my-secret"} // Invalid: contains hyphen
				return validateSecretReferences(secrets)
			},
			shouldContain: []string{
				"invalid secret name",
				"Start with an uppercase letter",
				"uppercase",
				"Examples:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "tracker-id type error shows actual type and example",
			testFunc: func() error {
				c := NewCompiler()
				frontmatter := map[string]any{
					"tracker-id": 12345678, // Wrong type: integer instead of string
				}
				_, err := c.extractTrackerID(frontmatter)
				return err
			},
			shouldContain: []string{
				"tracker-id must be a string",
				"got",
				"Example:",
				"tracker-id:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "stop-after type error shows actual type and example",
			testFunc: func() error {
				c := NewCompiler()
				frontmatter := map[string]any{
					"on": map[string]any{
						"stop-after": 123, // Wrong type: integer instead of string
					},
				}
				_, err := c.extractStopAfterFromOn(frontmatter)
				return err
			},
			shouldContain: []string{
				"stop-after value must be a string",
				"got",
				"Example:",
				"stop-after:",
			},
			shouldNotBeVague: true,
		},
		{
			name: "MCP property type error shows actual type with %T",
			testFunc: func() error {
				tools := map[string]any{
					"test-tool": map[string]any{
						"type":    "stdio",
						"command": 123, // Wrong type: integer instead of string
					},
				}
				return ValidateMCPConfigs(tools)
			},
			shouldContain: []string{
				"must be a string",
				"got",
				"Example:",
			},
			shouldNotBeVague: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFunc()

			if tt.shouldContain == nil {
				// Skip test
				return
			}

			require.Error(t, err, "Test should produce an error")
			errMsg := err.Error()

			// Check that error contains expected content
			for _, content := range tt.shouldContain {
				assert.Contains(t, errMsg, content,
					"Error message should contain '%s'\nActual error: %s",
					content, errMsg)
			}

			// Check that error is not too vague
			if tt.shouldNotBeVague {
				// Error should be longer than just a few words
				assert.Greater(t, len(errMsg), 30,
					"Error message should be descriptive (>30 chars)\nActual: %s", errMsg)

				// Should not be just "invalid X" or "error"
				vaguePhrases := []string{
					"error",
					"invalid",
					"failed",
				}
				wordCount := len(strings.Fields(errMsg))
				if wordCount < 5 {
					for _, phrase := range vaguePhrases {
						if errMsg == phrase || strings.HasPrefix(errMsg, phrase+":") {
							t.Errorf("Error message is too vague: %s", errMsg)
						}
					}
				}
			}
		})
	}
}

// TestMultipleEngineErrorMessage tests the specific error when multiple engines are defined
func TestMultipleEngineErrorMessage(t *testing.T) {
	c := NewCompiler()

	err := c.validateEngine("invalid")
	require.Error(t, err)

	// Should explain what's wrong
	assert.Contains(t, err.Error(), "invalid engine")

	// Should list valid options
	assert.Contains(t, err.Error(), "copilot")
	assert.Contains(t, err.Error(), "claude")
	assert.Contains(t, err.Error(), "codex")

	// Should include example
	assert.Contains(t, err.Error(), "Example:")
}

// TestMCPValidationErrorQuality tests MCP validation error messages
func TestMCPValidationErrorQuality(t *testing.T) {
	tests := []struct {
		name          string
		tools         map[string]any
		errorContains []string
	}{
		{
			name: "missing command or container",
			tools: map[string]any{
				"incomplete": map[string]any{
					"type": "stdio",
					// Missing both command and container
				},
			},
			errorContains: []string{
				"must specify either",
				"command",
				"container",
				"Example",
			},
		},
		{
			name: "http type cannot use container",
			tools: map[string]any{
				"http-with-container": map[string]any{
					"type":      "http",
					"url":       "https://example.com",
					"container": "my-image",
				},
			},
			errorContains: []string{
				"cannot use",
				"container",
				"http",
				"Example:",
			},
		},
		{
			name: "unknown property in tool config",
			tools: map[string]any{
				"typo-tool": map[string]any{
					"typ":     "stdio", // Typo: should be "type"
					"command": "test",
				},
			},
			errorContains: []string{
				"unknown property",
				"Valid properties",
				"Example:",
			},
		},
		{
			name: "type field wrong type",
			tools: map[string]any{
				"bad-type": map[string]any{
					"type":    123, // Should be string
					"command": "test",
				},
			},
			errorContains: []string{
				"type",
				"must be a string",
				"got int",
				"Valid types per MCP Gateway Specification:",
				"stdio",
				"http",
				"Example:",
				"tools:",
			},
		},
		{
			name: "both command and container specified",
			tools: map[string]any{
				"conflict-tool": map[string]any{
					"type":      "stdio",
					"command":   "node",
					"container": "my-image",
				},
			},
			errorContains: []string{
				"cannot specify both",
				"command",
				"container",
				"Choose one",
				"Example",
				"tools:",
			},
		},
		{
			name: "invalid type value",
			tools: map[string]any{
				"bad-type-value": map[string]any{
					"type":    "websocket",
					"command": "test",
				},
			},
			errorContains: []string{
				"type",
				"must be one of:",
				"stdio",
				"http",
				"local",
				"websocket",
				"Example:",
				"tools:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPConfigs(tt.tools)
			require.Error(t, err)

			errMsg := err.Error()
			for _, expected := range tt.errorContains {
				assert.Contains(t, errMsg, expected,
					"Error should contain '%s'\nActual: %s",
					expected, errMsg)
			}
		})
	}
}
