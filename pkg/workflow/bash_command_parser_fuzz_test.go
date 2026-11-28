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

// FuzzBashCommandParserModel generates shell programs from a grammar with an
// independent executable-name oracle, then checks the Copilot SDK parser and
// permission handler. Shell programs are only parsed, never executed.
//
//	go test -run='^$' -fuzz=FuzzBashCommandParserModel -fuzztime=30s ./pkg/workflow
func FuzzBashCommandParserModel(f *testing.F) {
	node, err := exec.LookPath("node")
	if err != nil {
		f.Skip("Node.js is required for the Bash parser fuzz harness")
	}
	harness, err := filepath.Abs("../../actions/setup/js/fuzz_bash_command_parser_harness.cjs")
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte{}, "")
	f.Add([]byte{1}, "' \" # ; && || | $() \r \\\n")
	f.Add([]byte{8, 5, 3, 2, 3, 6, 5, 4, 7, 1}, "# comments must not conceal python3")
	f.Add(bytes.Repeat([]byte{255}, 128), "\x00\r\n\u00a0")
	for seed := range byte(16) {
		input := make([]byte, 128)
		for i := range input {
			input[i] = seed + byte(i*17)
		}
		f.Add(input, "quoted ' \" # ; | & \r text")
	}
	f.Fuzz(func(t *testing.T, input []byte, payload string) {
		if len(input) > 4096 || len(payload) > 4096 {
			t.Skip("Input too large")
		}
		// JSON encodes []byte as base64; use integers for the JS choice stream.
		choices := make([]int, len(input))
		for i, value := range input {
			choices[i] = int(value)
		}
		request, err := json.Marshal(map[string]any{
			"mode": "model", "bytes": choices, "commandText": payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, node, harness)
		cmd.Stdin = bytes.NewReader(request)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("shell model failed: %v\ninput=%x payload=%q\n%s", err, input, payload, output)
		}
		var result struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(output, &result); err != nil || !result.OK {
			t.Fatalf("invalid harness response: %s (error: %v)", output, err)
		}
	})
}
