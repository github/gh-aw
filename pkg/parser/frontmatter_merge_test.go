//go:build !integration

package parser

import (
	"encoding/json"
	"testing"
)

func TestMergeTools(t *testing.T) {
	tests := []struct {
		name       string
		base       map[string]any
		additional map[string]any
		expected   map[string]any
	}{
		{
			name: "merge with allowed arrays",
			base: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls", "cat"},
				},
			},
			additional: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"grep", "ls"}, // ls is duplicate
				},
			},
			expected: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls", "cat", "grep"},
				},
			},
		},
		{
			name: "new tool added",
			base: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls"},
				},
			},
			additional: map[string]any{
				"github": map[string]any{
					"allowed": []any{"create_issue"},
				},
			},
			expected: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls"},
				},
				"github": map[string]any{
					"allowed": []any{"create_issue"},
				},
			},
		},
		{
			name: "empty base",
			base: map[string]any{},
			additional: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls"},
				},
			},
			expected: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"ls"},
				},
			},
		},
		{
			name: "merge neutral tools with maps (no Claude-specific logic)",
			base: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
				"bash": map[string]any{
					"allowed": []any{"ls", "cat"},
				},
			},
			additional: map[string]any{
				"bash": map[string]any{
					"allowed": []any{"grep", "ps"},
				},
			},
			expected: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
				"bash": map[string]any{
					"allowed": []any{"ls", "cat", "grep", "ps"},
				},
			},
		},
		{
			name: "merge neutral tools with different allowed arrays",
			base: map[string]any{
				"web-fetch": map[string]any{
					"allowed": []any{"get", "post"},
				},
			},
			additional: map[string]any{
				"web-fetch": map[string]any{
					"allowed": []any{"put", "get"}, // get is duplicate
				},
			},
			expected: map[string]any{
				"web-fetch": map[string]any{
					"allowed": []any{"get", "post", "put"},
				},
			},
		},
		{
			name: "merge mcp tools with wildcard allowed",
			base: map[string]any{
				"notion": map[string]any{
					"type":    "mcp",
					"allowed": []any{"create_page"},
				},
			},
			additional: map[string]any{
				"notion": map[string]any{
					"type":    "mcp",
					"allowed": []any{"*"},
				},
			},
			expected: map[string]any{
				"notion": map[string]any{
					"type":    "mcp",
					"allowed": []any{"create_page", "*"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeTools(tt.base, tt.additional)
			if err != nil {
				t.Fatalf("MergeTools() returned unexpected error: %v", err)
			}

			// Convert result to JSON and back for easier comparison
			resultJSON, _ := json.Marshal(result)
			expectedJSON, _ := json.Marshal(tt.expected)

			var resultMap, expectedMap map[string]any
			if err := json.Unmarshal(resultJSON, &resultMap); err != nil {
				t.Fatalf("Failed to unmarshal result JSON: %v", err)
			}
			if err := json.Unmarshal(expectedJSON, &expectedMap); err != nil {
				t.Fatalf("Failed to unmarshal expected JSON: %v", err)
			}

			// Compare JSON strings for easier debugging
			if string(resultJSON) != string(expectedJSON) {
				t.Errorf("MergeTools() = %s, want %s", string(resultJSON), string(expectedJSON))
			}
		})
	}
}

func TestMergeToolsFromJSON(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
		wantErr  bool
	}{
		{
			name:     "single valid JSON object",
			content:  `{"tool1": {"enabled": true}, "tool2": {"enabled": false}}`,
			expected: `{"tool1":{"enabled":true},"tool2":{"enabled":false}}`,
			wantErr:  false,
		},
		{
			name: "multiple JSON objects on separate lines",
			content: `{"tool1": {"enabled": true}}
{"tool2": {"enabled": false}}
{"tool3": {"config": "value"}}`,
			expected: `{"tool1":{"enabled":true},"tool2":{"enabled":false},"tool3":{"config":"value"}}`,
			wantErr:  false,
		},
		{
			name:     "empty content",
			content:  ``,
			expected: `{}`,
			wantErr:  false,
		},
		{
			name:     "empty JSON objects",
			content:  `{}\n{}\n{}`,
			expected: `{}`,
			wantErr:  false,
		},
		{
			name:     "whitespace only",
			content:  `   \n  \t  \n   `,
			expected: `{}`,
			wantErr:  false,
		},
		{
			name: "mixed empty and non-empty objects",
			content: `{}
{"tool1": {"enabled": true}}
{}
{"tool2": {"value": 42}}`,
			expected: `{"tool1":{"enabled":true},"tool2":{"value":42}}`,
			wantErr:  false,
		},
		{
			name: "objects with overlapping keys",
			content: `{"tool1": {"enabled": true, "config": "old"}}
{"tool1": {"config": "new", "version": 2}}`,
			expected: `{"tool1":{"config":"new","enabled":true,"version":2}}`,
			wantErr:  false,
		},
		{
			name:     "invalid JSON",
			content:  `{"invalid": json}`,
			expected: `{}`,
			wantErr:  false, // Function handles invalid JSON gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeToolsFromJSON(tt.content)

			if tt.wantErr {
				if err == nil {
					t.Errorf("mergeToolsFromJSON(%q) expected error, but got none", tt.content)
				}
				return
			}

			if err != nil {
				t.Errorf("mergeToolsFromJSON(%q) unexpected error: %v", tt.content, err)
				return
			}

			// For JSON comparison, parse both strings to ensure equivalent content
			var gotObj, expectedObj map[string]any
			if err := json.Unmarshal([]byte(got), &gotObj); err != nil {
				t.Errorf("mergeToolsFromJSON(%q) returned invalid JSON: %v", tt.content, err)
				return
			}
			if err := json.Unmarshal([]byte(tt.expected), &expectedObj); err != nil {
				t.Errorf("Test case has invalid expected JSON: %v", err)
				return
			}

			// Convert back to JSON strings for comparison
			gotJSON, _ := json.Marshal(gotObj)
			expectedJSON, _ := json.Marshal(expectedObj)

			if string(gotJSON) != string(expectedJSON) {
				t.Errorf("mergeToolsFromJSON(%q) = %q, want %q", tt.content, string(gotJSON), string(expectedJSON))
			}
		})
	}
}

// Test StripANSI function
