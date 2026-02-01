//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPToolUsageSummary(t *testing.T) {
	tests := []struct {
		name              string
		processedRuns     []ProcessedRun
		expectedServers   int
		expectedTools     int
		expectedToolCalls int
		expectNil         bool
	}{
		{
			name: "single run with MCP tool usage",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{
						DatabaseID:   12345,
						WorkflowName: "Test Workflow",
					},
					MCPToolUsage: &MCPToolUsageData{
						Summary: []MCPToolSummary{
							{
								ServerName:      "github",
								ToolName:        "search_issues",
								CallCount:       5,
								TotalInputSize:  5000,
								TotalOutputSize: 25000,
								MaxInputSize:    1500,
								MaxOutputSize:   8000,
								AvgDuration:     "150ms",
								MaxDuration:     "200ms",
								ErrorCount:      0,
							},
						},
						Servers: []MCPServerStats{
							{
								ServerName:      "github",
								RequestCount:    5,
								ToolCallCount:   5,
								TotalInputSize:  5000,
								TotalOutputSize: 25000,
								AvgDuration:     "150ms",
								ErrorCount:      0,
							},
						},
						ToolCalls: []MCPToolCall{
							{
								Timestamp:  "2024-01-12T10:00:00Z",
								ServerName: "github",
								ToolName:   "search_issues",
								InputSize:  1000,
								OutputSize: 5000,
								Duration:   "150ms",
								Status:     "success",
							},
						},
					},
				},
			},
			expectedServers:   1,
			expectedTools:     1,
			expectedToolCalls: 1,
			expectNil:         false,
		},
		{
			name: "multiple runs with same tool",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{DatabaseID: 1},
					MCPToolUsage: &MCPToolUsageData{
						Summary: []MCPToolSummary{
							{
								ServerName:      "github",
								ToolName:        "search_issues",
								CallCount:       3,
								TotalInputSize:  3000,
								TotalOutputSize: 15000,
								MaxInputSize:    1200,
								MaxOutputSize:   6000,
								AvgDuration:     "100ms",
							},
						},
						Servers: []MCPServerStats{
							{
								ServerName:      "github",
								RequestCount:    3,
								ToolCallCount:   3,
								TotalInputSize:  3000,
								TotalOutputSize: 15000,
								AvgDuration:     "100ms",
							},
						},
						ToolCalls: []MCPToolCall{
							{ServerName: "github", ToolName: "search_issues", InputSize: 1000, OutputSize: 5000, Status: "success"},
						},
					},
				},
				{
					Run: WorkflowRun{DatabaseID: 2},
					MCPToolUsage: &MCPToolUsageData{
						Summary: []MCPToolSummary{
							{
								ServerName:      "github",
								ToolName:        "search_issues",
								CallCount:       2,
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
								MaxInputSize:    1500,
								MaxOutputSize:   8000,
								AvgDuration:     "150ms",
							},
						},
						Servers: []MCPServerStats{
							{
								ServerName:      "github",
								RequestCount:    2,
								ToolCallCount:   2,
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
								AvgDuration:     "150ms",
							},
						},
						ToolCalls: []MCPToolCall{
							{ServerName: "github", ToolName: "search_issues", InputSize: 1000, OutputSize: 5000, Status: "success"},
						},
					},
				},
			},
			expectedServers:   1,
			expectedTools:     1,
			expectedToolCalls: 2,
			expectNil:         false,
		},
		{
			name: "multiple servers and tools",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{DatabaseID: 1},
					MCPToolUsage: &MCPToolUsageData{
						Summary: []MCPToolSummary{
							{
								ServerName:      "github",
								ToolName:        "search_issues",
								CallCount:       2,
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
								MaxInputSize:    1200,
								MaxOutputSize:   6000,
							},
							{
								ServerName:      "playwright",
								ToolName:        "navigate",
								CallCount:       1,
								TotalInputSize:  500,
								TotalOutputSize: 1000,
								MaxInputSize:    500,
								MaxOutputSize:   1000,
							},
						},
						Servers: []MCPServerStats{
							{
								ServerName:      "github",
								RequestCount:    2,
								ToolCallCount:   2,
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
							},
							{
								ServerName:      "playwright",
								RequestCount:    1,
								ToolCallCount:   1,
								TotalInputSize:  500,
								TotalOutputSize: 1000,
							},
						},
						ToolCalls: []MCPToolCall{
							{ServerName: "github", ToolName: "search_issues"},
							{ServerName: "playwright", ToolName: "navigate"},
						},
					},
				},
			},
			expectedServers:   2,
			expectedTools:     2,
			expectedToolCalls: 2,
			expectNil:         false,
		},
		{
			name: "no MCP tool usage data",
			processedRuns: []ProcessedRun{
				{
					Run:          WorkflowRun{DatabaseID: 1},
					MCPToolUsage: nil,
				},
			},
			expectNil: true,
		},
		{
			name:          "empty runs",
			processedRuns: []ProcessedRun{},
			expectNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildMCPToolUsageSummary(tt.processedRuns)

			if tt.expectNil {
				assert.Nil(t, summary, "Expected nil summary when no MCP data")
				return
			}

			require.NotNil(t, summary, "Expected non-nil summary")
			assert.Len(t, summary.Servers, tt.expectedServers, "Server count mismatch")
			assert.Len(t, summary.Summary, tt.expectedTools, "Tool count mismatch")
			assert.Len(t, summary.ToolCalls, tt.expectedToolCalls, "Tool calls count mismatch")
		})
	}
}

func TestBuildMCPToolUsageSummaryAggregation(t *testing.T) {
	// Test that aggregation correctly merges data from multiple runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{
						ServerName:      "github",
						ToolName:        "search_issues",
						CallCount:       3,
						TotalInputSize:  3000,
						TotalOutputSize: 15000,
						MaxInputSize:    1200,
						MaxOutputSize:   6000,
						AvgDuration:     "100ms",
						MaxDuration:     "150ms",
						ErrorCount:      0,
					},
				},
				Servers: []MCPServerStats{
					{
						ServerName:      "github",
						RequestCount:    3,
						ToolCallCount:   3,
						TotalInputSize:  3000,
						TotalOutputSize: 15000,
						AvgDuration:     "100ms",
						ErrorCount:      0,
					},
				},
				ToolCalls: []MCPToolCall{
					{ServerName: "github", ToolName: "search_issues", InputSize: 1000, OutputSize: 5000},
				},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 2},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{
						ServerName:      "github",
						ToolName:        "search_issues",
						CallCount:       2,
						TotalInputSize:  2000,
						TotalOutputSize: 10000,
						MaxInputSize:    1500, // Larger than first run
						MaxOutputSize:   8000, // Larger than first run
						AvgDuration:     "150ms",
						MaxDuration:     "200ms",
						ErrorCount:      1,
					},
				},
				Servers: []MCPServerStats{
					{
						ServerName:      "github",
						RequestCount:    2,
						ToolCallCount:   2,
						TotalInputSize:  2000,
						TotalOutputSize: 10000,
						AvgDuration:     "150ms",
						ErrorCount:      1,
					},
				},
				ToolCalls: []MCPToolCall{
					{ServerName: "github", ToolName: "search_issues", InputSize: 1000, OutputSize: 5000},
				},
			},
		},
	}

	summary := buildMCPToolUsageSummary(processedRuns)
	require.NotNil(t, summary)

	// Should have one server and one tool (merged)
	require.Len(t, summary.Servers, 1, "Should merge into one server")
	require.Len(t, summary.Summary, 1, "Should merge into one tool summary")

	// Check server aggregation
	server := summary.Servers[0]
	assert.Equal(t, "github", server.ServerName)
	assert.Equal(t, 5, server.RequestCount, "Should sum request counts: 3+2=5")
	assert.Equal(t, 5, server.ToolCallCount, "Should sum tool call counts: 3+2=5")
	assert.Equal(t, 5000, server.TotalInputSize, "Should sum input sizes: 3000+2000=5000")
	assert.Equal(t, 25000, server.TotalOutputSize, "Should sum output sizes: 15000+10000=25000")
	assert.Equal(t, 1, server.ErrorCount, "Should sum error counts: 0+1=1")

	// Check tool summary aggregation
	tool := summary.Summary[0]
	assert.Equal(t, "github", tool.ServerName)
	assert.Equal(t, "search_issues", tool.ToolName)
	assert.Equal(t, 5, tool.CallCount, "Should sum call counts: 3+2=5")
	assert.Equal(t, 5000, tool.TotalInputSize, "Should sum input sizes: 3000+2000=5000")
	assert.Equal(t, 25000, tool.TotalOutputSize, "Should sum output sizes: 15000+10000=25000")
	assert.Equal(t, 1500, tool.MaxInputSize, "Should use max of max inputs: max(1200, 1500)=1500")
	assert.Equal(t, 8000, tool.MaxOutputSize, "Should use max of max outputs: max(6000, 8000)=8000")
	assert.Equal(t, "200ms", tool.MaxDuration, "Should use max of max durations: max(150ms, 200ms)=200ms")
	assert.Equal(t, 1, tool.ErrorCount, "Should sum error counts: 0+1=1")

	// Check that tool calls are all present
	assert.Len(t, summary.ToolCalls, 2, "Should have all tool calls from both runs")
}

func TestBuildMCPToolUsageSummarySorting(t *testing.T) {
	// Test that results are sorted correctly
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "playwright", ToolName: "navigate", CallCount: 1},
					{ServerName: "github", ToolName: "search_issues", CallCount: 1},
					{ServerName: "github", ToolName: "get_repository", CallCount: 1},
				},
				Servers: []MCPServerStats{
					{ServerName: "playwright", RequestCount: 1},
					{ServerName: "github", RequestCount: 2},
				},
				ToolCalls: []MCPToolCall{},
			},
		},
	}

	summary := buildMCPToolUsageSummary(processedRuns)
	require.NotNil(t, summary)

	// Servers should be sorted alphabetically
	require.Len(t, summary.Servers, 2)
	assert.Equal(t, "github", summary.Servers[0].ServerName, "First server should be github")
	assert.Equal(t, "playwright", summary.Servers[1].ServerName, "Second server should be playwright")

	// Tools should be sorted by server name, then tool name
	require.Len(t, summary.Summary, 3)
	assert.Equal(t, "github", summary.Summary[0].ServerName)
	assert.Equal(t, "get_repository", summary.Summary[0].ToolName)
	assert.Equal(t, "github", summary.Summary[1].ServerName)
	assert.Equal(t, "search_issues", summary.Summary[1].ToolName)
	assert.Equal(t, "playwright", summary.Summary[2].ServerName)
	assert.Equal(t, "navigate", summary.Summary[2].ToolName)
}
