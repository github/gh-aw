package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeCodecRejectionCodeParity(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ErrorCodes []struct {
			Name  string `json:"name"`
			Input string `json:"input"`
			Code  string `json:"code"`
		} `json:"error_codes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ErrorCodes) == 0 {
		t.Fatal("independent canonical fixture must specify rejection codes")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript codec parity tooling unavailable; native fixture checks still run")
		return
	}
	var input bytes.Buffer
	for _, test := range fixture.ErrorCodes {
		request, err := json.Marshal(map[string]any{"action": "canonical", "data": test.Input})
		if err != nil {
			t.Fatal(err)
		}
		input.Write(request)
		input.WriteByte('\n')
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "../../specs/work-queue/native_probe.cjs")
	command.Stdin = &input
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript codec parity: %v\n%s", err, stderr.String())
	}
	responses := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(responses) != len(fixture.ErrorCodes) {
		t.Fatalf("expected %d codec responses, received %d", len(fixture.ErrorCodes), len(responses))
	}
	for index, test := range fixture.ErrorCodes {
		t.Run(test.Name, func(t *testing.T) {
			_, nativeError := Canonical([]byte(test.Input))
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(responses[index]), &response); err != nil {
				t.Fatal(err)
			}
			prefix := test.Code + ":"
			if nativeError == nil || !strings.HasPrefix(nativeError.Error(), prefix) ||
				!strings.HasPrefix(response.Error, prefix) {
				t.Fatalf("expected %s in both engines: Go=%v JavaScript=%s", test.Code, nativeError, response.Error)
			}
		})
	}
}
