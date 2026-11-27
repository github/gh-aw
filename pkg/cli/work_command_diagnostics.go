package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
)

type workQueuePoolStats struct {
	Pool           string         `json:"pool"`
	WorkByState    map[string]int `json:"work_by_state"`
	ClaimsByState  map[string]int `json:"claims_by_state"`
	OpenDispatches int            `json:"open_dispatches"`
	// Eligible counts available Work whose dependencies are satisfied; worker
	// routes and concurrency come from the compiled configuration, not the ledger.
	Eligible     int            `json:"eligible"`
	Blocked      map[string]int `json:"blocked"`
	NextEligible []string       `json:"next_eligible"`
}

type workQueueStatsReport struct {
	workqueue.Stats
	workQueueSnapshot
	ClaimsByState            map[string]int       `json:"claims_by_state"`
	ClaimCancellationReasons map[string]int       `json:"claim_cancellation_reasons"`
	WorkCancellationReasons  map[string]int       `json:"work_cancellation_reasons"`
	DispatchesByState        map[string]int       `json:"dispatches_by_state"`
	Pools                    []workQueuePoolStats `json:"pools"`
}

const workQueueNextEligible = 3

func workQueueWorkStates() map[string]int {
	return map[string]int{"available": 0, "claimed": 0, "completed": 0, "cancelled": 0}
}

func workQueueClaimStates() map[string]int {
	return map[string]int{"open": 0, "completed": 0, "cancelled": 0}
}

func workQueueStats(state workqueue.Projection, branch string) workQueueStatsReport {
	report := workQueueStatsReport{
		Stats: state.Stats, workQueueSnapshot: workQueueSnapshotOf(state, branch),
		ClaimsByState: workQueueClaimStates(), ClaimCancellationReasons: map[string]int{},
		WorkCancellationReasons: map[string]int{}, DispatchesByState: map[string]int{"reserved": 0, "started": 0, "bound": 0, "closed": 0},
		Pools: []workQueuePoolStats{},
	}
	pools := map[string]*workQueuePoolStats{}
	pool := func(name string) *workQueuePoolStats {
		if pools[name] == nil {
			pools[name] = &workQueuePoolStats{Pool: name, WorkByState: workQueueWorkStates(), ClaimsByState: workQueueClaimStates(), Blocked: map[string]int{}, NextEligible: []string{}}
		}
		return pools[name]
	}
	for _, work := range state.Works {
		stats := pool(work.Pool)
		stats.WorkByState[work.State]++
		if work.State == "cancelled" {
			report.WorkCancellationReasons[work.CancellationReason]++
		}
		if work.State != "available" {
			continue
		}
		if ready, reason := state.DependencyStatus(work); !ready {
			stats.Blocked[reason]++
		}
	}
	for _, claim := range state.Claims {
		report.ClaimsByState[claim.State]++
		if work := state.Works[claim.WorkID]; work != nil {
			pool(work.Pool).ClaimsByState[claim.State]++
		}
		if claim.State == "cancelled" {
			report.ClaimCancellationReasons[claim.CancellationReason]++
		}
	}
	for _, dispatch := range state.Dispatches {
		report.DispatchesByState[dispatch.State]++
		if dispatch.State != "closed" {
			pool(dispatch.Pool).OpenDispatches++
		}
	}
	names := make([]string, 0, len(pools))
	for name := range pools {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		stats := pools[name]
		eligible := state.EligibleWork(name, nil)
		stats.Eligible = len(eligible)
		for _, work := range eligible[:min(len(eligible), workQueueNextEligible)] {
			stats.NextEligible = append(stats.NextEligible, work.WorkID)
		}
		report.Pools = append(report.Pools, *stats)
	}
	return report
}

func workQueueStatsText(report workQueueStatsReport) string {
	s := report.Stats
	lines := []string{fmt.Sprintf("Work: %d; available: %d; claimed: %d; completed: %d; cancelled: %d; Claims: %d; open dispatches: %d; graph nodes: %d",
		s.Work, s.Available, s.Claimed, s.Completed, s.Cancelled, s.Claims, s.Dispatches, s.Nodes),
		fmt.Sprintf("Claims: open=%d completed=%d cancelled=%d | dispatches: reserved=%d started=%d bound=%d closed=%d",
			report.ClaimsByState["open"], report.ClaimsByState["completed"], report.ClaimsByState["cancelled"],
			report.DispatchesByState["reserved"], report.DispatchesByState["started"], report.DispatchesByState["bound"], report.DispatchesByState["closed"]),
		fmt.Sprintf("Tip: %s | transactions: %d", workQueueText(report.Tip), s.Transactions)}
	if reasons := workQueueReasonCounts(report.ClaimCancellationReasons); reasons != "" {
		lines = append(lines, "Claim cancellations: "+reasons)
	}
	if reasons := workQueueReasonCounts(report.WorkCancellationReasons); reasons != "" {
		lines = append(lines, "Work cancellations: "+reasons)
	}
	for _, pool := range report.Pools {
		lines = append(lines, strings.TrimSuffix(strings.TrimPrefix(workQueuePoolDiagnosticsText(pool), "    "), "\n"))
	}
	lines = append(lines, "Dispatch states and run bindings are ledger records, not live Actions status; eligibility ignores worker routes and concurrency.")
	return strings.Join(lines, "\n")
}

func workQueuePoolDiagnosticsText(pool workQueuePoolStats) string {
	text := fmt.Sprintf("    pool: %s available=%d eligible=%d claimed=%d open_dispatches=%d",
		escapeWorkQueueDisplay(pool.Pool), pool.WorkByState["available"], pool.Eligible, pool.WorkByState["claimed"], pool.OpenDispatches)
	if len(pool.NextEligible) > 0 {
		next := make([]string, 0, len(pool.NextEligible))
		for _, id := range pool.NextEligible {
			next = append(next, escapeWorkQueueDisplay(id))
		}
		text += " next=" + strings.Join(next, ",")
	}
	if reasons := workQueueReasonCounts(pool.Blocked); reasons != "" {
		text += " blocked=" + reasons
	}
	return text + "\n"
}

func workQueueReasonCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys[:min(len(keys), 16)] {
		parts = append(parts, fmt.Sprintf("%s=%d", escapeWorkQueueDisplay(key), counts[key]))
	}
	if len(keys) > 16 {
		parts = append(parts, "... (additional reasons in JSON)")
	}
	return strings.Join(parts, ",")
}
