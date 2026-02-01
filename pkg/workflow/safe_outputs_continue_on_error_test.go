//go:build !integration

package workflow

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsContinueOnError tests the continue-on-error configuration parsing and compilation
func TestSafeOutputsContinueOnError(t *testing.T) {
	tests := []struct {
		name                   string
		frontmatter            map[string]any
		expectContinueOnError  bool
		expectInConfig         bool
	}{
		{
			name: "continue-on-error enabled",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"continue-on-error": true,
					"update-project": map[string]any{
						"max": 20,
					},
				},
			},
			expectContinueOnError: true,
			expectInConfig:        true,
		},
		{
			name: "continue-on-error disabled",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"continue-on-error": false,
					"update-project": map[string]any{
						"max": 20,
					},
				},
			},
			expectContinueOnError: false,
			expectInConfig:        true,
		},
		{
			name: "continue-on-error not specified (default false)",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"max": 20,
					},
				},
			},
			expectContinueOnError: false,
			expectInConfig:        false,
		},
		{
			name: "continue-on-error with multiple project handlers",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"continue-on-error": true,
					"update-project": map[string]any{
						"max": 20,
					},
					"create-project-status-update": map[string]any{
						"max": 5,
					},
				},
			},
			expectContinueOnError: true,
			expectInConfig:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Extract safe-outputs configuration
			config := compiler.extractSafeOutputsConfig(tt.frontmatter)
			require.NotNil(t, config, "Safe outputs config should be extracted")

			// Check that ContinueOnError is set correctly
			assert.Equal(t, tt.expectContinueOnError, config.ContinueOnError,
				"ContinueOnError should be %v", tt.expectContinueOnError)

			// If we have project handlers, verify the config is passed through
			if tt.expectInConfig {
				// Build the project handler config
				projectConfig := make(map[string]map[string]any)
				for handlerName, builder := range projectHandlerRegistry {
					if handlerConfig := builder(config); len(handlerConfig) > 0 {
						projectConfig[handlerName] = handlerConfig
					}
				}

				// Verify at least one handler has the continue_on_error flag
				if len(projectConfig) > 0 {
					foundContinueOnError := false
					for handlerName, handlerConfig := range projectConfig {
						if val, exists := handlerConfig["continue_on_error"]; exists {
							foundContinueOnError = true
							assert.Equal(t, tt.expectContinueOnError, val,
								"Handler %s should have continue_on_error=%v", handlerName, tt.expectContinueOnError)
						}
					}
					assert.True(t, foundContinueOnError,
						"At least one project handler should have continue_on_error in config")
				}
			}
		})
	}
}

// TestSafeOutputsContinueOnErrorCompilation tests full workflow compilation with continue-on-error
func TestSafeOutputsContinueOnErrorCompilation(t *testing.T) {
	markdown := `---
on: workflow_dispatch
engine: copilot
project: "https://github.com/orgs/test-org/projects/99999"
safe-outputs:
  continue-on-error: true
  update-project:
    max: 20
  create-project-status-update:
    max: 5
---

# Test Workflow

Test the agent.
`

	// Write markdown to a temp file
	tmpFile := "/tmp/test-continue-on-error.md"
	err := os.WriteFile(tmpFile, []byte(markdown), 0644)
	require.NoError(t, err, "Should write temp file")
	defer os.Remove(tmpFile)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(tmpFile)
	require.NoError(t, err, "Workflow should compile successfully")

	// Read the compiled lock file
	lockFile := "/tmp/test-continue-on-error.lock.yml"
	defer os.Remove(lockFile)

	compiled, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Should read compiled lock file")

	compiledStr := string(compiled)

	// Verify the continue_on_error flag is in the compiled YAML
	assert.Contains(t, compiledStr, "continue_on_error", "Compiled workflow should contain continue_on_error")
	assert.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG",
		"Compiled workflow should contain project handler config")

	// Verify both project handlers have the flag
	assert.Contains(t, compiledStr, `"update_project":`, "Should include update_project handler")
	assert.Contains(t, compiledStr, `"create_project_status_update":`, "Should include create_project_status_update handler")
}
