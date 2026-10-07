package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestNativeRunTitleCorrelationParity(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	dispatchID := assignment.DispatchID
	cases := []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
		Valid bool   `json:"valid"`
	}{
		{Name: "exact", Title: dispatchID, Valid: true},
		{Name: "diagnostic-prefix-suffix", Title: "custom worker " + dispatchID + " diagnostic suffix", Valid: true},
		{Name: "parentheses", Title: "custom worker (" + dispatchID + ") diagnostic suffix", Valid: true},
		{Name: "brackets", Title: "[" + dispatchID + "]", Valid: true},
		{Name: "tabs", Title: "worker\t" + dispatchID + "\tdiagnostic", Valid: true},
		{Name: "number-suffix", Title: "worker " + dispatchID + "0"},
		{Name: "underscore-suffix", Title: "worker " + dispatchID + "_foreign"},
		{Name: "underscore-prefix", Title: "worker prefix_" + dispatchID},
		{Name: "letter-prefix", Title: "worker prefix" + dispatchID},
		{Name: "letter-suffix", Title: "worker " + dispatchID + "foreign"},
		{Name: "embedded-parentheses", Title: "(" + dispatchID + "0)"},
		{Name: "empty", Title: ""},
		{Name: "unrelated", Title: "worker another_dispatch"},
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			mock.nativeRun.DisplayTitle = test.Title
			_, err := branch.InspectEvidence(context.Background(), dispatchID)
			if (err == nil) != test.Valid {
				t.Fatalf("authenticated native API title validation: valid=%t, error=%v", test.Valid, err)
			}
		})
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-engine test tooling requires Node; native production has no Node dependency")
	}
	input, err := canonicalValue(map[string]any{
		"run": mock.nativeRun,
		"expected": map[string]any{
			"dispatch_id": dispatchID, "run_id": "202", "repository": testRepository,
			"workflow": mock.nativeRun.Path, "ref": mock.nativeRun.HeadSHA, "principal_id": testPrincipal,
		},
		"cases": cases,
	})
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const fs = require("node:fs");
const { validateNativeRun } = require("./actions/setup/js/work_queue_native.cjs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
const accepted = input.cases.map(test => {
  try {
    validateNativeRun({ ...input.run, display_title: test.title }, input.expected);
    return true;
  } catch (error) {
    if (error.message !== "run_correlation_mismatch") throw error;
    return false;
  }
});
console.log(JSON.stringify(accepted));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Dir = "../.."
	command.Stdin = bytes.NewReader(input)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("actual JS native API title validation: %v\n%s", err, diagnostics.String())
	}
	var accepted []bool
	if err := json.Unmarshal(output, &accepted); err != nil {
		t.Fatal(err)
	}
	if len(accepted) != len(cases) {
		t.Fatalf("JS native validation returned %d cases, want %d", len(accepted), len(cases))
	}
	for index, test := range cases {
		if accepted[index] != test.Valid {
			t.Errorf("JS native validation of %s: valid=%t, want %t", test.Name, accepted[index], test.Valid)
		}
	}
}
