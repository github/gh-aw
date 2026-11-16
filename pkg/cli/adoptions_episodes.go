package cli

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/safeoutput"
)

// EpisodeAdoptionObservation joins an adoption to its source workflow run.
type EpisodeAdoptionObservation struct {
	RunID        int64 `json:"run_id"`
	RunItemCount int   `json:"run_item_count,omitempty"`
	AdoptionReport
}

// EpisodeAdoptionsData joins selected episode members and adoptions.
type EpisodeAdoptionsData struct {
	Episodes                []EpisodeAdoptionData `json:"episodes"`
	UnmatchedAdoptionRunIDs []int64               `json:"unmatched_adoption_run_ids"`
}

// EpisodeAdoptionMembershipCoverage reads optional logs coverage without depending
// on the episode producer's coverage implementation.
type EpisodeAdoptionMembershipCoverage struct {
	Scope               string   `json:"scope"`
	Status              string   `json:"status"`
	Reasons             []string `json:"reasons,omitempty"`
	CollectionTruncated bool     `json:"collection_truncated,omitempty"`
}

type episodeAdoptionsLogs struct {
	Runs      []RunData                   `json:"runs"`
	Episodes  []episodeAdoptionMembership `json:"episodes"`
	Adoptions []AdoptionsData             `json:"adoptions,omitempty"`
}

type episodeAdoptionMembership struct {
	EpisodeData
	Coverage *EpisodeAdoptionMembershipCoverage `json:"coverage,omitempty"`
}

// EpisodeAdoptionData never asserts that the selected runs exhaust an episode.
type EpisodeAdoptionData struct {
	EpisodeID               string                             `json:"episode_id"`
	RunIDs                  []int64                            `json:"run_ids"`
	MembershipScope         string                             `json:"membership_scope" jsonschema:"Always selected_runs; membership outside the supplied logs is not verified"`
	MembershipCoverage      *EpisodeAdoptionMembershipCoverage `json:"membership_coverage,omitempty" jsonschema:"Optional lineage and collection coverage copied from logs, separate from adoption and cost coverage"`
	Items                   []EpisodeAdoptionObservation       `json:"items"`
	Summary                 AdoptionSummary                    `json:"summary"`
	ObservedAIC             *float64                           `json:"observed_aic" jsonschema:"Sum of available selected-run AIC, including detection usage; null when no cost is available"`
	CostPerAcceptedAdoption *float64                           `json:"cost_per_accepted_adoption" jsonschema:"Selected-run observed AIC divided by accepted items; null for incomplete evidence or cost, ambiguous identities, or no accepted items"`
	CostCoverage            string                             `json:"cost_coverage" jsonschema:"available, partial, or unavailable for selected run IDs"`
	AdoptionCoverage        string                             `json:"adoption_coverage" jsonschema:"complete or partial for selected runs only, not full-episode membership"`
	AdoptionRunIDs          []int64                            `json:"adoption_run_ids"`
	MissingAdoptionRunIDs   []int64                            `json:"missing_adoption_run_ids"`
	CostRunIDs              []int64                            `json:"cost_run_ids"`
	MissingCostRunIDs       []int64                            `json:"missing_cost_run_ids"`
	MissingRunIDs           []int64                            `json:"missing_run_ids"`
	Qualifications          []string                           `json:"qualifications"`
}

type episodeAdoptionCost struct {
	aic      *float64
	warnings []string
}

type episodeAdoptionSnapshot struct {
	report   EpisodeAdoptionObservation
	at       time.Time
	conflict bool
	targets  []string
}

// latestEpisodeAdoptions deduplicates by action identity, never by acceptance.
func latestEpisodeAdoptions(observations []EpisodeAdoptionObservation) ([]EpisodeAdoptionObservation, error) {
	latest, err := selectEpisodeAdoptionSnapshots(observations)
	if err != nil {
		return nil, err
	}
	reports, err := unshadowedEpisodeAdoptionReports(latest)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(reports, func(a, b EpisodeAdoptionObservation) int {
		if order := cmp.Compare(a.RunID, b.RunID); order != 0 {
			return order
		}
		if order := cmp.Compare(a.ItemID, b.ItemID); order != 0 {
			return order
		}
		return cmp.Compare(episodeAdoptionLegacyKey(a), episodeAdoptionLegacyKey(b))
	})
	return reports, nil
}

func selectEpisodeAdoptionSnapshots(observations []EpisodeAdoptionObservation) (map[string]episodeAdoptionSnapshot, error) {
	latest := make(map[string]episodeAdoptionSnapshot)
	for _, observation := range observations {
		if observation.RunID <= 0 || observation.Type == "" {
			return nil, errors.New("adoption observation requires a positive run_id and nonempty type")
		}
		at, err := time.Parse(time.RFC3339Nano, observation.CheckedAt)
		if err != nil {
			return nil, fmt.Errorf("invalid checked_at for adoption in run %d: %w", observation.RunID, err)
		}
		observation.CheckedAt = at.UTC().Format(time.RFC3339Nano)
		identity := observation.ItemID
		if identity == "" {
			identity = episodeAdoptionLegacyKey(observation)
		}
		key := fmt.Sprintf("%d:%s", observation.RunID, identity)
		previous, exists := latest[key]
		if !exists || at.After(previous.at) {
			latest[key] = episodeAdoptionSnapshot{report: observation, at: at}
		} else if at.Equal(previous.at) && !episodeAdoptionObservationsEqual(observation, previous.report) {
			previous.conflict = true
			target := episodeAdoptionLegacyKey(observation)
			if target != episodeAdoptionLegacyKey(previous.report) && !slices.Contains(previous.targets, target) {
				previous.targets = append(previous.targets, target)
			}
			latest[key] = previous
		}
	}
	return latest, nil
}

func episodeAdoptionObservationsEqual(a, b EpisodeAdoptionObservation) bool {
	return a.RunID == b.RunID &&
		a.RunItemCount == b.RunItemCount &&
		a.AdoptionEvaluation == b.AdoptionEvaluation &&
		a.ItemID == b.ItemID &&
		a.Type == b.Type &&
		a.ObjectURL == b.ObjectURL &&
		a.ObjectNumber == b.ObjectNumber &&
		a.TracedRootURL == b.TracedRootURL &&
		a.AttributionStatus == b.AttributionStatus &&
		a.AttributionSource == b.AttributionSource &&
		a.Repo == b.Repo &&
		a.Detail == b.Detail &&
		a.TimeToAdoptionHours == b.TimeToAdoptionHours &&
		a.HumanComments == b.HumanComments &&
		a.HumanEdits == b.HumanEdits &&
		a.HumanReviews == b.HumanReviews &&
		a.ZeroTouch == b.ZeroTouch &&
		a.ZeroTouchEvidenceComplete == b.ZeroTouchEvidenceComplete &&
		a.ObjectiveValue == b.ObjectiveValue &&
		(a.ObjectiveLabels == nil) == (b.ObjectiveLabels == nil) &&
		slices.Equal(a.ObjectiveLabels, b.ObjectiveLabels) &&
		a.CreatedAt == b.CreatedAt &&
		a.CheckedAt == b.CheckedAt &&
		optionalDeliveryEqual(a.Delivery, b.Delivery) &&
		a.EvalError == b.EvalError
}

func optionalDeliveryEqual(a, b *safeoutput.DeliveryEvaluation) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func unshadowedEpisodeAdoptionReports(latest map[string]episodeAdoptionSnapshot) ([]EpisodeAdoptionObservation, error) {
	preciseTimes := map[int]map[string]time.Time{1: {}, 2: {}}
	for _, item := range latest {
		if precision := episodeAdoptionIdentityPrecision(item.report); precision > 0 {
			key := episodeAdoptionLegacyKey(item.report)
			if at, exists := preciseTimes[precision][key]; !exists || item.at.Before(at) {
				preciseTimes[precision][key] = item.at
			}
		}
	}
	reports := make([]EpisodeAdoptionObservation, 0, len(latest))
	for _, key := range slices.Sorted(maps.Keys(latest)) {
		item := latest[key]
		shadowed, err := episodeAdoptionSnapshotSuperseded(item, preciseTimes)
		if err != nil {
			return nil, err
		}
		if shadowed {
			continue
		}
		if item.conflict {
			identity := item.report.ItemID
			if identity == "" {
				identity = episodeAdoptionLegacyKey(item.report)
			}
			return nil, fmt.Errorf("conflicting adoptions for run %d item %q at %s; reevaluate the run", item.report.RunID, identity, item.report.CheckedAt)
		}
		reports = append(reports, item.report)
	}
	return reports, nil
}

func episodeAdoptionSnapshotSuperseded(item episodeAdoptionSnapshot, preciseTimes map[int]map[string]time.Time) (bool, error) {
	targets := append([]string{episodeAdoptionLegacyKey(item.report)}, item.targets...)
	slices.Sort(targets)
	for _, target := range targets {
		shadowed := false
		for precision := 2; precision > episodeAdoptionIdentityPrecision(item.report); precision-- {
			if at, modern := preciseTimes[precision][target]; modern {
				// A legacy row cannot identify which precise action a newer status replaces.
				if item.at.After(at) {
					return false, fmt.Errorf("legacy adoption for run %d is newer than precise item observations; reevaluate the run", item.report.RunID)
				}
				shadowed = true
				break
			}
		}
		if !shadowed {
			return false, nil
		}
	}
	return true, nil
}

// Identity precision increases from missing IDs to positional IDs to stable IDs.
func episodeAdoptionIdentityPrecision(item EpisodeAdoptionObservation) int {
	if item.ItemID == "" {
		return 0
	}
	if suffix, positional := strings.CutPrefix(item.ItemID, "item-"); positional {
		if index, err := strconv.Atoi(suffix); err == nil && index > 0 {
			return 1
		}
	}
	return 2
}

func episodeAdoptionLegacyKey(item EpisodeAdoptionObservation) string {
	return fmt.Sprintf("legacy:%d:%q:%q:%q:%d:%q", item.RunID, item.Type,
		item.Repo, item.ObjectURL, item.ObjectNumber, item.CreatedAt)
}

func buildEpisodeAdoptions(logs episodeAdoptionsLogs, observations []EpisodeAdoptionObservation, costs map[int64]episodeAdoptionCost) (EpisodeAdoptionsData, error) {
	latest, err := latestEpisodeAdoptions(observations)
	if err != nil {
		return EpisodeAdoptionsData{}, err
	}
	byRun := make(map[int64][]EpisodeAdoptionObservation)
	for _, observation := range latest {
		byRun[observation.RunID] = append(byRun[observation.RunID], observation)
	}
	for _, report := range logs.Adoptions {
		if report.EvalError == "" && len(report.Items) == 0 {
			if _, observed := byRun[report.RunID]; !observed {
				byRun[report.RunID] = []EpisodeAdoptionObservation{}
			}
		}
	}
	runs := make(map[int64]bool)
	for _, run := range logs.Runs {
		runs[run.RunID] = true
	}
	members := make(map[int64]bool)
	data := EpisodeAdoptionsData{Episodes: []EpisodeAdoptionData{}, UnmatchedAdoptionRunIDs: []int64{}}
	for _, episode := range logs.Episodes {
		for _, id := range episode.RunIDs {
			if id <= 0 || members[id] {
				return EpisodeAdoptionsData{}, fmt.Errorf("invalid or overlapping episode membership for run %d", id)
			}
			members[id] = true
		}
		row, err := summarizeEpisodeAdoptions(episode, byRun, costs, runs)
		if err != nil {
			return EpisodeAdoptionsData{}, err
		}
		data.Episodes = append(data.Episodes, row)
	}
	for id := range byRun {
		if !members[id] {
			data.UnmatchedAdoptionRunIDs = append(data.UnmatchedAdoptionRunIDs, id)
		}
	}
	slices.Sort(data.UnmatchedAdoptionRunIDs)
	slices.SortFunc(data.Episodes, func(a, b EpisodeAdoptionData) int { return cmp.Compare(a.EpisodeID, b.EpisodeID) })
	return data, nil
}

func summarizeEpisodeAdoptions(episode episodeAdoptionMembership, byRun map[int64][]EpisodeAdoptionObservation, costs map[int64]episodeAdoptionCost, runs map[int64]bool) (EpisodeAdoptionData, error) {
	row := EpisodeAdoptionData{
		EpisodeID: episode.EpisodeID, RunIDs: slices.Clone(episode.RunIDs),
		MembershipScope: "selected_runs", Items: []EpisodeAdoptionObservation{},
		MembershipCoverage: episode.Coverage, CostCoverage: "available", AdoptionCoverage: "complete",
		AdoptionRunIDs: []int64{}, MissingAdoptionRunIDs: []int64{},
		CostRunIDs: []int64{}, MissingCostRunIDs: []int64{}, MissingRunIDs: []int64{},
		Qualifications: []string{"Membership is limited to the supplied logs selection; full-episode membership is not verified."},
	}
	slices.Sort(row.RunIDs)
	if episode.EpisodeID == "" || len(row.RunIDs) == 0 {
		return row, errors.New("each episode requires an episode_id and at least one run_id; regenerate logs with --verbose --json")
	}
	legacy, incompleteEvidence := false, false
	for _, id := range row.RunIDs {
		items, evaluated := byRun[id]
		if err := addEpisodeAdoptionRun(&row, id, items, costs[id], runs[id], evaluated); err != nil {
			return row, err
		}
		for _, item := range byRun[id] {
			legacy = legacy || episodeAdoptionIdentityPrecision(item) < 2
			incompleteEvidence = incompleteEvidence || item.RunItemCount != len(byRun[id])
		}
	}
	reports := make([]AdoptionReport, 0, len(row.Items))
	for _, item := range row.Items {
		reports = append(reports, item.AdoptionReport)
	}
	row.Summary = ComputeAdoptionSummary(reports, github.LoadObjectiveMapping())
	if len(row.MissingAdoptionRunIDs) > 0 || legacy || incompleteEvidence {
		row.AdoptionCoverage = "partial"
	}
	if len(row.CostRunIDs) == 0 {
		row.CostCoverage = "unavailable"
	}
	incompleteMembership := qualifyEpisodeAdoptions(&row, episode, legacy, incompleteEvidence)
	if row.Summary.Accepted > 0 && row.CostCoverage == "available" && row.AdoptionCoverage == "complete" && !incompleteMembership {
		cost := *row.ObservedAIC / float64(row.Summary.Accepted)
		row.CostPerAcceptedAdoption = &cost
	}
	return row, nil
}

func addEpisodeAdoptionRun(row *EpisodeAdoptionData, id int64, items []EpisodeAdoptionObservation, cost episodeAdoptionCost, present, evaluated bool) error {
	if !present {
		row.MissingRunIDs = append(row.MissingRunIDs, id)
	}
	if !evaluated {
		row.MissingAdoptionRunIDs = append(row.MissingAdoptionRunIDs, id)
	} else {
		row.AdoptionRunIDs = append(row.AdoptionRunIDs, id)
		row.Items = append(row.Items, items...)
	}
	if cost.aic == nil || *cost.aic < 0 || math.IsNaN(*cost.aic) || math.IsInf(*cost.aic, 0) {
		row.MissingCostRunIDs = append(row.MissingCostRunIDs, id)
		row.CostCoverage = "partial"
	} else {
		if row.ObservedAIC == nil {
			row.ObservedAIC = new(float64)
		}
		*row.ObservedAIC += *cost.aic
		if !isFinite(*row.ObservedAIC) {
			return fmt.Errorf("observed AIC exceeds the supported range for episode %q", row.EpisodeID)
		}
		row.CostRunIDs = append(row.CostRunIDs, id)
	}
	for _, warning := range cost.warnings {
		row.Qualifications = append(row.Qualifications, fmt.Sprintf("Run %d: %s", id, warning))
		row.CostCoverage = "partial"
	}
	return nil
}

func qualifyEpisodeAdoptions(row *EpisodeAdoptionData, episode episodeAdoptionMembership, legacy, incompleteEvidence bool) bool {
	incompleteMembership := len(row.MissingRunIDs) > 0 || episode.TotalRuns != len(row.RunIDs)
	if episode.Coverage != nil {
		incompleteMembership = incompleteMembership || episode.Coverage.Status == "partial" || episode.Coverage.CollectionTruncated
		for _, reason := range episode.Coverage.Reasons {
			row.Qualifications = append(row.Qualifications, "Membership coverage: "+reason)
		}
	}
	if incompleteMembership {
		row.Qualifications = append(row.Qualifications, "Selected episode membership is incomplete or inconsistent with the supplied run records.")
	}
	if len(row.MissingAdoptionRunIDs) > 0 {
		row.Qualifications = append(row.Qualifications, "Some selected runs have no adoption observations; this does not establish that they produced no outputs.")
	}
	if legacy {
		row.Qualifications = append(row.Qualifications, "Legacy observations lack stable action identities; indistinguishable actions may be coalesced and efficiency is unavailable. Reevaluate these runs.")
	}
	if incompleteEvidence {
		row.Qualifications = append(row.Qualifications, "Adoption item counts are missing or inconsistent; the supplied observations do not establish complete evidence for selected runs.")
	}
	if row.CostCoverage != "available" {
		row.Qualifications = append(row.Qualifications, "Cost is incomplete or unavailable; observed_aic is only the sum of available usage, not an episode total.")
	}
	if row.Summary.Pending+row.Summary.Unknown+row.Summary.Errors > 0 {
		row.Qualifications = append(row.Qualifications, "Pending, unknown, and error adoptions remain unresolved; accepted counts are an observation snapshot.")
	}
	return incompleteMembership
}
