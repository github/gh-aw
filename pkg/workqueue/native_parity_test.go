package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestNativeEnginesStrictEveryPrefixParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-engine test tooling requires Node; native production has no Node dependency")
	}
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical-prefix.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Commits []QueueCommit `json:"commits"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Commits) < 4 {
		t.Fatal("missing independent every-prefix fixture")
	}
	parameters := DispatchParameters{Pool: "default", MaxClaims: 16, MaxDispatches: 16, MaxBytes: 48 << 10}
	check := func(t *testing.T, commits []QueueCommit, at int64) {
		t.Helper()
		state, err := Replay(commits)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := Serialize(commits)
		if err != nil {
			t.Fatal(err)
		}
		next, err := PlanNext(state, "default", at)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := PlanDispatch(state, parameters, "parity-request", "parity-commit", at)
		if err != nil {
			t.Fatal(err)
		}
		input, err := canonicalValue(map[string]any{
			"transactions": commits, "pool": "default", "at": at,
			"parameters": parameters, "request_id": "parity-request", "commit_id": "parity-commit",
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, node, "../../actions/setup/js/work_queue_conformance_checks.cjs", "--replay")
		command.Stdin = bytes.NewReader(input)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("JavaScript parity adapter: %v\n%s", err, stderr.Bytes())
		}
		var actual struct {
			State     json.RawMessage `json:"state"`
			Canonical string          `json:"canonical"`
			Next      json.RawMessage `json:"next"`
			Decision  json.RawMessage `json:"decision"`
		}
		if err := json.Unmarshal(output, &actual); err != nil {
			t.Fatal(err)
		}
		if actual.Canonical != string(canonical) {
			t.Error("causal canonical ledger differs between native engines")
		}
		for _, pair := range []struct {
			name   string
			native any
			js     json.RawMessage
		}{
			{"projection", state, actual.State},
			{"next", next, actual.Next},
			{"decision", decision, actual.Decision},
		} {
			expected, err := canonicalValue(pair.native)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Canonical(pair.js)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(expected, got) {
				var native, js any
				_ = json.Unmarshal(expected, &native)
				_ = json.Unmarshal(got, &js)
				t.Errorf("%s strict parity mismatch: %s", pair.name, firstParityDifference(native, js, pair.name))
			}
		}
	}
	for index := range fixture.Commits {
		t.Run(fmt.Sprintf("prefix-%02d", index+1), func(t *testing.T) {
			check(t, fixture.Commits[:index+1], fixture.Commits[index].At)
		})
	}
	t.Run("physical-reorder-and-exact-duplicate", func(t *testing.T) {
		commits := slices.Clone(fixture.Commits)
		slices.Reverse(commits)
		commits = append(commits, fixture.Commits[3])
		check(t, commits, fixture.Commits[len(fixture.Commits)-1].At)
	})
	t.Run("bounded-observed-grant", func(t *testing.T) {
		commits, observation := observedDispatchFixture(t)
		actor := testActor("administrator")
		request, err := NewRequest("observed-parity", "dispatch_next", actor, parameters)
		if err != nil {
			t.Fatal(err)
		}
		next, _, _, err := buildCandidateWithObservations(commits, actor, request, 3000, []Observation{observation})
		if err != nil {
			t.Fatal(err)
		}
		check(t, next, 3000)
		commits = testOperations(t, commits, actor, "observe-parity", "observe", Op(observation))
		check(t, commits, 4000)
	})
}

func firstParityDifference(native, js any, path string) string {
	if reflect.DeepEqual(native, js) {
		return ""
	}
	if left, ok := native.(map[string]any); ok {
		if right, ok := js.(map[string]any); ok {
			keys := make([]string, 0, len(left)+len(right))
			for key := range left {
				keys = append(keys, key)
			}
			for key := range right {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range slices.Compact(keys) {
				l, lok := left[key]
				r, rok := right[key]
				if !lok || !rok {
					return fmt.Sprintf("%s.%s exists native=%t JS=%t", path, key, lok, rok)
				}
				if difference := firstParityDifference(l, r, path+"."+key); difference != "" {
					return difference
				}
			}
		}
	}
	if left, ok := native.([]any); ok {
		if right, ok := js.([]any); ok && len(left) == len(right) {
			for index := range left {
				if difference := firstParityDifference(left[index], right[index], fmt.Sprintf("%s[%d]", path, index)); difference != "" {
					return difference
				}
			}
		}
	}
	return fmt.Sprintf("%s native=%v JS=%v", path, native, js)
}
