package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func terminalIssueFixture(t *testing.T) ([]QueueCommit, string) {
	t.Helper()
	commits := testGenesis(t, awTestPolicy)
	producer := testActor("producer")
	producer.Workflow, producer.RunID, producer.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	work := testNode(t, commits, "mirror")
	request, err := NewRequest("mirror-admission", "submit", producer, SubmitParameters{Nodes: []WorkDefinition{work}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, producer, request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	projector := producer
	projector.Role = "projector"
	commits = testOperations(t, commits, projector, "mirror-binding", "issue_link",
		mustOp(t, IssueLinkOperation{Kind: "IssueLink", WorkID: work.WorkID, Resource: Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "999", Number: "99"}, ProjectorRef: strings.Repeat("0", 40)}),
		mustOp(t, IssueCommentOperation{Kind: "IssueComment", WorkID: work.WorkID, CommentID: "summary", ProjectorRef: strings.Repeat("0", 40)}))
	commits = testOperations(t, commits, testActor("administrator"), "cancel-mirror", "cancel_work",
		mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "operator_cancelled"}))
	return commits, work.WorkID
}

func TestTerminalIssueTargetsSurviveCheckpointWithoutChangingOriginalAuthority(t *testing.T) {
	commits, workID := terminalIssueFixture(t)
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range [][]QueueCommit{commits, checkpoint} {
		state, err := Replay(log)
		if err != nil {
			t.Fatal(err)
		}
		targets, _, err := state.issueTransitionSelection("cancel-mirror")
		if err != nil || len(targets) != 1 || targets[0].WorkID != workID || targets[0].Status != "Cancelled" {
			t.Fatalf("exact terminal receipt was not retained: %+v %v", targets, err)
		}
		impostor := testActor("projector")
		impostor.Workflow, impostor.RunID, impostor.RunAttempt = ".github/workflows/other.lock.yml", "303", 1
		if state.projectionAuthority(impostor, workID, strings.Repeat("0", 40), testRepository, "") == nil {
			t.Fatal("terminal synchronization broadened original admission authority")
		}
		if _, _, err := state.issueTransitionSelection("unknown"); err == nil {
			t.Fatal("unknown request acquired terminal targets")
		}
		if targets, _, err := state.issueTransitionSelection("mirror-admission"); err != nil || len(targets) != 0 {
			t.Fatalf("admission acquired terminal transition authority: %v %v", targets, err)
		}
		state.Works[workID].BackingIssue = state.Works[workID].IssueLink
		if targets, _, err := state.issueTransitionSelection("cancel-mirror"); err != nil || len(targets) != 0 {
			t.Fatal("pre-existing backing Issue acquired automatic terminal projection")
		}
	}
}

func syncGraph(t *testing.T, mutationError error, transform func(map[string]any)) issueGraphQL {
	t.Helper()
	return func(_ context.Context, query string, variables map[string]any, response any) error {
		t.Helper()
		label := func(id, name string) map[string]string { return map[string]string{"id": id, "name": name} }
		labels := map[string]any{"nodes": []any{label("tracking", "work"), label("cancelled", "work:cancelled"), label("human", "unrelated")}, "pageInfo": map[string]any{"hasNextPage": false}}
		var result map[string]any
		if strings.HasPrefix(query, "query") {
			result = map[string]any{
				"repository": map[string]any{"id": "repo", "databaseId": 1, "nameWithOwner": testRepository,
					"tracking": label("tracking", "work"), "status": label("cancelled", "work:cancelled"),
					"issue": map[string]any{"id": "issue", "databaseId": 999, "number": 99, "labels": map[string]any{"nodes": []any{label("tracking", "work"), label("queued", "work:queued"), label("human", "unrelated")}, "pageInfo": map[string]any{"hasNextPage": false}}}},
				"node": map[string]any{"id": "summary", "body": "Status: **Queued**", "issue": map[string]string{"id": "issue"}},
			}
		} else {
			if strings.Contains(query, "createIssue") || strings.Contains(query, "addComment") || strings.Contains(query, "closeIssue") {
				t.Fatal("terminal synchronization attempted creation or closure")
			}
			if mutationError != nil {
				return mutationError
			}
			if !strings.Contains(variables["body"].(string), "Status: **Cancelled**") {
				t.Fatal("canonical summary does not reflect checked terminal state")
			}
			labelable := map[string]any{"labelable": map[string]any{"id": "issue", "labels": labels}}
			result = map[string]any{"add": labelable, "remove": labelable, "comment": map[string]any{"issueComment": map[string]any{"id": "summary", "body": variables["body"]}}}
		}
		if transform != nil {
			transform(result)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, response)
	}
}

func TestOperatorTerminalIssueSynchronizationUsesExactReceiptsAndSharedLocks(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, workID := terminalIssueFixture(t)
	installMockLog(t, server, commits)
	head := server.head
	report, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"}, syncGraph(t, nil, nil), nil)
	if err != nil || report.Synchronized != 1 {
		t.Fatalf("terminal mirror synchronization failed: %+v %v", report, err)
	}
	if len(server.projectionRefs) != 0 || server.head != head || server.refWrites != 0 {
		t.Fatal("terminal synchronization changed the queue or retained acknowledged locks")
	}
	state, err := Replay(commits)
	if err != nil || state.Works[workID].State != "cancelled" {
		t.Fatal("test lost the original authoritative cancellation")
	}
}

func TestOperatorTerminalIssueSynchronizationRetainsUnknownWritesAndRejectsLostIdentity(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted-summary", true: "unknown-write"}[unknown], func(t *testing.T) {
			branch, server := newQueueAPI(t)
			commits, _ := terminalIssueFixture(t)
			installMockLog(t, server, commits)
			var mutationError error
			var transform func(map[string]any)
			if unknown {
				mutationError = errors.New("lost native acknowledgement")
			} else {
				transform = func(result map[string]any) { result["node"] = nil }
			}
			_, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"}, syncGraph(t, mutationError, transform), nil)
			if err == nil {
				t.Fatal("uncertain write or deleted immutable summary was silently accepted")
			}
			if unknown && len(server.projectionRefs) != 2 || !unknown && len(server.projectionRefs) != 0 {
				t.Fatalf("coordination fence disagrees with native write uncertainty: %v", server.projectionRefs)
			}
		})
	}
}

func TestOperatorTerminalIssueSynchronizationRequiresRepositoryMaintainOrAdmin(t *testing.T) {
	branch, server := newQueueAPI(t)
	server.noAdmin = true
	if _, err := branch.SynchronizeIssueTransition(context.Background(), "cancel-mirror"); err == nil || server.ledgerCalls != 0 {
		t.Fatal("ordinary writer acquired terminal Issue authority")
	}
}

func TestTerminalIssueTargetStatusAndExactBarrierIdentity(t *testing.T) {
	for _, test := range []struct {
		kind, barrier, descriptor, want string
	}{
		{"DeliveryFailure", "failed", `{}`, "Needs attention"},
		{"Result", "verified", `{}`, "Done"},
		{"Result", "verified", `{"outputs":[{"type":"create_pull_request"}]}`, "Needs review"},
		{"Result", "verified", `{"outputs":[{"resource":{"kind":"pull_request"}}]}`, "Needs review"},
		{"Result", "verified", `{"outputs":[{"resource":{"kind":"pull_request","number":123}}]}`, "Needs review"},
		{"Result", "verified", `{"outputs":[{"type":"create_pull_request","resource":17}]}`, "Needs review"},
		{"Result", "verified", `{"outputs":[null,123,"unrelated",{"type":{},"resource":{"kind":[],"number":123}}]}`, "Done"},
		{"Result", "verified", `{"outputs":{"type":"create_pull_request"}}`, "Done"},
		{"Result", "verified", `{"outputs":"unrelated"}`, "Done"},
		{"Result", "verified", `{"outputs":null,"unrelated":{"number":123}}`, "Done"},
	} {
		t.Run(test.want+test.descriptor, func(t *testing.T) {
			commits, workID := terminalIssueFixture(t)
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			work := state.Works[workID]
			work.State, work.ClaimID, work.CompletionID, work.Barrier = "completed", "claim", "completion", test.barrier
			work.Result = json.RawMessage(test.descriptor)
			operation := mustOp(t, map[string]any{"kind": test.kind, "work_id": workID, "claim_id": "claim", "completion_id": "completion"})
			receipt := state.Requests["cancel-mirror"]
			receipt.Operations = []Operation{operation}
			state.Requests["terminal"] = receipt
			state.TerminalBarriers[workID] = operation
			targets, _, err := state.issueTransitionSelection("terminal")
			if err != nil || len(targets) != 1 || targets[0].Status != test.want {
				t.Fatalf("terminal status diverged from runtime: %+v %v", targets, err)
			}
			state.TerminalBarriers[workID] = mustOp(t, map[string]any{"kind": test.kind, "work_id": workID, "claim_id": "other", "completion_id": "completion"})
			if targets, _, err := state.issueTransitionSelection("terminal"); err != nil || len(targets) != 0 {
				t.Fatal("stale terminal barrier acquired mirror authority")
			}
		})
	}
}

func TestTerminalDeliveryIssueTargetsSurviveRealReplayAndCheckpoint(t *testing.T) {
	for _, kind := range []string{"Result", "DeliveryFailure"} {
		t.Run(kind, func(t *testing.T) {
			commits, workID := terminalIssueFixture(t)
			commits = commits[:len(commits)-1]
			commits, decision := testGrant(t, commits, "terminal-grant", 1, 1)
			assignment := decision.Assignments[0]
			sender := testActor("dispatcher")
			sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
			commits = testOperations(t, commits, sender, "terminal-start", "dispatch", mustOp(t, map[string]any{
				"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender, "credential_principal": testPrincipal,
			}))
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			profile := state.Policy.Pools["default"].Profiles["default"]
			run := RunBinding{RunID: "202", RunAttempt: 1, Repository: testRepository, Workflow: profile.Workflow,
				Ref: profile.Ref, Principal: testPrincipal, Event: "workflow_dispatch"}
			evidence := terminalFor(state, assignment)
			evidence.Principal = testPrincipal
			evidence.Kind, evidence.Status, evidence.Conclusion = "reconciliation", "", ""
			commits = testOperations(t, commits, testActor("reconciler"), "terminal-bind", "dispatch", mustOp(t, map[string]any{
				"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": run, "evidence": evidence,
			}))
			commits = finishMember(t, commits, assignment, 0, "completed")
			state, err = Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			evidence = terminalFor(state, assignment)
			evidence.Principal = testPrincipal
			operation := map[string]any{"kind": kind, "work_id": workID, "claim_id": assignment.Claims[0].ClaimID,
				"completion_id": state.Works[workID].CompletionID}
			requestKind, want := "result", "Needs review"
			if kind == "Result" {
				evidence.Kind, evidence.Source, evidence.Receipt = "delivery", "verified_receipts", "real-delivery-receipt"
				operation["descriptor"] = map[string]any{"outputs": []any{map[string]any{"resource": map[string]any{"kind": "pull_request", "number": 123}}}}
			} else {
				requestKind, want = "delivery_failure", "Needs attention"
				evidence.Attempts, evidence.Effects = 5, "unknown"
				operation["reason"], operation["disposition"] = "receipts_unknown", "unknown"
			}
			operation["evidence"] = evidence
			commits = testOperations(t, commits, testActor("reconciler"), "terminal-delivery", requestKind, mustOp(t, operation))
			checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 5000)
			if err != nil {
				t.Fatal(err)
			}
			var expected []issueTransitionTarget
			for _, log := range [][]QueueCommit{commits, checkpoint} {
				state, err := Replay(log)
				if err != nil {
					t.Fatal(err)
				}
				targets, pending, err := state.issueTransitionSelection("terminal-delivery")
				if err != nil || len(pending) != 0 || len(targets) != 1 || targets[0].Status != want {
					t.Fatalf("real terminal receipt lost authority: %+v %+v %v", targets, pending, err)
				}
				if expected != nil && !reflect.DeepEqual(targets, expected) {
					t.Fatal("compaction changed terminal targets")
				}
				expected = targets
				for _, field := range []string{"work_id", "claim_id", "completion_id"} {
					forged := map[string]any{}
					if err := json.Unmarshal(state.TerminalBarriers[workID], &forged); err != nil {
						t.Fatal(err)
					}
					forged[field] = "foreign"
					original := state.TerminalBarriers[workID]
					state.TerminalBarriers[workID] = mustOp(t, forged)
					if targets, _, err := state.issueTransitionSelection("terminal-delivery"); err != nil || len(targets) != 0 {
						t.Fatalf("changed %s acquired terminal authority: %v %v", field, targets, err)
					}
					state.TerminalBarriers[workID] = original
				}
			}
		})
	}
}

func TestTerminalIssueSelectionKeepsEligibleMirrorsAlongsidePendingReceipts(t *testing.T) {
	commits, workID := terminalIssueFixture(t)
	commits = commits[:len(commits)-1]
	producer := testActor("producer")
	producer.Workflow, producer.RunID, producer.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	missingIssue, missingSummary := testNode(t, commits, "missing-issue"), testNode(t, commits, "missing-summary")
	request, err := NewRequest("additional-admission", "submit", producer, SubmitParameters{Nodes: []WorkDefinition{missingIssue, missingSummary}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, producer, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	projector := producer
	projector.Role = "projector"
	commits = testOperations(t, commits, projector, "partial-mirror", "issue_link",
		mustOp(t, IssueLinkOperation{Kind: "IssueLink", WorkID: missingSummary.WorkID, Resource: Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "998", Number: "98"}, ProjectorRef: strings.Repeat("0", 40)}))
	operations := []Operation{}
	for _, id := range []string{missingIssue.WorkID, workID, missingSummary.WorkID} {
		operations = append(operations, mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": id, "reason": "operator_cancelled"}))
	}
	commits = testOperations(t, commits, testActor("administrator"), "mixed-cancellation", "cancel_work", operations...)
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range [][]QueueCommit{commits, checkpoint} {
		state, err := Replay(log)
		if err != nil {
			t.Fatal(err)
		}
		targets, pending, err := state.issueTransitionSelection("mixed-cancellation")
		if err != nil || len(targets) != 1 || targets[0].WorkID != workID ||
			!reflect.DeepEqual(pending, []IssueSynchronizationPending{{missingIssue.WorkID, "projection_receipt_pending"}, {missingSummary.WorkID, "projection_receipt_pending"}}) {
			t.Fatalf("missing receipts blocked eligible mirror: %+v %+v %v", targets, pending, err)
		}
	}
}

func TestOperatorTerminalIssueSynchronizationDoesNotStealRetainedLocks(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, workID := terminalIssueFixture(t)
	installMockLog(t, server, commits)
	name := projectionLockName("work:" + workID)
	server.projectionRefs = map[string]string{name: "existing-owner"}
	calls := 0
	_, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"},
		func(context.Context, string, map[string]any, any) error {
			calls++
			return nil
		}, nil)
	if err == nil || calls != 0 || len(server.projectionRefs) != 1 || server.projectionRefs[name] != "existing-owner" {
		t.Fatalf("retained coordination was bypassed: %v %v", server.projectionRefs, err)
	}
}

func TestOperatorTerminalIssueSynchronizationProvisionsOnlyCheckedLabels(t *testing.T) {
	branch, server := newQueueAPI(t)
	commits, _ := terminalIssueFixture(t)
	installMockLog(t, server, commits)
	base := syncGraph(t, nil, func(result map[string]any) {
		if repository, ok := result["repository"].(map[string]any); ok {
			repository["tracking"], repository["status"] = nil, nil
		}
	})
	created := 0
	graph := func(ctx context.Context, query string, variables map[string]any, response any) error {
		if !strings.Contains(query, "createLabel") {
			return base(ctx, query, variables, response)
		}
		created++
		id := "cancelled"
		if variables["name"] == "work" {
			id = "tracking"
		}
		data, err := json.Marshal(map[string]any{"createLabel": map[string]any{"label": map[string]any{"id": id, "name": variables["name"]}}})
		if err != nil {
			return err
		}
		return json.Unmarshal(data, response)
	}
	result, err := branch.synchronizeCheckedIssueTransition(context.Background(), "cancel-mirror", &IssuesSettings{Label: "work"}, graph, nil)
	if err != nil || result.Synchronized != 1 || created != 2 || len(server.projectionRefs) != 0 {
		t.Fatalf("checked label provisioning failed: %+v %v", result, err)
	}
}

func TestOperatorTerminalIssueSynchronizationAllowsMaintainAndSkipsDisabledProjection(t *testing.T) {
	branch, server := newQueueAPI(t)
	server.noAdmin, server.maintain = true, true
	result, err := branch.SynchronizeIssueTransition(context.Background(), "exact-request")
	if err != nil || !result.Disabled || server.ledgerCalls != 0 {
		t.Fatalf("maintain-only disabled projection read or wrote the ledger: %+v %v", result, err)
	}
}

func TestIssueStatusLabelWhitespaceMatchesJavaScript(t *testing.T) {
	for _, test := range []struct{ label, want string }{
		{"work", "work"},
		{"  work  ", "-work-"},
		{"work\u00a0\u2000queue", "work-queue"},
		{"\ufeffwork\ufeff", "-work-"},
		{"\u0085work", "\u0085work"},
	} {
		if got := issueLabelWhitespace.ReplaceAllString(test.label, "-"); got != test.want {
			t.Fatalf("status label namespace diverges from JavaScript: %q != %q", got, test.want)
		}
	}
}
