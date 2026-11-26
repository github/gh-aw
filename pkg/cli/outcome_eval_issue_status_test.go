//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
)

func TestEvalSetIssueStatus(t *testing.T) {
	snapshot := func(id any, name, category, state string, reason any) map[string]any {
		return map[string]any{
			"planning_status": map[string]any{"id": id, "name": name, "category": category},
			"state":           state, "state_reason": reason,
		}
	}
	before := snapshot(2, "Backlog", "backlog", "open", nil)
	after := snapshot(4, "In progress", "started", "open", nil)
	duplicate := snapshot(7, "Duplicate", "duplicate", "closed", "duplicate")
	duplicate["duplicate_issue_id"] = 900
	retargeted := snapshot(7, "Duplicate", "duplicate", "closed", "duplicate")
	retargeted["duplicate_issue_id"] = 901
	for _, tt := range []struct {
		name     string
		current  map[string]any
		before   map[string]any
		after    map[string]any
		apiError error
		want     OutcomeStatus
		signal   string
	}{
		{"retained", after, before, after, nil, OutcomeStatusAccepted, "state_retained"},
		{"renamed and JSON numeric ID", snapshot(float64(4), "Renamed", "started", "open", nil), before, after, nil, OutcomeStatusAccepted, "state_retained"},
		{"reverted", before, before, after, nil, OutcomeStatusRejected, "state_reverted"},
		{"replaced", snapshot(8, "In review", "started", "open", nil), before, after, nil, OutcomeStatusRejected, "state_replaced"},
		{"missing execution", after, nil, after, nil, OutcomeStatusUnknown, "missing_execution_state"},
		{"missing after", after, before, nil, nil, OutcomeStatusUnknown, "missing_execution_state"},
		{"no change", after, after, after, nil, OutcomeStatusUnknown, "no_state_delta"},
		{"first status", after, map[string]any{"planning_status": nil, "state": "open"}, after, nil, OutcomeStatusAccepted, "state_retained"},
		{"reopen legacy closed issue", after, map[string]any{"planning_status": nil, "state": "closed", "state_reason": "completed"}, after, nil, OutcomeStatusAccepted, "state_retained"},
		{"missing current", map[string]any{"state": "open"}, before, after, nil, OutcomeStatusError, ""},
		{"null current", map[string]any{"planning_status": nil, "state": "open"}, before, after, nil, OutcomeStatusError, ""},
		{"future category", snapshot(4, "Future", "future", "open", nil), before, after, nil, OutcomeStatusError, ""},
		{"malformed ID", snapshot("bad", "Bad", "started", "open", nil), before, after, nil, OutcomeStatusError, ""},
		{"API failure", nil, before, after, errors.New("403 Forbidden"), OutcomeStatusError, ""},
		{"closed status retained", snapshot(5, "Done", "completed", "closed", "completed"), before, snapshot(5, "Done", "completed", "closed", "completed"), nil, OutcomeStatusAccepted, "state_retained"},
		{"inconsistent close reason", snapshot(5, "Done", "completed", "closed", "not_planned"), before, snapshot(5, "Done", "completed", "closed", "completed"), nil, OutcomeStatusError, ""},
		{"duplicate retained", duplicate, before, duplicate, nil, OutcomeStatusAccepted, "state_retained"},
		{"duplicate target replaced", retargeted, before, duplicate, nil, OutcomeStatusRejected, "state_replaced"},
		{"duplicate retarget retained", retargeted, duplicate, retargeted, nil, OutcomeStatusAccepted, "state_retained"},
		{"missing duplicate relationship", snapshot(7, "Duplicate", "duplicate", "closed", "duplicate"), before, duplicate, nil, OutcomeStatusError, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			old := outcomeUpdateGHAPIGet
			t.Cleanup(func() { outcomeUpdateGHAPIGet = old })
			oldGraphQL := outcomeIssueStatusGHAPIGraphQL
			t.Cleanup(func() { outcomeIssueStatusGHAPIGraphQL = oldGraphQL })
			outcomeIssueStatusGHAPIGraphQL = func(_ context.Context, _ string, variables map[string]any, repo string) (map[string]any, error) {
				assert.Equal(t, "owner/repo", repo)
				assert.Equal(t, map[string]any{"owner": "owner", "name": "repo", "number": 12}, variables)
				return map[string]any{"data": map[string]any{"repository": map[string]any{
					"issue": map[string]any{"duplicateOf": map[string]any{"fullDatabaseId": tt.current["duplicate_issue_id"]}},
				}}}, nil
			}
			outcomeUpdateGHAPIGet = func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				assert.Equal(t, "issues/12", endpoint)
				assert.Equal(t, "owner/repo", repo)
				return map[string]any{
					"status": tt.current["planning_status"], "state": tt.current["state"],
					"state_reason": tt.current["state_reason"],
				}, tt.apiError
			}
			report := evalSetIssueStatus(context.Background(), CreatedItemReport{
				Type: "set_issue_status", Number: 12, Repo: "owner/repo",
				BeforeState: tt.before, AfterState: tt.after,
			}, "")
			assert.Equal(t, tt.want, report.OutcomeStatus)
			assert.Equal(t, tt.signal, report.Signal)
		})
	}
	assert.NotNil(t, outcomeEvaluators["set_issue_status"])
}

func TestRetainedLifecycleIssueStatusSnapshots(t *testing.T) {
	item := CreatedItemReport{
		Type: "set_issue_status", Number: 12, Repo: "owner/repo",
		BeforeState: map[string]any{"planning_status": nil, "state": "open", "state_reason": nil},
		AfterState: map[string]any{
			"planning_status": map[string]any{"id": 7, "name": "Duplicate", "category": "duplicate"},
			"state":           "closed", "state_reason": "duplicate", "duplicate_issue_id": 900,
		},
	}

	execution := retainedLifecycleExecution(item)
	restored := retainedLifecycleItem(lifecycle.Record{
		Identity:  lifecycle.Identity{Type: item.Type, Host: "github.com", TargetRepository: item.Repo},
		Execution: execution,
	})
	assert.Equal(t, item.BeforeState, restored.BeforeState)
	assert.Equal(t, item.AfterState, restored.AfterState)
	_, ok := nativeIssueStatusState(restored.BeforeState, true)
	assert.True(t, ok)
	_, ok = nativeIssueStatusState(restored.AfterState, false)
	assert.True(t, ok)
}

func TestNativeStatusIDJSONRoundTripLargeInteger(t *testing.T) {
	var raw any
	assert.NoError(t, json.Unmarshal([]byte("1234567890"), &raw))
	id, ok := nativeStatusID(raw)
	assert.True(t, ok)
	assert.Equal(t, "1234567890", id)
}
