package workqueue

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestIndependentWorkerChildEntitlementFixtures(t *testing.T) {
	data, err := os.ReadFile("../../actions/setup/js/work_queue_worker_child_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name      string `json:"name"`
			Valid     bool   `json:"valid"`
			Canonical string `json:"canonical"`
			ErrorCode string `json:"error_code"`
			Expected  struct {
				WorkerPrincipal        string `json:"worker_principal"`
				ProducerPrincipal      string `json:"producer_principal"`
				Pool                   string `json:"pool"`
				Priority               int    `json:"priority"`
				FairnessKey            string `json:"fairness_key"`
				ChildWorkID            string `json:"child_work_id"`
				ParentWorkID           string `json:"parent_work_id"`
				AccountingWeight       int    `json:"accounting_weight"`
				BoundClaims            int    `json:"bound_claims"`
				CancelledSiblingWorkID string `json:"cancelled_sibling_work_id"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != Version || len(fixture.Cases) != 33 {
		t.Fatal("all independent worker-child fixtures must be consumed")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			assertRejection := func(err error) {
				t.Helper()
				if err == nil || test.ErrorCode == "" {
					t.Fatal("independent invalid child must have an explicit rejection code")
				}
				code, _, _ := strings.Cut(err.Error(), ":")
				if code != test.ErrorCode {
					t.Fatalf("independent rejection code=%s, got %v", test.ErrorCode, err)
				}
			}
			commits, err := Parse([]byte(test.Canonical))
			if err != nil {
				if test.Valid {
					t.Fatal(err)
				}
				assertRejection(err)
				return
			}
			committed := commits[len(commits)-1]
			t.Run("public-candidate-admission", func(t *testing.T) {
				prefix := commits[:len(commits)-1]
				next, candidate, _, err := BuildCandidate(prefix, committed.Actor, committed.Request, committed.At)
				if !test.Valid {
					if err == nil || candidate != nil {
						t.Fatal("public publisher accepted spoofed or escalated worker child")
					}
					assertRejection(err)
					return
				}
				expectedID := map[string]string{
					"completed-parent-inherited-entitlement":                     "q_fcbf77ec37d0fbede12448a2dbb8945122306588aef02103bd45fdebfae18780",
					"completed-parent-cancelled-sibling-unit-weight-entitlement": "q_c8adb7e344b60107ec2bd1ce87403ec2a5161a79c96fa43562a95e58e9dc1a46",
				}[test.Name]
				if err != nil || candidate == nil || expectedID == "" || candidate.ID != expectedID || len(next) != len(commits) ||
					!sameJSON(candidate.Actor, committed.Actor) || !sameJSON(candidate.Request, committed.Request) ||
					!sameJSON(candidate.Operations, committed.Operations) {
					t.Fatalf("public candidate differs from independent child intent: %+v %v", candidate, err)
				}
				state, err := Replay(next)
				if err != nil {
					t.Fatal(err)
				}
				child := state.Works[test.Expected.ChildWorkID]
				if child == nil || child.State != "available" || child.Pool != test.Expected.Pool ||
					child.Priority != test.Expected.Priority || child.FairnessKey != test.Expected.FairnessKey {
					t.Fatal("public candidate did not admit the literal inherited child")
				}
				if _, ok := state.Policy.Producers[committed.Actor.Principal]; ok {
					t.Fatal("public candidate granted worker root-producer entitlement")
				}
				t.Run("nonworker-producer-allowlist-remains-required", func(t *testing.T) {
					producer := Actor{Role: "producer", Principal: committed.Actor.Principal, Repository: committed.Actor.Repository}
					request, err := NewRequest("unentitled-root-"+test.Name, "submit", producer, committed.Request.Parameters)
					if err != nil {
						t.Fatal(err)
					}
					if _, candidate, _, err := BuildCandidate(prefix, producer, request, committed.At); err == nil ||
						candidate != nil || !strings.HasPrefix(err.Error(), "admission_unauthorized:") {
						t.Fatalf("nonworker acquired completed worker's producer entitlement: %v", err)
					}
				})
				_, recovered, decision, err := BuildCandidate(next, committed.Actor, committed.Request, committed.At+1)
				if err != nil || recovered == nil || recovered.ID != candidate.ID || decision.Reason != "already_committed" {
					t.Fatalf("accepted public child request did not recover exactly: %+v %v", decision, err)
				}
				if test.Valid {
					t.Run("terminal-parent-cannot-authorize-new-child", func(t *testing.T) {
						before, err := Replay(prefix)
						if err != nil {
							t.Fatal(err)
						}
						dispatch := before.Dispatches[committed.Actor.DispatchID]
						parent := before.Works[test.Expected.ParentWorkID]
						reconciler := Actor{Role: "reconciler", Principal: test.Expected.ProducerPrincipal, Repository: committed.Actor.Repository}
						for _, kind := range []string{"delivery_failure", "release"} {
							t.Run(kind, func(t *testing.T) {
								evidence := terminalFor(before, dispatch.Assignment)
								operations := []Operation{}
								if kind == "delivery_failure" {
									evidence.Attempts, evidence.Effects = 5, "unknown"
									operations = append(operations, Op(map[string]any{
										"kind": "DeliveryFailure", "work_id": parent.WorkID, "claim_id": parent.ClaimID,
										"completion_id": parent.CompletionID, "reason": "verification_exhausted",
										"disposition": "unknown", "evidence": evidence,
									}))
								} else {
									for _, member := range dispatch.Claims {
										if before.Claims[member.ClaimID].State == "open" {
											operations = append(operations, Op(map[string]any{
												"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
												"reason": "native_terminal", "retry_not_before": 34000,
											}))
										}
									}
									operations = append(operations, Op(map[string]any{
										"kind": "Release", "dispatch_id": dispatch.DispatchID, "evidence": evidence,
									}))
								}
								request, err := NewRequest("close-parent-"+kind, kind, reconciler, OperationsParameters{Operations: operations})
								if err != nil {
									t.Fatal(err)
								}
								closed, closure, _, err := BuildCandidate(prefix, reconciler, request, 6000)
								if err != nil || closure == nil {
									t.Fatalf("fixture parent did not reach its authentic normalized closure: %v", err)
								}
								if _, child, _, err := BuildCandidate(closed, committed.Actor, committed.Request, 7000); err == nil ||
									child != nil {
									t.Fatal("failed or released parent admitted a fresh worker child")
								}
								accepted, _, _, err := BuildCandidate(next, reconciler, request, 6000)
								if err != nil {
									t.Fatal(err)
								}
								_, acknowledgment, decision, err := BuildCandidate(accepted, committed.Actor, committed.Request, 7000)
								if err != nil || acknowledgment == nil || acknowledgment.ID != candidate.ID ||
									decision.Reason != "already_committed" {
									t.Fatalf("terminal scope blocked the previously accepted child acknowledgment: %v", err)
								}
							})
						}
					})
				}
			})
			state, err := Replay(commits)
			if !test.Valid {
				assertRejection(err)
				if test.Name == "open-parent-cannot-admit" {
					prefix, err := Replay(commits[:len(commits)-1])
					if err != nil {
						t.Fatal(err)
					}
					if _, err := NewChildWork(prefix, commits[len(commits)-1].Actor, []byte(`{}`), "child", 5000); err == nil {
						t.Fatal("normalization granted open-Claim worker authority")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := test.Expected
			child, parent := state.Works[expected.ChildWorkID], state.Works[expected.ParentWorkID]
			if child == nil || parent == nil {
				t.Fatal("independent parent or child is absent")
			}
			if committed.Actor.Role != "worker" || committed.Actor.Principal != expected.WorkerPrincipal ||
				commits[parent.Position.Commit].Actor.Principal != expected.ProducerPrincipal {
				t.Fatal("actual worker identity or original producer provenance changed")
			}
			if _, ok := state.Policy.Producers[expected.WorkerPrincipal]; ok {
				t.Fatal("worker received root producer authority")
			}
			if child.Pool != expected.Pool || child.Priority != expected.Priority || child.FairnessKey != expected.FairnessKey {
				t.Fatal("child escaped its frozen parent's entitlement")
			}
			if expected.CancelledSiblingWorkID != "" {
				dispatch := state.Dispatches[committed.Actor.DispatchID]
				sibling := state.Works[expected.CancelledSiblingWorkID]
				if state.Policy.AccountingWeights[expected.FairnessKey] != expected.AccountingWeight ||
					dispatch == nil || len(dispatch.Claims) != expected.BoundClaims || expected.BoundClaims != 2 ||
					len(state.Works) != 3 || parent.State != "completed" || parent.Barrier != "pending" ||
					sibling == nil || sibling.State != "available" || sibling.RetryNotBefore != 34400 ||
					state.Claims[dispatch.Claims[1].ClaimID].State != "cancelled" {
					t.Fatal("exact unit-weight two-Claim fanout lost independent completed-pending or cancelled scope")
				}
			}
			normalized, err := NewChildWork(state, committed.Actor, child.Payload, child.NodeKey, child.Enqueued)
			if err != nil {
				t.Fatal(err)
			}
			normalized.DependsOn = child.DependsOn
			if !sameJSON(normalized, child.WorkDefinition) {
				t.Fatal("normalizer differs from the independent closed Work definition")
			}
			serialized, err := Serialize(commits)
			if err != nil || string(serialized) != test.Canonical {
				t.Fatalf("independent canonical ledger changed: %v", err)
			}
			t.Run("originating-rerun-preserves-original-worker-scope", func(t *testing.T) {
				rerun, err := Parse([]byte(test.Canonical))
				if err != nil {
					t.Fatal(err)
				}
				changed := false
				for index := range rerun {
					commit := &rerun[index]
					if commit.Actor.Role != "dispatcher" {
						continue
					}
					actor := commit.Actor
					actor.RunAttempt = 2
					var parameters OperationsParameters
					if err := json.Unmarshal(commit.Request.Parameters, &parameters); err != nil {
						t.Fatal(err)
					}
					if len(parameters.Operations) != 1 {
						t.Fatal("independent fixture must contain one native sender marker")
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(parameters.Operations[0], &fields); err != nil {
						t.Fatal(err)
					}
					fields["sender"], err = json.Marshal(actor)
					if err != nil {
						t.Fatal(err)
					}
					parameters.Operations[0], err = json.Marshal(fields)
					if err != nil {
						t.Fatal(err)
					}
					request, err := NewRequest(commit.Request.ID, commit.Request.Kind, actor, parameters)
					if err != nil {
						t.Fatal(err)
					}
					commit.Actor, commit.Request, commit.Operations = actor, request, parameters.Operations
					changed = true
				}
				if !changed {
					t.Fatal("fixture has no originating dispatcher marker")
				}
				continued, err := Replay(rerun)
				if err != nil {
					t.Fatal(err)
				}
				if rerun[len(rerun)-1].Actor.Principal != expected.WorkerPrincipal ||
					rerun[len(rerun)-1].Actor.RunAttempt != 1 ||
					continued.Works[expected.ChildWorkID].Priority != expected.Priority ||
					continued.Works[expected.ChildWorkID].FairnessKey != expected.FairnessKey ||
					!sameJSON(continued.Works[expected.ChildWorkID], child) {
					t.Fatal("originating rerun changed worker identity or inherited child entitlement")
				}
			})
		})
	}
}
