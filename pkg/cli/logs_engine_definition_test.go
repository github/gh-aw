//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalLogParserEngine(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"cursor", "crush"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			engine, err := loadLocalLogParserEngine(id)
			require.NoError(t, err)
			require.NotNil(t, engine)
			require.Equal(t, id, engine.GetID())
			require.NotEmpty(t, engine.GetLogParserScriptSource())
			root := t.TempDir()
			writeSessionTestFile(t, root, "aw_info.json", `{"engine_id":"`+id+`"}`)
			require.Equal(t, id, extractEngineFromAwInfo(filepath.Join(root, "aw_info.json"), true).GetID())
		})
	}
	engine, err := loadLocalLogParserEngine("unknown-fixture")
	require.NoError(t, err)
	require.Nil(t, engine)
}

func TestLocalLogParserEngineRejectsEscapingDefinition(t *testing.T) {
	t.Parallel()
	for _, importPath := range []string{
		"github/gh-aw/../outside.md",
		"github/gh-aw/.github/workflows/shared/../../../outside.md",
	} {
		engine, err := loadLocalLogParserEngineAt(t.TempDir(), "cursor", []byte(`{"engines":[{"id":"cursor","import":"`+importPath+`"}]}`))
		require.ErrorContains(t, err, "outside the shared engine directory")
		require.Nil(t, engine)
	}
}

func TestLocalLogParserEngineRejectsSymlinkedArtifactDefinition(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	artifact := t.TempDir()
	writeSessionTestFile(t, artifact, "cursor.md", "---\nengine:\n  id: cursor\n  behaviors:\n    log-parser: |\n      throw new Error('ARTIFACT_CODE_EXECUTED');\n---\n")
	shared := filepath.Join(root, ".github", "workflows", "shared")
	require.NoError(t, os.MkdirAll(shared, 0755))
	require.NoError(t, os.Symlink(filepath.Join(artifact, "cursor.md"), filepath.Join(shared, "cursor.md")))
	engine, err := loadLocalLogParserEngineAt(root, "cursor", []byte(`{"engines":[{"id":"cursor","import":"github/gh-aw/.github/workflows/shared/cursor.md"}]}`))
	require.ErrorContains(t, err, "outside the shared engine directory")
	require.Nil(t, engine)
}

func TestArtifactEngineCodeAndRemoteReferencesRemainData(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprintln(w, "ARTIFACT_CODE_EXECUTED")
	}))
	defer server.Close()
	root := t.TempDir()
	code := "throw new Error('ARTIFACT_CODE_EXECUTED');"
	info, err := json.Marshal(map[string]any{
		"engine_id":  "cursor",
		"source":     server.URL + "/engine.md",
		"log_parser": code,
		"engine_definition": map[string]any{
			"id": "cursor", "behaviors": map[string]any{"log-parser": code},
		},
	})
	require.NoError(t, err)
	writeSessionTestFile(t, root, "aw_info.json", string(info))
	writeSessionTestFile(t, root, "cursor_log_parser.cjs", code)
	writeSessionTestFile(t, root, "behavior_log_parser.cjs", code)
	writeSessionTestFile(t, root, ".github/aw/engines.json", `{"engines":[{"id":"cursor","import":"`+server.URL+`/engine.md"}]}`)
	writeSessionTestFile(t, root, "agent-stdio.log", "Assistant: Trusted parser answer\n")
	engine := extractEngineFromAwInfo(filepath.Join(root, "aw_info.json"), true)
	require.NotNil(t, engine)
	require.Equal(t, "cursor", engine.GetID())
	require.NoError(t, parseAgentLog(root, engine, true))
	report, err := os.ReadFile(filepath.Join(root, "log.md"))
	require.NoError(t, err)
	require.Contains(t, string(report), "Trusted parser answer")
	require.NotContains(t, string(report), "ARTIFACT_CODE_EXECUTED")
	require.Zero(t, requests.Load())
	rejected, err := loadLocalLogParserEngineAt(root, "cursor", []byte(`{"engines":[{"id":"cursor","import":"`+server.URL+`/engine.md"}]}`))
	require.NoError(t, err)
	require.Nil(t, rejected)
	require.Zero(t, requests.Load())
}

func TestDeclaredEngineSessionParserBundle(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	for _, id := range []string{"cursor", "crush"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSessionTestFile(t, root, "agent-stdio.log", "Assistant: Real answer\nSecond line\n"+`{"type":"result","num_turns":1}`+"\n")
			output, err := runSessionParser(context.Background(), "reconstruct", root, id)
			require.NoError(t, err)
			require.NoError(t, validateSessionJSONL(output))
			require.Contains(t, string(output), `"sourceEngine":"`+id+`"`)
			require.Contains(t, string(output), `"type":"assistant.message"`)
			session := filepath.Join(root, "aw_session.jsonl")
			require.NoError(t, os.WriteFile(session, output, 0600))
			markdown, err := runSessionParser(context.Background(), "markdown", session)
			require.NoError(t, err)
			require.Contains(t, string(markdown), "Real answer")
			require.Contains(t, string(markdown), "Second line")
			require.NotContains(t, string(markdown), `{"type":"result"`)
			engine, err := loadLocalLogParserEngine(id)
			require.NoError(t, err)
			require.NoError(t, parseAgentLog(root, engine, true))
			agentMarkdown, err := os.ReadFile(filepath.Join(root, "log.md"))
			require.NoError(t, err)
			require.Contains(t, string(agentMarkdown), "Real answer")
			require.NotContains(t, string(agentMarkdown), `{"type":"result"`)
		})
	}
}
