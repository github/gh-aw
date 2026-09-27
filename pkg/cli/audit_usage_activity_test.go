//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyUsageSummaryToAuditResultsBackfillsMCPPayloadMetrics(t *testing.T) {
	t.Parallel()

	results := auditAnalysisResults{}
	applyUsageSummaryToAuditResults(&usageActivitySummary{
		Gateway: &usageActivityGateway{
			Tools: []usageActivityGatewayTool{{
				ServerName:      "github",
				ToolName:        "issue_read",
				CallCount:       2,
				TotalInputSize:  300,
				TotalOutputSize: 900,
				AvgInputSize:    150,
				AvgOutputSize:   450,
				MaxInputSize:    200,
				MaxOutputSize:   600,
			}},
			Servers: []usageActivityGatewayServer{{
				ServerName:      "github",
				ToolCallCount:   2,
				TotalInputSize:  300,
				TotalOutputSize: 900,
				AvgInputSize:    150,
				AvgOutputSize:   450,
				MaxInputSize:    200,
				MaxOutputSize:   600,
			}},
		},
	}, &results)

	require.NotNil(t, results.mcpToolUsage)
	require.Len(t, results.mcpToolUsage.Summary, 1)
	assert.Equal(t, 150, results.mcpToolUsage.Summary[0].AvgInputSize)
	assert.Equal(t, 450, results.mcpToolUsage.Summary[0].AvgOutputSize)
	assert.Equal(t, 600, results.mcpToolUsage.Summary[0].MaxOutputSize)
	require.Len(t, results.mcpToolUsage.Servers, 1)
	assert.Equal(t, 200, results.mcpToolUsage.Servers[0].MaxInputSize)
}
