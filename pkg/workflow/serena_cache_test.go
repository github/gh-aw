//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSerenaEnabled(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: false,
		},
		{
			name:     "no tools configured",
			data:     &WorkflowData{},
			expected: false,
		},
		{
			name: "serena in parsed tools",
			data: &WorkflowData{
				ParsedTools: &ToolsConfig{
					Serena: &SerenaToolConfig{
						ShortSyntax: []string{"go"},
					},
				},
			},
			expected: true,
		},
		{
			name: "serena in tools map",
			data: &WorkflowData{
				Tools: map[string]any{
					"serena": []string{"go"},
				},
			},
			expected: true,
		},
		{
			name: "other tools but not serena",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{},
					"bash":   true,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSerenaEnabled(tt.data)
			assert.Equal(t, tt.expected, result, "isSerenaEnabled result mismatch")
		})
	}
}

func TestGenerateSerenaCacheStep(t *testing.T) {
	tests := []struct {
		name           string
		data           *WorkflowData
		needsCheckout  bool
		expectCache    bool
		checkContents  []string
	}{
		{
			name: "serena enabled with checkout",
			data: &WorkflowData{
				ParsedTools: &ToolsConfig{
					Serena: &SerenaToolConfig{
						ShortSyntax: []string{"go"},
					},
				},
			},
			needsCheckout: true,
			expectCache:   true,
			checkContents: []string{
				"name: Cache Serena",
				"uses: actions/cache@",
				"continue-on-error: true",
				"path: .serena/cache",
				"key: serena-${{ runner.os }}-${{ github.run_id }}-${{ github.run_attempt }}",
				"restore-keys: |",
				"serena-${{ runner.os }}-",
				"save-always: true",
			},
		},
		{
			name: "serena enabled without checkout",
			data: &WorkflowData{
				ParsedTools: &ToolsConfig{
					Serena: &SerenaToolConfig{
						ShortSyntax: []string{"go"},
					},
				},
			},
			needsCheckout: false,
			expectCache:   false,
		},
		{
			name: "serena not enabled",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{},
				},
			},
			needsCheckout: true,
			expectCache:   false,
		},
		{
			name:          "no tools configured",
			data:          &WorkflowData{},
			needsCheckout: true,
			expectCache:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			compiler := NewCompiler()

			compiler.generateSerenaCacheStep(&yaml, tt.data, tt.needsCheckout)
			result := yaml.String()

			if tt.expectCache {
				require.NotEmpty(t, result, "Expected cache step to be generated")
				for _, content := range tt.checkContents {
					assert.Contains(t, result, content, "Missing expected content")
				}
			} else {
				assert.Empty(t, result, "Expected no cache step to be generated")
			}
		})
	}
}

func TestSerenaCacheStepPlacement(t *testing.T) {
	// Test that cache step is added in the correct position
	var yaml strings.Builder
	compiler := NewCompiler()

	data := &WorkflowData{
		ParsedTools: &ToolsConfig{
			Serena: &SerenaToolConfig{
				ShortSyntax: []string{"go"},
			},
		},
	}

	// Add checkout step manually
	yaml.WriteString("      - name: Checkout repository\n")
	yaml.WriteString("        uses: actions/checkout@v4\n")

	// Generate Serena cache step
	compiler.generateSerenaCacheStep(&yaml, data, true)

	result := yaml.String()

	// Verify cache step is present and properly formatted
	assert.Contains(t, result, "name: Cache Serena", "Cache step should be present")
	assert.Contains(t, result, "path: .serena/cache", "Cache path should be .serena/cache")
	assert.Contains(t, result, "continue-on-error: true", "Cache should continue on error")
	assert.Contains(t, result, "save-always: true", "Cache should use save-always")

	// Verify cache comes after checkout
	checkoutIndex := strings.Index(result, "name: Checkout repository")
	cacheIndex := strings.Index(result, "name: Cache Serena")
	assert.True(t, cacheIndex > checkoutIndex, "Cache step should come after checkout")
}
