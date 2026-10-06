package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDeliveryExhaustionDeadlineAndAttemptNativeParity(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/delivery-deadlines.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CompletionAt int64 `json:"completion_at"`
		MaxAttempts  int   `json:"max_attempts"`
		DeadlineMS   int64 `json:"deadline_ms"`
		Cases        []struct {
			ID          string `json:"id"`
			At          int64  `json:"at"`
			Attempts    int    `json:"attempts"`
			Effects     string `json:"effects"`
			Disposition string `json:"disposition"`
			Receipt     string `json:"receipt"`
			Expected    string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	commits, assignment := boundAssignment(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		pool.Reconciliation.MaxAttempts = fixture.MaxAttempts
		pool.Reconciliation.DeadlineMS = fixture.DeadlineMS
		policy.Pools["default"] = pool
	})
	commits = finishMember(t, commits, assignment, 0, "completed", fixture.CompletionAt)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	node, nodeErr := exec.LookPath("node")
	if nodeErr != nil {
		t.Log("JavaScript deadline parity tooling unavailable; native independent fixtures still run")
	}
	for _, test := range fixture.Cases {
		t.Run(test.ID, func(t *testing.T) {
			evidence := terminalFor(state, assignment)
			evidence.Attempts, evidence.Effects, evidence.Receipt = test.Attempts, test.Effects, test.Receipt
			actor := testActor("reconciler")
			request, err := NewRequest(test.ID, "delivery_failure", actor, OperationsParameters{
				Operations: []Operation{Op(map[string]any{
					"kind": "DeliveryFailure", "work_id": assignment.Claims[0].WorkID,
					"claim_id":      assignment.Claims[0].ClaimID,
					"completion_id": state.Works[assignment.Claims[0].WorkID].CompletionID,
					"reason":        "delivery_verification_exhausted", "disposition": test.Disposition,
					"evidence": evidence,
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			var parameters OperationsParameters
			if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
				t.Fatal(err)
			}
			commit := QueueCommit{
				Version: 3, ID: "failure-" + test.ID, Previous: &state.Tip,
				Request: request, Actor: actor, PolicyEpoch: state.PolicyEpoch,
				At: test.At, Operations: parameters.Operations,
			}
			candidate := append(slices.Clone(commits), commit)
			native, replayErr := Replay(candidate)
			code := "allowed"
			if replayErr != nil {
				code, _, _ = strings.Cut(replayErr.Error(), ":")
			}
			if code != test.Expected {
				t.Fatalf("native expected %s; got %s (%v)", test.Expected, code, replayErr)
			}
			if replayErr == nil {
				work := native.Works[assignment.Claims[0].WorkID]
				if work.State != "completed" || work.Barrier != "failed" ||
					work.Disposition != test.Disposition || native.Stats.Dispatches != 1 ||
					native.Claims[assignment.Claims[1].ClaimID].State != "open" {
					t.Fatal("deadline failure changed ownership, native reservation or sibling state")
				}
			}
			if nodeErr != nil {
				return
			}
			ledger, err := Serialize(candidate)
			if err != nil {
				t.Fatal(err)
			}
			input, err := json.Marshal(map[string]any{
				"action": "replay", "data": string(ledger), "pool": "default",
				"at": test.At, "include_canonical": true,
				"parameters": DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10},
				"request_id": "deadline-probe", "commit_id": "deadline-probe-commit",
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, node, "../../specs/work-queue/native_probe.cjs")
			command.Stdin = bytes.NewReader(append(input, '\n'))
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil {
				t.Fatalf("JavaScript deadline parity: %v\n%s", err, stderr.String())
			}
			var response struct {
				Error      string          `json:"error"`
				Projection json.RawMessage `json:"projection"`
				Canonical  string          `json:"canonical_ledger"`
			}
			if err := json.Unmarshal(output, &response); err != nil {
				t.Fatal(err)
			}
			jsCode := "allowed"
			if response.Error != "" {
				jsCode, _, _ = strings.Cut(response.Error, ":")
			}
			if jsCode != test.Expected {
				t.Fatalf("JavaScript expected %s; got %s (%s)", test.Expected, jsCode, response.Error)
			}
			if replayErr == nil {
				expected, err := canonicalValue(native)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := Canonical(response.Projection)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(expected, actual) || response.Canonical != string(ledger) {
					t.Fatal("deadline settlement changed complete native projection or canonical ledger")
				}
			}
		})
	}
}

func TestNativeExpiredDeliveryDeadlineDoesNotRestartOrRelease(t *testing.T) {
	for _, status := range []string{"completed", "in_progress"} {
		t.Run(status, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, assignment := boundAssignment(t)
			commits = finishMember(t, commits, assignment, 0, "completed")
			installMockLog(t, mock, commits)
			configureNativeRun(mock, assignment)
			mock.nativeRun.Status = status
			if status != "completed" {
				mock.nativeRun.Conclusion = ""
			}
			branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
				t.Fatal("expired Completion deadline restarted verification polling")
				return DeliveryVerification{}, nil
			}
			recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "expired-delivery")
			if err != nil || recovery.Attempts != 0 {
				t.Fatalf("expired deadline recovery failed: %+v %v", recovery, err)
			}
			if status == "completed" {
				if recovery.Publication == nil || recovery.Reason != "delivery_failed_unknown" {
					t.Fatalf("terminal deadline exhaustion did not conservatively settle: %+v", recovery)
				}
			} else if recovery.Publication != nil || recovery.Reason != "delivery_unresolved" || mock.refWrites != 0 {
				t.Fatalf("deadline replaced positive native terminal proof: %+v", recovery)
			}
			latest, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(latest)
			if err != nil || state.Stats.Dispatches != 1 || state.Stats.Completed != 1 ||
				state.Claims[assignment.Claims[1].ClaimID].State != "open" {
				t.Fatalf("deadline released native capacity or changed original sibling ownership: %v", err)
			}
			work := state.Works[assignment.Claims[0].WorkID]
			if status == "completed" {
				if work.Barrier != "failed" || work.Disposition != "unknown" {
					t.Fatal("missing receipt became positive no-effects proof")
				}
			} else if work.Barrier != "pending" {
				t.Fatal("nonterminal native run lost its pending delivery barrier")
			}
		})
	}
}
