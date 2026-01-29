//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateEventFilters(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		// Valid configurations
		{
			name: "valid branches only",
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
			name: "valid branches-ignore only",
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
			name: "valid paths only",
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
			name: "valid paths-ignore only",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid both branches and paths",
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
			name: "valid pull_request with branches",
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
			name: "valid pull_request with paths-ignore",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr: false,
		},
		{
			name:        "valid no on section",
			frontmatter: map[string]any{},
			wantErr:     false,
		},
		{
			name: "valid on section with string value",
			frontmatter: map[string]any{
				"on": "push",
			},
			wantErr: false,
		},
		{
			name: "valid push event with empty map",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid push event with null value",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": nil,
				},
			},
			wantErr: false,
		},

		// Invalid configurations - branches/branches-ignore
		{
			name: "invalid both branches and branches-ignore on push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "push event cannot specify both 'branches' and 'branches-ignore'",
		},
		{
			name: "invalid both branches and branches-ignore on pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "pull_request event cannot specify both 'branches' and 'branches-ignore'",
		},

		// Invalid configurations - paths/paths-ignore
		{
			name: "invalid both paths and paths-ignore on push",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"paths":        []string{"src/**"},
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "push event cannot specify both 'paths' and 'paths-ignore'",
		},
		{
			name: "invalid both paths and paths-ignore on pull_request",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"paths":        []string{"src/**"},
						"paths-ignore": []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "pull_request event cannot specify both 'paths' and 'paths-ignore'",
		},

		// Complex cases
		{
			name: "invalid multiple violations on same event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
						"paths":           []string{"src/**"},
						"paths-ignore":    []string{"docs/**"},
					},
				},
			},
			wantErr:     true,
			errContains: "branches", // Should catch the first violation
		},
		{
			name: "valid one event, invalid another",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []string{"main"},
					},
					"pull_request": map[string]any{
						"branches":        []string{"main"},
						"branches-ignore": []string{"dev"},
					},
				},
			},
			wantErr:     true,
			errContains: "pull_request",
		},
		{
			name: "valid both push and pull_request without conflicts",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []string{"main"},
						"paths":    []string{"src/**"},
					},
					"pull_request": map[string]any{
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
			err := ValidateEventFilters(tt.frontmatter)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEventFilters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateEventFilters() error = %v, should contain %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateFilterExclusivity(t *testing.T) {
	tests := []struct {
		name        string
		eventVal    any
		eventName   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid nil event",
			eventVal:  nil,
			eventName: "push",
			wantErr:   false,
		},
		{
			name:      "valid string event",
			eventVal:  "some-string",
			eventName: "push",
			wantErr:   false,
		},
		{
			name:      "valid empty map",
			eventVal:  map[string]any{},
			eventName: "push",
			wantErr:   false,
		},
		{
			name: "valid single filter",
			eventVal: map[string]any{
				"branches": []string{"main"},
			},
			eventName: "push",
			wantErr:   false,
		},
		{
			name: "invalid branches conflict",
			eventVal: map[string]any{
				"branches":        []string{"main"},
				"branches-ignore": []string{"dev"},
			},
			eventName:   "push",
			wantErr:     true,
			errContains: "branches",
		},
		{
			name: "invalid paths conflict",
			eventVal: map[string]any{
				"paths":        []string{"src/**"},
				"paths-ignore": []string{"docs/**"},
			},
			eventName:   "pull_request",
			wantErr:     true,
			errContains: "paths",
		},
		{
			name: "valid with other fields present",
			eventVal: map[string]any{
				"branches": []string{"main"},
				"types":    []string{"opened"},
				"paths":    []string{"src/**"},
			},
			eventName: "pull_request",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilterExclusivity(tt.eventVal, tt.eventName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFilterExclusivity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateFilterExclusivity() error = %v, should contain %q", err, tt.errContains)
				}
			}
		})
	}
}
