//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestParseAgentExecution(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		data string
		fail bool
	}{
		{"zero exit", `{"categories":[],"errorCodes":[0,"0"],"errorTypes":[],"exitCode":0}`, false},
		{"unknown exit", `{"categories":["agentic_engine_timeout"],"errorCodes":[502],"errorTypes":["server_error"]}`, false},
		{"exponent code", `{"categories":[],"errorCodes":[1e3],"errorTypes":[]}`, false},
		{"decimal code", `{"categories":[],"errorCodes":[1.0],"errorTypes":[]}`, false},
		{"negative zero code", `{"categories":[],"errorCodes":[-0],"errorTypes":[]}`, false},
		{"safe code boundaries", `{"categories":[],"errorCodes":[-9007199254740991,9007199254740991],"errorTypes":[]}`, false},
		{"missing arrays", `{}`, true},
		{"null array", `{"categories":null,"errorCodes":[],"errorTypes":[]}`, true},
		{"duplicate categories", `{"categories":["timeout","timeout"],"errorCodes":[],"errorTypes":[]}`, true},
		{"duplicate codes", `{"categories":[],"errorCodes":[400,400],"errorTypes":[]}`, true},
		{"duplicate numeric spellings", `{"categories":[],"errorCodes":[1.0,1e0],"errorTypes":[]}`, true},
		{"duplicate zero spellings", `{"categories":[],"errorCodes":[0,-0.0],"errorTypes":[]}`, true},
		{"fractional code", `{"categories":[],"errorCodes":[1.5],"errorTypes":[]}`, true},
		{"null code", `{"categories":[],"errorCodes":[null],"errorTypes":[]}`, true},
		{"invalid type", `{"categories":[],"errorCodes":[],"errorTypes":[1]}`, true},
		{"invalid code", `{"categories":[],"errorCodes":[false],"errorTypes":[]}`, true},
		{"unsafe code", `{"categories":[],"errorCodes":[9007199254740992],"errorTypes":[]}`, true},
		{"null exit", `{"categories":[],"errorCodes":[],"errorTypes":[],"exitCode":null}`, true},
		{"negative exit", `{"categories":[],"errorCodes":[],"errorTypes":[],"exitCode":-1}`, true},
		{"invalid exit", `{"categories":[],"errorCodes":[],"errorTypes":[],"exitCode":256}`, true},
		{"string exit", `{"categories":[],"errorCodes":[],"errorTypes":[],"exitCode":"0"}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			execution, err := parseAgentExecution(json.RawMessage(test.data))
			if test.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if test.name == "zero exit" {
				require.NotNil(t, execution.ExitCode)
				require.Zero(t, *execution.ExitCode)
			} else {
				require.Nil(t, execution.ExitCode)
			}
			entry := `{"type":"agent.execution","data":` + test.data + `}` + "\n"
			require.NoError(t, validateSessionJSONL([]byte(sessionTestHeader+entry)))
			require.ErrorContains(t, validateSessionJSONL([]byte(sessionTestHeader+entry+entry)), "multiple agent.execution")
		})
	}
}

func TestAgentExecutionNumericReaderParity(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, test := range []struct {
		name string
		code string
		exit string
		fail bool
	}{
		{"exponent", "1e3", "1e0", false},
		{"decimal", "1.0", "1.0", false},
		{"negative zero", "-0", "-0.0", false},
		{"max safe code", "9007199254740991", "2.55e2", false},
		{"min safe code", "-9007199254740991", "0.0", false},
		{"fractional code", "1.5", "0", true},
		{"unsafe code", "9007199254740992", "0", true},
		{"boolean code", "true", "0", true},
		{"null code", "null", "0", true},
		{"duplicate code values", "1e3,1000.0", "0", true},
		{"duplicate zero values", "0,-0", "0", true},
		{"distinct code types", `0,"0"`, "0", false},
		{"fractional exit", "0", "0.1", true},
		{"null exit", "0", "null", true},
		{"string exit", "0", `"0"`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := `{"categories":[],"errorCodes":[` + test.code + `],"errorTypes":[],"exitCode":` + test.exit + `}`
			entry := `{"type":"agent.execution","data":` + data + "}\n"
			content := sessionTestHeader + entry
			goErr := validateSessionJSONL([]byte(content))
			root := t.TempDir()
			writeSessionTestFile(t, root, "aw_session.jsonl", content)
			_, jsErr := runSessionParser(context.Background(), "markdown", filepath.Join(root, "aw_session.jsonl"))
			require.Equal(t, test.fail, goErr != nil, "Go result: %v", goErr)
			require.Equal(t, test.fail, jsErr != nil, "JavaScript result: %v", jsErr)
		})
	}
}

func TestSessionParserExecutionErrors(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "agent-stdio.log", `{"type":"turn.failed","error":{"code":502,"type":"server_error","message":"Provider unavailable"}}`+"\n[codex-harness] done: exitCode=1\n")
	writeSessionTestFile(t, root, "agent_execution_exit_code.txt", "0\n")
	session, err := runSessionParser(context.Background(), "reconstruct", root, "codex")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL(session))
	var execution *agentExecutionData
	for line := range strings.SplitSeq(string(session), "\n") {
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "agent.execution" {
			execution, err = parseAgentExecution(event.Data)
			require.NoError(t, err)
		}
	}
	require.NotNil(t, execution)
	require.Equal(t, []string{"server_error"}, execution.ErrorTypes)
	require.Equal(t, []json.RawMessage{json.RawMessage("502")}, execution.ErrorCodes)
	require.NotNil(t, execution.ExitCode)
	require.Zero(t, *execution.ExitCode)
}

func TestSessionParserPolicyRefusals(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, test := range []struct {
		engine  string
		content string
		reason  string
	}{
		{"copilot", `{"object":"chat.completion","id":"filtered","choices":[{"finish_reason":"content_filter","message":{"role":"assistant","content":null,"refusal":null}}],"usage":{"prompt_tokens":10,"completion_tokens":0}}`, "content_filter"},
		{"claude", `{"type":"assistant","message":{"id":"refused","content":[],"stop_reason":"refusal","stop_details":{"category":null,"explanation":null},"usage":{"input_tokens":10,"output_tokens":0}}}`, "refusal"},
		{"codex", `{"type":"assistant.refusal","data":{"reason":"refusal","content":"Cannot provide that answer."}}`, "refusal"},
	} {
		t.Run(test.engine, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSessionTestFile(t, root, "agent-stdio.log", test.content+"\n")
			session, err := runSessionParser(context.Background(), "reconstruct", root, test.engine)
			require.NoError(t, err)
			require.NoError(t, validateSessionJSONL(session))
			require.Contains(t, string(session), `"type":"assistant.refusal"`)
			require.Contains(t, string(session), `"reason":"`+test.reason+`"`)
			require.NotContains(t, string(session), `"type":"assistant.message"`)
			writeSessionTestFile(t, root, "aw_session.jsonl", string(session))
			markdown, err := runSessionParser(context.Background(), "markdown", filepath.Join(root, "aw_session.jsonl"))
			require.NoError(t, err)
			require.Contains(t, string(markdown), "Policy refusal: "+test.reason)
		})
	}
}

func TestSessionParserEngines(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	tests := []struct {
		engine  string
		file    string
		content string
	}{
		{"claude", "agent-stdio.log", `{"type":"assistant","message":{"content":[{"type":"text","text":"Engine session text"}]}}`},
		{"codex", "agent-stdio.log", `{"type":"item.completed","item":{"type":"agent_message","text":"Engine session text"}}`},
		{"copilot", "sandbox/agent/logs/copilot-session-state/session/events.jsonl", `{"type":"assistant.message","data":{"content":"Engine session text"},"id":"copilot-native"}`},
		{"gemini", "agent-stdio.log", `{"type":"message","role":"assistant","content":"Engine session text"}`},
		{"pi", "pi-streaming.jsonl", `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Engine session text"}]}}`},
		{"opencode", "agent-stdio.log", `{"type":"text","sessionID":"ses_fixture","timestamp":1790899201000,"part":{"id":"prt_fixture","type":"text","text":"Engine session text"}}`},
		{"goose", "agent-stdio.log", `{"type":"message","message":{"id":"goose-native","role":"assistant","created":1790899201,"content":[{"type":"text","text":"Engine session text"}]}}`},
		{"custom", "agent-stdio.log", `{"type":"assistant.message","data":{"content":"Engine session text"}}`},
	}
	for _, test := range tests {
		t.Run(test.engine, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSessionTestFile(t, root, test.file, test.content+"\n")
			session, err := runSessionParser(context.Background(), "reconstruct", root, test.engine)
			require.NoError(t, err)
			require.NoError(t, validateSessionJSONL(session))
			require.Contains(t, string(session), "Engine session text")
			require.Contains(t, string(session), `"type":"assistant.message"`)
		})
	}
}

func TestSessionParserCopilotSubagentAttribution(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, source := range []string{
		"sandbox/agent/logs/copilot-session-state/session/events.jsonl",
		"agent-session.jsonl",
		"agent-stdio.log",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSessionTestFile(t, root, source, copilotSubagentIntegrationEvents(t))
			session, err := runSessionParser(context.Background(), "reconstruct", root, "copilot")
			require.NoError(t, err)
			require.NoError(t, validateSessionJSONL(session))
			requireCopilotSubagentIntegrationEvidence(t, session)

			// Consume only the published artifact: neither native events nor
			// bootstrap parser traces are available in the audit directory.
			published := t.TempDir()
			writeSessionTestFile(t, published, "usage/aw_session.jsonl", string(session))
			writeSessionTestFile(t, published, "usage/agent_usage.json", `{"primary_model":"proxy-only-model","input_tokens":1000,"output_tokens":100,"ai_credits":933.447}`)
			writeSessionTestFile(t, published, "agent-stdio.log", "● Fake(wrong-model) Must not override structured evidence\n[INFO] container(s)\nlen(d)\n")
			summary, err := analyzeTokenUsage(published, false)
			require.NoError(t, err)
			require.NotNil(t, summary)
			require.Equal(t, []SubagentModelRequest{
				{AgentName: "awf-routing", RequestedModel: "opus", ResolvedModel: "opus", EffectiveModel: "opus", InvocationCount: 1, CompletedCount: 1, Effort: "low"},
				{AgentName: "ghaw-issues", RequestedModel: "opus", ResolvedModel: "opus", EffectiveModel: "opus", InvocationCount: 1, IncompleteCount: 1, Effort: "low"},
				{AgentName: "subagent-research", RequestedModel: "opus", ResolvedModel: "opus", EffectiveModel: "opus", InvocationCount: 1, IncompleteCount: 1, Effort: "xhigh"},
			}, summary.SubagentModelRequests)
			require.Equal(t, []SubagentModelActual{{
				Model: "opus", ResolvedModel: "opus", Requests: 60,
				TokenCoreMetrics: TokenCoreMetrics{InputTokens: 6000, OutputTokens: 600, CacheReadTokens: 60, CacheWriteTokens: 120},
			}}, summary.SubagentModelActuals)
			require.Zero(t, summary.MismatchCount)
			require.Empty(t, summary.Warnings)
			require.Contains(t, summary.ByModel, "proxy-only-model")
			require.InDelta(t, 933.447, summary.TotalAIC, 0.000001)
		})
	}
}

func requireCopilotSubagentIntegrationEvidence(t *testing.T, session []byte) {
	t.Helper()
	events := make(map[string]map[string]json.RawMessage)
	for line := range strings.SplitSeq(string(session), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Type    string                     `json:"type"`
			AgentID string                     `json:"agentId"`
			Data    map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events[event.Type+":"+event.AgentID] = event.Data
	}
	require.JSONEq(t, `"xhigh"`, string(events["session.init:"]["reasoningEffort"]))
	require.JSONEq(t, `"built-in"`, string(events["subagent.started:research"]["agentType"]))
	require.JSONEq(t, `"agent_definition_default"`, string(events["subagent.started:research"]["modelSelectionSource"]))
	require.JSONEq(t, `"background"`, string(events["subagent.started:research"]["executionMode"]))
	for _, id := range []string{"ghaw", "awf"} {
		require.JSONEq(t, `"research"`, string(events["subagent.started:"+id]["parentId"]))
		require.JSONEq(t, `"low"`, string(events["subagent.configured:"+id]["reasoningEffort"]))
	}
	require.JSONEq(t, `"opus"`, string(events["subagent.completed:awf"]["firstDispatchedModel"]))
	message := events["assistant.message:awf"]
	for key, expected := range map[string]string{"model": "opus", "apiCallId": "api-awf", "interactionId": "interaction-awf", "turnId": "turn-awf", "parentToolCallId": "call-awf"} {
		require.JSONEq(t, `"`+expected+`"`, string(message[key]))
	}
	require.JSONEq(t, copilotSubagentIntegrationMetrics, string(events["session.result:"]["agentMetrics"]))
	require.NotContains(t, events["subagent.started:research"], "agentDescription")
}

func TestSessionParserCopilotSubagentsWithoutAccounting(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "sandbox/agent/logs/copilot-session-state/session/events.jsonl", copilotSubagentIntegrationLifecycle)
	session, err := runSessionParser(context.Background(), "reconstruct", root, "copilot")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL(session))
	published := t.TempDir()
	writeSessionTestFile(t, published, "usage/aw_session.jsonl", string(session))
	writeSessionTestFile(t, published, "usage/agent_usage.json", `{"primary_model":"main-only-model","input_tokens":1000}`)
	summary, err := analyzeTokenUsage(published, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Len(t, summary.SubagentModelRequests, 3)
	require.Empty(t, summary.SubagentModelActuals, "missing subagent accounting must not use whole-run usage")
	require.Empty(t, summary.Warnings)
	require.Zero(t, summary.MismatchCount)
}

func TestSessionParserCopilotWithoutSubagents(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "sandbox/agent/logs/copilot-session-state/session/events.jsonl",
		`{"type":"session.start","data":{"sessionId":"main-only","selectedModel":"opus"}}`+"\n"+
			`{"type":"assistant.message","data":{"content":"No delegation"}}`+"\n"+
			`{"type":"session.shutdown","data":{"agentMetrics":{"main":{"modelMetrics":{"opus":{"requests":{"count":41}}}}}}}`+"\n")
	session, err := runSessionParser(context.Background(), "reconstruct", root, "copilot")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL(session))
	published := t.TempDir()
	writeSessionTestFile(t, published, "usage/aw_session.jsonl", string(session))
	writeSessionTestFile(t, published, "usage/agent_usage.json", `{"primary_model":"opus","input_tokens":1000}`)
	writeSessionTestFile(t, published, "agent-stdio.log", "● Fake(wrong-model) Quoted dispatch\n[INFO] container(s)\n")
	summary, err := analyzeTokenUsage(published, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Empty(t, summary.SubagentModelRequests)
	require.Empty(t, summary.SubagentModelActuals)
	require.Empty(t, summary.Warnings)
	require.Zero(t, summary.MismatchCount)
}

func TestSessionParserDoesNotInferSubagentsFromStdio(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, dispatch := range []string{"Research(opus)", "Research (model: opus)"} {
		t.Run(dispatch, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSessionTestFile(t, root, "agent-stdio.log",
				"[INFO] container(s)\nclass RoutingProfile(StrictModel)\nlen(d)\n● "+dispatch+" Research routing\n")
			writeSessionTestFile(t, root, "agent_usage.json", `{"primary_model":"opus","input_tokens":1000}`)
			require.NoError(t, parseAgentLog(root, workflow.NewCopilotEngine(), false))
			markdown, err := os.ReadFile(filepath.Join(root, "log.md"))
			require.NoError(t, err)
			require.Contains(t, string(markdown), "Research")
			summary, err := analyzeTokenUsage(root, false)
			require.NoError(t, err)
			require.NotNil(t, summary)
			require.Empty(t, summary.SubagentModelRequests)
			require.Empty(t, summary.Warnings)
			require.Zero(t, summary.MismatchCount)
		})
	}
}

func copilotSubagentIntegrationEvents(t *testing.T) string {
	t.Helper()
	shutdown, err := json.Marshal(map[string]any{
		"type": "session.shutdown",
		"data": map[string]json.RawMessage{"agentMetrics": json.RawMessage(copilotSubagentIntegrationMetrics)},
	})
	require.NoError(t, err)
	return copilotSubagentIntegrationLifecycle + string(shutdown) + "\n"
}

const copilotSubagentIntegrationLifecycle = `{"type":"session.start","data":{"sessionId":"root","selectedModel":"opus","reasoningEffort":"xhigh"}}
{"type":"subagent.started","agentId":"research","data":{"toolCallId":"call-research","agentName":"research","agentDisplayName":"subagent-research","agentType":"built-in","model":"opus","modelSelectionSource":"agent_definition_default","executionMode":"background","agentDescription":"PRIVATE_DESCRIPTION"}}
{"type":"subagent.configured","agentId":"research","data":{"model":"opus","reasoningEffort":"xhigh"}}
{"type":"subagent.started","agentId":"ghaw","data":{"toolCallId":"call-ghaw","agentName":"explore","agentDisplayName":"ghaw-issues","parentId":"research","model":"opus","executionMode":"sync"}}
{"type":"subagent.configured","agentId":"ghaw","data":{"model":"opus","reasoningEffort":"low"}}
{"type":"subagent.started","agentId":"awf","data":{"toolCallId":"call-awf","agentName":"explore","agentDisplayName":"awf-routing","parentId":"research","model":"opus","executionMode":"sync"}}
{"type":"subagent.configured","agentId":"awf","data":{"model":"opus","reasoningEffort":"low"}}
{"type":"assistant.message","agentId":"awf","data":{"content":"Subagent findings","model":"opus","apiCallId":"api-awf","interactionId":"interaction-awf","turnId":"turn-awf","parentToolCallId":"call-awf"}}
{"type":"subagent.completed","agentId":"awf","data":{"model":"opus","firstDispatchedModel":"opus","totalTokens":1320,"totalToolCalls":1,"durationMs":100}}
`

func TestSessionParserRejectsUnrecognizedAgent(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "agent-stdio.log", "unrecognized log text\n")
	_, err := runSessionParser(context.Background(), "reconstruct", root, "claude")
	require.ErrorContains(t, err, "No recognizable agent session")
}

func TestParseAgentLogUsesSharedSessionParser(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "agent-stdio.log", `{"type":"assistant","message":{"content":[{"type":"text","text":"Audit parser works"}]}}`+"\n")
	require.NoError(t, parseAgentLog(root, workflow.NewClaudeEngine(), false))
	markdown, err := os.ReadFile(filepath.Join(root, "log.md"))
	require.NoError(t, err)
	require.Contains(t, string(markdown), "Audit parser works")
}

func TestParseAgentLogUsesBehaviorDefinedParser(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, test := range []struct {
		name       string
		parser     string
		wantOutput string
		wantError  string
	}{
		{
			name: "structured parser",
			parser: `function parseLog(logContent) {
  return { markdown: "### Parser markdown", logEntries: [{ type: "assistant.message", data: { content: "Custom structured finding" } }], mcpFailures: [], maxTurnsHit: false };
}`,
			wantOutput: "Custom structured finding",
		},
		{
			name: "MCP failure",
			parser: `function parseLog(logContent) {
  return { markdown: "### Parser markdown", logEntries: [], mcpFailures: ["custom server"], maxTurnsHit: false };
}`,
			wantError: "MCP server(s) failed to launch: custom server",
		},
		{
			name: "max turns",
			parser: `function parseLog(logContent) {
  return { markdown: "### Parser markdown", logEntries: [], mcpFailures: [], maxTurnsHit: true };
}`,
			wantError: "max-turns limit reached",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeSessionTestFile(t, root, "agent-stdio.log", "unrecognized custom log format\n")
			engine, err := workflow.NewBehaviorDefinedEngine(&workflow.EngineDefinition{
				ID:          "testparser",
				DisplayName: "TestParser",
				Behaviors: &workflow.EngineBehaviorDefinition{
					Execution: &workflow.EngineExecutionDefinition{
						CommandName: "testparser-cli",
						StepName:    "Execute TestParser",
					},
					LogParser: test.parser,
				},
			})
			require.NoError(t, err)
			err = parseAgentLog(root, engine, false)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			markdown, err := os.ReadFile(filepath.Join(root, "log.md"))
			require.NoError(t, err)
			require.Contains(t, string(markdown), test.wantOutput)
		})
	}
}
