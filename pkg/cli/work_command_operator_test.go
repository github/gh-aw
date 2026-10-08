package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

func workOperatorFixture(t *testing.T, grant bool) (workqueue.Projection, []workqueue.QueueCommit, []string) {
	t.Helper()
	actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: "owner/repo"}
	policy := workqueue.DefaultPolicy(actor.Principal, actor.Repository)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.MaxClaims = 3
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	genesis, err := workqueue.Genesis(actor, policy, "genesis", "init", 1000)
	if err != nil {
		t.Fatal(err)
	}
	nodes := []workqueue.WorkDefinition{}
	ids := []string{}
	for _, key := range []string{"compile", "analyze", "publish"} {
		node, err := workqueue.NewWork([]byte(`{"task":"PAYLOAD-MUST-STAY-PRIVATE","token":"SECRET-MUST-STAY-PRIVATE"}`), "factory", key, "default", policy, 1000)
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
		ids = append(ids, node.WorkID)
	}
	request, err := workqueue.NewRequest("submit", "submit", actor, workqueue.SubmitParameters{Nodes: nodes})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err := workqueue.BuildCandidate([]workqueue.QueueCommit{genesis}, actor, request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if grant {
		request, err = workqueue.NewRequest("grant", "dispatch_next", actor, workqueue.DispatchParameters{Pool: "default", MaxClaims: 3, MaxDispatches: 1, MaxBytes: 48 << 10})
		if err != nil {
			t.Fatal(err)
		}
		commits, _, _, err = workqueue.BuildCandidate(commits, actor, request, 3000)
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := workqueue.Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return state, commits, ids
}

func TestWorkOperatorForestDetailsAndRedaction(t *testing.T) {
	state, _, ids := workOperatorFixture(t, true)
	rows := workQueueRows(state, workQueueFilter{}, nil)
	if len(rows) != 7 || rows[0].Kind != "graph" || rows[1].ID != ids[0] {
		t.Fatalf("unstable graph/Work/Claim forest: %+v", rows)
	}
	for _, row := range rows {
		text, err := workQueueDetails(state, row, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, "PAYLOAD-MUST") || strings.Contains(text, "SECRET-MUST") {
			t.Fatal("details leaked stored payload")
		}
		if row.Kind == "claim" && (!strings.Contains(text, row.ID) || !strings.Contains(text, "Original assignment: 3 Claims") ||
			!strings.Contains(text, "native reservation retained: true")) {
			t.Fatalf("Claim details omit immutable scope/reservation: %s", text)
		}
	}
	// A diamond is represented as two references, never duplicated ownership.
	state.Works[ids[2]].DependsOn = []workqueue.Dependency{{Kind: "work", WorkID: ids[0]}, {Kind: "work", WorkID: ids[1]}}
	text, err := workQueueDetails(state, workQueueRow{Kind: "work", ID: ids[2], WorkID: ids[2]}, 4000)
	if err != nil || !strings.Contains(text, ids[0]) || !strings.Contains(text, ids[1]) || !strings.Contains(text, "cross-references") {
		t.Fatalf("DAG details lost cross-references: %v %s", err, text)
	}
	state.Works[ids[0]].NodeKey = "\x1b[31mINJECT\n\u202e"
	rows = workQueueRows(state, workQueueFilter{}, nil)
	if strings.Contains(rows[1].Text, "\x1b") || strings.Contains(rows[1].Text, "\n") || strings.Contains(rows[1].Text, "\u202e") {
		t.Fatal("forest allowed terminal or bidi injection")
	}
	for _, row := range rows {
		for _, r := range row.Text {
			if r > 127 {
				t.Fatal("forest is not ASCII")
			}
		}
	}
	filtered := workQueueRows(state, workQueueFilter{Query: rows[2].ID}, nil)
	if len(filtered) != 3 || filtered[2].ID != rows[2].ID {
		t.Fatal("Claim search did not retain its Work context")
	}
	collapsed := workQueueRows(state, workQueueFilter{}, map[string]bool{ids[0]: true})
	if len(collapsed) != 6 {
		t.Fatal("collapse lost Work or did not hide Claim history")
	}
}

func TestWorkOperatorStateBoundsEmptyAndPagination(t *testing.T) {
	state, _, _ := workOperatorFixture(t, false)
	row := workQueueRow{Kind: "work", ID: "copyable", Text: strings.Repeat("x", 2000)}
	rows := make([]workQueueRow, 256)
	for index := range rows {
		rows[index] = row
	}
	page, err := workQueueStatePage(state, "work-queue", rows, 0, 256)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > workQueueViewBytes || len(workQueuePageText(page)) > workQueueViewBytes || page.NextOffset == nil || *page.NextOffset == 0 {
		t.Fatal("state page is not bounded or cannot be resumed")
	}
	next, err := workQueueStatePage(state, "work-queue", rows, *page.NextOffset, 3)
	if err != nil || len(next.Rows) != 3 || next.Offset != *page.NextOffset {
		t.Fatal("pagination lost its exact next offset")
	}
	empty, err := workQueueStatePage(state, "work-queue", nil, 0, 80)
	if err != nil || !strings.Contains(workQueuePageText(empty), "No matching Work or Claims") {
		t.Fatal("empty view is unclear")
	}
}

func TestWorkOperatorStablePriorityRecovery(t *testing.T) {
	state, commits, ids := workOperatorFixture(t, false)
	targets := []workOperatorTarget{{WorkID: ids[0]}, {WorkID: ids[1]}}
	params, kind, err := workOperatorIntent(state, slices.Clone(targets), "operator_reprioritized", 1, "priority-action")
	if err != nil {
		t.Fatal(err)
	}
	actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: "owner/repo"}
	request, err := workqueue.NewRequest("priority-action", kind, actor, params)
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = workqueue.BuildCandidate(commits, actor, request, 3000)
	if err != nil {
		t.Fatal(err)
	}
	state, err = workqueue.Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, err := workOperatorIntent(state, slices.Clone(targets), "operator_reprioritized", 1, "priority-action")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(params)
	after, _ := json.Marshal(recovered)
	if string(before) != string(after) {
		t.Fatal("restarted operator action rewrote its accepted compare-and-set fingerprint")
	}
	if _, _, err := workOperatorIntent(state, targets, "operator_reprioritized", 2, "priority-action"); err == nil {
		t.Fatal("request reuse changed desired priority")
	}
	duplicate := []workOperatorTarget{{WorkID: ids[0]}, {WorkID: ids[0]}}
	if _, _, err := workOperatorIntent(state, duplicate, "operator_cancelled", 0, "duplicate"); err == nil {
		t.Fatal("duplicate bulk Work accepted")
	}
	for index := range targets {
		if got := state.Works[targets[index].WorkID].SchedulingPriority(); got != 1 {
			t.Fatalf("priority target %d has %d", index, got)
		}
	}
}
