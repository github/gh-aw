package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestNativeEnginesBoundedObservedRefreshParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-engine test tooling requires Node; native production has no Node dependency")
	}
	for _, test := range []struct {
		name           string
		operations     int
		frontier       int
		expectedProofs int
	}{
		{name: "small-operation-budget", operations: 3, frontier: 4, expectedProofs: 2},
		{name: "large-unknown-frontier", operations: 256, frontier: 130, expectedProofs: 128},
	} {
		t.Run(test.name, func(t *testing.T) {
			commits, ready := observationFrontierFixture(t, test.operations, test.frontier, true)
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			actor := testActor("administrator")
			request, err := NewRequest("independent-grant", "dispatch_next", actor, DispatchParameters{
				Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			input, err := canonicalValue(map[string]any{
				"commits": commits, "request": request, "actor": actor,
				"at": 5000, "commit_id": ProposedCommitID(state, request),
			})
			if err != nil {
				t.Fatal(err)
			}
			const script = `
const fs = require("node:fs");
const { canonical } = require("./actions/setup/js/work_queue_codec.cjs");
const { refreshDependencies } = require("./actions/setup/js/work_queue_dispatch.cjs");
const { replayTransactions, serializeProjection, planDispatchWithObservations, appendCommit } = require("./actions/setup/js/work_queue_replay.cjs");
(async () => {
  const input = JSON.parse(fs.readFileSync(0, "utf8"));
  const state = replayTransactions(input.commits);
  const before = canonical(serializeProjection(state));
  const githubClient = new Proxy({}, { get() { throw new Error("missing read scope must not invoke an SDK"); } });
  const observations = await refreshDependencies({ now: input.at, githubClient }, state, "default", { role: "administrator" });
  const decision = planDispatchWithObservations(state, input.request, input.actor, input.at, input.commit_id, observations);
  const commit = { version: 3, id: input.commit_id, previous: state.tip, request: input.request, actor: input.actor, policy_epoch: state.policy_epoch, at: input.at, operations: decision.operations };
  const accepted = appendCommit(input.commits, commit).state;
  console.log(JSON.stringify({
    observations, operations: decision.operations, assignments: decision.assignments,
    projection: canonical(serializeProjection(accepted)),
    pure: before === canonical(serializeProjection(state)),
  }));
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
`
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, node, "-e", script)
			command.Dir = "../.."
			command.Stdin = bytes.NewReader(input)
			var diagnostics bytes.Buffer
			command.Stderr = &diagnostics
			output, err := command.Output()
			if err != nil {
				t.Fatalf("actual JS observed dispatch failed: %v\n%s", err, diagnostics.String())
			}
			var result struct {
				Observations []Observation `json:"observations"`
				Operations   []Operation   `json:"operations"`
				Assignments  []Assignment  `json:"assignments"`
				Projection   string        `json:"projection"`
				Pure         bool          `json:"pure"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			if !result.Pure || len(result.Observations) != test.expectedProofs ||
				len(result.Operations) != test.expectedProofs+1 ||
				len(result.Assignments) != 1 || result.Assignments[0].Claims[0].WorkID != ready.WorkID {
				t.Fatal("bounded automatic unknown proofs blocked independent ready Work or mutated a preview")
			}
			next, commit, decision, err := buildCandidateWithObservations(commits, actor, request, 5000, result.Observations)
			if err != nil {
				t.Fatal(err)
			}
			if commit == nil || !sameJSON(commit.Operations, result.Operations) ||
				!sameJSON(decision.Assignments, result.Assignments) {
				t.Fatal("actual native observed prefixes or assignments differ")
			}
			accepted, err := Replay(next)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := canonicalValue(accepted)
			if err != nil || string(projection) != result.Projection {
				t.Fatalf("complete native observed projections differ: %v", err)
			}
		})
	}
}
