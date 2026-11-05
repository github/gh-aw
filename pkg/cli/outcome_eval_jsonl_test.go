//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcomeJSONLObjectiveRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outcomesDir := filepath.Join(dir, "outcomes")
	reports := []OutcomeReport{}
	for i, objective := range []struct {
		status OutcomeStatus
		value  int
		labels []string
	}{
		{OutcomeStatusAccepted, 30, []string{"security", "automation"}},
		{OutcomeStatusRejected, 70, []string{"security"}},
		{OutcomeStatusPending, 0, []string{"documentation"}},
		{OutcomeStatusAccepted, 0, nil},
	} {
		report := episodeObservation(42, "item-"+strconv.Itoa(i+1), objective.status).OutcomeReport
		report.ObjectNumber = i + 1
		report.ObjectiveValue = objective.value
		report.ObjectiveLabels = objective.labels
		report.ZeroTouchEvidenceComplete = i%2 == 0
		reports = append(reports, report)
	}
	writeOutcomeJSONL(outcomesDir, 42, reports)
	content, err := os.ReadFile(filepath.Join(outcomesDir, "outcomes-42.jsonl"))
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(content))
	for i := range reports {
		var observation EpisodeOutcomeObservation
		require.NoError(t, decoder.Decode(&observation))
		assert.Equal(t, reports[i], observation.OutcomeReport)
		assert.Equal(t, int64(42), observation.RunID)
		assert.Equal(t, len(reports), observation.RunItemCount)
	}
	var extra EpisodeOutcomeObservation
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF)

	usageDir := filepath.Join(dir, "run-42", "usage")
	require.NoError(t, os.MkdirAll(usageDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.jsonl"), []byte("{\"aic\":1.25}\n"), 0o600))

	logs := episodeLogs(42)
	logs.Runs[0].LogsPath = filepath.Dir(usageDir)
	data := LogsData{Runs: logs.Runs, Episodes: []EpisodeData{logs.Episodes[0].EpisodeData}}
	require.NoError(t, attachLogsOutcomes(&data, []OutcomesData{{RunID: 42, Items: reports}}))
	require.NotNil(t, data.EpisodeOutcomes)
	require.Len(t, data.EpisodeOutcomes.Episodes, 1)
	episode := data.EpisodeOutcomes.Episodes[0]
	assert.Equal(t, "selected_runs", episode.MembershipScope)
	assert.Equal(t, "complete", episode.OutcomeCoverage)
	assert.Equal(t, 4, episode.Summary.Total)
	assert.Equal(t, 2, episode.Summary.Accepted)
	assert.Equal(t, 100, episode.Summary.TotalObjectiveValue)
	assert.Equal(t, 30, episode.Summary.AcceptedObjectiveValue)
	assert.InDelta(t, 0.3, episode.Summary.ObjectiveEfficiency, 1e-9)
	assert.Equal(t, ComputeOutcomeSummary(reports, nil).DomainBreakdowns, episode.Summary.DomainBreakdowns)
	assert.Equal(t, []DomainBreakdown{
		{Label: "security", Attempted: 2, Accepted: 1, Rejected: 1, TotalObjectiveValue: 100, AcceptedObjectiveValue: 30, ObjectiveEfficiency: 0.3, AcceptanceRate: 0.5},
		{Label: "automation", Attempted: 1, Accepted: 1, TotalObjectiveValue: 30, AcceptedObjectiveValue: 30, ObjectiveEfficiency: 1, AcceptanceRate: 1},
		{Label: "documentation", Attempted: 1, Pending: 1},
		{Label: "unmapped", Attempted: 1, Accepted: 1, AcceptanceRate: 1},
	}, episode.Summary.DomainBreakdowns)
	require.NotNil(t, episode.CostPerAcceptedOutcome)
	assert.InDelta(t, 0.625, *episode.CostPerAcceptedOutcome, 1e-9)
	require.Len(t, episode.Items, len(reports))
	for i, item := range episode.Items {
		assert.Equal(t, reports[i].ObjectiveValue, item.ObjectiveValue)
		assert.Equal(t, reports[i].ObjectiveLabels, item.ObjectiveLabels)
	}

	schemaJSON, err := GenerateNamedOutputSchema("logs")
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	var value any
	content, err = json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &value))
	require.NoError(t, resolved.Validate(value))
}
