//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyProjectSafeOutputs(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         map[string]any
		existingSafeOutputs *SafeOutputsConfig
		expectedResult      *SafeOutputsConfig
	}{
		{
			name: "project with URL string - no longer creates safe-outputs",
			frontmatter: map[string]any{
				"project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
			},
			existingSafeOutputs: nil,
			expectedResult:      nil,
		},
		{
			name: "project with existing safe-outputs returns existing",
			frontmatter: map[string]any{
				"project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
			},
			existingSafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				},
			},
			expectedResult: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				},
			},
		},
		{
			name: "no project field - returns existing",
			frontmatter: map[string]any{
				"name": "test-workflow",
			},
			existingSafeOutputs: nil,
			expectedResult:      nil,
		},
		{
			name: "project with blank URL string - returns existing",
			frontmatter: map[string]any{
				"project": "   ",
			},
			existingSafeOutputs: nil,
			expectedResult:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.applyProjectSafeOutputs(tt.frontmatter, tt.existingSafeOutputs)
			assert.Equal(t, tt.expectedResult, result, "Result should match expected")
		})
	}
}
