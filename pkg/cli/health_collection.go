package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// HealthCollection describes a bounded sample, including any quota-related truncation.
type HealthCollection struct {
	ProducerVersion           string                              `json:"producer_version,omitempty"`
	ReportSchemaVersion       int                                 `json:"report_schema_version,omitempty"`
	ObservedAt                time.Time                           `json:"observed_at,omitzero"`
	Capabilities              []string                            `json:"capabilities,omitempty"`
	WorkQueue                 *HealthWorkQueue                    `json:"work_queue,omitempty"`
	CacheEvidence             map[int64]HealthCacheEvidence       `json:"cache_evidence,omitempty"`
	JobEvidence               map[int64]HealthJobEvidence         `json:"job_evidence,omitempty"`
	JobRequests               int                                 `json:"job_requests"`
	Partial                   bool                                `json:"partial"`
	Reason                    string                              `json:"reason,omitempty"`
	Warning                   string                              `json:"warning,omitempty"`
	Host                      string                              `json:"host"`
	APIRateLimit              *GitHubAPIRateLimitState            `json:"api_rate_limit,omitempty"`
	RevisionLookups           int                                 `json:"revision_lookups"`
	APIRequests               int                                 `json:"api_requests"`
	MinimumRuns               int                                 `json:"minimum_runs"`
	MaximumDays               int                                 `json:"maximum_days"`
	StartDate                 string                              `json:"start_date,omitempty"`
	EndDate                   string                              `json:"end_date,omitempty"`
	FailurePassComplete       bool                                `json:"failure_pass_complete"`
	FailureListingComplete    bool                                `json:"failure_listing_complete"`
	FailureCandidates         int                                 `json:"failure_candidates"`
	FailureCandidateWorkflows int                                 `json:"failure_candidate_workflows"`
	PotentialIssues           []HealthPotentialIssue              `json:"potential_issues,omitempty"`
	PRLifecycleEvidence       map[int64]HealthPRLifecycleEvidence `json:"pr_lifecycle_evidence,omitempty"`
	LifecycleRequests         int                                 `json:"lifecycle_requests"`
	ApprovalExpiryEvidence    map[int64]HealthApprovalEvidence    `json:"approval_expiry_evidence,omitempty"`
	ApprovalRequests          int                                 `json:"approval_requests"`
	TimeoutSeconds            float64                             `json:"timeout_seconds,omitempty"`
	ElapsedSeconds            float64                             `json:"elapsed_seconds"`
	ctx                       context.Context
	startedAt                 time.Time
	failureInventory          []WorkflowRun
	lifecycleResponses        map[string]healthLifecycleResponse
	workflows                 []GitHubWorkflow
	revisions                 map[string]*workflow.LockMetadata
	headRevisions             map[string]*workflow.LockMetadata
	sourceRevisions           map[string]string
	fileSnapshots             map[string]healthFileSnapshot
	lockSnapshots             map[string]healthLockSnapshot
	localGit                  *healthLocalGit
	localGitChecked           bool
	LocalRevisionReads        int `json:"local_revision_reads"`
	checkedRequests           int
	repoOverride              string
	stopped                   bool
	visited                   map[string]struct{}
	jobPresence               map[healthRunAttempt]bool
	jobs                      map[healthRunAttempt]*healthJobs
	cacheSummaries            map[healthRunAttempt]*RunSummary
	failureScopes             map[string]map[string]struct{}
}

const healthMinimumRuns = 5
const healthMaximumAPIRequests = 1000

var errHealthAPIBudget = errors.New("health API request budget reached")
var healthRunGH = workflow.RunGHCombinedContext

func (collection *HealthCollection) context() context.Context {
	if collection.ctx != nil {
		return collection.ctx
	}
	return context.Background()
}

func checkHealthTimeBudget(collection *HealthCollection) error {
	if err := collection.context().Err(); err != nil {
		stopHealthCollection(collection, err)
		return err
	}
	return nil
}

func finishHealthCollection(collection *HealthCollection) {
	if collection == nil || collection.startedAt.IsZero() {
		return
	}
	checkHealthTimeBudget(collection)
	elapsed := time.Since(collection.startedAt)
	collection.ElapsedSeconds = elapsed.Seconds()
	healthLog.Printf("Health collection finished: elapsed=%s api_requests=%d partial=%t reason=%s",
		elapsed.Round(time.Millisecond), collection.APIRequests, collection.Partial, collection.Reason)
}

func checkHealthAPIBudget(collection *HealthCollection) error {
	if err := checkHealthTimeBudget(collection); err != nil {
		return err
	}
	if collection.APIRequests >= healthMaximumAPIRequests {
		return errHealthAPIBudget
	}
	if collection.APIRateLimit != nil && collection.APIRequests-collection.checkedRequests < 20 &&
		collection.APIRateLimit.Remaining-(collection.APIRequests-collection.checkedRequests) > RateLimitThreshold {
		return nil
	}
	ctx, cancel := context.WithTimeout(collection.context(), 10*time.Second)
	defer cancel()
	state, err := fetchRateLimitForHostFunc(ctx, collection.Host)
	if err != nil {
		if budgetErr := checkHealthTimeBudget(collection); budgetErr != nil {
			return budgetErr
		}
		healthLog.Printf("Health API budget unavailable: %v", err)
		collection.Warning = "GitHub API quota could not be checked: " + err.Error()
		return fmt.Errorf("cannot check health API budget: %w", err)
	}
	collection.APIRateLimit = &state
	collection.checkedRequests = collection.APIRequests
	if state.Remaining <= RateLimitThreshold {
		return fmt.Errorf("%w: %d core requests remain on %s; resets at %s",
			errLogsAPIRateLimitReached, state.Remaining, collection.Host,
			time.Unix(state.Reset, 0).UTC().Format(time.RFC3339))
	}
	return nil
}

func stopHealthCollection(collection *HealthCollection, err error) {
	collection.Partial = true
	collection.stopped = true
	collection.Reason = "api_error"
	if errorutil.IsRateLimitError(err.Error()) || errors.Is(err, errLogsAPIRateLimitReached) {
		collection.Reason = "rate_limit"
	} else if errors.Is(err, errHealthAPIBudget) {
		collection.Reason = "request_budget"
	} else if errors.Is(err, context.DeadlineExceeded) && errors.Is(collection.context().Err(), context.DeadlineExceeded) {
		collection.Reason = "time_budget"
	} else if errors.Is(err, context.Canceled) && errors.Is(collection.context().Err(), context.Canceled) {
		collection.Reason = "canceled"
	}
	collection.Warning = err.Error()
	if collection.Reason == "time_budget" {
		collection.Warning = "Health collection time budget exhausted; results are partial. Increase --timeout or narrow the workflow/time window for more evidence."
	}
	healthLog.Printf("Health collection stopped: %s", collection.Warning)
}

func healthAPI(collection *HealthCollection, endpoint string, extra ...string) ([]byte, error) {
	if err := checkHealthAPIBudget(collection); err != nil {
		stopHealthCollection(collection, err)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(collection.context(), 30*time.Second)
	defer cancel()
	args := []string{"api", "--hostname", collection.Host, endpoint}
	args = append(args, extra...)
	collection.APIRequests++
	started := time.Now()
	// Only log the endpoint path, never response bodies or request headers.
	endpointPath, _, _ := strings.Cut(endpoint, "?")
	healthLog.Printf("Health API request %d started: %s", collection.APIRequests, endpointPath)
	out, err := healthRunGH(ctx, "Fetching health evidence...", args...)
	healthLog.Printf("Health API request %d finished: elapsed=%s failed=%t", collection.APIRequests, time.Since(started).Round(time.Millisecond), err != nil)
	if budgetErr := checkHealthTimeBudget(collection); budgetErr != nil {
		return nil, budgetErr
	}
	if err != nil {
		err = fmt.Errorf("health API request failed: %w: %s", err, strings.TrimSpace(string(out)))
		message := err.Error()
		rateLimited := errorutil.IsRateLimitError(message)
		// HTML failures may include full pages; bound the error before warnings or debug logging.
		if slices.Contains(extra, "Accept: text/html") && (len(out) > 0 || len(message) > 256 || strings.Contains(message, "<")) {
			err = errors.New("health HTML request failed: response omitted")
			if rateLimited {
				err = fmt.Errorf("health HTML request failed: %w", errLogsAPIRateLimitReached)
			}
		}
		if rateLimited {
			stopHealthCollection(collection, err)
		}
	}
	return out, err
}

func (config HealthConfig) collectionWorkflows() []GitHubWorkflow {
	if config.collection == nil {
		return nil
	}
	return config.collection.workflows
}

func renderHealthCollection(w io.Writer, collection *HealthCollection) error {
	if collection == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n[health_evidence] producer=%q schema=%d observed_at=%s api_requests=%d job_requests=%d; bounded sample, not a fleet failure rate\n",
		collection.ProducerVersion, collection.ReportSchemaVersion, collection.ObservedAt.Format(time.RFC3339),
		collection.APIRequests, collection.JobRequests); err != nil {
		return err
	}
	if err := renderHealthCacheEvidence(w, collection.CacheEvidence); err != nil {
		return err
	}
	if err := renderHealthWorkQueue(w, collection.WorkQueue); err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(collection.ApprovalExpiryEvidence)) {
		evidence := collection.ApprovalExpiryEvidence[id]
		if _, err := fmt.Fprintf(w, "[health_approval_expiry] run=%d attempt=%d status=%s reason=%q url=%s\n",
			id, evidence.Attempt, evidence.Status, evidence.Reason, evidence.URL); err != nil {
			return err
		}
	}
	for _, id := range slices.Sorted(maps.Keys(collection.PRLifecycleEvidence)) {
		evidence := collection.PRLifecycleEvidence[id]
		if _, err := fmt.Fprintf(w, "[health_pr_lifecycle] run=%d status=%s reason=%q pr=%d event_at=%s\n",
			id, evidence.Status, evidence.Reason, evidence.Number, evidence.EventAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if collection.FailureCandidates > 0 {
		if _, err := fmt.Fprintf(w, "\n[health_candidates] failed_runs=%d workflows=%d listing_complete=%t; list-only signals may include jobless or superseded runs, not a fleet failure rate\n",
			collection.FailureCandidates, collection.FailureCandidateWorkflows, collection.FailureListingComplete); err != nil {
			return err
		}
	}
	if !collection.Partial && collection.Warning == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "\n[health_collection]\npartial=%t reason=%s warning=%q\n",
		collection.Partial, collection.Reason, collection.Warning)
	return err
}
