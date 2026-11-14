package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

func TestIssueSyncCancelledCallerReleasesOnlyKnownWrites(t *testing.T) {
	for _, phase := range []string{"read", "acknowledged-write", "unknown-write"} {
		t.Run(phase, func(t *testing.T) {
			branch, server := newQueueAPI(t)
			commits, _ := terminalIssueFixture(t)
			installMockLog(t, server, commits)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := syncGraph(t, nil, nil)
			graph := func(ctx context.Context, query string, variables map[string]any, response any) error {
				if phase == "read" || strings.HasPrefix(query, "mutation") {
					cancel()
					if phase != "acknowledged-write" {
						return ctx.Err()
					}
				}
				return base(ctx, query, variables, response)
			}
			report, err := branch.synchronizeCheckedIssueTransition(ctx, "cancel-mirror", &IssuesSettings{Label: "work"}, graph, nil)
			if phase == "acknowledged-write" {
				if err != nil || report.Synchronized != 1 {
					t.Fatalf("acknowledged mutation lost its result to caller cancellation: %+v %v", report, err)
				}
			} else if err == nil {
				t.Fatal("cancelled caller was accepted")
			}
			want := 0
			if phase == "unknown-write" {
				want = 2
			}
			if len(server.projectionRefs) != want {
				t.Fatalf("cleanup did not distinguish acknowledgement from uncertainty: %v", server.projectionRefs)
			}
		})
	}
}

func TestIssueSyncLabelDefinitiveRejectionAndUncertainty(t *testing.T) {
	duplicate := &api.GraphQLError{Errors: []api.GraphQLErrorItem{{Type: "UNPROCESSABLE", Path: []any{"createLabel"}, Message: "PRIVATE API VALUE"}}}
	for _, test := range []struct {
		name      string
		err       error
		partial   bool
		readID    string
		readName  string
		wantLabel bool
		uncertain bool
	}{
		{"duplicate-exact", duplicate, false, "repo", "work", true, false},
		{"duplicate-foreign-repository", duplicate, false, "other", "work", false, false},
		{"duplicate-wrong-label", duplicate, false, "repo", "other", false, false},
		{"duplicate-missing-label", duplicate, false, "repo", "", false, false},
		{"confirmed-forbidden", &api.HTTPError{StatusCode: http.StatusForbidden}, false, "repo", "", false, false},
		{"transport-unknown", errors.New("PRIVATE API VALUE"), false, "", "", false, true},
		{"partial-response", duplicate, true, "", "", false, true},
		{"unknown-graphql-type", &api.GraphQLError{Errors: []api.GraphQLErrorItem{{Type: "INTERNAL", Path: []any{"createLabel"}}}}, false, "", "", false, true},
		{"wrong-error-path", &api.GraphQLError{Errors: []api.GraphQLErrorItem{{Type: "UNPROCESSABLE", Path: []any{"other"}}}}, false, "", "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			graph := func(_ context.Context, query string, variables map[string]any, response any) error {
				if variables["repository"] != "repo" || variables["name"] != "work" {
					t.Fatal("label scope changed")
				}
				if strings.HasPrefix(query, "mutation") {
					if test.partial {
						if err := json.Unmarshal([]byte(`{"createLabel":{"label":{"id":"partial"}}}`), response); err != nil {
							t.Fatal(err)
						}
					}
					return test.err
				}
				reads++
				var label any
				if test.readName != "" {
					label = map[string]string{"id": "existing", "name": test.readName}
				}
				data, err := json.Marshal(map[string]any{"node": map[string]any{"id": test.readID, "label": label}})
				if err != nil {
					return err
				}
				return json.Unmarshal(data, response)
			}
			uncertain := false
			label, err := ensureIssueSyncLabel(context.Background(), graph, "repo", "work", nil, &uncertain)
			if (err == nil) != test.wantLabel || uncertain != test.uncertain {
				t.Fatalf("label recovery mismatch: %+v uncertain=%v err=%v", label, uncertain, err)
			}
			if test.wantLabel && (label.ID != "existing" || reads != 1) || test.uncertain && reads != 0 {
				t.Fatal("unknown mutation was recovered without an exact definitive rejection")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE API VALUE") {
				t.Fatal("raw API diagnostics escaped")
			}
		})
	}
}

func TestIssueSyncDefinitiveLabelRejectionReleasesCoordination(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, _ := terminalIssueFixture(t)
	installMockLog(t, server, commits)
	base := syncGraph(t, nil, func(result map[string]any) {
		result["repository"].(map[string]any)["status"] = nil
	})
	graph := func(ctx context.Context, query string, variables map[string]any, response any) error {
		if strings.Contains(query, "createLabel") {
			return &api.GraphQLError{Errors: []api.GraphQLErrorItem{{Type: "FORBIDDEN", Path: []any{"createLabel"}}}}
		}
		if strings.Contains(query, "node(id:$repository)") {
			return json.Unmarshal([]byte(`{"node":{"id":"repo","label":null}}`), response)
		}
		return base(ctx, query, variables, response)
	}
	if _, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"}, graph, nil); err == nil || len(server.projectionRefs) != 0 {
		t.Fatalf("definitive no-write retained a fence: %v %v", server.projectionRefs, err)
	}
}

func TestIssueSyncPublishedReceiptMustMatchCheckedHistory(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, _ := terminalIssueFixture(t)
	installMockLog(t, server, commits)
	publication := commits[len(commits)-1]
	publication.Actor.Principal = "999"
	calls := 0
	report, err := branch.synchronizeCheckedIssueTransition(context.Background(), publication.Request.ID, &IssuesSettings{Label: "work"},
		func(context.Context, string, map[string]any, any) error { calls++; return nil }, &publication)
	if err == nil || calls != 0 || report.Synchronized != 0 || len(server.projectionRefs) != 0 {
		t.Fatal("supplied publication bypassed the checked committed receipt")
	}
}

func TestIssueSyncPublishedReceiptFailsClosedAfterCheckpointNormalization(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, _ := terminalIssueFixture(t)
	publication := commits[len(commits)-1]
	installMockLog(t, server, commits)
	prior := server.head
	checkpoint, err := CompactCheckpoint(commits, prior, testActor("administrator"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	installMockLog(t, server, checkpoint)
	native := server.commits[server.head]
	native.Parents = []string{prior}
	server.commits[server.head] = native
	calls := 0
	_, err = branch.synchronizeCheckedIssueTransition(context.Background(), publication.Request.ID, &IssuesSettings{Label: "work"},
		func(context.Context, string, map[string]any, any) error { calls++; return nil }, &publication)
	var protocol *ProtocolError
	if !errors.As(err, &protocol) || protocol.Code != "projection_unauthorized" || calls != 0 || len(server.projectionRefs) != 0 {
		t.Fatalf("normalized receipt widened automatic publication authority: %v", err)
	}
	state, err := Replay(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	targets, pending, err := state.issueTransitionSelection(publication.Request.ID)
	if err != nil || len(targets) != 1 || len(pending) != 0 {
		t.Fatalf("strict automatic comparison lost explicit privileged recovery scope: %v", err)
	}
}

func TestIssueSyncPublishedWriterDisabledAndForeignPublisher(t *testing.T) {
	for _, principal := range []string{testPrincipal, "999"} {
		t.Run(principal, func(t *testing.T) {
			branch, server := newQueueAPI(t)
			server.noAdmin = true
			publication := &Publication{Commit: &QueueCommit{Request: Request{ID: "terminal"}, Actor: Actor{Role: "reconciler", Principal: principal, Repository: testRepository}}}
			report, err := branch.SynchronizePublishedIssueTransition(context.Background(), publication)
			if principal == testPrincipal {
				if err != nil || !report.Disabled || server.ledgerCalls != 0 {
					t.Fatalf("disabled projection escalated writer authority or touched queue: %+v %v", report, err)
				}
			} else if err == nil || server.ledgerCalls != 0 {
				t.Fatal("automatic synchronization accepted a foreign publisher")
			}
		})
	}
}

func TestIssueSyncPublishedWriterEnabledRequiresOwnExactCommittedReceipt(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	commits, assignment, worker := awBoundAssignment(t)
	request, err := NewRequest("finish-aw", "finish", worker, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: worker.ClaimHandle, Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, worker, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := state.Dispatches[assignment.DispatchID]
	member := assignment.Claims[0]
	evidence := Evidence{Kind: "delivery", Source: "verified_receipts", Repository: worker.Repository,
		Workflow: dispatch.Profile.Workflow, Ref: dispatch.Profile.Ref, Principal: worker.Principal,
		CheckedAt: 4000, RunID: worker.RunID, RunAttempt: 1, Receipt: "verified-none"}
	commits = testOperations(t, commits, testActor("reconciler"), "result-aw", "result", mustOp(t, map[string]any{
		"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID, "completion_id": state.Works[member.WorkID].CompletionID,
		"descriptor": map[string]any{}, "evidence": evidence,
	}))
	for _, change := range []string{"none", "foreign-principal", "foreign-repository", "uncommitted-receipt"} {
		t.Run(change, func(t *testing.T) {
			branch, server := newQueueAPI(t)
			server.noAdmin, server.settings = true, `{"work_queue":{"issues":true}}`
			installMockLog(t, server, commits)
			commit := commits[len(commits)-1]
			switch change {
			case "foreign-principal":
				commit.Actor.Principal = "999"
			case "foreign-repository":
				commit.Actor.Repository = "other/repo"
			case "uncommitted-receipt":
				commit.At++
			}
			result, err := branch.SynchronizePublishedIssueTransition(context.Background(), &Publication{Commit: &commit})
			var protocol *ProtocolError
			if !errors.As(err, &protocol) {
				t.Fatalf("expected safe, explicit pending/authority diagnostic: %+v %v", result, err)
			}
			if change == "none" {
				if protocol.Code != "projection_receipt_pending" || len(result.Pending) != 1 || result.Pending[0].WorkID != member.WorkID || server.ledgerCalls == 0 {
					t.Fatalf("valid native writer did not reach its exact checked terminal scope: %+v %v", result, err)
				}
			} else if protocol.Code != "projection_unauthorized" || len(result.Pending) != 0 {
				t.Fatalf("writer acquired another publisher or uncommitted scope: %+v %v", result, err)
			}
			if len(server.projectionRefs) != 0 || server.refWrites != 0 {
				t.Fatal("pending receipt/unauthorized publication mutated native or queue state")
			}
		})
	}
}

func TestIssueSyncPendingReceiptsDoNotBlockValidMirrors(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, workID := terminalIssueFixture(t)
	commits = commits[:len(commits)-1]
	work := testNode(t, commits, "missing-mirror")
	producer := commits[1].Actor
	request, err := NewRequest("missing-admission", "submit", producer, SubmitParameters{Nodes: []WorkDefinition{work}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, producer, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	commits = testOperations(t, commits, testActor("administrator"), "cancel-mirror", "cancel_work",
		mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "operator_cancelled"}),
		mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": workID, "reason": "operator_cancelled"}))
	installMockLog(t, server, commits)
	result, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"}, syncGraph(t, nil, nil), nil)
	if err == nil || result.Synchronized != 1 || len(result.Pending) != 1 || result.Pending[0].WorkID != work.WorkID ||
		result.Pending[0].Reason != "projection_receipt_pending" || len(server.projectionRefs) != 0 || server.refWrites != 0 {
		t.Fatalf("missing receipts blocked valid synchronization or disappeared: %+v %v", result, err)
	}
}

func TestIssueTransitionSummaryRetainsCheckedRunsClaimsAndVerifiedOutcomes(t *testing.T) {
	commits, workID := terminalIssueFixture(t)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	targets, _, err := state.issueTransitionSelection("cancel-mirror")
	if err != nil {
		t.Fatal(err)
	}
	work := state.Works[workID]
	work.State, work.Barrier = "completed", "verified"
	work.Payload = json.RawMessage(`{"description":"PRIVATE PAYLOAD","token":"PRIVATE SECRET"}`)
	work.Result = json.RawMessage(`{"outputs":[{"resource":{"kind":"pull_request","repository":"owner/repo","number":"12"}},{"resource":{"kind":"issue","repository":"owner/repo","number":13}},{"resource":{"kind":"issue","repository":"malicious)[x](https://evil.example","number":"14"}}]}`)
	for i := 1; i <= 2; i++ {
		id, dispatch := fmt.Sprintf("c_claim_%d", i), fmt.Sprintf("d_%d", i)
		state.Claims[id] = &ClaimState{WorkID: workID, ClaimID: id, DispatchID: dispatch}
		state.Dispatches[dispatch] = &DispatchState{Run: &RunBinding{Repository: testRepository, RunID: strconv.Itoa(200 + i), RunAttempt: i}}
	}
	target := targets[0]
	target.Status = "Needs review"
	body := issueTransitionSummary(target, state, "work-queue")
	for _, expected := range []string{"Status: **Needs review**", "Runs and Claims", "View admitting run", "/actions/runs/101/attempts/1", "c_claim_1", "c_claim_2", "/actions/runs/201/attempts/1", "/actions/runs/202/attempts/2", "Verified pull_request: https://github.com/owner/repo/pull/12", "Verified issue: https://github.com/owner/repo/issues/13", "Git is authoritative", "A PR is not a merge"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("canonical summary lost checked state section %q: %s", expected, body)
		}
	}
	for _, forbidden := range []string{"PRIVATE", "evil.example", "ignored`payload"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unsafe summary field rendered: %s", forbidden)
		}
	}
	work.Barrier = "failed"
	if body := issueTransitionSummary(target, state, "work-queue"); strings.Contains(body, "Verified pull_request:") {
		t.Fatal("failed delivery rendered unchecked outcomes as verified")
	}
	for i := 3; i < 1000; i++ {
		id := fmt.Sprintf("c_claim_%d", i)
		state.Claims[id] = &ClaimState{WorkID: workID, ClaimID: id, DispatchID: "d_1"}
	}
	body = issueTransitionSummary(target, state, "work-queue")
	if len(body) > 32*1024 || !strings.Contains(body, "Additional Runs and Claims are retained") {
		t.Fatal("canonical history rendering is unbounded or silently truncated")
	}
}
