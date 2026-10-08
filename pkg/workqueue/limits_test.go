package workqueue

import (
	"fmt"
	"strings"
	"testing"
)

func TestResourceBudgetsFailBeforePublication(t *testing.T) {
	commits := testGenesis(t, nil)
	state, _ := Replay(commits)
	node := testNode(t, commits, "oversized")
	node.Payload = mustOp(t, map[string]string{"task": strings.Repeat("x", 16<<10)})
	request, _ := NewRequest("oversized", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000); err == nil ||
		!strings.Contains(err.Error(), "payload_limit") {
		t.Fatalf("payload budget not enforced: %v", err)
	}
	if _, err := PlanDispatch(state, DispatchParameters{Pool: "default", MaxClaims: 257, MaxDispatches: 1, MaxBytes: 48 << 10}, "r", "q", 2000); err == nil {
		t.Fatal("unbounded assignment budget accepted")
	}
	if _, err := PlanNext(Projection{}, "default", 2000); err == nil {
		t.Fatal("missing policy was treated as an empty queue")
	}
	if _, err := PlanDispatch(Projection{}, DispatchParameters{}, "r", "q", 2000); err == nil {
		t.Fatal("missing policy panicked or returned a grant")
	}
	policy := DefaultPolicy(testPrincipal, testRepository)
	policy.Limits.Operations = 1
	if err := validatePolicy(policy); err == nil {
		t.Fatal("policy cannot reserve less operation headroom than required for closure")
	}
	policy = DefaultPolicy(testPrincipal, testRepository)
	policy.AccountingWeights["\t"] = 1
	if err := validatePolicy(policy); err == nil {
		t.Fatal("unbounded/control-character account identity accepted")
	}
}

func TestLedgerAdmissionWatermarkPreservesRecovery(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		policy.Limits.LedgerBytes = 4000
	})
	node := testNode(t, commits, "a")
	request, _ := NewRequest("submit", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
	next, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	control := mustOp(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "limit"})
	req, _ := NewRequest("pause", "control", testActor("administrator"), OperationsParameters{Operations: []Operation{control}})
	next, _, _, err = BuildCandidate(next, testActor("administrator"), req, 3000)
	if err != nil {
		t.Fatal("closure/control cannot use its separate recovery capacity")
	}
	state, _ := Replay(next)
	other := testNode(t, next, "b")
	request, _ = NewRequest("other", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{other}})
	if _, _, _, err := BuildCandidate(next, testActor("producer"), request, 3000); err == nil {
		t.Fatal("paused/new admission consumed recovery capacity")
	}
	if !state.AdmissionPaused {
		t.Fatal("control did not persist after admission watermark")
	}
}

func TestAdmissionReservesWorstCaseFutureResultReferences(t *testing.T) {
	for _, count := range []int{7, 9, 10, 11} {
		t.Run(strings.Repeat("p", count), func(t *testing.T) {
			commits := testGenesis(t, nil)
			nodes := []WorkDefinition{}
			child := testNode(t, commits, "child")
			for index := range count {
				parent := testNode(t, commits, strings.Repeat("p", index+1))
				nodes = append(nodes, parent)
				child.DependsOn = append(child.DependsOn, Dependency{Kind: "work", WorkID: parent.WorkID})
			}
			// Reserve unknown bounded descriptors and valid maximally escaped IDs.
			nodes = append([]WorkDefinition{child}, nodes...)
			request, err := NewRequest("fan-in", "submit", testActor("producer"), SubmitParameters{Nodes: nodes})
			if err != nil {
				t.Fatal(err)
			}
			before, err := Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			next, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000)
			if count <= 10 {
				if err != nil {
					t.Fatalf("valid bounded future references were over-reserved: %v", err)
				}
				if _, err := Replay(next); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != "assignment_limit: Work and bounded declared Result inputs cannot fit a single-Claim assignment" {
				t.Fatalf("unpackable bounded future references were admitted: %v", err)
			}
			after, err := Serialize(commits)
			if err != nil || string(before) != string(after) {
				t.Fatal("admission changed original authoritative history")
			}
		})
	}
}

func TestProspectiveAssignmentIdentityValidityAndExactBudgets(t *testing.T) {
	const resultBytes = 1024
	escapedIdentity := `"` + strings.Repeat(`\\`, 256) + `"`
	if len(escapedIdentity)-2 != 512 {
		t.Fatal("maximum valid Identity must occupy 512 escaped content bytes")
	}
	descriptor := `{"x":"` + strings.Repeat("x", resultBytes-8) + `"}`
	if len(descriptor) != resultBytes {
		t.Fatal("independent Result descriptor does not fill its declared bound")
	}
	for _, count := range []int{7, 9, 10, 11} {
		t.Run(fmt.Sprintf("predecessors-%d", count), func(t *testing.T) {
			commits := testGenesis(t, func(policy *Policy) {
				policy.Limits.ResultBytes = resultBytes
			})
			child := testNode(t, commits, "child")
			nodes := []WorkDefinition{child}
			references := []string{}
			for index := range count {
				parent := testNode(t, commits, strings.Repeat("p", index+1))
				nodes = append(nodes, parent)
				child.DependsOn = append(child.DependsOn, Dependency{Kind: "work", WorkID: parent.WorkID})
				references = append(references, fmt.Sprintf(
					`{"descriptor":%s,"result_commit_id":%s,"work_id":%q}`,
					descriptor, escapedIdentity, parent.WorkID,
				))
			}
			nodes[0] = child
			// Construct the complete canonical wire independently of the estimator.
			wire := fmt.Sprintf(
				`{"claims":[{"claim_id":%q,"handle":"h16","result_refs":[%s],"work":%s,"work_id":%q}],"commit_id":%s,"dispatch_id":%q,"policy_epoch":%q,"pool":%q,"request_id":%s,"version":3,"worker_profile":%q}`,
				strings.Repeat("c", 70), strings.Join(references, ","), child.Payload, child.WorkID,
				escapedIdentity, strings.Repeat("d", 70), commits[0].PolicyEpoch,
				child.Pool, escapedIdentity, child.WorkerProfile,
			)
			canonical, err := Canonical([]byte(wire))
			if err != nil || string(canonical) != wire {
				t.Fatalf("independent prospective assignment is not canonical: %v", err)
			}
			assignment, err := ParseAssignment([]byte(wire))
			if err != nil {
				t.Fatalf("all prospective Identity placeholders must be legal: %v", err)
			}
			worstIdentity := strings.Repeat("\\", 256)
			if assignment.RequestID != worstIdentity || assignment.CommitID != worstIdentity {
				t.Fatal("complete prospective wire did not retain both maximum envelope identities")
			}
			for _, reference := range assignment.Claims[0].ResultRefs {
				if reference.ResultCommitID != worstIdentity {
					t.Fatal("prospective Result identity is not the same valid maximum")
				}
			}
			for _, field := range []string{"request_id", "commit_id", "result_commit_id"} {
				t.Run("forbidden-control-"+field, func(t *testing.T) {
					invalid, err := ParseAssignment([]byte(wire))
					if err != nil {
						t.Fatal(err)
					}
					controlIdentity := strings.Repeat("\x01", 256)
					switch field {
					case "request_id":
						invalid.RequestID = controlIdentity
					case "commit_id":
						invalid.CommitID = controlIdentity
					case "result_commit_id":
						invalid.Claims[0].ResultRefs[0].ResultCommitID = controlIdentity
					}
					data, err := canonicalValue(invalid)
					if err != nil {
						t.Fatal(err)
					}
					if err := validateAssignmentJSON("WorkQueueAssignment", data); err == nil ||
						!strings.HasPrefix(err.Error(), "identity_invalid:") {
						t.Fatalf("old forbidden prospective %s must not satisfy Identity: %v", field, err)
					}
				})
			}
			request, err := NewRequest("fan-in-exact", "submit", testActor("producer"), SubmitParameters{Nodes: nodes})
			if err != nil {
				t.Fatal(err)
			}
			for _, delta := range []int{-1, 0, 1} {
				t.Run(fmt.Sprintf("budget-delta-%d", delta), func(t *testing.T) {
					bounded := testGenesis(t, func(policy *Policy) {
						policy.Limits.ResultBytes = resultBytes
						policy.Limits.AssignmentBytes = int64(len(wire) + delta)
					})
					before, err := Serialize(bounded)
					if err != nil {
						t.Fatal(err)
					}
					next, _, _, err := BuildCandidate(bounded, testActor("producer"), request, 2000)
					if delta < 0 {
						if err == nil || !strings.HasPrefix(err.Error(), "assignment_limit:") {
							t.Fatalf("independently bounded assignment was admitted one byte too small: %v", err)
						}
					} else {
						if err != nil {
							t.Fatalf("complete future assignment must fit at its exact byte boundary: %v", err)
						}
						if _, err := Replay(next); err != nil {
							t.Fatal(err)
						}
					}
					after, err := Serialize(bounded)
					if err != nil || string(after) != string(before) {
						t.Fatal("prospective admission changed original authoritative history")
					}
				})
			}
		})
	}
}
