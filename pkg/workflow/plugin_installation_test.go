//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePluginInstallationSteps(t *testing.T) {
	tests := []struct {
		name         string
		plugins      []string
		engineID     string
		githubToken  string
		expectSteps  int
		expectCmds   []string
		expectTokens []string
	}{
		{
			name:         "No plugins",
			plugins:      []string{},
			engineID:     "copilot",
			githubToken:  "",
			expectSteps:  0,
			expectCmds:   []string{},
			expectTokens: []string{},
		},
		{
			name:         "Single plugin for Copilot",
			plugins:      []string{"github/test-plugin"},
			engineID:     "copilot",
			githubToken:  "${{ secrets.GITHUB_TOKEN }}",
			expectSteps:  1,
			expectCmds:   []string{"copilot install plugin github/test-plugin"},
			expectTokens: []string{"${{ secrets.GITHUB_TOKEN }}"},
		},
		{
			name:        "Multiple plugins for Claude",
			plugins:     []string{"github/plugin1", "acme/plugin2"},
			engineID:    "claude",
			githubToken: "${{ secrets.CUSTOM_TOKEN }}",
			expectSteps: 2,
			expectCmds: []string{
				"claude install plugin github/plugin1",
				"claude install plugin acme/plugin2",
			},
			expectTokens: []string{
				"${{ secrets.CUSTOM_TOKEN }}",
				"${{ secrets.CUSTOM_TOKEN }}",
			},
		},
		{
			name:         "Plugin for Codex",
			plugins:      []string{"org/codex-plugin"},
			engineID:     "codex",
			githubToken:  "",
			expectSteps:  1,
			expectCmds:   []string{"codex install plugin org/codex-plugin"},
			expectTokens: []string{"${{ secrets.GITHUB_TOKEN }}"}, // Default token
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GeneratePluginInstallationSteps(tt.plugins, tt.engineID, tt.githubToken)

			// Verify number of steps
			assert.Len(t, steps, tt.expectSteps, "Number of steps should match")

			// Verify each step
			for i, step := range steps {
				stepText := strings.Join(step, "\n")

				// Verify plugin name in step name (with quotes)
				assert.Contains(t, stepText, fmt.Sprintf("'Install plugin: %s'", tt.plugins[i]),
					"Step should contain quoted plugin name")

				// Verify command
				assert.Contains(t, stepText, tt.expectCmds[i],
					"Step should contain correct install command")

				// Verify GitHub token
				assert.Contains(t, stepText, tt.expectTokens[i],
					"Step should contain correct GitHub token")

				// Verify env section
				assert.Contains(t, stepText, "env:",
					"Step should have env section")
				assert.Contains(t, stepText, "GITHUB_TOKEN:",
					"Step should set GITHUB_TOKEN environment variable")
			}
		})
	}
}

func TestExtractPluginsFromFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    []string
	}{
		{
			name:        "No plugins field",
			frontmatter: map[string]any{},
			expected:    nil,
		},
		{
			name: "Empty plugins array",
			frontmatter: map[string]any{
				"plugins": []any{},
			},
			expected: nil,
		},
		{
			name: "Single plugin",
			frontmatter: map[string]any{
				"plugins": []any{"github/test-plugin"},
			},
			expected: []string{"github/test-plugin"},
		},
		{
			name: "Multiple plugins",
			frontmatter: map[string]any{
				"plugins": []any{"github/plugin1", "acme/plugin2", "org/plugin3"},
			},
			expected: []string{"github/plugin1", "acme/plugin2", "org/plugin3"},
		},
		{
			name: "Mixed types in array (only strings extracted)",
			frontmatter: map[string]any{
				"plugins": []any{"github/plugin1", 123, "acme/plugin2"},
			},
			expected: []string{"github/plugin1", "acme/plugin2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPluginsFromFrontmatter(tt.frontmatter)
			assert.Equal(t, tt.expected, result, "Extracted plugins should match expected")
		})
	}
}

func TestPluginInstallationIntegration(t *testing.T) {
	// Test that plugins are properly integrated into engine installation steps
	engines := []struct {
		engineID string
		engine   CodingAgentEngine
	}{
		{"copilot", NewCopilotEngine()},
		{"claude", NewClaudeEngine()},
		{"codex", NewCodexEngine()},
	}

	for _, e := range engines {
		t.Run(e.engineID, func(t *testing.T) {
			// Create workflow data with plugins
			workflowData := &WorkflowData{
				Name:    "test-workflow",
				Plugins: []string{"github/test-plugin"},
			}

			// Get installation steps
			steps := e.engine.GetInstallationSteps(workflowData)

			// Convert steps to string for searching
			var allStepsText string
			for _, step := range steps {
				allStepsText += strings.Join(step, "\n") + "\n"
			}

			// Verify plugin installation step is present
			assert.Contains(t, allStepsText, fmt.Sprintf("%s install plugin github/test-plugin", e.engineID),
				"Installation steps should include plugin installation command")

			// Verify GITHUB_TOKEN is set
			assert.Contains(t, allStepsText, "GITHUB_TOKEN:",
				"Plugin installation should have GITHUB_TOKEN environment variable")
		})
	}
}
