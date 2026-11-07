//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackfillRunTokenUsageFromFirewall(t *testing.T) {
	t.Parallel()
	t.Run("backfills run and metrics token usage from firewall summary", func(t *testing.T) {
		t.Parallel()
		metrics := LogMetrics{}
		result := DownloadResult{}
		tokenUsage := &TokenUsageSummary{
			TotalInputTokens:  2000,
			TotalOutputTokens: 1000,
		}

		backfillRunTokenUsageFromFirewall(&metrics, &result, tokenUsage)

		assert.Equal(t, 3000, metrics.TokenUsage)
		assert.Equal(t, 3000, result.Metrics.TokenUsage)
		assert.Equal(t, 3000, result.Run.TokenUsage)
	})

	t.Run("does not overwrite non-zero event token usage", func(t *testing.T) {
		t.Parallel()
		metrics := LogMetrics{TokenUsage: 123}
		result := DownloadResult{RunAnalysis: RunAnalysis{
			Run:     WorkflowRun{TokenUsage: 123},
			Metrics: LogMetrics{TokenUsage: 123},
		},
		}
		tokenUsage := &TokenUsageSummary{
			TotalInputTokens:  2000,
			TotalOutputTokens: 1000,
		}

		backfillRunTokenUsageFromFirewall(&metrics, &result, tokenUsage)

		assert.Equal(t, 123, metrics.TokenUsage)
		assert.Equal(t, 123, result.Metrics.TokenUsage)
		assert.Equal(t, 123, result.Run.TokenUsage)
	})
}

func TestNewRunSummaryCarriesGatewaySteeringEvents(t *testing.T) {
	t.Parallel()
	events := []GatewaySteeringEvent{{
		Type:      tokenSteeringEventName,
		Message:   "[AWF TOKEN WARNING] You are running out of AI Credits.",
		Timestamp: "2026-09-23T12:00:00Z",
	}}
	result := &DownloadResult{RunAnalysis: RunAnalysis{
		Run:                   WorkflowRun{DatabaseID: 300},
		GatewaySteeringEvents: events,
	}}

	summary := newRunSummary(result, LogMetrics{}, nil, nil)

	assert.Equal(t, events, summary.GatewaySteeringEvents)
}
