//go:build !integration && !windows

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadGooseSmokeChecker(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/smoke-goose.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		PostSteps []struct {
			Name string            `yaml:"name"`
			With map[string]string `yaml:"with"`
		} `yaml:"post-steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.Len(t, frontmatter.PostSteps, 1)
	require.Equal(t, "Assert Goose smoke evidence", frontmatter.PostSteps[0].Name)
	return frontmatter.PostSteps[0].With["script"]
}

func writeGooseSmokeFixtures(t *testing.T, dir string) {
	t.Helper()
	for name, content := range map[string]string{
		"smoke-test-goose-123.json": `{"pullRequests":[{"number":1,"title":"PR 1"},{"number":2,"title":"PR 2"}],"bash":true,"build":true,"fileWrite":true,"runtime":true,"webFetch":true}`,
		"smoke-test-goose-123.txt":  "Smoke test passed for Goose at test timestamp\n",
		"web-fetch.json":            `{"containsGitHub":true}`,
		"gh-aw":                     "test build output",
		"agent-stdio.log": `[goose-harness] verified Goose 1.53.0
{"type":"message","message":{"role":"assistant","content":[{"type":"toolRequest","id":"tool-1","toolCall":{"status":"success","value":{"name":"github__pull_request_read"}}}]}}
{"type":"message","message":{"role":"user","content":[{"type":"toolResponse","id":"tool-1","toolResult":{"status":"success","value":{"content":[]}}}]}}
{"type":"message","message":{"id":"text-1","role":"assistant","content":[{"type":"text","text":"Done."}]}}
{"type":"complete","input_tokens":10,"output_tokens":5}
`,
		"agent-session.jsonl": `{"type":"session.init","data":{"sourceEngine":"goose"}}
{"type":"tool.execution_start","data":{"toolCallId":"tool-1","toolName":"github__pull_request_read"}}
{"type":"tool.execution_complete","data":{"toolCallId":"tool-1","toolName":"github__pull_request_read","success":true}}
{"type":"assistant.message","data":{"content":"Done."}}
{"type":"session.result","data":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5,"input_tokens_include_cache":true}}}
{"type":"agent.execution","data":{"exitCode":0}}
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700))
	}
}

func TestGooseSmokeCheckerRequiresOperationalEvidence(t *testing.T) {
	checker := loadGooseSmokeChecker(t)
	for _, test := range []struct {
		name      string
		file      string
		content   string
		execution string
		wantError string
	}{
		{name: "passing evidence"},
		{name: "failed execution", execution: "failure", wantError: "Goose execution failed"},
		{name: "missing evidence", file: "smoke-test-goose-123.json", wantError: "ENOENT"},
		{name: "failed build", file: "smoke-test-goose-123.json", content: `{"pullRequests":[{"number":1,"title":"PR 1"},{"number":2,"title":"PR 2"}],"bash":true,"build":false,"fileWrite":true,"runtime":true,"webFetch":true}`, wantError: "build did not pass"},
		{name: "invented PR", file: "smoke-test-goose-123.json", content: `{"pullRequests":[{"number":1,"title":"invented"},{"number":2,"title":"PR 2"}],"bash":true,"build":true,"fileWrite":true,"runtime":true,"webFetch":true}`, wantError: "invented"},
		{name: "missing web receipt", file: "web-fetch.json", wantError: "ENOENT"},
		{name: "missing build binary", file: "gh-aw", wantError: "ENOENT"},
		{name: "missing unified session", file: "agent-session.jsonl", wantError: "ENOENT"},
		{name: "legacy session events", file: "agent-session.jsonl", content: `{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}`, wantError: "Goose session contains non-canonical events"},
		{name: "failed MCP", file: "agent-stdio.log", content: `[goose-harness] verified Goose 1.53.0
{"type":"message","message":{"content":[{"type":"toolRequest","id":"tool-1","toolCall":{"status":"success","value":{"name":"github__pull_request_read"}}},{"type":"toolResponse","id":"tool-1","toolResult":{"status":"success","value":{"isError":true,"content":[]}}}]}}
{"type":"complete"}
`, wantError: "No successful native GitHub MCP round-trip"},
		{name: "missing completion", file: "agent-stdio.log", content: "[goose-harness] verified Goose 1.53.0\n", wantError: "Goose did not finish a structured run"},
		{name: "disconnected extension", file: "agent-stdio.log", content: "[goose-harness] verified Goose 1.53.0\nFailed to start extension 'github'\n", wantError: "A Goose MCP extension failed to connect"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGooseSmokeFixtures(t, dir)
			if test.file != "" {
				file := filepath.Join(dir, test.file)
				if test.content == "" {
					require.NoError(t, os.Remove(file))
				} else {
					require.NoError(t, os.WriteFile(file, []byte(test.content), 0o700))
				}
			}
			execution := test.execution
			if execution == "" {
				execution = "success"
			}
			output, err := runGooseSmokeChecker(t, dir, checker, execution)
			if test.wantError == "" {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err)
				assert.Contains(t, output, test.wantError)
			}
		})
	}
}

func runGooseSmokeChecker(t *testing.T, dir, checker, execution string) (string, error) {
	t.Helper()
	scriptPath := filepath.Join(dir, "checker.cjs")
	require.NoError(t, os.WriteFile(scriptPath, []byte(checker), 0o600))
	runner := `const fs = require("node:fs");
const path = require("node:path");
const wrapper = name => name === "node:fs" ? {
	...fs,
	readFileSync: (file, ...args) => fs.readFileSync(["/tmp/gh-aw/agent-stdio.log", "/tmp/gh-aw/agent-session.jsonl"].includes(file) ? path.join(process.env.SMOKE_ROOT, path.basename(file)) : file, ...args)
} : require(name);
const github = { rest: { pulls: { get: async ({pull_number}) => ({data: {merged_at: "2026-10-01", title: "PR " + pull_number}}) } } };
const summary = {addHeading() {return this;}, addRaw() {return this;}, async write() {}};
const AsyncFunction = Object.getPrototypeOf(async function() {}).constructor;
new AsyncFunction("require", "github", "context", "core", fs.readFileSync(process.argv[1], "utf8"))(
	wrapper, github, {repo: {owner: "test", repo: "test"}}, {summary}
).catch(error => {console.error(error.message); process.exitCode = 1;});`
	cmd := exec.Command("node", "-e", runner, scriptPath)
	cmd.Env = append(os.Environ(), "SMOKE_ROOT="+dir, "SMOKE_STATE="+dir,
		"GITHUB_WORKSPACE="+dir, "SMOKE_RUN_ID=123", "SMOKE_EXECUTION="+execution)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
