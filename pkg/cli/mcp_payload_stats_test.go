//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMCPPayloadStatsRoundsPayloadAverages(t *testing.T) {
	t.Parallel()

	usage := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{
				ServerName:      "github",
				ToolName:        "search_issues",
				CallCount:       2,
				TotalInputSize:  31,
				TotalOutputSize: 75,
				MaxInputSize:    20,
				MaxOutputSize:   50,
			},
		},
		Servers: []MCPServerStats{
			{
				MCPServerStatsBase: MCPServerStatsBase{
					ServerName:    "github",
					ToolCallCount: 2,
				},
				TotalInputSize:  31,
				TotalOutputSize: 75,
			},
		},
	}

	normalized := normalizeMCPPayloadStats(usage)

	require.NotNil(t, normalized)
	require.Len(t, normalized.Summary, 1)
	require.Len(t, normalized.Servers, 1)
	assert.Equal(t, 16, normalized.Summary[0].AvgInputSize)
	assert.Equal(t, 38, normalized.Summary[0].AvgOutputSize)
	assert.Equal(t, 16, normalized.Servers[0].AvgInputSize)
	assert.Equal(t, 38, normalized.Servers[0].AvgOutputSize)
	assert.Equal(t, 20, normalized.Servers[0].MaxInputSize)
	assert.Equal(t, 50, normalized.Servers[0].MaxOutputSize)
}

func TestNormalizeMCPPayloadStatsHandlesNilUsage(t *testing.T) {
	t.Parallel()

	assert.Nil(t, normalizeMCPPayloadStats(nil))
}

func TestNormalizeMCPPayloadStatsLeavesZeroCountAveragesAtZero(t *testing.T) {
	t.Parallel()

	usage := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{
				ServerName:      "github",
				ToolName:        "search_issues",
				TotalInputSize:  31,
				TotalOutputSize: 75,
			},
		},
		Servers: []MCPServerStats{
			{
				MCPServerStatsBase: MCPServerStatsBase{ServerName: "github"},
				TotalInputSize:     31,
				TotalOutputSize:    75,
			},
		},
	}

	normalized := normalizeMCPPayloadStats(usage)

	require.NotNil(t, normalized)
	require.Len(t, normalized.Summary, 1)
	require.Len(t, normalized.Servers, 1)
	assert.Zero(t, normalized.Summary[0].AvgInputSize)
	assert.Zero(t, normalized.Summary[0].AvgOutputSize)
	assert.Zero(t, normalized.Servers[0].AvgInputSize)
	assert.Zero(t, normalized.Servers[0].AvgOutputSize)
}

func TestRoundPayloadAverage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 16, roundPayloadAverage(31, 2))
	assert.Equal(t, 38, roundPayloadAverage(75, 2))
	assert.Zero(t, roundPayloadAverage(31, 0))
	assert.Zero(t, roundPayloadAverage(31, -1))
}
