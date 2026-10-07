package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestIndependentWorkerChildEveryPrefixNativeParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("strict JavaScript worker-child parity requires Node test tooling")
	}
	data, err := os.ReadFile("../../actions/setup/js/work_queue_worker_child_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name      string `json:"name"`
			Valid     bool   `json:"valid"`
			Canonical string `json:"canonical"`
			ErrorCode string `json:"error_code"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 33 {
		t.Fatal("all independently authored child entitlement cases are required")
	}
	type prefixCase struct {
		name      string
		canonical string
		valid     bool
		code      string
	}
	prefixes := []prefixCase{}
	seen := map[string]bool{}
	for _, test := range fixture.Cases {
		lines := strings.Split(strings.TrimSuffix(test.Canonical, "\n"), "\n")
		var prefix strings.Builder
		for index, line := range lines {
			prefix.WriteString(line)
			prefix.WriteByte('\n')
			canonical := prefix.String()
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			prefixes = append(prefixes, prefixCase{
				name: fmt.Sprintf("%s/prefix-%d", test.Name, index+1), canonical: canonical,
				valid: index < len(lines)-1 || test.Valid,
				code:  test.ErrorCode,
			})
		}
	}
	inputs := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		inputs[index] = prefix.canonical
	}
	input, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const fs = require("node:fs");
const { parseTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog } =
  require("../../actions/setup/js/work_queue_replay.cjs");
const inputs = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(inputs.map(data => {
  try {
    const commits = parseTransactionLog(data);
    return { accepted: true, state: serializeProjection(replayTransactions(commits)),
      canonical: serializeTransactionLog(commits) };
  } catch (error) {
    if (typeof error.code !== "string") throw error;
    return { accepted: false, code: error.code };
  }
})));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript worker-child prefix parity: %v\n%s", err, stderr.String())
	}
	var results []struct {
		Accepted  bool            `json:"accepted"`
		State     json.RawMessage `json:"state"`
		Canonical string          `json:"canonical"`
		Code      string          `json:"code"`
	}
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(prefixes) {
		t.Fatalf("expected all %d unique causal prefixes, got %d", len(prefixes), len(results))
	}
	for index, prefix := range prefixes {
		t.Run(prefix.name, func(t *testing.T) {
			commits, err := Parse([]byte(prefix.canonical))
			var state Projection
			if err == nil {
				state, err = Replay(commits)
			}
			result := results[index]
			if (err == nil) != prefix.valid || result.Accepted != prefix.valid {
				t.Fatalf("independent expected acceptance=%t; Go error=%v, JavaScript code=%s",
					prefix.valid, err, result.Code)
			}
			if !prefix.valid {
				nativeCode, _, _ := strings.Cut(err.Error(), ":")
				if prefix.code == "" || nativeCode != prefix.code || result.Code != prefix.code {
					t.Fatalf("independent expected code=%s; Go code=%s, JavaScript code=%s",
						prefix.code, nativeCode, result.Code)
				}
				return
			}
			expected, err := canonicalValue(state)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := Canonical(result.State)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(expected, actual) {
				var native, js any
				if err := json.Unmarshal(expected, &native); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(actual, &js); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("full worker-child projection differs: %s", firstParityDifference(native, js, "projection"))
			}
			serialized, err := Serialize(commits)
			if err != nil || string(serialized) != prefix.canonical || result.Canonical != prefix.canonical {
				t.Fatalf("independent canonical worker-child prefix changed: %v", err)
			}
		})
	}
}
