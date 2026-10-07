package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIndependentGraphIDUsesCanonicalPayloadNotNodeEncoding(t *testing.T) {
	const graphID = "43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777"
	for _, payload := range []string{`{"a":1,"b":2}`, `{"b": 2, "a": 1}`, `{"\u0061":1,"b":2}`} {
		id, err := IndependentGraphID([]byte(payload))
		if err != nil || id != graphID {
			t.Fatalf("canonical independent graph: %s %v", id, err)
		}
		if workID := NodeID(id, "root"); workID != "e41927adc7ede48a4d9ebae45074bbfd55f7714af750a3fe5b3734bc6c1ed62e" {
			t.Fatalf("independent root differs from literal expected identity: %s", workID)
		}
		if NodeID(id, "distinct") == NodeID(id, "root") || NodeID("distinct", "root") == NodeID(id, "root") {
			t.Fatal("explicit same-payload distinct nodes lost their requested identity")
		}
	}
	if _, err := IndependentGraphID([]byte(`{"a":1.5}`)); err == nil {
		t.Fatal("graph hashing weakened the canonical numeric domain")
	}
}

func TestTypedNumericCodecRejectionCodeParity(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string `json:"name"`
			Input string `json:"input"`
			Code  string `json:"code"`
		} `json:"typed_number_rejections"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("independent typed-number rejection fixture is empty")
	}
	for _, test := range fixture.Cases {
		var value any
		if err := json.Unmarshal([]byte(test.Input), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := canonicalValue(value); err == nil || !strings.HasPrefix(err.Error(), test.Code+":") {
			t.Fatalf("Go %s expected %s without numeric coercion: %v", test.Name, test.Code, err)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript typed-number parity tooling unavailable; native rejection checks passed")
		return
	}
	const script = `
const fs = require("node:fs");
const { canonical } = require("../../actions/setup/js/work_queue_codec.cjs");
const cases = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(cases.map(test => {
  try { return { canonical: canonical(JSON.parse(test.input)) }; }
  catch (error) {
    if (typeof error.code !== "string") throw error;
    return { code: error.code };
  }
})));
`
	input, err := json.Marshal(fixture.Cases)
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
		t.Fatalf("JavaScript typed-number parity: %v\n%s", err, stderr.String())
	}
	var results []struct {
		Code      string `json:"code"`
		Canonical string `json:"canonical"`
	}
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(fixture.Cases) {
		t.Fatalf("expected %d numeric cases, got %d", len(fixture.Cases), len(results))
	}
	for index, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if results[index].Code != test.Code || results[index].Canonical != "" {
				t.Fatalf("JavaScript must reject %s without coercion: %+v", test.Code, results[index])
			}
		})
	}
}

func TestAcceptedPayloadValuesSurviveAdmissionReplayAndAssignment(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Valid []struct {
			Name     string `json:"name"`
			Input    string `json:"input"`
			Expected string `json:"expected"`
		} `json:"valid"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var input, expected string
	for _, test := range fixture.Valid {
		if test.Name == "accepted nested payload data is preserved without numeric or Unicode coercion" {
			input, expected = test.Input, test.Expected
		}
	}
	if input == "" || expected == "" {
		t.Fatal("independent accepted-payload fixture is missing")
	}
	decode := func(data string) any {
		t.Helper()
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(input), decode(expected)) {
		t.Fatal("independent expected canonical payload changes accepted fields or decoded values")
	}
	commits := testGenesis(t, nil)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	work, err := NewWork([]byte(input), "opaque-payload", "preserved", "default", *state.Policy, 1000)
	if err != nil || string(work.Payload) != expected {
		t.Fatalf("payload ingestion did not preserve independently specified values: %v", err)
	}
	commits = testSubmit(t, commits, "opaque-payload-submit", work)
	commits, decision := testGrant(t, commits, "opaque-payload-grant", 1, 1)
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if string(state.Works[work.WorkID].Payload) != expected ||
		len(decision.Assignments) != 1 || len(decision.Assignments[0].Claims) != 1 ||
		string(decision.Assignments[0].Claims[0].Work) != expected {
		t.Fatal("replay or assignment discarded fields, coerced quantities or normalized Unicode")
	}
	assertRecoveryReplayParity(t, commits, "")
}
