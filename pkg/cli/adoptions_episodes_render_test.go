//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type episodeAdoptionsErrorWriter struct{}

func (episodeAdoptionsErrorWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestRenderEpisodeAdoptions(t *testing.T) {
	t.Parallel()
	partial := episodeCost(2.5)
	partial.warnings = []string{"unpriced usage"}
	for _, tc := range []struct {
		name  string
		cost  episodeAdoptionCost
		value string
		ratio string
	}{
		{"available", episodeCost(2.5), "2.500", "AIC per accepted adoption (selected runs): 2.500"},
		{"explicit zero", episodeCost(0), "0.000", "AIC per accepted adoption (selected runs): 0.000"},
		{"unavailable", episodeAdoptionCost{}, "unavailable", "AIC per accepted adoption: unavailable"},
		{"partial", partial, "2.500", "AIC per accepted adoption: unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := buildEpisodeAdoptions(episodeLogs(1),
				[]EpisodeAdoptionObservation{episodeObservation(1, "item-1", AdoptionStatusAccepted), episodeObservation(2, "item-2", AdoptionStatusAccepted)},
				map[int64]episodeAdoptionCost{1: tc.cost})
			require.NoError(t, err)
			var output bytes.Buffer
			require.NoError(t, renderEpisodeAdoptions(&output, data))
			assert.Contains(t, output.String(), "Observed adoptions: 1 accepted")
			assert.Contains(t, output.String(), "Observed AIC: "+tc.value)
			assert.Contains(t, output.String(), tc.ratio)
			assert.Contains(t, output.String(), "full-episode membership is not verified")
			assert.Contains(t, output.String(), "Adoption source runs outside selected membership (not counted): [2]")
			require.ErrorIs(t, renderEpisodeAdoptions(episodeAdoptionsErrorWriter{}, data), io.ErrClosedPipe)
		})
	}
}

func TestEpisodeAdoptionCostAvailability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		content   string
		available bool
	}{
		{"unpriced tokens", `{"model":"unpriced","input_tokens":10}`, false},
		{"explicit zero", `{"ai_credits":0}`, true},
		{"positive cost", `{"ai_credits":2.5}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "agent_usage.json"), []byte(tc.content), 0o600))
			usage, err := analyzeTokenUsageAICOnly(dir, false)
			require.NoError(t, err)
			require.NotNil(t, usage)
			assert.Equal(t, tc.available, usage.AICFound)
		})
	}
}

func TestEpisodeAdoptionsOptionalCoverageInput(t *testing.T) {
	t.Parallel()
	var logs episodeAdoptionsLogs
	require.NoError(t, json.Unmarshal([]byte(`{
		"runs":[{"run_id":1}],
		"episodes":[{
			"episode_id":"ep-1","run_ids":[1],"total_runs":1,
			"recorded_roots":[{"run_id":"99","observed":false}],
			"coverage":{
				"scope":"selected_runs","status":"partial",
				"reasons":["missing_parent"],"missing_parent_run_ids":[99],"collection_truncated":true
			}
		}]
	}`), &logs))
	data, err := buildEpisodeAdoptions(logs, []EpisodeAdoptionObservation{episodeObservation(1, "item-1", AdoptionStatusAccepted)},
		map[int64]episodeAdoptionCost{1: episodeCost(2)})
	require.NoError(t, err)
	row := data.Episodes[0]
	require.NotNil(t, row.MembershipCoverage)
	assert.Equal(t, "partial", row.MembershipCoverage.Status)
	assert.True(t, row.MembershipCoverage.CollectionTruncated)
	assert.Contains(t, row.Qualifications, "Membership coverage: missing_parent")
	assert.Nil(t, row.CostPerAcceptedAdoption)
}
