//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorAssignmentLogEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, log, outcome string
		assigned, existing int
	}{
		{name: "actual assignment", log: "Successfully assigned copilot coding agent to issue #42", outcome: "assigned", assigned: 1},
		{name: "PR assignment", log: "Successfully assigned copilot coding agent to pull request #42", outcome: "assigned", assigned: 1},
		{name: "helper and handler duplicates", log: "Successfully assigned copilot coding agent to issue #42\nSuccessfully assigned copilot coding agent to issue #42", outcome: "assigned", assigned: 1},
		{name: "already assigned", log: "copilot is already assigned to issue #42", outcome: "already_assigned", existing: 1},
		{name: "successful run with suppressed preflight failure", log: "2026-10-29T10:43:49.7032089Z ##[warning]Agent assignment credential validation failed: Bad credentials (HTTP 401). token-not-for-output\n2026-10-29T10:43:49.8Z Exported assign_to_agent outputs (0 error(s))", outcome: "credential_error"},
		{name: "ignored permission error", log: "##[warning]Agent assignment failed for copilot on issue #42 due to authentication/permission error. Skipping due to ignore-if-error=true.", outcome: "credential_error"},
		{name: "regular permission error", log: `##[error]Failed to assign agent "copilot" to issue #42: Resource not accessible by integration`, outcome: "credential_error"},
		{name: "HTTP unauthorized", log: `##[error]Failed to assign agent "copilot" to issue #42: Bad credentials (HTTP 401)`, outcome: "credential_error"},
		{name: "legacy permission template", log: "Failed to assign copilot: Copilot assignment permission requirements not met", outcome: "credential_error"},
		{name: "availability failure", log: "##[warning]Agent assignment failed for copilot on issue #42 due to agent availability error. Skipping due to ignore-if-error=true.", outcome: "assignment_failed"},
		{name: "other failure", log: `##[error]Failed to assign agent "copilot" to issue #42: target not found`, outcome: "assignment_failed"},
		{name: "mixed failures and assignments", log: "Successfully assigned copilot coding agent to issue #42\ncopilot is already assigned to issue #43\n##[warning]Agent assignment credential validation failed: Bad credentials", outcome: "credential_error", assigned: 1, existing: 1},
		{name: "ANSI and timestamps", log: "2026-10-29T10:43:49.7032089Z \x1b[32mSuccessfully assigned copilot coding agent to issue #42\x1b[0m\r\n", outcome: "assigned", assigned: 1},
		{name: "start only", log: "Assigning copilot coding agent to issue #42", outcome: "no_evidence"},
		{name: "staged", log: "Staged assign_to_agent: success: true, skipped: true\nExported assign_to_agent outputs (0 error(s))", outcome: "no_evidence"},
		{name: "skipped", log: "Skipping assign_to_agent for #42: target is a pull request, not an issue.", outcome: "no_evidence"},
		{name: "unrelated credentials", log: "Bad credentials\n##[error]Failed to assign users: Bad credentials\n##[error]Failed to assign milestone #1 to issue #42: Resource not accessible", outcome: "no_evidence"},
		{name: "echoed source and config", log: `core.warning("Agent assignment credential validation failed: Bad credentials");
core.info("Successfully assigned copilot coding agent to issue #42");
{"message":"Successfully assigned copilot coding agent to issue #42"}
echo "Successfully assigned copilot coding agent to issue #42"`, outcome: "no_evidence"},
		{name: "empty logs", outcome: "no_evidence"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			evidence := classifyDoctorAssignmentLog(tc.log, DoctorAssignmentRunEvidence{Conclusion: "success"})
			assert.Equal(t, tc.outcome, evidence.Outcome)
			assert.Equal(t, tc.assigned, evidence.Assigned)
			assert.Equal(t, tc.existing, evidence.AlreadyAssigned)
			data, err := json.Marshal(evidence)
			require.NoError(t, err)
			assert.NotContains(t, string(data), "token-not-for-output")
		})
	}
}

func TestDoctorAssignmentRecentRunRequests(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	miner := doctorAssignmentRunMiner{ctx: context.Background(), repo: "octo/repo", host: "https://ghe.example.com", now: now}
	miner.runGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.GreaterOrEqual(t, len(args), 4)
		assert.Equal(t, []string{"api", "--hostname", "ghe.example.com"}, args[:3])
		endpoint := args[3]
		switch {
		case strings.Contains(endpoint, "workflows/assign.lock.yml/runs?"):
			u, err := url.Parse(endpoint)
			require.NoError(t, err)
			assert.Equal(t, "completed", u.Query().Get("status"))
			assert.Equal(t, "5", u.Query().Get("per_page"))
			assert.Equal(t, "2026-10-22T12:00:00Z..2026-10-29T12:00:00Z", u.Query().Get("created"))
			return []byte(`{"workflow_runs":[{"id":42,"run_attempt":2,"html_url":"https://ghe.example.com/octo/repo/actions/runs/42","created_at":"2026-10-29T10:00:00Z","status":"completed","conclusion":"success"}]}`), nil
		case endpoint == "repos/octo/repo/actions/runs/42/attempts/2/jobs?per_page=100":
			return []byte(`{"total_count":2,"jobs":[{"id":11,"name":"agent","status":"completed"},{"id":12,"run_id":42,"run_attempt":2,"name":"safe_outputs","status":"completed","conclusion":"success"}]}`), nil
		case endpoint == "repos/octo/repo/actions/jobs/12/logs":
			assert.Contains(t, args, "--allow-escape-sequences")
			return []byte("##[warning]Agent assignment credential validation failed: Bad credentials (HTTP 401)\nExported assign_to_agent outputs (0 error(s))"), nil
		default:
			t.Fatalf("unexpected request: %v", args)
			return nil, errors.New("unexpected request")
		}
	}
	history := miner.collect(".github/workflows/assign.md")
	require.Empty(t, history.Error)
	require.Len(t, history.Runs, 1)
	assert.Equal(t, "success", history.Runs[0].Conclusion)
	assert.Equal(t, "credential_error", history.Runs[0].Outcome)
	assert.Equal(t, 3, miner.requests)
	assert.Equal(t, now.Add(-7*24*time.Hour), history.Since)
	assert.True(t, doctorAssignmentRunFailure(history))
}

func TestDoctorAssignmentRunCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, jobs, outcome string
		jobsErr, logErr     error
		requests            int
	}{
		{name: "skipped handler", jobs: `{"total_count":1,"jobs":[{"id":12,"run_id":42,"run_attempt":2,"name":"safe_outputs","status":"completed","conclusion":"skipped"}]}`, outcome: "not_attempted", requests: 1},
		{name: "unrelated job logs not read", jobs: `{"total_count":1,"jobs":[{"id":11,"name":"agent","status":"completed"}]}`, outcome: "no_evidence", requests: 1},
		{name: "legacy handler", jobs: `{"total_count":1,"jobs":[{"id":12,"run_id":42,"run_attempt":2,"name":"assign_to_agent","status":"completed"}]}`, outcome: "assigned", requests: 2},
		{name: "wrong attempt", jobs: `{"total_count":1,"jobs":[{"id":12,"run_id":42,"run_attempt":1,"name":"safe_outputs","status":"completed"}]}`, outcome: "unknown", requests: 1},
		{name: "wrong run", jobs: `{"total_count":1,"jobs":[{"id":12,"run_id":99,"run_attempt":2,"name":"safe_outputs","status":"completed"}]}`, outcome: "unknown", requests: 1},
		{name: "incomplete pagination", jobs: `{"total_count":101,"jobs":[{"id":12,"run_id":42,"run_attempt":2,"name":"safe_outputs","status":"completed"}]}`, outcome: "unknown", requests: 1},
		{name: "missing jobs", jobs: `{}`, outcome: "unknown", requests: 1},
		{name: "malformed jobs", jobs: `not JSON`, outcome: "unknown", requests: 1},
		{name: "inaccessible jobs", jobsErr: errors.New("HTTP 403"), outcome: "unknown", requests: 1},
		{name: "expired logs", jobs: `{"total_count":1,"jobs":[{"id":12,"run_id":42,"run_attempt":2,"name":"safe_outputs","status":"completed"}]}`, logErr: errors.New("HTTP 410"), outcome: "unknown", requests: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Now().UTC()
			miner := doctorAssignmentRunMiner{ctx: context.Background(), repo: "octo/repo", host: "https://github.com", now: now}
			miner.runGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if strings.HasSuffix(strings.Join(args, " "), "jobs?per_page=100") {
					return []byte(tc.jobs), tc.jobsErr
				}
				return []byte("Successfully assigned copilot coding agent to issue #42"), tc.logErr
			}
			run := doctorAssignmentRunMetadata{ID: 42, Attempt: 2, CreatedAt: now, Status: "completed"}
			evidence := miner.inspect(run, now.Add(-doctorAssignmentRunLookback))
			assert.Equal(t, tc.outcome, evidence.Outcome)
			assert.Equal(t, tc.requests, miner.requests)
		})
	}
}

func TestDoctorAssignmentRunBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, response string
		apiErr         error
		wantError      bool
	}{
		{name: "no runs", response: `{"workflow_runs":[]}`},
		{name: "missing runs", response: `{}`, wantError: true},
		{name: "malformed runs", response: `not JSON`, wantError: true},
		{name: "denied", apiErr: errors.New("HTTP 403"), wantError: true},
		{name: "maximum five runs", response: `{"workflow_runs":[{},{},{},{},{},{}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner := doctorAssignmentRunMiner{ctx: context.Background(), now: time.Now(), host: "https://github.com"}
			miner.runGH = func(context.Context, string, ...string) ([]byte, error) { return []byte(tc.response), tc.apiErr }
			history := miner.collect("assign.md")
			assert.Equal(t, tc.wantError, history.Error != "")
			assert.LessOrEqual(t, len(history.Runs), 5)
			assert.Equal(t, 1, miner.requests)
		})
	}
}

func TestDoctorAssignmentRequestBudgetAndCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, miner := range []doctorAssignmentRunMiner{
		{ctx: context.Background(), requests: doctorAssignmentRequestLimit},
		{ctx: ctx},
	} {
		miner.runGH = func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("must not request after the budget or deadline")
			return nil, nil
		}
		history := miner.collect("assign.md")
		assert.NotEmpty(t, history.Error)
		assert.Empty(t, history.Runs)
	}
}

func TestDoctorAssignmentRejectsInvalidRunMetadata(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, run := range []doctorAssignmentRunMetadata{
		{ID: 42, Attempt: 0, CreatedAt: now, Status: "completed"},
		{ID: 42, Attempt: 2, CreatedAt: now.Add(-8 * 24 * time.Hour), Status: "completed"},
		{ID: 42, Attempt: 2, CreatedAt: now.Add(time.Hour), Status: "completed"},
		{ID: 42, Attempt: 2, CreatedAt: now, Status: "in_progress"},
	} {
		miner := doctorAssignmentRunMiner{ctx: context.Background(), now: now}
		miner.runGH = func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("must not inspect invalid run metadata")
			return nil, nil
		}
		evidence := miner.inspect(run, now.Add(-doctorAssignmentRunLookback))
		assert.Equal(t, "unknown", evidence.Outcome)
		assert.Zero(t, miner.requests)
	}
}

func TestDoctorAssignmentHistoryReporting(t *testing.T) {
	for _, outcome := range []string{"assigned", "already_assigned", "credential_error", "assignment_failed", "unknown", "no_evidence"} {
		t.Run(outcome, func(t *testing.T) {
			history := &DoctorAssignmentRunHistory{Limit: 5, Runs: []DoctorAssignmentRunEvidence{{
				RunID: 42, Attempt: 2, URL: "https://github.com/octo/repo/actions/runs/42", Outcome: outcome,
			}}}
			check := DoctorAgentAssignmentCheck{Workflow: "assign.md", Status: "configured", RecentRuns: history, PATURL: "pat-setup", SecretsURL: "secret-setup"}
			_, stderr := captureOutput(t, func() error {
				err := printDoctorAgentAssignments([]DoctorAgentAssignmentCheck{check})
				assert.Equal(t, outcome == "credential_error" || outcome == "assignment_failed", err != nil)
				return nil
			})
			assert.Contains(t, stderr, "Run 42 attempt 2: "+outcome)
			assert.Contains(t, stderr, history.Runs[0].URL)
			if outcome == "credential_error" {
				assert.Contains(t, stderr, "Create PAT: pat-setup")
				assert.Contains(t, stderr, "Add repository secret: secret-setup")
			}
			data, err := json.Marshal(check)
			require.NoError(t, err)
			assert.Contains(t, string(data), `"recent_runs":`)
			assert.Contains(t, string(data), `"outcome":"`+outcome+`"`)
		})
	}
	_, stderr := captureOutput(t, func() error {
		printDoctorAssignmentRunHistory(&DoctorAssignmentRunHistory{Error: "HTTP 403: outcome unknown"})
		return nil
	})
	assert.Contains(t, stderr, "HTTP 403: outcome unknown")
}
