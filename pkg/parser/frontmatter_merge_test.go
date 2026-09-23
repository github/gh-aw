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
			name:       "merge tool profiles from string and array",
			base:       map[string]any{"profile": "go"},
			additional: map[string]any{"profile": []any{"go"}},
			expected:   map[string]any{"profile": []any{"go"}},
		},
		{
			name:       "merge tool profiles in declaration order",
			base:       map[string]any{"profile": []any{"go"}},
			additional: map[string]any{"profile": "future"},
			expected:   map[string]any{"profile": []any{"go", "future"}},
		},
		{
			name:       "preserve a premerged MCP server named profile",
			base:       map[string]any{"profile": map[string]any{"type": "http", "url": "https://example.invalid/mcp"}},
			additional: map[string]any{"bash": false},
			expected: map[string]any{
				"profile": map[string]any{"type": "http", "url": "https://example.invalid/mcp"},
				"bash":    false,
			},
		},
		{
			name:       "preserve an imported MCP server named profile",
			base:       map[string]any{"bash": false},
			additional: map[string]any{"profile": map[string]any{"type": "http", "url": "https://example.invalid/mcp"}},
			expected: map[string]any{
				"bash":    false,
				"profile": map[string]any{"type": "http", "url": "https://example.invalid/mcp"},
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
		{
			name: "base false overrides additional map (tools.github: false)",
			base: map[string]any{
				"github": false,
			},
			additional: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			expected: map[string]any{
				"github": false,
			},
		},
		{
			name: "base map not overridden by additional false",
			base: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			additional: map[string]any{
				"github": false,
			},
			expected: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
		},
		{
			name: "base scalar values override additional scalars in nested maps",
			base: map[string]any{
				"tool1": map[string]any{
					"config": "base-value",
					"other":  "kept",
				},
			},
			additional: map[string]any{
				"tool1": map[string]any{
					"config":  "additional-value",
					"new-key": "added",
				},
			},
			expected: map[string]any{
				"tool1": map[string]any{
					"config":  "base-value",
					"other":   "kept",
					"new-key": "added",
				},
			},
		},
		{
			name: "main runtime overrides imported runtime",
			base: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"runtime": "sbx",
					},
				},
			},
			additional: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"runtime": "docker",
					},
				},
			},
			expected: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"runtime": "sbx",
					},
				},
			},
		},
		{
			name: "imported runtime fills missing main field",
			base: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"timeout": 30,
					},
				},
			},
			additional: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"runtime": "sbx",
					},
				},
			},
			expected: map[string]any{
				"github": map[string]any{
					"nested-config": map[string]any{
						"timeout": 30,
						"runtime": "sbx",
					},
				},
			},
		},
		{
			// bash: true in main workflow (or parent import) must win over an import's
			// specific bash command list, e.g. ["ls", "cat"]. Both are valid bash tool
			// configurations, but they are different types (bool vs array).
			name: "base bash true overrides additional bash array",
			base: map[string]any{
				"bash": true,
			},
			additional: map[string]any{
				"bash": []any{"ls", "cat"},
			},
			expected: map[string]any{
				"bash": true,
			},
		},
		{
			// A parent import's specific bash command list must not be overridden by a
			// child import's unrestricted bash: true.
			name: "base bash array not overridden by additional bash true",
			base: map[string]any{
				"bash": []any{"ls", "cat"},
			},
			additional: map[string]any{
				"bash": true,
			},
			expected: map[string]any{
				"bash": []any{"ls", "cat"},
			},
		},
		{
			// cache-memory: {key: "specific-name"} in a parent import must win over a
			// child import's generic cache-memory: true. The specific key provides
			// more information and should not be overwritten by a generic boolean.
			name: "base cache-memory map with key overrides additional cache-memory true",
			base: map[string]any{
				"cache-memory": map[string]any{
					"key": "my-specific-cache-key-${{ github.run_id }}",
				},
			},
			additional: map[string]any{
				"cache-memory": true,
			},
			expected: map[string]any{
				"cache-memory": map[string]any{
					"key": "my-specific-cache-key-${{ github.run_id }}",
				},
			},
		},
		{
			// cache-memory: true in main workflow must not be overridden by an import's
			// specific cache-memory map configuration.
			name: "base cache-memory true not overridden by additional cache-memory map",
			base: map[string]any{
				"cache-memory": true,
			},
			additional: map[string]any{
				"cache-memory": map[string]any{
					"key": "import-specific-key-${{ github.run_id }}",
				},
			},
			expected: map[string]any{
				"cache-memory": true,
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
			resultJSON, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("json.Marshal(result) failed: %v", err)
			}
			expectedJSON, err := json.Marshal(tt.expected)
			if err != nil {
				t.Fatalf("json.Marshal(tt.expected) failed: %v", err)
			}

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

func TestMergeToolsRejectsInvalidProfileValue(t *testing.T) {
	for _, test := range []struct {
		name       string
		base       map[string]any
		additional map[string]any
	}{
		{name: "invalid base type", base: map[string]any{"profile": true}, additional: map[string]any{}},
		{name: "invalid additional type", base: map[string]any{}, additional: map[string]any{"profile": true}},
		{name: "empty array", base: map[string]any{}, additional: map[string]any{"profile": []any{}}},
		{name: "mixed array", base: map[string]any{}, additional: map[string]any{"profile": []any{"go", 7}}},
		{name: "duplicate array", base: map[string]any{}, additional: map[string]any{"profile": []any{"go", "go"}}},
		{name: "blank profile", base: map[string]any{}, additional: map[string]any{"profile": ""}},
		{name: "MCP server collision", base: map[string]any{"profile": map[string]any{"type": "http"}}, additional: map[string]any{"profile": "go"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := MergeTools(test.base, test.additional); err == nil {
				t.Fatal("MergeTools() must reject an invalid imported profile value")
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
			name:    "single invalid profile object",
			content: `{"profile":[]}`,
			wantErr: true,
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
			expected: `{"tool1":{"config":"old","enabled":true,"version":2}}`,
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
			gotJSON, err := json.Marshal(gotObj)
			if err != nil {
				t.Fatalf("json.Marshal(gotObj) failed: %v", err)
			}
			expectedJSON, err := json.Marshal(expectedObj)
			if err != nil {
				t.Fatalf("json.Marshal(expectedObj) failed: %v", err)
			}

			if string(gotJSON) != string(expectedJSON) {
				t.Errorf("mergeToolsFromJSON(%q) = %q, want %q", tt.content, string(gotJSON), string(expectedJSON))
			}
		})
	}
}
