//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractWorkflowRunConclusionCondition tests the standalone helper that converts
// on.workflow_run.conclusion into a GitHub Actions expression.
func TestExtractWorkflowRunConclusionCondition(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        string
		wantErr     bool
	}{
		{
			name:        "no on field",
			frontmatter: map[string]any{},
			want:        "",
		},
		{
			name: "no workflow_run key",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{},
				},
			},
			want: "",
		},
		{
			name: "workflow_run with no conclusion",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows": []any{"CI"},
						"types":     []any{"completed"},
					},
				},
			},
			want: "",
		},
		{
			name: "single conclusion string",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure",
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure')",
		},
		{
			name: "single conclusion in array",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": []any{"failure"},
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure')",
		},
		{
			name: "multiple conclusions in array",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": []any{"failure", "timed_out"},
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure' || github.event.workflow_run.conclusion == 'timed_out')",
		},
		{
			name: "success conclusion",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"Deploy"},
						"types":      []any{"completed"},
						"conclusion": "success",
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'success')",
		},
		{
			name: "startup failure conclusion",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "startup_failure",
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'startup_failure')",
		},
		{
			name: "workflow_run value is not a map",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": "completed",
				},
			},
			want: "",
		},
		{
			name: "invalid conclusion value is rejected",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "invalid_value",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "conclusion with single quote injection is rejected",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure' || true || '",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractWorkflowRunConclusionCondition(tt.frontmatter)
			if tt.wantErr {
				assert.Error(t, err, "should return error for invalid conclusion value")
				return
			}
			require.NoError(t, err, "should not error for valid conclusion values")
			assert.Equal(t, tt.want, got, "condition should match expected expression")
		})
	}
}

// TestExtractIfConditionMergesWorkflowRunConclusion tests that extractIfCondition
// merges an on.workflow_run.conclusion filter with the existing if expression.
func TestExtractIfConditionMergesWorkflowRunConclusion(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        string
		wantErr     bool
	}{
		{
			name: "conclusion only - no existing if",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure",
					},
				},
			},
			want: "github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure')",
		},
		{
			name: "conclusion merges with existing bare if",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure",
					},
				},
				"if": "github.actor != 'bot'",
			},
			want: "(github.actor != 'bot') && (github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure'))",
		},
		{
			name: "conclusion merges with wrapped ${{ }} if",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure",
					},
				},
				"if": "${{ github.actor != 'bot' }}",
			},
			want: "(github.actor != 'bot') && (github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure'))",
		},
		{
			name: "multiple conclusions merge with existing if",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI", "Deploy"},
						"types":      []any{"completed"},
						"conclusion": []any{"failure", "timed_out"},
					},
				},
				"if": "github.actor != 'dependabot[bot]'",
			},
			want: "(github.actor != 'dependabot[bot]') && (github.event_name != 'workflow_run' || (github.event.workflow_run.conclusion == 'failure' || github.event.workflow_run.conclusion == 'timed_out'))",
		},
		{
			name: "no conclusion - existing if preserved",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows": []any{"CI"},
						"types":     []any{"completed"},
					},
				},
				"if": "${{ github.event.workflow_run.conclusion == 'failure' }}",
			},
			want: "github.event.workflow_run.conclusion == 'failure'",
		},
		{
			name: "invalid conclusion propagates error",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "not_a_real_conclusion",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			got, err := compiler.extractIfCondition(tt.frontmatter)
			if tt.wantErr {
				assert.Error(t, err, "should propagate error for invalid conclusion value")
				return
			}
			require.NoError(t, err, "should not error for valid conclusion values")
			assert.Equal(t, tt.want, got, "merged if condition should match expected expression")
		})
	}
}

// TestWorkflowRunConclusionCommentedInYAML verifies that the conclusion field is
// commented out in the compiled on: YAML section and a comment explaining the filtering
// is appended to the line.
func TestWorkflowRunConclusionCommentedInYAML(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    map[string]any
		wantCommented  bool
		wantOnContains []string
		wantOnAbsent   []string
	}{
		{
			name: "conclusion string is commented out",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": "failure",
					},
				},
			},
			wantCommented:  true,
			wantOnContains: []string{"workflow_run:", "workflows:", "types:", "# conclusion: failure"},
		},
		{
			name: "conclusion array is commented out",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":  []any{"CI"},
						"types":      []any{"completed"},
						"conclusion": []any{"failure", "timed_out"},
					},
				},
			},
			wantCommented:  true,
			wantOnContains: []string{"workflow_run:", "# conclusion:", "# - failure"},
		},
		{
			name: "no conclusion - unmodified",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows": []any{"CI"},
						"types":     []any{"completed"},
					},
				},
			},
			wantCommented:  false,
			wantOnContains: []string{"workflow_run:", "workflows:", "types:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetWorkflowIdentifier("test.md")

			onSection := compiler.extractTopLevelYAMLSection(tt.frontmatter, "on")
			require.NotEmpty(t, onSection, "on section should not be empty")

			if tt.wantCommented {
				assert.Contains(t, onSection, "# Conclusion filtering compiled into if condition",
					"on section should contain conclusion filter comment")
			}

			for _, want := range tt.wantOnContains {
				assert.Contains(t, onSection, want,
					"on section should contain %q", want)
			}
			for _, absent := range tt.wantOnAbsent {
				assert.NotContains(t, onSection, absent,
					"on section should NOT contain %q", absent)
			}
		})
	}
}
