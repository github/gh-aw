package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func nativeHostFixture(t *testing.T, contract string) (Branch, *queueAPI, Projection, Assignment) {
	t.Helper()
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Payload = json.RawMessage(`{"effect_contract":` + contract + `,"resource_scope":{"version":1,"resources":[{"kind":"issue","host":"github.com","repository":"owner/repo","repository_id":"1","resource_id":"2","number":"7"}]}}`)
	})
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return branch, mock, state, assignment
}

func nativeHostOutput(kind string) NativeDeliveryOutput {
	return NativeDeliveryOutput{
		Type: kind, Intent: json.RawMessage(`{"title":"delivered"}`),
		Target: EffectResource{
			"kind": "issue", "host": "github.com", "repository": testRepository,
			"repository_id": "1", "resource_id": "2", "number": "7",
		},
	}
}

func TestNativeDeliveryHostClosedProcessAndExactAPIProof(t *testing.T) {
	branch, mock, state, assignment := nativeHostFixture(t, `{"kind":"none"}`)
	member := assignment.Claims[0]
	closed := false
	calls := 0
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(_ context.Context, scope NativeDeliveryScope) (NativeDeliveryInventory, error) {
			calls++
			if scope.Member.ClaimID != member.ClaimID || scope.Run.RunID != "202" ||
				scope.Run.RunAttempt != 1 || scope.CompletionID == "" {
				t.Fatal("process callback lost original completion/member/attempt binding")
			}
			return NativeDeliveryInventory{Closed: closed}, nil
		},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	proof, err := branch.DeliveryVerifier(context.Background(), state, *state.Claims[member.ClaimID])
	if err != nil || proof.Verified || proof.Disposition != "unknown" {
		t.Fatalf("open process channel settled no-write contract: %+v %v", proof, err)
	}
	closed = true
	recovery, err := branch.RecoverDelivery(context.Background(), member.WorkID, "native-host-result")
	if err != nil || recovery.Reason != "delivery_verified" || calls != 3 {
		t.Fatalf("protected process + API proof did not settle concrete native delivery: %+v calls=%d %v", recovery, calls, err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := Replay(commits)
	if latest.Works[member.WorkID].Barrier != "verified" || latest.Stats.Dispatches != 1 || mock.nativeReads < 3 {
		t.Fatal("native host skipped API reads or confused Result with capacity release")
	}
}

func TestNativeDeliveryHostMissingCapabilityIsUnknown(t *testing.T) {
	branch, _, state, assignment := nativeHostFixture(t, `{"kind":"none"}`)
	for _, host := range []*NativeDeliveryHost{nil, NewNativeDeliveryHost(NativeDeliveryHostOptions{})} {
		proof, err := host.Verifier(branch)(context.Background(), state, *state.Claims[assignment.Claims[0].ClaimID])
		if err != nil || proof.Verified || proof.PositiveNoEffects || proof.Receipt != "" || proof.Disposition != "unknown" {
			t.Fatalf("serialized/absent proof created native authority: %+v %v", proof, err)
		}
	}
	configured := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true}, nil
		},
	})
	type hostEnvelope struct {
		Host *NativeDeliveryHost `json:"host"`
	}
	serialized, err := json.Marshal(hostEnvelope{Host: configured})
	if err != nil {
		t.Fatal(err)
	}
	var copied hostEnvelope
	if err := json.Unmarshal(serialized, &copied); err != nil {
		t.Fatal(err)
	}
	if copied.Host == nil || copied.Host == configured || copied.Host.options.Inventory != nil {
		t.Fatal("copyable envelope retained protected native host capabilities")
	}
	proof, err := copied.Host.Verifier(branch)(context.Background(), state, *state.Claims[assignment.Claims[0].ClaimID])
	if err != nil || proof.Verified || proof.PositiveNoEffects || proof.Receipt != "" || proof.Disposition != "unknown" {
		t.Fatalf("copyable native host serialization became a process capability: %+v %v", proof, err)
	}
}

func TestNativeDeliveryHostBuiltinAndDeclaredReadback(t *testing.T) {
	for _, kind := range []string{"update_issue", "custom_external"} {
		t.Run(kind, func(t *testing.T) {
			declaration := ""
			if kind != "update_issue" {
				declaration = `,"verification":{"verifier_id":"approved_readback","expected":{"title":"delivered"}}`
			}
			branch, mock, state, assignment := nativeHostFixture(t, `{"version":1,"outputs":[{"type":"`+kind+`","min":1,"max":1`+declaration+`}]}`)
			mock.issue = map[string]any{"id": 2, "number": 7, "title": "delivered"}
			order := []string{}
			output := nativeHostOutput(kind)
			verifier := func(ctx context.Context, scope NativeDeliveryScope, output NativeDeliveryOutput, expected json.RawMessage) (json.RawMessage, bool, error) {
				order = append(order, "readback")
				if scope.CredentialGeneration != state.CredentialGeneration ||
					output.Target["resource_id"] != "2" ||
					kind != "update_issue" && !sameJSON(expected, json.RawMessage(`{"title":"delivered"}`)) {
					t.Fatal("approved verifier lost frozen intent/resource/generation")
				}
				var issue struct {
					ID     json.Number `json:"id"`
					Number int         `json:"number"`
					Title  string      `json:"title"`
				}
				if err := branch.request(ctx, http.MethodGet, "issues/7", nil, &issue); err != nil {
					return nil, false, err
				}
				descriptor, _ := canonicalValue(map[string]string{"resource_id": issue.ID.String(), "title": issue.Title})
				return descriptor, issue.ID.String() == "2" && issue.Number == 7 && issue.Title == "delivered", nil
			}
			registrations := map[string]NativeOutputVerifier{kind: verifier}
			host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
				Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
					order = append(order, "process")
					return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{output}}, nil
				},
				Credentials: func(_ context.Context, scope NativeDeliveryScope, target EffectResource) error {
					order = append(order, "credentials")
					if target["repository"] != scope.Run.Repository {
						return errors.New("foreign credentials")
					}
					return nil
				},
				Builtin:  registrations,
				Declared: map[string]NativeOutputVerifier{"approved_readback": verifier},
			})
			// Later changes to caller-owned maps cannot replace the approved host.
			registrations[kind] = nil
			branch.DeliveryVerifier = host.Verifier(branch)
			recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "native-resource-result")
			if err != nil || recovery.Reason != "delivery_verified" || mock.resourceReads != 2 ||
				strings.Join(order, ",") != "process,credentials,readback,process,credentials,readback" {
				t.Fatalf("native protected readback path failed: %+v %v order=%v reads=%d", recovery, err, order, mock.resourceReads)
			}
		})
	}
}

func TestNativeDeliveryHostPersistentGitAPIReadback(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Payload = json.RawMessage(`{"effect_contract":{"version":1,"outputs":[{"type":"persistent_snapshot","min":1,"max":1,"verification":{"verifier_id":"git_blob","expected":{"path":"` + FileName + `"}}}]},"resource_scope":{"version":1,"resources":[{"host":"github.com","repository":"owner/repo","repository_id":"1","path":"` + FileName + `"}]}}`)
	})
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	tree := mock.commits[mock.head].Tree
	target := EffectResource{"host": "github.com", "repository": testRepository, "repository_id": "1", "path": FileName}
	reads := 0
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{{Type: "persistent_snapshot", Target: target}}}, nil
		},
		Credentials: func(_ context.Context, scope NativeDeliveryScope, resource EffectResource) error {
			if scope.CredentialGeneration == "" || !sameJSON(resource, target) {
				return errors.New("missing native persistence credential binding")
			}
			return nil
		},
		Declared: map[string]NativeOutputVerifier{
			"git_blob": func(ctx context.Context, _ NativeDeliveryScope, output NativeDeliveryOutput, intent json.RawMessage) (json.RawMessage, bool, error) {
				reads++
				var expected struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(intent, &expected) != nil || expected.Path != output.Target["path"] {
					return nil, false, nil
				}
				stored, data, err := branch.readLog(ctx, tree)
				if err != nil {
					return nil, false, err
				}
				descriptor, err := canonicalValue(map[string]string{"path": expected.Path, "blob_sha256": hashBytes(data)})
				return descriptor, len(stored) == len(commits), err
			},
		},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "persistent-readback")
	if err != nil || recovery.Reason != "delivery_verified" || reads != 2 {
		t.Fatalf("persistent output required actual protected Git API readback: %+v reads=%d %v", recovery, reads, err)
	}
}

func TestNativeDeliveryHostRejectsBeforeExternalSDKReads(t *testing.T) {
	for _, failure := range []string{"foreign", "resource", "credentials", "unsupported", "attempt", "cancelled", "not_closed"} {
		t.Run(failure, func(t *testing.T) {
			branch, mock, state, assignment := nativeHostFixture(t, `{"version":1,"outputs":[{"type":"custom","min":1,"max":1,"verification":{"verifier_id":"approved","expected":{}}}]}`)
			output := nativeHostOutput("custom")
			claim := state.Claims[assignment.Claims[0].ClaimID]
			if failure == "foreign" {
				output.Target["repository"] = "foreign/repo"
			}
			if failure == "resource" {
				output.Target["resource_id"] = "999"
			}
			if failure == "attempt" {
				mock.nativeRun.RunAttempt = 2
			}
			if failure == "cancelled" {
				claim.State = "cancelled"
			}
			reads := 0
			verify := func(context.Context, NativeDeliveryScope, NativeDeliveryOutput, json.RawMessage) (json.RawMessage, bool, error) {
				reads++
				return json.RawMessage(`{}`), true, nil
			}
			options := NativeDeliveryHostOptions{
				Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
					return NativeDeliveryInventory{Closed: failure != "not_closed", Outputs: []NativeDeliveryOutput{output}}, nil
				},
				Credentials: func(context.Context, NativeDeliveryScope, EffectResource) error {
					if failure == "credentials" {
						return errors.New("credential scope unavailable")
					}
					return nil
				},
				Declared: map[string]NativeOutputVerifier{"approved": verify},
			}
			if failure == "unsupported" {
				options.Declared = nil
			}
			proof, _ := NewNativeDeliveryHost(options).Verifier(branch)(context.Background(), state, *claim)
			if reads != 0 || proof.Verified || proof.Disposition != "unknown" || proof.PositiveNoEffects {
				t.Fatalf("unapproved native evidence reached SDK or settled delivery: %+v reads=%d", proof, reads)
			}
		})
	}
}

func TestNativeDeliveryHostProfileAloneCannotGrantResourceReadback(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Payload = json.RawMessage(`{"effect_contract":{"version":1,"outputs":[{"type":"update_issue","min":1,"max":1}]}}`)
	})
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	state, _ := Replay(commits)
	reads, credentialLookups := 0, 0
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{nativeHostOutput("update_issue")}}, nil
		},
		Credentials: func(context.Context, NativeDeliveryScope, EffectResource) error {
			credentialLookups++
			return nil
		},
		Builtin: map[string]NativeOutputVerifier{
			"update_issue": func(context.Context, NativeDeliveryScope, NativeDeliveryOutput, json.RawMessage) (json.RawMessage, bool, error) {
				reads++
				return json.RawMessage(`{}`), true, nil
			},
		},
	})
	proof, err := host.Verifier(branch)(context.Background(), state, *state.Claims[assignment.Claims[0].ClaimID])
	if err == nil || !strings.Contains(err.Error(), "claim_scope_invalid") ||
		proof.Verified || reads != 0 || credentialLookups != 0 {
		t.Fatalf("profile-only scope reached external credentials/SDK or settled effects: %+v %v", proof, err)
	}
}

func TestNativeDeliveryHostGenericTargetsRequireExactImmutableSelectors(t *testing.T) {
	for _, field := range []string{"run_id", "ref", "path"} {
		t.Run(field, func(t *testing.T) {
			branch, mock, state, assignment := nativeHostFixture(t, `{"version":1,"outputs":[{"type":"update_issue","min":1,"max":1}]}`)
			output := nativeHostOutput("update_issue")
			output.Target[field] = map[string]string{"run_id": "202", "ref": "heads/foreign", "path": "foreign.json"}[field]
			credentials, reads := 0, 0
			host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
				Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
					return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{output}}, nil
				},
				Credentials: func(context.Context, NativeDeliveryScope, EffectResource) error {
					credentials++
					return nil
				},
				Builtin: map[string]NativeOutputVerifier{
					"update_issue": func(context.Context, NativeDeliveryScope, NativeDeliveryOutput, json.RawMessage) (json.RawMessage, bool, error) {
						reads++
						return json.RawMessage(`{}`), true, nil
					},
				},
			})
			member := assignment.Claims[0]
			proof, err := host.Verifier(branch)(context.Background(), state, *state.Claims[member.ClaimID])
			if err == nil || !strings.Contains(err.Error(), "claim_scope_invalid") ||
				proof.Verified || credentials != 0 || reads != 0 || mock.resourceReads != 0 {
				t.Fatalf("generic target bypassed immutable selector before SDK credentials/readback: %+v %v", proof, err)
			}
		})
	}
}

func TestNativeDeliveryHostFullSubjectIntersectsPartialExactGenericSelector(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Subject = &Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
			RepositoryID: "1", ResourceID: "2", Number: "7"}
		work.Payload = json.RawMessage(`{"effect_contract":{"version":1,"outputs":[{"type":"update_issue","min":1,"max":1}]},"resource_scope":{"version":1,"resources":[{"repository":"owner/repo","path":"src/a.go"}]}}`)
	})
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	mock.issue = map[string]any{"id": 2, "number": 7, "title": "delivered"}
	output := nativeHostOutput("update_issue")
	output.Target["path"] = "src/a.go"
	credentials := 0
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{output}}, nil
		},
		Credentials: func(_ context.Context, _ NativeDeliveryScope, target EffectResource) error {
			credentials++
			if !sameJSON(target, output.Target) {
				t.Fatal("native credential callback lost exact Subject and generic target")
			}
			return nil
		},
		Builtin: map[string]NativeOutputVerifier{"update_issue": func(ctx context.Context, _ NativeDeliveryScope, _ NativeDeliveryOutput, _ json.RawMessage) (json.RawMessage, bool, error) {
			var issue struct {
				ID     json.Number `json:"id"`
				Number int         `json:"number"`
				Title  string      `json:"title"`
			}
			if err := branch.request(ctx, http.MethodGet, "issues/7", nil, &issue); err != nil {
				return nil, false, err
			}
			descriptor, err := canonicalValue(map[string]string{"resource_id": issue.ID.String(), "title": issue.Title})
			return descriptor, issue.ID.String() == "2" && issue.Number == 7 && issue.Title == "delivered", err
		}},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "subject-generic-readback")
	if err != nil || recovery.Reason != "delivery_verified" || credentials != 2 || mock.resourceReads != 2 {
		t.Fatalf("native host narrowed the authoritative Subject/selector intersection: %+v %v credentials=%d reads=%d",
			recovery, err, credentials, mock.resourceReads)
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(latest)
	if err != nil || state.Works[assignment.Claims[0].WorkID].Barrier != "verified" || state.Stats.Dispatches != 1 {
		t.Fatalf("verified generic-target Result released native capacity or lost delivery: %+v %v", state.Stats, err)
	}
}

func TestNativeDeliveryHostRequiresWorkerCreatedAncestorTargetBinding(t *testing.T) {
	branch, mock := newQueueAPI(t)
	contract := `{"effect_contract":{"version":1,"outputs":[{"type":"persistent_snapshot","min":1,"max":1,"verification":{"verifier_id":"approved","expected":{}}}]},"resource_scope":{"version":1,"resources":[{"host":"github.com","repository":"owner/repo","repository_id":"1","path":"%s"}]}}`
	commits, original := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Payload = json.RawMessage(fmt.Sprintf(contract, "original.json"))
	})
	commits = finishMember(t, commits, original, 0, "completed", time.Now().UnixMilli())
	child := testNode(t, commits, "broader-worker-child")
	child.Payload = json.RawMessage(fmt.Sprintf(contract, "foreign.json"))
	commits = testSubmitAsWorker(t, commits, workerActor(original, "h1"), "worker-child-target", child)
	commits, decision := testGrant(t, commits, "grant-worker-child", 1, 1)
	if len(decision.Assignments) != 1 {
		t.Fatal("worker-created child must receive an ordinary fair grant")
	}
	assignment := decision.Assignments[0]
	state, _ := Replay(commits)
	profile := state.Dispatches[assignment.DispatchID].Profile
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "start-worker-child", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender,
	}))
	binding := RunBinding{RunID: "303", RunAttempt: 1, Repository: testRepository, Workflow: profile.Workflow,
		Ref: profile.Ref, Principal: profile.Principal, Event: "workflow_dispatch"}
	evidence := Evidence{Kind: "reconciliation", Source: "github_api", Repository: testRepository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal, CheckedAt: 4000, RunID: "303", RunAttempt: 1}
	commits = testOperations(t, commits, testActor("reconciler"), "bind-worker-child", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": binding, "evidence": evidence,
	}))
	actor := workerActor(assignment, assignment.Claims[0].Handle)
	actor.RunID = "303"
	request, err := NewRequest("finish-worker-child", "finish", actor,
		FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: actor.ClaimHandle, Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, actor, request, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	commits = testSubmit(t, commits, "producer-retries-worker-child", child)
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	mock.nativeRun.ID = json.Number("303")
	state, _ = Replay(commits)
	credentials, reads := 0, 0
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{{
				Type: "persistent_snapshot", Target: EffectResource{
					"host": "github.com", "repository": testRepository, "repository_id": "1", "path": "foreign.json",
				},
			}}}, nil
		},
		Credentials: func(context.Context, NativeDeliveryScope, EffectResource) error {
			credentials++
			return nil
		},
		Declared: map[string]NativeOutputVerifier{"approved": func(context.Context, NativeDeliveryScope, NativeDeliveryOutput, json.RawMessage) (json.RawMessage, bool, error) {
			reads++
			return json.RawMessage(`{}`), true, nil
		}},
	})
	proof, err := host.Verifier(branch)(context.Background(), state, *state.Claims[assignment.Claims[0].ClaimID])
	if err == nil || !strings.Contains(err.Error(), "claim_scope_invalid") || proof.Verified || credentials != 0 || reads != 0 {
		t.Fatalf("producer retry erased worker child's immutable ancestor readback authority: %+v %v", proof, err)
	}
}

func TestNativeDeliveryHostCompleteControlCASFence(t *testing.T) {
	branch, mock, _, assignment := nativeHostFixture(t, `{"version":1,"outputs":[{"type":"work_queue_submit","min":1,"max":1}]}`)
	member := assignment.Claims[0]
	commits, _ := branch.Read(context.Background())
	actor := workerActor(assignment, member.Handle)
	child := testNode(t, commits, "first-native-child")
	params := SubmitParameters{Nodes: []WorkDefinition{child}}
	request, err := NewRequest("native-control", "submit", actor, params)
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, actor, request, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	installMockLog(t, mock, commits)
	intent := request.Parameters
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{{Type: "work_queue_submit", RequestID: request.ID, Intent: intent}}}, nil
		},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := branch.DeliveryVerifier(context.Background(), state, *state.Claims[member.ClaimID])
	if err != nil || !proof.Verified {
		t.Fatalf("committed exact control was not independently verified: %+v %v", proof, err)
	}
	// Simulate a control arriving after the initial verification but before the
	// fresh publication prefix: the complete contract cannot be overtaken.
	child = testNode(t, commits, "late-native-child")
	commits = testSubmitAsWorker(t, commits, actor, "late-native-control", child)
	installMockLog(t, mock, commits)
	state, _ = Replay(commits)
	actor = testActor("reconciler")
	result := mustOp(t, map[string]any{
		"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID,
		"completion_id": state.Works[member.WorkID].CompletionID, "descriptor": proof.Descriptor,
		"evidence": Evidence{
			Kind: "delivery", Source: "verified_receipts", Repository: testRepository,
			Workflow: state.Dispatches[assignment.DispatchID].Run.Workflow,
			Ref:      state.Dispatches[assignment.DispatchID].Run.Ref, Principal: testPrincipal,
			RunID: "202", RunAttempt: 1, CheckedAt: time.Now().UnixMilli(), Receipt: proof.Receipt,
		},
	})
	request, _ = NewRequest("stale-native-result", "result", actor, OperationsParameters{Operations: []Operation{result}})
	before := mock.head
	if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
		!strings.Contains(err.Error(), "delivery_evidence_required") {
		t.Fatalf("Result overtook an undeclared native control: %v", err)
	}
	if mock.refWrites != 0 || mock.head != before {
		t.Fatal("failed native complete-contract fence mutated the ledger")
	}
}

func TestNativeDeliveryHostRechecksActualConflictingCAS(t *testing.T) {
	branch, mock, _, assignment := nativeHostFixture(t, `{"kind":"none"}`)
	commits, _ := branch.Read(context.Background())
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true}, nil
		},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	var concurrent []QueueCommit
	mock.concurrent = func() {
		child := testNode(t, commits, "cas-racing-native-child")
		concurrent = testSubmitAsWorker(t, commits, workerActor(assignment, "h1"), "cas-racing-control", child)
		installMockLog(t, mock, concurrent)
	}
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "racing-result")
	if err == nil || !strings.Contains(err.Error(), "delivery_evidence_required") ||
		recovery.Reason != "delivery_unresolved" || recovery.Publication != nil {
		t.Fatalf("conflicting Result CAS falsely reported success: %+v %v", recovery, err)
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, _ := Replay(latest)
	if len(latest) != len(concurrent) || state.Works[assignment.Claims[0].WorkID].Barrier != "pending" ||
		state.Stats.Dispatches != 1 || mock.refWrites != 1 {
		t.Fatal("fresh CAS verification lost racing control or native reservation")
	}
}

func TestNativeDeliveryHostDispatchControlAndAcceptedACK(t *testing.T) {
	branch, mock, _, assignment := nativeHostFixture(t, `{"version":1,"outputs":[{"type":"work_queue_dispatch_next","min":1,"max":1}]}`)
	commits, _ := branch.Read(context.Background())
	node := testNode(t, commits, "next-native-work")
	commits = testSubmit(t, commits, "next-work", node)
	actor := workerActor(assignment, "h1")
	parameters := DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10}
	request, err := NewRequest("native-dispatch-control", "dispatch_next", actor, parameters)
	if err != nil {
		t.Fatal(err)
	}
	commits, accepted, decision, err := BuildCandidate(commits, actor, request, time.Now().UnixMilli())
	if err != nil || accepted == nil || len(decision.Assignments) != 1 {
		t.Fatalf("worker control did not create a concrete ordinary fair grant: %+v %v", decision, err)
	}
	installMockLog(t, mock, commits)
	host := NewNativeDeliveryHost(NativeDeliveryHostOptions{
		Inventory: func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error) {
			return NativeDeliveryInventory{Closed: true, Outputs: []NativeDeliveryOutput{{Type: "work_queue_dispatch_next", RequestID: request.ID, Intent: request.Parameters}}}, nil
		},
	})
	branch.DeliveryVerifier = host.Verifier(branch)
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "native-dispatch-result")
	if err != nil || recovery.Reason != "delivery_verified" {
		t.Fatalf("exact committed native dispatch control did not settle: %+v %v", recovery, err)
	}
	commits, _ = branch.Read(context.Background())
	if _, recovered, decision, err := BuildCandidate(commits, actor, request, time.Now().UnixMilli()); err != nil ||
		recovered.ID != accepted.ID || decision.Reason != "already_committed" {
		t.Fatalf("Result discarded old accepted-request ACK: %+v %v", decision, err)
	}
	fresh, _ := NewRequest("fresh-after-native-result", "dispatch_next", actor, parameters)
	if _, _, _, err := BuildCandidate(commits, actor, fresh, time.Now().UnixMilli()); err == nil {
		t.Fatal("verified Result allowed a fresh native worker control")
	}
}

func testSubmitAsWorker(t *testing.T, commits []QueueCommit, actor Actor, id string, node WorkDefinition) []QueueCommit {
	t.Helper()
	request, err := NewRequest(id, "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err := BuildCandidate(commits, actor, request, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return next
}
