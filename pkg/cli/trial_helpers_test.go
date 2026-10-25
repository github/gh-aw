//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDisplayWorkflowTrialOutput(t *testing.T) {
	result := WorkflowTrialResult{
		WorkflowName:     "test-workflow",
		RunID:            "123",
		SafeOutputs:      map[string]any{"result": "created"},
		Success:          false,
		SafeOutputErrors: []string{"rejected"},
	}
	stdout, stderr := captureOutput(t, func() error {
		displayWorkflowTrialOutput(result, true)
		return nil
	})
	var decoded WorkflowTrialResult
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RunID != result.RunID || decoded.Success || len(decoded.SafeOutputErrors) != 1 || stderr != "" {
		t.Fatalf("JSON mode changed result or stream: %+v, stderr=%q", decoded, stderr)
	}
	stdout, stderr = captureOutput(t, func() error {
		displayWorkflowTrialOutput(result, false)
		return nil
	})
	if !strings.Contains(stdout, `"result": "created"`) || strings.Contains(stdout, "workflow_name") || !strings.Contains(stderr, "=== End of Safe Outputs ===") {
		t.Fatalf("unexpected artifact display: stdout=%q, stderr=%q", stdout, stderr)
	}
}

func TestDisplayWorkflowTrialArtifactInfo(t *testing.T) {
	result := WorkflowTrialResult{
		WorkflowName:        "test-workflow",
		SafeOutputErrors:    []string{"unsafe\x00message"},
		AgenticRunInfo:      map[string]any{"engine": "copilot"},
		AdditionalArtifacts: map[string]any{"report": "data"},
	}
	stdout, stderr := captureOutput(t, func() error {
		displayWorkflowTrialArtifactInfo(result)
		return nil
	})
	if stdout != "" || strings.Contains(stderr, "\x00") {
		t.Fatalf("unexpected output stream or unsanitized error: stdout=%q, stderr=%q", stdout, stderr)
	}
	for _, expected := range []string{"1 Safe Output Message(s) Rejected", "unsafe", "message", "Agentic Run Information", "Additional Artifacts Available from test-workflow (1 files)"} {
		if !strings.Contains(stderr, expected) {
			t.Fatalf("missing %q in %q", expected, stderr)
		}
	}
}
