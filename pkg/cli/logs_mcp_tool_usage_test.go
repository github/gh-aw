//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPToolUsageSummary(t *testing.T) {
	t.Parallel()
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
								ServerName: "github",
								ToolUsageStatsBase: ToolUsageStatsBase{
									ToolName:      "search_issues",
									CallCount:     5,
									MaxOutputSize: 8000,
									MaxDuration:   "200ms",
								},
								TotalInputSize:  5000,
								TotalOutputSize: 25000,
								MaxInputSize:    1500,
								AvgDuration:     "150ms",
								ErrorCount:      0,
							},
						},
						Servers: []MCPServerStats{
							{
								MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 5, ErrorCount: 0},
								RequestCount:       5,
								TotalInputSize:     5000,
								TotalOutputSize:    25000,
								AvgDuration:        "150ms",
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
								ServerName: "github",
								ToolUsageStatsBase: ToolUsageStatsBase{
									ToolName:      "search_issues",
									CallCount:     3,
									MaxOutputSize: 6000,
								},
								TotalInputSize:  3000,
								TotalOutputSize: 15000,
								MaxInputSize:    1200,
								AvgDuration:     "100ms",
							},
						},
						Servers: []MCPServerStats{
							{
								MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 3},
								RequestCount:       3,
								TotalInputSize:     3000,
								TotalOutputSize:    15000,
								AvgDuration:        "100ms",
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
								ServerName: "github",
								ToolUsageStatsBase: ToolUsageStatsBase{
									ToolName:      "search_issues",
									CallCount:     2,
									MaxOutputSize: 8000,
								},
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
								MaxInputSize:    1500,
								AvgDuration:     "150ms",
							},
						},
						Servers: []MCPServerStats{
							{
								MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 2},
								RequestCount:       2,
								TotalInputSize:     2000,
								TotalOutputSize:    10000,
								AvgDuration:        "150ms",
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
								ServerName: "github",
								ToolUsageStatsBase: ToolUsageStatsBase{
									ToolName:      "search_issues",
									CallCount:     2,
									MaxOutputSize: 6000,
								},
								TotalInputSize:  2000,
								TotalOutputSize: 10000,
								MaxInputSize:    1200,
							},
							{
								ServerName: "playwright",
								ToolUsageStatsBase: ToolUsageStatsBase{
									ToolName:      "navigate",
									CallCount:     1,
									MaxOutputSize: 1000,
								},
								TotalInputSize:  500,
								TotalOutputSize: 1000,
								MaxInputSize:    500,
							},
						},
						Servers: []MCPServerStats{
							{
								MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 2},
								RequestCount:       2,
								TotalInputSize:     2000,
								TotalOutputSize:    10000,
							},
							{
								MCPServerStatsBase: MCPServerStatsBase{ServerName: "playwright", ToolCallCount: 1},
								RequestCount:       1,
								TotalInputSize:     500,
								TotalOutputSize:    1000,
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
			t.Parallel()
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
	t.Parallel()
	// Test that aggregation correctly merges data from multiple runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{
						ServerName: "github",
						ToolUsageStatsBase: ToolUsageStatsBase{
							ToolName:      "search_issues",
							CallCount:     3,
							MaxOutputSize: 6000,
							MaxDuration:   "150ms",
						},
						TotalInputSize:  3000,
						TotalOutputSize: 15000,
						MaxInputSize:    1200,
						AvgDuration:     "100ms",
						ErrorCount:      0,
					},
				},
				Servers: []MCPServerStats{
					{
						MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 3, ErrorCount: 0},
						RequestCount:       3,
						TotalInputSize:     3000,
						TotalOutputSize:    15000,
						AvgDuration:        "100ms",
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
						ServerName: "github",
						ToolUsageStatsBase: ToolUsageStatsBase{
							ToolName:      "search_issues",
							CallCount:     2,
							MaxOutputSize: 8000, // Larger than first run
							MaxDuration:   "200ms",
						},
						TotalInputSize:  2000,
						TotalOutputSize: 10000,
						MaxInputSize:    1500, // Larger than first run
						AvgDuration:     "150ms",
						ErrorCount:      1,
					},
				},
				Servers: []MCPServerStats{
					{
						MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 2, ErrorCount: 1},
						RequestCount:       2,
						TotalInputSize:     2000,
						TotalOutputSize:    10000,
						AvgDuration:        "150ms",
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
	t.Parallel()
	// Test that results are sorted correctly
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "playwright", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "navigate", CallCount: 1}},
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "search_issues", CallCount: 1}},
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "get_repository", CallCount: 1}},
				},
				Servers: []MCPServerStats{
					{MCPServerStatsBase: MCPServerStatsBase{ServerName: "playwright"}, RequestCount: 1},
					{MCPServerStatsBase: MCPServerStatsBase{ServerName: "github"}, RequestCount: 2},
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

func TestBuildMCPToolUsageSummaryFilteredEvents(t *testing.T) {
	t.Parallel()
	// Verify FilteredEvents are aggregated across runs and that a non-nil summary
	// is returned when filtered events exist even if there is no tool usage data.
	event1 := DifcFilteredEvent{
		Timestamp: "2024-01-12T10:00:00Z",
		ServerID:  "github",
		ToolName:  "pull_request_read",
		Reason:    "integrity check failed",
	}
	event2 := DifcFilteredEvent{
		Timestamp: "2024-01-12T10:00:01Z",
		ServerID:  "github",
		ToolName:  "issue_read",
		Reason:    "secrecy violation",
	}

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1},
			MCPToolUsage: &MCPToolUsageData{
				FilteredEvents: []DifcFilteredEvent{event1},
				Integrity: &IntegrityFilterSummary{
					TotalFiltered:        2,
					FilteredServerCounts: map[string]int{"github": 2},
					FilteredToolCounts:   map[string]int{"pull_request_read": 2},
					FilteredReasonCounts: map[string]int{"integrity check failed": 2},
				},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 2},
			MCPToolUsage: &MCPToolUsageData{
				FilteredEvents: []DifcFilteredEvent{event2},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 3},
			MCPToolUsage: &MCPToolUsageData{
				Integrity: &IntegrityFilterSummary{
					TotalFiltered:        2,
					FilteredServerCounts: map[string]int{"github": 1, "playwright": 1},
					FilteredToolCounts:   map[string]int{"issue_read": 1, "navigate": 1},
					FilteredReasonCounts: map[string]int{"integrity": 2},
				},
			},
		},
	}

	summary := buildMCPToolUsageSummary(processedRuns)

	// Should not be nil even though there is no tool data
	require.NotNil(t, summary, "summary should not be nil when filtered events exist")
	require.Len(t, summary.FilteredEvents, 2, "should aggregate filtered events from all runs")
	assert.Equal(t, event1, summary.FilteredEvents[0])
	assert.Equal(t, event2, summary.FilteredEvents[1])
	require.NotNil(t, summary.Integrity, "integrity aggregates should be included")
	assert.Equal(t, 4, summary.Integrity.TotalFiltered, "raw and compact integrity counts should be aggregated without duplicating a run")
	assert.Equal(t, 3, summary.Integrity.RunsWithFilteredEvents, "runs with filtered events should be counted")
	assert.Equal(t, map[string]int{"github": 3, "playwright": 1}, summary.Integrity.FilteredServerCounts)
	assert.Equal(t, map[string]int{"issue_read": 2, "navigate": 1, "pull_request_read": 1}, summary.Integrity.FilteredToolCounts)
	assert.Equal(t, map[string]int{"integrity": 2, "integrity check failed": 1, "secrecy violation": 1}, summary.Integrity.FilteredReasonCounts)
}
