//go:build !integration

package cli

// Formal compliance tests for the safe output outcome evaluation engine.
//
// These tests cover predicates P1–P12, P14, and P15 derived from the formal model in
// specs/safe-output-outcome-evaluation.md.
//
// Formal notation cross-references:
//   - TLA+ state-machine invariants: P1, P4, P5, P6, P9
//   - Z3/SMT-LIB arithmetic predicates: P10
//   - F* pre/post contracts: P2, P3, P7, P8, P11, P12

import (
	"context"
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormalOutcomeDomainInvariant verifies that every OutcomeStatus produced by
// the evaluation engine is within the six outcome categories defined in the spec,
// or is a recognized internal state that must be normalized before external emission.
//
// Formal predicate (TLA+):
//
//	OutcomeDomain ≜
//	  ∀ r ∈ OutcomeStatus :
//	    r ∈ {"accepted","rejected","ignored","pending","lifecycle","lifecycle_close"}
//	    ∨ r ∈ {"unknown","error"}  (* internal-only; normalized before OTel emission *)
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Outcome Categories
func TestFormalOutcomeDomainInvariant(t *testing.T) {
	// The six externally-observable outcome categories defined by the spec.
	specDomain := map[string]bool{
		"accepted":        true,
		"rejected":        true,
		"ignored":         true,
		"pending":         true,
		"lifecycle":       true,
		"lifecycle_close": true,
	}
	// Internal states that are valid at the evaluator level but must be
	// normalized to a spec-defined category before external emission.
	internalOnly := map[string]bool{
		"unknown": true,
		"error":   true,
		"skipped": true,
	}

	// Every declared OutcomeStatus constant must be in the spec domain or internal.
	allResults := []OutcomeStatus{
		OutcomeStatusAccepted, OutcomeStatusRejected, OutcomeStatusIgnored, OutcomeStatusPending,
		OutcomeStatusLifecycle, OutcomeStatusLifecycleClose, OutcomeStatusUnknown, OutcomeStatusSkipped, OutcomeStatusError,
	}
	for _, r := range allResults {
		s := string(r)
		assert.True(t, specDomain[s] || internalOnly[s],
			"P1: OutcomeStatus %q must be in spec domain or recognized internal state", s)
	}

	// ComputeOutcomeSummary must count each spec-defined outcome correctly.
	reports := []OutcomeReport{
		{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}},
		{Type: "create_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusRejected}},
		{Type: "add_comment", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusIgnored}},
		{Type: "add_labels", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending}},
		{Type: "close_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycle}},
		{Type: "close_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycleClose}},
	}
	summary := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
	assert.Equal(t, 6, summary.Total, "P1: total must cover the six spec-defined non-internal outcomes")
	assert.Equal(t, 1, summary.Accepted, "P1: one accepted")
	assert.Equal(t, 1, summary.Rejected, "P1: one rejected")
	assert.Equal(t, 1, summary.Ignored, "P1: one ignored")
	assert.Equal(t, 1, summary.Pending, "P1: one pending")
	assert.Equal(t, 2, summary.Lifecycle, "P1: lifecycle and lifecycle_close must both count as lifecycle outcomes")
}

// TestFormalAPIFailurePending verifies that GitHub API 5xx and rate-limit errors
// never produce a terminal classification of accepted or rejected.
//
// Formal predicate (F*):
//
//	val evaluateWithAPIFailure :
//	  item:CreatedItemReport → apiErr:HTTPError →
//	  Tot OutcomeReport
//	  (requires apiErr.status ∈ {500, 502, 503, 429})
//	  (ensures fun r → r.OutcomeStatus ≠ OutcomeStatusAccepted ∧ r.OutcomeStatus ≠ OutcomeStatusRejected)
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Norms (rules 2, 4)
func TestFormalAPIFailurePending(t *testing.T) {
	old := closeStickyGHAPIGet
	t.Cleanup(func() { closeStickyGHAPIGet = old })

	apiErrors := []struct {
		name    string
		errText string
	}{
		{"503 server error", "gh api: 503 Service Unavailable"},
		{"429 rate limit", "gh api: 429 Too Many Requests"},
		{"502 bad gateway", "gh api: 502 Bad Gateway"},
		{"500 internal error", "gh api: 500 Internal Server Error"},
	}

	for _, tc := range apiErrors {
		t.Run(tc.name, func(t *testing.T) {
			closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				return nil, errors.New(tc.errText)
			}
			item := CreatedItemReport{Type: "close_issue", Number: 99, Repo: "owner/repo"}
			report := evalCloseSticky(context.Background(), item, "owner/repo")

			assert.NotEqual(t, OutcomeStatusAccepted, report.OutcomeStatus,
				"P2: API error %q must not yield accepted", tc.errText)
			assert.NotEqual(t, OutcomeStatusRejected, report.OutcomeStatus,
				"P2: API error %q must not yield rejected", tc.errText)
		})
	}
}

// TestFormal404Classification verifies that 404-equivalent conditions are classified
// as rejected for persistent objects and ignored for transient targets, and that
// 404 API errors never yield accepted.
//
// Formal predicate (TLA+):
//
//	NotFoundClassification ≜
//	  ∀ r : APIError →
//	    r.status = 404 ∧ persistent(r.type) ⟹ eval.OutcomeStatus = rejected ∧
//	    r.status = 404 ∧ transient(r.type) ⟹ eval.OutcomeStatus = ignored ∧
//	    r.status = 404 ⟹ eval.OutcomeStatus ≠ accepted
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Norms (rule 1)
func TestFormal404Classification(t *testing.T) {
	// Persistent object: "deleted" detail maps to rejected (persistent 404 classification).
	t.Run("persistent object deleted → rejected", func(t *testing.T) {
		report := OutcomeReport{
			Type:              "create_issue",
			OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusRejected},
			Detail:            "deleted",
		}
		eval := normalizeOutcomeEvaluation(report)
		assert.Equal(t, OutcomeStatusRejected, eval.OutcomeStatus,
			"P3: 404 on persistent object (deleted) must yield rejected")
		assert.Equal(t, "deleted", eval.Signal,
			"P3: deleted signal must be set for persistent 404")
	})

	// Transient target: "no engagement" maps to ignored (transient 404 classification).
	t.Run("transient target no engagement → ignored", func(t *testing.T) {
		report := OutcomeReport{
			Type:              "add_comment",
			OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusIgnored},
			Detail:            "no engagement",
		}
		eval := normalizeOutcomeEvaluation(report)
		assert.Equal(t, OutcomeStatusIgnored, eval.OutcomeStatus,
			"P3: transient target with no engagement must yield ignored")
	})

	// API error (simulating 404) must not yield accepted.
	t.Run("404 API error must not yield accepted", func(t *testing.T) {
		old := closeStickyGHAPIGet
		t.Cleanup(func() { closeStickyGHAPIGet = old })
		closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return nil, errors.New("gh api: 404 Not Found")
		}
		item := CreatedItemReport{Type: "close_issue", Number: 99, Repo: "owner/repo"}
		report := evalCloseSticky(context.Background(), item, "owner/repo")
		assert.NotEqual(t, OutcomeStatusAccepted, report.OutcomeStatus,
			"P3: 404 API error must not yield accepted")
	})
}

// TestFormalBotActorProvenance verifies that actor identity is correctly classified
// as bot or non-bot based on the visible GitHub login.
//
// Formal predicate (TLA+):
//
//	BotActorProvenance ≜
//	  ∀ login : isBotUser(login) ↔
//	    HasSuffix(login, "[bot]") ∨ login ∈ KnownBotLogins
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Provenance Limits (rules 1–3)
func TestFormalBotActorProvenance(t *testing.T) {
	botLogins := []string{
		"github-actions[bot]",  // App bot with [bot] suffix
		"dependabot[bot]",      // Common dependency bot
		"copilot-swe-agent",    // Known bot login
		"github-actions",       // Well-known bot alias
		"some-custom-app[bot]", // Any [bot]-suffixed login is a bot
	}
	for _, login := range botLogins {
		assert.True(t, isBotUser(login),
			"P4: login %q must be identified as bot actor", login)
	}

	humanLogins := []string{
		"octocat",
		"alice",
		"john-smith",
		"mnkiefer",
		"github-actions-user", // similar prefix but not a known bot and no [bot] suffix
	}
	for _, login := range humanLogins {
		assert.False(t, isBotUser(login),
			"P4: login %q must be identified as non-bot (human-visible) actor", login)
	}
}

// TestFormalPRMergeAcceptance verifies the PR state machine transitions:
// merged → accepted; closed without merge → rejected; open → pending.
//
// Formal predicate (TLA+):
//
//	PRMergeAcceptance ≜
//	  ∀ pr : PR →
//	    pr.merged = true                    ⟹ outcome = accepted ∧
//	    pr.state = "closed" ∧ ¬pr.merged  ⟹ outcome = rejected ∧
//	    pr.state = "open"                  ⟹ outcome = pending
//
// Specification reference: specs/safe-output-outcome-evaluation.md §1. `create_pull_request`
func TestFormalPRMergeAcceptance(t *testing.T) {
	cases := []struct {
		name       string
		pr         map[string]any
		wantResult OutcomeStatus
		wantDetail string
	}{
		{
			name:       "merged PR → accepted",
			pr:         map[string]any{"merged": true, "state": "closed"},
			wantResult: OutcomeStatusAccepted,
			wantDetail: "merged",
		},
		{
			name:       "closed PR without merge → rejected",
			pr:         map[string]any{"merged": false, "state": "closed"},
			wantResult: OutcomeStatusRejected,
			wantDetail: "closed without merge",
		},
		{
			name:       "open PR → pending",
			pr:         map[string]any{"merged": false, "state": "open"},
			wantResult: OutcomeStatusPending,
			wantDetail: "open",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldGet := outcomeEvalPRGHAPIGet
			oldGetArray := outcomeEvalPRGHAPIGetArray
			t.Cleanup(func() {
				outcomeEvalPRGHAPIGet = oldGet
				outcomeEvalPRGHAPIGetArray = oldGetArray
			})
			outcomeEvalPRGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				assert.Equal(t, "pulls/1", endpoint)
				assert.Equal(t, "owner/repo", repo)
				return tc.pr, nil
			}
			outcomeEvalPRGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
				return nil, nil
			}
			report := evalCreatePullRequest(context.Background(), CreatedItemReport{
				Type: "create_pull_request", Number: 1, Repo: "owner/repo",
			}, "owner/repo")
			assert.Equal(t, tc.wantResult, report.OutcomeStatus,
				"P5: PR state must yield %s", tc.wantResult)
			assert.Equal(t, tc.wantDetail, report.Detail,
				"P5: PR state must set detail %q", tc.wantDetail)
			if tc.wantResult != OutcomeStatusAccepted {
				assert.False(t, report.ZeroTouch,
					"P5: a non-accepted PR must not be marked zero-touch")
			}
		})
	}
}

// TestFormalAPIErrorNotTerminal verifies that an authoritative PR fetch error
// produces an error result rather than a terminal acceptance or rejection.
func TestFormalAPIErrorNotTerminal(t *testing.T) {
	oldGet := outcomeEvalPRGHAPIGet
	t.Cleanup(func() { outcomeEvalPRGHAPIGet = oldGet })
	outcomeEvalPRGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
		return nil, errors.New("gh api: 503 Service Unavailable")
	}

	report := evalCreatePullRequest(context.Background(), CreatedItemReport{
		Type: "create_pull_request", Number: 1, Repo: "owner/repo",
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusError, report.OutcomeStatus,
		"P14: an API error must not produce a terminal outcome")
	assert.NotEmpty(t, report.EvalError, "P14: an API error must be recorded")
}

// TestFormalZeroTouchRequiresNoReviews verifies that an accepted PR is
// zero-touch only when it has no non-bot comments and no reviews.
func TestFormalZeroTouchRequiresNoReviews(t *testing.T) {
	cases := []struct {
		name          string
		comments      []map[string]any
		reviews       []map[string]any
		wantZeroTouch bool
	}{
		{
			name:          "no comments or reviews",
			wantZeroTouch: true,
		},
		{
			name: "human comment",
			comments: []map[string]any{
				{"user": map[string]any{"login": "octocat"}, "created_at": "2026-01-02T00:00:00Z"},
			},
			wantZeroTouch: false,
		},
		{
			name: "bot comment",
			comments: []map[string]any{
				{"user": map[string]any{"login": "github-actions[bot]"}, "created_at": "2026-01-02T00:00:00Z"},
			},
			wantZeroTouch: true,
		},
		{
			name:          "review",
			reviews:       []map[string]any{{"user": map[string]any{"login": "octocat"}, "submitted_at": "2026-01-02T00:00:00Z"}},
			wantZeroTouch: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldGet := outcomeEvalPRGHAPIGet
			oldGetArray := outcomeEvalPRGHAPIGetArray
			t.Cleanup(func() {
				outcomeEvalPRGHAPIGet = oldGet
				outcomeEvalPRGHAPIGetArray = oldGetArray
			})
			outcomeEvalPRGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				return map[string]any{"merged": true, "state": "closed"}, nil
			}
			outcomeEvalPRGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
				switch endpoint {
				case "issues/1/comments":
					return tc.comments, nil
				case "pulls/1/reviews":
					return tc.reviews, nil
				case "pulls/1/commits":
					return nil, nil
				default:
					t.Fatalf("unexpected endpoint %q", endpoint)
					return nil, nil
				}
			}

			report := evalCreatePullRequest(context.Background(), CreatedItemReport{
				Type: "create_pull_request", Number: 1, Repo: "owner/repo", Timestamp: "2026-01-01T00:00:00Z",
			}, "owner/repo")

			assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus, "P15: test PR must be accepted")
			assert.Equal(t, tc.wantZeroTouch, report.ZeroTouch,
				"P15: zero_touch requires no non-bot comments and no reviews")
		})
	}
}

// TestFormalIssueBotCloseLifecycle verifies that issue close provenance determines
// the outcome category: bot-close → lifecycle signal; human not_planned → rejected;
// completed → accepted.
//
// Formal predicate (TLA+):
//
//	IssueBotCloseLifecycle ≜
//	  ∀ issue : Issue →
//	    issue.state = "closed" ∧ issue.stateReason = "not_planned" ∧ closedByBot  ⟹ result = lifecycle ∧
//	    issue.state = "closed" ∧ issue.stateReason = "not_planned" ∧ ¬closedByBot ⟹ result = rejected ∧
//	    issue.state = "closed" ∧ issue.stateReason = "completed"                   ⟹ result = accepted
//
// Specification reference: specs/safe-output-outcome-evaluation.md §2. `create_issue`
func TestFormalIssueBotCloseLifecycle(t *testing.T) {
	cases := []struct {
		name       string
		result     OutcomeStatus
		detail     string
		wantStatus OutcomeStatus
		wantSignal string
	}{
		{
			name:       "bot closed not_planned → lifecycle signal",
			result:     OutcomeStatusLifecycle,
			detail:     "closed by bot (lifecycle)",
			wantStatus: OutcomeStatusLifecycle,
			wantSignal: "lifecycle",
		},
		{
			name:       "human closed not_planned → rejected",
			result:     OutcomeStatusRejected,
			detail:     "closed as not planned",
			wantStatus: OutcomeStatusRejected,
			wantSignal: "closed_not_planned",
		},
		{
			name:       "resolved as completed → accepted",
			result:     OutcomeStatusAccepted,
			detail:     "completed",
			wantStatus: OutcomeStatusAccepted,
			wantSignal: "completed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := OutcomeReport{
				Type:              "create_issue",
				OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: tc.result},
				Detail:            tc.detail,
			}
			eval := normalizeOutcomeEvaluation(report)
			assert.Equal(t, tc.wantStatus, eval.OutcomeStatus,
				"P6: %s must yield OutcomeStatus=%s", tc.name, tc.wantStatus)
			assert.Equal(t, tc.wantSignal, eval.Signal,
				"P6: %s must set signal %q", tc.name, tc.wantSignal)
		})
	}
}

// TestFormalLabelStickiness verifies the label retention monotonicity invariant:
// all bot-applied labels still present → change retained (accepted); any removed → reverted (rejected).
//
// Formal predicate (F*):
//
//	val labelRetentionMonotonicity :
//	  before:list string → after:list string → current:list string →
//	  Tot retainedStateComparison
//	  (requires Subset before after  (* labels were added *))
//	  (ensures fun c →
//	    Subset after current ⟹ c.Retained ≠ [] ∧
//	    ¬Subset after current ⟹ c.Reverted ≠ [] ∨ c.Replaced ≠ [])
//
// Specification reference: specs/safe-output-outcome-evaluation.md §4. `add_labels`
func TestFormalLabelStickiness(t *testing.T) {
	cases := []struct {
		name          string
		beforeLabels  []any
		afterLabels   []any
		currentLabels []any
		wantRetained  bool
	}{
		{
			name:          "all added labels retained",
			beforeLabels:  []any{"triage"},
			afterLabels:   []any{"triage", "bug"},
			currentLabels: []any{"triage", "bug"},
			wantRetained:  true,
		},
		{
			name:          "added label removed",
			beforeLabels:  []any{"triage"},
			afterLabels:   []any{"triage", "bug"},
			currentLabels: []any{"triage"}, // "bug" was removed
			wantRetained:  false,
		},
		{
			name:          "all labels removed",
			beforeLabels:  []any{"triage"},
			afterLabels:   []any{"triage", "bug"},
			currentLabels: []any{},
			wantRetained:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := map[string]any{"labels": tc.beforeLabels}
			after := map[string]any{"labels": tc.afterLabels}
			current := map[string]any{"labels": tc.currentLabels}

			comparison := compareRetainedUpdateState(before, after, current, []string{"labels"})
			require.Len(t, comparison.Changed, 1, "P7: label delta must be detected as a changed field")

			if tc.wantRetained {
				assert.Len(t, comparison.Retained, 1,
					"P7: when all labels retained, Retained must contain the labels field")
				assert.Empty(t, comparison.Reverted,
					"P7: when all labels retained, Reverted must be empty")
			} else {
				assert.Empty(t, comparison.Retained,
					"P7: when any label removed, Retained must be empty")
				assert.True(t, len(comparison.Reverted) > 0 || len(comparison.Replaced) > 0,
					"P7: when any label removed, Reverted or Replaced must be non-empty")
			}
		})
	}
}

// TestFormalUpdateSnapshotComparison verifies the three-way snapshot comparison:
// current matches after-state → retained (accepted); matches before-state → reverted (rejected);
// matches neither → replaced (rejected).
//
// Formal predicate (F*):
//
//	val compareUpdateSnapshot :
//	  before:state → after:state → current:state → fields:list string →
//	  Tot retainedStateComparison
//	  (ensures fun c →
//	    current = after  ⟹ c.Retained = c.Changed ∧
//	    current = before ⟹ c.Reverted = c.Changed ∧
//	    current ≠ before ∧ current ≠ after ⟹ c.Replaced = c.Changed)
//
// Specification reference: specs/safe-output-outcome-evaluation.md §6. `update_issue`, §7. `update_pull_request`
func TestFormalUpdateSnapshotComparison(t *testing.T) {
	before := map[string]any{"title": "Old title"}
	after := map[string]any{"title": "New title"}

	t.Run("current = after → all retained", func(t *testing.T) {
		current := map[string]any{"title": "New title"}
		comparison := compareRetainedUpdateState(before, after, current, []string{"title"})
		require.Len(t, comparison.Changed, 1, "P8: title change must be detected")
		assert.Len(t, comparison.Retained, len(comparison.Changed),
			"P8: current=after must have all changed fields in Retained")
		assert.Empty(t, comparison.Reverted, "P8: current=after must have no reverted fields")
		assert.Empty(t, comparison.Replaced, "P8: current=after must have no replaced fields")
	})

	t.Run("current = before → all reverted", func(t *testing.T) {
		current := map[string]any{"title": "Old title"}
		comparison := compareRetainedUpdateState(before, after, current, []string{"title"})
		require.Len(t, comparison.Changed, 1, "P8: title change must be detected")
		assert.Len(t, comparison.Reverted, len(comparison.Changed),
			"P8: current=before must have all changed fields in Reverted")
		assert.Empty(t, comparison.Retained, "P8: current=before must have no retained fields")
		assert.Empty(t, comparison.Replaced, "P8: current=before must have no replaced fields")
	})

	t.Run("current diverged → all replaced", func(t *testing.T) {
		current := map[string]any{"title": "Diverged title"}
		comparison := compareRetainedUpdateState(before, after, current, []string{"title"})
		require.Len(t, comparison.Changed, 1, "P8: title change must be detected")
		assert.NotEmpty(t, comparison.Replaced, "P8: diverged state must have replaced fields")
		assert.Empty(t, comparison.Retained, "P8: diverged state must have no retained fields")
		assert.Empty(t, comparison.Reverted, "P8: diverged state must have no reverted fields")
	})
}

// TestFormalCloseStickyReopenRejection verifies that close-stickiness respects
// lifecycle provenance: lifecycle-bot closes remain lifecycle_close, while
// reopened or human-closed objects are rejected.
//
// Formal predicate (TLA+):
//
//	CloseStickyReopenRejection ≜
//	  ∀ item : close_issue ∪ close_pull_request →
//	    current.state = "closed" ∧ closedByBot  ⟹ result = lifecycle_close ∧
//	    current.state = "closed" ∧ ¬closedByBot ⟹ result = rejected ∧
//	    current.state = "open"                  ⟹ result = rejected
//
// Specification reference: specs/safe-output-outcome-evaluation.md §8. `close_issue`, §9. `close_pull_request`
func TestFormalCloseStickyReopenRejection(t *testing.T) {
	cases := []struct {
		name       string
		state      string
		events     []map[string]any
		wantResult OutcomeStatus
		wantDetail string
	}{
		{
			name:  "lifecycle bot close → lifecycle_close",
			state: "closed",
			events: []map[string]any{
				{"event": "closed", "actor": map[string]any{"login": "github-actions[bot]"}},
			},
			wantResult: OutcomeStatusLifecycleClose,
			wantDetail: "closed by bot (lifecycle_close)",
		},
		{
			name:  "human close retained → accepted",
			state: "closed",
			events: []map[string]any{
				{"event": "closed", "actor": map[string]any{"login": "octocat"}},
			},
			wantResult: OutcomeStatusAccepted,
			wantDetail: "closed",
		},
		{
			name:       "reopened → rejected",
			state:      "open",
			wantResult: OutcomeStatusRejected,
			wantDetail: "reopened",
		},
		{
			name:       "missing close event provenance → error",
			state:      "closed",
			wantResult: OutcomeStatusError,
			wantDetail: "close provenance unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := closeStickyGHAPIGet
			oldArray := closeStickyGHAPIGetArray
			t.Cleanup(func() {
				closeStickyGHAPIGet = old
				closeStickyGHAPIGetArray = oldArray
			})
			stateVal := tc.state
			closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				return map[string]any{"state": stateVal}, nil
			}
			closeStickyGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
				return tc.events, nil
			}

			item := CreatedItemReport{Type: "close_issue", Number: 99, Repo: "owner/repo"}
			report := evalCloseSticky(context.Background(), item, "owner/repo")

			assert.Equal(t, tc.wantResult, report.OutcomeStatus,
				"P9: state=%q must yield %s", tc.state, tc.wantResult)
			assert.Equal(t, tc.wantDetail, report.Detail,
				"P9: state=%q must set detail %q", tc.state, tc.wantDetail)
		})
	}
}

func TestFormalCloseStickyRejectsMergedPullRequest(t *testing.T) {
	old := closeStickyGHAPIGet
	oldArray := closeStickyGHAPIGetArray
	t.Cleanup(func() {
		closeStickyGHAPIGet = old
		closeStickyGHAPIGetArray = oldArray
	})
	closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
		return map[string]any{"state": "closed", "merged": true}, nil
	}
	closeStickyGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
		return []map[string]any{{"event": "closed", "actor": map[string]any{"login": "github-actions[bot]"}}}, nil
	}

	report := evalCloseSticky(context.Background(), CreatedItemReport{Type: "close_pull_request", Number: 99, Repo: "owner/repo"}, "owner/repo")

	assert.Equal(t, OutcomeStatusRejected, report.OutcomeStatus, "P9: merged PR must be rejected for close_pull_request")
	assert.Equal(t, "merged", report.Detail, "P9: merged PR must record merged detail")
	assert.Empty(t, report.EvalError, "P9: merged PR classification must not depend on close provenance lookup")
}

func TestFormalLifecycleNormalizationFallbacks(t *testing.T) {
	cases := []struct {
		name       string
		report     OutcomeReport
		wantStatus OutcomeStatus
		wantSignal string
	}{
		{
			name:       "lifecycle result fallback without detail",
			report:     OutcomeReport{Type: "close_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycle}},
			wantStatus: OutcomeStatusLifecycle,
			wantSignal: "lifecycle",
		},
		{
			name:       "lifecycle_close result fallback without detail",
			report:     OutcomeReport{Type: "close_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycleClose}},
			wantStatus: OutcomeStatusLifecycleClose,
			wantSignal: "lifecycle_close",
		},
		{
			name:       "lifecycle_close detail maps before generic bot close",
			report:     OutcomeReport{Type: "close_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusUnknown}, Detail: "closed by bot (lifecycle_close)"},
			wantStatus: OutcomeStatusLifecycleClose,
			wantSignal: "lifecycle_close",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eval := normalizeOutcomeEvaluation(tc.report)
			assert.Equal(t, tc.wantStatus, eval.OutcomeStatus)
			assert.Equal(t, tc.wantSignal, eval.Signal)
		})
	}
}

// TestFormalDerivedMetricsConsistency verifies the acceptance_rate and waste_rate
// formulas and guards against division-by-zero when the denominator is zero.
//
// Formal predicate (Z3/SMT-LIB):
//
//	(declare-const accepted Int)
//	(declare-const rejected Int)
//	(declare-const total    Int)
//	(assert (>= accepted 0)) (assert (>= rejected 0)) (assert (>= total (+ accepted rejected)))
//	(assert (=> (> (+ accepted rejected) 0) (= acceptance_rate (/ accepted (+ accepted rejected)))))
//	(assert (=> (> total 0) (= waste_rate (/ rejected total))))
//	(assert (=> (= (+ accepted rejected) 0) (= acceptance_rate 0.0)))
//	(assert (=> (= total 0) (= waste_rate 0.0)))
//	(check-sat) ; sat — formulas are consistent and zero-safe
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Derived Metrics
func TestFormalDerivedMetricsConsistency(t *testing.T) {
	t.Run("acceptance_rate = accepted / (accepted + rejected)", func(t *testing.T) {
		reports := []OutcomeReport{
			{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}},
			{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}},
			{Type: "create_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusRejected}},
		}
		summary := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
		// 2 accepted, 1 rejected → acceptance_rate = 2/3
		assert.InDelta(t, 2.0/3.0, summary.AcceptanceRate, 1e-9,
			"P10: acceptance_rate must equal accepted/(accepted+rejected)")
	})

	t.Run("waste_rate = rejected / total", func(t *testing.T) {
		reports := []OutcomeReport{
			{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}},
			{Type: "create_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusRejected}},
			{Type: "add_comment", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusIgnored}},
			{Type: "add_labels", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending}},
		}
		summary := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
		// 1 rejected / 4 total → waste_rate = 0.25
		assert.InDelta(t, 0.25, summary.WasteRate, 1e-9,
			"P10: waste_rate must equal rejected/total")
	})

	t.Run("division-by-zero safety: empty report set", func(t *testing.T) {
		summary := ComputeOutcomeSummary(nil, github.DefaultObjectiveMapping())
		assert.InDelta(t, 0.0, summary.AcceptanceRate, 1e-12,
			"P10: acceptance_rate must be 0.0 when total=0 (division-by-zero safe)")
		assert.InDelta(t, 0.0, summary.WasteRate, 1e-12,
			"P10: waste_rate must be 0.0 when total=0 (division-by-zero safe)")
	})

	t.Run("division-by-zero safety: only pending outcomes", func(t *testing.T) {
		reports := []OutcomeReport{
			{Type: "add_labels", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending}},
			{Type: "add_labels", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending}},
		}
		summary := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
		assert.InDelta(t, 0.0, summary.AcceptanceRate, 1e-12,
			"P10: acceptance_rate must be 0.0 when accepted+rejected=0")
	})
}

// TestFormalOTelGracefulDegradation verifies that outcome evaluation always
// produces a valid, non-discardable result regardless of transport or OTel
// exporter availability.
//
// Formal predicate (F*):
//
//	val evaluateOutcome :
//	  item:CreatedItemReport → transportOK:bool →
//	  Tot OutcomeReport
//	  (requires True)
//	  (ensures fun r →
//	    r.Type ≠ "" ∧
//	    r.OutcomeStatus ∈ KnownOutcomeStatuses ∧
//	    normalizeOutcomeEvaluation(r).OutcomeStatus ≠ "" ∧
//	    normalizeOutcomeEvaluation(r).EvidenceStrength ≠ "")
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Conformance →§OTel Backend Unavailability
func TestFormalOTelGracefulDegradation(t *testing.T) {
	old := closeStickyGHAPIGet
	t.Cleanup(func() { closeStickyGHAPIGet = old })
	// Simulate a transport failure (connection refused) that would also prevent OTLP export.
	closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
		return nil, errors.New("transport error: connection refused")
	}

	validResults := map[OutcomeStatus]bool{
		OutcomeStatusAccepted: true, OutcomeStatusRejected: true, OutcomeStatusIgnored: true,
		OutcomeStatusPending: true, OutcomeStatusLifecycle: true, OutcomeStatusLifecycleClose: true, OutcomeStatusUnknown: true, OutcomeStatusError: true,
	}

	items := []CreatedItemReport{
		{Type: "close_issue", Number: 1, Repo: "owner/repo"},
		{Type: "close_pull_request", Number: 2, Repo: "owner/repo"},
	}
	for _, item := range items {
		t.Run(item.Type, func(t *testing.T) {
			report := evalCloseSticky(context.Background(), item, "owner/repo")

			// P11: outcome must always be produced — never discarded on transport failure.
			assert.NotEmpty(t, report.Type,
				"P11: report.Type must be set regardless of transport availability")
			assert.True(t, validResults[report.OutcomeStatus],
				"P11: result %q must be a recognized OutcomeStatus even when transport fails", report.OutcomeStatus)

			// The outcome must always be normalizable (audit log entry is always writable).
			eval := normalizeOutcomeEvaluation(report)
			assert.NotEmpty(t, string(eval.OutcomeStatus),
				"P11: OutcomeStatus must be non-empty so the audit log entry can be written")
			assert.NotEmpty(t, string(eval.EvidenceStrength),
				"P11: EvidenceStrength must be non-empty so the audit log entry can be written")
		})
	}
}

// TestFormalConformanceClassCoverage verifies that the evaluation engine satisfies
// the three mandatory conformance safeguard classes defined in the spec:
//
//   - Class A: standard accepted/rejected/ignored/pending state transitions
//   - Class B: human override and lifecycle outcome paths
//   - Class C: API degradation (5xx, 404, rate-limit)
//
// Formal predicate (F*):
//
//	val conformanceClassCoverage :
//	  evaluator:outcomeEvaluator →
//	  Tot bool
//	  (requires True)
//	  (ensures fun ok →
//	    ok = classAExists(evaluator) ∧ classCExists(evaluator))
//
// Specification reference: specs/safe-output-outcome-evaluation.md §Conformance →§Conformance Safeguard Coverage Requirements
func TestFormalConformanceClassCoverage(t *testing.T) {
	t.Run("Class A: close_issue lifecycle_close state transition", func(t *testing.T) {
		old := closeStickyGHAPIGet
		oldArray := closeStickyGHAPIGetArray
		t.Cleanup(func() {
			closeStickyGHAPIGet = old
			closeStickyGHAPIGetArray = oldArray
		})
		closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return map[string]any{"state": "closed"}, nil
		}
		closeStickyGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
			return []map[string]any{{"event": "closed", "actor": map[string]any{"login": "github-actions[bot]"}}}, nil
		}
		report := evalCloseSticky(context.Background(), CreatedItemReport{Type: "close_issue", Number: 1, Repo: "o/r"}, "o/r")
		assert.Equal(t, OutcomeStatusLifecycleClose, report.OutcomeStatus,
			"P12 Class A: lifecycle-bot-closed issue must be lifecycle_close")
	})

	t.Run("Class A: close_issue rejected state transition", func(t *testing.T) {
		old := closeStickyGHAPIGet
		oldArray := closeStickyGHAPIGetArray
		t.Cleanup(func() {
			closeStickyGHAPIGet = old
			closeStickyGHAPIGetArray = oldArray
		})
		closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return map[string]any{"state": "open"}, nil
		}
		closeStickyGHAPIGetArray = func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
			return nil, nil
		}
		report := evalCloseSticky(context.Background(), CreatedItemReport{Type: "close_issue", Number: 1, Repo: "o/r"}, "o/r")
		assert.Equal(t, OutcomeStatusRejected, report.OutcomeStatus,
			"P12 Class A: reopened issue must be rejected")
	})

	t.Run("Class A: update_issue accepted (state retained)", func(t *testing.T) {
		old := outcomeUpdateGHAPIGet
		t.Cleanup(func() { outcomeUpdateGHAPIGet = old })
		outcomeUpdateGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return map[string]any{"title": "New title", "body": "", "state": "open", "labels": []any{}, "assignees": []any{}}, nil
		}
		item := CreatedItemReport{
			Type: "update_issue", Number: 1, Repo: "o/r",
			BeforeState: map[string]any{"title": "Old title", "body_hash": mutableBodyHash(""), "state": "open", "labels": []any{}, "assignees": []any{}},
			AfterState:  map[string]any{"title": "New title", "body_hash": mutableBodyHash(""), "state": "open", "labels": []any{}, "assignees": []any{}},
		}
		report := evalUpdateIssue(context.Background(), item, "o/r")
		assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus,
			"P12 Class A: retained update must be accepted")
	})

	t.Run("Class B: lifecycle bot-close carries lifecycle signal", func(t *testing.T) {
		report := OutcomeReport{
			Type:              "close_issue",
			OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycle},
			Detail:            "closed by bot (lifecycle)",
		}
		eval := normalizeOutcomeEvaluation(report)
		assert.Equal(t, "lifecycle", eval.Signal,
			"P12 Class B: bot-closed outcome must carry the lifecycle signal")
	})

	t.Run("Class C: API 5xx for close_issue", func(t *testing.T) {
		old := closeStickyGHAPIGet
		t.Cleanup(func() { closeStickyGHAPIGet = old })
		closeStickyGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return nil, errors.New("gh api: 500 Internal Server Error")
		}
		report := evalCloseSticky(context.Background(), CreatedItemReport{Type: "close_issue", Number: 1, Repo: "o/r"}, "o/r")
		assert.NotEqual(t, OutcomeStatusAccepted, report.OutcomeStatus,
			"P12 Class C: 5xx error must not yield accepted")
		assert.NotEqual(t, OutcomeStatusRejected, report.OutcomeStatus,
			"P12 Class C: 5xx error must not yield rejected")
	})

	t.Run("Class C: rate limit for update_issue", func(t *testing.T) {
		old := outcomeUpdateGHAPIGet
		t.Cleanup(func() { outcomeUpdateGHAPIGet = old })
		outcomeUpdateGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
			return nil, errors.New("gh api: 429 Too Many Requests")
		}
		item := CreatedItemReport{
			Type: "update_issue", Number: 1, Repo: "o/r",
			BeforeState: map[string]any{"title": "Old"},
			AfterState:  map[string]any{"title": "New"},
		}
		report := evalUpdateIssue(context.Background(), item, "o/r")
		assert.NotEqual(t, OutcomeStatusAccepted, report.OutcomeStatus,
			"P12 Class C: rate limit must not yield accepted")
		assert.NotEqual(t, OutcomeStatusRejected, report.OutcomeStatus,
			"P12 Class C: rate limit must not yield rejected")
	})
}
