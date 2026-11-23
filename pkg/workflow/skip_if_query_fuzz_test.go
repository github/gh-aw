//go:build !integration

package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// FuzzSkipIfQueryParser checks the skip-if search query parser in
// actions/setup/js/skip_if_query.cjs. Each input is checked both as a raw
// query and as a choice stream for the harness's qualifier grammar. The
// harness asserts the parser and backend-selection invariants; queries are
// only parsed, never sent to GitHub.
//
//	go test -run='^$' -fuzz=FuzzSkipIfQueryParser -fuzztime=30s ./pkg/workflow
func FuzzSkipIfQueryParser(f *testing.F) {
	node, err := exec.LookPath("node")
	if err != nil {
		f.Skip("Node.js is required for the skip-if query fuzz harness")
	}
	harness, err := filepath.Abs("../../actions/setup/js/fuzz_skip_if_query_harness.cjs")
	if err != nil {
		f.Fatal(err)
	}
	f.Add("is:issue is:open label:bug repo:octo/demo", []byte{0, 4, 13}, "bug")
	f.Add(`is:pr is:merged label:"needs triage" repo:octo/demo`, []byte{1, 6, 20}, "octo/demo")
	f.Add(`is:issue in:title "[daily-report]" repo:octo/demo`, []byte{33, 34}, `"`)
	f.Add(`label:"a b"c repo:octo/demo`, []byte{15, 16, 17}, "a,b")
	f.Add("label:a label:b assignee:octocat repo:octo/demo", []byte{25, 26, 27}, "@me")
	f.Add("", []byte{}, "")
	f.Fuzz(func(t *testing.T, query string, choices []byte, payload string) {
		if len(query) > 4096 || len(choices) > 256 || len(payload) > 1024 {
			t.Skip("Input too large")
		}
		// JSON encodes []byte as base64; use integers for the JS choice stream.
		ints := make([]int, len(choices))
		for i, value := range choices {
			ints[i] = int(value)
		}
		for _, request := range []map[string]any{
			{"query": query},
			{"bytes": ints, "payload": payload},
		} {
			runSkipIfQueryHarness(t, node, harness, request)
		}
	})
}

func runSkipIfQueryHarness(t *testing.T, node, harness string, request map[string]any) {
	t.Helper()
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, harness)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("skip-if query harness failed: %v\nrequest=%s\n%s", err, input, output)
	}
	var result struct {
		OK         bool     `json:"ok"`
		Query      string   `json:"query"`
		Violations []string `json:"violations"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("invalid harness response: %s (error: %v)", output, err)
	}
	if !result.OK {
		t.Fatalf("skip-if query invariants violated for %q: %v", result.Query, result.Violations)
	}
}
