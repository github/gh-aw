//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMCPToolUsageData(t *testing.T) {
	tests := []struct {
		name              string
		logContent        string
		wantServers       int
		wantTools         int
		wantToolCalls     int
		wantErr           bool
		checkServerStats  bool
		expectedCallCount int
	}{
		{
			name: "valid gateway log with tool calls",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","method":"search_issues","duration":150.5,"input_size":1024,"output_size":5120,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","method":"search_issues","duration":200.3,"input_size":512,"output_size":6144,"status":"success"}
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"get_repository","method":"get_repository","duration":100.0,"input_size":256,"output_size":2048,"status":"success"}
`,
			wantServers:       1,
			wantTools:         2,
			wantToolCalls:     3,
			wantErr:           false,
			checkServerStats:  true,
			expectedCallCount: 3,
		},
		{
			name: "multiple servers",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":150.5,"input_size":1024,"output_size":5120,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"playwright","tool_name":"navigate","duration":250.0,"input_size":512,"output_size":1024,"status":"success"}
`,
			wantServers:   2,
			wantTools:     2,
			wantToolCalls: 2,
			wantErr:       false,
		},
		{
			name: "tool call with errors",
			logContent: `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":50.0,"input_size":100,"output_size":0,"status":"error","error":"connection timeout"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":100.0,"input_size":200,"output_size":1000,"status":"success"}
`,
			wantServers:   1,
			wantTools:     1,
			wantToolCalls: 2,
			wantErr:       false,
		},
		{
			name:          "no gateway.jsonl file",
			logContent:    "",
			wantServers:   0,
			wantTools:     0,
			wantToolCalls: 0,
			wantErr:       false, // Should return nil, not an error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Only create gateway.jsonl if there's content
			if tt.logContent != "" {
				gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
				err := os.WriteFile(gatewayLogPath, []byte(tt.logContent), 0644)
				require.NoError(t, err, "Failed to write test gateway.jsonl")
			}

			// Extract MCP tool usage data
			mcpData, err := extractMCPToolUsageData(tmpDir, false)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// If no content, expect nil data
			if tt.logContent == "" {
				assert.Nil(t, mcpData, "Expected nil data when gateway.jsonl doesn't exist")
				return
			}

			require.NotNil(t, mcpData, "Expected non-nil MCP data")

			// Verify server count
			assert.Len(t, mcpData.Servers, tt.wantServers, "Server count mismatch")

			// Verify tool summary count
			assert.Len(t, mcpData.Summary, tt.wantTools, "Tool summary count mismatch")

			// Verify individual tool calls count
			assert.Len(t, mcpData.ToolCalls, tt.wantToolCalls, "Tool calls count mismatch")

			// Additional checks for server statistics
			if tt.checkServerStats && len(mcpData.Servers) > 0 {
				server := mcpData.Servers[0]
				assert.Equal(t, "github", server.ServerName, "Server name mismatch")
				assert.Equal(t, tt.expectedCallCount, server.ToolCallCount, "Tool call count mismatch")
				assert.Positive(t, server.TotalInputSize, "Total input size should be positive")
				assert.Positive(t, server.TotalOutputSize, "Total output size should be positive")
			}
		})
	}
}

func TestMCPToolSummaryCalculations(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a log with multiple calls to the same tool with varying sizes
	logContent := `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":100.0,"input_size":500,"output_size":2000,"status":"success"}
{"timestamp":"2024-01-12T10:00:01Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":150.0,"input_size":1500,"output_size":8000,"status":"success"}
{"timestamp":"2024-01-12T10:00:02Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":120.0,"input_size":800,"output_size":5000,"status":"success"}
`

	gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
	err := os.WriteFile(gatewayLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	mcpData, err := extractMCPToolUsageData(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, mcpData)

	// Verify we have exactly one tool summary
	require.Len(t, mcpData.Summary, 1, "Should have exactly one tool summary")
	tool := mcpData.Summary[0]

	// Verify aggregated statistics
	assert.Equal(t, "github", tool.ServerName)
	assert.Equal(t, "search_issues", tool.ToolName)
	assert.Equal(t, 3, tool.CallCount, "Should have 3 calls")
	assert.Equal(t, 2800, tool.TotalInputSize, "Total input: 500+1500+800 = 2800")
	assert.Equal(t, 15000, tool.TotalOutputSize, "Total output: 2000+8000+5000 = 15000")
	assert.Equal(t, 1500, tool.MaxInputSize, "Max input should be 1500")
	assert.Equal(t, 8000, tool.MaxOutputSize, "Max output should be 8000")

	// Verify we have 3 individual tool call records
	require.Len(t, mcpData.ToolCalls, 3, "Should have 3 tool call records")

	// Verify individual tool call data
	for i, tc := range mcpData.ToolCalls {
		assert.Equal(t, "github", tc.ServerName, "Tool call %d: server name mismatch", i)
		assert.Equal(t, "search_issues", tc.ToolName, "Tool call %d: tool name mismatch", i)
		assert.Equal(t, "success", tc.Status, "Tool call %d: status mismatch", i)
		assert.NotEmpty(t, tc.Timestamp, "Tool call %d: timestamp should not be empty", i)
		assert.NotEmpty(t, tc.Duration, "Tool call %d: duration should not be empty", i)
	}
}

func TestBuildAuditDataWithMCPToolUsage(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a simple gateway log
	logContent := `{"timestamp":"2024-01-12T10:00:00Z","level":"info","type":"request","event":"tool_call","server_name":"github","tool_name":"search_issues","duration":100.0,"input_size":1024,"output_size":5120,"status":"success"}
`
	gatewayLogPath := filepath.Join(tmpDir, "gateway.jsonl")
	err := os.WriteFile(gatewayLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract MCP data
	mcpData, err := extractMCPToolUsageData(tmpDir, false)
	require.NoError(t, err)
	require.NotNil(t, mcpData)

	// Create a ProcessedRun with minimal data
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   12345,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
		},
	}

	// Create LogMetrics with minimal data
	metrics := LogMetrics{
		TokenUsage:    1000,
		EstimatedCost: 0.01,
		Turns:         5,
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, mcpData)

	// Verify MCP tool usage is included
	require.NotNil(t, auditData.MCPToolUsage, "MCP tool usage should be included in audit data")
	assert.Len(t, auditData.MCPToolUsage.Summary, 1, "Should have one tool summary")
	assert.Len(t, auditData.MCPToolUsage.ToolCalls, 1, "Should have one tool call")
	assert.Len(t, auditData.MCPToolUsage.Servers, 1, "Should have one server")

	// Verify the summary data
	tool := auditData.MCPToolUsage.Summary[0]
	assert.Equal(t, "github", tool.ServerName)
	assert.Equal(t, "search_issues", tool.ToolName)
	assert.Equal(t, 1, tool.CallCount)
	assert.Equal(t, 1024, tool.TotalInputSize)
	assert.Equal(t, 5120, tool.TotalOutputSize)
}
