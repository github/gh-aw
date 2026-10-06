package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func assertRecoveryReplayParity(t *testing.T, commits []QueueCommit, expectedError string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("cross-engine recovery test tooling unavailable; native boundary assertions still run")
		return
	}
	input, err := canonicalValue(map[string]any{"transactions": commits, "at": int64(4000)})
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
	if expectedError != "" {
		if err == nil || !strings.Contains(stderr.String(), expectedError) {
			t.Fatalf("JavaScript recovery parity must reject %s: err=%v stderr=%s", expectedError, err, stderr.String())
		}
		return
	}
	if err != nil {
		t.Fatalf("JavaScript recovery parity: %v\n%s", err, stderr.String())
	}
	var actual struct {
		State     json.RawMessage `json:"state"`
		Canonical string          `json:"canonical"`
	}
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatal(err)
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalValue(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(actual.State)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, got) {
		var native, js any
		_ = json.Unmarshal(expected, &native)
		_ = json.Unmarshal(got, &js)
		t.Fatalf("recovery full projection parity: %s", firstParityDifference(native, js, "projection"))
	}
	canonical, err := Serialize(commits)
	if err != nil || actual.Canonical != string(canonical) {
		t.Fatalf("recovery canonical ledger parity: %v", err)
	}
}
