package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

const (
	doctorAssignmentRunLimit     = 5
	doctorAssignmentRunLookback  = 7 * 24 * time.Hour
	doctorAssignmentRequestLimit = 40
	doctorTokenRunLogLimitBytes  = 1 << 20
)

// DoctorAssignmentRunHistory describes a bounded historical sample, not current token validity.
type DoctorAssignmentRunHistory struct {
	Since time.Time                     `json:"since"`
	Until time.Time                     `json:"until"`
	Limit int                           `json:"limit"`
	Runs  []DoctorAssignmentRunEvidence `json:"runs"`
	Error string                        `json:"error,omitempty"`
}

// DoctorAssignmentRunEvidence contains recognized handler outcomes, never raw log text.
type DoctorAssignmentRunEvidence struct {
	RunID                int64     `json:"run_id"`
	Attempt              int       `json:"attempt"`
	URL                  string    `json:"url,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	Conclusion           string    `json:"conclusion"`
	Outcome              string    `json:"outcome"`
	Assigned             int       `json:"assigned"`
	AlreadyAssigned      int       `json:"already_assigned"`
	Message              string    `json:"message"`
	credentialDiagnostic bool
}

type doctorAssignmentRunMiner struct {
	ctx          context.Context
	repo         string
	host         string
	now          time.Time
	requests     int
	runGH        func(context.Context, string, ...string) ([]byte, error)
	runGHLimited func(context.Context, int, ...string) ([]byte, bool, error)
	classify     func(string, DoctorAssignmentRunEvidence) DoctorAssignmentRunEvidence
	allJobs      bool
	limit        int
	maxLogBytes  int
	logBytes     int
}

type doctorAssignmentRunMetadata struct {
	ID         int64     `json:"id"`
	Attempt    int       `json:"run_attempt"`
	URL        string    `json:"html_url"`
	CreatedAt  time.Time `json:"created_at"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
}

func collectDoctorAssignmentRunEvidence(ctx context.Context, repo, host string, checks []DoctorAgentAssignmentCheck) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	miner := doctorAssignmentRunMiner{
		ctx: ctx, repo: repo, host: host, now: time.Now().UTC(), runGH: workflow.RunGHContext,
	}
	for i := range checks {
		check := &checks[i]
		check.RecentRuns = miner.collect(check.Workflow)
		if check.Status == "configured" && doctorAssignmentRunFailure(check.RecentRuns) {
			check.Message = fmt.Sprintf("%s Recent assignment evidence needs attention; logs alone do not establish invalid credentials. %s", check.Message, doctorAssignmentRemedy(repo, check.SuggestedSecret))
		}
	}
}

func (m *doctorAssignmentRunMiner) api(endpoint string, extra ...string) ([]byte, error) {
	data, _, err := m.apiWithLimit(endpoint, 0, extra...)
	return data, err
}

func (m *doctorAssignmentRunMiner) apiWithLimit(endpoint string, maxBytes int, extra ...string) ([]byte, bool, error) {
	if m.requests >= doctorAssignmentRequestLimit {
		return nil, false, errors.New("recent-run inspection reached the 40-request limit")
	}
	if err := m.ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("recent-run inspection interrupted: %w", err)
	}
	m.requests++
	args := append([]string{"api", "--hostname", strings.TrimPrefix(m.host, "https://"), endpoint}, extra...)
	if maxBytes > 0 && m.runGHLimited != nil {
		return m.runGHLimited(m.ctx, maxBytes, args...)
	}
	data, err := m.runGH(m.ctx, "Checking recent agent assignments...", args...)
	if err != nil {
		return nil, false, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func (m *doctorAssignmentRunMiner) collect(workflowPath string) *DoctorAssignmentRunHistory {
	limit := doctorAssignmentRunLimit
	if m.limit > 0 && m.limit < limit {
		limit = m.limit
	}
	history := &DoctorAssignmentRunHistory{
		Since: m.now.Add(-doctorAssignmentRunLookback), Until: m.now,
		Limit: limit, Runs: []DoctorAssignmentRunEvidence{},
	}
	query := url.Values{
		"status": {"completed"}, "per_page": {strconv.Itoa(limit)},
		"created": {history.Since.Format(time.RFC3339) + ".." + history.Until.Format(time.RFC3339)},
	}
	filename := strings.TrimSuffix(path.Base(workflowPath), ".md") + ".lock.yml"
	endpoint := path.Join("repos", m.repo, "actions/workflows", url.PathEscape(filename), "runs")
	data, err := m.api(endpoint + "?" + query.Encode())
	if err != nil {
		history.Error = fmt.Sprintf("Cannot list recent runs; assignment outcome is unknown: %v", err)
		return history
	}
	var response struct {
		Runs []doctorAssignmentRunMetadata `json:"workflow_runs"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Runs == nil {
		history.Error = "Cannot parse recent runs; assignment outcome is unknown."
		return history
	}
	for i, run := range response.Runs {
		if i >= limit {
			break
		}
		history.Runs = append(history.Runs, m.inspect(run, history.Since))
	}
	return history
}

func (m *doctorAssignmentRunMiner) inspect(run doctorAssignmentRunMetadata, since time.Time) DoctorAssignmentRunEvidence {
	m.logBytes = 0
	evidence := DoctorAssignmentRunEvidence{
		RunID: run.ID, Attempt: run.Attempt, URL: run.URL,
		CreatedAt: run.CreatedAt, Conclusion: run.Conclusion, Outcome: "unknown",
	}
	if run.ID <= 0 || run.Attempt <= 0 || run.Status != "completed" || run.CreatedAt.Before(since) || run.CreatedAt.After(m.now) {
		evidence.Message = "Missing or out-of-window run identity, attempt, status, or timestamp."
		return evidence
	}
	endpoint := fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", m.repo, run.ID, run.Attempt)
	data, err := m.api(endpoint)
	if err != nil {
		evidence.Message = fmt.Sprintf("Cannot inspect jobs for this attempt: %v", err)
		return evidence
	}
	var response struct {
		TotalCount *int      `json:"total_count"`
		Jobs       []JobInfo `json:"jobs"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Jobs == nil || response.TotalCount == nil || *response.TotalCount != len(response.Jobs) {
		evidence.Message = "Incomplete jobs metadata; assignment outcome is unknown."
		return evidence
	}
	if m.allJobs {
		slices.SortStableFunc(response.Jobs, func(a, b JobInfo) int {
			return doctorTokenJobPriority(b) - doctorTokenJobPriority(a)
		})
	}
	coverageMessage := ""
	for _, job := range response.Jobs {
		if !m.allJobs && job.Name != "safe_outputs" && job.Name != "assign_to_agent" {
			continue
		}
		jobEvidence := evidence
		jobEvidence.Outcome, jobEvidence.Message = "unknown", ""
		jobEvidence.credentialDiagnostic = false
		current := m.inspectJob(job, jobEvidence)
		if !m.allJobs || current.Outcome == "credential_error" {
			return current
		}
		if current.Outcome == "unknown" {
			if current.credentialDiagnostic {
				evidence = current
			} else {
				if coverageMessage == "" {
					coverageMessage = current.Message
				}
				if !evidence.credentialDiagnostic {
					evidence = current
				}
			}
		}
	}
	return m.finishInspection(evidence, coverageMessage)
}

func (m *doctorAssignmentRunMiner) finishInspection(evidence DoctorAssignmentRunEvidence, coverageMessage string) DoctorAssignmentRunEvidence {
	if m.allJobs {
		if evidence.credentialDiagnostic && coverageMessage != "" {
			evidence.Message += " Inspection coverage is incomplete: " + coverageMessage
		}
		if evidence.Message == "" {
			evidence.Outcome, evidence.Message = "no_evidence", "No recognized credential failure found; token validity remains unverified."
		}
		return evidence
	}
	evidence.Outcome, evidence.Message = "no_evidence", "No assignment handler job found in this run attempt."
	return evidence
}

func doctorTokenJobPriority(job JobInfo) int {
	priority := 0
	if job.Conclusion == "failure" {
		priority += 2
	}
	if job.Name == "safe_outputs" || job.Name == "assign_to_agent" {
		priority++
	}
	return priority
}

func (m *doctorAssignmentRunMiner) inspectJob(job JobInfo, evidence DoctorAssignmentRunEvidence) DoctorAssignmentRunEvidence {
	if job.ID <= 0 || job.Status != "completed" || job.RunAttempt != evidence.Attempt || job.RunID != evidence.RunID {
		evidence.Message = "Missing or mismatched assignment job identity, status, or attempt."
		return evidence
	}
	if job.Conclusion == "skipped" {
		evidence.Outcome, evidence.Message = "not_attempted", "The assignment handler job was skipped."
		return evidence
	}
	endpoint := fmt.Sprintf("repos/%s/actions/jobs/%d/logs", m.repo, job.ID)
	var data []byte
	var truncated bool
	var err error
	if m.maxLogBytes > 0 {
		remaining := m.maxLogBytes - m.logBytes
		if remaining <= 0 {
			evidence.Outcome, evidence.Message = "unknown", "The per-run log-inspection byte limit was reached; remaining job logs are unknown."
			return evidence
		}
		data, truncated, err = m.apiWithLimit(endpoint, remaining, "--allow-escape-sequences")
		m.logBytes += len(data)
	} else {
		data, err = m.api(endpoint, "--allow-escape-sequences")
	}
	if err != nil {
		evidence.Message = fmt.Sprintf("Assignment logs are unavailable; outcome is unknown: %v", err)
		return evidence
	}
	if truncated {
		evidence.Outcome, evidence.Message = "unknown", "Job log inspection was truncated at the per-run byte limit; credential validity is unknown."
		return evidence
	}
	// Logs can contain ANSI sequences, source code, and masked credentials. Never
	// render or retain raw lines; export only recognized handler outcomes.
	if m.classify != nil {
		return m.classify(string(data), evidence)
	}
	return classifyDoctorAssignmentLog(string(data), evidence)
}

func runDoctorGHContextLimited(ctx context.Context, maxBytes int, args ...string) ([]byte, bool, error) {
	cmd := workflow.ExecGHContext(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(io.LimitReader(stdout, int64(maxBytes)+1))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, false, err
	}
	if len(data) > maxBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return data[:maxBytes], true, nil
	}
	if err := cmd.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, false, fmt.Errorf("%w: %s", err, message)
		}
		return nil, false, err
	}
	return data, false, nil
}

var doctorAssignmentSuccessPattern = regexp.MustCompile(`^Successfully assigned [A-Za-z0-9_-]+ coding agent to (?:issue|pull request) #[1-9][0-9]*$`)
var doctorAssignmentExistingPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+ is already assigned to (?:issue|pull request) #[1-9][0-9]*$`)
var doctorAssignmentFailurePattern = regexp.MustCompile(`^(?:Failed to assign agent "[A-Za-z0-9_-]+" to (?:issue|pull request) #[1-9][0-9]*: |Agent assignment failed for [A-Za-z0-9_-]+ on (?:issue|pull request) #[1-9][0-9]* due to |Failed to assign [A-Za-z0-9_-]+: Copilot assignment permission requirements not met)`)

func classifyDoctorAssignmentLog(content string, evidence DoctorAssignmentRunEvidence) DoctorAssignmentRunEvidence {
	credentialError, assignmentFailure := false, false
	seen := map[string]struct{}{}
	for line := range strings.SplitSeq(stripGHALogTimestamps(stringutil.StripANSI(content)), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(strings.TrimPrefix(line, "##[warning]"), "##[error]")
		if _, exists := seen[line]; exists {
			continue
		}
		switch {
		case doctorAssignmentSuccessPattern.MatchString(line):
			seen[line] = struct{}{}
			evidence.Assigned++
		case doctorAssignmentExistingPattern.MatchString(line):
			seen[line] = struct{}{}
			evidence.AlreadyAssigned++
		case strings.HasPrefix(line, "Agent assignment credential validation failed:"):
			credentialError = true
		case doctorAssignmentFailurePattern.MatchString(line):
			if doctorAssignmentCredentialFailure(line) {
				credentialError = true
			} else {
				assignmentFailure = true
			}
		}
	}
	switch {
	case credentialError:
		evidence.credentialDiagnostic = true
		evidence.Outcome, evidence.Message = "unknown", "Assignment logs contain a credential-like diagnostic, but workflow code can emit log text; token validity is unknown even if the run failed."
	case assignmentFailure:
		evidence.Outcome, evidence.Message = "assignment_failed", "Assignment handler reported a failure without conclusive credential evidence."
	case evidence.Assigned > 0:
		evidence.Outcome, evidence.Message = "assigned", "Assignment handler confirmed successful agent assignment."
	case evidence.AlreadyAssigned > 0:
		evidence.Outcome, evidence.Message = "already_assigned", "Agent was already assigned; this does not demonstrate a new assignment."
	default:
		evidence.Outcome, evidence.Message = "no_evidence", "No confirmed assignment or credential failure found. No-op, staged, filtered, and older handler formats are inconclusive."
	}
	return evidence
}

func doctorAssignmentCredentialFailure(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"bad credentials", "http 401", "not authenticated", "requires authentication",
		"resource not accessible", "insufficient permissions", "authentication/permission error",
		"copilot assignment permission requirements not met",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func doctorAssignmentRunFailure(history *DoctorAssignmentRunHistory) bool {
	if history != nil {
		for _, run := range history.Runs {
			if run.Outcome == "credential_error" || run.Outcome == "assignment_failed" || run.Outcome == "unknown" {
				return true
			}
		}
	}
	return false
}

func printDoctorAssignmentRunHistory(history *DoctorAssignmentRunHistory) {
	printDoctorRunHistory(history, "assignment")
}

func printDoctorRunHistory(history *DoctorAssignmentRunHistory, kind string) {
	if history == nil {
		return
	}
	if history.Error != "" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(history.Error))
		if len(history.Runs) == 0 {
			return
		}
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Recent %s evidence: %d completed run(s) sampled since %s (limit %d).", kind, len(history.Runs), history.Since.Format(time.RFC3339), history.Limit)))
	for _, run := range history.Runs {
		message := fmt.Sprintf("Run %d attempt %d: %s. %s %s", run.RunID, run.Attempt, run.Outcome, run.Message, run.URL)
		if kind == "assignment" {
			message = fmt.Sprintf("Run %d attempt %d: %s; %d assigned, %d already assigned. %s %s", run.RunID, run.Attempt, run.Outcome, run.Assigned, run.AlreadyAssigned, run.Message, run.URL)
		}
		if run.Outcome == "credential_error" || run.Outcome == "assignment_failed" || run.Outcome == "unknown" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(message))
		}
	}
}
