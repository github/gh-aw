package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/intent"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/safeoutput"
)

var adoptionEvalLog = logger.New("cli:adoption_eval")

var objectiveMappingGHAPIGetArray = ghAPIGetArray
var objectiveMappingGHAPIGraphQL = ghAPIGraphQL

// AdoptionReport is the result of evaluating one safe output item.
type AdoptionReport struct {
	AdoptionEvaluation
	ItemID                    string                         `json:"item_id,omitempty" console:"-"`
	Type                      string                         `json:"type" console:"header:Type"`
	ObjectURL                 string                         `json:"object_url,omitempty" console:"header:URL,omitempty"`
	ObjectNumber              int                            `json:"object_number,omitempty" console:"header:#,omitempty"`
	TracedRootURL             string                         `json:"traced_root_url,omitempty" console:"-"`
	AttributionStatus         string                         `json:"attribution_status,omitempty" console:"-"`
	AttributionSource         string                         `json:"attribution_source,omitempty" console:"-"`
	Repo                      string                         `json:"repo,omitempty" console:"header:Repo,omitempty"`
	Detail                    string                         `json:"detail,omitempty" console:"header:Detail,omitempty"`
	TimeToAdoptionHours       float64                        `json:"time_to_adoption_hours,omitempty" console:"header:Time,omitempty"`
	HumanComments             int                            `json:"human_comments,omitempty" console:"header:Comments,omitempty"`
	HumanEdits                int                            `json:"human_edits,omitempty" console:"header:Edits,omitempty"`
	HumanReviews              int                            `json:"human_reviews,omitempty" console:"header:Reviews,omitempty"`
	ZeroTouch                 bool                           `json:"zero_touch,omitempty" console:"header:Zero-touch,omitempty"`
	ZeroTouchEvidenceComplete bool                           `json:"zero_touch_evidence_complete" console:"-"`
	ObjectiveValue            int                            `json:"objective_value,omitempty" console:"header:Obj Value,omitempty"`
	ObjectiveLabels           []string                       `json:"objective_labels,omitempty" console:"-"`
	CreatedAt                 string                         `json:"created_at" console:"-"`
	CheckedAt                 string                         `json:"checked_at" console:"-"`
	Delivery                  *safeoutput.DeliveryEvaluation `json:"delivery,omitempty" console:"-"`
	EvalError                 string                         `json:"eval_error,omitempty" console:"-"`
}

// AdoptionSummary aggregates adoptions across multiple safe output items.
type AdoptionSummary struct {
	Total                   int               `json:"total" console:"header:Total"`
	Accepted                int               `json:"accepted" console:"header:Accepted"`
	Rejected                int               `json:"rejected" console:"header:Rejected"`
	Pending                 int               `json:"pending" console:"header:Pending"`
	NotApplicable           int               `json:"not_applicable" console:"header:N/A"`
	Unknown                 int               `json:"unknown" console:"header:Unknown"`
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
	MedianTimeToAdoption    float64           `json:"median_time_to_adoption_hours,omitempty"`
	CostPerAcceptedAdoption float64           `json:"cost_per_accepted_adoption,omitempty"`
	TotalObjectiveValue     int               `json:"total_objective_value,omitempty" console:"header:Total Obj Value"`
	AcceptedObjectiveValue  int               `json:"accepted_objective_value,omitempty" console:"header:Accepted Obj Value"`
	ObjectiveEfficiency     float64           `json:"objective_efficiency,omitempty" console:"header:Obj Efficiency"`
	DomainBreakdowns        []DomainBreakdown `json:"domain_breakdowns,omitempty" console:"-"`
}

// adoptionEvaluator is a function that evaluates one safe output item.
type adoptionEvaluator func(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport

// adoptionEvaluators maps safe output types to their evaluator functions.
var adoptionEvaluators = map[string]adoptionEvaluator{
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

// EvaluateAdoptions checks the current state of all safe output items from a run.
// The mapping parameter is required and defines how labels map to objective values.
func EvaluateAdoptions(ctx context.Context, items []CreatedItemReport, repoOverride string, mapping *github.ObjectiveMapping) []AdoptionReport {
	return evaluateAdoptions(ctx, items, repoOverride, mapping, adoptionAPIHostForRepo(repoOverride))
}

func evaluateAdoptions(ctx context.Context, items []CreatedItemReport, repoOverride string, mapping *github.ObjectiveMapping, trustedHost string) []AdoptionReport {
	ctx = withAdoptionAPICache(ctx)
	adoptionEvalLog.Printf("Evaluating adoptions: items=%d, repo_override=%q", len(items), repoOverride)
	if repoOverride == "" {
		slug, err := GetCurrentRepoSlug()
		if err == nil {
			repoOverride = slug
			adoptionEvalLog.Printf("Resolved repo override from current repo slug: %s", repoOverride)
		}
	}
	if trustedHost == "" {
		trustedHost = adoptionAPIHostForRepo(repoOverride)
	}

	reports := make([]AdoptionReport, 0, len(items))
	occurrences := make(map[[sha256.Size]byte]int)
	skipped := 0
	for _, item := range items {
		if item.Type == "noop" || item.Type == "report_missing_tool" || item.Type == "missing_tool" || item.Type == "report_missing_data" || item.Type == "missing_data" || item.Type == "report_incomplete" {
			skipped++
			continue
		}
		repo := resolveItemRepo(item, repoOverride)
		itemID := adoptionItemID(item, occurrences)
		if !sameAdoptionAPIHost(repo, trustedHost) {
			report := AdoptionReport{
				ItemID: itemID, Type: item.Type, ObjectURL: item.URL,
				ObjectNumber: resolveItemNumber(item), Repo: repo,
				CreatedAt: item.Timestamp, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
				EvalError:          "item repository host does not match workflow run host",
				AdoptionEvaluation: adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "untrusted_host"),
			}
			report.Source = "adoption-evaluator"
			attachAdoptionRuleMetadata(&report, item.Type)
			reports = append(reports, report)
			continue
		}
		eval, ok := adoptionEvaluators[item.Type]
		if !ok {
			adoptionEvalLog.Printf("No evaluator registered for type %q, using generic sticky", item.Type)
			eval = evalGenericSticky
		}
		report := eval(ctx, item, repo)
		report.ItemID = itemID
		report.CreatedAt = item.Timestamp
		report.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		adoptionAt := report.AdoptionAt
		report.AdoptionEvaluation = normalizeAdoptionEvaluation(report)
		report.AdoptionAt = adoptionAt
		finalizeAdoptionEvaluation(&report, item)

		// Compute objective value from issue/PR labels
		enrichAdoptionWithObjectiveValue(ctx, &report, repo, mapping)

		reports = append(reports, report)
	}
	adoptionEvalLog.Printf("Adoption evaluation complete: reports=%d, skipped=%d", len(reports), skipped)
	return reports
}

func sameAdoptionAPIHost(targetRepo, runRepo string) bool {
	_, targetHost := repoutil.NormalizeRepoForAPI(targetRepo)
	if targetHost == "" {
		targetHost = lookupEnv("GH_HOST")
	}
	return strings.EqualFold(normalizedGitHubAPIHost(targetHost), normalizedGitHubAPIHost(runRepo))
}

func adoptionAPIHostForRepo(repo string) string {
	_, host := repoutil.NormalizeRepoForAPI(repo)
	if host == "" {
		host = lookupEnv("GH_HOST")
	}
	return strings.ToLower(normalizedGitHubAPIHost(host))
}

func adoptionItemID(item CreatedItemReport, occurrences map[[sha256.Size]byte]int) string {
	source, err := json.Marshal(item)
	if err != nil {
		adoptionEvalLog.Printf("Cannot identify safe output item: %v", err)
		return ""
	}
	digest := sha256.Sum256(source)
	// Only indistinguishable source records need an occurrence number.
	occurrences[digest]++
	return fmt.Sprintf("item-sha256-%x-%d", digest, occurrences[digest])
}

// ComputeAdoptionSummary aggregates adoption reports into a summary.
// The mapping parameter is required and defines how labels map to objective values.
func ComputeAdoptionSummary(reports []AdoptionReport, mapping *github.ObjectiveMapping) AdoptionSummary {
	s := AdoptionSummary{Total: len(reports)}
	var times []float64
	for _, r := range reports {
		eval := normalizeAdoptionEvaluation(r)
		s.countAdoption(r, eval)
		if r.TimeToAdoptionHours > 0 {
			times = append(times, r.TimeToAdoptionHours)
		}

		// Aggregate objective values
		s.TotalObjectiveValue += r.ObjectiveValue
		if eval.AdoptionStatus == AdoptionStatusAccepted {
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
		s.MedianTimeToAdoption = medianFloat(times)
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
	owner, repo, found := strings.Cut(ownerRepo, "/")
	if found {
		return strings.Join([]string{url.PathEscape(owner), url.PathEscape(repo)}, "/")
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
	ownerRepo, _ := repoutil.NormalizeRepoForAPI(repo)
	args := []string{"api", fmt.Sprintf("repos/%s/%s", escapeOwnerRepo(ownerRepo), escapeEndpoint(endpoint))}
	result, err := requestAdoptionJSON[map[string]any](ctx, repo, args)
	if err != nil {
		adoptionEvalLog.Printf("gh api GET failed: endpoint=%s, err=%v", endpoint, err)
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	return result, nil
}

// ghAPIGetArray calls the GitHub REST API and returns a JSON array.
func ghAPIGetArray(ctx context.Context, endpoint string, repo string) ([]map[string]any, error) {
	if err := validateAPIEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if labels, ok := adoptionCachedLabels(ctx, endpoint, repo); ok {
		return labels, nil
	}
	ownerRepo, _ := repoutil.NormalizeRepoForAPI(repo)
	args := []string{"api", fmt.Sprintf("repos/%s/%s", escapeOwnerRepo(ownerRepo), escapeEndpoint(endpoint)),
		"--method", "GET", "-f", "per_page=100", "--paginate", "--slurp"}
	pages, err := requestAdoptionJSON[[][]map[string]any](ctx, repo, args)
	if err != nil {
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	var result []map[string]any
	for _, page := range pages {
		result = append(result, page...)
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
	args, err := buildGraphQLArgs(query, variables)
	if err != nil {
		return nil, err
	}
	result, err := requestAdoptionJSON[map[string]any](ctx, repo, args)
	if err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
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

func setAdoptionDecisionTime(report *AdoptionReport, createdAt, decisionAt string) {
	if report == nil || decisionAt == "" {
		return
	}
	if _, err := time.Parse(time.RFC3339, decisionAt); err != nil {
		return
	}
	report.AdoptionAt = decisionAt
	if createdAt != "" {
		report.TimeToAdoptionHours = timeBetween(createdAt, decisionAt)
	}
}

// parseNumberFromURL extracts a number from a GitHub URL like
// https://github.com/owner/repo/pull/42 or .../issues/108
func parseNumberFromURL(url string) int {
	clean, _, _ := strings.Cut(url, "#")
	clean, _, _ = strings.Cut(clean, "?")
	nextIsNumber := false
	for part := range strings.SplitSeq(clean, "/") {
		if nextIsNumber {
			n, err := strconv.Atoi(part)
			if err == nil && n > 0 {
				return n
			}
		}
		nextIsNumber = part == "issues" || part == "pull" || part == "discussions"
	}
	return 0
}

// isBotUser returns true if the login looks like a bot account.
func isBotUser(login string) bool {
	login = strings.ToLower(login)
	return strings.HasSuffix(login, "[bot]") || login == "github-actions" || login == "copilot-swe-agent"
}

// resolveItemRepo prefers the item's repository, inheriting its URL or run host when hostless.
func resolveItemRepo(item CreatedItemReport, repoOverride string) string {
	repo, host := repoutil.NormalizeRepoForAPI(item.Repo)
	if host != "" {
		return item.Repo
	}
	_, host = repoutil.NormalizeRepoForAPI(repoOverride)
	if objectURL, err := url.Parse(item.URL); err == nil && objectURL.Host != "" &&
		(objectURL.Scheme == "https" || objectURL.Scheme == "http") {
		owner, rest, found := strings.Cut(strings.Trim(objectURL.Path, "/"), "/")
		repository, _, _ := strings.Cut(rest, "/")
		if found && owner != "" && repository != "" {
			if repo == "" {
				repo = strings.Join([]string{owner, repository}, "/")
			}
			// An explicit object URL can target a different host from the run.
			if objectURL.Host != "github.com" || host != "" {
				return strings.Join([]string{objectURL.Host, repo}, "/")
			}
			return repo
		}
	}
	if repo == "" {
		return repoOverride
	}
	if host != "" {
		return strings.Join([]string{host, repo}, "/")
	}
	return repo
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

// enrichAdoptionWithObjectiveValue computes the objective value for an adoption by fetching
// its associated issue/PR labels and applying the label-to-value mapping.
func enrichAdoptionWithObjectiveValue(ctx context.Context, report *AdoptionReport, repo string, mapping *github.ObjectiveMapping) {
	if report == nil || mapping == nil {
		return
	}

	// Only compute objective values for items that have a GitHub object number
	num := report.ObjectNumber
	if num == 0 || repo == "" {
		return
	}

	// Skip types that don't have associated labels on issues/PRs
	if report.Type == "noop" || report.Type == "report_missing_tool" || report.Type == "missing_tool" || report.Type == "report_missing_data" || report.Type == "missing_data" || report.Type == "report_incomplete" {
		return
	}

	adoptionEvalLog.Printf("Computing objective value: type=%s, repo=%s, number=%d", report.Type, repo, num)

	resolvedIntent, err := resolveAdoptionIntent(ctx, *report, repo, mapping)
	if err != nil {
		adoptionEvalLog.Printf("Could not trace root for objective value computation: %v", err)
		return
	}
	report.AttributionStatus = string(resolvedIntent.Status)
	report.AttributionSource = string(resolvedIntent.Source)
	report.TracedRootURL = resolvedIntent.RootURL

	labelNames := resolvedIntent.Labels
	if len(labelNames) > 0 {
		adoptionEvalLog.Printf("Fetched root labels for %s#%d: root=%s labels=%v", repo, num, resolvedIntent.RootURL, labelNames)
	}

	// Compute objective value
	objectiveValue := mapping.ComputeObjectiveValue(labelNames)
	objectiveLabels := mapping.FilterObjectiveLabels(labelNames)

	report.ObjectiveValue = objectiveValue
	report.ObjectiveLabels = objectiveLabels
	adoptionEvalLog.Printf("Computed objective value for %s#%d: value=%d, labels=%v", repo, num, objectiveValue, objectiveLabels)
}

func resolveAdoptionIntent(ctx context.Context, report AdoptionReport, repo string, mapping *github.ObjectiveMapping) (intent.IntentRecord, error) {
	resolver := intent.Resolver{
		ResolverVersion: "adoption-eval-v1",
		MatchLabels: func(labels []string) []string {
			return mapping.FilterObjectiveLabels(labels)
		},
	}

	if isPullRequestAdoptionType(report.Type) {
		prIntent, err := resolvePullRequestIntent(ctx, report, repo, resolver)
		if err == nil {
			return prIntent, nil
		}
		if err != nil {
			adoptionEvalLog.Printf("Falling back to direct labels after PR root trace failure: %v", err)
		}
	}

	labels, err := objectiveMappingGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/labels", report.ObjectNumber), repo)
	if err != nil {
		return intent.IntentRecord{}, err
	}
	return resolver.ResolveIssue("", report.ObjectURL, labelsToStringsFromMaps(labels)), nil
}

func isPullRequestAdoptionType(adoptionType string) bool {
	switch adoptionType {
	case "create_pull_request", "update_pull_request", "create_pull_request_review_comment",
		"resolve_pull_request_review_thread", "mark_pull_request_as_ready_for_review",
		"push_to_pull_request_branch", "add_reviewer", "submit_pull_request_review":
		return true
	default:
		return false
	}
}

func resolvePullRequestIntent(ctx context.Context, report AdoptionReport, repo string, resolver intent.Resolver) (intent.IntentRecord, error) {
	prData, err := loadPullRequestIntentData(ctx, report, repo)
	if err != nil {
		return intent.IntentRecord{}, err
	}
	return resolver.ResolvePullRequest(prData), nil
}

func loadPullRequestIntentData(ctx context.Context, report AdoptionReport, repo string) (intent.PullRequestData, error) {
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
	data := adoptionValue[map[string]any](result["data"])
	repository := adoptionValue[map[string]any](data["repository"])
	pullRequest := adoptionValue[map[string]any](repository["pullRequest"])
	prData := intent.PullRequestData{URL: report.ObjectURL}
	if nodeID, ok := pullRequest["id"].(string); ok {
		prData.NodeID = nodeID
	}
	closingRefs := adoptionValue[map[string]any](pullRequest["closingIssuesReferences"])
	nodes := adoptionValue[[]any](closingRefs["nodes"])
	if len(nodes) == 0 {
		labels, labelErr := objectiveMappingGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/labels", report.ObjectNumber), repo)
		if labelErr != nil {
			return intent.PullRequestData{}, labelErr
		}
		prData.Labels = labelsToStringsFromMaps(labels)
		return prData, nil
	}

	prData.ClosingIssues = adoptionClosingIssueReferences(nodes)

	return prData, nil
}

func labelsToStringsFromNodes(nodes []any) []string {
	return collectLabelNames(nodes, func(node any) (string, bool) {
		labelMap := adoptionValue[map[string]any](node)
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

func (s *AdoptionSummary) countAdoption(r AdoptionReport, eval AdoptionEvaluation) {
	switch eval.AdoptionStatus {
	case AdoptionStatusAccepted:
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
	case AdoptionStatusRejected:
		s.Rejected++
	case AdoptionStatusPending:
		s.Pending++
	case AdoptionStatusNotApplicable:
		s.NotApplicable++
	case AdoptionStatusUnknown:
		s.Unknown++
	}
	if eval.Signal == "target_exists_only" {
		s.FallbackExistsOnlyCount++
	}
	if eval.AdoptionStatus == AdoptionStatusLifecycle {
		s.Lifecycle++
	}
	if r.EvalError != "" {
		s.Errors++
	}
}

func adoptionClosingIssueReferences(nodes []any) []intent.RootReference {
	var prData intent.PullRequestData
	prData.ClosingIssues = make([]intent.RootReference, 0, len(nodes))
	for _, node := range nodes {
		rootNode := adoptionValue[map[string]any](node)
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

	return prData.ClosingIssues
}
