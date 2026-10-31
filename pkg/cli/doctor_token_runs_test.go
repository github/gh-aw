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
	assert.Equal(t, "credential_error", checks[1].Status)
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
	assert.Equal(t, "credential_error", checks[0].Status)
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
