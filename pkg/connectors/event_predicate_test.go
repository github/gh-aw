package connectors

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventPredicateConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/event-predicates.json")
	require.NoError(t, err)
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
