//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAuditDataIncludesAmbientContext(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "test",
			Status:       "completed",
			Conclusion:   "success",
			CreatedAt:    time.Now(),
		},
		TokenUsage: &TokenUsageSummary{
			AmbientContext: &AmbientContextMetrics{
				InputTokens:  1200,
				CachedTokens: 300,
			},
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, workflow.LogMetrics{}, nil)
	require.NotNil(t, auditData.Metrics.AmbientContext, "ambient context should be populated")
	assert.Equal(t, 1200, auditData.Metrics.AmbientContext.InputTokens, "input tokens should match")
	assert.Equal(t, 300, auditData.Metrics.AmbientContext.CachedTokens, "cached tokens should match")
}

func TestBuildAuditDataIncludesWorkingSet(t *testing.T) {
	t.Parallel()
	factor := 3.9017857142857144
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "test",
			Status:       "completed",
			Conclusion:   "success",
			CreatedAt:    time.Now(),
		},
		WorkingSet: &WorkingSetMetrics{
			MeasurementState:      "measured",
			RebuildFactor:         &factor,
			CumulativeInputTokens: 874000,
			PeakInputTokens:       224000,
			RebuildExcessTokens:   650000,
			Invocations:           5,
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, workflow.LogMetrics{}, nil)
	require.NotNil(t, auditData.Metrics.WorkingSet)
	assert.Same(t, processedRun.WorkingSet, auditData.Metrics.WorkingSet)

	encoded, err := json.Marshal(auditData)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"working_set":{"measurement_state":"measured","rebuild_factor":3.9017857142857144`)
}
