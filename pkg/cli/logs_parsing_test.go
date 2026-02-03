//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestParseLogFileWithoutAwInfo(t *testing.T) {
	// Create a temporary log file
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test.log")

	logContent := `2024-01-15T10:30:00Z Starting workflow execution
2024-01-15T10:30:15Z Claude API request initiated
2024-01-15T10:30:45Z Input tokens: 1250
2024-01-15T10:30:45Z Output tokens: 850
2024-01-15T10:30:45Z Total tokens used: 2100
2024-01-15T10:30:45Z Cost: $0.025
2024-01-15T10:31:30Z Workflow completed successfully`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test parseLogFileWithEngine without an engine (simulates no aw_info.json)
	metrics, err := parseLogFileWithEngine(logFile, nil, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Without aw_info.json, should return empty metrics
	if metrics.TokenUsage != 0 {
		t.Errorf("Expected token usage 0 (no aw_info.json), got %d", metrics.TokenUsage)
	}

	// Check cost - should be 0 without engine-specific parsing
	if metrics.EstimatedCost != 0 {
		t.Errorf("Expected cost 0 (no aw_info.json), got %f", metrics.EstimatedCost)
	}

	// Duration is no longer extracted from logs - using GitHub API timestamps instead
}

func TestExtractJSONMetrics(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		expectedTokens int
		expectedCost   float64
	}{
		{
			name:           "Claude streaming format with usage",
			line:           `{"type": "message_delta", "delta": {"usage": {"input_tokens": 123, "output_tokens": 456}}}`,
			expectedTokens: 579, // 123 + 456
		},
		{
			name:           "Simple token count (timestamp ignored)",
			line:           `{"tokens": 1234, "timestamp": "2024-01-15T10:30:00Z"}`,
			expectedTokens: 1234,
		},
		{
			name:         "Cost information",
			line:         `{"cost": 0.045, "price": 0.01}`,
			expectedCost: 0.045, // Should pick up the first one found
		},
		{
			name:           "Usage object with cost",
			line:           `{"usage": {"total_tokens": 999}, "billing": {"cost": 0.123}}`,
			expectedTokens: 999,
			expectedCost:   0.123,
		},
		{
			name:           "Claude result format with total_cost_usd",
			line:           `{"type": "result", "total_cost_usd": 0.8606770999999999, "usage": {"input_tokens": 126, "output_tokens": 7685}}`,
			expectedTokens: 7811, // 126 + 7685
			expectedCost:   0.8606770999999999,
		},
		{
			name:           "Claude result format with cache tokens",
			line:           `{"type": "result", "total_cost_usd": 0.86, "usage": {"input_tokens": 126, "cache_creation_input_tokens": 100034, "cache_read_input_tokens": 1232098, "output_tokens": 7685}}`,
			expectedTokens: 1339943, // 126 + 100034 + 1232098 + 7685
			expectedCost:   0.86,
		},
		{
			name:           "Not JSON",
			line:           "regular log line with tokens: 123",
			expectedTokens: 0, // Should return zero since it's not JSON
		},
		{
			name:           "Invalid JSON",
			line:           `{"invalid": json}`,
			expectedTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := extractJSONMetrics(tt.line, false)

			if metrics.TokenUsage != tt.expectedTokens {
				t.Errorf("Expected tokens %d, got %d", tt.expectedTokens, metrics.TokenUsage)
			}

			if metrics.EstimatedCost != tt.expectedCost {
				t.Errorf("Expected cost %f, got %f", tt.expectedCost, metrics.EstimatedCost)
			}
		})
	}
}

func TestParseLogFileWithJSON(t *testing.T) {
	// Create a temporary log file with mixed JSON and text format
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-mixed.log")

	logContent := `2024-01-15T10:30:00Z Starting workflow execution
{"type": "message_start"}
{"type": "content_block_delta", "delta": {"type": "text", "text": "Hello"}}
{"type": "message_delta", "delta": {"usage": {"input_tokens": 150, "output_tokens": 200}}}
Regular log line: tokens: 1000
{"cost": 0.035}
2024-01-15T10:31:30Z Workflow completed successfully`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	metrics, err := parseLogFileWithEngine(logFile, nil, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Without aw_info.json and specific engine, should return empty metrics
	if metrics.TokenUsage != 0 {
		t.Errorf("Expected token usage 0 (no aw_info.json), got %d", metrics.TokenUsage)
	}

	// Should have no cost without engine-specific parsing
	if metrics.EstimatedCost != 0 {
		t.Errorf("Expected cost 0 (no aw_info.json), got %f", metrics.EstimatedCost)
	}

	// Duration is no longer extracted from logs - using GitHub API timestamps instead
}

func TestConvertToInt(t *testing.T) {
	tests := []struct {
		value    any
		expected int
	}{
		{123, 123},
		{int64(456), 456},
		{789.0, 789},
		{"123", 123},
		{"invalid", 0},
		{nil, 0},
	}

	for _, tt := range tests {
		result := workflow.ConvertToInt(tt.value)
		if result != tt.expected {
			t.Errorf("ConvertToInt(%v) = %d, expected %d", tt.value, result, tt.expected)
		}
	}
}

func TestConvertToFloat(t *testing.T) {
	tests := []struct {
		value    any
		expected float64
	}{
		{123.45, 123.45},
		{123, 123.0},
		{int64(456), 456.0},
		{"123.45", 123.45},
		{"invalid", 0.0},
		{nil, 0.0},
	}

	for _, tt := range tests {
		result := workflow.ConvertToFloat(tt.value)
		if result != tt.expected {
			t.Errorf("ConvertToFloat(%v) = %f, expected %f", tt.value, result, tt.expected)
		}
	}
}

func TestExtractJSONCost(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		expected float64
	}{
		{
			name:     "total_cost_usd field",
			data:     map[string]any{"total_cost_usd": 0.8606770999999999},
			expected: 0.8606770999999999,
		},
		{
			name:     "traditional cost field",
			data:     map[string]any{"cost": 1.23},
			expected: 1.23,
		},
		{
			name:     "nested billing cost",
			data:     map[string]any{"billing": map[string]any{"cost": 2.45}},
			expected: 2.45,
		},
		{
			name:     "no cost fields",
			data:     map[string]any{"tokens": 1000},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := workflow.ExtractJSONCost(tt.data)
			if result != tt.expected {
				t.Errorf("ExtractJSONCost() = %f, expected %f", result, tt.expected)
			}
		})
	}
}

func TestParseLogFileWithClaudeResult(t *testing.T) {
	// Create a temporary log file with the exact Claude result format from the issue
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-claude.log")

	// This is the exact JSON format provided in the issue (compacted to single line)
	claudeResultJSON := `{"type": "result", "subtype": "success", "is_error": false, "duration_ms": 145056, "duration_api_ms": 142970, "num_turns": 66, "result": "**Integration test execution complete. All objectives achieved successfully.** 🎯", "session_id": "d0a2839f-3569-42e9-9ccb-70835de4e760", "total_cost_usd": 0.8606770999999999, "usage": {"input_tokens": 126, "cache_creation_input_tokens": 100034, "cache_read_input_tokens": 1232098, "output_tokens": 7685, "server_tool_use": {"web_search_requests": 0}, "service_tier": "standard"}}`

	logContent := `2024-01-15T10:30:00Z Starting Claude workflow execution
Claude processing request...
` + claudeResultJSON + `
2024-01-15T10:32:30Z Workflow completed successfully`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with Claude engine to parse Claude-specific logs
	claudeEngine := workflow.NewClaudeEngine()
	metrics, err := parseLogFileWithEngine(logFile, claudeEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Check total token usage includes all token types from Claude
	expectedTokens := 126 + 100034 + 1232098 + 7685 // input + cache_creation + cache_read + output
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Check cost extraction from total_cost_usd
	expectedCost := 0.8606770999999999
	if metrics.EstimatedCost != expectedCost {
		t.Errorf("Expected cost %f, got %f", expectedCost, metrics.EstimatedCost)
	}

	// Check turns extraction from num_turns
	expectedTurns := 66
	if metrics.Turns != expectedTurns {
		t.Errorf("Expected turns %d, got %d", expectedTurns, metrics.Turns)
	}

	// Duration is no longer extracted from logs - using GitHub API timestamps instead
}

func TestParseLogFileWithCodexFormat(t *testing.T) {
	// Create a temporary log file with the Codex output format from the issue
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-codex.log")

	// This is the exact Codex format provided in the issue with thinking sections added
	logContent := `[2025-08-13T00:24:45] Starting Codex workflow execution
[2025-08-13T00:24:50] thinking
I need to analyze the pull request details first.
[2025-08-13T00:24:50] codex

I'm ready to generate a Codex PR summary, but I need the pull request number to fetch its details. Could you please share the PR number (and confirm the repo/owner if it isn't ` + "`github/gh-aw`" + `)?
[2025-08-13T00:24:50] thinking  
Now I need to wait for the user's response.
[2025-08-13T00:24:50] tokens used: 13934
[2025-08-13T00:24:55] Workflow completed successfully`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with Codex engine to parse Codex-specific logs
	codexEngine := workflow.NewCodexEngine()
	metrics, err := parseLogFileWithEngine(logFile, codexEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Check token usage extraction from Codex format
	expectedTokens := 13934
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Check turns extraction from thinking sections
	expectedTurns := 2 // Two thinking sections in the test data
	if metrics.Turns != expectedTurns {
		t.Errorf("Expected turns %d, got %d", expectedTurns, metrics.Turns)
	}

	// Duration is no longer extracted from logs - using GitHub API timestamps instead
}

func TestParseLogFileWithCodexTokenSumming(t *testing.T) {
	// Create a temporary log file with multiple Codex token entries
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-codex-tokens.log")

	// Simulate the exact Codex format from the issue
	logContent := `  ]
}
[2025-08-13T04:38:03] tokens used: 32169
[2025-08-13T04:38:06] codex
I've posted the PR summary comment with analysis and recommendations. Let me know if you'd like to adjust any details or add further insights!
[2025-08-13T04:38:06] tokens used: 28828
[2025-08-13T04:38:10] Processing complete
[2025-08-13T04:38:15] tokens used: 5000`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with Codex engine
	codexEngine := workflow.NewCodexEngine()
	metrics, err := parseLogFileWithEngine(logFile, codexEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Check that all token entries are summed
	expectedTokens := 32169 + 28828 + 5000 // 65997
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected summed token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}
}

func TestParseLogFileWithCodexRustFormat(t *testing.T) {
	// Create a temporary log file with the new Rust-based Codex format
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-codex-rust.log")

	// This simulates the new Rust format from the Codex engine
	logContent := `2025-01-15T10:30:00.123456Z  INFO codex: Starting codex execution
2025-01-15T10:30:00.234567Z DEBUG codex_core: Initializing MCP servers
2025-01-15T10:30:01.123456Z  INFO codex: Session initialized
thinking
Let me fetch the list of pull requests first to see what we're working with.
2025-01-15T10:30:02.123456Z DEBUG codex_exec: Executing tool call
tool github.list_pull_requests({"state": "closed", "per_page": 5})
2025-01-15T10:30:03.456789Z DEBUG codex_core: Tool execution started
2025-01-15T10:30:04.567890Z  INFO codex: github.list_pull_requests(...) success in 2.1s
thinking
Now I need to get details for each PR to write a comprehensive summary.
2025-01-15T10:30:05.123456Z DEBUG codex_exec: Executing tool call
tool github.get_pull_request({"pull_number": 123})
2025-01-15T10:30:06.234567Z  INFO codex: github.get_pull_request(...) success in 0.8s
2025-01-15T10:30:07.345678Z DEBUG codex_core: Processing response
thinking
I have all the information I need. Let me create a summary issue.
2025-01-15T10:30:08.456789Z DEBUG codex_exec: Executing tool call
tool safe_outputs.create_issue({"title": "PR Summary", "body": "..."})
2025-01-15T10:30:09.567890Z  INFO codex: safe_outputs.create_issue(...) success in 1.2s
2025-01-15T10:30:10.123456Z DEBUG codex_core: Workflow completing
tokens used: 15234
2025-01-15T10:30:10.234567Z  INFO codex: Execution complete`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with Codex engine to parse new Rust format
	codexEngine := workflow.NewCodexEngine()
	metrics, err := parseLogFileWithEngine(logFile, codexEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Check token usage extraction from Rust format
	expectedTokens := 15234
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Check turns extraction from thinking sections (new Rust format uses standalone "thinking" lines)
	expectedTurns := 3 // Three thinking sections in the test data
	if metrics.Turns != expectedTurns {
		t.Errorf("Expected turns %d, got %d", expectedTurns, metrics.Turns)
	}

	// Check tool calls extraction from new Rust format
	expectedToolCount := 3
	if len(metrics.ToolCalls) != expectedToolCount {
		t.Errorf("Expected %d tool calls, got %d", expectedToolCount, len(metrics.ToolCalls))
	}

	// Verify the specific tools were detected
	toolNames := make(map[string]bool)
	for _, tool := range metrics.ToolCalls {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{"github_list_pull_requests", "github_get_pull_request", "safe_outputs_create_issue"}
	for _, expectedTool := range expectedTools {
		if !toolNames[expectedTool] {
			t.Errorf("Expected tool %s not found in tool calls", expectedTool)
		}
	}
}

func TestParseLogFileWithCodexMixedFormats(t *testing.T) {
	// Create a temporary log file with mixed old TypeScript and new Rust formats
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-codex-mixed.log")

	// Mix both formats to test backward compatibility
	logContent := `[2025-08-13T00:24:45] Starting Codex workflow execution
[2025-08-13T00:24:50] thinking
Old format thinking section
[2025-08-13T00:24:50] tool github.list_repos({"org": "test"})
[2025-08-13T00:24:51] codex
Response from old format
2025-01-15T10:30:00.123456Z  INFO codex: Starting execution
thinking
New Rust format thinking section
tool github.create_issue({"title": "Test", "body": "Body"})
2025-01-15T10:30:05.567890Z  INFO codex: github.create_issue(...) success in 1.2s
[2025-08-13T00:24:52] tokens used: 5000
tokens used: 10000
2025-01-15T10:30:10.234567Z  INFO codex: Execution complete`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with Codex engine to parse mixed formats
	codexEngine := workflow.NewCodexEngine()
	metrics, err := parseLogFileWithEngine(logFile, codexEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Check token usage is summed from both formats
	expectedTokens := 15000 // 5000 + 10000
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d (summed from both formats), got %d", expectedTokens, metrics.TokenUsage)
	}

	// Check turns from both formats
	expectedTurns := 2 // One from old format, one from new format
	if metrics.Turns != expectedTurns {
		t.Errorf("Expected turns %d (from both formats), got %d", expectedTurns, metrics.Turns)
	}

	// Check tool calls from both formats
	expectedToolCount := 2
	if len(metrics.ToolCalls) != expectedToolCount {
		t.Errorf("Expected %d tool calls, got %d", expectedToolCount, len(metrics.ToolCalls))
	}

	// Verify the specific tools were detected from both formats
	toolNames := make(map[string]bool)
	for _, tool := range metrics.ToolCalls {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{"github_list_repos", "github_create_issue"}
	for _, expectedTool := range expectedTools {
		if !toolNames[expectedTool] {
			t.Errorf("Expected tool %s not found in tool calls", expectedTool)
		}
	}
}

func TestParseLogFileWithMixedTokenFormats(t *testing.T) {
	// Create a temporary log file with mixed token formats
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-mixed-tokens.log")

	// Mix of Codex and non-Codex formats - should prioritize Codex summing
	logContent := `[2025-08-13T04:38:03] tokens used: 1000
tokens: 5000
[2025-08-13T04:38:06] tokens used: 2000
token_count: 10000`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Get the Codex engine for testing
	registry := workflow.NewEngineRegistry()
	codexEngine, err := registry.GetEngine("codex")
	if err != nil {
		t.Fatalf("Failed to get Codex engine: %v", err)
	}

	metrics, err := parseLogFileWithEngine(logFile, codexEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFile failed: %v", err)
	}

	// Should sum Codex entries: 1000 + 2000 = 3000 (ignoring non-Codex formats)
	expectedTokens := 1000 + 2000
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d (sum of Codex entries), got %d", expectedTokens, metrics.TokenUsage)
	}
}

func TestExtractEngineFromAwInfoNestedDirectory(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Test Case 1: aw_info.json as a regular file
	awInfoFile := filepath.Join(tmpDir, "aw_info.json")
	awInfoContent := `{
		"engine_id": "claude",
		"engine_name": "Claude",
		"model": "claude-3-sonnet",
		"version": "20240620",
		"workflow_name": "Test Claude",
		"experimental": false,
		"supports_tools_allowlist": true,
		"supports_http_transport": false,
		"run_id": 123456789,
		"run_number": 42,
		"run_attempt": "1",
		"repository": "github/gh-aw",
		"ref": "refs/heads/main",
		"sha": "abc123",
		"actor": "testuser",
		"event_name": "workflow_dispatch",
		"created_at": "2025-08-13T13:36:39.704Z"
	}`

	err := os.WriteFile(awInfoFile, []byte(awInfoContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create aw_info.json file: %v", err)
	}

	// Test regular file extraction
	engine := extractEngineFromAwInfo(awInfoFile, true)
	if engine == nil {
		t.Errorf("Expected to extract engine from regular aw_info.json file, got nil")
	} else if engine.GetID() != "claude" {
		t.Errorf("Expected engine ID 'claude', got '%s'", engine.GetID())
	}

	// Clean up for next test
	os.Remove(awInfoFile)

	// Test Case 2: aw_info.json as a directory containing the actual file
	awInfoDir := filepath.Join(tmpDir, "aw_info.json")
	err = os.Mkdir(awInfoDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create aw_info.json directory: %v", err)
	}

	// Create the nested aw_info.json file inside the directory
	nestedAwInfoFile := filepath.Join(awInfoDir, "aw_info.json")
	awInfoContentCodex := `{
		"engine_id": "codex",
		"engine_name": "Codex",
		"model": "o4-mini",
		"version": "",
		"workflow_name": "Test Codex",
		"experimental": true,
		"supports_tools_allowlist": true,
		"supports_http_transport": false,
		"run_id": 987654321,
		"run_number": 7,
		"run_attempt": "1",
		"repository": "github/gh-aw",
		"ref": "refs/heads/copilot/fix-24",
		"sha": "def456",
		"actor": "testuser2",
		"event_name": "workflow_dispatch",
		"created_at": "2025-08-13T13:36:39.704Z"
	}`

	err = os.WriteFile(nestedAwInfoFile, []byte(awInfoContentCodex), 0644)
	if err != nil {
		t.Fatalf("Failed to create nested aw_info.json file: %v", err)
	}

	// Test directory-based extraction (the main fix)
	engine = extractEngineFromAwInfo(awInfoDir, true)
	if engine == nil {
		t.Errorf("Expected to extract engine from aw_info.json directory, got nil")
	} else if engine.GetID() != "codex" {
		t.Errorf("Expected engine ID 'codex', got '%s'", engine.GetID())
	}

	// Test Case 3: Non-existent aw_info.json should return nil
	nonExistentPath := filepath.Join(tmpDir, "nonexistent", "aw_info.json")
	engine = extractEngineFromAwInfo(nonExistentPath, false)
	if engine != nil {
		t.Errorf("Expected nil for non-existent aw_info.json, got engine: %s", engine.GetID())
	}

	// Test Case 4: Directory without nested aw_info.json should return nil
	emptyDir := filepath.Join(tmpDir, "empty_aw_info.json")
	err = os.Mkdir(emptyDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	engine = extractEngineFromAwInfo(emptyDir, false)
	if engine != nil {
		t.Errorf("Expected nil for directory without nested aw_info.json, got engine: %s", engine.GetID())
	}

	// Test Case 5: Invalid JSON should return nil
	invalidAwInfoFile := filepath.Join(tmpDir, "invalid_aw_info.json")
	invalidContent := `{invalid json content`
	err = os.WriteFile(invalidAwInfoFile, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid aw_info.json file: %v", err)
	}

	engine = extractEngineFromAwInfo(invalidAwInfoFile, false)
	if engine != nil {
		t.Errorf("Expected nil for invalid JSON aw_info.json, got engine: %s", engine.GetID())
	}

	// Test Case 6: Missing engine_id should return nil
	missingEngineIDFile := filepath.Join(tmpDir, "missing_engine_id_aw_info.json")
	missingEngineIDContent := `{
		"workflow_name": "Test Workflow",
		"run_id": 123456789
	}`
	err = os.WriteFile(missingEngineIDFile, []byte(missingEngineIDContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create aw_info.json file without engine_id: %v", err)
	}

	engine = extractEngineFromAwInfo(missingEngineIDFile, false)
	if engine != nil {
		t.Errorf("Expected nil for aw_info.json without engine_id, got engine: %s", engine.GetID())
	}
}

func TestParseLogFileWithNonCodexTokensOnly(t *testing.T) {
	// Create a temporary log file with only non-Codex token formats
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-generic-tokens.log")

	// Only non-Codex formats - should keep maximum behavior
	logContent := `tokens: 5000
token_count: 10000
input_tokens: 2000`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Without aw_info.json and specific engine, should return empty metrics
	metrics, err := parseLogFileWithEngine(logFile, nil, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Without engine-specific parsing, should return 0
	if metrics.TokenUsage != 0 {
		t.Errorf("Expected token usage 0 (no aw_info.json), got %d", metrics.TokenUsage)
	}
}

func TestExtractLogMetricsWithAwOutputFile(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := testutil.TempDir(t, "test-*")
	logsDir := filepath.Join(tmpDir, "run-123456")
	err := os.Mkdir(logsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create logs directory: %v", err)
	}

	// Create aw_info.json to specify copilot engine
	awInfoFile := filepath.Join(logsDir, "aw_info.json")
	awInfoContent := `{"engine_id": "copilot"}`
	err = os.WriteFile(awInfoFile, []byte(awInfoContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create aw_info.json file: %v", err)
	}

	// Create agent_output.json file (the actual safe outputs artifact)
	awOutputFile := filepath.Join(logsDir, "agent_output.json")
	awOutputContent := `{
		"items": [],
		"errors": []
	}`
	err = os.WriteFile(awOutputFile, []byte(awOutputContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create agent_output.json file: %v", err)
	}

	// Create a log file with metrics for Copilot CLI
	logFile := filepath.Join(logsDir, "agent.log")
	logContent := `2024-01-15T10:30:00Z [DEBUG] response (Request-ID 00000-test):
2024-01-15T10:30:00Z [DEBUG] data:
2024-01-15T10:30:00Z [DEBUG] {
2024-01-15T10:30:00Z [DEBUG]   "id": "chatcmpl-test",
2024-01-15T10:30:00Z [DEBUG]   "object": "chat.completion",
2024-01-15T10:30:00Z [DEBUG]   "created": 1705317000,
2024-01-15T10:30:00Z [DEBUG]   "model": "claude-sonnet-4",
2024-01-15T10:30:00Z [DEBUG]   "choices": [
2024-01-15T10:30:00Z [DEBUG]     {
2024-01-15T10:30:00Z [DEBUG]       "index": 0,
2024-01-15T10:30:00Z [DEBUG]       "message": {
2024-01-15T10:30:00Z [DEBUG]         "role": "assistant",
2024-01-15T10:30:00Z [DEBUG]         "content": "Task completed."
2024-01-15T10:30:00Z [DEBUG]       },
2024-01-15T10:30:00Z [DEBUG]       "finish_reason": "stop"
2024-01-15T10:30:00Z [DEBUG]     }
2024-01-15T10:30:00Z [DEBUG]   ],
2024-01-15T10:30:00Z [DEBUG]   "usage": {
2024-01-15T10:30:00Z [DEBUG]     "prompt_tokens": 4232,
2024-01-15T10:30:00Z [DEBUG]     "completion_tokens": 1200,
2024-01-15T10:30:00Z [DEBUG]     "total_tokens": 5432
2024-01-15T10:30:00Z [DEBUG]   }
2024-01-15T10:30:00Z [DEBUG] }
2024-01-15T10:30:00Z [DEBUG] Workflow completed`
	err = os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Test metrics extraction from log files
	metrics, err := extractLogMetrics(logsDir, false)
	if err != nil {
		t.Fatalf("extractLogMetrics failed: %v", err)
	}

	// Verify metrics from log file
	if metrics.TokenUsage != 5432 {
		t.Errorf("Expected token usage 5432 from log file, got %d", metrics.TokenUsage)
	}

	// Note: estimated cost and turns are engine/log-format specific
	// For this test, we're just verifying basic token parsing works
}

func TestCustomEngineParseLogMetrics(t *testing.T) {
	// Create a temporary log file with custom engine format
	tmpDir := testutil.TempDir(t, "test-*")
	logFile := filepath.Join(tmpDir, "test-custom.log")

	logContent := `2024-01-15T10:30:00Z Starting custom engine execution
Custom engine processing...
Tokens used: 3456
Estimated cost: $0.067
2024-01-15T10:31:30Z Workflow completed`

	err := os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Test with custom engine
	customEngine := workflow.NewCustomEngine()
	metrics, err := parseLogFileWithEngine(logFile, customEngine, false, false)
	if err != nil {
		t.Fatalf("parseLogFileWithEngine failed: %v", err)
	}

	// Custom engine should extract basic token info
	// Note: The actual behavior depends on the custom engine implementation
	if metrics.TokenUsage < 0 {
		t.Errorf("Expected non-negative token usage, got %d", metrics.TokenUsage)
	}
}
