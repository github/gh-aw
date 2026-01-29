//go:build !integration

package workflow

import (
	"testing"
)

func TestParseTriggerShorthand(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantEvent   string
		wantTypes   []string
		wantFilters map[string]any
		wantConds   []string
		wantNil     bool
		wantErr     bool
	}{
		// Source Control Patterns - Push
		{
			name:    "simple push (left as-is)",
			input:   "push",
			wantNil: true, // Simple triggers left as-is
		},
		{
			name:      "push to branch",
			input:     "push to main",
			wantEvent: "push",
			wantFilters: map[string]any{
				"branches": []string{"main"},
			},
		},
		{
			name:      "push to branch with spaces",
			input:     "push to feature branch",
			wantEvent: "push",
			wantFilters: map[string]any{
				"branches": []string{"feature branch"},
			},
		},
		{
			name:      "push tags",
			input:     "push tags v*",
			wantEvent: "push",
			wantFilters: map[string]any{
				"tags": []string{"v*"},
			},
		},

		// Source Control Patterns - Pull Request
		{
			name:    "simple pull_request (left as-is)",
			input:   "pull_request",
			wantNil: true, // Simple triggers left as-is
		},
		{
			name:    "simple pull (left as-is)",
			input:   "pull",
			wantNil: true, // Simple triggers left as-is
		},
		{
			name:      "pull_request opened",
			input:     "pull_request opened",
			wantEvent: "pull_request",
			wantTypes: []string{"opened"},
		},
		{
			name:      "pull_request merged",
			input:     "pull_request merged",
			wantEvent: "pull_request",
			wantTypes: []string{"closed"},
			wantConds: []string{"github.event.pull_request.merged == true"},
		},
		{
			name:      "pull_request affecting path",
			input:     "pull_request affecting src/**.go",
			wantEvent: "pull_request",
			wantTypes: []string{"opened", "synchronize", "reopened"},
			wantFilters: map[string]any{
				"paths": []string{"src/**.go"},
			},
		},
		{
			name:      "pull_request opened affecting path",
			input:     "pull_request opened affecting docs/**",
			wantEvent: "pull_request",
			wantTypes: []string{"opened"},
			wantFilters: map[string]any{
				"paths": []string{"docs/**"},
			},
		},

		// Issue Patterns
		{
			name:      "issue opened",
			input:     "issue opened",
			wantEvent: "issues",
			wantTypes: []string{"opened"},
		},
		{
			name:      "issue edited",
			input:     "issue edited",
			wantEvent: "issues",
			wantTypes: []string{"edited"},
		},
		{
			name:      "issue closed",
			input:     "issue closed",
			wantEvent: "issues",
			wantTypes: []string{"closed"},
		},
		{
			name:      "issue opened labeled bug",
			input:     "issue opened labeled bug",
			wantEvent: "issues",
			wantTypes: []string{"opened"},
			wantConds: []string{"contains(github.event.issue.labels.*.name, 'bug')"},
		},

		// Discussion Patterns
		{
			name:      "discussion created",
			input:     "discussion created",
			wantEvent: "discussion",
			wantTypes: []string{"created"},
		},
		{
			name:      "discussion edited",
			input:     "discussion edited",
			wantEvent: "discussion",
			wantTypes: []string{"edited"},
		},

		// Manual Invocation Patterns
		{
			name:    "manual",
			input:   "manual",
			wantNil: false, // Returns IR with only workflow_dispatch
		},
		{
			name:    "manual with input",
			input:   "manual with input version",
			wantNil: false,
		},
		{
			name:      "workflow completed",
			input:     "workflow completed ci-test",
			wantEvent: "workflow_run",
			wantTypes: []string{"completed"},
			wantFilters: map[string]any{
				"workflows": []string{"ci-test"},
			},
		},

		// Comment Patterns
		{
			name:      "comment created",
			input:     "comment created",
			wantEvent: "issue_comment",
			wantTypes: []string{"created"},
		},

		// Release Patterns
		{
			name:      "release published",
			input:     "release published",
			wantEvent: "release",
			wantTypes: []string{"published"},
		},
		{
			name:      "release prereleased",
			input:     "release prereleased",
			wantEvent: "release",
			wantTypes: []string{"prereleased"},
		},

		// Repository Patterns
		{
			name:      "repository starred",
			input:     "repository starred",
			wantEvent: "watch",
			wantTypes: []string{"started"},
		},
		{
			name:      "repository forked",
			input:     "repository forked",
			wantEvent: "fork",
		},

		// Security Patterns
		{
			name:      "dependabot pull request",
			input:     "dependabot pull request",
			wantEvent: "pull_request",
			wantTypes: []string{"opened", "synchronize", "reopened"},
			wantConds: []string{"github.actor == 'dependabot[bot]'"},
		},
		{
			name:      "security alert",
			input:     "security alert",
			wantEvent: "code_scanning_alert",
			wantTypes: []string{"created", "reopened", "fixed"},
		},
		{
			name:      "code scanning alert",
			input:     "code scanning alert",
			wantEvent: "code_scanning_alert",
			wantTypes: []string{"created", "reopened", "fixed"},
		},

		// External Integration Patterns
		{
			name:      "api dispatch",
			input:     "api dispatch custom-event",
			wantEvent: "repository_dispatch",
			wantFilters: map[string]any{
				"types": []string{"custom-event"},
			},
		},

		// Invalid/Unrecognized Patterns
		{
			name:    "not a trigger shorthand",
			input:   "some random text",
			wantNil: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir, err := ParseTriggerShorthand(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTriggerShorthand() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTriggerShorthand() unexpected error = %v", err)
				return
			}

			if tt.wantNil {
				if ir != nil {
					t.Errorf("ParseTriggerShorthand() expected nil but got IR with event %s", ir.Event)
				}
				return
			}

			if ir == nil {
				t.Errorf("ParseTriggerShorthand() returned nil when IR was expected")
				return
			}

			if ir.Event != tt.wantEvent {
				t.Errorf("ParseTriggerShorthand() event = %v, want %v", ir.Event, tt.wantEvent)
			}

			if !slicesEqual(ir.Types, tt.wantTypes) {
				t.Errorf("ParseTriggerShorthand() types = %v, want %v", ir.Types, tt.wantTypes)
			}

			if !mapsEqual(ir.Filters, tt.wantFilters) {
				t.Errorf("ParseTriggerShorthand() filters = %v, want %v", ir.Filters, tt.wantFilters)
			}

			if !slicesEqual(ir.Conditions, tt.wantConds) {
				t.Errorf("ParseTriggerShorthand() conditions = %v, want %v", ir.Conditions, tt.wantConds)
			}
		})
	}
}

func TestTriggerIRToYAMLMap(t *testing.T) {
	tests := []struct {
		name string
		ir   *TriggerIR
		want map[string]any
	}{
		{
			name: "simple event",
			ir: &TriggerIR{
				Event: "push",
			},
			want: map[string]any{
				"push": map[string]any{}, // Empty map instead of nil to avoid null in YAML
			},
		},
		{
			name: "event with types",
			ir: &TriggerIR{
				Event: "issues",
				Types: []string{"opened", "edited"},
			},
			want: map[string]any{
				"issues": map[string]any{
					"types": []string{"opened", "edited"},
				},
			},
		},
		{
			name: "event with filters",
			ir: &TriggerIR{
				Event: "push",
				Filters: map[string]any{
					"branches": []string{"main"},
				},
			},
			want: map[string]any{
				"push": map[string]any{
					"branches": []string{"main"},
				},
			},
		},
		{
			name: "event with additional events",
			ir: &TriggerIR{
				Event: "push",
				AdditionalEvents: map[string]any{
					"workflow_dispatch": nil,
				},
			},
			want: map[string]any{
				"push":              map[string]any{}, // Empty map instead of nil
				"workflow_dispatch": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ir.ToYAMLMap()

			if !mapsEqual(got, tt.want) {
				t.Errorf("TriggerIR.ToYAMLMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSourceControlTriggers(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEvent string
		wantTypes []string
		wantErr   bool
	}{
		{
			name:      "pull_request with synchronize",
			input:     "pull_request synchronize",
			wantEvent: "pull_request",
			wantTypes: []string{"synchronize"},
		},
		{
			name:      "pull_request with reopened",
			input:     "pull_request reopened",
			wantEvent: "pull_request",
			wantTypes: []string{"reopened"},
		},
		{
			name:      "pull_request with labeled",
			input:     "pull_request labeled",
			wantEvent: "pull_request",
			wantTypes: []string{"labeled"},
		},
		{
			name:    "invalid push format",
			input:   "push invalid format",
			wantErr: true,
		},
		{
			name:    "invalid pull_request type",
			input:   "pull_request invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir, err := ParseTriggerShorthand(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTriggerShorthand() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTriggerShorthand() unexpected error = %v", err)
				return
			}

			if ir == nil {
				t.Errorf("ParseTriggerShorthand() returned nil")
				return
			}

			if ir.Event != tt.wantEvent {
				t.Errorf("Event = %v, want %v", ir.Event, tt.wantEvent)
			}

			if !slicesEqual(ir.Types, tt.wantTypes) {
				t.Errorf("Types = %v, want %v", ir.Types, tt.wantTypes)
			}
		})
	}
}

func TestParseIssueDiscussionTriggers(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEvent string
		wantTypes []string
		wantErr   bool
	}{
		{
			name:      "issue assigned",
			input:     "issue assigned",
			wantEvent: "issues",
			wantTypes: []string{"assigned"},
		},
		{
			name:      "issue unassigned",
			input:     "issue unassigned",
			wantEvent: "issues",
			wantTypes: []string{"unassigned"},
		},
		{
			name:      "discussion answered",
			input:     "discussion answered",
			wantEvent: "discussion",
			wantTypes: []string{"answered"},
		},
		{
			name:      "discussion unanswered",
			input:     "discussion unanswered",
			wantEvent: "discussion",
			wantTypes: []string{"unanswered"},
		},
		{
			name:    "invalid issue type",
			input:   "issue invalid",
			wantErr: true,
		},
		{
			name:    "invalid discussion type",
			input:   "discussion invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir, err := ParseTriggerShorthand(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTriggerShorthand() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTriggerShorthand() unexpected error = %v", err)
				return
			}

			if ir == nil {
				t.Errorf("ParseTriggerShorthand() returned nil")
				return
			}

			if ir.Event != tt.wantEvent {
				t.Errorf("Event = %v, want %v", ir.Event, tt.wantEvent)
			}

			if !slicesEqual(ir.Types, tt.wantTypes) {
				t.Errorf("Types = %v, want %v", ir.Types, tt.wantTypes)
			}
		})
	}
}

// Helper function to compare maps
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}

	for key, aVal := range a {
		bVal, exists := b[key]
		if !exists {
			return false
		}

		// Handle nil values
		if aVal == nil && bVal == nil {
			continue
		}
		if aVal == nil || bVal == nil {
			return false
		}

		// Handle map values
		if aMap, ok := aVal.(map[string]any); ok {
			if bMap, ok := bVal.(map[string]any); ok {
				if !mapsEqual(aMap, bMap) {
					return false
				}
				continue
			}
			return false
		}

		// Handle slice values
		if aSlice, ok := aVal.([]string); ok {
			if bSlice, ok := bVal.([]string); ok {
				if !slicesEqual(aSlice, bSlice) {
					return false
				}
				continue
			}
			return false
		}

		// Handle []any values
		if aSlice, ok := aVal.([]any); ok {
			if bSlice, ok := bVal.([]any); ok {
				if len(aSlice) != len(bSlice) {
					return false
				}
				for i := range aSlice {
					if aSlice[i] != bSlice[i] {
						return false
					}
				}
				continue
			}
			return false
		}

		// Direct comparison for other types
		if aVal != bVal {
			return false
		}
	}

	return true
}
