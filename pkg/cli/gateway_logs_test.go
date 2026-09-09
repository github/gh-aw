//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGatewayLogs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		logContent    string
		wantServers   int
		wantRequests  int
		wantToolCalls int
		wantErrors    int
		wantErr       bool
	}{
		{
			name: "valid gateway log with tool calls",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","method":"get_repository","duration":150.5,"input_size":100,"output_size":500,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"list_issues","method":"list_issues","duration":250.3,"input_size":50,"output_size":1000,"status":"success"}
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"tool_call","server_name":"playwright","tool_name":"navigate","method":"navigate","duration":500.0,"input_size":200,"output_size":300,"status":"success"}
`,
			wantServers:   2,
			wantRequests:  3,
			wantToolCalls: 3,
			wantErrors:    0,
			wantErr:       false,
		},
		{
			name: "gateway log with errors",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"error","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":50.0,"status":"error","error":"connection timeout"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":100.0,"status":"success"}
`,
			wantServers:   1,
			wantRequests:  2,
			wantToolCalls: 2,
			wantErrors:    1,
			wantErr:       false,
		},
		{
			name: "gateway log with multiple servers",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"rpc_call","server_name":"github","method":"list_repos","duration":100.0,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"rpc_call","server_name":"playwright","method":"screenshot","duration":200.0,"status":"success"}
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"rpc_call","server_name":"tavily","method":"search","duration":300.0,"status":"success"}
`,
			wantServers:   3,
			wantRequests:  3,
			wantToolCalls: 0,
			wantErrors:    0,
			wantErr:       false,
		},
		{
			name:         "empty log file",
			logContent:   "",
			wantServers:  0,
			wantRequests: 0,
			wantErrors:   0,
			wantErr:      false,
		},
		{
			name: "log with invalid JSON line",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":150.5,"status":"success"}
invalid json line
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":250.3,"status":"success"}
`,
			wantServers:   1,
			wantRequests:  2,
			wantToolCalls: 2,
			wantErrors:    0,
			wantErr:       false, // Should continue parsing after invalid line
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a temporary directory
			tmpDir := t.TempDir()

			// Write the test log content
			gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
			err := os.WriteFile(gatewayLogPath, []byte(tt.logContent), 0644)
			require.NoError(t, err, "Failed to write test gateway.jsonl")

			// Parse the gateway logs
			metrics, err := parseGatewayLogs(tmpDir, false)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, metrics)

			// Verify metrics
			assert.Len(t, metrics.Servers, tt.wantServers, "Server count mismatch")
			assert.Equal(t, tt.wantRequests, metrics.TotalRequests, "Total requests mismatch")
			assert.Equal(t, tt.wantToolCalls, metrics.TotalToolCalls, "Total tool calls mismatch")
			assert.Equal(t, tt.wantErrors, metrics.TotalErrors, "Total errors mismatch")
		})
	}
}

func TestParseGatewayLogsFileNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	metrics, err := parseGatewayLogs(tmpDir, false)

	require.Error(t, err)
	assert.Nil(t, metrics)
	require.ErrorContains(t, err, "gateway.jsonl not found")
}

func TestGatewayToolMetrics(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a log with multiple calls to the same tool
	logContent := `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":100.0,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":200.0,"status":"success"}
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":300.0,"status":"success"}
`

	gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
	err := os.WriteFile(gatewayLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	metrics, err := parseGatewayLogs(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify server metrics
	require.Len(t, metrics.Servers, 1)
	server := metrics.Servers["github"]
	require.NotNil(t, server)
	assert.Equal(t, "github", server.ServerName)
	assert.Equal(t, 3, server.RequestCount)

	// Verify tool metrics
	require.Len(t, server.Tools, 1)
	tool := server.Tools["get_repository"]
	require.NotNil(t, tool)
	assert.Equal(t, "get_repository", tool.ToolName)
	assert.Equal(t, 3, tool.CallCount)
	assert.InDelta(t, 600.0, tool.TotalDuration, 0.001)
	assert.InDelta(t, 200.0, tool.AvgDuration, 0.001)
	assert.InDelta(t, 300.0, tool.MaxDuration, 0.001)
	assert.InDelta(t, 100.0, tool.MinDuration, 0.001)
}

func TestRenderGatewayMetricsTable(t *testing.T) {
	t.Parallel()
	// Create metrics with some data
	metrics := &GatewayMetrics{
		TotalRequests:  10,
		TotalToolCalls: 8,
		TotalErrors:    2,
		Servers: map[string]*GatewayServerMetrics{
			"github": {
				ServerName:    "github",
				RequestCount:  6,
				ToolCallCount: 5,
				TotalDuration: 600.0,
				ErrorCount:    1,
				Tools: map[string]*GatewayToolMetrics{
					"get_repository": {
						ToolName:      "get_repository",
						CallCount:     3,
						TotalDuration: 300.0,
						AvgDuration:   100.0,
						MaxDuration:   150.0,
						MinDuration:   50.0,
						ErrorCount:    0,
					},
				},
			},
			"playwright": {
				ServerName:    "playwright",
				RequestCount:  4,
				ToolCallCount: 3,
				TotalDuration: 400.0,
				ErrorCount:    1,
				Tools: map[string]*GatewayToolMetrics{
					"navigate": {
						ToolName:      "navigate",
						CallCount:     2,
						TotalDuration: 200.0,
						AvgDuration:   100.0,
						MaxDuration:   120.0,
						MinDuration:   80.0,
						ErrorCount:    0,
					},
				},
			},
		},
	}

	// Test non-verbose output
	output := renderGatewayMetricsTable(metrics, false)
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "MCP Gateway Metrics")
	assert.Contains(t, output, "Total Requests: 10")
	assert.Contains(t, output, "Total Tool Calls: 8")
	assert.Contains(t, output, "Total Errors: 2")
	assert.Contains(t, output, "Servers: 2")
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "playwright")

	// Test verbose output
	verboseOutput := renderGatewayMetricsTable(metrics, true)
	assert.NotEmpty(t, verboseOutput)
	assert.Contains(t, verboseOutput, "Tool Usage Details")
	assert.Contains(t, verboseOutput, "get_repository")
	assert.Contains(t, verboseOutput, "navigate")
}

func TestRenderGatewayMetricsTableEmpty(t *testing.T) {
	t.Parallel()
	// Test with nil metrics
	output := renderGatewayMetricsTable(nil, false)
	assert.Empty(t, output)

	// Test with empty metrics
	emptyMetrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}
	output = renderGatewayMetricsTable(emptyMetrics, false)
	assert.Empty(t, output)
}

func TestGatewayTruncateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "string shorter than max",
			input:  "short",
			maxLen: 10,
			want:   "short",
		},
		{
			name:   "string equal to max",
			input:  "exactlyten",
			maxLen: 10,
			want:   "exactlyten",
		},
		{
			name:   "string longer than max",
			input:  "this is a very long string",
			maxLen: 10,
			want:   "this is...",
		},
		{
			name:   "max length very small",
			input:  "test",
			maxLen: 2,
			want:   "te",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := stringutil.Truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, result)
			assert.LessOrEqual(t, len(result), tt.maxLen)
		})
	}
}

func TestProcessGatewayLogEntry(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	// Test request entry
	entry := &GatewayLogEntry{
		Timestamp:  "2024-01-12T10:00:00Z",
		Event:      "tool_call",
		ServerName: "github",
		ToolName:   "get_repository",
		Duration:   150.5,
		InputSize:  100,
		OutputSize: 500,
		Status:     "success",
	}

	processGatewayLogEntry(entry, metrics, false)

	assert.Equal(t, 1, metrics.TotalRequests)
	assert.Equal(t, 1, metrics.TotalToolCalls)
	assert.Equal(t, 0, metrics.TotalErrors)
	assert.Len(t, metrics.Servers, 1)

	server := metrics.Servers["github"]
	require.NotNil(t, server)
	assert.Equal(t, 1, server.RequestCount)
	assert.Equal(t, 1, server.ToolCallCount)
	assert.InDelta(t, 150.5, server.TotalDuration, 0.001)

	// Test error entry
	errorEntry := &GatewayLogEntry{
		Timestamp:  "2024-01-12T10:00:01Z",
		Event:      "tool_call",
		ServerName: "github",
		ToolName:   "list_issues",
		Status:     "error",
		Error:      "connection timeout",
	}

	processGatewayLogEntry(errorEntry, metrics, false)

	assert.Equal(t, 2, metrics.TotalRequests)
	assert.Equal(t, 1, metrics.TotalErrors)
	assert.Equal(t, 1, server.ErrorCount)
}

func TestProcessGatewayLogEntryDifcFiltered(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	entry := &GatewayLogEntry{
		Timestamp:         "2024-01-12T10:00:00Z",
		Type:              "DIFC_FILTERED",
		ServerID:          "github",
		ToolName:          "pull_request_read",
		Reason:            "Resource has lower integrity than agent requires.",
		AuthorLogin:       "octocat",
		AuthorAssociation: "CONTRIBUTOR",
		HTMLURL:           "https://github.com/github/gh-aw/pull/42",
		Number:            "42",
	}

	processGatewayLogEntry(entry, metrics, false)

	assert.Equal(t, 0, metrics.TotalRequests, "DIFC_FILTERED should not increment TotalRequests")
	assert.Equal(t, 1, metrics.TotalFiltered, "DIFC_FILTERED should increment TotalFiltered")
	require.Len(t, metrics.FilteredEvents, 1, "should record one filtered event")

	evt := metrics.FilteredEvents[0]
	assert.Equal(t, "github", evt.ServerID)
	assert.Equal(t, "pull_request_read", evt.ToolName)
	assert.Equal(t, "Resource has lower integrity than agent requires.", evt.Reason)
	assert.Equal(t, "octocat", evt.AuthorLogin)
	assert.Equal(t, "CONTRIBUTOR", evt.AuthorAssociation)
	assert.Equal(t, "https://github.com/github/gh-aw/pull/42", evt.HTMLURL)
	assert.Equal(t, "42", evt.Number)

	require.Len(t, metrics.Servers, 1, "should create server entry for DIFC_FILTERED server")
	githubServer := metrics.Servers["github"]
	require.NotNil(t, githubServer)
	assert.Equal(t, 1, githubServer.FilteredCount)
}

func TestParseRPCMessagesDifcFiltered(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	content := `{"timestamp":"2024-01-12T10:00:00.000000000Z","type":"DIFC_FILTERED","server_id":"github","tool_name":"pull_request_read","reason":"Resource has lower integrity than agent requires.","author_login":"octocat","author_association":"CONTRIBUTOR","html_url":"https://github.com/github/gh-aw/pull/42","number":"42"}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.200000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
{"timestamp":"2024-01-12T10:00:02.000000000Z","type":"DIFC_FILTERED","server_id":"github","tool_name":"issue_read","reason":"Secrecy violation.","secrecy_tags":["private"]}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0644))

	metrics, err := parseRPCMessages(logPath, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, 2, metrics.TotalFiltered, "should count 2 DIFC_FILTERED events")
	assert.Equal(t, 1, metrics.TotalRequests, "should count 1 REQUEST")
	require.Len(t, metrics.FilteredEvents, 2)

	// First filtered event — with user and resource metadata
	first := metrics.FilteredEvents[0]
	assert.Equal(t, "github", first.ServerID)
	assert.Equal(t, "pull_request_read", first.ToolName)
	assert.Equal(t, "Resource has lower integrity than agent requires.", first.Reason)
	assert.Equal(t, "octocat", first.AuthorLogin)
	assert.Equal(t, "CONTRIBUTOR", first.AuthorAssociation)
	assert.Equal(t, "https://github.com/github/gh-aw/pull/42", first.HTMLURL)
	assert.Equal(t, "42", first.Number)

	// Second filtered event — with secrecy tags
	second := metrics.FilteredEvents[1]
	assert.Equal(t, "github", second.ServerID)
	assert.Equal(t, "issue_read", second.ToolName)
	assert.Equal(t, "Secrecy violation.", second.Reason)
	assert.Equal(t, []string{"private"}, second.SecrecyTags)

	// Server should have FilteredCount = 2
	githubServer := metrics.Servers["github"]
	require.NotNil(t, githubServer)
	assert.Equal(t, 2, githubServer.FilteredCount)
}

// TestParseRPCMessagesSchemaV2Format verifies that parseRPCMessages correctly parses
// real-world rpc-messages.jsonl entries that use the schema "rpc-message/v2" format:
// a top-level "event" field ("rpc_request"/"rpc_response") and "_schema" marker instead
// of the legacy top-level "type" field ("REQUEST"/"RESPONSE"). Regression test for
// https://github.com/github/gh-aw/issues/53254.
func TestParseRPCMessagesSchemaV2Format(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	content := `{"timestamp":"2026-08-15T23:48:42.233Z","event":"rpc_request","_schema":"rpc-message/v2","direction":"OUT","server_id":"github","method":"tools/call","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2026-08-15T23:48:42.259Z","event":"rpc_response","_schema":"rpc-message/v2","direction":"IN","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0644))

	metrics, err := parseRPCMessages(logPath, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, 1, metrics.TotalRequests, "should count the REQUEST despite missing top-level 'type'")
	assert.Equal(t, 1, metrics.TotalToolCalls, "should count the tools/call request as a tool call")
	require.Len(t, metrics.Servers, 1)

	server := metrics.Servers["github"]
	require.NotNil(t, server)
	assert.Equal(t, 1, server.RequestCount)
	assert.Equal(t, 1, server.ToolCallCount)
}

// TestRPCMessageEntryEffectiveType verifies the normalization helper used to bridge
// the legacy top-level "type" field and the "event" field used by schema "rpc-message/v2".
func TestRPCMessageEntryEffectiveType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry RPCMessageEntry
		want  string
	}{
		{"legacy type takes precedence", RPCMessageEntry{Type: "REQUEST", Event: "rpc_response"}, "REQUEST"},
		{"rpc_request maps to REQUEST", RPCMessageEntry{Event: "rpc_request"}, "REQUEST"},
		{"rpc_response maps to RESPONSE", RPCMessageEntry{Event: "rpc_response"}, "RESPONSE"},
		{"difc_filtered maps to DIFC_FILTERED", RPCMessageEntry{Event: "difc_filtered"}, "DIFC_FILTERED"},
		{"unknown event passes through", RPCMessageEntry{Event: "something_else"}, "something_else"},
		{"empty entry returns empty string", RPCMessageEntry{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.entry.EffectiveType())
		})
	}
}

func TestGetSortedServerNames(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		Servers: map[string]*GatewayServerMetrics{
			"github": {
				ServerName:   "github",
				RequestCount: 10,
			},
			"playwright": {
				ServerName:   "playwright",
				RequestCount: 5,
			},
			"tavily": {
				ServerName:   "tavily",
				RequestCount: 15,
			},
		},
	}

	names := getSortedServerNames(metrics)
	require.Len(t, names, 3)

	// Should be sorted by request count (descending)
	assert.Equal(t, "tavily", names[0])
	assert.Equal(t, "github", names[1])
	assert.Equal(t, "playwright", names[2])
}

func TestGatewayLogsWithMethodField(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Test with method field instead of tool_name
	logContent := `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"rpc_call","server_name":"github","method":"tools/list","duration":100.0,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"rpc_call","server_name":"github","method":"tools/call","duration":200.0,"status":"success"}
`

	gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
	err := os.WriteFile(gatewayLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	metrics, err := parseGatewayLogs(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Len(t, metrics.Servers, 1)
	assert.Equal(t, 2, metrics.TotalRequests)
	assert.Equal(t, 1, metrics.TotalToolCalls)

	server := metrics.Servers["github"]
	require.NotNil(t, server)
	assert.Len(t, server.Tools, 1)

	// Protocol discovery remains a request but is not counted as tool usage.
	assert.NotContains(t, server.Tools, "tools/list")
	assert.Contains(t, server.Tools, "tools/call")
}

func TestGatewayLogsParsingIntegration(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a comprehensive test log
	logContent := `{"timestamp":"2024-01-12T10:00:00.000Z","level":"info","type":"gateway","event":"startup","message":"Gateway started"}
{"timestamp":"2024-01-12T10:00:01.123Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","method":"get_repository","duration":150.5,"input_size":100,"output_size":500,"status":"success"}
{"timestamp":"2024-01-12T10:00:02.456Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"list_issues","method":"list_issues","duration":250.3,"input_size":50,"output_size":1000,"status":"success"}
{"timestamp":"2024-01-12T10:00:03.789Z","level":"error","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","duration":50.0,"status":"error","error":"rate limit exceeded"}
{"timestamp":"2024-01-12T10:00:04.012Z","level":"info","type":"request","event":"tool_call","server_name":"playwright","tool_name":"navigate","method":"navigate","duration":500.0,"input_size":200,"output_size":300,"status":"success"}
{"timestamp":"2024-01-12T10:00:05.345Z","level":"info","type":"request","event":"tool_call","server_name":"playwright","tool_name":"screenshot","method":"screenshot","duration":300.0,"input_size":50,"output_size":2000,"status":"success"}
{"timestamp":"2024-01-12T10:00:06.678Z","level":"info","type":"gateway","event":"shutdown","message":"Gateway shutting down"}
`

	gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
	err := os.WriteFile(gatewayLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	metrics, err := parseGatewayLogs(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify overall metrics
	assert.Len(t, metrics.Servers, 2, "Should have 2 servers")
	assert.Equal(t, 5, metrics.TotalRequests, "Should have 5 requests")
	assert.Equal(t, 5, metrics.TotalToolCalls, "Should have 5 tool calls")
	assert.Equal(t, 1, metrics.TotalErrors, "Should have 1 error")

	// Verify GitHub server metrics
	githubServer := metrics.Servers["github"]
	require.NotNil(t, githubServer)
	assert.Equal(t, 3, githubServer.RequestCount)
	assert.Equal(t, 3, githubServer.ToolCallCount)
	assert.Equal(t, 1, githubServer.ErrorCount)

	// Verify Playwright server metrics
	playwrightServer := metrics.Servers["playwright"]
	require.NotNil(t, playwrightServer)
	assert.Equal(t, 2, playwrightServer.RequestCount)
	assert.Equal(t, 2, playwrightServer.ToolCallCount)
	assert.Equal(t, 0, playwrightServer.ErrorCount)

	// Verify tool metrics
	assert.Len(t, githubServer.Tools, 2)
	assert.Len(t, playwrightServer.Tools, 2)

	// Verify GitHub tools
	getRepoTool := githubServer.Tools["get_repository"]
	require.NotNil(t, getRepoTool)
	assert.Equal(t, 2, getRepoTool.CallCount)
	assert.Equal(t, 1, getRepoTool.ErrorCount)

	listIssuesTool := githubServer.Tools["list_issues"]
	require.NotNil(t, listIssuesTool)
	assert.Equal(t, 1, listIssuesTool.CallCount)
	assert.Equal(t, 0, listIssuesTool.ErrorCount)

	// Test rendering
	output := renderGatewayMetricsTable(metrics, false)
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "playwright")

	// Test verbose rendering
	verboseOutput := renderGatewayMetricsTable(metrics, true)
	assert.Contains(t, verboseOutput, "Tool Usage Details")
	assert.Contains(t, verboseOutput, "get_repository")
	assert.Contains(t, verboseOutput, "list_issues")
	assert.Contains(t, verboseOutput, "navigate")
	assert.Contains(t, verboseOutput, "screenshot")

	// Verify time range was captured
	assert.False(t, metrics.StartTime.IsZero())
	assert.False(t, metrics.EndTime.IsZero())
	assert.True(t, metrics.EndTime.After(metrics.StartTime))
}

func TestParseGatewayLogsFromMCPLogsSubdirectory(t *testing.T) {
	t.Parallel()
	// Create temp directory for test
	tmpDir := t.TempDir()

	// Create mcp-logs subdirectory (path after artifact download)
	mcpLogsDir := filepath.Join(tmpDir, "mcp-logs")
	err := os.MkdirAll(mcpLogsDir, 0755)
	require.NoError(t, err, "should create mcp-logs directory")

	// Create test gateway.jsonl in mcp-logs subdirectory
	testLogContent := `{"timestamp":"2024-01-15T10:00:00Z","level":"info","event":"tool_call","server_name":"github","tool_name":"search_code","duration":250}
{"timestamp":"2024-01-15T10:00:01Z","level":"info","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":180}
{"timestamp":"2024-01-15T10:00:02Z","level":"error","event":"tool_call","server_name":"github","tool_name":"create_issue","duration":100}
`
	gatewayLogPath := filepath.Join(mcpLogsDir, "gateway.jsonl")
	err = os.WriteFile(gatewayLogPath, []byte(testLogContent), 0644)
	require.NoError(t, err, "should write test gateway.jsonl in mcp-logs")

	// Test parsing from mcp-logs subdirectory
	metrics, err := parseGatewayLogs(tmpDir, false)
	require.NoError(t, err, "should parse gateway logs from mcp-logs subdirectory")
	require.NotNil(t, metrics, "metrics should not be nil")

	// Verify results
	assert.Equal(t, 3, metrics.TotalRequests, "should have 3 total requests")
	assert.Len(t, metrics.Servers, 1, "should have 1 server")

	// Verify server metrics
	githubMetrics, ok := metrics.Servers["github"]
	require.True(t, ok, "should have github server metrics")
	assert.Equal(t, 3, githubMetrics.RequestCount, "should have 3 total calls for github server")
}

func TestParseRPCMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		logContent    string
		wantServers   int
		wantRequests  int
		wantToolCalls int
		wantErrors    int
		wantErr       bool
	}{
		{
			name: "valid rpc-messages with tool calls",
			logContent: `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_repository","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.150000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{"content":[]}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.250000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"result":{"content":[]}}}
`,
			wantServers:   1,
			wantRequests:  2,
			wantToolCalls: 2,
			wantErrors:    0,
			wantErr:       false,
		},
		{
			name: "rpc-messages with error response",
			logContent: `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_repository","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.050000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"connection timeout"}}}
`,
			wantServers:   1,
			wantRequests:  1,
			wantToolCalls: 1,
			wantErrors:    1,
			wantErr:       false,
		},
		{
			name: "rpc-messages with multiple servers",
			logContent: `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_repos","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.100000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"playwright","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"navigate","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.500000000Z","direction":"IN","type":"RESPONSE","server_id":"playwright","payload":{"jsonrpc":"2.0","id":2,"result":{}}}
`,
			wantServers:   2,
			wantRequests:  2,
			wantToolCalls: 2,
			wantErrors:    0,
			wantErr:       false,
		},
		{
			name: "rpc-messages skips non-tools/call methods",
			logContent: `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}}
{"timestamp":"2024-01-12T10:00:00.010000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_repository","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.150000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"result":{}}}
`,
			wantServers:   1,
			wantRequests:  1,
			wantToolCalls: 1,
			wantErrors:    0,
			wantErr:       false,
		},
		{
			name:         "empty file",
			logContent:   "",
			wantServers:  0,
			wantRequests: 0,
			wantErrors:   0,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
			err := os.WriteFile(logPath, []byte(tt.logContent), 0644)
			require.NoError(t, err, "should write test rpc-messages.jsonl")

			metrics, err := parseRPCMessages(logPath, false)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err, "parseRPCMessages should not return error")
			require.NotNil(t, metrics, "metrics should not be nil")

			assert.Len(t, metrics.Servers, tt.wantServers, "server count mismatch")
			assert.Equal(t, tt.wantRequests, metrics.TotalRequests, "total requests mismatch")
			assert.Equal(t, tt.wantToolCalls, metrics.TotalToolCalls, "total tool calls mismatch")
			assert.Equal(t, tt.wantErrors, metrics.TotalErrors, "total errors mismatch")
		})
	}
}

func TestParseGatewayLogsFallsBackToRPCMessages(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create mcp-logs/rpc-messages.jsonl (no gateway.jsonl present)
	mcpLogsDir := filepath.Join(tmpDir, "mcp-logs")
	require.NoError(t, os.MkdirAll(mcpLogsDir, 0755))

	rpcContent := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.200000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
`
	err := os.WriteFile(filepath.Join(mcpLogsDir, "rpc-messages.jsonl"), []byte(rpcContent), 0644)
	require.NoError(t, err, "should write rpc-messages.jsonl")

	// parseGatewayLogs should fall back to rpc-messages.jsonl
	metrics, err := parseGatewayLogs(tmpDir, false)
	require.NoError(t, err, "should fall back to rpc-messages.jsonl")
	require.NotNil(t, metrics, "metrics should not be nil")

	assert.Equal(t, 1, metrics.TotalRequests, "should have 1 request from rpc-messages.jsonl")
	assert.Len(t, metrics.Servers, 1, "should have 1 server")

	_, hasGitHub := metrics.Servers["github"]
	assert.True(t, hasGitHub, "should have github server")
}

func TestFindRPCMessagesPath(t *testing.T) {
	t.Parallel()
	t.Run("rpc-messages in mcp-logs subdirectory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		mcpDir := filepath.Join(tmpDir, "mcp-logs")
		require.NoError(t, os.MkdirAll(mcpDir, 0755))
		rpcPath := filepath.Join(mcpDir, "rpc-messages.jsonl")
		require.NoError(t, os.WriteFile(rpcPath, []byte("{}"), 0644))

		result := findRPCMessagesPath(tmpDir)
		assert.Equal(t, rpcPath, result, "should find rpc-messages.jsonl in mcp-logs")
	})

	t.Run("rpc-messages in root directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		rpcPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
		require.NoError(t, os.WriteFile(rpcPath, []byte("{}"), 0644))

		result := findRPCMessagesPath(tmpDir)
		assert.Equal(t, rpcPath, result, "should find rpc-messages.jsonl in root")
	})

	t.Run("mcp-logs subdirectory takes priority over root", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		mcpDir := filepath.Join(tmpDir, "mcp-logs")
		require.NoError(t, os.MkdirAll(mcpDir, 0755))
		mcpPath := filepath.Join(mcpDir, "rpc-messages.jsonl")
		rootPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
		require.NoError(t, os.WriteFile(mcpPath, []byte("{}"), 0644))
		require.NoError(t, os.WriteFile(rootPath, []byte("{}"), 0644))

		result := findRPCMessagesPath(tmpDir)
		assert.Equal(t, mcpPath, result, "mcp-logs should take priority over root")
	})

	t.Run("not found returns empty string", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		result := findRPCMessagesPath(tmpDir)
		assert.Empty(t, result, "should return empty string when not found")
	})
}

func TestBuildToolCallsFromRPCMessages(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	rpcContent := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.200000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_repository","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.050000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"rate limit"}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(rpcContent), 0644))

	calls, err := buildToolCallsFromRPCMessages(logPath)
	require.NoError(t, err, "should build tool calls without error")
	require.Len(t, calls, 2, "should have 2 tool calls")

	// Find each call (order may vary)
	var listIssues, getRepo *MCPToolCall
	for i := range calls {
		switch calls[i].ToolName {
		case "list_issues":
			listIssues = &calls[i]
		case "get_repository":
			getRepo = &calls[i]
		}
	}

	require.NotNil(t, listIssues, "should have list_issues call")
	assert.Equal(t, "github", listIssues.ServerName, "server name should be github")
	assert.Equal(t, "success", listIssues.Status, "status should be success")
	assert.NotEmpty(t, listIssues.Duration, "duration should be set for paired request/response")

	require.NotNil(t, getRepo, "should have get_repository call")
	assert.Equal(t, "github", getRepo.ServerName, "server name should be github")
	assert.Equal(t, "error", getRepo.Status, "status should be error")
	assert.Equal(t, "rate limit", getRepo.Error, "error message should be set")
}

// TestBuildToolCallsFromRPCMessagesNullID verifies that requests with a null/missing ID
// are still included in the tool_calls output (regression test for mcp_tool_usage.tool_calls
// always being null when parseRPCMessages counted tool calls in the summary but
// buildToolCallsFromRPCMessages skipped null-ID requests).
func TestBuildToolCallsFromRPCMessagesNullID(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Requests with null ID (id:null) - these are counted in the summary by parseRPCMessages
	// but were previously skipped by buildToolCallsFromRPCMessages, causing tool_calls=null.
	rpcContent := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":null,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":null,"method":"tools/call","params":{"name":"issue_read","arguments":{}}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(rpcContent), 0644))

	calls, err := buildToolCallsFromRPCMessages(logPath)
	require.NoError(t, err, "should build tool calls without error")

	// Both requests should produce tool call records even without IDs
	assert.Len(t, calls, 2, "null-ID requests should still produce tool call records")

	toolNames := make(map[string]bool)
	for _, c := range calls {
		toolNames[c.ToolName] = true
		assert.Equal(t, "github", c.ServerName, "server name should be set")
		assert.Equal(t, "unknown", c.Status, "status should be 'unknown' for null-ID requests")
	}
	assert.True(t, toolNames["list_issues"], "should include list_issues")
	assert.True(t, toolNames["issue_read"], "should include issue_read")
}

// TestBuildToolCallsFromRPCMessagesSchemaV2Format verifies that buildToolCallsFromRPCMessages
// handles the real-world schema "rpc-message/v2" format ("event" field instead of "type").
func TestBuildToolCallsFromRPCMessagesSchemaV2Format(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	rpcContent := `{"timestamp":"2026-08-15T23:48:42.233Z","event":"rpc_request","_schema":"rpc-message/v2","direction":"OUT","server_id":"github","method":"tools/call","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2026-08-15T23:48:42.400Z","event":"rpc_response","_schema":"rpc-message/v2","direction":"IN","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(rpcContent), 0644))

	calls, err := buildToolCallsFromRPCMessages(logPath)
	require.NoError(t, err, "should build tool calls without error")
	require.Len(t, calls, 1, "should have 1 tool call")

	assert.Equal(t, "list_issues", calls[0].ToolName)
	assert.Equal(t, "github", calls[0].ServerName)
	assert.Equal(t, "success", calls[0].Status)
	assert.NotEmpty(t, calls[0].Duration, "duration should be set for paired request/response")
}

func TestParseRPCMessagesGuardPolicyErrors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Test rpc-messages.jsonl with guard policy error responses:
	// - A tools/call request followed by a -32006 (integrity below minimum) error response
	// - A tools/call request followed by a -32002 (repository not allowed) error response
	// - A tools/call request followed by a normal success response
	content := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pull_request_read","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.100000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"error":{"code":-32006,"message":"Content integrity below minimum threshold","data":{"reason":"integrity_below_minimum","details":"Content integrity 'unapproved' is below minimum 'approved'"}}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_file_contents","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.100000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"Repository not in allowlist","data":{"reason":"repository_not_allowed","repository":"owner/private-repo","details":"Repository 'owner/private-repo' does not match any repos patterns"}}}}
{"timestamp":"2024-01-12T10:00:02.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:02.200000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":3,"result":{"content":[]}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0644))

	metrics, err := parseRPCMessages(logPath, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Should count 3 requests total
	assert.Equal(t, 3, metrics.TotalRequests, "should count 3 requests")
	assert.Equal(t, 3, metrics.TotalToolCalls, "should count 3 tool calls")

	// 2 errors total (guard policy errors are also errors)
	assert.Equal(t, 2, metrics.TotalErrors, "should count 2 errors")

	// 2 guard policy blocks
	assert.Equal(t, 2, metrics.TotalGuardBlocked, "should count 2 guard policy blocks")

	// Check guard policy events
	require.Len(t, metrics.GuardPolicyEvents, 2, "should have 2 guard policy events")

	// First event: integrity below minimum
	evt1 := metrics.GuardPolicyEvents[0]
	assert.Equal(t, "github", evt1.ServerID)
	assert.Equal(t, "pull_request_read", evt1.ToolName)
	assert.Equal(t, -32006, evt1.ErrorCode)
	assert.Equal(t, "integrity_below_minimum", evt1.Reason)
	assert.Equal(t, "Content integrity below minimum threshold", evt1.Message)
	assert.Contains(t, evt1.Details, "below minimum")

	// Second event: repository not allowed
	evt2 := metrics.GuardPolicyEvents[1]
	assert.Equal(t, "github", evt2.ServerID)
	assert.Equal(t, "get_file_contents", evt2.ToolName)
	assert.Equal(t, -32002, evt2.ErrorCode)
	assert.Equal(t, "repository_not_allowed", evt2.Reason)
	assert.Equal(t, "owner/private-repo", evt2.Repository)

	// Server should have GuardPolicyBlocked = 2
	githubServer := metrics.Servers["github"]
	require.NotNil(t, githubServer)
	assert.Equal(t, 2, githubServer.GuardPolicyBlocked, "server should have 2 guard policy blocks")
	assert.Equal(t, 2, githubServer.ErrorCount, "server should have 2 errors")
}

func TestParseRPCMessagesGuardPolicyWithoutData(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Guard policy error without the optional data field
	content := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"issue_read","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.050000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"error":{"code":-32005,"message":"Content from blocked user"}}}
`
	logPath := filepath.Join(tmpDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0644))

	metrics, err := parseRPCMessages(logPath, false)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, 1, metrics.TotalGuardBlocked, "should count 1 guard policy block")
	require.Len(t, metrics.GuardPolicyEvents, 1)

	evt := metrics.GuardPolicyEvents[0]
	assert.Equal(t, -32005, evt.ErrorCode)
	assert.Equal(t, "blocked_user", evt.Reason, "should use default reason from error code")
	assert.Equal(t, "Content from blocked user", evt.Message)
	assert.Empty(t, evt.Details, "details should be empty without data field")
	assert.Empty(t, evt.Repository, "repository should be empty without data field")
}

func TestIsGuardPolicyErrorCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code     int
		expected bool
	}{
		{-32001, true},  // Access denied
		{-32002, true},  // Repo not allowed
		{-32003, true},  // Insufficient permissions
		{-32004, true},  // Private repo denied
		{-32005, true},  // Blocked user
		{-32006, true},  // Integrity below minimum
		{-32000, false}, // Regular JSON-RPC error
		{-32007, false}, // Out of range
		{0, false},
		{-1, false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, isGuardPolicyErrorCode(tt.code),
			"isGuardPolicyErrorCode(%d) should be %v", tt.code, tt.expected)
	}
}

func TestGuardPolicyReasonFromCode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "access_denied", guardPolicyReasonFromCode(-32001))
	assert.Equal(t, "repo_not_allowed", guardPolicyReasonFromCode(-32002))
	assert.Equal(t, "insufficient_permissions", guardPolicyReasonFromCode(-32003))
	assert.Equal(t, "private_repo_denied", guardPolicyReasonFromCode(-32004))
	assert.Equal(t, "blocked_user", guardPolicyReasonFromCode(-32005))
	assert.Equal(t, "integrity_below_minimum", guardPolicyReasonFromCode(-32006))
	assert.Equal(t, "unknown", guardPolicyReasonFromCode(-32000))
}

func TestProcessGatewayLogEntryGuardPolicyBlocked(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	entry := &GatewayLogEntry{
		Timestamp:   "2024-01-12T10:00:00Z",
		Type:        "GUARD_POLICY_BLOCKED",
		ServerID:    "github",
		ToolName:    "pull_request_read",
		Reason:      "integrity_below_minimum",
		Message:     "Content integrity below minimum threshold",
		Description: "Content integrity 'unapproved' is below minimum 'approved'",
	}

	processGatewayLogEntry(entry, metrics, false)

	assert.Equal(t, 0, metrics.TotalRequests, "GUARD_POLICY_BLOCKED should not increment TotalRequests")
	assert.Equal(t, 1, metrics.TotalGuardBlocked, "should increment TotalGuardBlocked")
	require.Len(t, metrics.GuardPolicyEvents, 1, "should record one guard policy event")

	evt := metrics.GuardPolicyEvents[0]
	assert.Equal(t, "github", evt.ServerID)
	assert.Equal(t, "pull_request_read", evt.ToolName)
	assert.Equal(t, "integrity_below_minimum", evt.Reason)
	assert.Equal(t, "Content integrity below minimum threshold", evt.Message)
	assert.Equal(t, "Content integrity 'unapproved' is below minimum 'approved'", evt.Details)

	githubServer := metrics.Servers["github"]
	require.NotNil(t, githubServer)
	assert.Equal(t, 1, githubServer.GuardPolicyBlocked)
}

func TestBuildGuardPolicySummary(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		TotalGuardBlocked: 5,
		GuardPolicyEvents: []GuardPolicyEvent{
			// Two identical pull_request_read events to verify per-tool count aggregation
			{ServerID: "github", ToolName: "pull_request_read", ErrorCode: guardPolicyErrorCodeIntegrityBelowMin, Reason: "integrity_below_minimum"},
			{ServerID: "github", ToolName: "pull_request_read", ErrorCode: guardPolicyErrorCodeIntegrityBelowMin, Reason: "integrity_below_minimum"},
			{ServerID: "github", ToolName: "get_file_contents", ErrorCode: guardPolicyErrorCodeRepoNotAllowed, Reason: "repo_not_allowed", Repository: "owner/repo"},
			{ServerID: "github", ToolName: "issue_read", ErrorCode: guardPolicyErrorCodeBlockedUser, Reason: "blocked_user"},
			{ServerID: "other-server", ToolName: "list_issues", ErrorCode: guardPolicyErrorCodeAccessDenied, Reason: "access_denied"},
		},
		Servers: make(map[string]*GatewayServerMetrics),
	}

	summary := buildGuardPolicySummary(metrics)
	require.NotNil(t, summary)

	assert.Equal(t, 5, summary.TotalBlocked)
	assert.Equal(t, 2, summary.IntegrityBlocked, "should have 2 integrity blocks")
	assert.Equal(t, 1, summary.RepoScopeBlocked, "should have 1 repo scope block")
	assert.Equal(t, 1, summary.BlockedUserDenied, "should have 1 blocked user")
	assert.Equal(t, 1, summary.AccessDenied, "should have 1 access denied")
	assert.Equal(t, 0, summary.PermissionDenied, "should have 0 permission denied")
	assert.Equal(t, 0, summary.PrivateRepoDenied, "should have 0 private repo denied")

	// Check per-tool blocked counts
	assert.Equal(t, 2, summary.BlockedToolCounts["pull_request_read"])
	assert.Equal(t, 1, summary.BlockedToolCounts["get_file_contents"])
	assert.Equal(t, 1, summary.BlockedToolCounts["issue_read"])
	assert.Equal(t, 1, summary.BlockedToolCounts["list_issues"])

	// Check per-server blocked counts
	assert.Equal(t, 4, summary.BlockedServerCounts["github"])
	assert.Equal(t, 1, summary.BlockedServerCounts["other-server"])
}

func TestExtractMCPToolUsageDataWithGuardPolicy(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create rpc-messages.jsonl with guard policy errors
	content := `{"timestamp":"2024-01-12T10:00:00.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pull_request_read","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:00.100000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"error":{"code":-32006,"message":"Integrity below minimum","data":{"reason":"integrity_below_minimum"}}}}
{"timestamp":"2024-01-12T10:00:01.000000000Z","direction":"OUT","type":"REQUEST","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_issues","arguments":{}}}}
{"timestamp":"2024-01-12T10:00:01.200000000Z","direction":"IN","type":"RESPONSE","server_id":"github","payload":{"jsonrpc":"2.0","id":2,"result":{"content":[]}}}
`
	// Create in mcp-logs subdirectory to test the fallback path
	mcpLogsDir := filepath.Join(tmpDir, "mcp-logs")
	require.NoError(t, os.MkdirAll(mcpLogsDir, 0755))
	logPath := filepath.Join(mcpLogsDir, "rpc-messages.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0644))

	mcpData, err := extractMCPToolUsageData(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, mcpData)

	// Guard policy summary should be populated
	require.NotNil(t, mcpData.GuardPolicySummary, "guard policy summary should be populated")
	assert.Equal(t, 1, mcpData.GuardPolicySummary.TotalBlocked)
	assert.Equal(t, 1, mcpData.GuardPolicySummary.IntegrityBlocked)
	require.Len(t, mcpData.GuardPolicySummary.Events, 1)
	assert.Equal(t, "pull_request_read", mcpData.GuardPolicySummary.Events[0].ToolName)
}

// TestExtractToolCallsStatusInference verifies that extractToolCallsFromGatewayLog correctly
// infers the per-call status when the gateway.jsonl entry omits the "status" field.
// Post-OTel-collector migrations may drop the explicit "status" key; the parser must
// fall back to the "error"/"level" fields to produce "success" or "error" rather than
// leaving Status blank (which downstream agents render as "unknown").
func TestExtractToolCallsStatusInference(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		logLine      string
		wantStatus   string
		wantErrField string
	}{
		{
			name:       "explicit status success",
			logLine:    `{"timestamp":"2024-01-12T10:00:00Z","level":"info","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":100,"status":"success"}`,
			wantStatus: "success",
		},
		{
			name:         "explicit status error with error field",
			logLine:      `{"timestamp":"2024-01-12T10:00:01Z","level":"error","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":50,"status":"error","error":"rate limit"}`,
			wantStatus:   "error",
			wantErrField: "rate limit",
		},
		{
			name:       "no status field, level info — infer success",
			logLine:    `{"timestamp":"2024-01-12T10:00:02Z","level":"info","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":120}`,
			wantStatus: "success",
		},
		{
			name:         "no status field, level error — infer error",
			logLine:      `{"timestamp":"2024-01-12T10:00:03Z","level":"error","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":30}`,
			wantStatus:   "error",
			wantErrField: "",
		},
		{
			name:         "no status field, non-empty error field — infer error",
			logLine:      `{"timestamp":"2024-01-12T10:00:04Z","level":"info","event":"tool_call","server_name":"github","tool_name":"list_issues","duration":40,"error":"connection reset"}`,
			wantStatus:   "error",
			wantErrField: "connection reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
			require.NoError(t, os.WriteFile(gatewayLogPath, []byte(tt.logLine+"\n"), 0600))

			mcpData := &MCPToolUsageData{ToolCalls: []MCPToolCall{}}
			require.NoError(t, extractToolCallsFromGatewayLog(gatewayLogPath, mcpData))
			require.Len(t, mcpData.ToolCalls, 1, "should have exactly one tool call record")

			tc := mcpData.ToolCalls[0]
			require.Equal(t, tt.wantStatus, tc.Status, "tool call status mismatch")
			assert.Equal(t, tt.wantErrField, tc.Error, "tool call error field mismatch")
		})
	}
}

// TestProcessGatewayLogEntryLevelErrorCounting verifies that processGatewayLogEntry
// counts a tool-call entry as an error when "level":"error" is set, even if the
// "status" and "error" fields are absent (post-OTel format drift).
func TestProcessGatewayLogEntryLevelErrorCounting(t *testing.T) {
	t.Parallel()
	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	// Simulate a gateway log entry that uses level:"error" without an explicit status field.
	entry := &GatewayLogEntry{
		Timestamp:  "2024-01-12T10:00:00Z",
		Level:      "error",
		Event:      "tool_call",
		ServerName: "github",
		ToolName:   "list_issues",
		Duration:   50,
	}

	processGatewayLogEntry(entry, metrics, false)

	assert.Equal(t, 1, metrics.TotalRequests, "should count as a request")
	assert.Equal(t, 1, metrics.TotalErrors, "level:error should increment TotalErrors")

	server := metrics.Servers["github"]
	require.NotNil(t, server)
	assert.Equal(t, 1, server.ErrorCount, "server error count should be 1")
	tool := server.Tools["list_issues"]
	require.NotNil(t, tool)
	assert.Equal(t, 1, tool.ErrorCount, "tool error count should be 1")
}
