package workqueue

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestIndependentCanonicalEveryPrefixFixture(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical-prefix.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Commits    []QueueCommit `json:"commits"`
		Canonical  string        `json:"canonical"`
		Assignment Assignment    `json:"assignment"`
		Prefixes   []struct {
			Tip                string `json:"tip"`
			PolicyEpoch        string `json:"policy_epoch"`
			Claims             int    `json:"claims"`
			NativeReservations int    `json:"native_reservations"`
			NextNode           string `json:"next_node"`
			Reason             string `json:"reason"`
			ClassV             string `json:"class_v"`
			KeyV               string `json:"key_v"`
			Works              map[string]struct {
				State   string `json:"state"`
				Barrier string `json:"barrier"`
			} `json:"works"`
		} `json:"prefixes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse([]byte(fixture.Canonical))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Serialize(fixture.Commits)
	if err != nil || string(canonical) != fixture.Canonical || !sameJSON(parsed, fixture.Commits) {
		t.Fatalf("independent canonical wire encoding differs: %v", err)
	}
	operationKinds := map[string]bool{}
	for index, expected := range fixture.Prefixes {
		state, err := Replay(fixture.Commits[:index+1])
		if err != nil {
			t.Fatalf("prefix %d: %v", index+1, err)
		}
		if state.Tip != expected.Tip || state.PolicyEpoch != expected.PolicyEpoch ||
			state.Stats.Claims != expected.Claims || state.Stats.Dispatches != expected.NativeReservations {
			t.Fatalf("prefix %d ownership/provenance/resources differ: %+v", index+1, state.Stats)
		}
		for _, work := range state.Works {
			wanted := expected.Works[work.NodeKey]
			if work.State != wanted.State || work.Barrier != wanted.Barrier {
				t.Fatalf("prefix %d node %s: %s/%s expected %s/%s",
					index+1, work.NodeKey, work.State, work.Barrier, wanted.State, wanted.Barrier)
			}
		}
		selection, err := PlanNext(state, "default", fixture.Commits[index].At)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if selection.WorkID != "" {
			got = state.Works[selection.WorkID].NodeKey
		}
		if got != expected.NextNode || selection.Reason != expected.Reason {
			t.Fatalf("prefix %d independent next answer: %s/%s expected %s/%s",
				index+1, got, selection.Reason, expected.NextNode, expected.Reason)
		}
		clock := state.Clocks["default"]
		classV, keyV := clock.Classes.V, clock.Keys[3].V
		if classV == "" {
			classV = "0"
		}
		if keyV == "" {
			keyV = "0"
		}
		if classV != expected.ClassV || keyV != expected.KeyV {
			t.Fatalf("prefix %d exact debt differs %s/%s expected %s/%s", index+1, classV, keyV, expected.ClassV, expected.KeyV)
		}
		for _, operation := range fixture.Commits[index].Operations {
			operationKinds[operationKind(operation)] = true
		}
	}
	if len(operationKinds) != 12 {
		t.Fatalf("shared fixture does not cover all twelve operations: %v", operationKinds)
	}
	admitted, err := Replay(fixture.Commits[:4])
	if err != nil || ValidateAssignment(admitted, fixture.Assignment) != nil {
		t.Fatalf("immutable assignment differs from independent fixture: %v", err)
	}
	reversed := slices.Clone(fixture.Commits)
	slices.Reverse(reversed)
	reversed = append(reversed, fixture.Commits[3])
	final, err := Replay(reversed)
	want, _ := Replay(fixture.Commits)
	if err != nil || !sameJSON(final, want) {
		t.Fatalf("physical reorder/exact duplicate changed causal projection: %v", err)
	}
}

func TestIndependentFixtureGenerationIsReproducible(t *testing.T) {
	command := exec.Command("python3", "../../specs/work-queue/fixtures/generate_prefix.py", "--check")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent fixture drift: %v\n%s", err, output)
	}
}

func TestSharedCanonicalEncodingFixtures(t *testing.T) {
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
		Invalid    []string `json:"invalid"`
		ErrorCodes []struct {
			Name  string `json:"name"`
			Input string `json:"input"`
			Code  string `json:"code"`
		} `json:"error_codes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Valid {
		t.Run(test.Name, func(t *testing.T) {
			actual, err := Canonical([]byte(test.Input))
			if err != nil || string(actual) != test.Expected {
				t.Fatalf("shared canonical encoding: %s expected %s err=%v", actual, test.Expected, err)
			}
		})
	}
	for _, input := range fixture.Invalid {
		if _, err := Canonical([]byte(input)); err == nil {
			t.Fatalf("shared invalid canonical input accepted: %s", input)
		}
	}
	for _, test := range fixture.ErrorCodes {
		t.Run(test.Name, func(t *testing.T) {
			if _, err := Canonical([]byte(test.Input)); err == nil || !strings.HasPrefix(err.Error(), test.Code+":") {
				t.Fatalf("expected canonical rejection code %s, received %v", test.Code, err)
			}
		})
	}
}

func TestSharedReasonValidationFixtures(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/reason-validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int    `json:"version"`
		Pattern string `json:"pattern"`
		Cases   []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
			Valid  bool   `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != Version || fixture.Pattern != reasonPattern.String() || len(fixture.Cases) == 0 {
		t.Fatal("shared reason contract is missing or inconsistent")
	}
	commits := testGenesis(t, nil)
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			operation := Op(map[string]any{
				"kind": "Control", "control": "admission_paused", "value": false, "reason": test.Reason,
			})
			actor := testActor("administrator")
			request, err := NewRequest("reason-fixture", "control", actor,
				OperationsParameters{Operations: []Operation{operation}})
			if err != nil {
				t.Fatal(err)
			}
			commit := QueueCommit{
				Version: Version, ID: "reason-commit", Previous: &commits[0].ID,
				Request: request, Actor: actor, PolicyEpoch: commits[0].PolicyEpoch, At: 2000,
				Operations: []Operation{operation},
			}
			for name, validate := range map[string]func() error{
				"closed wire": func() error { return ValidateCommit(commit) },
				"semantic":    func() error { return validateRequest(commit) },
			} {
				err := validate()
				if (err == nil) != test.Valid {
					t.Fatalf("%s reason validity disagrees with independent answer %t: %v", name, test.Valid, err)
				}
			}
		})
	}
}
