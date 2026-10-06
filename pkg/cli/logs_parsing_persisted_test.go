//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionParserUnsupportedSessionSentinel(t *testing.T) {
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "agent-stdio.log", "unrecognized plaintext")
	_, err := runSessionParser(context.Background(), "reconstruct", root)
	require.ErrorIs(t, err, errNoRecognizableAgentSession)
	writeSessionTestFile(t, root, "invalid.jsonl", "not JSON")
	_, err = runSessionParser(context.Background(), "markdown", filepath.Join(root, "invalid.jsonl"))
	require.Error(t, err)
	require.NotErrorIs(t, err, errNoRecognizableAgentSession)
}

func TestPersistedImportedEngineReportWithoutDefinitions(t *testing.T) {
	requireSessionTestNode(t)
	root := t.TempDir()
	t.Chdir(root)
	// Any accidental definition lookup would fail, even for a known catalog ID.
	writeSessionTestFile(t, root, ".github/aw/engines.json", "{unavailable")
	for _, id := range []string{"aider", "opencode", "kiro", "unregistered-engine"} {
		t.Run(id, func(t *testing.T) {
			runDir := filepath.Join(root, id)
			writeSessionTestFile(t, runDir, "aw_info.json", `{"engine_id":"`+id+`"}`)
			writeSessionTestFile(t, runDir, "agent-session.jsonl", `{"type":"session.init","data":{"sourceEngine":"`+id+`"}}`+"\n"+
				`{"type":"user.message","data":{"content":"PRIVATE_PROMPT"}}`+"\n"+
				`{"type":"assistant.message","data":{"content":"Observed answer\nSecond line"}}`+"\n"+
				`{"type":"tool.execution_start","data":{"toolCallId":"call","toolName":"bash","input":{"command":"pwd"}}}`+"\n"+
				`{"type":"tool.execution_complete","data":{"toolCallId":"call","success":false,"output":"Observed failure"}}`+"\n")
			writeSessionTestFile(t, runDir, "agent-stdio.log", "PRIVATE_RAW_DUPLICATE")
			parseAgentLogIfRequested(1, runDir, true)
			report, err := os.ReadFile(filepath.Join(runDir, "log.md"))
			require.NoError(t, err)
			require.Contains(t, string(report), "Observed answer")
			require.Contains(t, string(report), "Second line")
			require.Contains(t, string(report), "Observed failure")
			require.Contains(t, string(report), "pwd")
			require.NotContains(t, string(report), "PRIVATE_PROMPT")
			require.NotContains(t, string(report), "PRIVATE_RAW_DUPLICATE")
			require.NoError(t, os.Remove(filepath.Join(runDir, "log.md")))
			require.NoError(t, parseAgentLog(runDir, nil, true))
			_, err = os.Stat(filepath.Join(runDir, "log.md"))
			require.NoError(t, err)
		})
	}
}

func TestLegacyPersistedReportWithoutDefinitions(t *testing.T) {
	requireSessionTestNode(t)
	root := t.TempDir()
	t.Chdir(root)
	writeSessionTestFile(t, root, ".github/aw/engines.json", "{unavailable")
	writeSessionTestFile(t, root, "aw_info.json", `{"engine_id":"cursor"}`)
	writeSessionTestFile(t, root, "agent-session.jsonl", `{"type":"system","subtype":"init"}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Legacy answer"}]}}`+"\n"+
		`{"type":"result","num_turns":1}`+"\n"+
		`{"type":"agent.execution","data":{"categories":[],"errorCodes":[],"errorTypes":[],"exitCode":0}}`+"\n")
	require.NoError(t, parseAgentLog(root, nil, true))
	report, err := os.ReadFile(filepath.Join(root, "log.md"))
	require.NoError(t, err)
	require.Contains(t, string(report), "Legacy answer")
	require.Contains(t, string(report), "cursor")
	require.NotContains(t, string(report), "Claude")
}

func TestUnsupportedPlaintextDoesNotProduceOfflineReport(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	writeSessionTestFile(t, root, "aw_info.json", `{"engine_id":"unregistered-engine"}`)
	writeSessionTestFile(t, root, "agent-stdio.log", "PRIVATE_RAW_PROMPT")
	rendered, err := parsePersistedAgentLog(root)
	require.NoError(t, err)
	require.False(t, rendered)
	_, err = os.Stat(filepath.Join(root, "log.md"))
	require.True(t, os.IsNotExist(err))
}
