//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorTokenFailureFirstBudget(t *testing.T) {
	t.Parallel()
	checks := []*DoctorTokenValidation{
		{Workflow: ".github/workflows/a.md", Available: []string{"CUSTOM_TOKEN"}},
		{Workflow: ".github/workflows/z.md", Available: []string{"CUSTOM_TOKEN"}},
	}
	now := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)
	miner := doctorAssignmentRunMiner{ctx: t.Context(), repo: "octo/repo", host: "https://github.com", now: now,
		requests: 37, allJobs: true, limit: 1, classify: classifyDoctorTokenLog}
	miner.runGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch endpoint := args[3]; {
		case strings.Contains(endpoint, "actions/runs?status=failure"):
			return []byte(`{"workflow_runs":[{"id":42,"run_attempt":1,"path":".github/workflows/z.lock.yml","created_at":"2026-10-31T16:00:00Z","status":"completed","conclusion":"failure"}]}`), nil
		case strings.Contains(endpoint, "runs/42/attempts/1/jobs?"):
			return []byte(`{"total_count":1,"jobs":[{"id":12,"run_id":42,"run_attempt":1,"name":"safe_outputs","status":"completed","conclusion":"failure"}]}`), nil
		case strings.HasSuffix(endpoint, "jobs/12/logs"):
			return []byte("##[error]Failed to add comment: Bad credentials"), nil
		default:
			t.Fatalf("Unexpected request: %s", endpoint)
			return nil, errors.New("unexpected request")
		}
	}
	collectDoctorTokenRunEvidence(checks, &miner)
	assert.Equal(t, "unknown", checks[0].Status)
	assert.Equal(t, "unknown", checks[1].Status)
	assert.Equal(t, int64(42), checks[1].RecentRuns.Runs[0].RunID)
	assert.Equal(t, 40, miner.requests)
}

func TestDoctorTokenLogInspectionIsBounded(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	miner := doctorAssignmentRunMiner{
		ctx: t.Context(), repo: "octo/repo", host: "https://github.com", now: now,
		allJobs: true, maxLogBytes: 32,
		runGH: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch {
			case strings.Contains(args[3], "/jobs?"):
				return []byte(`{"total_count":1,"jobs":[{"id":11,"run_id":42,"run_attempt":1,"name":"agent","status":"completed","conclusion":"success"}]}`), nil
			case strings.HasSuffix(args[3], "/jobs/11/logs"):
				return []byte(strings.Repeat("x", 33)), nil
			default:
				t.Fatalf("Unexpected request: %s", args[3])
				return nil, errors.New("unexpected request")
			}
		},
	}

	evidence := miner.inspect(doctorAssignmentRunMetadata{ID: 42, Attempt: 1, CreatedAt: now, Status: "completed"}, now.Add(-time.Hour))
	assert.Equal(t, "unknown", evidence.Outcome)
	assert.Contains(t, evidence.Message, "truncated")
	assert.Equal(t, 2, miner.requests)
}

func TestDoctorTokenPriorityUsesDiscoveredAvailability(t *testing.T) {
	t.Parallel()
	missing := &DoctorTokenValidation{Workflow: "a.md", Missing: []string{"OTHER_TOKEN"}}
	builtin := &DoctorTokenValidation{Workflow: "b.md", Available: []string{"GH_AW_AGENT_TOKEN"}}
	custom := &DoctorTokenValidation{Workflow: "z.md", Available: []string{"REPOSITORY_SPECIFIC_CREDENTIAL"}}
	checks := []*DoctorTokenValidation{missing, builtin, custom}
	prioritizeDoctorTokenChecks(checks)
	assert.Equal(t, []*DoctorTokenValidation{custom, builtin, missing}, checks)
}

func TestDoctorTokenAssignmentEvidenceReuse(t *testing.T) {
	t.Parallel()
	history := &DoctorAssignmentRunHistory{Runs: []DoctorAssignmentRunEvidence{{RunID: 42, Outcome: "credential_error"}}}
	checks := []DoctorTokenValidation{
		{Workflow: "issue.md", Secrets: []string{"GH_AW_AGENT_TOKEN"}, Status: "unknown"},
		{Workflow: "other.md", Secrets: []string{"OTHER_TOKEN"}, Status: "unknown"},
	}
	assignments := []DoctorAgentAssignmentCheck{
		{Workflow: "issue.md", Secret: "GH_AW_AGENT_TOKEN", RecentRuns: history},
		{Workflow: "other.md", Secret: "GH_AW_AGENT_TOKEN", RecentRuns: history},
	}
	applyDoctorAssignmentTokenEvidence(checks, assignments)
	assert.Equal(t, "unknown", checks[0].Status)
	assert.Same(t, history, checks[0].RecentRuns)
	assert.Contains(t, checks[0].Message, "not every operation")
	assert.Equal(t, "unknown", checks[1].Status)
}

func TestDoctorTokenFailureInventoryBound(t *testing.T) {
	t.Parallel()
	batch := make([]map[string]any, 100)
	for i := range batch {
		batch[i] = map[string]any{"id": i + 1, "conclusion": "failure", "path": "unrelated.yml"}
	}
	response, err := json.Marshal(map[string]any{"workflow_runs": batch})
	require.NoError(t, err)
	miner := doctorAssignmentRunMiner{ctx: t.Context(), repo: "octo/repo", host: "https://github.com", now: time.Now(),
		runGH: func(context.Context, string, ...string) ([]byte, error) { return response, nil }}
	runs, err := listDoctorTokenFailureCandidates(&miner, []GitHubWorkflow{{Path: ".github/workflows/test.lock.yml"}})
	assert.Empty(t, runs)
	require.ErrorContains(t, err, "two-page limit")
	assert.Equal(t, 2, miner.requests)
}

func TestDoctorTokenPartialHistoryRetainsEvidence(t *testing.T) {
	history := &DoctorAssignmentRunHistory{
		Error: "Failure-first inventory is incomplete",
		Limit: 1,
		Runs: []DoctorAssignmentRunEvidence{{
			RunID: 42, Attempt: 1, Outcome: "unknown",
			Message: "Workflow logs contain a credential-like diagnostic",
			URL:     "https://github.com/octo/repo/actions/runs/42",
		}},
	}
	_, output := captureOutput(t, func() error {
		printDoctorRunHistory(history, "token")
		return nil
	})
	assert.Contains(t, output, history.Error)
	assert.Contains(t, output, "Run 42 attempt 1: unknown")
	assert.Contains(t, output, history.Runs[0].Message)
	assert.Contains(t, output, history.Runs[0].URL)
}

func TestDoctorTokenRunLogsDoNotValidateCredentials(t *testing.T) {
	t.Parallel()
	for _, conclusion := range []string{"success", "failure"} {
		for _, log := range []string{
			"Agent assignment credential validation failed: Bad credentials",
			`##[error]Failed to assign agent "copilot" to issue #42: Bad credentials (HTTP 401)`,
			"Agent assignment failed for copilot on issue #42 due to authentication/permission error",
		} {
			t.Run(conclusion+"/"+log, func(t *testing.T) {
				evidence := classifyDoctorAssignmentLog(log, DoctorAssignmentRunEvidence{Conclusion: conclusion})
				require.Equal(t, "unknown", evidence.Outcome)
				assert.Contains(t, evidence.Message, "workflow code can emit log text")
				history := &DoctorAssignmentRunHistory{Runs: []DoctorAssignmentRunEvidence{evidence}}
				check := DoctorTokenValidation{Workflow: "issue.md", Secrets: []string{"CUSTOM_TOKEN"}, Status: "unverified", RecentRuns: history}
				applyDoctorTokenRunEvidence(&check)
				assert.Equal(t, "unknown", check.Status)
				require.Error(t, doctorTokenValidationError([]DoctorTokenValidation{check}))
				require.Error(t, doctorAgentAssignmentError([]DoctorAgentAssignmentCheck{{Status: "configured", RecentRuns: history}}))
				check.Status, check.RecentRuns = "unverified", nil
				checks := []DoctorTokenValidation{check}
				applyDoctorAssignmentTokenEvidence(checks, []DoctorAgentAssignmentCheck{{Workflow: "issue.md", Secret: "CUSTOM_TOKEN", RecentRuns: history}})
				assert.Equal(t, "unknown", checks[0].Status)
				assert.Same(t, history, checks[0].RecentRuns)
			})
		}
	}
	for _, outcome := range []string{"credential_error", "assignment_failed"} {
		check := DoctorTokenValidation{Status: "unverified", RecentRuns: &DoctorAssignmentRunHistory{
			Runs: []DoctorAssignmentRunEvidence{{Conclusion: "failure", Outcome: outcome}},
		}}
		applyDoctorTokenRunEvidence(&check)
		assert.Equal(t, "unknown", check.Status, "legacy log evidence must not promote token status")
	}
}

func TestDoctorTokenStreamedLogBudgetAcrossJobs(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	var limits []int
	miner := doctorAssignmentRunMiner{
		ctx: t.Context(), repo: "octo/repo", host: "https://github.com", now: now,
		allJobs: true, maxLogBytes: 32, classify: classifyDoctorTokenLog,
		runGH: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			require.Contains(t, args[3], "/jobs?")
			return []byte(`{"total_count":3,"jobs":[{"id":11,"run_id":42,"run_attempt":1,"name":"build","status":"completed"},{"id":12,"run_id":42,"run_attempt":1,"name":"test","status":"completed"},{"id":13,"run_id":42,"run_attempt":1,"name":"deploy","status":"completed"}]}`), nil
		},
		runGHLimited: func(_ context.Context, maxBytes int, args ...string) ([]byte, bool, error) {
			limits = append(limits, maxBytes)
			if strings.HasSuffix(args[3], "/11/logs") {
				return []byte(strings.Repeat("x", 12)), false, nil
			}
			require.Contains(t, args[3], "/12/logs")
			return []byte(strings.Repeat("x", maxBytes)), true, nil
		},
	}
	run := doctorAssignmentRunMetadata{ID: 42, Attempt: 1, CreatedAt: now, Status: "completed"}
	evidence := miner.inspect(run, now.Add(-time.Hour))
	assert.Equal(t, "unknown", evidence.Outcome)
	assert.Equal(t, []int{32, 20}, limits)
	assert.Equal(t, 32, miner.logBytes)
	assert.Equal(t, 3, miner.requests, "no log request after the byte budget is exhausted")
	limits = nil
	_ = miner.inspect(run, now.Add(-time.Hour))
	assert.Equal(t, []int{32, 20}, limits, "the byte budget resets for each run")
}

func TestDoctorTokenJobPriorityRetainsWeakEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		conclusion string
		firstJob   string
		secondJob  string
	}{
		{name: "failed job before successful handler and noisy agent", conclusion: "failure", firstJob: "/13/logs", secondJob: "/12/logs"},
		{name: "handler before noisy successful jobs", conclusion: "success", firstJob: "/12/logs", secondJob: "/11/logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			jobs := []JobInfo{
				{ID: 11, RunID: 42, RunAttempt: 1, Name: "agent", Status: "completed", Conclusion: "success"},
				{ID: 12, RunID: 42, RunAttempt: 1, Name: "safe_outputs", Status: "completed", Conclusion: "success"},
				{ID: 13, RunID: 42, RunAttempt: 1, Name: "custom-job", Status: "completed", Conclusion: tc.conclusion},
			}
			data, err := json.Marshal(map[string]any{"total_count": len(jobs), "jobs": jobs})
			require.NoError(t, err)
			logRequests := 0
			miner := doctorAssignmentRunMiner{
				ctx: t.Context(), repo: "octo/repo", host: "https://github.com", now: now,
				allJobs: true, maxLogBytes: 256, classify: classifyDoctorTokenLog,
				runGH: func(_ context.Context, _ string, args ...string) ([]byte, error) {
					require.Contains(t, args[3], "/jobs?")
					return data, nil
				},
				runGHLimited: func(_ context.Context, maxBytes int, args ...string) ([]byte, bool, error) {
					logRequests++
					if logRequests == 1 {
						require.True(t, strings.HasSuffix(args[3], tc.firstJob))
						return []byte("##[error]Failed to add comment: Bad credentials"), false, nil
					}
					require.Equal(t, 2, logRequests, "exhausted logs must not be fetched")
					require.True(t, strings.HasSuffix(args[3], tc.secondJob))
					return []byte(strings.Repeat("x", maxBytes)), true, nil
				},
			}
			evidence := miner.inspect(doctorAssignmentRunMetadata{ID: 42, Attempt: 1, CreatedAt: now, Status: "completed", Conclusion: "failure"}, now.Add(-time.Hour))
			assert.Equal(t, "unknown", evidence.Outcome)
			assert.Contains(t, evidence.Message, "credential-like diagnostic")
			assert.Contains(t, evidence.Message, "Inspection coverage is incomplete")
			assert.Contains(t, evidence.Message, "truncated")
			assert.Contains(t, evidence.Message, "byte limit")
			assert.Equal(t, 256, miner.logBytes)
			assert.Equal(t, 2, logRequests)
			check := DoctorTokenValidation{Status: "unverified", RecentRuns: &DoctorAssignmentRunHistory{Runs: []DoctorAssignmentRunEvidence{evidence}}}
			applyDoctorTokenRunEvidence(&check)
			assert.Equal(t, "unknown", check.Status)
		})
	}
}
