//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexParseLogMetricsWithOutputSize(t *testing.T) {
	engine := NewCodexEngine()

	// Sample log with tool call and result
	logContent := `[2025-08-31T12:37:33] tool time.get_current_time({"timezone":"UTC"})
[2025-08-31T12:37:33] time.get_current_time({"timezone":"UTC"}) success in 2ms:
{
  "content": [
    {
      "text": "{\"timezone\":\"UTC\",\"datetime\":\"2025-08-31T12:37:33+00:00\",\"is_dst\":false}",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:37:33] tokens used: 1000`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify tool calls were tracked
	require.Len(t, metrics.ToolCalls, 1, "Expected 1 tool call")

	toolCall := metrics.ToolCalls[0]
	assert.Equal(t, "time_get_current_time", toolCall.Name)
	assert.Equal(t, 1, toolCall.CallCount)

	// Verify output size was extracted (should be length of text content)
	// The text content is: {"timezone":"UTC","datetime":"2025-08-31T12:37:33+00:00","is_dst":false}
	expectedSize := len(`{"timezone":"UTC","datetime":"2025-08-31T12:37:33+00:00","is_dst":false}`)
	assert.Positive(t, toolCall.MaxOutputSize, "MaxOutputSize should be greater than 0")
	assert.Equal(t, expectedSize, toolCall.MaxOutputSize, "MaxOutputSize should match text content length")
}

func TestCodexParseLogMetricsMultipleToolsWithOutputSizes(t *testing.T) {
	engine := NewCodexEngine()

	// Sample log with multiple tool calls and results
	logContent := `[2025-08-31T12:37:47] thinking
[2025-08-31T12:37:49] tool github.list_pull_requests({"owner":"githubnext","repo":"gh-aw","state":"open","perPage":100})
[2025-08-31T12:37:50] github.list_pull_requests({"owner":"githubnext","repo":"gh-aw","state":"open","perPage":100}) success in 175ms:
{
  "content": [
    {
      "text": "[]",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:38:18] tool github.search_pull_requests({"query":"is:pr repo:github/gh-aw codex","perPage":10})
[2025-08-31T12:38:20] github.search_pull_requests({"query":"is:pr repo:github/gh-aw codex","perPage":10}) success in 331ms:
{
  "content": [
    {
      "text": "[{\"number\":123,\"title\":\"Test PR\"}]",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:38:20] tokens used: 5000`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify tool calls were tracked
	require.Len(t, metrics.ToolCalls, 2, "Expected 2 tool calls")

	// Find tools by name
	var listPRTool, searchPRTool *ToolCallInfo
	for i := range metrics.ToolCalls {
		switch metrics.ToolCalls[i].Name {
		case "github_list_pull_requests":
			listPRTool = &metrics.ToolCalls[i]
		case "github_search_pull_requests":
			searchPRTool = &metrics.ToolCalls[i]
		}
	}

	require.NotNil(t, listPRTool, "list_pull_requests tool should be found")
	require.NotNil(t, searchPRTool, "search_pull_requests tool should be found")

	// Verify output sizes
	assert.Equal(t, len("[]"), listPRTool.MaxOutputSize, "list_pull_requests output size")
	assert.Equal(t, len("[{\"number\":123,\"title\":\"Test PR\"}]"), searchPRTool.MaxOutputSize, "search_pull_requests output size")
}

func TestCodexParseLogMetricsNoOutputSize(t *testing.T) {
	engine := NewCodexEngine()

	// Log with tool call but no result (e.g., still running or failed without output)
	logContent := `[2025-08-31T12:37:33] tool time.get_current_time({"timezone":"UTC"})
[2025-08-31T12:37:33] tokens used: 1000`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify tool call was tracked
	require.Len(t, metrics.ToolCalls, 1, "Expected 1 tool call")

	toolCall := metrics.ToolCalls[0]
	assert.Equal(t, "time_get_current_time", toolCall.Name)
	assert.Equal(t, 0, toolCall.MaxOutputSize, "MaxOutputSize should be 0 when no result is present")
}

func TestCodexParseLogMetricsWithFailure(t *testing.T) {
	engine := NewCodexEngine()

	// Log with tool call that fails with output
	logContent := `[2025-08-31T12:37:33] tool api.call_endpoint({"url":"https://example.com"})
[2025-08-31T12:37:34] api.call_endpoint({"url":"https://example.com"}) failure in 100ms:
{
  "content": [
    {
      "text": "Error: Connection timeout",
      "type": "text"
    }
  ],
  "isError": true
}
[2025-08-31T12:37:34] tokens used: 500`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify tool call was tracked
	require.Len(t, metrics.ToolCalls, 1, "Expected 1 tool call")

	toolCall := metrics.ToolCalls[0]
	assert.Equal(t, "api_call_endpoint", toolCall.Name)

	// Verify output size was extracted even from failure
	expectedSize := len("Error: Connection timeout")
	assert.Equal(t, expectedSize, toolCall.MaxOutputSize, "MaxOutputSize should be extracted from failure result")
}

func TestCodexExtractOutputSizeFromJSON(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name         string
		jsonStr      string
		expectedSize int
	}{
		{
			name: "simple text content",
			jsonStr: `{
  "content": [
    {
      "text": "Hello, World!",
      "type": "text"
    }
  ],
  "isError": false
}`,
			expectedSize: len("Hello, World!"),
		},
		{
			name: "multiple content items",
			jsonStr: `{
  "content": [
    {
      "text": "First item",
      "type": "text"
    },
    {
      "text": "Second item",
      "type": "text"
    }
  ],
  "isError": false
}`,
			expectedSize: len("First item") + len("Second item"),
		},
		{
			name: "escaped characters in text",
			jsonStr: `{
  "content": [
    {
      "text": "{\"key\":\"value\"}",
      "type": "text"
    }
  ],
  "isError": false
}`,
			expectedSize: len(`{"key":"value"}`),
		},
		{
			name:         "empty content array",
			jsonStr:      `{"content": [], "isError": false}`,
			expectedSize: 0,
		},
		{
			name:         "malformed JSON",
			jsonStr:      `{"content": [{"text": "incomplete`,
			expectedSize: 0, // Fallback should handle this gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.extractOutputSizeFromJSON(tt.jsonStr)
			assert.Equal(t, tt.expectedSize, result, "Output size should match expected")
		})
	}
}

func TestCodexExtractOutputSizeFromResult(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name         string
		lines        []string
		lineIndex    int
		expectedSize int
	}{
		{
			name: "success with JSON block",
			lines: []string{
				"[2025-08-31T12:37:33] tool.method() success in 2ms:",
				"{",
				"  \"content\": [",
				"    {",
				"      \"text\": \"Result data\",",
				"      \"type\": \"text\"",
				"    }",
				"  ],",
				"  \"isError\": false",
				"}",
			},
			lineIndex:    0,
			expectedSize: len("Result data"),
		},
		{
			name: "failure with JSON block",
			lines: []string{
				"[2025-08-31T12:37:33] tool.method() failed in 2ms:",
				"{",
				"  \"content\": [",
				"    {",
				"      \"text\": \"Error message\",",
				"      \"type\": \"text\"",
				"    }",
				"  ],",
				"  \"isError\": true",
				"}",
			},
			lineIndex:    0,
			expectedSize: len("Error message"),
		},
		{
			name: "no JSON following result line",
			lines: []string{
				"[2025-08-31T12:37:33] tool.method() success in 2ms:",
				"[2025-08-31T12:37:34] next log line",
			},
			lineIndex:    0,
			expectedSize: 0,
		},
		{
			name: "not a result line",
			lines: []string{
				"[2025-08-31T12:37:33] tool.method()",
				"{",
				"  \"content\": []",
				"}",
			},
			lineIndex:    0,
			expectedSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.extractOutputSizeFromResult(tt.lines[tt.lineIndex], tt.lines, tt.lineIndex)
			assert.Equal(t, tt.expectedSize, result, "Output size should match expected")
		})
	}
}

func TestCodexParseLogMetricsMaxOutputSize(t *testing.T) {
	engine := NewCodexEngine()

	// Log with same tool called multiple times with different output sizes
	logContent := `[2025-08-31T12:37:33] tool api.fetch({"id":"1"})
[2025-08-31T12:37:33] api.fetch({"id":"1"}) success in 2ms:
{
  "content": [
    {
      "text": "short",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:37:34] tool api.fetch({"id":"2"})
[2025-08-31T12:37:34] api.fetch({"id":"2"}) success in 3ms:
{
  "content": [
    {
      "text": "this is a much longer response with more data",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:37:35] tool api.fetch({"id":"3"})
[2025-08-31T12:37:35] api.fetch({"id":"3"}) success in 2ms:
{
  "content": [
    {
      "text": "medium",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:37:35] tokens used: 2000`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify tool call was tracked with correct count
	require.Len(t, metrics.ToolCalls, 1, "Expected 1 unique tool")

	toolCall := metrics.ToolCalls[0]
	assert.Equal(t, "api_fetch", toolCall.Name)
	assert.Equal(t, 3, toolCall.CallCount, "Tool should be called 3 times")

	// Verify MaxOutputSize is the largest of all calls
	expectedMaxSize := len("this is a much longer response with more data")
	assert.Equal(t, expectedMaxSize, toolCall.MaxOutputSize, "MaxOutputSize should be the largest output")
}

func TestCodexParseLogMetricsBashCommand(t *testing.T) {
	engine := NewCodexEngine()

	// Log with bash command execution
	logContent := `[2025-08-31T12:37:33] exec ls -la in /tmp
[2025-08-31T12:37:33] ls -la success in 10ms:
{
  "content": [
    {
      "text": "total 8\ndrwxr-xr-x  2 user group 4096 Aug 31 12:37 .\ndrwxr-xr-x 20 user group 4096 Aug 31 12:30 ..",
      "type": "text"
    }
  ],
  "isError": false
}
[2025-08-31T12:37:33] tokens used: 500`

	metrics := engine.ParseLogMetrics(logContent, true)

	// Verify bash command was tracked
	require.Len(t, metrics.ToolCalls, 1, "Expected 1 tool call for bash")

	toolCall := metrics.ToolCalls[0]
	assert.True(t, strings.HasPrefix(toolCall.Name, "bash_"), "Tool name should start with bash_")

	// Verify output size was extracted
	expectedSize := len("total 8\ndrwxr-xr-x  2 user group 4096 Aug 31 12:37 .\ndrwxr-xr-x 20 user group 4096 Aug 31 12:30 ..")
	assert.Equal(t, expectedSize, toolCall.MaxOutputSize, "MaxOutputSize should match bash output")
}

func TestCodexExtractOutputSizeFromJSONFallback(t *testing.T) {
	engine := NewCodexEngine()

	// Test fallback extraction with slightly malformed JSON that can still be parsed
	jsonStr := `{"content": [{"text": "fallback test", "type": "text"}]`

	// First try normal extraction (should fail and trigger fallback)
	result := engine.extractOutputSizeFromJSON(jsonStr)

	// Fallback should still extract the text
	assert.Positive(t, result, "Fallback should extract some text")
}
