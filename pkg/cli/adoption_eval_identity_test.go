//go:build !integration

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateAdoptionsStableActionIdentity(t *testing.T) {
	const actionType = "identity_test_action"
	adoptionEvaluators[actionType] = func(_ context.Context, item CreatedItemReport, repo string) AdoptionReport {
		return AdoptionReport{
			Type: item.Type, Repo: repo, ObjectNumber: item.Number,
			AdoptionEvaluation: adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "accepted"),
		}
	}
	t.Cleanup(func() { delete(adoptionEvaluators, actionType) })
	items := []CreatedItemReport{
		{Type: actionType, Number: 1, Timestamp: "2026-10-01T01:00:00Z", Metadata: map[string]any{"change": "first"}},
		{Type: actionType, Number: 1, Timestamp: "2026-10-01T01:00:00Z", Metadata: map[string]any{"change": "second"}},
		{Type: actionType, Number: 2, Timestamp: "2026-10-01T01:00:00Z"},
	}
	first := EvaluateAdoptions(context.Background(), items, "acme/repo", nil)
	require.Len(t, first, 3)
	for i, report := range first {
		assert.NotEmpty(t, report.ItemID)
		for _, other := range first[i+1:] {
			assert.NotEqual(t, report.ItemID, other.ItemID, "same-target actions remain distinct")
		}
	}
	source, err := json.Marshal(items)
	require.NoError(t, err)
	var restored []CreatedItemReport
	require.NoError(t, json.Unmarshal(source, &restored))
	slices.Reverse(restored)
	restored = append([]CreatedItemReport{{Type: "noop"}}, restored...)
	second := EvaluateAdoptions(context.Background(), restored, "acme/repo", nil)
	require.Len(t, second, 3)
	observations := make([]EpisodeAdoptionObservation, 0, 6)
	for i := range first {
		assert.Equal(t, first[i].ItemID, second[len(second)-1-i].ItemID)
		observations = append(observations, EpisodeAdoptionObservation{RunID: 1, RunItemCount: 3, AdoptionReport: first[i]})
		observations = append(observations, EpisodeAdoptionObservation{RunID: 1, RunItemCount: 3, AdoptionReport: second[i]})
	}
	data, err := buildEpisodeAdoptions(episodeLogs(1), observations, map[int64]episodeAdoptionCost{1: episodeCost(6)})
	require.NoError(t, err)
	assert.Equal(t, 3, data.Episodes[0].Summary.Total)
	assert.Equal(t, 3, data.Episodes[0].Summary.Accepted)
	assert.Equal(t, "complete", data.Episodes[0].AdoptionCoverage)
	require.NotNil(t, data.Episodes[0].CostPerAcceptedAdoption)
	assert.InDelta(t, 2.0, *data.Episodes[0].CostPerAcceptedAdoption, 1e-9)

	filtered := EvaluateAdoptions(context.Background(), items[1:2], "acme/repo", nil)
	require.Len(t, filtered, 1)
	assert.Equal(t, first[1].ItemID, filtered[0].ItemID, "filtering unrelated actions cannot change identity")
}

func TestAdoptionItemIDIndistinguishableActions(t *testing.T) {
	t.Parallel()
	item := CreatedItemReport{Type: "add_comment", Number: 1, Timestamp: "2026-10-01T01:00:00Z"}
	occurrences := make(map[[sha256.Size]byte]int)
	first := adoptionItemID(item, occurrences)
	second := adoptionItemID(item, occurrences)
	assert.NotEqual(t, first, second, "identical manifest entries still represent distinct actions")
	again := make(map[[sha256.Size]byte]int)
	assert.Equal(t, first, adoptionItemID(item, again))
	assert.Equal(t, second, adoptionItemID(item, again))
}

func TestAdoptionItemIDCanonicalSourceEncoding(t *testing.T) {
	t.Parallel()
	first := CreatedItemReport{
		Type: "update_issue", Number: 1, Timestamp: "2026-10-01T01:00:00Z",
		Metadata: map[string]any{"z": float64(1), "a": map[string]any{"title": "updated", "number": float64(2)}},
	}
	second := first
	second.Metadata = map[string]any{"a": map[string]any{"number": 2, "title": "updated"}, "z": 1}
	assert.Equal(t, adoptionItemID(first, make(map[[sha256.Size]byte]int)),
		adoptionItemID(second, make(map[[sha256.Size]byte]int)))
	second.Metadata = map[string]any{"invalid": make(chan int)}
	assert.Empty(t, adoptionItemID(second, make(map[[sha256.Size]byte]int)),
		"unrepresentable evidence must not receive a misleading stable identity")
}
