//go:build !integration

package workflow

import (
	"testing"
)

func TestClaudeEngine_ParseLogMetrics_Basic(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name          string
		logContent    string
		verbose       bool
		expectNoCrash bool
	}{
		{
			name:          "empty log content",
			logContent:    "",
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name:          "whitespace only",
			logContent:    "   \n\t   \n   ",
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name: "simple log with errors",
			logContent: `Starting process...
Error: Something went wrong
Warning: Deprecated feature
Process completed`,
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name: "verbose mode",
			logContent: `Debug: Starting
Processing...
Debug: Completed`,
			verbose:       true,
			expectNoCrash: true,
		},
		{
			name: "multiline content",
			logContent: `Line 1
Line 2
Line 3
Line 4
Line 5`,
			verbose:       false,
			expectNoCrash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The main test is that this doesn't crash
			func() {
				defer func() {
					if r := recover(); r != nil {
						if tt.expectNoCrash {
							t.Errorf("ParseLogMetrics crashed unexpectedly: %v", r)
						}
					}
				}()

				metrics := engine.ParseLogMetrics(tt.logContent, tt.verbose)

				// Basic validation - should return valid struct
				if metrics.TokenUsage < 0 {
					t.Errorf("TokenUsage should not be negative, got %d", metrics.TokenUsage)
				}
				if metrics.EstimatedCost < 0 {
					t.Errorf("EstimatedCost should not be negative, got %f", metrics.EstimatedCost)
				}
			}()
		})
	}
}

func TestClaudeEngine_ParseLogMetrics_WithDuration(t *testing.T) {
	engine := NewClaudeEngine()

	// Test Claude log with tool calls and duration information
	claudeLogWithDuration := `[
  {
    "type": "assistant",
    "message": {
      "content": [
        {
          "type": "tool_use",
          "id": "tool_123",
          "name": "Bash",
          "input": {
            "command": "echo hello"
          }
        },
        {
          "type": "tool_use",
          "id": "tool_456",
          "name": "mcp__github__search_issues",
          "input": {
            "query": "test"
          }
        }
      ]
    }
  },
  {
    "type": "user",
    "message": {
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "tool_123",
          "content": "hello"
        },
        {
          "type": "tool_result",
          "tool_use_id": "tool_456",
          "content": "found issues"
        }
      ]
    }
  },
  {
    "type": "result",
    "total_cost_usd": 0.005,
    "usage": {
      "input_tokens": 100,
      "output_tokens": 50
    },
    "num_turns": 1,
    "duration_ms": 2500
  }
]`

	metrics := engine.ParseLogMetrics(claudeLogWithDuration, false)

	// Verify basic metrics
	if metrics.EstimatedCost != 0.005 {
		t.Errorf("Expected cost 0.005, got %f", metrics.EstimatedCost)
	}
	if metrics.TokenUsage != 150 {
		t.Errorf("Expected 150 tokens, got %d", metrics.TokenUsage)
	}
	if metrics.Turns != 1 {
		t.Errorf("Expected 1 turn, got %d", metrics.Turns)
	}

	// Verify tool calls were parsed
	if len(metrics.ToolCalls) != 2 {
		t.Fatalf("Expected 2 tool calls, got %d", len(metrics.ToolCalls))
	}

	// Check that both tools have duration set (from total workflow duration)
	for _, toolCall := range metrics.ToolCalls {
		if toolCall.MaxDuration == 0 {
			t.Errorf("Tool %s should have duration set, but MaxDuration is 0", toolCall.Name)
		}
		// Duration should be 2.5 seconds (2500ms)
		expectedDuration := 2500 * 1000000 // 2.5s in nanoseconds
		if int64(toolCall.MaxDuration) != int64(expectedDuration) {
			t.Errorf("Tool %s expected duration %d ns, got %d ns",
				toolCall.Name, expectedDuration, int64(toolCall.MaxDuration))
		}
	}

	// Verify tool names are correctly prettified
	toolNames := make(map[string]bool)
	for _, toolCall := range metrics.ToolCalls {
		toolNames[toolCall.Name] = true
	}

	if !toolNames["bash_echo hello"] {
		t.Error("Expected bash tool to be named 'bash_echo hello'")
	}
	if !toolNames["github_search_issues"] {
		t.Error("Expected github tool to be named 'github_search_issues'")
	}
}

// func TestClaudeEngine_ParseLogMetrics_WithInputSizes(t *testing.T) {
// 	engine := NewClaudeEngine()

// 	// Test Claude log with tool calls that have varying input sizes
// 	claudeLogWithInputSizes := `[
//   {
//     "type": "assistant",
//     "message": {
//       "content": [
//         {
//           "type": "tool_use",
//           "id": "tool_123",
//           "name": "mcp__github__issue_read",
//           "input": {
//             "owner": "test-owner",
//             "repo": "test-repo",
//             "issue_number": 42
//           }
//         },
//         {
//           "type": "tool_use",
//           "id": "tool_456",
//           "name": "Bash",
//           "input": {
//             "command": "echo 'This is a longer command with more content to test input size tracking'"
//           }
//         }
//       ]
//     }
//   },
//   {
//     "type": "user",
//     "message": {
//       "content": [
//         {
//           "type": "tool_result",
//           "tool_use_id": "tool_123",
//           "content": "Issue data with some content that is longer than the input"
//         },
//         {
//           "type": "tool_result",
//           "tool_use_id": "tool_456",
//           "content": "output"
//         }
//       ]
//     }
//   }
// ]`

// 	metrics := engine.ParseLogMetrics(claudeLogWithInputSizes, false)

// 	// Verify tool calls were parsed
// 	if len(metrics.ToolCalls) != 2 {
// 		t.Fatalf("Expected 2 tool calls, got %d", len(metrics.ToolCalls))
// 	}

// 	// Verify that input sizes are captured
// 	for _, toolCall := range metrics.ToolCalls {
// 		if toolCall.MaxInputSize == 0 {
// 			t.Errorf("Tool %s should have MaxInputSize > 0, got %d", toolCall.Name, toolCall.MaxInputSize)
// 		}

// 		// Both tools should have some input size since they have input fields
// 		if toolCall.MaxInputSize < 10 {
// 			t.Errorf("Tool %s MaxInputSize seems too small: %d tokens", toolCall.Name, toolCall.MaxInputSize)
// 		}
// 	}

// 	// Verify output sizes are also captured
// 	for _, toolCall := range metrics.ToolCalls {
// 		if toolCall.MaxOutputSize == 0 {
// 			t.Errorf("Tool %s should have MaxOutputSize > 0, got %d", toolCall.Name, toolCall.MaxOutputSize)
// 		}
// 	}
// }

func TestCodexEngine_ParseLogMetrics_Basic(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name          string
		logContent    string
		verbose       bool
		expectNoCrash bool
	}{
		{
			name:          "empty log content",
			logContent:    "",
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name:          "whitespace only",
			logContent:    "   \n\t   \n   ",
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name: "simple log with errors",
			logContent: `Starting process...
Error: Something went wrong
Warning: Deprecated feature
Process completed`,
			verbose:       false,
			expectNoCrash: true,
		},
		{
			name: "verbose mode",
			logContent: `Debug: Starting
Processing...
Debug: Completed`,
			verbose:       true,
			expectNoCrash: true,
		},
		{
			name: "multiline content",
			logContent: `Line 1
Line 2
Line 3
Line 4
Line 5`,
			verbose:       false,
			expectNoCrash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The main test is that this doesn't crash
			func() {
				defer func() {
					if r := recover(); r != nil {
						if tt.expectNoCrash {
							t.Errorf("ParseLogMetrics crashed unexpectedly: %v", r)
						}
					}
				}()

				metrics := engine.ParseLogMetrics(tt.logContent, tt.verbose)

				// Basic validation - should return valid struct
				if metrics.TokenUsage < 0 {
					t.Errorf("TokenUsage should not be negative, got %d", metrics.TokenUsage)
				}
				// Codex engine doesn't track cost, so it should be 0
				if metrics.EstimatedCost != 0 {
					t.Errorf("Codex engine should have 0 cost, got %f", metrics.EstimatedCost)
				}
			}()
		})
	}
}

func TestCompiler_SetFileTracker_Simple(t *testing.T) {
	// Create compiler
	compiler := NewCompiler()

	// Initial state should have nil tracker
	if compiler.fileTracker != nil {
		t.Errorf("Expected initial fileTracker to be nil")
	}

	// Create mock tracker
	mockTracker := &SimpleMockFileTracker{}

	// Set tracker
	compiler.SetFileTracker(mockTracker)

	// Verify tracker was set
	if compiler.fileTracker != mockTracker {
		t.Errorf("Expected tracker to be set")
	}

	// Set to nil
	compiler.SetFileTracker(nil)

	// Verify tracker is nil
	if compiler.fileTracker != nil {
		t.Errorf("Expected tracker to be nil after setting to nil")
	}
}

// SimpleMockFileTracker is a basic implementation for testing
type SimpleMockFileTracker struct {
	tracked []string
}

func (s *SimpleMockFileTracker) TrackCreated(filePath string) {
	s.tracked = append(s.tracked, filePath)
}
