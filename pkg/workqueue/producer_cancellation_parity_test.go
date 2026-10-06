package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestProducerCancellationPreservesForeignScopeAndNativeProjection(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		policy.AccountingWeights["other"] = 1
		policy.Producers["1002"] = ProducerRule{
			Pools: []string{"default"}, Priorities: []int{3}, FairnessKeys: []string{"other"},
		}
	})
	own := testNode(t, commits, "producer-owned")
	other := testNode(t, commits, "producer-foreign-account")
	other.FairnessKey = "other"
	commits = testSubmit(t, commits, "admit-owned-cancellation", own)
	actor := testActor("producer")
	actor.Principal = "1002"
	request, err := NewRequest("admit-cancellation-scopes", "submit", actor,
		SubmitParameters{Nodes: []WorkDefinition{other}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, actor, request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	actor = testActor("producer")
	foreign, err := NewRequest("foreign-cancellation", "cancel_work", actor,
		OperationsParameters{Operations: []Operation{Op(map[string]any{
			"kind": "WorkCancellation", "work_id": other.WorkID, "reason": "producer_cancelled",
		})}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, foreign, 3000); err == nil ||
		!strings.HasPrefix(err.Error(), "admission_unauthorized:") {
		t.Fatalf("producer cancellation escaped its installed accounting entitlement: %v", err)
	}
	open := slices.Clone(commits)
	commits = testOperations(t, commits, actor, "owned-cancellation", "cancel_work", Op(map[string]any{
		"kind": "WorkCancellation", "work_id": own.WorkID, "reason": "producer_cancelled",
	}))
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if state.Works[own.WorkID].State != "cancelled" || state.Works[other.WorkID].State != "available" {
		t.Fatal("producer cancellation did not preserve independent foreign-account Work")
	}
	ledger, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript cancellation parity tooling unavailable; native entitlement checks passed")
		return
	}
	input, err := json.Marshal(map[string]any{
		"action": "replay", "data": string(ledger), "pool": "default", "at": 4000, "include_canonical": true,
		"parameters": DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10},
		"request_id": "producer-parity-request", "commit_id": "producer-parity-commit",
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
		t.Fatalf("JavaScript producer cancellation parity: %v\n%s", err, stderr.String())
	}
	var response struct {
		Error      string          `json:"error"`
		Projection json.RawMessage `json:"projection"`
		Canonical  string          `json:"canonical_ledger"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" {
		t.Fatalf("JavaScript rejected entitled producer cancellation: %s", response.Error)
	}
	expected, err := canonicalValue(state)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := Canonical(response.Projection)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) || response.Canonical != string(ledger) {
		t.Fatal("producer cancellation changed canonical bytes or complete native projection")
	}
	for _, test := range []struct {
		name    string
		commits []QueueCommit
	}{
		{"open", open},
		{"already-cancelled", commits},
	} {
		t.Run("foreign-"+test.name, func(t *testing.T) {
			actor := testActor("producer")
			actor.Principal = "1002"
			operations := []Operation{Op(map[string]any{
				"kind": "WorkCancellation", "work_id": own.WorkID, "reason": "producer_cancelled",
			})}
			request, err := NewRequest("foreign-"+test.name, "cancel_work", actor,
				OperationsParameters{Operations: operations})
			if err != nil {
				t.Fatal(err)
			}
			previous := test.commits[len(test.commits)-1].ID
			rejected := append(slices.Clone(test.commits), QueueCommit{
				Version: Version, ID: "foreign-" + test.name, Previous: &previous,
				Request: request, Actor: actor, PolicyEpoch: state.PolicyEpoch, At: 5000,
				Operations: operations,
			})
			if _, err := Replay(rejected); err == nil ||
				!strings.HasPrefix(err.Error(), "admission_unauthorized:") {
				t.Fatalf("Go accepted fresh foreign-account %s cancellation: %v", test.name, err)
			}
			var data bytes.Buffer
			for _, commit := range rejected {
				line, err := canonicalValue(commit)
				if err != nil {
					t.Fatal(err)
				}
				data.Write(line)
				data.WriteByte('\n')
			}
			input, err := json.Marshal(map[string]any{"action": "replay", "data": data.String()})
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
				t.Fatalf("JavaScript cancellation rejection parity: %v\n%s", err, stderr.String())
			}
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(output, &response); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(response.Error, "admission_unauthorized:") {
				t.Fatalf("JavaScript accepted fresh foreign-account %s cancellation: %s", test.name, response.Error)
			}
		})
	}
}
