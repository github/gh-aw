package cli

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// EpisodeHop is recorded workflow invocation evidence, not a unit of accounting.
type EpisodeHop struct {
	RunID            int64    `json:"run_id"`
	Repository       string   `json:"repository,omitempty"`
	Workflow         string   `json:"workflow,omitempty"`
	HopID            string   `json:"hop_id,omitempty"`
	ParentHopID      string   `json:"parent_hop_id,omitempty"`
	CallerRunID      string   `json:"caller_run_id,omitempty"`
	CallerRepository string   `json:"caller_repository,omitempty"`
	SharedRun        bool     `json:"shared_run"`
	Evidence         []string `json:"evidence"`
	MissingEvidence  []string `json:"missing_evidence,omitempty"`
}

type EpisodeHopEdge struct {
	SourceHopID string `json:"source_hop_id"`
	TargetHopID string `json:"target_hop_id,omitempty"`
	SourceRunID int64  `json:"source_run_id,omitempty"`
	TargetRunID int64  `json:"target_run_id"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

type EpisodeRunCost struct {
	RunID      int64   `json:"run_id"`
	Repository string  `json:"repository,omitempty"`
	Tokens     int     `json:"tokens"`
	AIC        float64 `json:"aic"`
}

type EpisodeExplanation struct {
	Hops            []EpisodeHop     `json:"hops"`
	Edges           []EpisodeHopEdge `json:"edges"`
	RunEdges        []EpisodeEdge    `json:"run_edges"`
	RunCosts        []EpisodeRunCost `json:"run_costs"`
	MissingEvidence []string         `json:"missing_evidence,omitempty"`
}

func collectRunEpisodeHops(pr ProcessedRun, context *AwContext) []EpisodeHop {
	if context == nil || strings.TrimSpace(context.EpisodeID) == "" {
		return nil
	}
	infos := pr.EpisodeContexts
	var gaps []string
	if infos == nil {
		infos, gaps = readRunEpisodeContexts(pr.Run)
	}
	if !slices.ContainsFunc(infos, func(info *AwInfo) bool { return info != nil && info.Context != nil && *info.Context == *context }) {
		infos = append(slices.Clone(infos), &AwInfo{WorkflowName: pr.Run.WorkflowName, Context: context})
	}
	hops := make([]EpisodeHop, 0, len(infos))
	for _, info := range infos {
		if info == nil || info.Context == nil || strings.TrimSpace(info.Context.EpisodeID) != strings.TrimSpace(context.EpisodeID) {
			gaps = append(gaps, "conflicting_or_missing_episode_context")
			continue
		}
		ctx := info.Context
		hop := EpisodeHop{
			RunID: pr.Run.DatabaseID, Repository: pr.Run.Repository, Workflow: info.WorkflowName,
			HopID: ctx.HopID, ParentHopID: ctx.ParentHopID, CallerRunID: ctx.RunID, CallerRepository: ctx.Repo,
			Evidence: []string{"context.episode_id"},
		}
		if hop.HopID == "" {
			hop.MissingEvidence = append(hop.MissingEvidence, "missing_hop_id")
		} else {
			hop.Evidence = append(hop.Evidence, "context.hop_id")
		}
		if ctx.ParentHopID == "" && ctx.RunID != "" && ctx.RunID != strconv.FormatInt(pr.Run.DatabaseID, 10) {
			hop.MissingEvidence = append(hop.MissingEvidence, "missing_parent_hop_id")
		}
		if !slices.ContainsFunc(hops, func(existing EpisodeHop) bool { return sameEpisodeHop(existing, hop) }) {
			hops = append(hops, hop)
		}
	}
	for i := range hops {
		hops[i].MissingEvidence = append(hops[i].MissingEvidence, gaps...)
	}
	return hops
}

func sameEpisodeHop(a, b EpisodeHop) bool {
	return a.RunID == b.RunID && strings.EqualFold(a.Repository, b.Repository) &&
		a.HopID == b.HopID && a.Workflow == b.Workflow && a.ParentHopID == b.ParentHopID &&
		a.CallerRunID == b.CallerRunID && strings.EqualFold(a.CallerRepository, b.CallerRepository)
}

func readRunEpisodeContexts(run WorkflowRun) ([]*AwInfo, []string) {
	if run.LogsPath == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(run.LogsPath)
	if err != nil {
		return nil, []string{"hop_artifact_inventory_unavailable"}
	}
	var infos []*AwInfo
	var gaps []string
	for _, entry := range entries {
		if !entry.IsDir() || !artifactMatchesFilter(entry.Name(), []string{"info", "usage"}) {
			continue
		}
		path := filepath.Join(run.LogsPath, entry.Name(), "aw_info.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		info, err := parseAwInfo(path, false)
		if err != nil {
			gaps = append(gaps, "hop_artifact_metadata_unreadable")
			continue
		}
		if info.RunAttempt != "" && run.Attempt > 0 && info.RunAttempt != strconv.Itoa(run.Attempt) {
			continue
		}
		if info.RunID != 0 && int64(info.RunID) != run.DatabaseID || episodeRepositoryConflict(info.Repository, run.Repository) {
			gaps = append(gaps, "conflicting_hop_artifact_identity")
			continue
		}
		infos = append(infos, info)
	}
	return infos, gaps
}

func explainEpisode(episode EpisodeData, runs []RunData, runEdges []EpisodeEdge) *EpisodeExplanation {
	view := &EpisodeExplanation{
		Hops: []EpisodeHop{}, Edges: []EpisodeHopEdge{}, RunEdges: []EpisodeEdge{}, RunCosts: []EpisodeRunCost{},
	}
	seenRuns := make(map[int64]bool)
	for _, run := range runs {
		if seenRuns[run.RunID] || !slices.Contains(episode.RunIDs, run.RunID) {
			continue
		}
		seenRuns[run.RunID] = true
		view.RunCosts = append(view.RunCosts, EpisodeRunCost{RunID: run.RunID, Repository: run.Repository, Tokens: run.TokenUsage, AIC: runAICValue(run)})
		hops := run.EpisodeHops
		if len(hops) == 0 && run.AwContext != nil {
			hops = collectRunEpisodeHops(ProcessedRun{Run: WorkflowRun{DatabaseID: run.RunID, Repository: run.Repository, WorkflowName: run.WorkflowName}}, run.AwContext)
		}
		if len(hops) == 0 {
			view.MissingEvidence = append(view.MissingEvidence, "run "+strconv.FormatInt(run.RunID, 10)+": missing hop context")
		}
		for _, hop := range hops {
			if hop.RunID != run.RunID || episodeRepositoryConflict(hop.Repository, run.Repository) {
				view.MissingEvidence = append(view.MissingEvidence, "conflicting hop/run identity")
				continue
			}
			if !slices.ContainsFunc(view.Hops, func(existing EpisodeHop) bool { return sameEpisodeHop(existing, hop) }) {
				hop.MissingEvidence = slices.Clone(hop.MissingEvidence)
				view.Hops = append(view.Hops, hop)
			}
		}
	}
	for _, edge := range runEdges {
		if edge.EpisodeID == episode.EpisodeID {
			view.RunEdges = append(view.RunEdges, edge)
		}
	}
	finalizeEpisodeHops(view)
	return view
}

func finalizeEpisodeHops(view *EpisodeExplanation) {
	slices.SortFunc(view.Hops, func(a, b EpisodeHop) int {
		if a.RunID != b.RunID {
			return cmp.Compare(a.RunID, b.RunID)
		}
		if a.HopID != b.HopID {
			return cmp.Compare(a.HopID, b.HopID)
		}
		return cmp.Compare(a.Workflow, b.Workflow)
	})
	slices.SortFunc(view.RunCosts, func(a, b EpisodeRunCost) int { return cmp.Compare(a.RunID, b.RunID) })
	counts := make(map[int64]int)
	for _, hop := range view.Hops {
		counts[hop.RunID]++
	}
	for i := range view.Hops {
		hop := &view.Hops[i]
		hop.SharedRun = counts[hop.RunID] > 1
		if hop.ParentHopID != "" {
			edge := explainEpisodeHopEdge(*hop, view.Hops)
			view.Edges = append(view.Edges, edge)
			if edge.Reason != "" {
				hop.MissingEvidence = append(hop.MissingEvidence, edge.Reason)
			}
		}
	}
}

func explainEpisodeHopEdge(hop EpisodeHop, hops []EpisodeHop) EpisodeHopEdge {
	edge := EpisodeHopEdge{SourceHopID: hop.ParentHopID, TargetHopID: hop.HopID, TargetRunID: hop.RunID, Status: "missing", Reason: "missing_parent_hop"}
	var parents []EpisodeHop
	for _, parent := range hops {
		if parent.HopID != hop.ParentHopID {
			continue
		}
		if hop.CallerRunID != "" && hop.CallerRunID != strconv.FormatInt(parent.RunID, 10) ||
			episodeRepositoryConflict(hop.CallerRepository, parent.Repository) {
			edge.Status, edge.Reason = "conflicting", "conflicting_parent_identity"
			continue
		}
		parents = append(parents, parent)
	}
	if len(parents) == 1 {
		for _, parent := range parents {
			edge.SourceRunID, edge.Status, edge.Reason = parent.RunID, "recorded", ""
			if sameEpisodeHop(parent, hop) {
				edge.Status, edge.Reason = "conflicting", "self_referential_parent_hop"
			}
		}
	} else if len(parents) > 1 {
		edge.Status, edge.Reason = "conflicting", "ambiguous_parent_hop"
	}
	return edge
}
