package workqueue

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

func issueProtocolFixture(t *testing.T, option string) []QueueCommit {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for native protocol parity")
	}
	input, err := exec.Command(node, "../../actions/setup/js/work_queue_issues_checks.cjs", option).Output()
	if err != nil {
		t.Fatal(err)
	}
	var commits []QueueCommit
	if err := json.Unmarshal(input, &commits); err != nil {
		t.Fatal(err)
	}
	return commits
}

func assertIssueNativeParity(t *testing.T, commits []QueueCommit) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for native protocol parity")
	}
	for prefix := 1; prefix <= len(commits); prefix++ {
		state, err := Replay(commits[:prefix])
		if err != nil {
			t.Fatalf("prefix %d: %v", prefix, err)
		}
		script := `const q=require("../../actions/setup/js/work_queue_replay.cjs"); let text=""; process.stdin.on("data",chunk=>text+=chunk); process.stdin.on("end",()=>process.stdout.write(JSON.stringify(q.serializeProjection(q.replayTransactions(JSON.parse(text))))));`
		command := exec.Command(node, "-e", script)
		data, _ := json.Marshal(commits[:prefix])
		command.Stdin = bytes.NewReader(data)
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		expected, err := canonicalValue(state)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := Canonical(output)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("prefix %d: native Issue protocol projection differs:\nGo %s\nJS %s\n%v", prefix, expected, actual, err)
		}
	}
}

func TestIssueBindingNativeParity(t *testing.T) {
	commits := issueProtocolFixture(t, "--fixture")
	assertIssueNativeParity(t, commits)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	var first, second *WorkState
	for _, work := range state.Works {
		if first == nil {
			first = work
		} else {
			second = work
		}
	}
	if first.IssueLink == nil || first.IssueSummary == "" || second.IssueLink == nil || second.IssueSummary == "" {
		t.Fatal("checked Issue links and canonical handles were lost")
	}
	actor := commits[len(commits)-1].Actor
	op := IssueLinkOperation{Kind: "IssueLink", WorkID: second.WorkID, Resource: *first.IssueLink, ProjectorRef: state.Policy.Projectors[0].Ref}
	request, err := NewRequest("rebind", "issue_link", actor, OperationsParameters{Operations: []Operation{mustOp(t, op)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, 5000); err == nil {
		t.Fatal("concurrent rebinding accepted")
	}
	actor.RunAttempt++
	request, err = NewRequest("foreign-attempt", "issue_link", actor, OperationsParameters{Operations: []Operation{mustOp(t, op)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, 5000); err == nil {
		t.Fatal("rerun inherited another attempt's admission authority")
	}
}

func TestIssueWorkerClaimsNativeParity(t *testing.T) {
	commits := issueProtocolFixture(t, "--worker-fixture")
	assertIssueNativeParity(t, commits)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	var operations []Operation
	for _, claim := range state.Claims {
		if claim.IssueComment == "" || state.Works[claim.WorkID].IssueSummary == "" {
			t.Fatal("mixed original Claims lost their historical comment handles")
		}
		operations = append(operations, mustOp(t, IssueCommentOperation{
			Kind: "IssueComment", WorkID: claim.WorkID, ClaimID: claim.ClaimID,
			CommentID: claim.IssueComment, ProjectorRef: state.Dispatches[claim.DispatchID].Profile.Ref,
		}))
	}
	actor := commits[len(commits)-1].Actor
	actor.RunAttempt++
	request, err := NewRequest("foreign-worker-attempt", "issue_link", actor, OperationsParameters{Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, 5000); err == nil {
		t.Fatal("rerun inherited original mixed-Claim projection authority")
	}
}

func TestBackingIssueAdmissionUniqueness(t *testing.T) {
	commits := testGenesis(t, nil)
	first, second := testNode(t, commits, "first"), testNode(t, commits, "second")
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "9007199254740993", Number: "3"}
	first.BackingIssue, second.BackingIssue = &resource, &resource
	commits = testSubmit(t, commits, "first", first)
	request, err := NewRequest("second", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{second}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000); err == nil {
		t.Fatal("one Issue was admitted for multiple Work nodes")
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if state.Works[first.WorkID].BackingIssue.ResourceID != "9007199254740993" || state.Works[first.WorkID].Subject != nil {
		t.Fatal("backing Issue identity was lossy or conflated with subject")
	}
}
