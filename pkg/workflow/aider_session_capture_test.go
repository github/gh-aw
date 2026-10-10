//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Synthetic provider protocol coverage; these responses are not Actions evidence.
func runAiderSessionCapture(t *testing.T, scenario string) []map[string]any {
	t.Helper()
	return runAiderSessionCaptureWithMessage(t, scenario, nil)
}

func runAiderSessionCaptureWithMessage(t *testing.T, scenario string, providerMessage map[string]any) []map[string]any {
	t.Helper()
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "aider")
	codersDir := filepath.Join(packageDir, "coders")
	require.NoError(t, os.MkdirAll(codersDir, 0o700))
	for _, path := range []string{filepath.Join(packageDir, "__init__.py"), filepath.Join(codersDir, "__init__.py")} {
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "io.py"), []byte(`class InputOutput:
    def assistant_output(self, message, pretty=None):
        print("empty warning" if not message else "display: " + message)

    def tool_error(self, message="", strip=True):
        print(message)
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(codersDir, "base_coder.py"), []byte(`class Coder:
    def show_send_output(self, completion):
        if completion.choices:
            self.io.assistant_output("  DISPLAY REASONING + REPLY  ")
        if completion.choices and completion.choices[0].finish_reason == "length":
            raise ValueError("length limit")

    def calculate_and_show_tokens_and_cost(self, messages, completion=None):
        self.usage_report = "Tokens: rounded"
        self.total_cost = self.test_cost

def run_cmd(command, *args, **kwargs):
    if command == "exception":
        raise OSError("  command interrupted\n")
    return (1, "  failed\n") if command == "fail" else (0, "")
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "main.py"), []byte(`import json
import os
from types import SimpleNamespace as Obj
from .io import InputOutput
from .coders import base_coder

class Payload(Obj):
    def model_dump(self):
        return vars(self)

def main():
    io = InputOutput()
    scenario = os.environ["AIDER_TEST_SCENARIO"]
    coder = base_coder.Coder()
    coder.io = io
    coder.main_model = Obj(info={"input_cost_per_token": 0.01} if scenario != "missing" else {})
    coder.test_cost = 0 if scenario == "zero" else 0.012345
    if scenario == "error":
        io.tool_error(["  provider error\n", False, 0, None])
        raise RuntimeError("provider unavailable")
    if scenario == "empty":
        completion = Obj(choices=[], usage=None)
        coder.show_send_output(completion)
        return 0
    content = None if scenario == "null" else "" if scenario == "zero" else "  exact reply\n\n"
    reasoning = "" if scenario == "zero" else "  exact reasoning\n"
    message = Payload(content=content, reasoning_content=reasoning, refusal=None, tool_calls=None)
    reason = "stop"
    if scenario == "refusal":
        message.refusal = "  exact refusal\n"
    if scenario == "filter":
        reason = "content_filter"
    if scenario == "partial":
        reason = "length"
    if "AIDER_TEST_MESSAGE" in os.environ:
        message = Payload(**json.loads(os.environ["AIDER_TEST_MESSAGE"]))
    usage = None if scenario == "missing" else Payload(
        prompt_tokens=0 if scenario == "zero" else 17,
        completion_tokens=0 if scenario == "zero" else 3,
        total_tokens=0 if scenario == "zero" else 20,
        prompt_tokens_details={"cached_tokens": 0},
        completion_tokens_details={"reasoning_tokens": 0},
        vendor_zero=0, vendor_false=False,
    )
    if scenario == "anthropic":
        usage = Payload(prompt_tokens=17, completion_tokens=3, cache_read_input_tokens=0, cache_creation_input_tokens=0)
    if scenario == "invalid":
        usage = Payload(prompt_tokens=-1, completion_tokens=True, total_tokens=9007199254740992)
    for index in range(2 if scenario in ("multiple", "mixed", "overflow") else 1):
        response_usage = usage
        if scenario == "mixed" and index == 1:
            response_usage = None
        if scenario == "overflow":
            response_usage = Payload(prompt_tokens=9007199254740991, completion_tokens=0)
        completion = Obj(choices=[Obj(message=message, finish_reason=reason)], usage=response_usage, id="native-response", model="native-model", created=0)
        coder.show_send_output(completion)
        coder.calculate_and_show_tokens_and_cost([], completion)
    assert base_coder.run_cmd("ok") == (0, "")
    assert base_coder.run_cmd("fail") == (1, "  failed\n")
    if scenario == "command-error":
        base_coder.run_cmd("exception")
    return 0
`), 0o600))
	prompt := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(prompt, []byte("  exact prompt\n"), 0o600))
	execution := loadAiderSample(t).Behaviors.Execution
	cmd := exec.Command("python3", execution.Args...)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+dir, "GH_AW_PROMPT="+prompt, "AIDER_TEST_SCENARIO="+scenario)
	if providerMessage != nil {
		messageJSON, err := json.Marshal(providerMessage)
		require.NoError(t, err)
		cmd.Env = append(cmd.Env, "AIDER_TEST_MESSAGE="+string(messageJSON))
	}
	output, err := cmd.CombinedOutput()
	if scenario == "error" || scenario == "partial" || scenario == "command-error" {
		require.Error(t, err, "%s", output)
	} else {
		require.NoError(t, err, "%s", output)
	}
	assert.NotContains(t, string(output), "DISPLAY REASONING")
	var events []map[string]any
	for line := range strings.SplitSeq(string(output), "\n") {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil && event["type"] != nil {
			events = append(events, event)
		}
	}
	require.NotEmpty(t, events)
	return events
}

func aiderEventsOfType(events []map[string]any, eventType string) []map[string]any {
	var matched []map[string]any
	for _, event := range events {
		if event["type"] == eventType {
			matched = append(matched, event)
		}
	}
	return matched
}

func TestAiderSessionCapturePreservesProviderPayloads(t *testing.T) {
	events := runAiderSessionCapture(t, "normal")
	reasoning := aiderEventsOfType(events, "assistant.reasoning")
	messages := aiderEventsOfType(events, "assistant.message")
	require.Len(t, reasoning, 1)
	require.Len(t, messages, 1)
	assert.Equal(t, "  exact reasoning\n", reasoning[0]["data"].(map[string]any)["content"])
	data := messages[0]["data"].(map[string]any)
	assert.Equal(t, "  exact reply\n\n", data["content"])
	assert.Equal(t, "native-response", data["apiCallId"])
	assert.Equal(t, "native-model", data["model"])
	assert.Zero(t, data["created"])
	assert.Equal(t, false, data["usage"].(map[string]any)["vendor_false"])
	assert.Zero(t, data["usage"].(map[string]any)["vendor_zero"])
	result := aiderEventsOfType(events, "session.result")
	require.Len(t, result, 1)
	resultData := result[0]["data"].(map[string]any)
	assert.Equal(t, "completed", resultData["status"])
	assert.InDelta(t, 1, resultData["numTurns"], 0)
	assert.InDelta(t, 0.012345, resultData["totalCostUsd"], 0)
	assert.Equal(t, map[string]any{
		"input_tokens": float64(17), "output_tokens": float64(3), "total_tokens": float64(20),
		"cache_read_input_tokens": float64(0), "reasoning_output_tokens": float64(0),
		"input_tokens_include_cache": true,
	}, resultData["usage"])
	tools := aiderEventsOfType(events, "tool.execution_complete")
	require.Len(t, tools, 2)
	assert.Contains(t, tools[0]["data"].(map[string]any), "output")
	assert.Empty(t, tools[0]["data"].(map[string]any)["output"])
	assert.Equal(t, false, tools[1]["data"].(map[string]any)["success"])
	assert.Equal(t, "  failed\n", tools[1]["data"].(map[string]any)["output"])
}

func TestAiderSessionCaptureAccountingAvailability(t *testing.T) {
	for _, scenario := range []string{"zero", "missing", "empty", "multiple", "mixed", "anthropic", "invalid", "overflow", "null"} {
		t.Run(scenario, func(t *testing.T) {
			events := runAiderSessionCapture(t, scenario)
			results := aiderEventsOfType(events, "session.result")
			require.Len(t, results, 1)
			data := results[0]["data"].(map[string]any)
			switch scenario {
			case "null":
				assert.Empty(t, aiderEventsOfType(events, "assistant.refusal"))
				messages := aiderEventsOfType(events, "assistant.message")
				require.Len(t, messages, 1)
				assert.Contains(t, messages[0]["data"].(map[string]any), "content")
				assert.Nil(t, messages[0]["data"].(map[string]any)["content"])
			case "zero":
				assert.Empty(t, aiderEventsOfType(events, "assistant.refusal"))
				assert.Zero(t, data["totalCostUsd"])
				assert.Zero(t, data["usage"].(map[string]any)["input_tokens"])
				messages := aiderEventsOfType(events, "assistant.message")
				require.Len(t, messages, 1)
				assert.Contains(t, messages[0]["data"].(map[string]any), "content")
				assert.Empty(t, messages[0]["data"].(map[string]any)["content"])
			case "missing", "empty", "mixed":
				assert.NotContains(t, data, "usage")
				if scenario != "mixed" {
					assert.NotContains(t, data, "totalCostUsd")
				}
			case "multiple":
				assert.InDelta(t, 2, data["numTurns"], 0)
				assert.InDelta(t, 34, data["usage"].(map[string]any)["input_tokens"], 0)
				assert.InDelta(t, 0.012345, data["totalCostUsd"], 0, "cost is an observed session snapshot, not additive")
			case "anthropic":
				assert.Equal(t, false, data["usage"].(map[string]any)["input_tokens_include_cache"])
				assert.NotContains(t, data["usage"].(map[string]any), "total_tokens")
			case "invalid":
				usage, _ := data["usage"].(map[string]any)
				assert.NotContains(t, usage, "input_tokens")
				assert.NotContains(t, usage, "output_tokens")
				assert.NotContains(t, usage, "total_tokens")
			case "overflow":
				assert.NotContains(t, data["usage"].(map[string]any), "input_tokens")
				assert.Zero(t, data["usage"].(map[string]any)["output_tokens"])
			}
		})
	}
}

func TestAiderSessionCaptureRefusalsPartialsAndErrors(t *testing.T) {
	for _, scenario := range []string{"refusal", "filter", "partial", "error", "command-error"} {
		t.Run(scenario, func(t *testing.T) {
			events := runAiderSessionCapture(t, scenario)
			result := aiderEventsOfType(events, "session.result")[0]["data"].(map[string]any)
			switch scenario {
			case "command-error":
				tools := aiderEventsOfType(events, "tool.execution_complete")
				require.Len(t, tools, 3)
				data := tools[2]["data"].(map[string]any)
				assert.Equal(t, false, data["success"])
				assert.Equal(t, "  command interrupted\n", data["error"])
				assert.NotContains(t, data, "exitCode")
				assert.NotContains(t, data, "output")
			case "refusal", "filter":
				assert.Empty(t, aiderEventsOfType(events, "assistant.message"))
				refusals := aiderEventsOfType(events, "assistant.refusal")
				require.Len(t, refusals, 1)
				data := refusals[0]["data"].(map[string]any)
				if scenario == "refusal" {
					assert.Equal(t, "refusal", data["reason"])
					assert.Equal(t, "  exact refusal\n", data["content"])
				} else {
					assert.Equal(t, "content_filter", data["reason"])
					assert.Equal(t, "  exact reply\n\n", data["content"])
				}
			case "partial":
				assert.Equal(t, true, aiderEventsOfType(events, "assistant.message")[0]["data"].(map[string]any)["partial"])
				assert.Equal(t, true, aiderEventsOfType(events, "assistant.reasoning")[0]["data"].(map[string]any)["partial"])
				assert.Equal(t, "failed", result["status"])
				assert.Equal(t, "ValueError", result["sourceType"])
			case "error":
				errors := aiderEventsOfType(events, "session.error")
				require.Len(t, errors, 1)
				assert.Equal(t, []any{"  provider error\n", false, float64(0), nil}, errors[0]["data"].(map[string]any)["content"])
				assert.Equal(t, []any{[]any{"  provider error\n", false, float64(0), nil}, "provider unavailable"}, result["errors"])
				assert.Equal(t, "failed", result["status"])
				assert.Zero(t, result["numTurns"])
				assert.NotContains(t, result, "usage")
			}
		})
	}
}

func TestAiderSessionCaptureRefusalContentAvailability(t *testing.T) {
	for _, tc := range []struct {
		name         string
		scenario     string
		message      map[string]any
		reason       string
		content      any
		hasContent   bool
		hasReasoning bool
	}{
		{
			name: "refusal-null", scenario: "normal",
			message: map[string]any{"content": nil, "refusal": "  exact refusal\n", "reasoning_content": ""},
			reason:  "refusal", content: "  exact refusal\n", hasContent: true, hasReasoning: true,
		},
		{
			name: "refusal-with-content", scenario: "normal",
			message: map[string]any{"content": "  filtered answer\n", "refusal": "  exact refusal\n", "reasoning_content": "  exact reasoning\n", "vendor_false": false, "vendor_zero": 0, "policy": map[string]any{"category": nil, "explanation": "  exact policy\n"}},
			reason:  "refusal", content: "  exact refusal\n", hasContent: true, hasReasoning: true,
		},
		{
			name: "refusal-empty", scenario: "normal",
			message: map[string]any{"content": nil, "refusal": ""},
			reason:  "refusal", content: "", hasContent: true,
		},
		{
			name: "refusal-length-is-not-streaming", scenario: "partial",
			message: map[string]any{"content": "  incomplete answer\n", "refusal": "  exact refusal\n", "reasoning_content": "  incomplete reasoning\n"},
			reason:  "refusal", content: "  exact refusal\n", hasContent: true, hasReasoning: true,
		},
		{
			name: "filter-null", scenario: "filter",
			message: map[string]any{"content": nil, "refusal": nil},
			reason:  "content_filter", hasContent: true,
		},
		{
			name: "filter-content", scenario: "filter",
			message: map[string]any{"content": "  exact filtered text\n", "refusal": nil},
			reason:  "content_filter", content: "  exact filtered text\n", hasContent: true,
		},
		{
			name: "filter-empty", scenario: "filter",
			message: map[string]any{"content": ""},
			reason:  "content_filter", content: "", hasContent: true,
		},
		{
			name: "filter-false", scenario: "filter",
			message: map[string]any{"content": false},
			reason:  "content_filter", content: false, hasContent: true,
		},
		{
			name: "filter-zero", scenario: "filter",
			message: map[string]any{"content": 0},
			reason:  "content_filter", content: 0, hasContent: true,
		},
		{
			name: "filter-structured", scenario: "filter",
			message: map[string]any{"content": []any{"  exact filtered text\n", false, 0, nil}},
			reason:  "content_filter", content: []any{"  exact filtered text\n", false, 0, nil}, hasContent: true,
		},
		{
			name: "filter-absent", scenario: "filter",
			message: map[string]any{"refusal": nil},
			reason:  "content_filter",
		},
		{
			name: "filter-with-refusal", scenario: "filter",
			message: map[string]any{"content": "  filtered text\n", "refusal": "  exact refusal\n"},
			reason:  "content_filter", content: "  exact refusal\n", hasContent: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := runAiderSessionCaptureWithMessage(t, tc.scenario, tc.message)
			assert.Empty(t, aiderEventsOfType(events, "assistant.message"))
			refusals := aiderEventsOfType(events, "assistant.refusal")
			require.Len(t, refusals, 1)
			data := refusals[0]["data"].(map[string]any)
			assert.Equal(t, tc.reason, data["reason"])
			expectedJSON, err := json.Marshal(map[string]any{"message": tc.message, "content": tc.content})
			require.NoError(t, err)
			var expected map[string]any
			require.NoError(t, json.Unmarshal(expectedJSON, &expected))
			assert.Equal(t, expected["message"], data["message"])
			if tc.hasContent {
				assert.Contains(t, data, "content")
				assert.Equal(t, expected["content"], data["content"])
			} else {
				assert.NotContains(t, data, "content")
			}
			assert.NotContains(t, data, "partial", "non-streaming length limits are not streamed refusal fragments")
			assert.Equal(t, "native-response", data["apiCallId"])
			assert.Equal(t, "native-model", data["model"])
			assert.Zero(t, data["created"])
			usage := data["usage"].(map[string]any)
			assert.Equal(t, false, usage["vendor_false"])
			assert.Zero(t, usage["vendor_zero"])
			assert.InDelta(t, 17, usage["prompt_tokens"], 0)
			reasoning := aiderEventsOfType(events, "assistant.reasoning")
			if tc.hasReasoning {
				require.Len(t, reasoning, 1)
				reasoningData := reasoning[0]["data"].(map[string]any)
				assert.Equal(t, tc.message["reasoning_content"], reasoningData["content"])
				if tc.scenario == "partial" {
					assert.Equal(t, true, reasoningData["partial"])
				}
			} else {
				assert.Empty(t, reasoning)
			}
			results := aiderEventsOfType(events, "session.result")
			require.Len(t, results, 1)
			result := results[0]["data"].(map[string]any)
			assert.InDelta(t, 1, result["numTurns"], 0)
			assert.InDelta(t, 17, result["usage"].(map[string]any)["input_tokens"], 0)
			if tc.scenario == "partial" {
				assert.Equal(t, "failed", result["status"])
				assert.Equal(t, "ValueError", result["sourceType"])
			} else {
				assert.Equal(t, "completed", result["status"], "a refusal is not a process failure")
				assert.InDelta(t, 0.012345, result["totalCostUsd"], 0)
			}
		})
	}
}
