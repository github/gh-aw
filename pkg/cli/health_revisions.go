package cli

import (
	"cmp"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
)

// HealthRevisionCohort is a bounded, recent sample of one AW content revision.
type HealthRevisionCohort struct {
	FrontmatterHash      string    `json:"frontmatter_hash,omitempty"`
	BodyHash             string    `json:"body_hash,omitempty"`
	CompilerVersion      string    `json:"compiler_version,omitempty"`
	SourceSHA            string    `json:"source_sha,omitempty"`
	RunIDs               []int64   `json:"run_ids"`
	Successes            int       `json:"successes"`
	Failures             int       `json:"failures"`
	SubagentFailures     int       `json:"subagent_failures"`
	DetectionFailureRuns int       `json:"detection_failure_runs"`
	Pending              int       `json:"pending"`
	IndependentRuns      int       `json:"independent_runs"`
	FirstSeen            time.Time `json:"first_seen"`
	LastSeen             time.Time `json:"last_seen"`
	lastCompleted        time.Time
	key                  string
	revisionUnknown      bool
}

type HealthRevisionStatus struct {
	WorkflowPath string                `json:"workflow_path"`
	Branch       string                `json:"branch"`
	Event        string                `json:"event"`
	Status       string                `json:"status"`
	Current      *HealthRevisionCohort `json:"current"`
	Previous     *HealthRevisionCohort `json:"previous,omitempty"`
}

func healthRunRevision(run WorkflowRun, collection *HealthCollection) (*workflow.LockMetadata, string) {
	if collection != nil {
		source, checked := collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)]
		if checked && source == "" {
			return nil, "unknown:" + run.HeadSha
		}
		if checked && healthSourceChanged(run, collection) {
			return nil, healthContentRevisionKey(nil, source)
		}
		if metadata := collection.revisions[healthRevisionLookupKey(run)]; metadata != nil {
			return metadata, healthContentRevisionKey(metadata, source)
		}
	}
	return nil, "unknown:" + run.HeadSha
}

func healthRevisionStatuses(runs []WorkflowRun, collection *HealthCollection) []HealthRevisionStatus {
	ordered := append([]WorkflowRun(nil), runs...)
	slices.SortFunc(ordered, func(a, b WorkflowRun) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.DatabaseID, a.DatabaseID))
	})
	var statuses []HealthRevisionStatus
	byScope := make(map[string]int)
	seen := make(map[int64]struct{})
	for _, run := range ordered {
		if isIgnoredHealthRun(run) {
			continue
		}
		if _, duplicate := seen[run.DatabaseID]; duplicate {
			continue
		}
		seen[run.DatabaseID] = struct{}{}
		scope := failureClusterKey(run.Repository, run.WorkflowPath, run.HeadBranch, run.Event)
		index, exists := byScope[scope]
		if !exists {
			index = len(statuses)
			byScope[scope] = index
			statuses = append(statuses, HealthRevisionStatus{WorkflowPath: run.WorkflowPath, Branch: run.HeadBranch, Event: run.Event})
		}
		metadata, key := healthRunRevision(run, collection)
		status := &statuses[index] //nolint:uncheckedsliceindex // index comes from this slice's scope map.
		if status.Current == nil && collection != nil {
			if head, checked := collection.headRevisions[healthHeadRevisionKey(run)]; checked {
				headKey := "unknown:current"
				source, sourceChecked := collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadBranch)]
				if head != nil && (!sourceChecked || source != "") {
					headKey = healthContentRevisionKey(head, source)
				}
				status.Current = newHealthRevisionCohort(head, headKey)
				status.Current.SourceSHA = source
			}
		}
		appendHealthRevisionRun(status, run, metadata, key)
		if collection != nil {
			source := collection.sourceRevisions[healthSourceRevisionKey(run, run.HeadSha)]
			if status.Current.key == key && source != "" {
				status.Current.SourceSHA = source
			} else if status.Previous != nil && status.Previous.key == key && source != "" {
				status.Previous.SourceSHA = source
			}
		}
	}
	for i := range statuses {
		statuses[i].Status = classifyHealthRevision(statuses[i])
	}
	return statuses
}

func appendHealthRevisionRun(status *HealthRevisionStatus, run WorkflowRun, metadata *workflow.LockMetadata, key string) {
	if status.Current == nil {
		status.Current = newHealthRevisionCohort(metadata, key)
	}
	unknownCurrent := status.Previous == nil && strings.HasPrefix(key, "unknown:")
	if unknownCurrent {
		// A failed lookup does not establish a revision boundary or remove current evidence.
		key = status.Current.key
	}
	cohort := status.Current
	if status.Previous != nil {
		cohort = status.Previous
	} else if cohort.key != key {
		if status.Previous == nil {
			status.Previous = newHealthRevisionCohort(metadata, key)
		}
		cohort = status.Previous
	}
	if cohort.key != key || cohort.IndependentRuns >= healthMinimumRuns {
		return
	}
	cohort.revisionUnknown = cohort.revisionUnknown || unknownCurrent
	cohort.RunIDs = append(cohort.RunIDs, run.DatabaseID)
	cohort.FirstSeen = run.CreatedAt
	if cohort.LastSeen.IsZero() {
		cohort.LastSeen = run.CreatedAt
	}
	if run.Conclusion == "success" {
		cohort.Successes++
	} else if isClusterFailureConclusion(run.Conclusion) {
		cohort.Failures++
	} else {
		cohort.Pending++
	}
	if run.Status == "completed" && run.Attempt <= 1 && (run.Conclusion == "success" || isClusterFailureConclusion(run.Conclusion)) {
		cohort.IndependentRuns++
	}
	if run.UpdatedAt.After(cohort.lastCompleted) {
		cohort.lastCompleted = run.UpdatedAt
	}
}

func newHealthRevisionCohort(metadata *workflow.LockMetadata, key string) *HealthRevisionCohort {
	cohort := &HealthRevisionCohort{key: key, RunIDs: []int64{}}
	if metadata != nil {
		cohort.FrontmatterHash, cohort.BodyHash = metadata.FrontmatterHash, metadata.BodyHash
		cohort.CompilerVersion = metadata.CompilerVersion
	}
	return cohort
}

func classifyHealthRevision(status HealthRevisionStatus) string {
	current := status.Current
	if current.revisionUnknown || strings.HasPrefix(current.key, "unknown:") {
		return "revision_unknown"
	}
	if len(current.RunIDs) == 0 {
		return "no_current_revision_runs"
	}
	if current.Failures > 0 {
		return "failures_observed"
	}
	if current.Pending > 0 || current.IndependentRuns < healthMinimumRuns {
		return "insufficient_evidence"
	}
	if status.Previous != nil && status.Previous.Failures > 0 && status.Previous.Pending == 0 &&
		!strings.HasPrefix(status.Previous.key, "unknown:") && !status.Previous.lastCompleted.IsZero() &&
		!current.FirstSeen.IsZero() && status.Previous.lastCompleted.Before(current.FirstSeen) {
		return "recovery_observed"
	}
	return "healthy"
}

func higherPriorityHealthStatus(current, candidate string) bool {
	return healthStatusPriority(candidate) > healthStatusPriority(current)
}

func healthStatusPriority(status string) int {
	switch status {
	case "failures_observed":
		return 7
	case "not_collected":
		return 6
	case "revision_unknown":
		return 5
	case "no_current_revision_runs":
		return 4
	case "insufficient_evidence":
		return 3
	case "recovery_observed":
		return 2
	case "healthy":
		return 1
	case "no_recent_runs":
		return 0
	default:
		return -1
	}
}

func healthWorkflowEvidenceEnough(runs []WorkflowRun, collection *HealthCollection) bool {
	statuses := healthRevisionStatuses(runs, collection)
	if len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		if status.Current.IndependentRuns < healthMinimumRuns && status.Previous == nil {
			return false
		}
	}
	return true
}

func calculateCurrentWorkflowHealth(name string, runs []WorkflowRun, config HealthConfig) WorkflowHealth {
	revisions := healthRevisionStatuses(runs, config.collection)
	currentRuns := currentHealthRuns(runs, revisions)
	health := CalculateWorkflowHealth(name, currentRuns, config.Threshold)
	health.Revisions = revisions
	for _, revision := range revisions {
		health.WorkflowPath = revision.WorkflowPath
		break
	}
	health.Status = "no_recent_runs"
	if len(revisions) == 0 && config.collection != nil && config.collection.stopped {
		health.Status = "not_collected"
	}
	for _, revision := range revisions {
		if higherPriorityHealthStatus(health.Status, revision.Status) {
			health.Status = revision.Status
		}
	}
	if isHealthEvidenceInsufficient(health.Status) {
		health.DisplayRate += " (insufficient evidence)"
	}
	applyHealthSubagentFailures(&health, currentRuns, config)
	applyHealthDetectionFailures(&health, currentRuns, config)
	applyHealthDurableSessions(&health, currentRuns, config)
	return health
}

func calculateHealthWorkflowReports(runs []WorkflowRun, config HealthConfig) []WorkflowHealth {
	grouped := make(map[string][]WorkflowRun)
	names := make(map[string]string)
	for _, registered := range config.collectionWorkflows() {
		grouped[registered.Path] = nil
		names[registered.Path] = registered.Name
	}
	for _, run := range runs {
		key := run.WorkflowPath
		if key == "" {
			key = run.WorkflowName
		}
		grouped[key] = append(grouped[key], run)
		if names[key] == "" {
			names[key] = run.WorkflowName
		}
	}
	var reports []WorkflowHealth
	for key, history := range grouped {
		health := calculateCurrentWorkflowHealth(names[key], history, config)
		if health.WorkflowPath == "" && strings.HasPrefix(key, logsWorkflowsPathSegment) {
			health.WorkflowPath = key
		}
		if len(history) == 0 && config.collection != nil && config.collection.Partial {
			health.Status = "not_collected"
			if _, visited := config.collection.visited[key]; visited {
				health.Status = "no_recent_runs"
			}
		}
		if config.RepoOverride == "" {
			health.IntentionalFailure = workflow.IsIntentionalFailure(health.WorkflowPath)
		}
		reports = append(reports, health)
	}
	return reports
}

func isHealthEvidenceInsufficient(status string) bool {
	switch status {
	case "no_recent_runs", "no_current_revision_runs", "revision_unknown", "insufficient_evidence", "not_collected":
		return true
	default:
		return false
	}
}

func currentHealthRuns(runs []WorkflowRun, revisions []HealthRevisionStatus) []WorkflowRun {
	currentIDs := make(map[int64]struct{})
	for _, revision := range revisions {
		for _, id := range revision.Current.RunIDs {
			currentIDs[id] = struct{}{}
		}
	}
	var currentRuns []WorkflowRun
	for _, run := range runs {
		if _, current := currentIDs[run.DatabaseID]; current {
			currentRuns = append(currentRuns, run)
			delete(currentIDs, run.DatabaseID)
		}
	}
	return currentRuns
}

func renderHealthRevisions(w io.Writer, statuses []HealthRevisionStatus) error {
	if len(statuses) == 0 {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("\n[health_revisions] minimum_completed_runs=%d; recovery is observed evidence, not proof of causation\n", healthMinimumRuns)
	for _, status := range statuses {
		current := status.Current
		out.printf("workflow=%q branch=%q event=%q status=%s source_sha=%q frontmatter_hash=%q body_hash=%q successes=%d failures=%d subagent_failures=%d detection_failure_runs=%d pending=%d runs=%s\n",
			status.WorkflowPath, status.Branch, status.Event, status.Status, current.SourceSHA, current.FrontmatterHash,
			current.BodyHash, current.Successes, current.Failures, current.SubagentFailures, current.DetectionFailureRuns, current.Pending, failureRunIDsDisplay(current.RunIDs))
		if previous := status.Previous; previous != nil {
			out.printf("  previous source_sha=%q frontmatter_hash=%q body_hash=%q successes=%d failures=%d runs=%s\n",
				previous.SourceSHA, previous.FrontmatterHash, previous.BodyHash, previous.Successes, previous.Failures, failureRunIDsDisplay(previous.RunIDs))
		}
	}
	return out.err
}
