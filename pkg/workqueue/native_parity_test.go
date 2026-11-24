package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Differential parity: the Go engine and the JS engine (work_queue_conformance_checks.cjs
// replayCommand) must produce the same canonical projection, wire and decision, or
// reject with the same code, for the shared lifecycle ledger and systematic
// mutations of its commits.

type parityInput struct {
	Transactions []any               `json:"transactions"`
	Parameters   *DispatchParameters `json:"parameters,omitempty"`
	RequestID    string              `json:"request_id,omitempty"`
	// Checkpoint, when set, also builds the replayed projection's Checkpoint commit as this actor.
	Checkpoint *Actor `json:"checkpoint,omitempty"`
	label      string
}

func goReplayCommand(t *testing.T, input parityInput) string {
	t.Helper()
	records := make([]any, len(input.Transactions))
	for index, record := range input.Transactions {
		records[index] = deepCopy(record)
	}
	state, err := replayValues(records)
	if err != nil {
		code := ErrorCode(err)
		require.NotEmpty(t, code, "Go replay error without a protocol code: %v", err)
		data, _ := canonicalGeneric(map[string]any{"error": code})
		return string(data)
	}
	projection, err := SerializeProjection(state)
	require.NoError(t, err)
	wire, err := serializeTransactions(state.Transactions)
	require.NoError(t, err)
	output := map[string]any{"state": json.RawMessage(projection), "canonical": string(wire)}
	if input.Parameters != nil {
		decision, err := PlanDispatch(state, *input.Parameters, input.RequestID)
		require.NoError(t, err)
		output["decision"] = decision
	}
	if input.Checkpoint != nil {
		checkpoint, err := CheckpointCommit(state, *input.Checkpoint, 1)
		require.NoError(t, err)
		output["checkpoint"] = checkpoint
	}
	data, err := CanonicalValue(output)
	require.NoError(t, err)
	return string(data)
}

const jsBatchHarness = `
const { replayCommand } = require("./actions/setup/js/work_queue_conformance_checks.cjs");
const { canonical, parseStrictJSON } = require("./actions/setup/js/work_queue_codec.cjs");
const inputs = parseStrictJSON(require("node:fs").readFileSync(0, "utf8"), { maxBytes: 80 * 1024 * 1024 });
process.stdout.write(inputs.map(input => canonical(replayCommand(input))).join("\n") + "\n");
`

func jsReplayCommands(t *testing.T, inputs []parityInput) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the differential parity test")
	}
	data, err := json.Marshal(inputs)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", jsBatchHarness)
	command.Dir = "../.."
	command.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	require.NoError(t, err, stderr.String())
	lines := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
	require.Len(t, lines, len(inputs))
	results := make([]string, len(lines))
	for index, line := range lines {
		results[index] = string(line)
	}
	return results
}

func deepCopy(value any) any {
	switch value := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(value))
		for key, child := range value {
			copied[key] = deepCopy(child)
		}
		return copied
	case []any:
		copied := make([]any, len(value))
		for index, child := range value {
			copied[index] = deepCopy(child)
		}
		return copied
	default:
		return value
	}
}

type valuePath []any

func collectPaths(value any, prefix valuePath, depth int, paths *[]valuePath) {
	if depth > 5 {
		return
	}
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			path := append(append(valuePath{}, prefix...), key)
			*paths = append(*paths, path)
			collectPaths(value[key], path, depth+1, paths)
		}
	case []any:
		for index, child := range value {
			path := append(append(valuePath{}, prefix...), index)
			*paths = append(*paths, path)
			collectPaths(child, path, depth+1, paths)
		}
	}
}

// mutate replaces (or deletes, when replacement is the delete sentinel) the value at path.
func mutate(root any, path valuePath, replacement any) any {
	copied := deepCopy(root)
	parent := copied
	for _, step := range path[:len(path)-1] {
		switch step := step.(type) {
		case string:
			parent = parent.(map[string]any)[step]
		case int:
			parent = parent.([]any)[step]
		}
	}
	switch last := path[len(path)-1].(type) {
	case string:
		if replacement == deleteSentinel {
			delete(parent.(map[string]any), last)
		} else {
			parent.(map[string]any)[last] = replacement
		}
	case int:
		if replacement == deleteSentinel {
			return nil
		}
		parent.([]any)[last] = replacement
	}
	return copied
}

type deleteMarker struct{}

var deleteSentinel any = deleteMarker{}

func TestNativeParityWithJavaScript(t *testing.T) {
	var fixture prefixFixture
	loadFixture(t, "canonical-prefix.json", &fixture)
	commits := make([]any, len(fixture.Commits))
	for index, record := range fixture.Commits {
		value, err := decodeStrict(record)
		require.NoError(t, err)
		commits[index] = value
	}
	parameters := fixture.NextParameters
	inputs := []parityInput{{Transactions: []any{}}}
	for index := range commits {
		inputs = append(inputs, parityInput{Transactions: commits[:index+1], Parameters: &parameters, RequestID: fixture.NextRequestID})
	}
	// Checkpoint restore followed by nothing, and the reversed (non-causal) ledger.
	checkpoint, err := decodeStrict(fixture.Checkpoint.Commit)
	require.NoError(t, err)
	inputs = append(inputs, parityInput{Transactions: []any{checkpoint}, Parameters: &parameters, RequestID: fixture.NextRequestID})
	reversed := make([]any, len(commits))
	for index, commit := range commits {
		reversed[len(commits)-1-index] = commit
	}
	inputs = append(inputs, parityInput{Transactions: reversed})
	replacements := []any{deleteSentinel, nil, json.Number("1"), json.Number("0"), "", "x", true, []any{}, map[string]any{}}
	for index, commit := range commits {
		var paths []valuePath
		collectPaths(commit, nil, 0, &paths)
		for _, path := range paths {
			for _, replacement := range replacements {
				mutated := mutate(commit, path, replacement)
				if mutated == nil {
					continue
				}
				transactions := append(append([]any{}, commits[:index]...), mutated)
				inputs = append(inputs, parityInput{Transactions: transactions, label: fmt.Sprintf("commit %d %v -> %#v", index, path, replacement)})
			}
		}
	}
	// Checkpoint-state mutations exercise restore validation.
	var paths []valuePath
	collectPaths(checkpoint, nil, 0, &paths)
	for _, path := range paths {
		for _, replacement := range replacements[:4] {
			if mutated := mutate(checkpoint, path, replacement); mutated != nil {
				inputs = append(inputs, parityInput{Transactions: []any{mutated}, label: fmt.Sprintf("checkpoint %v -> %#v", path, replacement)})
			}
		}
	}
	// One Claim per Dispatch is a replay rule: a grouped grant and a grouped or inconsistent
	// checkpoint fail with the same literal code in both engines. Checkpoint pruning builds
	// byte-identical checkpoints and restores (or rejects) them alike. A Dispatch records the
	// dispatcher attempt that reserved it, and only that attempt records its launch.
	rules := append(append(groupedDispatchCases(t, commits, checkpoint), pruningCases(t)...), reservedByCases(t, checkpoint)...)
	for _, rule := range rules {
		inputs = append(inputs, rule.input)
	}
	t.Logf("differential cases: %d", len(inputs))
	expected := jsReplayCommands(t, inputs)
	for index, rule := range rules {
		js, native := expected[len(inputs)-len(rules)+index], goReplayCommand(t, rule.input)
		if rule.code == "" {
			require.False(t, strings.HasPrefix(js, `{"error"`), "js: %s: %s", rule.input.label, js)
			require.False(t, strings.HasPrefix(native, `{"error"`), "go: %s: %s", rule.input.label, native)
			continue
		}
		want, err := canonicalGeneric(map[string]any{"error": rule.code})
		require.NoError(t, err)
		require.Equal(t, string(want), js, "js: %s", rule.input.label)
		require.Equal(t, string(want), native, "go: %s", rule.input.label)
	}
	mismatches := 0
	for index, input := range inputs {
		actual := goReplayCommand(t, input)
		if actual != expected[index] {
			mismatches++
			if mismatches <= 400 {
				t.Errorf("case %d (%s)\n go: %.200s\n js: %.200s", index, input.label, actual, expected[index])
			}
		}
	}
	require.Zero(t, mismatches, "%d of %d differential cases differ", mismatches, len(inputs))
}

// groupedDispatchCase is one input with the literal error code both engines must return,
// or "" when both must accept it.
type groupedDispatchCase struct {
	input parityInput
	code  string
}

// groupedDispatchCases derives, from the first grant (two Work, two Dispatches, both "h1")
// and the shared root checkpoint, inputs that group Claims under one Dispatch.
func groupedDispatchCases(t *testing.T, commits []any, checkpoint any) []groupedDispatchCase {
	t.Helper()
	grant := deepCopy(commits[1]).(map[string]any)
	operations := grant["operations"].([]any)
	first, second := operations[1].(map[string]any), operations[2].(map[string]any)
	require.Equal(t, "Claim", first["kind"])
	require.Equal(t, "Claim", second["kind"])
	second["dispatch_id"], second["handle"] = first["dispatch_id"], "h2"
	cases := []groupedDispatchCase{{parityInput{Transactions: []any{commits[0], grant}, label: "grouped grant"}, "dispatch_state_conflict"}}
	mutations := []struct {
		label  string
		change func(claims, dispatches map[string]any)
	}{
		{"two listed members", func(claims, dispatches map[string]any) {
			a, b := claimsByState(t, claims, "completed"), claimsByState(t, claims, "cancelled")
			dispatch := dispatches[a["dispatch_id"].(string)].(map[string]any)
			dispatch["claims"] = []any{a["claim_id"], b["claim_id"]}
		}},
		{"hidden second Claim", func(claims, dispatches map[string]any) {
			a, b := claimsByState(t, claims, "completed"), claimsByState(t, claims, "cancelled")
			delete(dispatches, b["dispatch_id"].(string))
			b["dispatch_id"] = a["dispatch_id"]
		}},
		{"closed Dispatch with open Claim", func(claims, dispatches map[string]any) {
			open := claimsByState(t, claims, "open")
			dispatches[open["dispatch_id"].(string)].(map[string]any)["state"] = "closed"
		}},
	}
	for _, mutation := range mutations {
		mutated := deepCopy(checkpoint).(map[string]any)
		state := mutated["operations"].([]any)[0].(map[string]any)["state"].(map[string]any)
		mutation.change(state["claims"].(map[string]any), state["dispatches"].(map[string]any))
		cases = append(cases, groupedDispatchCase{parityInput{Transactions: []any{mutated}, label: mutation.label}, "checkpoint_invalid"})
	}
	return cases
}

// reservedByCases checks the reserving dispatcher attempt in both engines: only a run attempt
// reserves a Dispatch, only that attempt records its launch, and a checkpoint Dispatch keeps a
// well-formed reserved_by.
func reservedByCases(t *testing.T, checkpoint any) []groupedDispatchCase {
	t.Helper()
	log, decision := testDispatchNext(t, testSubmit(t, nil, testWork(t, "a", nil)), testDispatchParameters(nil))
	require.Len(t, decision.Dispatches, 1)
	dispatchID := decision.Dispatches[0]
	started := mustOps(t, DispatchOperation{Kind: "Dispatch", DispatchID: dispatchID, State: "started", RunID: "200", Revision: testRevision})
	rerun := testDispatcher
	rerun.RunAttempt = 2
	otherAttempt := buildTestCommit(t, log, rerun, "launch", DispatchIDParameters{DispatchID: dispatchID}, started)

	submitted := testSubmit(t, nil, testWork(t, "a", nil))
	state, err := Replay(submitted)
	require.NoError(t, err)
	parameters := testDispatchParameters(nil)
	request, err := NewRequest("dispatch_next", testAdministrator, parameters)
	require.NoError(t, err)
	plan, err := PlanDispatch(state, parameters, request.ID)
	require.NoError(t, err)
	values := []any{}
	for _, operation := range plan.Operations {
		values = append(values, operation)
	}
	runless := BuildCommit(state, testAdministrator, request, mustOps(t, values...), 1)

	cases := []groupedDispatchCase{
		{parityInput{Transactions: genericCommits(t, testLaunch(t, log, dispatchID, "200")...), label: "launch by the reserving attempt"}, ""},
		{parityInput{Transactions: genericCommits(t, append(log, otherAttempt)...), label: "launch by another dispatcher attempt"}, "actor_unauthorized"},
		{parityInput{Transactions: genericCommits(t, append(submitted, runless)...), label: "reservation by an actor without a run"}, "actor_unauthorized"},
	}
	mutations := []struct {
		label, code string
		value       any
	}{
		{"missing reserved_by", "checkpoint_invalid", deleteSentinel},
		{"null reserved_by", "checkpoint_invalid", nil},
		{"reserved_by with an extra field", "checkpoint_invalid", map[string]any{"run_id": "100", "run_attempt": json.Number("1"), "workflow": "x"}},
		{"reserved_by without its attempt", "checkpoint_invalid", map[string]any{"run_id": "100"}},
		{"reserved_by with a non-decimal run", "checkpoint_invalid", map[string]any{"run_id": "run-100", "run_attempt": json.Number("1")}},
		{"reserved_by with attempt zero", "ledger_invalid", map[string]any{"run_id": "100", "run_attempt": json.Number("0")}},
	}
	for _, mutation := range mutations {
		mutated := deepCopy(checkpoint).(map[string]any)
		dispatches := mutated["operations"].([]any)[0].(map[string]any)["state"].(map[string]any)["dispatches"].(map[string]any)
		for _, id := range sortedKeys(dispatches) {
			dispatch := dispatches[id].(map[string]any)
			if mutation.value == deleteSentinel {
				delete(dispatch, "reserved_by")
			} else {
				dispatch["reserved_by"] = mutation.value
			}
		}
		cases = append(cases, groupedDispatchCase{parityInput{Transactions: []any{mutated}, label: mutation.label}, mutation.code})
	}
	return cases
}

// claimsByState returns the first checkpoint Claim, in key order, in the given state.
func claimsByState(t *testing.T, claims map[string]any, state string) map[string]any {
	t.Helper()
	for _, id := range sortedKeys(claims) {
		if claim := claims[id].(map[string]any); claim["state"] == state {
			return claim
		}
	}
	require.FailNow(t, "checkpoint has no Claim in state "+state)
	return nil
}

var testProjector = Actor{Role: "projector", Repository: testRepository, Workflow: ".github/workflows/dispatcher.lock.yml", RunID: "100", RunAttempt: 1}

func genericCommits(t *testing.T, commits ...QueueCommit) []any {
	t.Helper()
	values := make([]any, len(commits))
	for index, commit := range commits {
		value, err := genericValue(commit)
		require.NoError(t, err)
		values[index] = value
	}
	return values
}

func checkpointStateOf(t *testing.T, commit QueueCommit) object {
	t.Helper()
	return genericCommits(t, commit)[0].(object)["operations"].([]any)[0].(object)["state"].(object)
}

func issueResource(number string) Resource {
	return Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: number, Number: number}
}

// pruningLedger holds a completed chain x -> y -> z with z waiting on y, a cancelled Work
// gated on Issue 72, a waiting Work gated on Issue 71, and a completed Work whose backing
// Issue 73 still awaits the projector's terminal comment.
func pruningLedger(t *testing.T) ([]QueueCommit, map[string]WorkDefinition) {
	t.Helper()
	gate := func(number string) func(*WorkDefinition) {
		resource := issueResource(number)
		return func(work *WorkDefinition) {
			work.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
		}
	}
	after := func(parent WorkDefinition) func(*WorkDefinition) {
		return func(work *WorkDefinition) { work.DependsOn = []Dependency{{Kind: "work", WorkID: parent.WorkID}} }
	}
	works := map[string]WorkDefinition{"x": testWork(t, "x", nil), "gated": testWork(t, "gated", gate("71")), "lost": testWork(t, "lost", gate("72")), "backed": testWork(t, "backed", nil)}
	works["y"] = testWork(t, "y", after(works["x"]))
	works["z"] = testWork(t, "z", after(works["y"]))
	parameters := testDispatchParameters(nil)
	grant := func(log []QueueCommit, actor Actor, values ...any) ([]QueueCommit, []string) {
		state, err := Replay(log)
		require.NoError(t, err)
		request, err := NewRequest("dispatch_next", actor, parameters)
		require.NoError(t, err)
		decision, err := PlanDispatch(state, parameters, request.ID)
		require.NoError(t, err)
		for _, operation := range decision.Operations {
			values = append(values, operation)
		}
		return appendTestCommit(t, log, actor, "dispatch_next", parameters, mustOps(t, values...)), decision.Dispatches
	}
	run := func(log []QueueCommit, dispatchID, runID string) []QueueCommit {
		log = testBind(t, testLaunch(t, log, dispatchID, runID), dispatchID, testWorker(runID, 1))
		return appendTestCommit(t, log, testWorker(runID, 1), "finish", DispatchIDParameters{DispatchID: dispatchID}, testFinishOperations(t, log, dispatchID))
	}
	waiting := func(number string) ObservationOperation {
		return ObservationOperation{Kind: "Observation", ObservationID: "o-" + number, Resource: issueResource(number), Condition: "completed", State: "waiting"}
	}
	// Dispatcher runs 101 and 102 grant here; run 100 stays free for a grant after the checkpoint.
	first, second := testDispatcher, testDispatcher
	first.RunID, second.RunID = "101", "102"
	log, dispatches := grant(testSubmit(t, nil, works["x"], works["y"], works["z"], works["gated"], works["lost"], works["backed"]), first, waiting("71"), waiting("72"))
	require.Len(t, dispatches, 2)
	log = run(run(log, dispatches[0], "7"), dispatches[1], "8")
	link := mustOps(t, IssueLinkOperation{Kind: "IssueLink", WorkID: works["backed"].WorkID, Resource: issueResource("73")})
	log = appendTestCommit(t, log, testProjector, "issue_link", IssueLinkParameters{Operations: link}, link)
	log, dispatches = grant(log, second)
	require.Len(t, dispatches, 1)
	log = run(log, dispatches[0], "9")
	lost := works["lost"].WorkID
	log = appendTestCommit(t, log, testProducer, "cancel_work", CancelWorkParameters{WorkIDs: []string{lost}, Reason: "operator"}, mustOps(t, WorkCancellationOperation{Kind: "WorkCancellation", WorkID: lost, Reason: "operator"}))
	return log, works
}

// windowLedger cancels w, kept as the predecessor of waiting Work, then re-sends that
// idempotent cancellation from RetainedRequests+8 producer runs; it returns the first and
// last re-sent commits.
func windowLedger(t *testing.T) ([]QueueCommit, QueueCommit, QueueCommit) {
	t.Helper()
	w := testWork(t, "w", nil)
	dependent := testWork(t, "after-w", func(work *WorkDefinition) { work.DependsOn = []Dependency{{Kind: "work", WorkID: w.WorkID}} })
	parameters := CancelWorkParameters{WorkIDs: []string{w.WorkID}, Reason: "operator"}
	cancel := mustOps(t, WorkCancellationOperation{Kind: "WorkCancellation", WorkID: w.WorkID, Reason: "operator"})
	log := appendTestCommit(t, testSubmit(t, nil, w, dependent), testProducer, "cancel_work", parameters, cancel)
	state, err := Replay(log)
	require.NoError(t, err)
	for run := range RetainedRequests + 8 {
		actor := testProducer
		actor.RunID = strconv.Itoa(1000 + run)
		request, err := NewRequest("cancel_work", actor, parameters)
		require.NoError(t, err)
		commit := BuildCommit(state, actor, request, cancel, 1)
		value, err := genericValue(commit)
		require.NoError(t, err)
		require.NoError(t, state.applyCommitValue(value))
		log = append(log, commit)
	}
	return log, log[2], log[len(log)-1]
}

// pruningCases checks the Checkpoint fold in both engines: byte-identical checkpoints of the
// pruning and window ledgers, the same restored projection and decision, and the same
// literal verdict for pruned shapes restore must accept or reject.
func pruningCases(t *testing.T) []groupedDispatchCase {
	t.Helper()
	log, works := pruningLedger(t)
	state, err := Replay(log)
	require.NoError(t, err)
	built, err := CheckpointCommit(state, testReconciler, 1)
	require.NoError(t, err)
	snapshot := checkpointStateOf(t, built)
	// Only what a later commit reads survives: z, its predecessor y (whose own predecessor x
	// is never read again), gated Work with its gate, and the Work awaiting its Issue comment.
	retained := []string{works["y"].WorkID, works["z"].WorkID, works["gated"].WorkID, works["backed"].WorkID}
	sort.Strings(retained)
	require.Equal(t, retained, sortedKeys(snapshot["works"].(object)))
	require.Equal(t, retained, sortedKeys(snapshot["work_creators"].(object)))
	require.Equal(t, []string{GateKey(issueResource("71"), "completed")}, sortedKeys(snapshot["observations"].(object)))
	require.Len(t, snapshot["claims"], 2)
	require.Len(t, snapshot["dispatches"], 2)

	// The restored predecessor feeds the successor's result_refs.
	parameters := testDispatchParameters(nil)
	granted, decision := testDispatchNext(t, []QueueCommit{built}, parameters)
	require.Len(t, decision.Dispatches, 1)
	grantedState, err := Replay(granted)
	require.NoError(t, err)
	assignment, err := AssignmentForDispatch(grantedState, decision.Dispatches[0])
	require.NoError(t, err)
	require.Equal(t, works["z"].WorkID, assignment.Claims[0].WorkID)
	require.Equal(t, works["y"].WorkID, assignment.Claims[0].ResultRefs[0].WorkID)

	// Once the projector records the terminal comment, the next checkpoint drops `backed`.
	comment := mustOps(t, IssueCommentOperation{Kind: "IssueComment", WorkID: works["backed"].WorkID, CommentID: "IC_9"})
	commented := appendTestCommit(t, []QueueCommit{built}, testProjector, "issue_link", IssueLinkParameters{Operations: comment}, comment)
	commentedState, err := Replay(commented)
	require.NoError(t, err)
	next, err := CheckpointCommit(commentedState, testReconciler, 1)
	require.NoError(t, err)
	require.Len(t, checkpointStateOf(t, next)["works"], 3)
	// Its window is this epoch's requests, the first checkpoint's own included, none inherited.
	epoch := []any{built.Request.ID, commented[1].Request.ID}
	sort.Slice(epoch, func(i, j int) bool { return epoch[i].(string) < epoch[j].(string) })
	require.Equal(t, epoch, checkpointStateOf(t, next)["requests"])

	full := genericCommits(t, built)[0]
	fullData, err := SerializeProjection(state)
	require.NoError(t, err)
	decoded, err := decodeStrict(fullData)
	require.NoError(t, err)
	folded := decoded.(object)
	checkpoint := full.(object)
	section := func(state object, name string) object { return state[name].(object) }
	tamper := func(label, code string, change func(state object)) groupedDispatchCase {
		mutated := deepCopy(checkpoint).(object)
		change(mutated["operations"].([]any)[0].(object)["state"].(object))
		return groupedDispatchCase{parityInput{Transactions: []any{mutated}, label: label}, code}
	}
	x, y := works["x"].WorkID, works["y"].WorkID
	requests := func(count int) []any {
		ids := make([]any, count)
		for index := range ids {
			ids[index] = fmt.Sprintf("request:%064x", index)
		}
		return ids
	}
	cases := []groupedDispatchCase{
		{parityInput{Transactions: genericCommits(t, log...), Checkpoint: &testReconciler, label: "checkpoint of the pruning ledger"}, ""},
		{parityInput{Transactions: []any{checkpoint}, Parameters: &parameters, RequestID: "request:pruned", label: "restored pruned checkpoint"}, ""},
		{parityInput{Transactions: genericCommits(t, commented...), Checkpoint: &testReconciler, label: "checkpoint after the terminal Issue comment"}, ""},
		tamper("extra terminal Work", "", func(state object) {
			section(state, "works")[x] = deepCopy(section(folded, "works")[x])
			section(state, "work_creators")[x] = deepCopy(section(folded, "work_creators")[x])
		}),
		tamper("missing predecessor of non-terminal Work", "checkpoint_invalid", func(state object) {
			delete(section(state, "works"), y)
			delete(section(state, "work_creators"), y)
			for id, claim := range section(state, "claims") {
				if claim.(object)["work_id"] == y {
					delete(section(state, "claims"), id)
					delete(section(state, "dispatches"), claim.(object)["dispatch_id"].(string))
				}
			}
		}),
		tamper("Claim naming a dropped Work", "checkpoint_invalid", func(state object) {
			for id, claim := range section(folded, "claims") {
				if dispatch := claim.(object)["dispatch_id"].(string); claim.(object)["work_id"] == x {
					section(state, "claims")[id] = deepCopy(claim)
					section(state, "dispatches")[dispatch] = deepCopy(section(folded, "dispatches")[dispatch])
				}
			}
		}),
		tamper("requests at the window", "", func(state object) { state["requests"] = requests(RetainedRequests) }),
		tamper("requests over the window", "checkpoint_invalid", func(state object) { state["requests"] = requests(RetainedRequests + 1) }),
	}

	windowLog, oldest, newest := windowLedger(t)
	windowState, err := Replay(windowLog)
	require.NoError(t, err)
	windowCheckpoint, err := CheckpointCommit(windowState, testReconciler, 1)
	require.NoError(t, err)
	window := checkpointStateOf(t, windowCheckpoint)["requests"].([]any)
	require.Len(t, window, RetainedRequests)
	require.NotContains(t, window, oldest.Request.ID)
	require.Contains(t, window, newest.Request.ID)
	restored, err := Replay([]QueueCommit{windowCheckpoint})
	require.NoError(t, err)
	resend := func(original QueueCommit) []any {
		return genericCommits(t, windowCheckpoint, BuildCommit(restored, original.Actor, original.Request, original.Operations, 1))
	}
	return append(cases,
		groupedDispatchCase{parityInput{Transactions: genericCommits(t, windowLog...), Checkpoint: &testReconciler, label: "checkpoint of a log past the request window"}, ""},
		groupedDispatchCase{parityInput{Transactions: resend(newest), label: "retry of a request inside the window"}, "request_reused"},
		groupedDispatchCase{parityInput{Transactions: resend(oldest), label: "request folded out of the window"}, ""},
	)
}
