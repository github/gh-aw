package workqueue

import (
	"strings"
	"testing"
)

func TestAWIssueProducerAdmissionProjectionAndCheckpointParity(t *testing.T) {
	commits := testGenesis(t, awTestPolicy)
	producer := testActor("producer")
	producer.Workflow, producer.RunID, producer.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	work := testNode(t, commits, "issue")
	request, err := NewRequest("admit-issue", "submit", producer, SubmitParameters{Nodes: []WorkDefinition{work}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, producer, request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	projector := producer
	projector.Role = "projector"
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "999", Number: "99"}
	commits = testOperations(t, commits, projector, "aw-issue-link", "issue_link", mustOp(t, IssueLinkOperation{
		Kind: "IssueLink", WorkID: work.WorkID, Resource: resource, ProjectorRef: strings.Repeat("0", 40),
	}))
	commits = testOperations(t, commits, projector, "aw-issue-summary", "issue_link", mustOp(t, IssueCommentOperation{
		Kind: "IssueComment", WorkID: work.WorkID, CommentID: "aw-summary", ProjectorRef: strings.Repeat("0", 40),
	}))
	assertIssueNativeParity(t, commits)
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.projectionAuthority(projector, work.WorkID, strings.Repeat("0", 40), testRepository, ""); err != nil {
		t.Fatalf("checkpoint lost own admission projection authority: %v", err)
	}
	for _, update := range []func(*Actor){
		func(a *Actor) { a.Principal = "2002" },
		func(a *Actor) { a.Workflow = "ordinary.yml" },
		func(a *Actor) { a.RunID = "303" },
		func(a *Actor) { a.RunAttempt = 2 },
	} {
		impostor := projector
		update(&impostor)
		if err := state.projectionAuthority(impostor, work.WorkID, strings.Repeat("0", 40), testRepository, ""); err == nil {
			t.Fatal("AW Issue projection acquired another run's Work")
		}
		if err := state.projectionAuthority(projector, work.WorkID, "main", testRepository, ""); err == nil {
			t.Fatal("AW Issue projection trusted a mutable revision")
		}
	}
	foreign := work
	foreign.NodeKey, foreign.WorkID = "foreign", NodeID(work.GraphID, "foreign")
	foreign.BackingIssue = &resource
	if err := state.validateBackingIssueAdmission(foreign, state.Policy.Pools["default"]); err == nil {
		t.Fatal("automatic projection authorized a pre-existing backing Issue")
	}
}

func TestAWIssueOriginalClaimProjectionWithoutEnrollment(t *testing.T) {
	commits, assignment, worker := awBoundAssignment(t)
	projector := worker
	projector.Role = "projector"
	member := assignment.Claims[0]
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "999", Number: "99"}
	commits = testOperations(t, commits, projector, "claim-issue", "issue_link",
		mustOp(t, IssueLinkOperation{Kind: "IssueLink", WorkID: member.WorkID, ClaimID: member.ClaimID, Resource: resource, ProjectorRef: strings.Repeat("0", 40)}),
		mustOp(t, IssueCommentOperation{Kind: "IssueComment", WorkID: member.WorkID, AuthorityClaimID: member.ClaimID, CommentID: "claim-summary", ProjectorRef: strings.Repeat("0", 40)}),
		mustOp(t, IssueCommentOperation{Kind: "IssueComment", WorkID: member.WorkID, ClaimID: member.ClaimID, CommentID: "claim-comment", ProjectorRef: strings.Repeat("0", 40)}),
	)
	assertIssueNativeParity(t, commits)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Policy.Projectors) != 0 || state.Claims[member.ClaimID].IssueComment != "claim-comment" || state.Works[member.WorkID].IssueSummary != "claim-summary" {
		t.Fatal("AW Claim projection required enrollment or lost comment bindings")
	}
	for _, update := range []func(*Actor){
		func(a *Actor) { a.Principal = testPrincipal },
		func(a *Actor) { a.RunID = "303" },
		func(a *Actor) { a.RunAttempt = 2 },
		func(a *Actor) { a.DispatchID = "foreign" },
	} {
		impostor := projector
		update(&impostor)
		if err := state.projectionAuthority(impostor, member.WorkID, strings.Repeat("0", 40), testRepository, member.ClaimID); err == nil {
			t.Fatal("Issue projection trusted actor hints instead of actual original bound worker")
		}
	}
}
