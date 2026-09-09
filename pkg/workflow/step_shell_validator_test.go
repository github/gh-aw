//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStepShellScripts(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		strictMode  bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "no steps section is allowed",
			frontmatter: map[string]any{
				"on": "push",
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "step without gh command is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Setup",
						"run":  "echo hello",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "uses: action step without run is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Checkout",
						"uses": "actions/checkout@abc123",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "step with gh command and GH_TOKEN is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "List issues",
						"env": map[string]any{
							"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
						},
						"run": "gh issue list",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "step with gh command and GH_TOKEN is allowed (non-strict)",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "List issues",
						"env": map[string]any{
							"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
						},
						"run": "gh issue list",
					},
				},
			},
			strictMode:  false,
			expectError: false,
		},
		{
			name: "step with gh command missing GH_TOKEN is error in strict mode",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "List issues",
						"run":  "gh issue list",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode:",
		},
		{
			name: "step with gh command missing GH_TOKEN is warning in non-strict mode",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "List issues",
						"run":  "gh issue list",
					},
				},
			},
			strictMode:  false,
			expectError: false,
		},
		{
			name: "step with gh command missing env section entirely is error in strict mode",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Create PR",
						"run":  "gh pr create --title 'My PR'",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode:",
		},
		{
			name: "step with env but missing GH_TOKEN is error in strict mode",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Run gh",
						"env": map[string]any{
							"OTHER_VAR": "value",
						},
						"run": "gh repo list",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode:",
		},
		{
			name: "workflow-level GH_TOKEN is inherited by gh step",
			frontmatter: map[string]any{
				"env": map[string]any{
					"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
				},
				"steps": []any{
					map[string]any{
						"name": "List issues",
						"run":  "gh issue list",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "pre-steps with gh command missing GH_TOKEN is error in strict mode",
			frontmatter: map[string]any{
				"pre-steps": []any{
					map[string]any{
						"name": "Pre-step",
						"run":  "gh issue list",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "pre-steps",
		},
		{
			name: "pre-agent-steps with gh command missing GH_TOKEN is error in strict mode",
			frontmatter: map[string]any{
				"pre-agent-steps": []any{
					map[string]any{
						"name": "Pre-agent step",
						"run":  "gh pr list",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "pre-agent-steps",
		},
		{
			name: "post-steps with gh command missing GH_TOKEN is error in strict mode",
			frontmatter: map[string]any{
				"post-steps": []any{
					map[string]any{
						"name": "Post-step",
						"run":  "gh release create v1.0",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "post-steps",
		},
		{
			name: "gh inside command substitution is not detected (known limitation)",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Check PR",
						"run":  "RESULT=$(gh pr view --json state -q .state)",
					},
				},
			},
			strictMode:  true,
			expectError: false, // false negative: $(gh ...) is not matched by the crude heuristic
		},
		{
			name: "script without any gh usage is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Install deps",
						"run":  "npm install && npm test",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "step without name shows unnamed step in error",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"run": "gh issue list",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "unnamed step",
		},
		{
			name: "multiple steps with gh and GH_TOKEN all present is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Step A",
						"env": map[string]any{
							"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
						},
						"run": "gh issue list",
					},
					map[string]any{
						"name": "Step B",
						"env": map[string]any{
							"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
						},
						"run": "gh pr list",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "step with non-gh command named 'gh' prefix is not flagged",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Run custom tool",
						"run":  "github_helper script.sh",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "line containing only gh is detected",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Bare gh",
						"run":  "gh",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode:",
		},
		{
			name: "runner.tool_cache directly interpolated in run is an error",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Set runtime paths",
						"run":  `echo "RUNNER_TOOL_CACHE=${{ runner.tool_cache }}" >> "$GITHUB_ENV"`,
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "Pass it through the step's env:",
		},
		{
			name: "runner.tool_cache in env is allowed",
			frontmatter: map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Set runtime paths",
						"env": map[string]any{
							"GH_AW_RUNNER_TOOL_CACHE": "${{ runner.tool_cache }}",
						},
						"run": `echo "RUNNER_TOOL_CACHE=${GH_AW_RUNNER_TOOL_CACHE}" >> "$GITHUB_ENV"`,
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCompiler(t)
			c.strictMode = tt.strictMode

			err := c.validateStepShellScripts(tt.frontmatter)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckStepGHToken(t *testing.T) {
	tests := []struct {
		name             string
		step             any
		workflowHasToken bool
		expectViolation  bool
	}{
		{
			name:             "non-map step is skipped",
			step:             "echo hello",
			workflowHasToken: false,
			expectViolation:  false,
		},
		{
			name: "step without run field is skipped",
			step: map[string]any{
				"uses": "actions/checkout@abc123",
			},
			workflowHasToken: false,
			expectViolation:  false,
		},
		{
			name: "step with run not using gh is compliant",
			step: map[string]any{
				"run": "npm install",
			},
			workflowHasToken: false,
			expectViolation:  false,
		},
		{
			name: "step with gh and GH_TOKEN is compliant",
			step: map[string]any{
				"run": "gh issue list",
				"env": map[string]any{
					"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
				},
			},
			workflowHasToken: false,
			expectViolation:  false,
		},
		{
			name: "step with gh and missing GH_TOKEN is a violation",
			step: map[string]any{
				"run": "gh issue list",
			},
			workflowHasToken: false,
			expectViolation:  true,
		},
		{
			name: "workflow-level GH_TOKEN makes step compliant",
			step: map[string]any{
				"run": "gh issue list",
			},
			workflowHasToken: true,
			expectViolation:  false,
		},
		{
			name: "step name included in violation message",
			step: map[string]any{
				"name": "My Step",
				"run":  "gh pr create",
			},
			workflowHasToken: false,
			expectViolation:  true,
		},
		{
			name: "unnamed step returns unnamed step marker",
			step: map[string]any{
				"run": "gh pr create",
			},
			workflowHasToken: false,
			expectViolation:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkStepGHToken(tt.step, tt.workflowHasToken)
			if tt.expectViolation {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestCheckStepRunnerToolCache(t *testing.T) {
	assert.NotEmpty(t, checkStepRunnerToolCache(map[string]any{
		"name": "Set runtime paths",
		"run":  `echo "${{ runner.tool_cache }}"`,
	}))
	assert.Empty(t, checkStepRunnerToolCache(map[string]any{
		"env": map[string]any{"GH_AW_RUNNER_TOOL_CACHE": "${{ runner.tool_cache }}"},
		"run": `echo "$GH_AW_RUNNER_TOOL_CACHE"`,
	}))
}

func TestWorkflowEnvHasGHToken(t *testing.T) {
	assert.False(t, workflowEnvHasGHToken(map[string]any{}))

	assert.True(t, workflowEnvHasGHToken(map[string]any{
		"env": map[string]any{
			"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
		},
	}))

	assert.False(t, workflowEnvHasGHToken(map[string]any{
		"env": map[string]any{
			"OTHER_VAR": "value",
		},
	}))

	assert.False(t, workflowEnvHasGHToken(map[string]any{
		"env": "not-a-map",
	}))
}
