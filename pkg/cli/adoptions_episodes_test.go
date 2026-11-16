//go:build !integration

package cli

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func episodeObservation(runID int64, itemID string, status AdoptionStatus) EpisodeAdoptionObservation {
	if itemID != "" {
		itemID = "test-action-" + itemID
	}
	return EpisodeAdoptionObservation{
		RunID: runID, RunItemCount: 1,
		AdoptionReport: AdoptionReport{
			ItemID: itemID, Type: "create_issue", Repo: "acme/repo", ObjectNumber: 10,
			CreatedAt: "2026-10-01T01:00:00Z", CheckedAt: "2026-10-02T01:00:00Z",
			AdoptionEvaluation: adoptionEvidence(status, EvidenceStrong, string(status)),
		},
	}
}

func TestLatestEpisodeAdoptionsConflictingFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*EpisodeAdoptionObservation)
	}{
		{"status", func(item *EpisodeAdoptionObservation) { item.AdoptionStatus = AdoptionStatusRejected }},
		{"strength", func(item *EpisodeAdoptionObservation) { item.EvidenceStrength = EvidenceWeak }},
		{"signal", func(item *EpisodeAdoptionObservation) { item.Signal = "target_exists_only" }},
		{"zero touch", func(item *EpisodeAdoptionObservation) { item.ZeroTouch = true }},
		{"zero touch evidence", func(item *EpisodeAdoptionObservation) { item.ZeroTouchEvidenceComplete = true }},
		{"latency", func(item *EpisodeAdoptionObservation) { item.TimeToAdoptionHours = 1 }},
		{"objective value", func(item *EpisodeAdoptionObservation) { item.ObjectiveValue = 10 }},
		{"objective labels", func(item *EpisodeAdoptionObservation) { item.ObjectiveLabels = []string{"security"} }},
		{"item count", func(item *EpisodeAdoptionObservation) { item.RunItemCount++ }},
		{"target", func(item *EpisodeAdoptionObservation) { item.ObjectNumber++ }},
		{"type", func(item *EpisodeAdoptionObservation) { item.Type = "update_issue" }},
		{"error", func(item *EpisodeAdoptionObservation) { item.EvalError = "missing evidence" }},
		{"detail", func(item *EpisodeAdoptionObservation) { item.Detail = "object still exists" }},
		{"attribution", func(item *EpisodeAdoptionObservation) { item.AttributionStatus = "unmapped" }},
		{"comments", func(item *EpisodeAdoptionObservation) { item.HumanComments++ }},
		{"edits", func(item *EpisodeAdoptionObservation) { item.HumanEdits++ }},
		{"reviews", func(item *EpisodeAdoptionObservation) { item.HumanReviews++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, legacy := range []bool{false, true} {
				if legacy && (tc.name == "target" || tc.name == "type") {
					continue
				}
				first := episodeObservation(1, "item-1", AdoptionStatusAccepted)
				if legacy {
					first.ItemID = ""
				}
				conflict := first
				tc.mutate(&conflict)
				newer := first
				newer.CheckedAt = "2026-10-03T01:00:00Z"
				for _, superseded := range []bool{false, true} {
					third := first
					if superseded {
						third = newer
					}
					items := []EpisodeAdoptionObservation{first, conflict, third}
					for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
						input := []EpisodeAdoptionObservation{items[order[0]], items[order[1]], items[order[2]]}
						got, err := latestEpisodeAdoptions(input)
						if superseded {
							require.NoError(t, err)
							assert.Equal(t, []EpisodeAdoptionObservation{newer}, got)
						} else {
							require.ErrorContains(t, err, "conflicting adoptions")
						}
					}
				}
			}
		})
	}
}

func TestLatestEpisodeAdoptionsEquivalentTimestamps(t *testing.T) {
	t.Parallel()
	first := episodeObservation(1, "item-1", AdoptionStatusAccepted)
	offset := first
	offset.CheckedAt = "2026-10-02T03:00:00+02:00"
	for _, input := range [][]EpisodeAdoptionObservation{{first, offset}, {offset, first}} {
		got, err := latestEpisodeAdoptions(input)
		require.NoError(t, err)
		assert.Equal(t, []EpisodeAdoptionObservation{first}, got)
	}
}

func TestEpisodeAdoptionsPositionalIdentityMigration(t *testing.T) {
	t.Parallel()
	first := episodeObservation(1, "first", AdoptionStatusAccepted)
	second := episodeObservation(1, "second", AdoptionStatusAccepted)
	first.RunItemCount, second.RunItemCount = 2, 2
	older := first
	older.ItemID = "item-1"
	older.CheckedAt = "2026-10-01T01:00:00Z"
	for _, input := range [][]EpisodeAdoptionObservation{{older, first, second}, {second, first, older}} {
		data, err := buildEpisodeAdoptions(episodeLogs(1), input, map[int64]episodeAdoptionCost{1: episodeCost(6)})
		require.NoError(t, err)
		assert.Equal(t, 2, data.Episodes[0].Summary.Accepted)
		assert.Equal(t, "complete", data.Episodes[0].AdoptionCoverage)
		require.NotNil(t, data.Episodes[0].CostPerAcceptedAdoption)
		assert.InDelta(t, 3.0, *data.Episodes[0].CostPerAcceptedAdoption, 1e-9)
	}
	data, err := buildEpisodeAdoptions(episodeLogs(1), []EpisodeAdoptionObservation{older}, map[int64]episodeAdoptionCost{1: episodeCost(6)})
	require.NoError(t, err)
	assert.Equal(t, "partial", data.Episodes[0].AdoptionCoverage)
	assert.Nil(t, data.Episodes[0].CostPerAcceptedAdoption)

	legacy := older
	legacy.ItemID = ""
	another := older
	another.ItemID = "item-2"
	for _, input := range [][]EpisodeAdoptionObservation{{legacy, older, another}, {another, older, legacy}} {
		data, err = buildEpisodeAdoptions(episodeLogs(1), input, map[int64]episodeAdoptionCost{1: episodeCost(6)})
		require.NoError(t, err)
		assert.Equal(t, 2, data.Episodes[0].Summary.Total, "positional IDs retain distinct actions without counting missing-ID aliases")
		assert.Equal(t, "partial", data.Episodes[0].AdoptionCoverage)
		assert.Nil(t, data.Episodes[0].CostPerAcceptedAdoption)
	}

	older.CheckedAt = "2026-10-03T01:00:00Z"
	_, err = latestEpisodeAdoptions([]EpisodeAdoptionObservation{first, older, second})
	require.ErrorContains(t, err, "newer than precise item observations")
}

func TestEpisodeAdoptionsConflictingPositionalTargets(t *testing.T) {
	t.Parallel()
	first := episodeObservation(1, "first", AdoptionStatusAccepted)
	second := episodeObservation(1, "second", AdoptionStatusAccepted)
	second.ObjectNumber++
	older := first
	older.ItemID = "item-1"
	older.CheckedAt = "2026-10-01T01:00:00Z"
	conflict := older
	conflict.ObjectNumber = second.ObjectNumber
	for _, input := range [][]EpisodeAdoptionObservation{
		{older, conflict, first},
		{first, conflict, older},
		{conflict, first, older},
	} {
		_, err := latestEpisodeAdoptions(input)
		require.ErrorContains(t, err, "conflicting adoptions", "one matching target cannot hide an ambiguous positional alias")
		input = append(input, second)
		got, err := latestEpisodeAdoptions(input)
		require.NoError(t, err)
		assert.Equal(t, []EpisodeAdoptionObservation{first, second}, got, "all alternative targets must be superseded")
	}
}

func episodeLogs(ids ...int64) episodeAdoptionsLogs {
	logs := episodeAdoptionsLogs{Episodes: []episodeAdoptionMembership{{EpisodeData: EpisodeData{EpisodeID: "ep-1", RunIDs: ids, TotalRuns: len(ids)}}}}
	for _, id := range ids {
		logs.Runs = append(logs.Runs, RunData{RunID: id})
	}
	return logs
}

func episodeCost(aic float64) episodeAdoptionCost {
	return episodeAdoptionCost{aic: &aic}
}

func TestBuildEpisodeAdoptionsLatestAcrossRuns(t *testing.T) {
	t.Parallel()
	first := episodeObservation(1, "item-1", AdoptionStatusPending)
	accepted := episodeObservation(1, "item-1", AdoptionStatusAccepted)
	accepted.CheckedAt = "2026-10-03T01:00:00Z"
	acceptedSecond := episodeObservation(2, "item-1", AdoptionStatusAccepted)
	observations := []EpisodeAdoptionObservation{accepted, acceptedSecond, first, accepted}
	costs := map[int64]episodeAdoptionCost{1: episodeCost(3), 2: episodeCost(5)}
	data, err := buildEpisodeAdoptions(episodeLogs(2, 1), observations, costs)
	require.NoError(t, err)
	require.Len(t, data.Episodes, 1)
	row := data.Episodes[0]
	assert.Equal(t, []int64{1, 2}, row.RunIDs)
	assert.Equal(t, "selected_runs", row.MembershipScope)
	assert.Equal(t, "complete", row.AdoptionCoverage)
	assert.Equal(t, "available", row.CostCoverage)
	assert.Equal(t, 2, row.Summary.Total)
	assert.Equal(t, 2, row.Summary.Accepted)
	assert.Equal(t, []int64{1, 2}, row.AdoptionRunIDs)
	require.NotNil(t, row.ObservedAIC)
	require.NotNil(t, row.CostPerAcceptedAdoption)
	assert.InDelta(t, 8.0, *row.ObservedAIC, 1e-9)
	assert.InDelta(t, 4.0, *row.CostPerAcceptedAdoption, 1e-9)
	assert.Contains(t, row.Qualifications[0], "full-episode membership is not verified")

	slices.Reverse(observations)
	reversed, err := buildEpisodeAdoptions(episodeLogs(2, 1), observations, costs)
	require.NoError(t, err)
	assert.Equal(t, data, reversed)
}

func TestBuildEpisodeAdoptionsCostAndEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		costs           map[int64]episodeAdoptionCost
		status          AdoptionStatus
		missingEvidence bool
		costCoverage    string
		observed        *float64
		ratio           *float64
	}{
		{name: "zero is observed cost", costs: map[int64]episodeAdoptionCost{1: episodeCost(0)}, status: AdoptionStatusAccepted, costCoverage: "available", observed: new(float64), ratio: new(float64)},
		{name: "unavailable is not zero", status: AdoptionStatusAccepted, costCoverage: "unavailable"},
		{name: "negative cost", costs: map[int64]episodeAdoptionCost{1: episodeCost(-1)}, status: AdoptionStatusAccepted, costCoverage: "unavailable"},
		{name: "nonfinite cost", costs: map[int64]episodeAdoptionCost{1: episodeCost(math.Inf(1))}, status: AdoptionStatusAccepted, costCoverage: "unavailable"},
		{name: "nan cost", costs: map[int64]episodeAdoptionCost{1: episodeCost(math.NaN())}, status: AdoptionStatusAccepted, costCoverage: "unavailable"},
		{name: "no accepted", costs: map[int64]episodeAdoptionCost{1: episodeCost(2)}, status: AdoptionStatusRejected, costCoverage: "available", observed: episodeCost(2).aic},
		{name: "pending", costs: map[int64]episodeAdoptionCost{1: episodeCost(2)}, status: AdoptionStatusPending, costCoverage: "available", observed: episodeCost(2).aic},
		{name: "unknown", costs: map[int64]episodeAdoptionCost{1: episodeCost(2)}, status: AdoptionStatusUnknown, costCoverage: "available", observed: episodeCost(2).aic},
		{name: "error", costs: map[int64]episodeAdoptionCost{1: episodeCost(2)}, status: AdoptionStatusUnknown, costCoverage: "available", observed: episodeCost(2).aic},
		{name: "no observations", costs: map[int64]episodeAdoptionCost{1: episodeCost(2)}, missingEvidence: true, costCoverage: "available", observed: episodeCost(2).aic},
		{name: "usage warnings", costs: map[int64]episodeAdoptionCost{1: {aic: episodeCost(2).aic, warnings: []string{"unpriced usage"}}}, status: AdoptionStatusAccepted, costCoverage: "partial", observed: episodeCost(2).aic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []EpisodeAdoptionObservation{episodeObservation(1, "item-1", tc.status)}
			if tc.missingEvidence {
				input = nil
			}
			data, err := buildEpisodeAdoptions(episodeLogs(1), input, tc.costs)
			require.NoError(t, err)
			row := data.Episodes[0]
			assert.Equal(t, tc.costCoverage, row.CostCoverage)
			assert.Equal(t, tc.observed, row.ObservedAIC)
			assert.Equal(t, tc.ratio, row.CostPerAcceptedAdoption)
			if tc.missingEvidence {
				assert.Equal(t, []int64{1}, row.MissingAdoptionRunIDs)
				assert.Equal(t, "partial", row.AdoptionCoverage)
			}
			if tc.status == AdoptionStatusPending || tc.status == AdoptionStatusUnknown {
				assert.Contains(t, row.Qualifications, "Pending, unknown, and error adoptions remain unresolved; accepted counts are an observation snapshot.")
			}
		})
	}
}

func TestBuildEpisodeAdoptionsPartialCoverage(t *testing.T) {
	t.Parallel()
	observations := []EpisodeAdoptionObservation{episodeObservation(1, "item-1", AdoptionStatusAccepted)}
	costs := map[int64]episodeAdoptionCost{1: episodeCost(2)}
	data, err := buildEpisodeAdoptions(episodeLogs(1, 2), observations, costs)
	require.NoError(t, err)
	row := data.Episodes[0]
	assert.Equal(t, 1, row.Summary.Accepted)
	assert.Equal(t, "partial", row.CostCoverage)
	assert.Equal(t, "partial", row.AdoptionCoverage)
	assert.Equal(t, []int64{2}, row.MissingAdoptionRunIDs)
	assert.Equal(t, []int64{2}, row.MissingCostRunIDs)
	assert.InDelta(t, 2.0, *row.ObservedAIC, 1e-9)
	assert.Nil(t, row.CostPerAcceptedAdoption)

	logs := episodeLogs(1, 2)
	logs.Runs = logs.Runs[:1]
	costs[2] = episodeCost(3)
	observations = append(observations, episodeObservation(2, "item-1", AdoptionStatusAccepted))
	data, err = buildEpisodeAdoptions(logs, observations, costs)
	require.NoError(t, err)
	assert.Equal(t, []int64{2}, data.Episodes[0].MissingRunIDs)
	assert.Nil(t, data.Episodes[0].CostPerAcceptedAdoption)

	logs = episodeLogs(1)
	logs.Episodes[0].TotalRuns = 2
	data, err = buildEpisodeAdoptions(logs, observations, costs)
	require.NoError(t, err)
	assert.Nil(t, data.Episodes[0].CostPerAcceptedAdoption)
	assert.Equal(t, []int64{2}, data.UnmatchedAdoptionRunIDs)
}

func TestBuildEpisodeAdoptionsItemIdentity(t *testing.T) {
	t.Parallel()
	accepted := episodeObservation(1, "item-1", AdoptionStatusAccepted)
	second := episodeObservation(1, "item-2", AdoptionStatusPending)
	accepted.RunItemCount, second.RunItemCount = 2, 2
	observations := []EpisodeAdoptionObservation{accepted, second}
	data, err := buildEpisodeAdoptions(episodeLogs(1), observations, map[int64]episodeAdoptionCost{1: episodeCost(6)})
	require.NoError(t, err)
	assert.Equal(t, 2, data.Episodes[0].Summary.Total, "different actions on the same target remain distinct")
	assert.Equal(t, 1, data.Episodes[0].Summary.Accepted)
	assert.Equal(t, 1, data.Episodes[0].Summary.Pending)
	assert.InDelta(t, 6.0, *data.Episodes[0].CostPerAcceptedAdoption, 1e-9)

	legacy := accepted
	legacy.ItemID = ""
	legacy.CheckedAt = "2026-10-01T01:00:00Z"
	second.AdoptionStatus = AdoptionStatusAccepted
	observations = []EpisodeAdoptionObservation{accepted, second, legacy, legacy}
	data, err = buildEpisodeAdoptions(episodeLogs(1), observations, map[int64]episodeAdoptionCost{1: episodeCost(6)})
	require.NoError(t, err)
	assert.Equal(t, 2, data.Episodes[0].Summary.Accepted, "stale legacy observations neither merge precise items nor add items")
	assert.Equal(t, "complete", data.Episodes[0].AdoptionCoverage)
	require.NotNil(t, data.Episodes[0].CostPerAcceptedAdoption)
	assert.InDelta(t, 3.0, *data.Episodes[0].CostPerAcceptedAdoption, 1e-9)
}

func TestLatestEpisodeAdoptionsIdentitySnapshots(t *testing.T) {
	t.Parallel()
	snapshot := func(itemID string, status AdoptionStatus, checkedAt string) EpisodeAdoptionObservation {
		item := episodeObservation(1, itemID, status)
		item.CheckedAt = checkedAt
		item.RunItemCount = 2
		return item
	}
	const (
		oldTime    = "2026-10-01T01:00:00Z"
		modernTime = "2026-10-02T01:00:00Z"
		legacyTime = "2026-10-03T01:00:00Z"
		newTime    = "2026-10-04T01:00:00Z"
	)
	first := snapshot("item-1", AdoptionStatusAccepted, modernTime)
	second := snapshot("item-2", AdoptionStatusAccepted, modernTime)
	firstRejected := snapshot("item-1", AdoptionStatusRejected, newTime)
	secondPending := snapshot("item-2", AdoptionStatusPending, modernTime)
	legacyOld := snapshot("", AdoptionStatusAccepted, oldTime)
	legacyOld.RunItemCount = 0
	legacyRejected := snapshot("", AdoptionStatusRejected, newTime)
	legacyOtherTarget := legacyOld
	legacyOtherTarget.ObjectNumber++
	firstRefreshed := snapshot("item-1", AdoptionStatusAccepted, newTime)
	secondRefreshed := snapshot("item-2", AdoptionStatusAccepted, newTime)

	for _, tc := range []struct {
		name         string
		observations []EpisodeAdoptionObservation
		want         []EpisodeAdoptionObservation
		err          string
	}{
		{
			name:         "modern only replacement and repeated acceptance",
			observations: []EpisodeAdoptionObservation{first, second, firstRejected, second, firstRejected},
			want:         []EpisodeAdoptionObservation{firstRejected, second},
		},
		{
			name:         "legacy only duplicates",
			observations: []EpisodeAdoptionObservation{legacyOld, legacyOld},
			want:         []EpisodeAdoptionObservation{legacyOld},
		},
		{
			name:         "legacy only replacement",
			observations: []EpisodeAdoptionObservation{legacyOld, legacyRejected, legacyOld},
			want:         []EpisodeAdoptionObservation{legacyRejected},
		},
		{
			name:         "mixed stale legacy preserves precise actions",
			observations: []EpisodeAdoptionObservation{legacyOld, first, second, legacyOld, first, second},
			want:         []EpisodeAdoptionObservation{first, second},
		},
		{
			name:         "mixed legacy does not affect another target",
			observations: []EpisodeAdoptionObservation{legacyOld, first, second, legacyOtherTarget, legacyOtherTarget},
			want:         []EpisodeAdoptionObservation{legacyOtherTarget, first, second},
		},
		{
			name:         "precise statuses supersede same instant legacy",
			observations: []EpisodeAdoptionObservation{snapshot("", AdoptionStatusRejected, "2026-10-02T03:00:00+02:00"), first, secondPending},
			want:         []EpisodeAdoptionObservation{first, secondPending},
		},
		{
			name:         "later precise rejection replaces acceptance with stale legacy",
			observations: []EpisodeAdoptionObservation{legacyOld, first, firstRejected, second},
			want:         []EpisodeAdoptionObservation{firstRejected, second},
		},
		{
			name:         "fully refreshed precise snapshots supersede newer legacy",
			observations: []EpisodeAdoptionObservation{first, second, snapshot("", AdoptionStatusRejected, legacyTime), firstRefreshed, secondRefreshed},
			want:         []EpisodeAdoptionObservation{firstRefreshed, secondRefreshed},
		},
		{
			name:         "partially refreshed precise snapshots do not supersede newer legacy",
			observations: []EpisodeAdoptionObservation{first, second, snapshot("", AdoptionStatusRejected, legacyTime), firstRefreshed},
			err:          "legacy adoption for run 1 is newer than precise item observations; reevaluate the run",
		},
		{
			name:         "newer legacy acceptance is ambiguous",
			observations: []EpisodeAdoptionObservation{first, second, snapshot("", AdoptionStatusAccepted, legacyTime)},
			err:          "legacy adoption for run 1 is newer than precise item observations; reevaluate the run",
		},
		{
			name:         "newer legacy rejection is ambiguous",
			observations: []EpisodeAdoptionObservation{first, second, snapshot("", AdoptionStatusRejected, legacyTime)},
			err:          "legacy adoption for run 1 is newer than precise item observations; reevaluate the run",
		},
		{
			name:         "newer legacy pending status is ambiguous",
			observations: []EpisodeAdoptionObservation{first, second, snapshot("", AdoptionStatusPending, legacyTime)},
			err:          "legacy adoption for run 1 is newer than precise item observations; reevaluate the run",
		},
		{
			name:         "conflicting stale legacy is superseded by precise snapshots",
			observations: []EpisodeAdoptionObservation{legacyOld, snapshot("", AdoptionStatusRejected, oldTime), first, second},
			want:         []EpisodeAdoptionObservation{first, second},
		},
		{
			name:         "conflicting stale modern statuses are superseded",
			observations: []EpisodeAdoptionObservation{first, snapshot("item-1", AdoptionStatusRejected, modernTime), firstRefreshed},
			want:         []EpisodeAdoptionObservation{firstRefreshed},
		},
		{
			name:         "conflicting stale legacy statuses are superseded",
			observations: []EpisodeAdoptionObservation{legacyOld, snapshot("", AdoptionStatusRejected, oldTime), legacyRejected},
			want:         []EpisodeAdoptionObservation{legacyRejected},
		},
		{
			name:         "conflicting latest modern statuses remain errors",
			observations: []EpisodeAdoptionObservation{firstRefreshed, firstRejected, legacyOld},
			err:          "conflicting adoptions",
		},
		{
			name:         "conflicting latest legacy statuses remain errors",
			observations: []EpisodeAdoptionObservation{legacyRejected, snapshot("", AdoptionStatusAccepted, newTime)},
			err:          "conflicting adoptions",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				input := slices.Clone(tc.observations)
				if reverse {
					slices.Reverse(input)
				}
				got, err := latestEpisodeAdoptions(input)
				if tc.err != "" {
					require.ErrorContains(t, err, tc.err)
					continue
				}
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				data, err := buildEpisodeAdoptions(episodeLogs(1), input, nil)
				require.NoError(t, err)
				accepted := 0
				for _, item := range tc.want {
					if item.AdoptionStatus == AdoptionStatusAccepted {
						accepted++
					}
				}
				assert.Equal(t, len(tc.want), data.Episodes[0].Summary.Total)
				assert.Equal(t, accepted, data.Episodes[0].Summary.Accepted)
			}
		})
	}
}

func TestBuildEpisodeAdoptionsIncompleteItems(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 2} {
		item := episodeObservation(1, "item-1", AdoptionStatusAccepted)
		item.RunItemCount = count
		data, err := buildEpisodeAdoptions(episodeLogs(1), []EpisodeAdoptionObservation{item}, map[int64]episodeAdoptionCost{1: episodeCost(6)})
		require.NoError(t, err)
		assert.Equal(t, "partial", data.Episodes[0].AdoptionCoverage)
		assert.Nil(t, data.Episodes[0].CostPerAcceptedAdoption)
	}
}

func TestBuildEpisodeAdoptionsRecordedMembershipCoverage(t *testing.T) {
	t.Parallel()
	for _, coverage := range []*EpisodeAdoptionMembershipCoverage{
		{Scope: "selected_runs", Status: "partial", Reasons: []string{"parent_not_selected"}},
		{Scope: "selected_runs", Status: "unknown", CollectionTruncated: true},
	} {
		logs := episodeLogs(1)
		logs.Episodes[0].Coverage = coverage
		data, err := buildEpisodeAdoptions(logs, []EpisodeAdoptionObservation{episodeObservation(1, "item-1", AdoptionStatusAccepted)},
			map[int64]episodeAdoptionCost{1: episodeCost(2)})
		require.NoError(t, err)
		row := data.Episodes[0]
		assert.Equal(t, "complete", row.AdoptionCoverage, "lineage and adoption coverage are independent")
		assert.Equal(t, "available", row.CostCoverage)
		assert.Equal(t, coverage, row.MembershipCoverage)
		assert.Nil(t, row.CostPerAcceptedAdoption)
	}
}

func TestLatestEpisodeAdoptionsValidation(t *testing.T) {
	t.Parallel()
	item := episodeObservation(1, "item-1", AdoptionStatusAccepted)
	newer := item
	newer.AdoptionStatus = AdoptionStatusRejected
	newer.CheckedAt = "2026-10-03T01:00:00Z"
	data, err := buildEpisodeAdoptions(episodeLogs(1), []EpisodeAdoptionObservation{newer, item}, nil)
	require.NoError(t, err)
	assert.Zero(t, data.Episodes[0].Summary.Accepted, "a previous acceptance does not survive a later rejection")
	assert.Equal(t, 1, data.Episodes[0].Summary.Rejected)

	newer.CheckedAt = item.CheckedAt
	_, err = latestEpisodeAdoptions([]EpisodeAdoptionObservation{newer, item})
	require.ErrorContains(t, err, "conflicting adoptions")
	item.CheckedAt = "invalid"
	_, err = latestEpisodeAdoptions([]EpisodeAdoptionObservation{item})
	require.ErrorContains(t, err, "invalid checked_at")
	item.RunID = 0
	_, err = latestEpisodeAdoptions([]EpisodeAdoptionObservation{item})
	require.ErrorContains(t, err, "positive run_id")
}

func TestBuildEpisodeAdoptionsMultipleEpisodes(t *testing.T) {
	t.Parallel()
	logs := episodeLogs(1, 2)
	logs.Episodes = append(logs.Episodes, episodeAdoptionMembership{EpisodeData: EpisodeData{EpisodeID: "ep-2", RunIDs: []int64{3}, TotalRuns: 1}})
	logs.Runs = append(logs.Runs, RunData{RunID: 3})
	observations := []EpisodeAdoptionObservation{
		episodeObservation(1, "item-1", AdoptionStatusAccepted),
		episodeObservation(2, "item-1", AdoptionStatusAccepted),
		episodeObservation(3, "item-1", AdoptionStatusUnknown),
		episodeObservation(99, "item-1", AdoptionStatusAccepted),
	}
	data, err := buildEpisodeAdoptions(logs, observations, nil)
	require.NoError(t, err)
	require.Len(t, data.Episodes, 2)
	assert.Equal(t, 2, data.Episodes[0].Summary.Accepted)
	assert.Zero(t, data.Episodes[1].Summary.Accepted)
	assert.Equal(t, 1, data.Episodes[1].Summary.Unknown)
	assert.Equal(t, []int64{99}, data.UnmatchedAdoptionRunIDs)

	logs.Episodes[1].RunIDs = []int64{2}
	_, err = buildEpisodeAdoptions(logs, observations, nil)
	require.ErrorContains(t, err, "overlapping episode membership")
}
