//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAiderLogParserAndUnifiedSession(t *testing.T) {
	def := loadAiderSample(t)
	require.NotEmpty(t, def.Behaviors.LogParser)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent-stdio.log")
	log := `[INFO] Firewall started
{"type":"session.init","timestamp":1000,"data":{"sourceEngine":"aider","sessionId":"test","model":"openai/auto"}}
{"type":"user.message","timestamp":1050,"data":{"sourceEngine":"aider","sessionId":"test","content":"Run the smoke test"}}
{"type":"assistant.message","timestamp":1100,"data":{"sourceEngine":"aider","sessionId":"test","content":"Model reply with smoke results"}}
{"type":"tool.execution_start","timestamp":1200,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"success","toolName":"bash","input":{"command":"safeoutputs create_issue --title test --body PASS"}}}
{"type":"tool.execution_complete","timestamp":1300,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"success","toolName":"bash","success":true,"exitCode":0,"output":"issue queued","durationMs":100}}
{"type":"tool.execution_start","timestamp":1400,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"failure","toolName":"bash","input":{"command":"test missing-file"}}}
{"type":"tool.execution_complete","timestamp":1500,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"failure","toolName":"bash","success":false,"exitCode":1,"output":"file missing","durationMs":100}}
{"type":"session.result","timestamp":1600,"data":{"sourceEngine":"aider","sessionId":"test","numTurns":1,"durationMs":600,"errors":[]}}
[aider-harness] process completed
`
	require.NoError(t, os.WriteFile(logPath, []byte(log), 0o600))
	source := def.Behaviors.LogParser + `
const fs = require("fs");
const path = require("path");
const { collectUnifiedSession } = require("./unified_session.cjs");
const parsed = parseLog(fs.readFileSync(process.argv[1], "utf8"));
const unified = collectUnifiedSession({ rootDir: path.dirname(process.argv[1]), engine: "aider" });
process.stdout.write(JSON.stringify({
  parsed,
  events: unified.events.filter(event => event.provenance.component === "agent"),
  unrecognized: unified.events.some(event => event.type === "session.source_warning" && event.data.reason === "unrecognized_engine_log"),
}));
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source, logPath)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct {
		Parsed struct {
			Markdown   string
			LogEntries []struct {
				Type string
			}
		}
		Events []struct {
			Type string
			Data struct {
				Success *bool
			}
		}
		Unrecognized bool
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.Len(t, result.Parsed.LogEntries, 8)
	require.Len(t, result.Events, 8)
	assert.Contains(t, result.Parsed.Markdown, "Model reply with smoke results")
	assert.False(t, result.Unrecognized)
	require.NotNil(t, result.Events[4].Data.Success)
	assert.True(t, *result.Events[4].Data.Success)
	require.NotNil(t, result.Events[6].Data.Success)
	assert.False(t, *result.Events[6].Data.Success)
}
