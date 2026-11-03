package connectors

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventPredicateConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/event-predicates.json")
	require.NoError(t, err)
	checkEventPredicateConformance(t, data)
}

func checkEventPredicateConformance(t *testing.T, data []byte) {
	t.Helper()
	var cases []struct {
		Name    string
		Rule    EventPredicate
		Payload any
		Matched bool
		Known   bool
	}
	require.NoError(t, decodeJSON(data, &cases))
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			require.NoError(t, ValidateEventPredicate(test.Rule))
			matched, known := EvaluateEventPredicate(test.Rule, test.Payload)
			require.Equal(t, test.Matched, matched)
			require.Equal(t, test.Known, known)
		})
	}
	module, err := filepath.Abs("../../actions/setup/js/event_predicate.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const runtime = require(process.argv[1]);
const cases = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
for (const test of cases) {
  assert.equal(runtime.validEventPredicate(test.rule), true, test.name);
  assert.deepEqual(runtime.evaluateEventPredicate(test.rule,test.payload), {matched:test.matched,known:test.known},test.name);
}`, module)
	command.Stdin = strings.NewReader(string(data))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestEventPredicateEvaluationBudget(t *testing.T) {
	rule := EventPredicate{Path: "/match", Root: true, Op: "equals", Value: "yes"}
	for range 3 {
		child := rule
		rule = EventPredicate{Path: "/items", Root: true, Op: "any-item", Items: &child}
	}
	payload := map[string]any{"items": make([]any, eventArrayLimit), "match": "no"}
	cases := []map[string]any{
		{"name": "nested root arrays exhaust the shared budget", "rule": rule, "payload": payload, "matched": false, "known": false},
		{"name": "later alternative cannot override exhaustion", "rule": EventPredicate{Any: []EventPredicate{rule, {Path: "/match", Op: "equals", Value: "no"}}}, "payload": payload, "matched": false, "known": false},
		{"name": "later conjunction cannot override exhaustion", "rule": EventPredicate{All: []EventPredicate{rule, {Path: "/match", Op: "equals", Value: "yes"}}}, "payload": payload, "matched": false, "known": false},
	}
	data, err := json.Marshal(cases)
	require.NoError(t, err)
	checkEventPredicateConformance(t, data)

	remaining := 1
	leaf := EventPredicate{Path: "/match", Op: "equals", Value: "no"}
	matched, known := evaluateEventPredicate(leaf, payload, payload, &remaining)
	require.True(t, matched)
	require.True(t, known)
	require.Zero(t, remaining)
	matched, known = evaluateEventPredicate(leaf, payload, payload, &remaining)
	require.False(t, matched)
	require.False(t, known)
	require.Equal(t, -1, remaining)
}

func TestEventPredicateBoundsAndValidation(t *testing.T) {
	for _, raw := range []string{
		`{"path":"/a","op":"execute","value":"code"}`,
		`{"path":"/a","op":"equals"}`,
		`{"path":"/a","op":"equals","value":"a","other":"/b"}`,
		`{"all":[{"path":"/a","op":"null"}],"path":"/a"}`,
		`{"path":"/a","op":"set-added","other":"/b","separator":"::"}`,
		`{"path":"/a","op":"length","value":101}`,
		`{"path":"/a","op":"any-item"}`,
		`{"path":"/a","op":"changed","other":"/b","values":["bad"]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var rule EventPredicate
			require.NoError(t, Decode([]byte(raw), &rule))
			require.Error(t, ValidateEventPredicate(rule))
		})
	}

	leaf := EventPredicate{Path: "/a", Op: "null"}
	rule := EventPredicate{All: make([]EventPredicate, 65)}
	for index := range rule.All {
		rule.All[index] = leaf
	}
	require.Error(t, ValidateEventPredicate(rule))
	for range 7 {
		leaf = EventPredicate{All: []EventPredicate{leaf}}
	}
	require.Error(t, ValidateEventPredicate(leaf))
	var document any
	require.NoError(t, json.Unmarshal([]byte(`{"old":[],"new":[]}`), &document))
	document.(map[string]any)["new"] = make([]any, 101)
	matched, known := EvaluateEventPredicate(EventPredicate{Path: "/new", Other: "/old", Op: "set-added"}, document)
	require.False(t, matched)
	require.False(t, known)
}

func TestEventPrefixBudgetConformance(t *testing.T) {
	members := make([]string, eventArrayLimit)
	prefixes := make([]string, eventArrayLimit)
	for index := range eventArrayLimit {
		members[index] = "a" + strings.Repeat("x", index)
		prefixes[index] = "no" + strings.Repeat("x", index)
	}
	members[eventArrayLimit-1], prefixes[eventArrayLimit-1] = "z", "z"
	cases := []map[string]any{
		{
			"name":    "member-prefix comparison order shares the evaluation budget",
			"rule":    EventPredicate{Path: "/items", Op: "member-prefix", Values: prefixes},
			"payload": map[string]any{"items": members}, "matched": false, "known": false,
		},
		{
			"name":    "URL query semicolons are not silently reinterpreted",
			"rule":    EventPredicate{Path: "/url", Op: "url-id", Other: "/id", Value: "https://example.test/attachments/", Values: []string{"fileName"}},
			"payload": map[string]any{"id": "a", "url": "https://example.test/attachments/a?fileName=x;other=y"}, "matched": false, "known": false,
		},
		{
			"name":    "URL query encoded semicolon stays literal data",
			"rule":    EventPredicate{Path: "/url", Op: "url-id", Other: "/id", Value: "https://example.test/attachments/", Values: []string{"fileName"}},
			"payload": map[string]any{"id": "a", "url": "https://example.test/attachments/a?fileName=x%3Bother%3Dy"}, "matched": true, "known": true,
		},
	}
	slices.Reverse(members)
	cases = append(cases, map[string]any{
		"name":    "member input order cannot change budget exhaustion",
		"rule":    EventPredicate{Path: "/items", Op: "member-prefix", Values: prefixes},
		"payload": map[string]any{"items": members}, "matched": false, "known": false,
	}, map[string]any{
		"name":    "member-prefix match within the budget remains available",
		"rule":    EventPredicate{Path: "/items", Op: "member-prefix", Values: prefixes},
		"payload": map[string]any{"items": members[:50]}, "matched": true, "known": true,
	})
	data, err := json.Marshal(cases)
	require.NoError(t, err)
	checkEventPredicateConformance(t, data)
}
