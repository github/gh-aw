package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeEffectAndContinuationAuthorityParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript authority parity tooling unavailable; native scope tests still run")
		return
	}
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = finishMember(t, commits, assignment, 1, "completed")
	verified := verifiedMemberResult(t, commits, assignment, 0)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	failure := terminalFor(state, assignment)
	failure.Attempts, failure.Effects = 5, "unknown"
	failed := testOperations(t, commits, testActor("reconciler"), "authority-failure", "delivery_failure", Op(map[string]any{
		"kind": "DeliveryFailure", "work_id": assignment.Claims[0].WorkID,
		"claim_id": assignment.Claims[0].ClaimID, "completion_id": state.Works[assignment.Claims[0].WorkID].CompletionID,
		"reason": "verification_exhausted", "disposition": "unknown", "evidence": failure,
	}))
	tests := []struct {
		name         string
		commits      []QueueCommit
		handle       string
		retired      bool
		effect       string
		continuation string
	}{
		{"pending", commits, "h1", false, "allowed", "allowed"},
		{"verified", verified, "h1", false, "claim_effects_unauthorized", "allowed"},
		{"pending-sibling", verified, "h2", false, "allowed", "allowed"},
		{"failed", failed, "h1", false, "claim_effects_unauthorized", "claim_effects_unauthorized"},
		{"failed-sibling", failed, "h2", false, "allowed", "allowed"},
		{"retired-pending", commits, "h1", true, "claim_ineffective", "claim_ineffective"},
		{"retired-verified", verified, "h1", true, "claim_ineffective", "claim_ineffective"},
	}
	const script = `
const fs = require("node:fs");
const { replayTransactions, validateClaimAuthority, validateWorkerContinuation } =
  require("../../actions/setup/js/work_queue_replay.cjs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
const state = replayTransactions(input.commits);
if (input.retired) state.policy_epoch = "retired-snapshot-epoch";
const dispatch = state.dispatches.get(input.actor.dispatch_id);
const member = dispatch.claims.find(member => member.handle === input.actor.claim_handle);
const context = { ...input.actor, authenticated: true, roles: ["worker"],
  ref: dispatch.run.ref, event: dispatch.run.event };
function outcome(operation) {
  try { operation(); return "allowed"; }
  catch (error) {
    if (typeof error.code !== "string") throw error;
    return error.code;
  }
}
process.stdout.write(JSON.stringify({
  effect: outcome(() => validateClaimAuthority(state, member.claim_id, context,
    { requireCompletion: true })),
  continuation: outcome(() => validateWorkerContinuation(state, context))
}));
`
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := Replay(test.commits)
			if err != nil {
				t.Fatal(err)
			}
			if test.retired {
				state.PolicyEpoch = "retired-snapshot-epoch"
			}
			actor := workerActor(assignment, test.handle)
			request, err := NewRequest("authority-continuation", "dispatch_next", actor, DispatchParameters{
				Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			code := func(err error) string {
				if err == nil {
					return "allowed"
				}
				code, _, _ := strings.Cut(err.Error(), ":")
				return code
			}
			nativeEffect := code(AuthorizeEffect(state, actor))
			nativeContinuation := code(authorizeWorkerQueueRequest(state, actor, request))
			if nativeEffect != test.effect || nativeContinuation != test.continuation {
				t.Fatalf("native expected effects=%s continuation=%s; got %s / %s",
					test.effect, test.continuation, nativeEffect, nativeContinuation)
			}
			input, err := json.Marshal(map[string]any{
				"commits": test.commits, "actor": actor, "retired": test.retired,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, node, "-e", script)
			command.Stdin = bytes.NewReader(input)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil {
				t.Fatalf("JavaScript authority parity: %v\n%s", err, stderr.String())
			}
			var result struct {
				Effect       string `json:"effect"`
				Continuation string `json:"continuation"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			if result.Effect != test.effect || result.Continuation != test.continuation {
				t.Fatalf("JavaScript expected effects=%s continuation=%s; got %+v",
					test.effect, test.continuation, result)
			}
		})
	}
}
