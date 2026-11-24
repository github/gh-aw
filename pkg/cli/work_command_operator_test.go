package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

const workOperatorPrivatePayload = `{"task":"PAYLOAD-MUST-STAY-PRIVATE","token":"SECRET-MUST-STAY-PRIVATE"}`

// workOperatorFixture submits three "factory" Work with private payloads; when
// claimed, one dispatch_next claims each Work into its own Dispatch.
func workOperatorFixture(t *testing.T, claimed bool) (*workQueueTestLog, []string) {
	t.Helper()
	log := newWorkQueueTestLog(t)
	works := []workqueue.WorkDefinition{}
	for _, key := range []string{"compile", "analyze", "publish"} {
		works = append(works, log.work(key, func(work *workqueue.WorkDefinition) {
			work.GraphID = "factory"
			work.Payload = json.RawMessage(workOperatorPrivatePayload)
		}))
	}
	ids := log.submit(works...)
	if claimed {
		require.Len(t, log.dispatch(3), 3)
	}
	return log, ids
}

func TestWorkOperatorForestDetailsAndRedaction(t *testing.T) {
	log, ids := workOperatorFixture(t, true)
	state := log.state()
	rows := workQueueRows(state, workQueueFilter{}, nil)
	require.Len(t, rows, 7, "graph, three Work and one Claim per Work")
	require.Equal(t, "graph", rows[0].Kind)
	for index, id := range ids {
		work, claim := rows[1+2*index], rows[2+2*index]
		require.Equal(t, "work", work.Kind)
		require.Equal(t, id, work.ID)
		require.Equal(t, "claimed", work.State)
		require.Equal(t, "claim", claim.Kind)
		require.Equal(t, id, claim.WorkID)
		require.True(t, claim.Current)
		require.Equal(t, "open", claim.State)
		require.Equal(t, "reserved", claim.DispatchState)
		require.Equal(t, "h1", claim.Handle)
	}
	for _, row := range rows {
		inspection, err := workQueueInspect(state, "work-queue", row)
		require.NoError(t, err)
		encoded, err := json.Marshal(inspection)
		require.NoError(t, err)
		for _, forbidden := range []string{"PAYLOAD-MUST", "SECRET-MUST"} {
			require.NotContains(t, inspection.Details, forbidden)
			require.NotContains(t, string(encoded), forbidden)
		}
		if row.Kind == "claim" {
			require.Contains(t, inspection.Details, row.ID)
			require.Contains(t, inspection.Details, "DISPATCH "+workQueueText(row.DispatchID))
			require.Contains(t, inspection.Details, "State: reserved")
			require.Contains(t, inspection.Details, "Run: none")
		}
	}

	// A diamond is represented as two references, never duplicated ownership.
	state.Works[ids[2]].DependsOn = []workqueue.Dependency{{Kind: "work", WorkID: ids[0]}, {Kind: "work", WorkID: ids[1]}}
	inspection, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: ids[2], WorkID: ids[2]})
	require.NoError(t, err)
	require.Contains(t, inspection.Details, "cross-references")
	require.Contains(t, inspection.Details, "Work "+workQueueText(ids[0])+": claimed")
	require.Contains(t, inspection.Details, "Work "+workQueueText(ids[1])+": claimed")

	state.Works[ids[0]].NodeKey = "\x1b[31mINJECT\n\u202e"
	rows = workQueueRows(state, workQueueFilter{}, nil)
	require.NotContains(t, rows[1].Text, "\x1b")
	require.NotContains(t, rows[1].Text, "\n")
	require.NotContains(t, rows[1].Text, "\u202e")
	for _, row := range rows {
		for _, r := range row.Text {
			require.LessOrEqual(t, r, rune(127), "forest is not ASCII: %q", row.Text)
		}
	}
	inspection, err = workQueueInspect(state, "work-queue", rows[1])
	require.NoError(t, err)
	require.NotContains(t, inspection.Details, "\x1b")
	require.NotContains(t, inspection.Details, "\u202e")

	filtered := workQueueRows(state, workQueueFilter{Query: rows[2].ID}, nil)
	require.Len(t, filtered, 3, "Claim search must retain its graph and Work context")
	require.Equal(t, rows[2].ID, filtered[2].ID)
	byNode := workQueueRows(state, workQueueFilter{Query: "ANALYZE"}, nil)
	require.Len(t, byNode, 3, "Work metadata search keeps its Claims")
	require.Equal(t, ids[1], byNode[1].ID)
	collapsed := workQueueRows(state, workQueueFilter{}, map[string]bool{ids[0]: true})
	require.Len(t, collapsed, 6, "collapse hides only the Claim history")
	require.Contains(t, collapsed[1].Text, "[+]")
	require.Len(t, workQueueRows(state, workQueueFilter{Query: rows[2].ID}, map[string]bool{ids[0]: true}), 3, "search ignores collapse")
	require.Empty(t, workQueueRows(state, workQueueFilter{State: "available"}, nil))
	require.Empty(t, workQueueRows(state, workQueueFilter{Graph: "other"}, nil))
}

func TestWorkOperatorForestOrdersCurrentClaimBeforeHistory(t *testing.T) {
	log, ids := workOperatorFixture(t, true)
	state := log.state()
	first := state.Works[ids[0]].ClaimID
	log.release(state.Claims[first].DispatchID, "operator")
	require.Len(t, log.dispatch(1), 1)
	state = log.state()
	current := state.Works[ids[0]].ClaimID
	require.NotEqual(t, first, current, "the re-dispatch must claim the first Work again")

	rows := workQueueRows(state, workQueueFilter{Pool: "default", Graph: "factory"}, nil)
	require.Equal(t, ids[0], rows[1].ID)
	require.NotNil(t, rows[1].Attempts)
	require.Equal(t, 1, *rows[1].Attempts)
	require.Equal(t, current, rows[2].ID)
	require.True(t, rows[2].Current)
	require.Equal(t, first, rows[3].ID)
	require.False(t, rows[3].Current)
	require.Equal(t, "cancelled", rows[3].State)
	require.Equal(t, "operator", rows[3].CancellationReason)
	require.Equal(t, "closed", rows[3].DispatchState)
	require.Contains(t, rows[3].Text, "[cancelled history]")
}

func TestWorkOperatorStateBoundsEmptyAndPagination(t *testing.T) {
	log, _ := workOperatorFixture(t, false)
	state := log.state()
	row := workQueueRow{Kind: "work", ID: "copyable", Text: strings.Repeat("x", 2000)}
	rows := make([]workQueueRow, 256)
	for index := range rows {
		rows[index] = row
	}
	page, err := workQueueStatePage(state, "work-queue", rows, 0, 256)
	require.NoError(t, err)
	data, err := json.MarshalIndent(page, "", "  ")
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), workQueueViewBytes)
	require.LessOrEqual(t, len(workQueuePageText(page)), workQueueViewBytes)
	require.NotNil(t, page.NextOffset, "a bounded page must be resumable")
	require.NotZero(t, *page.NextOffset)
	require.Contains(t, workQueuePageText(page), "--offset")
	next, err := workQueueStatePage(state, "work-queue", rows, *page.NextOffset, 3)
	require.NoError(t, err)
	require.Len(t, next.Rows, 3)
	require.Equal(t, *page.NextOffset, next.Offset)
	empty, err := workQueueStatePage(state, "work-queue", nil, 0, 80)
	require.NoError(t, err)
	require.Contains(t, workQueuePageText(empty), "No matching Work or Claims")
	require.Nil(t, empty.NextOffset)
	for _, bounds := range [][2]int{{-1, 10}, {0, 0}, {0, 257}} {
		_, err := workQueueStatePage(state, "work-queue", rows, bounds[0], bounds[1])
		require.Error(t, err, "%v", bounds)
	}
}
