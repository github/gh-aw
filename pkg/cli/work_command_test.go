package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkCommandEndToEndWithoutCheckout(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "queue.git")
	if output, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	payload := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(payload, []byte(`{"task":"review"}`), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]any {
		t.Helper()
		command := NewWorkCommand()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
		if err := command.Execute(); err != nil {
			t.Fatalf("work %v: %v (%s)", args, err, output.String())
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatalf("invalid JSON output %q: %v", output.String(), err)
		}
		return result
	}
	submitted := run("submit-work", "--file", payload)
	workID := submitted["work_id"].(string)
	if !submitted["created"].(bool) || len(workID) != 64 {
		t.Fatalf("unexpected submit: %v", submitted)
	}
	if run("submit-work", "--file", payload)["created"] != false {
		t.Fatal("duplicate submission was not idempotent")
	}
	claimed := run("claim", "--work-id", workID, "--run-id", "run-1")
	claimID := claimed["claim_id"].(string)
	if claimID == "" {
		t.Fatal("claim ID missing")
	}
	run("finish", "--claim-id", claimID, "--attempt-id", "attempt-1")
	state := run("replay")
	works := state["works"].([]any)
	if works[0].(map[string]any)["state"] != "completed" {
		t.Fatalf("completion did not persist: %v", state)
	}
	if run("stats")["completed"] != float64(1) {
		t.Fatal("stats did not reflect completion")
	}
	run("compact")
	if run("compact")["changed"] != false {
		t.Fatal("second compaction should not change the canonical log")
	}
	command := NewWorkCommand()
	command.SetArgs([]string{"--repo", remote, "cancel-work", "--work-id", workID})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("terminal work accepted cancellation: %v", err)
	}
}
