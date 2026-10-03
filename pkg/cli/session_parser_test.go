//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

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
