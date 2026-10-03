//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type conformanceStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
	If   string `yaml:"if"`
}

type conformanceSuite struct {
	PreAgentSteps []conformanceStep `yaml:"pre-agent-steps"`
	PostSteps     []conformanceStep `yaml:"post-steps"`
	MCPScripts    map[string]struct {
		Description string `yaml:"description"`
		Script      string `yaml:"script"`
	} `yaml:"mcp-scripts"`
}

func readConformanceFrontmatter(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	frontmatter, err := extractMarkdownFrontmatterYAML(content)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(frontmatter, target))
}

func conformanceNodeScript(t *testing.T, run string) string {
	t.Helper()
	_, script, ok := strings.Cut(run, "node <<'JS'\n")
	require.True(t, ok, "expected embedded JavaScript heredoc")
	script, ok = strings.CutSuffix(script, "\nJS\n")
	require.True(t, ok, "expected closing JavaScript heredoc")
	return script
}

func runConformanceNode(t *testing.T, env []string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", args...)
	command.Env = append(os.Environ(), env...)
	return command.CombinedOutput()
}

func TestEngineConformanceJavaScript(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/engine-conformance.md", &suite)
	require.Len(t, suite.PreAgentSteps, 1)
	require.Len(t, suite.PostSteps, 2)
	require.Equal(t, "always()", suite.PostSteps[0].If)
	prepare := conformanceNodeScript(t, suite.PreAgentSteps[0].Run)
	verify := conformanceNodeScript(t, suite.PostSteps[0].Run)
	tool := suite.MCPScripts["conformance-challenge"]
	require.NotEmpty(t, tool.Script)

	tests := []struct {
		name       string
		mutate     string
		execution  string
		wantFailed string
	}{
		{name: "success", execution: "success"},
		{name: "failed agent", execution: "failure", wantFailed: "agent-execution"},
		{name: "timed out agent", execution: "cancelled", wantFailed: "agent-execution"},
		{name: "missing result", execution: "success", mutate: `fs.unlinkSync(resultPath);`, wantFailed: "result-schema"},
		{name: "malformed result", execution: "success", mutate: `fs.writeFileSync(resultPath, "{");`, wantFailed: "result-schema"},
		{name: "extra field", execution: "success", mutate: `result.passed = true; save();`, wantFailed: "result-schema"},
		{name: "wrong number type", execution: "success", mutate: `result.sum = String(result.sum); save();`, wantFailed: "result-schema"},
		{name: "wrong arithmetic", execution: "success", mutate: `result.sum++; save();`, wantFailed: "inference"},
		{name: "wrong file nonce", execution: "success", mutate: `result.fileNonce = "guessed"; save();`, wantFailed: "file-read"},
		{name: "wrong tool nonce", execution: "success", mutate: `result.toolNonce = "guessed"; save();`, wantFailed: "tool-round-trip"},
		{name: "missing receipt", execution: "success", mutate: `fs.unlinkSync(path.join(host, "receipt.json"));`, wantFailed: "tool-round-trip"},
		{name: "missing shell", execution: "success", mutate: `fs.unlinkSync(path.join(root, "shell.json"));`, wantFailed: "shell-and-environment"},
		{name: "wrong environment", execution: "success", mutate: `result.engineEnv = "other"; save();`, wantFailed: "shell-and-environment"},
		{name: "modified fixture", execution: "success", mutate: `fs.writeFileSync(path.join(root, "input.json"), "{}");`, wantFailed: "fixtures"},
		{name: "oversized result", execution: "success", mutate: `fs.writeFileSync(resultPath, " ".repeat(16385));`, wantFailed: "result-schema"},
		{name: "symlink result", execution: "success", mutate: `fs.renameSync(resultPath, resultPath + ".real"); fs.symlinkSync(resultPath + ".real", resultPath);`, wantFailed: "result-schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			root := filepath.Join(directory, "agent")
			host := filepath.Join(directory, "host")
			env := []string{
				"CONFORMANCE_ENGINE=copilot", "CONFORMANCE_ROOT=" + root,
				"CONFORMANCE_STATE=" + host, "CONFORMANCE_EXECUTION=" + test.execution,
				"ENGINE_CONFORMANCE_SENTINEL=conformance-copilot",
				"GITHUB_STEP_SUMMARY=" + filepath.Join(directory, "summary.md"),
			}
			output, err := runConformanceNode(t, env, "-e", prepare)
			require.NoError(t, err, "%s", output)
			output, err = runConformanceNode(t, env, filepath.Join(root, "shell-probe.cjs"))
			require.NoError(t, err, "%s", output)

			toolScript := `
const fs = require("node:fs");
const path = require("node:path");
const root = process.env.CONFORMANCE_ROOT;
const host = process.env.CONFORMANCE_STATE;
const input = JSON.parse(fs.readFileSync(path.join(root, "input.json"), "utf8"));
async function execute(file_nonce) {
` + tool.Script + `
}
(async () => {
  await require("node:assert/strict").rejects(execute("wrong"), /nonce does not match/);
  const proof = await execute(input.fileNonce);
  const shell = JSON.parse(fs.readFileSync(path.join(root, "shell.json"), "utf8"));
  const result = { fileNonce: input.fileNonce, sum: input.left + input.right, ...shell, toolNonce: proof.toolNonce };
  const resultPath = path.join(root, "result.json");
  const save = () => fs.writeFileSync(resultPath, JSON.stringify(result));
  save();
  ` + test.mutate + `
})().catch(error => { console.error(error); process.exitCode = 1; });
`
			output, err = runConformanceNode(t, env, "-e", toolScript)
			require.NoError(t, err, "%s", output)
			output, err = runConformanceNode(t, env, "-e", verify)
			reportContent, readErr := os.ReadFile(filepath.Join(host, "report.json"))
			require.NoError(t, readErr)
			var report struct {
				Status string `json:"status"`
				Checks []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"checks"`
			}
			require.NoError(t, json.Unmarshal(reportContent, &report))
			require.Len(t, report.Checks, 7)
			if test.wantFailed == "" {
				require.NoError(t, err, "%s", output)
				require.Equal(t, "passed", report.Status)
			} else {
				require.Error(t, err, "%s", output)
				require.Equal(t, "failed", report.Status)
				found := false
				for _, check := range report.Checks {
					if check.Name == test.wantFailed && check.Status == "failed" {
						found = true
					}
				}
				require.True(t, found, "%s should fail: %s", test.wantFailed, reportContent)
			}
		})
	}
}

func TestEngineConformanceCatalogCoverage(t *testing.T) {
	registry := NewEngineRegistry()
	ids := registry.GetSupportedEngines()
	var catalog knownEngineImportsFile
	content, err := os.ReadFile("../../.github/aw/engines.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &catalog))
	for _, entry := range catalog.Engines {
		if entry.ID == "custom" {
			var legacy engineDefinitionFile
			readConformanceFrontmatter(t, "../../.github/workflows/shared/genaiscript.md", &legacy)
			require.Nil(t, legacy.Engine.Behaviors, "a migrated GenAIScript engine needs a conformance workflow")
			require.False(t, registry.IsValidEngine(entry.ID))
			continue
		}
		ids = append(ids, entry.ID)
	}
	files, err := filepath.Glob("../../.github/workflows/engine-conformance-*.md")
	require.NoError(t, err)
	require.Len(t, files, len(ids), "each runnable engine must have exactly one conformance workflow")
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			file := "../../.github/workflows/engine-conformance-" + id + ".md"
			var source map[string]any
			readConformanceFrontmatter(t, file, &source)
			engine, ok := source["engine"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, id, engine["id"])
			require.Equal(t, true, source["strict"])
			require.Equal(t, true, source["inlined-imports"])
			require.EqualValues(t, 10, source["timeout-minutes"])
			require.EqualValues(t, 5, source["max-ai-credits"])
			on, ok := source["on"].(map[string]any)
			require.True(t, ok)
			require.Contains(t, on, "workflow_dispatch")
			require.Len(t, on, 1, "live inference should be manual-only")
			lock, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
			require.NoError(t, err)
			compiled := string(lock)
			for _, expected := range []string{
				"Prepare engine conformance fixtures", "Assert engine conformance",
				"Upload engine conformance evidence", "conformance-challenge",
				"ENGINE_CONFORMANCE_SENTINEL: conformance-" + id,
				"CONFORMANCE_EXECUTION: ${{ steps.agentic_execution.outcome }}",
			} {
				require.Contains(t, compiled, expected, "compiled %s is missing %s", id, expected)
			}
			require.NotContains(t, compiled, "github.aw.import-inputs", "import inputs must be resolved")
			require.Contains(t, compiled, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`, "safe outputs must not publish test issues")
		})
	}
}
