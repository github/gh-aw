package workqueue

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
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
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "9007199254740993", Number: "3"}
	commits := testGenesis(t, func(policy *Policy) {
		policy.Projectors = []ProjectorRule{{Principal: testPrincipal, Workflow: defaultWorkerWorkflow, Ref: strings.Repeat("0", 40), Pools: []string{"default"}, Repositories: []string{testRepository}, BackingIssues: []Resource{resource}}}
	})
	first, second := testNode(t, commits, "first"), testNode(t, commits, "second")
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

func TestBackingIssueRequiresExactInstalledTarget(t *testing.T) {
	for _, approved := range []bool{false, true} {
		commits := issueProtocolFixture(t, "--fixture")
		// The projector-created links in this fixture do not authorize a new
		// caller-selected pre-existing Issue, even in the same repository.
		resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "999", Number: "99"}
		if approved {
			commits = testGenesis(t, func(policy *Policy) {
				policy.Projectors = []ProjectorRule{{Principal: testPrincipal, Workflow: defaultWorkerWorkflow, Ref: strings.Repeat("0", 40), Pools: []string{"default"}, Repositories: []string{testRepository}, BackingIssues: []Resource{resource}}}
			})
		}
		node := testNode(t, commits, "human-issue")
		node.BackingIssue = &resource
		request, err := NewRequest("human-issue", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, err = BuildCandidate(commits, testActor("producer"), request, 5000)
		if (err == nil) != approved {
			t.Fatalf("exact target approval=%v: %v", approved, err)
		}
	}
}

func assertProjectorPolicyNativeParity(t *testing.T, policy Policy, valid bool) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for projector policy parity")
	}
	script := `const {validatePolicy}=require("../../actions/setup/js/work_queue_policy.cjs"); let text=""; process.stdin.on("data",chunk=>text+=chunk); process.stdin.on("end",()=>{try{validatePolicy(JSON.parse(text));process.stdout.write("accepted")}catch(error){if(error.code!=="policy_invalid")throw error;process.stdout.write("rejected")}});`
	command := exec.Command(node, "-e", script)
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdin = bytes.NewReader(data)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if (ValidatePolicy(policy) == nil) != valid || (string(output) == "accepted") != valid {
		t.Fatalf("projector policy differs across Go and JavaScript; expected valid=%v: %s", valid, data)
	}
}

func TestProjectorPolicyValidationNativeParity(t *testing.T) {
	for _, repository := range []string{"owner/repo", "owner-name_1/repo.name-2", "owner:bad/repo", "owner/repo:bad", "owner/repo/path", "owner/rep\u00f3", "owner/repo name"} {
		t.Run(repository, func(t *testing.T) {
			policy := DefaultPolicy("1001", "owner/repo")
			policy.Projectors = []ProjectorRule{{Principal: "1001", Workflow: defaultWorkerWorkflow, Ref: strings.Repeat("0", 40), Pools: []string{"default"}, Repositories: []string{repository}}}
			assertProjectorPolicyNativeParity(t, policy, repoPattern.MatchString(repository))
		})
	}
}

func TestProjectorIssueGrantPolicyNativeParity(t *testing.T) {
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "9007199254740993", Number: "3"}
	for _, completion := range []string{"", "keep-open", "close-on-result", "close"} {
		for _, field := range []string{"valid", "kind", "host", "repository", "resource_id"} {
			t.Run(completion+"/"+field, func(t *testing.T) {
				target := resource
				switch field {
				case "kind":
					target.Kind = "pull_request"
				case "host":
					target.Host = "other.example"
				case "repository":
					target.Repository = "owner/other"
				case "resource_id":
					target.ResourceID = "0"
				}
				policy := DefaultPolicy(testPrincipal, testRepository)
				policy.Projectors = []ProjectorRule{{Principal: testPrincipal, Workflow: defaultWorkerWorkflow, Ref: strings.Repeat("0", 40), Pools: []string{"default"}, Repositories: []string{testRepository}, BackingIssues: []Resource{target}, CompletionPolicy: completion}}
				assertProjectorPolicyNativeParity(t, policy, field == "valid" && completion != "close")
			})
		}
	}
}

func TestProjectorPolicyRejectsExplicitEmptyRules(t *testing.T) {
	policy := DefaultPolicy("1001", "owner/repo")
	if err := ValidatePolicy(policy); err != nil {
		t.Fatalf("omitted projector rules must remain compatible: %v", err)
	}
	policy.Projectors = []ProjectorRule{}
	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("explicit empty projector rules accepted")
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["projectors"] = []any{}
	schema, err := policySchema()
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err == nil {
		t.Fatal("generated TypeSpec schema accepted explicit empty projector rules")
	}
}

func TestIssueClaimCommentSummaryCollision(t *testing.T) {
	commits := issueProtocolFixture(t, "--worker-fixture")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range state.Claims {
		for _, work := range state.Works {
			if work.WorkID == claim.WorkID {
				continue
			}
			op := IssueCommentOperation{Kind: "IssueComment", WorkID: claim.WorkID, ClaimID: claim.ClaimID, CommentID: work.IssueSummary, ProjectorRef: state.Dispatches[claim.DispatchID].Profile.Ref}
			actor := commits[len(commits)-1].Actor
			request, err := NewRequest("summary-collision", "issue_link", actor, OperationsParameters{Operations: []Operation{mustOp(t, op)}})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := BuildCandidate(commits, actor, request, 5000); err == nil || !strings.Contains(err.Error(), "summary") {
				t.Fatalf("Claim comment colliding with a foreign summary was not rejected: %v", err)
			}
		}
	}
}
