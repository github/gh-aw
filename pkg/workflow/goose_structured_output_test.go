//go:build !integration && !windows

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGooseReleaseNativeStructuredOutput(t *testing.T) {
	binary := os.Getenv("GH_AW_TEST_GOOSE_BINARY")
	if binary == "" {
		t.Skip("set GH_AW_TEST_GOOSE_BINARY to a verified Goose v1.53.0 binary")
	}
	dir := t.TempDir()
	var inferenceCalls atomic.Int32
	var alwaysInvalid atomic.Bool
	var expectedParameters, firstArguments, correctedArguments atomic.Value
	const baselineSchema = `{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`
	expectedParameters.Store(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`)
	firstArguments.Store(`{"summary":42}`)
	correctedArguments.Store(`{"summary":"Validated native result"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"object": "list", "data": []any{map[string]any{"id": "test-model", "object": "model", "owned_by": "test"}},
			}))
			return
		}
		if !assert.Equal(t, "/chat/completions", r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Stream   bool             `json:"stream"`
			Tools    []map[string]any `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			http.Error(w, "Invalid test inference request", http.StatusBadRequest)
			return
		}
		found := false
		for _, tool := range request.Tools {
			function, ok := tool["function"].(map[string]any)
			if !ok || function["name"] != "recipe__final_output" {
				continue
			}
			found = true
			schema, err := json.Marshal(function["parameters"])
			if !assert.NoError(t, err) {
				http.Error(w, "Invalid test tool schema", http.StatusInternalServerError)
				return
			}
			assert.JSONEq(t, expectedParameters.Load().(string), string(schema))
		}
		if !assert.True(t, found, "the real CLI must advertise its native final-output schema") {
			http.Error(w, "Missing native final-output tool", http.StatusBadRequest)
			return
		}
		turn := inferenceCalls.Add(1)
		arguments := firstArguments.Load().(string)
		if turn > 1 && !alwaysInvalid.Load() {
			arguments = correctedArguments.Load().(string)
		}
		if turn > 1 {
			messages, err := json.Marshal(request.Messages)
			if !assert.NoError(t, err) {
				http.Error(w, "Invalid test message history", http.StatusInternalServerError)
				return
			}
			assert.Contains(t, string(messages), "Validation failed", "native schema validation must reject the first model output")
		}
		message := map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"index": 0, "id": fmt.Sprintf("native-call-%d", turn), "type": "function",
				"function": map[string]string{"name": "recipe__final_output", "arguments": arguments}},
		}}
		result := map[string]any{"id": fmt.Sprintf("completion-%d", turn), "object": "chat.completion", "model": "test-model",
			"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "tool_calls"}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			result["object"] = "chat.completion.chunk"
			result["choices"] = []any{map[string]any{"index": 0, "delta": message, "finish_reason": "tool_calls"}}
			data, err := json.Marshal(result)
			if !assert.NoError(t, err) {
				http.Error(w, "Invalid test inference response", http.StatusInternalServerError)
				return
			}
			_, err = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			assert.NoError(t, err)
		} else {
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(result))
		}
	}))
	defer server.Close()
	harness := filepath.Join(dir, "goose_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(loadGooseSample(t).Behaviors.HarnessScript), 0o600))
	for _, name := range []string{"goose_structured_output.cjs", "process_runner.cjs"} {
		content, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
	}
	sharedHelper, err := filepath.Abs("../../actions/setup/js/structured_output.cjs")
	require.NoError(t, err)
	encodedHelper, err := json.Marshal(sharedHelper)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "structured_output.cjs"), []byte("module.exports = require("+string(encodedHelper)+");"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte("module.exports = {};"), 0o600))
	prompt := filepath.Join(dir, "prompt.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("Return a summary. Treat {{ literal }} and {% endraw %} as literal text."), 0o600))
	config := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0o600))
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(baselineSchema), 0o600))
	outputPath := filepath.Join(dir, "structured-output.json")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", harness, binary)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION=1.53.0",
		"GH_AW_MCP_CONFIG="+config, "GH_AW_PROMPT="+prompt,
		"GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE="+schema, "GH_AW_STRUCTURED_OUTPUT_FILE="+outputPath,
		"GH_AW_STRUCTURED_OUTPUT_SCHEMA="+baselineSchema,
		"GOOSE_MODEL=openai/test-model", "GOOSE_MODE=auto",
		"GOOSE_DISABLE_SESSION_NAMING=true", "GH_AW_MAX_TURNS=3",
		"OPENAI_API_KEY=test-only", "AWF_REFLECT_ENABLED=0",
		"OPENAI_HOST="+server.URL, "OPENAI_BASE_PATH=chat/completions")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Equal(t, int32(2), inferenceCalls.Load(), "invalid native output must be retried before completing")
	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.JSONEq(t, `{"summary":"Validated native result"}`, string(content))
	assert.Contains(t, string(output), `"type":"complete"`)
	alwaysInvalid.Store(true)
	inferenceCalls.Store(0)
	failed := exec.CommandContext(ctx, "node", harness, binary)
	failed.Dir = cmd.Dir
	failed.Env = cmd.Env
	output, err = failed.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "Goose native structured output failed after one correction attempt")
	assert.Equal(t, int32(2), inferenceCalls.Load(), "native schema mismatch allows exactly one correction inference")
	_, err = os.Stat(outputPath)
	assert.True(t, os.IsNotExist(err), "exhausted native corrections must not publish a response")

	for _, tc := range []struct {
		name, schema, invalid, valid string
	}{
		{
			name:    "optional properties and open objects",
			schema:  `{"type":"object","properties":{"summary":{"type":"string"},"optional":{"type":"string"}}}`,
			invalid: `{"summary":42}`, valid: `{"extra":true}`,
		},
		{
			name:    "default draft-07 tuple semantics",
			schema:  `{"type":"object","properties":{"summary":{"type":"string"},"pair":{"type":"array","items":[{"type":"string"},{"type":"number"}],"additionalItems":false}},"required":["summary","pair"],"additionalProperties":false}`,
			invalid: `{"summary":42}`, valid: `{"summary":"tuple","pair":["left",2]}`,
		},
		{
			name:    "draft-2020-12 local references",
			schema:  `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","$defs":{"summary":{"type":"string"}},"properties":{"summary":{"$ref":"#/$defs/summary"}},"required":["summary"],"additionalProperties":false}`,
			invalid: `{"summary":42}`, valid: `{"summary":"reference"}`,
		},
		{
			name:    "draft-07 native format rejection",
			schema:  `{"type":"object","properties":{"summary":{"type":"string","format":"email"}},"required":["summary"],"additionalProperties":false}`,
			invalid: `{"summary":"not an email"}`, valid: `{"summary":"valid@example.com"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var expected map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.schema), &expected))
			if _, ok := expected["$schema"]; !ok {
				expected["$schema"] = "http://json-schema.org/draft-07/schema#"
			}
			// Goose's OpenAI adapter inserts an empty required list without
			// making any optional property required.
			if _, ok := expected["required"]; !ok {
				expected["required"] = []any{}
			}
			encoded, err := json.Marshal(expected)
			require.NoError(t, err)
			expectedParameters.Store(string(encoded))
			firstArguments.Store(tc.invalid)
			correctedArguments.Store(tc.valid)
			alwaysInvalid.Store(false)
			inferenceCalls.Store(0)
			require.NoError(t, os.WriteFile(schema, []byte(tc.schema), 0o600))
			run := exec.CommandContext(ctx, "node", harness, binary)
			run.Dir, run.Env = cmd.Dir, cmd.Env
			output, err := run.CombinedOutput()
			require.NoError(t, err, "%s", output)
			assert.Equal(t, int32(2), inferenceCalls.Load(), "native validator must reject invalid output before its one correction")
			content, err := os.ReadFile(outputPath)
			require.NoError(t, err)
			assert.JSONEq(t, tc.valid, string(content))
		})
	}
	t.Run("unsupported modern formats fail before inference", func(t *testing.T) {
		inferenceCalls.Store(0)
		require.NoError(t, os.WriteFile(schema, []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"summary":{"type":"string","format":"email"}}}`), 0o600))
		run := exec.CommandContext(ctx, "node", harness, binary)
		run.Dir, run.Env = cmd.Dir, cmd.Env
		output, err := run.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(output), "cannot natively enforce draft-2020-12 format constraints")
		assert.Zero(t, inferenceCalls.Load(), "unsupported native schema subset must fail before the provider is called")
		_, err = os.Stat(outputPath)
		assert.True(t, os.IsNotExist(err), "unsupported native schema must not retain previous output")
	})
}

func TestGooseStructuredOutputExecutionPaths(t *testing.T) {
	def := loadGooseSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, engine.GetCapabilities().StructuredOutput)
	data := &WorkflowData{
		Name: "Goose", Model: "copilot/test-model",
		EngineConfig: &EngineConfig{ID: "goose", Version: def.Version},
	}
	ordinary := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	assert.NotContains(t, strings.Join(ordinary[len(ordinary)-1], "\n"), "GH_AW_STRUCTURED_OUTPUT")
	data.StructuredOutput = &StructuredOutputConfig{Schema: map[string]any{"type": "object"}}
	structured := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	execution := strings.Join(structured[len(structured)-1], "\n")
	assert.Contains(t, execution, "GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE: "+StructuredOutputSchemaPath)
	assert.NotContains(t, execution, "GH_AW_STRUCTURED_OUTPUT_SCHEMA:")
	assert.Contains(t, execution, "GH_AW_STRUCTURED_OUTPUT_FILE: "+StructuredOutputFilePath)
}

func TestGooseStructuredOutputRejectsExecutionOverrides(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *EngineConfig
		edit   func(*EngineDefinition)
		error  string
	}{
		{name: "default", config: &EngineConfig{ID: "goose"}},
		{name: "explicit verified version", config: &EngineConfig{ID: "goose", Version: "1.53.0"}},
		{name: "custom command", config: &EngineConfig{ID: "goose", Command: "fake-goose"}, error: "remove engine.command"},
		{name: "custom arguments", config: &EngineConfig{ID: "goose", Args: []string{"--help"}}, error: "remove engine.args"},
		{name: "custom harness", config: &EngineConfig{ID: "goose", HarnessScript: "fake.cjs"}, error: "remove engine.harness"},
		{name: "custom driver", config: &EngineConfig{ID: "goose", Driver: "fake.cjs"}, error: "remove engine.harness"},
		{name: "inline driver", config: &EngineConfig{ID: "goose", InlineDriver: &InlineEngineDriver{Runtime: "javascript", Source: "process.exit(0)"}}, error: "remove engine.harness"},
		{name: "unverified version", config: &EngineConfig{ID: "goose", Version: "1.52.0"}, error: "requires verified version 1.53.0"},
		{name: "dynamic version", config: &EngineConfig{ID: "goose", Version: "${{ vars.VERSION }}"}, error: "requires verified version 1.53.0"},
		{name: "overridden definition version", edit: func(def *EngineDefinition) { def.Version = "1.52.0" }, error: "requires verified version 1.53.0"},
		{name: "overridden behavior command", edit: func(def *EngineDefinition) { def.Behaviors.Execution.CommandName = "fake-goose" }, error: "native execution behavior"},
		{name: "overridden behavior arguments", edit: func(def *EngineDefinition) { def.Behaviors.Execution.Args = []string{"--help"} }, error: "native execution behavior"},
		{name: "overridden behavior harness", edit: func(def *EngineDefinition) { def.Behaviors.HarnessScript = "process.exit(0)" }, error: "unmodified native harness"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := loadGooseSample(t)
			if tc.edit != nil {
				tc.edit(def)
			}
			engine, err := NewBehaviorDefinedEngine(def)
			require.NoError(t, err)
			frontmatter := map[string]any{"structured-output": map[string]any{"schema": map[string]any{"type": "object"}}}
			_, err = parseStructuredOutput(frontmatter, "workflow.md", engine, tc.config)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
			_, err = parseStructuredOutput(map[string]any{}, "workflow.md", engine, tc.config)
			require.NoError(t, err, "ordinary Goose workflows retain existing override behavior")
		})
	}
}

func TestGooseStructuredOutputImportOverrideGuard(t *testing.T) {
	dir := t.TempDir()
	source, err := os.ReadFile("../../.github/workflows/shared/goose.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "goose.md"), source, 0o600))
	for _, tc := range []struct {
		name, override, error string
	}{
		{name: "default"},
		{name: "explicit verified version", override: "  version: '1.53.0'\n"},
		{name: "custom command", override: "  command: fake-goose\n", error: "remove engine.command"},
		{name: "custom arguments", override: "  args: [--help]\n", error: "remove engine.args"},
		{name: "custom harness", override: "  harness: fake.cjs\n", error: "remove engine.harness"},
		{name: "custom harness mapping", override: "  harness:\n    use: fake.cjs\n", error: "remove engine.harness"},
		{name: "unverified version", override: "  version: '1.52.0'\n", error: "requires verified version 1.53.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(dir, "workflow.md")
			content := "---\non: workflow_dispatch\nstrict: false\nimports:\n  - goose.md\nengine:\n  id: goose\n" + tc.override + "model: copilot/auto\nstructured-output:\n  schema:\n    type: object\n---\nReturn an object.\n"
			require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			_, err := compiler.ParseWorkflowFile(file)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
		})
	}
}

func TestGooseHarnessNativeStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	def := loadGooseSample(t)
	harness := filepath.Join(dir, "goose_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(def.Behaviors.HarnessScript), 0o600))
	for _, name := range []string{"goose_structured_output.cjs", "process_runner.cjs"} {
		content, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
	}
	sharedHelper, err := filepath.Abs("../../actions/setup/js/structured_output.cjs")
	require.NoError(t, err)
	encodedHelper, err := json.Marshal(sharedHelper)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "structured_output.cjs"), []byte("module.exports = require("+string(encodedHelper)+");"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte("module.exports = {};"), 0o600))
	prompt := filepath.Join(dir, "prompt.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("Complete the task. Preserve literal {{ user }} and {% endraw %}."), 0o600))
	config := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0o600))
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`), 0o600))
	outputPath := filepath.Join(dir, "structured-output.json")
	capture := filepath.Join(dir, "capture.json")
	binary := filepath.Join(dir, "test-goose")
	fixture := `#!/usr/bin/env node
const fs = require("fs");
if (process.argv.includes("--version")) { console.log("1.53.0"); process.exit(0); }
const args = process.argv.slice(2);
const recipePath = args[args.indexOf("--recipe") + 1];
fs.writeFileSync(process.env.TEST_CAPTURE, JSON.stringify({
  args,
  recipe: JSON.parse(fs.readFileSync(recipePath, "utf8")),
  mode: fs.statSync(recipePath).mode & 0o777
}));
const value = process.env.TEST_CASE === "invalid" ? {summary: 42} : {summary: "Native result"};
const emit = event => console.log(JSON.stringify(event));
if (process.env.TEST_CASE !== "plain-text") {
  emit({type: "message", message: {role: "assistant", content: [
    {type: "toolRequest", id: "native", toolCall: {status: "success", value: {name: "recipe__final_output", arguments: value}}}
  ]}});
  emit({type: "message", message: {role: "user", content: [
    {type: "toolResponse", id: "native", toolResult: {status: "success", value: {content: [{type: "text", text: "Final output successfully collected."}]}}}
  ]}});
}
emit({type: "message", message: {role: "assistant", content: [{type: "text", text: JSON.stringify(value)}]}});
if (process.env.TEST_CASE !== "incomplete") emit({type: "complete"});
process.exit(process.env.TEST_CASE === "nonzero" ? 7 : 0);
`
	require.NoError(t, os.WriteFile(binary, []byte(fixture), 0o700))
	for _, test := range []struct {
		name      string
		wantError string
	}{
		{name: "native"},
		{name: "plain-text", wantError: "Goose did not complete a native schema-validated final output"},
		{name: "invalid", wantError: "Structured output schema violation"},
		{name: "incomplete", wantError: "Goose did not complete a native schema-validated final output"},
		{name: "nonzero", wantError: "Goose execution failed with exit code 7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, os.RemoveAll(outputPath))
			cmd := exec.Command("node", harness, binary)
			cmd.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION="+def.Version,
				"GH_AW_MCP_CONFIG="+config, "GH_AW_PROMPT="+prompt,
				"GOOSE_MODEL=openai/test-model", "AWF_REFLECT_ENABLED=0",
				"GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE="+schema,
				"GH_AW_STRUCTURED_OUTPUT_SCHEMA="+`{"type":"object"}`,
				"GH_AW_STRUCTURED_OUTPUT_FILE="+outputPath,
				"TEST_CAPTURE="+capture, "TEST_CASE="+test.name)
			output, err := cmd.CombinedOutput()
			if test.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, string(output), test.wantError)
				_, err = os.Stat(outputPath)
				assert.True(t, os.IsNotExist(err), "failed native output must not be published")
				return
			}
			require.NoError(t, err, "%s", output)
			content, err := os.ReadFile(outputPath)
			require.NoError(t, err)
			assert.JSONEq(t, `{"summary":"Native result"}`, string(content))
			stat, err := os.Stat(outputPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
			content, err = os.ReadFile(capture)
			require.NoError(t, err)
			var captured struct {
				Args   []string `json:"args"`
				Mode   int      `json:"mode"`
				Recipe struct {
					Prompt   string `json:"prompt"`
					Response struct {
						Schema map[string]any `json:"json_schema"`
					} `json:"response"`
				} `json:"recipe"`
			}
			require.NoError(t, json.Unmarshal(content, &captured))
			assert.Contains(t, captured.Args, "--recipe")
			assert.NotContains(t, captured.Args, "--instructions")
			assert.Equal(t, 0o600, captured.Mode)
			assert.Equal(t, "Complete the task. Preserve literal {{ user }} and {% endraw %}.", captured.Recipe.Prompt)
			assert.Equal(t, "object", captured.Recipe.Response.Schema["type"])
		})
	}
}
