//go:build !integration

package parser

import (
	"strings"
	"testing"
)

// TestValidateOneOfConstraints tests the oneOf constraints added to the schema
// to prevent mutually exclusive fields from being specified together.
func TestValidateOneOfConstraints(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		// branches vs branches-ignore in push event
		{
			name: "invalid: both branches and branches-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only branches in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []string{"main"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: only branches-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: neither branches nor branches-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"tags": []string{"v*"},
					},
				},
			},
			wantErr: false,
		},

		// paths vs paths-ignore in push event
		{
			name: "invalid: both paths and paths-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"paths":        []string{"src/**"},
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only paths in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"paths": []string{"src/**"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: only paths-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr: false,
		},

		// branches vs branches-ignore in pull_request event
		{
			name: "invalid: both branches and branches-ignore in pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only branches in pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"branches": []string{"main"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: only branches-ignore in pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr: false,
		},

		// paths vs paths-ignore in pull_request event
		{
			name: "invalid: both paths and paths-ignore in pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"paths":        []string{"src/**"},
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only paths in pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"paths": []string{"src/**"},
					},
				},
			},
			wantErr: false,
		},

		// branches vs branches-ignore in pull_request_target event
		{
			name: "invalid: both branches and branches-ignore in pull_request_target",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only branches in pull_request_target",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"branches": []string{"main"},
					},
				},
			},
			wantErr: false,
		},

		// paths vs paths-ignore in pull_request_target event
		{
			name: "invalid: both paths and paths-ignore in pull_request_target",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"paths":        []string{"src/**"},
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},

		// branches vs branches-ignore in workflow_run event
		{
			name: "invalid: both branches and branches-ignore in workflow_run",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows":       []string{"CI"},
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "valid: only branches in workflow_run",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_run": map[string]any{
						"workflows": []string{"CI"},
						"branches":  []string{"main"},
					},
				},
			},
			wantErr: false,
		},

		// slash_command vs label events
		{
			name: "invalid: slash_command with label event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": "mybot",
					"label": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			wantErr:     true,
			errContains: "not",
		},
		{
			name: "valid: slash_command without label event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": "mybot",
				},
			},
			wantErr: false,
		},
		{
			name: "valid: label event without slash_command",
			frontmatter: map[string]any{
				"on": map[string]any{
					"label": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			wantErr: false,
		},

		// command vs label events (deprecated command field)
		{
			name: "invalid: command with label event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "mybot",
					"label": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			wantErr:     true,
			errContains: "not",
		},

		// Valid combinations of branches and paths
		{
			name: "valid: branches and paths in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []string{"main"},
						"paths":    []string{"src/**"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: branches-ignore and paths-ignore in push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches-ignore": []string{"dev"},
						"paths-ignore":    []string{"docs/**"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() error = %v", err)
				return
			}

			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() error = %v, expected to contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}
