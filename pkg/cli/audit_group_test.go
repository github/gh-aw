//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupAuditFindingsForRun(t *testing.T) {
	t.Parallel()
	findings := []AuditFinding{
		{Code: AuditFindingWorkflowFailed, Title: "first failure"},
		{Code: AuditFindingWorkflowFailed, Title: "second failure"},
		{Code: AuditFindingHighTokenUsage, Title: "high tokens"},
	}

	entries := groupAuditFindingsForRun(123, findings)
	sortGroupedAuditEntries(entries)

	require.Len(t, entries, 2)
	assert.Equal(t, int64(123), entries[0].RunID)
	assert.Equal(t, AuditFindingHighTokenUsage, entries[0].Code)
	assert.Equal(t, 1, entries[0].Occurrences)
	assert.Equal(t, "high tokens", entries[0].RepresentativeEntry.Title)
	assert.Equal(t, AuditFindingWorkflowFailed, entries[1].Code)
	assert.Equal(t, 2, entries[1].Occurrences)
	assert.Equal(t, "first failure", entries[1].RepresentativeEntry.Title)
}

func TestSortGroupedAuditEntries(t *testing.T) {
	t.Parallel()
	entries := []GroupedAuditEntry{
		{RunID: 2, Code: AuditFindingWorkflowFailed},
		{RunID: 1, Code: AuditFindingWorkflowFailed},
		{RunID: 1, Code: AuditFindingHighTokenUsage},
	}

	sortGroupedAuditEntries(entries)

	assert.Equal(t, []GroupedAuditEntry{
		{RunID: 1, Code: AuditFindingHighTokenUsage},
		{RunID: 1, Code: AuditFindingWorkflowFailed},
		{RunID: 2, Code: AuditFindingWorkflowFailed},
	}, entries)
}

func TestResolveGroupedAuditRunRequests(t *testing.T) {
	t.Parallel()
	requests, err := resolveGroupedAuditRunRequests([]string{
		"https://github.com/owner/repo/actions/runs/100",
		"101",
	}, "")
	require.NoError(t, err)
	require.Len(t, requests, 2)
	assert.Equal(t, int64(100), requests[0].runID)
	assert.Equal(t, int64(101), requests[1].runID)
	assert.Equal(t, "owner", requests[1].owner)
	assert.Equal(t, "repo", requests[1].repo)
	assert.Equal(t, "github.com", requests[1].hostname)
}

func TestResolveGroupedAuditRunRequestsRejectsDuplicateRuns(t *testing.T) {
	t.Parallel()
	_, err := resolveGroupedAuditRunRequests([]string{"100", "100"}, "owner/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate run ID 100")
}
