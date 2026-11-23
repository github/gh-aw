package cli

// gh aw routing status and reset read and fence a workflow's adaptive routing
// history on memory/adaptive-routing/<workflow>, the branch the push_routing_history
// job writes (actions/setup/js/push_routing_history.cjs). They read and write the
// history's schema; applying runs is the writer's job alone.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var routingLog = logger.New("cli:routing_command")

const (
	routingHistoryBranchPrefix = "memory/adaptive-routing/"
	routingRecentRuns          = 20
	// Until live routing exists, status reports evidence only.
	routingNoRecommendation = "No recommendation yet; observe mode records evidence, and the decision engine arrives with live routing."
)

var routingWorkflowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*$`)

// routingHistoryState is state.json, version 2. Fields keep the history's order.
type routingHistoryState struct {
	V             int                             `json:"v"`
	Workflow      string                          `json:"workflow"`
	Seq           int64                           `json:"seq"`
	Generation    *routingGeneration              `json:"generation"`
	ResetAfterRun int64                           `json:"reset_after_run"`
	LastRun       int64                           `json:"last_run"`
	Unexplained   int64                           `json:"unexplained"`
	Scopes        map[string]*routingHistoryScope `json:"scopes"`
	Updated       *routingUpdated                 `json:"updated"`
}

type routingGeneration struct {
	Hash     string `json:"hash"`
	SinceRun int64  `json:"since_run"`
}

type routingUpdated struct {
	Run *string `json:"run"`
	At  string  `json:"at"`
}

type routingHistoryScope struct {
	Incumbent      *string                        `json:"incumbent"`
	LastRole       *string                        `json:"last_role"`
	LastSuccessSeq int64                          `json:"last_success_seq"`
	Pairs          map[string]*routingHistoryPair `json:"pairs"`
}

type routingHistoryPair struct {
	History      []json.RawMessage `json:"history"`
	Credits      *float64          `json:"credits"`
	ModelCredits *float64          `json:"model_credits"`
	Bills        int64             `json:"bills"`
	Streak       int64             `json:"streak"`
	Tests        int64             `json:"tests"`
	Suspended    *routingSuspended `json:"suspended"`
	Seen         string            `json:"seen"`
	Summary      *routingSummary   `json:"summary"`
}

type routingSuspended struct {
	SinceSeq int64 `json:"since_seq"`
	Pending  int64 `json:"pending"`
}

type routingSummary struct {
	Runs    int64   `json:"runs"`
	Rate    float64 `json:"rate"`
	Hopeful float64 `json:"hopeful"`
}

// routingRunLine is one runs.jsonl line.
type routingRunLine struct {
	Run     string   `json:"run"`
	Number  int64    `json:"number"`
	At      string   `json:"at"`
	Scope   *string  `json:"scope"`
	Mode    *string  `json:"mode"`
	Role    *string  `json:"role"`
	Pair    *string  `json:"pair"`
	Outcome *string  `json:"outcome"`
	Credits *float64 `json:"credits"`
	Counted bool     `json:"counted"`
	Note    *string  `json:"note"`
}

// RoutingStatus is the JSON output of gh aw routing status <workflow>.
type RoutingStatus struct {
	Workflow       string               `json:"workflow"`
	Branch         string               `json:"branch"`
	Head           string               `json:"head"`
	Generation     *routingGeneration   `json:"generation"`
	LastRun        int64                `json:"last_run"`
	ResetAfterRun  int64                `json:"reset_after_run"`
	Unexplained    int64                `json:"unexplained"`
	Updated        *routingUpdated      `json:"updated"`
	Recommendation *string              `json:"recommendation"`
	Note           string               `json:"note"`
	Scopes         []RoutingScopeStatus `json:"scopes"`
	Recent         RoutingRecentRuns    `json:"recent"`
}

// RoutingScopeStatus is one scope's evidence.
type RoutingScopeStatus struct {
	Scope     string              `json:"scope"`
	Incumbent *string             `json:"incumbent"`
	LastRole  *string             `json:"last_role"`
	Pairs     []RoutingPairStatus `json:"pairs"`
}

// RoutingPairStatus is one pair's evidence, with its rates as of the last write.
type RoutingPairStatus struct {
	Pair         string            `json:"pair"`
	Runs         int64             `json:"runs"`
	Rate         *float64          `json:"rate"`
	Hopeful      *float64          `json:"hopeful"`
	Credits      *float64          `json:"credits"`
	ModelCredits *float64          `json:"model_credits"`
	Bills        int64             `json:"bills"`
	Streak       int64             `json:"streak"`
	Tests        int64             `json:"tests"`
	Suspended    *routingSuspended `json:"suspended"`
}

// RoutingRecentRuns summarizes the newest runs.jsonl lines.
type RoutingRecentRuns struct {
	Runs       int              `json:"runs"`
	Counted    int              `json:"counted"`
	NotCounted map[string]int   `json:"not_counted"`
	Lines      []routingRunLine `json:"lines"`
}

// RoutingHistoryListEntry is one workflow's history in gh aw routing status without a workflow.
type RoutingHistoryListEntry struct {
	Workflow    string `json:"workflow"`
	Branch      string `json:"branch"`
	Scopes      int    `json:"scopes"`
	Pairs       int    `json:"pairs"`
	LastRun     int64  `json:"last_run"`
	Unexplained int64  `json:"unexplained"`
	UpdatedAt   string `json:"updated_at"`
}

// routingAPI is the GitHub access the commands need.
type routingAPI interface {
	// BranchHead returns the branch's head commit, or "" when the branch does not exist.
	BranchHead(ctx context.Context, branch string) (string, error)
	// ReadFile returns a file at a commit, or os.ErrNotExist.
	ReadFile(ctx context.Context, ref, path string) ([]byte, error)
	// HistoryBranches lists the branches under memory/adaptive-routing/.
	HistoryBranches(ctx context.Context) ([]string, error)
	// LatestRunNumber returns the workflow's newest run number, or 0 without runs.
	LatestRunNumber(ctx context.Context, workflowFile string) (int64, error)
	// CommitFile commits one file on the branch only while its head is still expectedHead.
	CommitFile(ctx context.Context, branch, expectedHead, path string, content []byte, headline string) (string, error)
}

// NewRoutingCommand creates the routing command with its subcommands.
func NewRoutingCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Show or reset what adaptive routing has recorded for a workflow",
		Long: `Show or reset what adaptive routing has recorded for a workflow.

A workflow with adaptive-routing: observe keeps its ordinary router pick, and each
run's model and effort, verdict and cost are recorded on its
memory/adaptive-routing/<workflow> branch.

Available subcommands:
  - status - Show each workflow's routing evidence
  - reset  - Start a workflow's routing history afresh`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` routing status                  # List the routing histories in this repository
  ` + string(constants.CLIExtensionPrefix) + ` routing status pr-sous-chef     # Show one workflow's evidence
  ` + string(constants.CLIExtensionPrefix) + ` routing reset pr-sous-chef      # Start that workflow's history afresh`,
	}
	cmd.AddCommand(newRoutingStatusSubcommand(), newRoutingResetSubcommand())
	return cmd
}

func newRoutingStatusSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [workflow]",
		Short: "Show a workflow's adaptive routing evidence",
		Long: `Show a workflow's adaptive routing evidence.

For each scope, status shows the incumbent (in observe mode, the ordinary pick
runs) and, for each model and effort, its runs, expected and hopeful success
rates as of the last write, average credits, streak, tests and suspension. It
also counts recent runs that did not count, by reason. Without a workflow it
lists every routing history in the repository.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` routing status
  ` + string(constants.CLIExtensionPrefix) + ` routing status pr-sous-chef --json
  ` + string(constants.CLIExtensionPrefix) + ` routing status pr-sous-chef --repo owner/repo`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repo, _ := cmd.Flags().GetString("repo")
			api, err := newGHRoutingAPI(repo)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				return runRoutingStatus(cmd.Context(), api, args[0], os.Stdout, jsonOutput)
			}
			return runRoutingList(cmd.Context(), api, os.Stdout, jsonOutput)
		},
	}
	addJSONFlag(cmd)
	addRepoFlag(cmd)
	return cmd
}

func newRoutingResetSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset <workflow>",
		Short: "Start a workflow's adaptive routing history afresh",
		Long: `Start a workflow's adaptive routing history afresh.

Reset reads the workflow's latest run number and, in one commit that lands only
if no run saved in the meantime, stops every run up to it from counting, empties
the recorded evidence and lets exploration resume. It keeps runs.jsonl and never
deletes the branch. It needs write access to the repository.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` routing reset pr-sous-chef
  ` + string(constants.CLIExtensionPrefix) + ` routing reset pr-sous-chef --repo owner/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			api, err := newGHRoutingAPI(repo)
			if err != nil {
				return err
			}
			return runRoutingReset(cmd.Context(), api, args[0], time.Now().UTC()) //nolint:uncheckedsliceindex // Cobra ExactArgs validates one argument.
		},
	}
	addRepoFlag(cmd)
	return cmd
}

func routingBranch(workflowID string) (string, error) {
	if len(workflowID) > 100 || strings.HasSuffix(workflowID, ".lock") || !routingWorkflowIDPattern.MatchString(workflowID) {
		return "", fmt.Errorf("%q is not a workflow ID; use the workflow file name without .md, for example pr-sous-chef", workflowID)
	}
	return routingHistoryBranchPrefix + workflowID, nil
}

func readRoutingState(ctx context.Context, api routingAPI, head, workflowID string) (*routingHistoryState, error) {
	data, err := api.ReadFile(ctx, head, "state.json")
	if err != nil {
		return nil, fmt.Errorf("could not read state.json: %w", err)
	}
	var state routingHistoryState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("state.json is not valid JSON: %w", err)
	}
	if state.V != 2 || state.Workflow != workflowID {
		return nil, fmt.Errorf("state.json is not a version 2 routing history for %s; update gh aw", workflowID)
	}
	return &state, nil
}

func readRoutingRuns(ctx context.Context, api routingAPI, head string) ([]routingRunLine, error) {
	data, err := api.ReadFile(ctx, head, "runs.jsonl")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read runs.jsonl: %w", err)
	}
	var lines []routingRunLine
	for raw := range bytes.SplitSeq(data, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line routingRunLine
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, fmt.Errorf("runs.jsonl has an invalid line: %w", err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// buildRoutingStatus turns the history into the status view, pairs by most runs first.
func buildRoutingStatus(workflowID, branch, head string, state *routingHistoryState, lines []routingRunLine) RoutingStatus {
	status := RoutingStatus{
		Workflow: workflowID, Branch: branch, Head: head, Generation: state.Generation, LastRun: state.LastRun,
		ResetAfterRun: state.ResetAfterRun, Unexplained: state.Unexplained, Updated: state.Updated,
		Note: routingNoRecommendation, Scopes: []RoutingScopeStatus{},
	}
	for _, name := range slices.Sorted(maps.Keys(state.Scopes)) {
		scope := state.Scopes[name]
		view := RoutingScopeStatus{Scope: name, Incumbent: scope.Incumbent, LastRole: scope.LastRole, Pairs: []RoutingPairStatus{}}
		for pairName, pair := range scope.Pairs {
			entry := RoutingPairStatus{Pair: pairName, Credits: pair.Credits, ModelCredits: pair.ModelCredits, Bills: pair.Bills,
				Streak: pair.Streak, Tests: pair.Tests, Suspended: pair.Suspended}
			if pair.Summary != nil {
				entry.Runs, entry.Rate, entry.Hopeful = pair.Summary.Runs, &pair.Summary.Rate, &pair.Summary.Hopeful
			}
			view.Pairs = append(view.Pairs, entry)
		}
		slices.SortFunc(view.Pairs, func(a, b RoutingPairStatus) int {
			if a.Runs != b.Runs {
				return int(b.Runs - a.Runs)
			}
			return strings.Compare(a.Pair, b.Pair)
		})
		status.Scopes = append(status.Scopes, view)
	}
	recent := lines[max(0, len(lines)-routingRecentRuns):]
	status.Recent = RoutingRecentRuns{Runs: len(recent), NotCounted: map[string]int{}, Lines: recent}
	for _, line := range recent {
		if line.Counted {
			status.Recent.Counted++
			continue
		}
		reason := "unknown"
		if line.Note != nil {
			reason = *line.Note
		}
		status.Recent.NotCounted[reason]++
	}
	return status
}

func runRoutingStatus(ctx context.Context, api routingAPI, workflowID string, out io.Writer, jsonOutput bool) error {
	branch, err := routingBranch(workflowID)
	if err != nil {
		return err
	}
	head, err := api.BranchHead(ctx, branch)
	if err != nil {
		return err
	}
	if head == "" {
		return fmt.Errorf("no routing history for %s: %s does not exist. Add adaptive-routing: observe to the workflow and let it run", workflowID, branch)
	}
	state, err := readRoutingState(ctx, api, head, workflowID)
	if err != nil {
		return err
	}
	lines, err := readRoutingRuns(ctx, api, head)
	if err != nil {
		return err
	}
	status := buildRoutingStatus(workflowID, branch, head, state, lines)
	if jsonOutput {
		encoded, err := marshalIndentJSONOrWrap(status, "routing status")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	}
	return printRoutingStatus(os.Stderr, status)
}

func formatRoutingFloat(value *float64, format string) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf(format, *value)
}

func printRoutingStatus(w io.Writer, status RoutingStatus) error {
	var text strings.Builder
	line := func(parts ...string) {
		for _, part := range parts {
			text.WriteString(part)
		}
		text.WriteByte('\n')
	}
	line(console.FormatInfoMessage(fmt.Sprintf("Routing history for %s (%s)", status.Workflow, status.Branch)))
	if status.Updated != nil {
		line("  Updated:     ", status.Updated.At)
	}
	line("  Last run:    ", strconv.FormatInt(status.LastRun, 10))
	if status.Generation != nil {
		line("  Generation:  since run ", strconv.FormatInt(status.Generation.SinceRun, 10))
	}
	if status.ResetAfterRun > 0 {
		line("  Reset after: run ", strconv.FormatInt(status.ResetAfterRun, 10))
	}
	line("  Unexplained: ", strconv.FormatInt(status.Unexplained, 10))
	line(console.FormatInfoMessage(status.Note))
	if len(status.Scopes) == 0 {
		line("\nNo evidence recorded yet.")
	}
	for _, scope := range status.Scopes {
		text.WriteString("\n")
		text.WriteString(routingScopeTable(scope))
	}
	if recent := status.Recent; recent.Runs > 0 {
		summary := fmt.Sprintf("\nRecent runs (last %d): %d counted", recent.Runs, recent.Counted)
		if len(recent.NotCounted) > 0 {
			reasons := make([]string, 0, len(recent.NotCounted))
			for _, reason := range slices.Sorted(maps.Keys(recent.NotCounted)) {
				reasons = append(reasons, fmt.Sprintf("%s %d", reason, recent.NotCounted[reason]))
			}
			summary += "; did not count: " + strings.Join(reasons, ", ")
		}
		line(summary)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func routingScopeTable(scope RoutingScopeStatus) string {
	title := scope.Scope
	switch {
	case scope.LastRole != nil && *scope.LastRole == "observe":
		title += " (observe: ordinary pick)"
	case scope.Incumbent != nil:
		title += " (incumbent " + *scope.Incumbent + ")"
	}
	rows := make([][]string, 0, len(scope.Pairs))
	for _, pair := range scope.Pairs {
		suspended := "-"
		if pair.Suspended != nil {
			suspended = fmt.Sprintf("%d pending", pair.Suspended.Pending)
		}
		rows = append(rows, []string{pair.Pair, strconv.FormatInt(pair.Runs, 10), formatRoutingFloat(pair.Rate, "%.3f"),
			formatRoutingFloat(pair.Hopeful, "%.3f"), formatRoutingFloat(pair.Credits, "%.1f"), strconv.FormatInt(pair.Bills, 10),
			strconv.FormatInt(pair.Streak, 10), strconv.FormatInt(pair.Tests, 10), suspended})
	}
	return console.RenderTable(console.TableConfig{
		Title:    title,
		Headers:  []string{"Pair", "Runs", "Expected", "Hopeful", "Credits", "Bills", "Streak", "Tests", "Suspended"},
		Rows:     rows,
		MaxWidth: console.DefaultTableWidth,
	})
}

func runRoutingList(ctx context.Context, api routingAPI, out io.Writer, jsonOutput bool) error {
	branches, err := api.HistoryBranches(ctx)
	if err != nil {
		return err
	}
	entries := []RoutingHistoryListEntry{}
	for _, branch := range branches {
		workflowID := strings.TrimPrefix(branch, routingHistoryBranchPrefix)
		head, err := api.BranchHead(ctx, branch)
		if err != nil || head == "" {
			continue
		}
		state, err := readRoutingState(ctx, api, head, workflowID)
		if err != nil {
			routingLog.Printf("Skipping %s: %v", branch, err)
			continue
		}
		entry := RoutingHistoryListEntry{Workflow: workflowID, Branch: branch, Scopes: len(state.Scopes), LastRun: state.LastRun, Unexplained: state.Unexplained}
		for _, scope := range state.Scopes {
			entry.Pairs += len(scope.Pairs)
		}
		if state.Updated != nil {
			entry.UpdatedAt = state.Updated.At
		}
		entries = append(entries, entry)
	}
	if jsonOutput {
		encoded, err := marshalIndentJSONOrWrap(entries, "routing status")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No routing histories found (branches matching "+routingHistoryBranchPrefix+"*)."))
		return nil
	}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{entry.Workflow, strconv.Itoa(entry.Scopes), strconv.Itoa(entry.Pairs), strconv.FormatInt(entry.LastRun, 10),
			strconv.FormatInt(entry.Unexplained, 10), entry.UpdatedAt})
	}
	fmt.Fprint(os.Stderr, console.RenderTable(console.TableConfig{
		Title:    "Routing histories",
		Headers:  []string{"Workflow", "Scopes", "Pairs", "Last run", "Unexplained", "Updated"},
		Rows:     rows,
		MaxWidth: console.DefaultTableWidth,
	}))
	return nil
}

// resetRoutingState fences every run up to latestRun, empties the scopes and lets exploration
// resume, as the writer's resetState does.
func resetRoutingState(state routingHistoryState, latestRun int64, at string) routingHistoryState {
	state.ResetAfterRun = latestRun
	state.Scopes = map[string]*routingHistoryScope{}
	state.Unexplained = 0
	state.Updated = &routingUpdated{Run: nil, At: at}
	return state
}

func encodeRoutingState(state routingHistoryState) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(state); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func runRoutingReset(ctx context.Context, api routingAPI, workflowID string, now time.Time) error {
	branch, err := routingBranch(workflowID)
	if err != nil {
		return err
	}
	head, err := api.BranchHead(ctx, branch)
	if err != nil {
		return err
	}
	if head == "" {
		return fmt.Errorf("no routing history for %s: %s does not exist", workflowID, branch)
	}
	state, err := readRoutingState(ctx, api, head, workflowID)
	if err != nil {
		return err
	}
	latest, err := api.LatestRunNumber(ctx, workflowID+".lock.yml")
	if err != nil {
		return err
	}
	content, err := encodeRoutingState(resetRoutingState(*state, latest, now.Format(time.RFC3339)))
	if err != nil {
		return err
	}
	commit, err := api.CommitFile(ctx, branch, head, "state.json", content, fmt.Sprintf("Reset the routing history after run %d", latest))
	if err != nil {
		return fmt.Errorf("could not reset %s: %w", branch, err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf(
		"Reset the routing history for %s: runs up to %d no longer count, and exploration can resume. runs.jsonl is kept (commit %s).", workflowID, latest, commit)))
	return nil
}

// ghRoutingAPI implements routingAPI with gh api.
type ghRoutingAPI struct {
	repo string
}

func newGHRoutingAPI(repo string) (*ghRoutingAPI, error) {
	if repo == "" {
		current, err := GetCurrentRepoSlug()
		if err != nil {
			return nil, fmt.Errorf("could not determine the repository; pass --repo owner/repo: %w", err)
		}
		repo = current
	}
	return &ghRoutingAPI{repo: repo}, nil
}

func (g *ghRoutingAPI) BranchHead(ctx context.Context, branch string) (string, error) {
	args, err := repositoryAPIArgs(g.repo, "", "git/ref/heads/"+escapeEndpoint(branch), "--jq", ".object.sha")
	if err != nil {
		return "", err
	}
	out, err := workflow.RunGHContext(ctx, "Reading the routing history...", args...)
	if err != nil {
		if errorutil.IsNotFoundError(err) {
			return "", nil
		}
		return "", fmt.Errorf("could not read %s: %w", branch, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *ghRoutingAPI) ReadFile(ctx context.Context, ref, path string) ([]byte, error) {
	return readRemoteRepoBranchFileContext(ctx, g.repo, ref, path, "")
}

func (g *ghRoutingAPI) HistoryBranches(ctx context.Context) ([]string, error) {
	args, err := repositoryAPIArgs(g.repo, "", "git/matching-refs/heads/"+escapeEndpoint(routingHistoryBranchPrefix), "--paginate", "--jq", ".[].ref")
	if err != nil {
		return nil, err
	}
	out, err := workflow.RunGHContext(ctx, "Listing routing histories...", args...)
	if err != nil {
		return nil, fmt.Errorf("could not list routing histories: %w", err)
	}
	var branches []string
	for ref := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if branch, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/"); ok {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func (g *ghRoutingAPI) LatestRunNumber(ctx context.Context, workflowFile string) (int64, error) {
	args, err := repositoryAPIArgs(g.repo, "", fmt.Sprintf("actions/workflows/%s/runs", escapeEndpoint(workflowFile)),
		"--method", "GET", "-f", "per_page=1", "--jq", ".workflow_runs[0].run_number // 0")
	if err != nil {
		return 0, err
	}
	out, err := workflow.RunGHContext(ctx, "Reading the latest run...", args...)
	if err != nil {
		return 0, fmt.Errorf("could not read the latest run of %s: %w", workflowFile, err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

const routingCommitMutation = `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) { commit { oid } }
}`

func (g *ghRoutingAPI) CommitFile(ctx context.Context, branch, expectedHead, path string, content []byte, headline string) (string, error) {
	ownerRepo, host := repoutil.NormalizeRepoForAPI(g.repo)
	request, err := json.Marshal(map[string]any{
		"query": routingCommitMutation,
		"variables": map[string]any{"input": map[string]any{
			"branch":          map[string]string{"repositoryNameWithOwner": ownerRepo, "branchName": branch},
			"message":         map[string]string{"headline": headline},
			"expectedHeadOid": expectedHead,
			"fileChanges":     map[string]any{"additions": []map[string]string{{"path": path, "contents": base64.StdEncoding.EncodeToString(content)}}},
		}},
	})
	if err != nil {
		return "", err
	}
	args := []string{"api", "graphql", "--input", "-", "--jq", ".data.createCommitOnBranch.commit.oid"}
	if host != "" {
		args = append(args, "--hostname", host)
	}
	out, err := workflow.RunGHInputContext(ctx, "Resetting the routing history...", bytes.NewReader(request), args...)
	if err != nil {
		return "", fmt.Errorf("the commit was refused (another run may have saved first; run the command again): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
