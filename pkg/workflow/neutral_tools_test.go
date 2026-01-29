//go:build !integration

package workflow

import (
	"testing"
)

func TestExpandNeutralToolsToClaudeTools(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name:  "empty tools",
			input: map[string]any{},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{},
				},
			},
		},
		{
			name: "bash tool with commands",
			input: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Bash": []any{"echo", "ls"},
					},
				},
			},
		},
		{
			name: "bash tool with nil (all commands)",
			input: map[string]any{
				"bash": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Bash": nil,
					},
				},
			},
		},
		{
			name: "web-fetch tool",
			input: map[string]any{
				"web-fetch": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"WebFetch": nil,
					},
				},
			},
		},
		{
			name: "web-search tool",
			input: map[string]any{
				"web-search": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"WebSearch": nil,
					},
				},
			},
		},
		{
			name: "edit tool",
			input: map[string]any{
				"edit": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Edit":         nil,
						"MultiEdit":    nil,
						"NotebookEdit": nil,
						"Write":        nil,
					},
				},
			},
		},
		{
			name: "playwright tool",
			input: map[string]any{
				"playwright": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{},
				},
				"playwright": map[string]any{
					"allowed": []any{
						"browser_click",
						"browser_close",
						"browser_console_messages",
						"browser_drag",
						"browser_evaluate",
						"browser_file_upload",
						"browser_fill_form",
						"browser_handle_dialog",
						"browser_hover",
						"browser_install",
						"browser_navigate",
						"browser_navigate_back",
						"browser_network_requests",
						"browser_press_key",
						"browser_resize",
						"browser_select_option",
						"browser_snapshot",
						"browser_tabs",
						"browser_take_screenshot",
						"browser_type",
						"browser_wait_for",
					},
				},
			},
		},
		{
			name: "all neutral tools",
			input: map[string]any{
				"bash":       []any{"echo"},
				"web-fetch":  nil,
				"web-search": nil,
				"edit":       nil,
				"playwright": nil,
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Bash":         []any{"echo"},
						"WebFetch":     nil,
						"WebSearch":    nil,
						"Edit":         nil,
						"MultiEdit":    nil,
						"NotebookEdit": nil,
						"Write":        nil,
					},
				},
				"playwright": map[string]any{
					"allowed": []any{
						"browser_click",
						"browser_close",
						"browser_console_messages",
						"browser_drag",
						"browser_evaluate",
						"browser_file_upload",
						"browser_fill_form",
						"browser_handle_dialog",
						"browser_hover",
						"browser_install",
						"browser_navigate",
						"browser_navigate_back",
						"browser_network_requests",
						"browser_press_key",
						"browser_resize",
						"browser_select_option",
						"browser_snapshot",
						"browser_tabs",
						"browser_take_screenshot",
						"browser_type",
						"browser_wait_for",
					},
				},
			},
		},
		{
			name: "neutral tools mixed with MCP tools",
			input: map[string]any{
				"bash": []any{"echo"},
				"edit": nil,
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Bash":         []any{"echo"},
						"Edit":         nil,
						"MultiEdit":    nil,
						"NotebookEdit": nil,
						"Write":        nil,
					},
				},
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
		},
		{
			name: "existing claude tools with neutral tools",
			input: map[string]any{
				"bash": []any{"echo"},
				"claude": map[string]any{
					"allowed": map[string]any{
						"Read": nil,
					},
				},
			},
			expected: map[string]any{
				"claude": map[string]any{
					"allowed": map[string]any{
						"Read": nil,
						"Bash": []any{"echo"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.expandNeutralToolsToClaudeTools(tt.input)

			// Check claude section
			claudeResult, hasClaudeResult := result["claude"]
			claudeExpected, hasClaudeExpected := tt.expected["claude"]

			if hasClaudeExpected != hasClaudeResult {
				t.Errorf("Claude section presence mismatch. Expected: %v, Got: %v", hasClaudeExpected, hasClaudeResult)
				return
			}

			if hasClaudeExpected {
				claudeResultMap, ok1 := claudeResult.(map[string]any)
				claudeExpectedMap, ok2 := claudeExpected.(map[string]any)

				if !ok1 || !ok2 {
					t.Errorf("Claude section type mismatch")
					return
				}

				allowedResult, hasAllowedResult := claudeResultMap["allowed"]
				allowedExpected, hasAllowedExpected := claudeExpectedMap["allowed"]

				if hasAllowedExpected != hasAllowedResult {
					t.Errorf("Claude allowed section presence mismatch. Expected: %v, Got: %v", hasAllowedExpected, hasAllowedResult)
					return
				}

				if hasAllowedExpected {
					allowedResultMap, ok1 := allowedResult.(map[string]any)
					allowedExpectedMap, ok2 := allowedExpected.(map[string]any)

					if !ok1 || !ok2 {
						t.Errorf("Claude allowed section type mismatch")
						return
					}

					// Check that all expected tools are present
					for toolName, expectedValue := range allowedExpectedMap {
						actualValue, exists := allowedResultMap[toolName]
						if !exists {
							t.Errorf("Expected tool '%s' not found in result", toolName)
							continue
						}

						// Compare values
						if !compareValues(expectedValue, actualValue) {
							t.Errorf("Tool '%s' value mismatch. Expected: %v, Got: %v", toolName, expectedValue, actualValue)
						}
					}

					// Check that no unexpected tools are present
					for toolName := range allowedResultMap {
						if _, expected := allowedExpectedMap[toolName]; !expected {
							t.Errorf("Unexpected tool '%s' found in result", toolName)
						}
					}
				}
			}

			// Check other sections (MCP tools, etc.)
			for key, expectedValue := range tt.expected {
				if key == "claude" {
					continue // Already checked above
				}

				actualValue, exists := result[key]
				if !exists {
					t.Errorf("Expected section '%s' not found in result", key)
					continue
				}

				if !compareValues(expectedValue, actualValue) {
					t.Errorf("Section '%s' value mismatch. Expected: %v, Got: %v", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// compareValues compares two any values for equality
func compareValues(expected, actual any) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}

	switch exp := expected.(type) {
	case []any:
		act, ok := actual.([]any)
		if !ok {
			return false
		}
		if len(exp) != len(act) {
			return false
		}
		for i, v := range exp {
			if !compareValues(v, act[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		if len(exp) != len(act) {
			return false
		}
		for k, v := range exp {
			if !compareValues(v, act[k]) {
				return false
			}
		}
		return true
	default:
		return expected == actual
	}
}
