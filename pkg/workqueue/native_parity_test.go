package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
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
	label        string
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
	// checkpoint fail with the same literal code in both engines.
	rules := groupedDispatchCases(t, commits, checkpoint)
	for _, rule := range rules {
		inputs = append(inputs, rule.input)
	}
	t.Logf("differential cases: %d", len(inputs))
	expected := jsReplayCommands(t, inputs)
	for index, rule := range rules {
		want, err := canonicalGeneric(map[string]any{"error": rule.code})
		require.NoError(t, err)
		require.Equal(t, string(want), expected[len(inputs)-len(rules)+index], "js: %s", rule.input.label)
		require.Equal(t, string(want), goReplayCommand(t, rule.input), "go: %s", rule.input.label)
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
