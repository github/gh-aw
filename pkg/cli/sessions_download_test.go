//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const sessionTestHeader = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"
const sessionTestAgentEvent = `{"type":"assistant.message","data":{"content":"Session download works.\n"},"timestamp":"2026-10-02T00:00:01Z","id":"native-1","provenance":{"component":"agent","phase":"agent","path":"agent-session.jsonl","index":0}}` + "\n"

func writeSessionTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), constants.DirPermSensitive))
	require.NoError(t, os.WriteFile(file, []byte(content), constants.FilePermSensitive))
}

func installSessionTestGH(t *testing.T, artifacts map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifacts")
	var names []string
	for name, files := range artifacts {
		names = append(names, name)
		require.NoError(t, os.MkdirAll(filepath.Join(artifactRoot, name), constants.DirPermSensitive))
		for file, content := range files {
			writeSessionTestFile(t, artifactRoot, filepath.Join(name, file), content)
		}
	}
	calls := filepath.Join(root, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SESSION_TEST_CALLS"
if [ "$1" = "api" ]; then
  if [ -n "$SESSION_TEST_LIST_ERROR" ]; then
    echo "listing failed" >&2
    exit 1
  fi
  printf '%s' "$SESSION_TEST_NAMES"
  exit 0
fi
if [ "$1" != "run" ] || [ "$2" != "download" ]; then
  echo "unexpected gh command" >&2
  exit 1
fi
shift 3
while [ "$#" -gt 0 ]; do
  case "$1" in
    --name) name="$2"; shift 2 ;;
    --dir) dir="$2"; shift 2 ;;
    -R) shift 2 ;;
    *) echo "unexpected download argument" >&2; exit 1 ;;
  esac
done
if [ "$name" = "$SESSION_TEST_DOWNLOAD_ERROR" ]; then
  echo "artifact download denied" >&2
  exit 1
fi
mkdir -p "$dir"
cp -R "$SESSION_TEST_ARTIFACTS/$name/." "$dir"
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "gh"), []byte(script), constants.FilePermExecutable))
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SESSION_TEST_NAMES", strings.Join(names, "\n"))
	t.Setenv("SESSION_TEST_ARTIFACTS", artifactRoot)
	t.Setenv("SESSION_TEST_CALLS", calls)
	t.Setenv("SESSION_TEST_LIST_ERROR", "")
	t.Setenv("SESSION_TEST_DOWNLOAD_ERROR", "")
	return calls
}

func executeSessionTestCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewSessionsCommand()
	cmd.PersistentFlags().Bool("verbose", false, "Verbose output")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(append([]string{"download"}, args...))
	err := cmd.Execute()
	return output.String(), err
}

func TestSessionsDownloadUsageFirst(t *testing.T) {
	content := sessionTestHeader + sessionTestAgentEvent
	calls := installSessionTestGH(t, map[string]map[string]string{
		"usage": {"aw_session.jsonl": content},
		"agent": {"agent-session.jsonl": "must not be read"},
	})
	// Published JSONL downloads do not depend on Node.js.
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(calls), "node"), []byte("#!/bin/sh\nexit 1\n"), constants.FilePermExecutable))
	output, err := executeSessionTestCommand(t, "123", "--repo", "owner/repo")
	require.NoError(t, err)
	require.Equal(t, content, output)
	commands, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Contains(t, string(commands), "repos/owner/repo/actions/runs/123/artifacts")
	require.Contains(t, string(commands), "--name usage")
	require.Regexp(t, `-R ([^ ]*/)?owner/repo`, string(commands))
	require.NotContains(t, string(commands), "--name agent")
}

func TestSessionsDownloadReconstructs(t *testing.T) {
	for _, usagePresent := range []bool{true, false} {
		for _, format := range []string{"jsonl", "markdown"} {
			t.Run(format+"/usage="+strconv.FormatBool(usagePresent), func(t *testing.T) {
				requireSessionTestNode(t)
				artifacts := map[string]map[string]string{
					"agent": {
						"agent-session.jsonl":               `{"type":"assistant.message","data":{"content":"Reconstructed session"},"id":"native-1","timestamp":"2026-10-02T00:00:01Z"}` + "\n",
						"aw_info.json":                      `{"engine_id":"copilot"}`,
						"mcp-logs/gateway.jsonl":            `{"event":"tool_call","timestamp":"2026-10-02T00:00:02Z","tool_name":"list_issues","server_name":"github"}` + "\n",
						"agent/graders/grader_results.json": `{"results":[{"id":"quality","score":0,"passed":false}]}`,
					},
				}
				if usagePresent {
					artifacts["usage"] = map[string]string{"agent/execution.json": `{"exitCode":0}`}
				}
				calls := installSessionTestGH(t, artifacts)
				output, err := executeSessionTestCommand(t, "123", "--repo", "owner/repo", "--format", format)
				require.NoError(t, err)
				require.Contains(t, output, "Reconstructed session")
				require.Contains(t, output, "mcp.tool_call")
				require.Contains(t, output, "grader.result")
				if format == "jsonl" {
					require.NoError(t, validateSessionJSONL([]byte(output)))
					require.Contains(t, output, `"id":"native-1"`)
					require.Contains(t, output, `"component":"agent"`)
					require.Contains(t, output, `"type":"session.collection"`)
					require.Contains(t, output, `"absentComponents"`)
				} else {
					require.Contains(t, output, "### Unified session")
				}
				commands, err := os.ReadFile(calls)
				require.NoError(t, err)
				if usagePresent {
					require.Less(t, strings.Index(string(commands), "--name usage"), strings.Index(string(commands), "--name agent"))
				}
			})
		}
	}
}

func TestSessionsDownloadLegacyAgentUsesInfo(t *testing.T) {
	requireSessionTestNode(t)
	calls := installSessionTestGH(t, map[string]map[string]string{
		"agent-artifacts": {
			"tmp/gh-aw/agent-stdio.log": `{"type":"assistant","message":{"content":[{"type":"text","text":"Claude reconstructed"}]}}` + "\n",
		},
		"info": {"aw_info.json": `{"engine_id":"claude"}`},
	})
	output, err := executeSessionTestCommand(t, "https://github.example.com/owner/repo/actions/runs/123")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL([]byte(output)))
	require.Contains(t, output, "Claude reconstructed")
	commands, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Contains(t, string(commands), "--hostname github.example.com")
	require.Contains(t, string(commands), "-R github.example.com/owner/repo")
	require.Contains(t, string(commands), "--name agent-artifacts")
	require.Contains(t, string(commands), "--name info")
}

func TestSessionsDownloadWithoutEngineMetadata(t *testing.T) {
	requireSessionTestNode(t)
	output, err := func() (string, error) {
		installSessionTestGH(t, map[string]map[string]string{
			"agent": {
				"pi-streaming.jsonl": `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Metadata-free Pi session"}]}}` + "\n",
			},
		})
		return executeSessionTestCommand(t, "123", "--repo", "owner/repo")
	}()
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL([]byte(output)))
	require.Contains(t, output, "Metadata-free Pi session")
}

func TestSessionsDownloadPreviousVersions(t *testing.T) {
	requireSessionTestNode(t)
	for _, test := range []struct {
		engine   string
		artifact string
		logFile  string
		metadata string
		content  string
	}{
		{"claude", "agent-stdio-log", "agent-stdio.log", "aw_info", `{"type":"assistant","message":{"content":[{"type":"text","text":"Legacy session"}]}}`},
		{"codex", "agent", "agent-stdio.log", "activation", `{"type":"item.completed","item":{"type":"agent_message","text":"Legacy session"}}`},
		{"copilot", "call-agent-artifacts", "sandbox/agent/logs/copilot-session-state/session/events.jsonl", "aw-info", `{"type":"assistant.message","data":{"content":"Legacy session"},"id":"native-copilot"}`},
		{"pi", "agent", "pi-streaming.jsonl", "info", `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Legacy session"}]}}`},
	} {
		t.Run(test.engine, func(t *testing.T) {
			// Previous versions have neither aw_session.jsonl nor agent-session.jsonl.
			calls := installSessionTestGH(t, map[string]map[string]string{
				"usage":       {"agent_usage.json": "{}"},
				test.artifact: {test.logFile: test.content + "\n"},
				test.metadata: {"aw_info.json": `{"engine_id":"` + test.engine + `"}`},
			})
			for _, format := range []string{"jsonl", "markdown"} {
				output, err := executeSessionTestCommand(t, "123", "--repo", "owner/repo", "--format", format)
				require.NoError(t, err)
				require.Contains(t, output, "Legacy session")
				if format == "jsonl" {
					require.NoError(t, validateSessionJSONL([]byte(output)))
					require.Contains(t, output, `"component":"agent"`)
				} else {
					require.Contains(t, output, "### Unified session")
				}
			}
			commands, err := os.ReadFile(calls)
			require.NoError(t, err)
			require.Less(t, strings.Index(string(commands), "--name usage"), strings.Index(string(commands), "--name "+test.artifact))
			require.Contains(t, string(commands), "--name "+test.metadata)
		})
	}
}

func TestSessionsDownloadMarkdownAndOutput(t *testing.T) {
	requireSessionTestNode(t)
	content := sessionTestHeader + sessionTestAgentEvent + `{"type":"user.message","data":{"content":"private prompt"},"provenance":{"component":"agent","phase":"agent","path":"agent-session.jsonl","index":1}}` + "\n"
	installSessionTestGH(t, map[string]map[string]string{"call-usage": {"aw_session.jsonl": content}})
	for _, format := range []string{"jsonl", "markdown"} {
		t.Run(format, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "session")
			output, err := executeSessionTestCommand(t, "123", "--repo", "github.example.com/owner/repo", "--format", format, "-o", file)
			require.NoError(t, err)
			require.Empty(t, output)
			written, err := os.ReadFile(file)
			require.NoError(t, err)
			if format == "jsonl" {
				require.Equal(t, content, string(written))
			} else {
				require.Contains(t, string(written), "### Unified session")
				require.Contains(t, string(written), "Session download works.")
				require.NotContains(t, string(written), "private prompt")
			}
		})
	}
}

func TestSessionsDownloadOutputFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are only enforced on Unix")
	}
	content := sessionTestHeader + sessionTestAgentEvent
	tests := []struct {
		name         string
		existingMode os.FileMode
		create       bool
		wantMode     os.FileMode
	}{
		{name: "new file", wantMode: constants.FilePermSensitive},
		{name: "restrict public file", existingMode: constants.FilePermPublic, create: true, wantMode: constants.FilePermSensitive},
		{name: "preserve restrictive file", existingMode: 0o400, create: true, wantMode: 0o400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installSessionTestGH(t, map[string]map[string]string{"usage": {"aw_session.jsonl": content}})
			file := filepath.Join(t.TempDir(), "session.jsonl")
			if test.create {
				require.NoError(t, os.WriteFile(file, []byte("old"), test.existingMode))
			}
			_, err := executeSessionTestCommand(t, "123", "--repo", "owner/repo", "-o", file)
			require.NoError(t, err)
			info, err := os.Stat(file)
			require.NoError(t, err)
			require.Equal(t, test.wantMode, info.Mode().Perm())
		})
	}
}

func TestSessionsDownloadErrors(t *testing.T) {
	tests := []struct {
		name      string
		artifacts map[string]map[string]string
		env       string
		envValue  string
		want      string
	}{
		{"no artifacts", nil, "", "", "no agent artifact"},
		{"usage without session or agent", map[string]map[string]string{"usage": {"agent_usage.json": "{}"}}, "", "", "no agent artifact"},
		{"empty published session", map[string]map[string]string{"usage": {"aw_session.jsonl": ""}, "agent": {}}, "", "", "session is empty"},
		{"invalid published session", map[string]map[string]string{"usage": {"aw_session.jsonl": "invalid"}, "agent": {}}, "", "", "invalid session JSONL"},
		{"list failure", nil, "SESSION_TEST_LIST_ERROR", "1", "failed to list artifacts"},
		{"download failure", map[string]map[string]string{"usage": {}}, "SESSION_TEST_DOWNLOAD_ERROR", "usage", "artifact download denied"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := installSessionTestGH(t, test.artifacts)
			if test.env != "" {
				t.Setenv(test.env, test.envValue)
			}
			file := filepath.Join(t.TempDir(), "session")
			require.NoError(t, os.WriteFile(file, []byte("existing output"), constants.FilePermSensitive))
			output, err := executeSessionTestCommand(t, "123", "--repo", "owner/repo", "-o", file)
			require.ErrorContains(t, err, test.want)
			require.Empty(t, output)
			written, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "existing output", string(written))
			commands, err := os.ReadFile(calls)
			require.NoError(t, err)
			require.NotContains(t, string(commands), "--name agent")
		})
	}
}

func TestSessionsDownloadArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{}, {"123", "456"}, {"123", "--format", "json"}, {"not-a-run"},
		{"0"}, {"123", "--repo", "invalid"},
	} {
		_, err := executeSessionTestCommand(t, args...)
		require.Error(t, err, "%v", args)
	}

}

func TestSessionsDownloadExperimentalHelp(t *testing.T) {
	t.Parallel()
	cmd := NewSessionsCommand()
	require.Contains(t, cmd.Short, "Experimental:")
	download, _, err := cmd.Find([]string{"download"})
	require.NoError(t, err)
	require.Contains(t, download.Short, "Experimental:")
	require.Contains(t, download.Long, "Experimental:")
}

func TestSessionsDownloadStdoutOverridesRootDiagnostics(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "gh aw"}
	root.SetOut(&bytes.Buffer{})
	sessions := NewSessionsCommand()
	root.AddCommand(sessions)
	download, _, err := root.Find([]string{"sessions", "download"})
	require.NoError(t, err)
	require.Same(t, os.Stdout, download.OutOrStdout())
}

func TestSessionArtifactName(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		names []string
		base  string
		want  string
	}{
		{[]string{"call-usage", "usage"}, "usage", "usage"},
		{[]string{"call-usage"}, "usage", "call-usage"},
		{[]string{"agent-artifacts"}, "agent", "agent-artifacts"},
		{[]string{"activation"}, "usage", ""},
	} {
		name, err := sessionArtifactName(test.names, test.base, "agent-artifacts")
		require.NoError(t, err)
		require.Equal(t, test.want, name)
	}
	_, err := sessionArtifactName([]string{"one-usage", "two-usage"}, "usage", "")
	require.ErrorContains(t, err, "multiple usage artifacts")

	name, err := sessionArtifactName([]string{"caller-info", "caller-aw-info"}, "info", "")
	require.NoError(t, err)
	require.Equal(t, "caller-info", name)
	name, err = sessionArtifactName([]string{"caller-info", "caller-aw-info"}, "aw-info", "aw_info")
	require.NoError(t, err)
	require.Equal(t, "caller-aw-info", name)
}

func TestValidateSessionJSONL(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		content string
		wantErr string
	}{
		{"valid", sessionTestHeader + sessionTestAgentEvent, ""},
		{"large event", sessionTestHeader + `{"type":"vendor.extension","data":{"content":"` + strings.Repeat("x", 100000) + `"}}` + "\n", ""},
		{"empty", "\n", "session is empty"},
		{"missing header", sessionTestAgentEvent, "leading session.format"},
		{"duplicate header", sessionTestHeader + sessionTestHeader, "multiple collector format"},
		{"future version", strings.Replace(sessionTestHeader, `"version":1`, `"version":2`, 1), "unsupported unified session"},
		{"string version", strings.Replace(sessionTestHeader, `"version":1`, `"version":"1"`, 1), "invalid unified session file-format"},
		{"malformed", sessionTestHeader + "{", "line 2"},
		{"legacy event", sessionTestHeader + `{"type":"assistant","data":{}}`, "namespaced type"},
		{"null data", sessionTestHeader + `{"type":"assistant.message","data":null}`, "object data"},
		{"array data", sessionTestHeader + `{"type":"assistant.message","data":[]}`, "object data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateSessionJSONL([]byte(test.content))
			if test.wantErr == "" {
				require.NoError(t, err)
				for line := range strings.SplitSeq(strings.TrimSpace(test.content), "\n") {
					require.True(t, json.Valid([]byte(line)))
				}
			} else {
				require.ErrorContains(t, err, test.wantErr)
			}
		})
	}
}

func requireSessionTestNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for session parser tests")
	}
}
