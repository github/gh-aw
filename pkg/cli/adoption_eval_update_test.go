//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvalUpdateIssueRetained(t *testing.T) {
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"title": "New title",
			"body":  "New body",
			"state": "open",
			"labels": []any{
				map[string]any{"name": "bug"},
				map[string]any{"name": "triage"},
			},
			"assignees": []any{
				map[string]any{"login": "octo"},
			},
		}, nil
	}

	report := evalUpdateIssue(context.Background(), CreatedItemReport{
		Type:   "update_issue",
		Number: 12,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"title":     "Old title",
			"body_hash": mutableBodyHash("Old body"),
			"state":     "open",
			"labels":    []any{"triage"},
			"assignees": []any{},
		},
		AfterState: map[string]any{
			"title":     "New title",
			"body_hash": mutableBodyHash("New body"),
			"state":     "open",
			"labels":    []any{"triage", "bug"},
			"assignees": []any{"octo"},
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusAccepted, report.AdoptionStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "state_retained", report.Signal)
}

func TestEvalUpdateIssueReverted(t *testing.T) {
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"title": "Old title",
			"body":  "Old body",
			"state": "open",
		}, nil
	}

	report := evalUpdateIssue(context.Background(), CreatedItemReport{
		Type:   "update_issue",
		Number: 12,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"title":     "Old title",
			"body_hash": mutableBodyHash("Old body"),
			"state":     "open",
		},
		AfterState: map[string]any{
			"title":     "New title",
			"body_hash": mutableBodyHash("New body"),
			"state":     "closed",
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusRejected, report.AdoptionStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "state_reverted", report.Signal)
}

func TestEvalUpdatePullRequestRetainedAndMerged(t *testing.T) {
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"title":  "New title",
			"body":   "New body",
			"state":  "closed",
			"merged": true,
			"base":   map[string]any{"ref": "release"},
			"draft":  false,
			"head":   map[string]any{"sha": "def456"},
		}, nil
	}

	report := evalUpdatePullRequest(context.Background(), CreatedItemReport{
		Type:   "update_pull_request",
		Number: 42,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"title":     "Old title",
			"body_hash": mutableBodyHash("Old body"),
			"state":     "open",
			"base":      "main",
			"draft":     true,
			"head_sha":  "abc123",
		},
		AfterState: map[string]any{
			"title":     "New title",
			"body_hash": mutableBodyHash("New body"),
			"state":     "closed",
			"base":      "release",
			"draft":     false,
			"head_sha":  "def456",
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusAccepted, report.AdoptionStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "state_retained_and_merged", report.Signal)
}

func TestEvalUpdatePullRequestReplaced(t *testing.T) {
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"title":  "Maintainer rewrite",
			"body":   "Reworked body",
			"state":  "open",
			"merged": false,
			"base":   map[string]any{"ref": "hotfix"},
			"draft":  false,
			"head":   map[string]any{"sha": "zzz999"},
		}, nil
	}

	report := evalUpdatePullRequest(context.Background(), CreatedItemReport{
		Type:   "update_pull_request",
		Number: 42,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"title":     "Old title",
			"body_hash": mutableBodyHash("Old body"),
			"state":     "open",
			"base":      "main",
			"draft":     true,
			"head_sha":  "abc123",
		},
		AfterState: map[string]any{
			"title":     "New title",
			"body_hash": mutableBodyHash("New body"),
			"state":     "open",
			"base":      "release",
			"draft":     false,
			"head_sha":  "def456",
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusRejected, report.AdoptionStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "state_replaced", report.Signal)
}

func TestEvalRetainedUpdateMissingExecutionStateUsesEvidenceNone(t *testing.T) {
	report := evalUpdateIssue(context.Background(), CreatedItemReport{
		Type:   "update_issue",
		Number: 12,
		Repo:   "owner/repo",
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusUnknown, report.AdoptionStatus)
	assert.Equal(t, EvidenceNone, report.EvidenceStrength)
	assert.Equal(t, "missing_execution_state", report.Signal)
}

func TestEvalReplaceLabelRetained(t *testing.T) {
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"labels": []any{
				map[string]any{"name": "triage"},
				map[string]any{"name": "done"},
			},
		}, nil
	}

	report := evalReplaceLabel(context.Background(), CreatedItemReport{
		Type:   "replace_label",
		Number: 12,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"labels": []any{"triage", "in-progress"},
		},
		AfterState: map[string]any{
			"labels": []any{"triage", "done"},
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusAccepted, report.AdoptionStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "state_retained", report.Signal)
}

func TestEvalReplaceLabelRetainedWithExtraLabel(t *testing.T) {
	// An unrelated label added after execution must not cause state_replaced.
	// Before: [in-progress], After: [done], Current: [done, security]
	// Delta: added=[done], removed=[in-progress]
	// "done" is still present, "in-progress" still absent → accepted.
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"labels": []any{
				map[string]any{"name": "done"},
				map[string]any{"name": "security"},
			},
		}, nil
	}

	report := evalReplaceLabel(context.Background(), CreatedItemReport{
		Type:   "replace_label",
		Number: 12,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"labels": []any{"in-progress"},
		},
		AfterState: map[string]any{
			"labels": []any{"done"},
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusAccepted, report.AdoptionStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "state_retained", report.Signal)
}

func TestEvalReplaceLabelReverted(t *testing.T) {
	// Before: [in-progress], After: [done], Current: [in-progress]
	// Delta: added=[done], removed=[in-progress]
	// "done" absent, "in-progress" back → reverted.
	old := adoptionUpdateGHAPIGet
	t.Cleanup(func() {
		adoptionUpdateGHAPIGet = old
	})
	adoptionUpdateGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"labels": []any{
				map[string]any{"name": "in-progress"},
			},
		}, nil
	}

	report := evalReplaceLabel(context.Background(), CreatedItemReport{
		Type:   "replace_label",
		Number: 12,
		Repo:   "owner/repo",
		BeforeState: map[string]any{
			"labels": []any{"in-progress"},
		},
		AfterState: map[string]any{
			"labels": []any{"done"},
		},
	}, "owner/repo")

	assert.Equal(t, AdoptionStatusRejected, report.AdoptionStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "state_reverted", report.Signal)
}
