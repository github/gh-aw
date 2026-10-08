package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

const workQueueViewBytes = 64 << 10

type workQueueRow struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	WorkID     string `json:"work_id,omitempty"`
	GraphID    string `json:"graph_id"`
	Text       string `json:"text"`
	State      string `json:"state,omitempty"`
	Priority   int    `json:"priority,omitempty"`
	Delivery   string `json:"delivery,omitempty"`
	Current    bool   `json:"current,omitempty"`
	DispatchID string `json:"dispatch_id,omitempty"`
	Handle     string `json:"claim_handle,omitempty"`
}

func (row workQueueRow) key() string { return row.Kind + ":" + row.ID }

type workQueueFilter struct{ Graph, Pool, State, Query string }
type workQueueGroup struct {
	work   *workqueue.WorkState
	claims []*workqueue.ClaimState
}

// Quote labels as data, including Unicode/bidi and terminal controls.
func workQueueText(value string) string { return strconv.QuoteToASCII(value) }

func workQueueMatches(query string, values ...string) bool {
	if query == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(query)) {
			return true
		}
	}
	return false
}

func workQueueOrderedWork(state workqueue.Projection, filter workQueueFilter) []*workqueue.WorkState {
	works := make([]*workqueue.WorkState, 0, len(state.Works))
	for _, work := range state.Works {
		if filter.Graph != "" && filter.Graph != work.GraphID || filter.Pool != "" && filter.Pool != work.Pool || filter.State != "" && filter.State != work.State {
			continue
		}
		works = append(works, work)
	}
	slices.SortFunc(works, func(a, b *workqueue.WorkState) int {
		if order := cmp.Compare(a.GraphID, b.GraphID); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Position.Commit, b.Position.Commit); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Position.Operation, b.Position.Operation); order != 0 {
			return order
		}
		return cmp.Compare(a.WorkID, b.WorkID)
	})
	return works
}

func workQueueMatchingClaims(state workqueue.Projection, work *workqueue.WorkState, members []*workqueue.ClaimState, query string) ([]*workqueue.ClaimState, bool) {
	slices.SortFunc(members, func(a, b *workqueue.ClaimState) int {
		if a.ClaimID == work.ClaimID && b.ClaimID != work.ClaimID {
			return -1
		}
		if b.ClaimID == work.ClaimID && a.ClaimID != work.ClaimID {
			return 1
		}
		if order := cmp.Compare(state.Requests[b.RequestID].At, state.Requests[a.RequestID].At); order != 0 {
			return order
		}
		return cmp.Compare(a.ClaimID, b.ClaimID)
	})
	matches := workQueueMatches(query, work.WorkID, work.GraphID, work.NodeKey, work.FairnessKey, work.State, work.Pool, work.WorkerProfile, work.Barrier)
	selected := []*workqueue.ClaimState{}
	for _, claim := range members {
		if matches || workQueueMatches(query, claim.ClaimID, claim.Handle, claim.DispatchID, claim.State) {
			selected = append(selected, claim)
		}
	}
	return selected, matches
}

func workQueueRows(state workqueue.Projection, filter workQueueFilter, collapsed map[string]bool) []workQueueRow {
	claims := map[string][]*workqueue.ClaimState{}
	for _, claim := range state.Claims {
		claims[claim.WorkID] = append(claims[claim.WorkID], claim)
	}
	groups := []workQueueGroup{}
	counts := map[string]int{}
	for _, work := range workQueueOrderedWork(state, filter) {
		members, matches := workQueueMatchingClaims(state, work, claims[work.WorkID], filter.Query)
		if !matches && len(members) == 0 {
			continue
		}
		counts[work.GraphID]++
		groups = append(groups, workQueueGroup{work: work, claims: members})
	}
	rows := []workQueueRow{}
	graph := ""
	for _, group := range groups {
		work := group.work
		if work.GraphID != graph {
			graph = work.GraphID
			rows = append(rows, workQueueRow{Kind: "graph", ID: graph, GraphID: graph, Text: "Graph " + workQueueText(graph)})
		}
		counts[graph]--
		last := counts[graph] == 0
		closed := collapsed[work.WorkID] && filter.Query == ""
		rows = append(rows, workQueueForestWorkRow(work, last, closed, len(group.claims) > 0))
		if !closed {
			rows = append(rows, workQueueForestClaimRows(work, group.claims, last)...)
		}
	}
	return rows
}

func workQueueForestWorkRow(work *workqueue.WorkState, last, collapsed, hasClaims bool) workQueueRow {
	connector, marker := "+--", "-"
	if last {
		connector = "\\--"
	}
	if hasClaims && collapsed {
		marker = "+"
	}
	return workQueueRow{Kind: "work", ID: work.WorkID, WorkID: work.WorkID, GraphID: work.GraphID,
		State: work.State, Priority: work.SchedulingPriority(), Delivery: work.Barrier,
		Text: fmt.Sprintf("%s [%s] W %s %s [%s P%d delivery=%s]", connector, marker, workQueueText(work.WorkID), workQueueText(work.NodeKey), work.State, work.SchedulingPriority(), work.Barrier)}
}

func workQueueForestClaimRows(work *workqueue.WorkState, members []*workqueue.ClaimState, last bool) []workQueueRow {
	rows := make([]workQueueRow, 0, len(members))
	prefix := "|   "
	if last {
		prefix = "    "
	}
	for index, claim := range members {
		connector := "+--"
		if index == len(members)-1 {
			connector = "\\--"
		}
		current := claim.ClaimID == work.ClaimID && work.State != "cancelled"
		owner := "history"
		if current {
			owner = "current"
		}
		rows = append(rows, workQueueRow{Kind: "claim", ID: claim.ClaimID, WorkID: work.WorkID, GraphID: work.GraphID,
			State: claim.State, Current: current, DispatchID: claim.DispatchID, Handle: claim.Handle,
			Text: fmt.Sprintf("%s%s C %s [%s %s] handle=%s dispatch=%s", prefix, connector, workQueueText(claim.ClaimID), claim.State, owner, workQueueText(claim.Handle), workQueueText(claim.DispatchID))})
	}
	return rows
}

type workQueuePage struct {
	EvaluatedAt     int64           `json:"evaluated_at"`
	Status          string          `json:"status"`
	Tip             string          `json:"tip"`
	PolicyEpoch     string          `json:"policy_epoch"`
	Repository      string          `json:"repository"`
	Branch          string          `json:"branch"`
	Stats           workqueue.Stats `json:"stats"`
	AdmissionPaused bool            `json:"admission_paused"`
	GrantsPaused    bool            `json:"grants_paused"`
	Offset          int             `json:"offset"`
	TotalRows       int             `json:"total_rows"`
	NextOffset      *int            `json:"next_offset,omitempty"`
	Rows            []workQueueRow  `json:"rows"`
}

func workQueueStatePage(state workqueue.Projection, branch string, rows []workQueueRow, offset, limit int) (workQueuePage, error) {
	page := workQueuePage{EvaluatedAt: time.Now().UnixMilli(), Status: "committed_snapshot", Tip: state.Tip, PolicyEpoch: state.PolicyEpoch,
		Repository: state.Repository, Branch: branch, Stats: state.Stats, AdmissionPaused: state.AdmissionPaused, GrantsPaused: state.GrantsPaused,
		Offset: offset, TotalRows: len(rows), Rows: []workQueueRow{}}
	if offset < 0 || limit < 1 || limit > 256 {
		return page, errors.New("invalid state page: offset >= 0 and limit 1..256 required")
	}
	start, bytes := min(offset, len(rows)), 0
	for _, row := range rows[start:] {
		if len(page.Rows) >= limit {
			break
		}
		page.Rows = append(page.Rows, row)
		encoded, err := json.MarshalIndent(page, "", "  ")
		if err != nil {
			return page, err
		}
		if len(encoded) > workQueueViewBytes-2048 || bytes+len(row.Text) > workQueueViewBytes-4096 {
			page.Rows = page.Rows[:len(page.Rows)-1]
			break
		}
		bytes += len(row.Text) + 1
	}
	if offset+len(page.Rows) < len(rows) {
		next := offset + len(page.Rows)
		page.NextOffset = &next
	}
	return page, nil
}

func workQueuePageText(page workQueuePage) string {
	lines := []string{fmt.Sprintf("WORK QUEUE %s branch=%s\nTip %s | policy %s\nWork %d: available=%d claimed=%d completed=%d cancelled=%d | Claims=%d native reservations=%d\nAdmission paused=%t | grants paused=%t | rows %d..%d/%d",
		workQueueText(page.Repository), workQueueText(page.Branch), workQueueText(page.Tip), workQueueText(page.PolicyEpoch),
		page.Stats.Work, page.Stats.Available, page.Stats.Claimed, page.Stats.Completed, page.Stats.Cancelled, page.Stats.Claims, page.Stats.Dispatches,
		page.AdmissionPaused, page.GrantsPaused, page.Offset, page.Offset+len(page.Rows), page.TotalRows)}
	for _, row := range page.Rows {
		lines = append(lines, row.Text)
	}
	if page.TotalRows == 0 {
		lines = append(lines, "No matching Work or Claims.")
	}
	if page.NextOffset != nil {
		lines = append(lines, fmt.Sprintf("... more rows: use state --offset %d (same filters; refresh may shift offsets)", *page.NextOffset))
	}
	lines = append(lines, "W=Work; C=Claim; branches group graph membership/attempts, not dependency edges.",
		"Use inspect for exact IDs, dependency cross-references, dispatch membership and delivery barriers.")
	return strings.Join(lines, "\n")
}

func workStateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "state", Short: "Show a bounded metadata-only ASCII Work/Claim forest", Args: cobra.NoArgs}
	cmd.Flags().String("graph", "", "Exact graph namespace")
	cmd.Flags().String("pool", "", "Exact scheduling pool")
	cmd.Flags().String("state", "", "Work state: available, claimed, completed, cancelled")
	cmd.Flags().String("search", "", "Case-insensitive metadata/ID substring")
	cmd.Flags().Int("offset", 0, "Matching row offset")
	cmd.Flags().Int("limit", 80, "Maximum rows on this page (1..256; 64 KiB cap)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		offset, _ := cmd.Flags().GetInt("offset")
		limit, _ := cmd.Flags().GetInt("limit")
		filter := workQueueFilter{}
		filter.Graph, _ = cmd.Flags().GetString("graph")
		filter.Pool, _ = cmd.Flags().GetString("pool")
		filter.State, _ = cmd.Flags().GetString("state")
		filter.Query, _ = cmd.Flags().GetString("search")
		if offset < 0 || limit < 1 || limit > 256 || filter.State != "" && !slices.Contains([]string{"available", "claimed", "completed", "cancelled"}, filter.State) {
			return errors.New("invalid state filters: use offset >= 0, limit 1..256 and a supported Work state")
		}
		state, err := workRead(cmd)
		if err != nil {
			return err
		}
		branch, _ := cmd.Flags().GetString("branch")
		page, err := workQueueStatePage(state, branch, workQueueRows(state, filter, nil), offset, limit)
		if err != nil {
			return err
		}
		return workPrint(cmd, page, workQueuePageText(page))
	}
	return cmd
}
