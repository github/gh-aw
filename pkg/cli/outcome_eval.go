package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/intent"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var outcomeEvalLog = logger.New("cli:outcome_eval")

var objectiveMappingGHAPIGetArray = ghAPIGetArray
var objectiveMappingGHAPIGraphQL = ghAPIGraphQL

// OutcomeReport is the result of evaluating one safe output item.
type OutcomeReport struct {
	OutcomeEvaluation
	Type               string   `json:"type" console:"header:Type"`
	ObjectURL          string   `json:"object_url,omitempty" console:"header:URL,omitempty"`
	ObjectNumber       int      `json:"object_number,omitempty" console:"header:#,omitempty"`
	TracedRootURL      string   `json:"traced_root_url,omitempty" console:"-"`
	AttributionStatus  string   `json:"attribution_status,omitempty" console:"-"`
	AttributionSource  string   `json:"attribution_source,omitempty" console:"-"`
	Repo               string   `json:"repo,omitempty" console:"header:Repo,omitempty"`
	Detail             string   `json:"detail,omitempty" console:"header:Detail,omitempty"`
	TimeToOutcomeHours float64  `json:"time_to_outcome_hours,omitempty" console:"header:Time,omitempty"`
	HumanComments      int      `json:"human_comments,omitempty" console:"header:Comments,omitempty"`
	HumanEdits         int      `json:"human_edits,omitempty" console:"header:Edits,omitempty"`
	HumanReviews       int      `json:"human_reviews,omitempty" console:"header:Reviews,omitempty"`
	ZeroTouch          bool     `json:"zero_touch,omitempty" console:"header:Zero-touch,omitempty"`
	ObjectiveValue     int      `json:"objective_value,omitempty" console:"header:Obj Value,omitempty"`
	ObjectiveLabels    []string `json:"objective_labels,omitempty" console:"-"`
	CreatedAt          string   `json:"created_at" console:"-"`
	CheckedAt          string   `json:"checked_at" console:"-"`
	EvalError          string   `json:"eval_error,omitempty" console:"-"`
}

// OutcomeSummary aggregates outcomes across multiple safe output items.
type OutcomeSummary struct {
	Total                   int               `json:"total" console:"header:Total"`
	Accepted                int               `json:"accepted" console:"header:Accepted"`
	Rejected                int               `json:"rejected" console:"header:Rejected"`
	Ignored                 int               `json:"ignored" console:"header:Ignored"`
	Pending                 int               `json:"pending" console:"header:Pending"`
	AcceptedStrong          int               `json:"accepted_strong,omitempty"`
	AcceptedMedium          int               `json:"accepted_medium,omitempty"`
	AcceptedWeak            int               `json:"accepted_weak,omitempty"`
	FallbackExistsOnlyCount int               `json:"fallback_exists_only_count,omitempty"`
	Lifecycle               int               `json:"lifecycle" console:"header:Lifecycle"`
	Errors                  int               `json:"errors" console:"header:Errors"`
	ZeroTouch               int               `json:"zero_touch" console:"header:Zero-touch"`
	AcceptanceRate          float64           `json:"acceptance_rate" console:"header:Acceptance Rate"`
	WasteRate               float64           `json:"waste_rate" console:"header:Waste Rate"`
	ZeroTouchRate           float64           `json:"zero_touch_rate" console:"header:Zero-touch Rate"`
	MedianTimeToOutcome     float64           `json:"median_time_to_outcome_hours,omitempty"`
	CostPerAcceptedOutcome  float64           `json:"cost_per_accepted_outcome,omitempty"`
	TotalObjectiveValue     int               `json:"total_objective_value,omitempty" console:"header:Total Obj Value"`
	AcceptedObjectiveValue  int               `json:"accepted_objective_value,omitempty" console:"header:Accepted Obj Value"`
	ObjectiveEfficiency     float64           `json:"objective_efficiency,omitempty" console:"header:Obj Efficiency"`
	DomainBreakdowns        []DomainBreakdown `json:"domain_breakdowns,omitempty" console:"-"`
}

// outcomeEvaluator is a function that evaluates one safe output item.
type outcomeEvaluator func(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport

// outcomeEvaluators maps safe output types to their evaluator functions.
var outcomeEvaluators = map[string]outcomeEvaluator{
	"create_pull_request":                   evalCreatePullRequest,
	"create_issue":                          evalCreateIssue,
	"update_issue":                          evalUpdateIssue,
	"update_pull_request":                   evalUpdatePullRequest,
	"add_comment":                           evalAddComment,
	"add_labels":                            evalAddLabels,
	"replace_label":                         evalReplaceLabel,
	"assign_to_agent":                       evalAssignToAgent,
	"close_issue":                           evalCloseSticky,
	"close_pull_request":                    evalCloseSticky,
	"close_discussion":                      evalCloseDiscussion,
	"create_discussion":                     evalCreateDiscussion,
	"hide_comment":                          evalHideComment,
	"assign_milestone":                      evalAssignMilestone,
	"create_pull_request_review_comment":    evalReviewComment,
	"resolve_pull_request_review_thread":    evalResolveThread,
	"mark_pull_request_as_ready_for_review": evalMarkReady,
	"push_to_pull_request_branch":           evalPushToPRBranch,
	"add_reviewer":                          evalAddReviewer,
	"submit_pull_request_review":            evalSubmitPullRequestReview,
	"dispatch_workflow":                     evalDispatchWorkflow,
	"update_discussion":                     evalUpdateDiscussion,
}

// EvaluateOutcomes checks the current state of all safe output items from a run.
// The mapping parameter is required and defines how labels map to objective values.
func EvaluateOutcomes(ctx context.Context, items []CreatedItemReport, repoOverride string, mapping *github.ObjectiveMapping) []OutcomeReport {
	outcomeEvalLog.Printf("Evaluating outcomes: items=%d, repo_override=%q", len(items), repoOverride)
	if repoOverride == "" {
		slug, err := GetCurrentRepoSlug()
		if err == nil {
			repoOverride = slug
			outcomeEvalLog.Printf("Resolved repo override from current repo slug: %s", repoOverride)
		}
	}

	reports := make([]OutcomeReport, 0, len(items))
	skipped := 0
	for _, item := range items {
		if item.Type == "noop" || item.Type == "missing_tool" || item.Type == "missing_data" || item.Type == "report_incomplete" {
			skipped++
			continue
		}
		repo := item.Repo
		if repo == "" {
			repo = repoOverride
		}
		eval, ok := outcomeEvaluators[item.Type]
		if !ok {
			outcomeEvalLog.Printf("No evaluator registered for type %q, using generic sticky", item.Type)
			eval = evalGenericSticky
		}
		report := eval(ctx, item, repo)
		report.CreatedAt = item.Timestamp
		report.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		report.OutcomeEvaluation = normalizeOutcomeEvaluation(report)

		// Compute objective value from issue/PR labels
		enrichOutcomeWithObjectiveValue(ctx, &report, repo, mapping)

		reports = append(reports, report)
	}
	outcomeEvalLog.Printf("Outcome evaluation complete: reports=%d, skipped=%d", len(reports), skipped)
	return reports
}

// ComputeOutcomeSummary aggregates outcome reports into a summary.
// The mapping parameter is required and defines how labels map to objective values.
func ComputeOutcomeSummary(reports []OutcomeReport, mapping *github.ObjectiveMapping) OutcomeSummary {
	s := OutcomeSummary{Total: len(reports)}
	var times []float64
	for _, r := range reports {
		eval := normalizeOutcomeEvaluation(r)
		switch eval.OutcomeStatus {
		case OutcomeStatusAccepted:
			s.Accepted++
			switch eval.EvidenceStrength {
			case EvidenceStrong:
				s.AcceptedStrong++
			case EvidenceMedium:
				s.AcceptedMedium++
			case EvidenceWeak:
				s.AcceptedWeak++
			}
			if r.ZeroTouch {
				s.ZeroTouch++
			}
		case OutcomeStatusRejected:
			s.Rejected++
		case OutcomeStatusIgnored:
			s.Ignored++
		case OutcomeStatusPending:
			s.Pending++
		}
		if eval.Signal == "target_exists_only" {
			s.FallbackExistsOnlyCount++
		}
		switch eval.OutcomeStatus {
		case OutcomeStatusLifecycle, OutcomeStatusLifecycleClose:
			s.Lifecycle++
		case OutcomeStatusError:
			s.Errors++
		}
		if r.TimeToOutcomeHours > 0 {
			times = append(times, r.TimeToOutcomeHours)
		}

		// Aggregate objective values
		s.TotalObjectiveValue += r.ObjectiveValue
		if eval.OutcomeStatus == OutcomeStatusAccepted {
			s.AcceptedObjectiveValue += r.ObjectiveValue
		}
	}
	resolved := s.Accepted + s.Rejected
	if resolved > 0 {
		s.AcceptanceRate = float64(s.Accepted) / float64(resolved)
	}
	if s.Total > 0 {
		s.WasteRate = float64(s.Rejected) / float64(s.Total)
	}
	if s.Accepted > 0 {
		s.ZeroTouchRate = float64(s.ZeroTouch) / float64(s.Accepted)
	}
	if len(times) > 0 {
		s.MedianTimeToOutcome = medianFloat(times)
	}

	// Compute objective efficiency
	if s.TotalObjectiveValue > 0 {
		s.ObjectiveEfficiency = float64(s.AcceptedObjectiveValue) / float64(s.TotalObjectiveValue)
	}

	// Compute domain breakdowns
	s.DomainBreakdowns = ComputeDomainBreakdowns(reports)

	return s
}

// escapeOwnerRepo URL-path-encodes each component of an "owner/repo" string to
// prevent path traversal when the value is interpolated into an API URL.
func escapeOwnerRepo(ownerRepo string) string {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) == 2 {
		return strings.Join([]string{url.PathEscape(parts[0]), url.PathEscape(parts[1])}, "/")
	}
	return url.PathEscape(ownerRepo)
}

func validateAPIEndpoint(endpoint string) error {
	if strings.HasPrefix(endpoint, "/") {
		return errors.New("endpoint must not start with '/'")
	}
	if slices.Contains(strings.Split(endpoint, "/"), "..") {
		return errors.New("endpoint must not contain '..' path segments")
	}
	return nil
}

// escapeEndpoint URL-path-encodes each segment of an endpoint string to
// prevent path injection when the value is interpolated into an API URL.
func escapeEndpoint(endpoint string) string {
	parts := strings.Split(endpoint, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ghAPIGet calls the GitHub REST API via gh cli and returns the parsed JSON.
func ghAPIGet(ctx context.Context, endpoint string, repo string) (map[string]any, error) {
	if err := validateAPIEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	ownerRepo, host := repoutil.NormalizeRepoForAPI(repo)
	outcomeEvalLog.Printf("gh api GET: repo=%s, endpoint=%s, host=%q", ownerRepo, endpoint, host)
	args := []string{"api", fmt.Sprintf("repos/%s/%s", escapeOwnerRepo(ownerRepo), escapeEndpoint(endpoint))}
	var output []byte
	var err error
	if host != "" {
		output, err = workflow.RunGHContextWithHost(ctx, "Checking outcome...", host, args...)
	} else {
		output, err = workflow.RunGH("Checking outcome...", args...)
	}
	if err != nil {
		outcomeEvalLog.Printf("gh api GET failed: endpoint=%s, err=%v", endpoint, err)
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parsing response for %s: %w", endpoint, err)
	}
	return result, nil
}

// ghAPIGetArray calls the GitHub REST API and returns a JSON array.
func ghAPIGetArray(ctx context.Context, endpoint string, repo string) ([]map[string]any, error) {
	if err := validateAPIEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	ownerRepo, host := repoutil.NormalizeRepoForAPI(repo)
	args := []string{"api", fmt.Sprintf("repos/%s/%s", escapeOwnerRepo(ownerRepo), escapeEndpoint(endpoint))}
	var output []byte
	var err error
	if host != "" {
		output, err = workflow.RunGHContextWithHost(ctx, "Checking outcome...", host, args...)
	} else {
		output, err = workflow.RunGH("Checking outcome...", args...)
	}
	if err != nil {
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	var result []map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parsing response for %s: %w", endpoint, err)
	}
	return result, nil
}

// buildGraphQLArgs builds the `gh api graphql` argument list for a query and its
// variables. It is a pure function so the CLI encoding can be unit tested in
// isolation from the actual `gh` invocation. Variable keys are emitted in sorted
// order for deterministic argument lists. String values use `-f` (raw field) so
// gh's `@file` / `{placeholder}` expansion can't be triggered by interpolated
// values; int and bool values use `-F` for correct GraphQL typing. Any other
// variable type is rejected explicitly rather than silently formatted.
func buildGraphQLArgs(query string, variables map[string]any) ([]string, error) {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		switch value := variables[name].(type) {
		case string:
			// -f sends the value literally, avoiding gh's @file / {placeholder} expansion.
			args = append(args, "-f", name+"="+value)
		case int, int32, int64, bool:
			args = append(args, "-F", fmt.Sprintf("%s=%v", name, value))
		default:
			return nil, fmt.Errorf("buildGraphQLArgs: unsupported variable type %T for key %q", value, name)
		}
	}
	return args, nil
}

// ghAPIGraphQL calls the GitHub GraphQL API via gh cli and returns the parsed JSON.
// Values are passed as GraphQL variables rather than interpolated into the query text.
func ghAPIGraphQL(ctx context.Context, query string, variables map[string]any, repo string) (map[string]any, error) {
	ownerRepo, host := repoutil.NormalizeRepoForAPI(repo)
	args, err := buildGraphQLArgs(query, variables)
	if err != nil {
		return nil, err
	}
	var output []byte
	if host != "" {
		output, err = workflow.RunGHContextWithHost(ctx, "Checking outcome...", host, args...)
	} else {
		output, err = workflow.RunGH("Checking outcome...", args...)
	}
	if err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parsing graphql response for %s: %w", ownerRepo, err)
	}
	return result, nil
}

// timeBetween computes hours between two ISO timestamps.
func timeBetween(from, to string) float64 {
	t1, err1 := time.Parse(time.RFC3339, from)
	t2, err2 := time.Parse(time.RFC3339, to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return t2.Sub(t1).Hours()
}

// parseNumberFromURL extracts a number from a GitHub URL like
// https://github.com/owner/repo/pull/42 or .../issues/108
func parseNumberFromURL(url string) int {
	parts := strings.Split(url, "/")
	for i := range slices.Backward(parts) {
		var n int
		if _, err := fmt.Sscanf(parts[i], "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// parseRepoFromURL extracts owner/repo from a GitHub URL.
func parseRepoFromURL(url string) string {
	// https://github.com/owner/repo/...
	const prefix = "github.com/"
	_, rest, found := strings.Cut(url, prefix)
	if !found {
		return ""
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "/")
	}
	return ""
}

// isBotUser returns true if the login looks like a bot account.
func isBotUser(login string) bool {
	return strings.HasSuffix(login, "[bot]") || login == "github-actions" || login == "copilot-swe-agent"
}

// resolveItemRepo returns the repo to use for API calls, preferring the item's repo field.
func resolveItemRepo(item CreatedItemReport, repoOverride string) string {
	if item.Repo != "" {
		return item.Repo
	}
	if item.URL != "" {
		if r := parseRepoFromURL(item.URL); r != "" {
			return r
		}
	}
	return repoOverride
}

// resolveItemNumber returns the object number, trying item.Number first then URL parsing.
func resolveItemNumber(item CreatedItemReport) int {
	if item.Number > 0 {
		return item.Number
	}
	if item.URL != "" {
		return parseNumberFromURL(item.URL)
	}
	return 0
}

// enrichOutcomeWithObjectiveValue computes the objective value for an outcome by fetching
// its associated issue/PR labels and applying the label-to-value mapping.
func enrichOutcomeWithObjectiveValue(ctx context.Context, report *OutcomeReport, repo string, mapping *github.ObjectiveMapping) {
	if report == nil || mapping == nil {
		return
	}

	// Only compute objective values for items that have a GitHub object number
	num := report.ObjectNumber
	if num == 0 || repo == "" {
		return
	}

	// Skip types that don't have associated labels on issues/PRs
	if report.Type == "noop" || report.Type == "missing_tool" || report.Type == "missing_data" || report.Type == "report_incomplete" {
		return
	}

	outcomeEvalLog.Printf("Computing objective value: type=%s, repo=%s, number=%d", report.Type, repo, num)

	resolvedIntent, err := resolveOutcomeIntent(ctx, *report, repo, mapping)
	if err != nil {
		outcomeEvalLog.Printf("Could not trace root for objective value computation: %v", err)
		return
	}
	report.AttributionStatus = string(resolvedIntent.Status)
	report.AttributionSource = string(resolvedIntent.Source)
	report.TracedRootURL = resolvedIntent.RootURL

	labelNames := resolvedIntent.Labels
	if len(labelNames) > 0 {
		outcomeEvalLog.Printf("Fetched root labels for %s#%d: root=%s labels=%v", repo, num, resolvedIntent.RootURL, labelNames)
	}

	// Compute objective value
	objectiveValue := mapping.ComputeObjectiveValue(labelNames)
	objectiveLabels := mapping.FilterObjectiveLabels(labelNames)

	report.ObjectiveValue = objectiveValue
	report.ObjectiveLabels = objectiveLabels
	outcomeEvalLog.Printf("Computed objective value for %s#%d: value=%d, labels=%v", repo, num, objectiveValue, objectiveLabels)
}

func resolveOutcomeIntent(ctx context.Context, report OutcomeReport, repo string, mapping *github.ObjectiveMapping) (intent.IntentRecord, error) {
	resolver := intent.Resolver{
		ResolverVersion: "outcome-eval-v1",
		MatchLabels: func(labels []string) []string {
			return mapping.FilterObjectiveLabels(labels)
		},
	}

	if isPullRequestOutcomeType(report.Type) {
		prIntent, err := resolvePullRequestIntent(ctx, report, repo, resolver)
		if err == nil {
			return prIntent, nil
		}
		if err != nil {
			outcomeEvalLog.Printf("Falling back to direct labels after PR root trace failure: %v", err)
		}
	}

	labels, err := objectiveMappingGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/labels", report.ObjectNumber), repo)
	if err != nil {
		return intent.IntentRecord{}, err
	}
	return resolver.ResolveIssue("", report.ObjectURL, labelsToStringsFromMaps(labels)), nil
}

func isPullRequestOutcomeType(outcomeType string) bool {
	switch outcomeType {
	case "create_pull_request", "update_pull_request", "create_pull_request_review_comment",
		"resolve_pull_request_review_thread", "mark_pull_request_as_ready_for_review",
		"push_to_pull_request_branch", "add_reviewer", "submit_pull_request_review":
		return true
	default:
		return false
	}
}

func resolvePullRequestIntent(ctx context.Context, report OutcomeReport, repo string, resolver intent.Resolver) (intent.IntentRecord, error) {
	prData, err := loadPullRequestIntentData(ctx, report, repo)
	if err != nil {
		return intent.IntentRecord{}, err
	}
	return resolver.ResolvePullRequest(prData), nil
}

func loadPullRequestIntentData(ctx context.Context, report OutcomeReport, repo string) (intent.PullRequestData, error) {
	prNumber := report.ObjectNumber
	ownerRepo, _ := repoutil.NormalizeRepoForAPI(repo)
	owner, name, found := strings.Cut(ownerRepo, "/")
	if !found || owner == "" || name == "" {
		return intent.PullRequestData{}, fmt.Errorf("invalid repo for root tracing: %s", repo)
	}

	query := `query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequest(number: $number) {
				id
				closingIssuesReferences(first: 10) {
					nodes {
						id
						number
						url
						labels(first: 20) {
							nodes { name }
						}
					}
				}
			}
		}
	}`
	variables := map[string]any{
		"owner":  owner,
		"name":   name,
		"number": prNumber,
	}

	result, err := objectiveMappingGHAPIGraphQL(ctx, query, variables, repo)
	if err != nil {
		return intent.PullRequestData{}, err
	}
	data, _ := result["data"].(map[string]any)
	repository, _ := data["repository"].(map[string]any)
	pullRequest, _ := repository["pullRequest"].(map[string]any)
	prData := intent.PullRequestData{URL: report.ObjectURL}
	if nodeID, ok := pullRequest["id"].(string); ok {
		prData.NodeID = nodeID
	}
	closingRefs, _ := pullRequest["closingIssuesReferences"].(map[string]any)
	nodes, _ := closingRefs["nodes"].([]any)
	if len(nodes) == 0 {
		labels, labelErr := objectiveMappingGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/labels", report.ObjectNumber), repo)
		if labelErr != nil {
			return intent.PullRequestData{}, labelErr
		}
		prData.Labels = labelsToStringsFromMaps(labels)
		return prData, nil
	}

	prData.ClosingIssues = make([]intent.RootReference, 0, len(nodes))
	for _, node := range nodes {
		rootNode, _ := node.(map[string]any)
		root := intent.RootReference{Type: "issue"}
		if nodeID, ok := rootNode["id"].(string); ok {
			root.NodeID = nodeID
		}
		if url, ok := rootNode["url"].(string); ok {
			root.URL = url
		}
		if labels, ok := rootNode["labels"].(map[string]any); ok {
			if labelNodes, ok := labels["nodes"].([]any); ok {
				root.Labels = labelsToStringsFromNodes(labelNodes)
			}
		}
		prData.ClosingIssues = append(prData.ClosingIssues, root)
	}

	return prData, nil
}

func labelsToStringsFromNodes(nodes []any) []string {
	return collectLabelNames(nodes, func(node any) (string, bool) {
		labelMap, _ := node.(map[string]any)
		name, ok := labelMap["name"].(string)
		return name, ok
	})
}

// labelsToStringsFromMaps converts GitHub API label map objects to string slice.
func labelsToStringsFromMaps(labels []map[string]any) []string {
	return collectLabelNames(labels, func(labelMap map[string]any) (string, bool) {
		name, ok := labelMap["name"].(string)
		return name, ok
	})
}

func collectLabelNames[T any](labels []T, nameOf func(T) (string, bool)) []string {
	if len(labels) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		if name, ok := nameOf(label); ok {
			result = append(result, name)
		}
	}
	return result
}
