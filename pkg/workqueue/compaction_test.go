package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func noncanonicalMockLog(t *testing.T, mock *queueAPI, commits []QueueCommit) {
	t.Helper()
	installMockLog(t, mock, commits)
	var data bytes.Buffer
	for index := len(commits) - 1; index >= 0; index-- {
		line, err := json.Marshal(commits[index])
		if err != nil {
			t.Fatal(err)
		}
		data.WriteString(" ")
		data.Write(line)
		data.WriteString(" \n")
	}
	line, err := json.Marshal(commits[0])
	if err != nil {
		t.Fatal(err)
	}
	data.Write(line)
	data.WriteByte('\n')
	mock.logs[mock.commits[mock.head].Tree] = data.String()
}

func TestBranchCompactionPreservesCompleteAuthorityAndIsIdempotent(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = verifiedMemberResult(t, commits, assignment, 0)
	commits = finishMember(t, commits, assignment, 1, "cancelled")
	before, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	noncanonicalMockLog(t, mock, commits)
	result, err := branch.Compact(context.Background())
	if err != nil || !result.Changed || result.DuplicatesRemoved != 1 ||
		result.Tip != before.Tip || result.Commits != len(commits) || result.BytesAfter != len(expected) {
		t.Fatalf("canonical/deduplicating compaction failed: %+v %v", result, err)
	}
	if actual := mock.logs[mock.commits[mock.head].Tree]; actual != string(expected) {
		t.Fatal("compaction rewrote a unique operation or failed to retain the causal chain")
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, err := Replay(latest)
	if err != nil || !sameJSON(before, after) || after.Stats.Dispatches != 1 {
		t.Fatal("compaction changed debt, request identity, outcomes or held native capacity")
	}
	next, err := branch.Compact(context.Background())
	if err != nil || next.Changed || next.AcknowledgmentRecovered || mock.refWrites != 1 {
		t.Fatalf("canonical compaction created unnecessary commits: %+v %v", next, err)
	}
	terminal := terminalFor(after, assignment)
	third := assignment.Claims[2]
	closed := testOperations(t, commits, testActor("reconciler"), "compact-released", "release",
		Op(map[string]any{"kind": "ClaimCancellation", "work_id": third.WorkID, "claim_id": third.ClaimID,
			"reason": "run_terminal", "retry_not_before": 34000}),
		Op(map[string]any{"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminal}))
	closedState, err := Replay(closed)
	if err != nil {
		t.Fatal(err)
	}
	noncanonicalMockLog(t, mock, closed)
	released, err := branch.Compact(context.Background())
	if err != nil || !released.Changed {
		t.Fatalf("released historical chain failed compaction: %+v %v", released, err)
	}
	latest, err = branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterRelease, err := Replay(latest)
	if err != nil || !sameJSON(closedState, afterRelease) ||
		afterRelease.Stats.Dispatches != 0 || afterRelease.Works[assignment.Claims[0].WorkID].Barrier != "verified" {
		t.Fatal("compaction lost terminal release, verified delivery or independent retry history")
	}
}

func TestBranchCompactionRecomputesOnConflictAndRecoversLostAcknowledgment(t *testing.T) {
	for _, name := range []string{"conflict", "ambiguous", "rewritten"} {
		t.Run(name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, _ := boundAssignment(t)
			noncanonicalMockLog(t, mock, commits)
			expected := commits
			switch name {
			case "conflict":
				node := testNode(t, commits, "concurrent-node")
				expected = testSubmit(t, commits, "concurrent-submit", node)
				mock.concurrent = func() { noncanonicalMockLog(t, mock, expected) }
			case "ambiguous":
				mock.ambiguous = true
			case "rewritten":
				mock.concurrent = func() { installMockLog(t, mock, testGenesis(t, nil)) }
			}
			result, err := branch.Compact(context.Background())
			if name == "rewritten" {
				if err == nil || !strings.HasPrefix(err.Error(), "ledger_nonextending:") || mock.refWrites != 1 {
					t.Fatalf("compaction accepted deleted history: %+v %v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := Serialize(expected)
			if err != nil {
				t.Fatal(err)
			}
			if mock.logs[mock.commits[mock.head].Tree] != string(data) {
				t.Fatal("CAS compaction discarded a concurrent operation or duplicated history")
			}
			if name == "ambiguous" && (!result.AcknowledgmentRecovered || result.Changed || mock.refWrites != 1) {
				t.Fatalf("ambiguous acknowledgment caused another write or false ownership: %+v", result)
			}
			if name == "conflict" && (!result.Changed || result.AcknowledgmentRecovered || mock.refWrites != 2) {
				t.Fatalf("CAS loser did not canonicalize the fresh full chain: %+v", result)
			}
		})
	}
}

func TestBranchCompactionNeverInitializesOrAdoptsInvalidAuthority(t *testing.T) {
	for _, name := range []string{"missing", "legacy", "conflicting-duplicate"} {
		t.Run(name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			if name != "missing" {
				commits := testGenesis(t, nil)
				installMockLog(t, mock, commits)
				if name == "legacy" {
					mock.logs[mock.commits[mock.head].Tree] = "{\"version\":2}\n"
				} else {
					other := commits[0]
					other.At++
					line, err := canonicalValue(other)
					if err != nil {
						t.Fatal(err)
					}
					mock.logs[mock.commits[mock.head].Tree] += string(line) + "\n"
				}
			}
			if _, err := branch.Compact(context.Background()); err == nil || mock.refWrites != 0 {
				t.Fatal("invalid/missing authority was initialized, adopted or rewritten")
			}
		})
	}
}
