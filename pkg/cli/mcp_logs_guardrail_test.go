//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLogsCollectionState_PreservesBothCursorShapes(t *testing.T) {
	output := `{"continuation":{"message":"Count limit reached.","before_run_id":42},"continuations":[{"repository":"github/gh-aw","message":"Timeout reached.","before_run_id":17}],"runs":[{"intentional_failure":true}]}`
	response := extractLogsCollectionState(output)
	require.NotNil(t, response.Continuation)
	assert.Equal(t, int64(42), response.Continuation.BeforeRunID)
	require.Len(t, response.Continuations, 1)
	assert.Equal(t, int64(17), response.Continuations[0].BeforeRunID)
	assert.True(t, response.Partial)
	assert.Empty(t, response.CoverageStatus)
}

func TestExtractLogsCollectionState_InvalidReport(t *testing.T) {
	for _, output := range []string{"", "diagnostic only in file", `{"continuation":`, "null", "[]", `"report"`, "42"} {
		t.Run(output, func(t *testing.T) {
			response := extractLogsCollectionState(output)
			assert.Equal(t, "unknown", response.CoverageStatus)
			assert.Equal(t, []string{"invalid_report_json"}, response.CoverageReasons)
			assert.False(t, response.Partial, "unknown collection state is not known partial evidence")
			assert.Nil(t, response.Continuation)
			assert.Empty(t, response.Continuations)
			assert.Contains(t, response.Message, "Collection state could not be fully decoded")
			assert.NotContains(t, response.Message, "diagnostic only in file")
		})
	}
}

func TestExtractLogsCollectionState_MalformedSectionIndependence(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		status  string
		partial bool
		reasons []string
		single  bool
		multi   bool
	}{
		{
			name:    "malformed coverage preserves single cursor",
			output:  `{"continuation":{"before_run_id":42},"episodes":[{"coverage":{"status":17}}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
			single:  true,
		},
		{
			name:    "malformed coverage preserves per target cursors",
			output:  `{"continuations":[{"repository":"github/gh-aw","before_run_id":17}],"episodes":[{"coverage":{"reasons":17}}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
			multi:   true,
		},
		{
			name:    "malformed coverage and retrieval preserve both cursor shapes",
			output:  `{"continuation":{"before_run_id":42},"continuations":[{"repository":"github/gh-aw","before_run_id":17}],"episodes":"invalid","episode_retrieval":"invalid"}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "invalid_episode_retrieval"},
			single:  true,
			multi:   true,
		},
		{
			name:    "malformed single cursor preserves per target cursors",
			output:  `{"continuation":{"before_run_id":"invalid"},"continuations":[{"repository":"github/gh-aw","before_run_id":17}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_continuation"},
			multi:   true,
		},
		{
			name:    "malformed per target cursors preserve single cursor",
			output:  `{"continuation":{"before_run_id":42},"continuations":"invalid"}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_continuations"},
			single:  true,
		},
		{
			name:    "malformed coverage alone is unknown not partial",
			output:  `{"episodes":[{"coverage":17}]}`,
			status:  "unknown",
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "malformed retrieval alone is unknown not partial",
			output:  `{"episode_retrieval":{"stop_reasons":17}}`,
			status:  "unknown",
			reasons: []string{"invalid_episode_retrieval"},
		},
		{
			name:    "known partial row overrides later malformed row",
			output:  `{"episodes":[{"coverage":{"status":"partial","reasons":["missing_recorded_parent"]}},{"coverage":17}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "missing_recorded_parent"},
		},
		{
			name:    "known partial row overrides earlier malformed row",
			output:  `{"episodes":[{"coverage":17},{"coverage":{"collection_truncated":true}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "valid status survives malformed reasons in same coverage",
			output:  `{"episodes":[{"coverage":{"status":"partial","reasons":17}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "valid truncation survives malformed status in same coverage",
			output:  `{"episodes":[{"coverage":{"status":17,"collection_truncated":true}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "retrieval stop overrides malformed coverage",
			output:  `{"episodes":[{"coverage":17}],"episode_retrieval":{"stop_reasons":["timeout"]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "retrieval_timeout"},
		},
		{
			name:    "coverage partial overrides malformed retrieval",
			output:  `{"episodes":[{"coverage":{"status":"partial"}}],"episode_retrieval":17}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval"},
		},
		{
			name:    "retrieval issue survives malformed stop reasons",
			output:  `{"episode_retrieval":{"stop_reasons":17,"issues":[{"reason":"discovery_failed","message":"diagnostic only in file"}]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_discovery_failed"},
		},
		{
			name:    "retrieval stop survives malformed issues",
			output:  `{"episode_retrieval":{"stop_reasons":["timeout"],"issues":17}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_timeout"},
		},
		{
			name:    "valid retrieval reasons survive invalid array entries",
			output:  `{"episode_retrieval":{"stop_reasons":[17,"timeout"],"issues":[17,{"reason":"discovery_failed"}]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_discovery_failed", "retrieval_timeout"},
		},
		{
			name:    "unrelated retrieval metadata does not erase stop reasons",
			output:  `{"episode_retrieval":{"start_date":"future format","stop_reasons":["timeout"]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"retrieval_timeout"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := extractLogsCollectionState(tt.output)
			assert.Equal(t, tt.status, response.CoverageStatus)
			assert.Equal(t, tt.partial, response.Partial)
			assert.Equal(t, tt.reasons, response.CoverageReasons)
			if tt.single {
				require.NotNil(t, response.Continuation)
				assert.Equal(t, int64(42), response.Continuation.BeforeRunID)
			} else {
				assert.Nil(t, response.Continuation)
			}
			if tt.multi {
				require.Len(t, response.Continuations, 1)
				assert.Equal(t, "github/gh-aw", response.Continuations[0].Repository)
				assert.Equal(t, int64(17), response.Continuations[0].BeforeRunID)
			} else {
				assert.Empty(t, response.Continuations)
			}
			if len(tt.reasons) > 0 && strings.HasPrefix(tt.reasons[0], "invalid_") {
				assert.Contains(t, response.Message, "Collection state could not be fully decoded")
			}
			assert.NotContains(t, response.Message, "diagnostic only in file")
		})
	}
}
