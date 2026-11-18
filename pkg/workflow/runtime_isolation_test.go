package workflow

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func decodeWindowsWSLRunScript(t *testing.T, rendered string) string {
	t.Helper()
	_, tail, ok := strings.Cut(rendered, "FromBase64String('")
	require.True(t, ok, "missing encoded WSL script")
	encoded, _, ok := strings.Cut(tail, "'")
	require.True(t, ok, "unterminated encoded WSL script")
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()
	script, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(script)
}

func TestRuntimeIsolationRelocatesEverySerializationContext(t *testing.T) {
	source := `name: isolated
on: push
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - run: echo '/tmp/gh-aw/pre-setup'
      - uses: github/gh-aw/actions/setup@sha
        with:
          destination: ${{ runner.temp }}/gh-aw/actions
      - run: |
          cat <<'JSON' > /tmp/gh-aw/config.json
          {"mounts":["/tmp/gh-aw:/tmp/gh-aw:rw","${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"],"topologyAttach":["awmg-mcpg","awmg-cli-proxy"],"prompt":"\/tmp\/gh-aw/aw-prompts/prompt.txt"}
          JSON
          docker logs awmg-proxy
          node "${RUNNER_TEMP}/gh-aw/actions/run.cjs"
      - uses: actions/github-script@sha
        with:
          script: |
            const actionsDir = path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions');
            require(actionsDir + '/run.cjs')
      - uses: actions/upload-artifact@sha
        with:
          path: /tmp/gh-aw/logs
  other:
    runs-on: ubuntu-latest
    steps:
      - run: echo no-runtime
  called:
    uses: ./.github/workflows/reusable.yml
`
	actual, err := finalizeRunnerTempSafety(source)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	jobs := parsed["jobs"].(map[string]any)
	steps := jobs["agent"].(map[string]any)["steps"].([]any)
	require.Equal(t, "Allocate isolated runtime directories", steps[0].(map[string]any)["name"])
	require.Contains(t, actual, `"${{ env.GH_AW_RUNTIME_ROOT }}:${{ env.GH_AW_RUNTIME_ROOT }}:rw"`)
	require.Contains(t, actual, `"${GH_AW_SETUP_ROOT}:${GH_AW_SETUP_ROOT}:ro"`)
	require.Contains(t, actual, `"awmg-mcpg-${{ env.GH_AW_RESOURCE_SUFFIX }}"`)
	require.Contains(t, actual, `"awmg-cli-proxy-${{ env.GH_AW_RESOURCE_SUFFIX }}"`)
	require.Contains(t, actual, "docker logs awmg-proxy-${{ env.GH_AW_RESOURCE_SUFFIX }}")
	require.Contains(t, actual, "path.join(process.env.GH_AW_SETUP_ROOT, 'actions')")
	require.Contains(t, actual, "path: ${{ env.GH_AW_RUNTIME_ROOT }}/logs")
	require.Equal(t, 1, strings.Count(actual, "Allocate isolated runtime directories"))
	require.NotContains(t, actual, `\/tmp\/gh-aw`)
	require.Equal(t, []any{map[string]any{"run": "echo no-runtime"}}, jobs["other"].(map[string]any)["steps"])
	require.NotContains(t, jobs["called"].(map[string]any), "steps")
}

func TestRuntimeIsolationDefersEnvironmentUntilAllocation(t *testing.T) {
	source := `env:
  INHERITED: /tmp/gh-aw/shared
  OVERRIDE: /tmp/gh-aw/unused
  KEEP: value
jobs:
  agent:
    runs-on: ubuntu-latest
    env:
      OWN: /tmp/gh-aw/job
      OVERRIDE: local
      STATIC: preserved
    steps:
      - run: echo /tmp/gh-aw/use
        env:
          OWN: /tmp/gh-aw/step
`
	actual, err := finalizeRunnerTempSafety(source)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	require.Equal(t, map[string]any{"KEEP": "value"}, parsed["env"])
	job := parsed["jobs"].(map[string]any)["agent"].(map[string]any)
	require.Equal(t, map[string]any{"OVERRIDE": "local", "STATIC": "preserved"}, job["env"])
	steps := job["steps"].([]any)
	allocator := steps[0].(map[string]any)
	require.Equal(t, map[string]any{
		"GH_AW_INITIAL_ENV_0": "/tmp/gh-aw/shared",
		"GH_AW_INITIAL_ENV_1": "/tmp/gh-aw/job",
	}, allocator["env"])
	require.NotContains(t, allocator["run"], "eval")
	require.NotContains(t, allocator["run"], "${{ runner.temp }}")
	require.Equal(t, map[string]any{"OWN": runtimeRootExpression + "/step"}, steps[1].(map[string]any)["env"])
}

func TestRuntimeIsolationPreservesUnrelatedPrefixes(t *testing.T) {
	source := "/tmp/gh-aw-other /tmp/gh-awful /tmp/gh-aw.log /tmp/gh-aw/foo /tmp/gh-aw\n"
	require.Equal(t, "/tmp/gh-aw-other /tmp/gh-awful /tmp/gh-aw.log "+runtimeRootExpression+"/foo "+runtimeRootExpression+"\n", relocateRuntimeText(source))
}

func TestRuntimeIsolationAllocatorEstablishesOwnershipBeforePublication(t *testing.T) {
	step := runtimeAllocationStep(nil)
	for _, expected := range []string{
		"set -euo pipefail", "umask 077",
		`mktemp -d /tmp/gh-aw-run.XXXXXXXXXX`,
		`mktemp -d "$setup_parent/gh-aw-setup.XXXXXXXXXX"`,
		`openssl rand -hex 32`,
		`"$GH_AW_RUNTIME_ROOT/.gh-aw-runtime-owner"`,
		`"$GH_AW_SETUP_ROOT/.gh-aw-runtime-owner"`,
		"GH_AW_RESOURCE_SUFFIX", "GH_AW_RUNTIME_TOKEN",
	} {
		require.Contains(t, step, expected)
	}
	require.Less(t, strings.Index(step, ".gh-aw-runtime-owner"), strings.Index(step, `>> "$GITHUB_ENV"`))
}

func TestRuntimeIsolationCleanupSkipsMissingMarker(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime allocator requires GNU stat")
	}
	temp := t.TempDir()
	envFile := filepath.Join(temp, "env")
	bash := exec.CommandContext(t.Context(), "bash", "-c", runtimeAllocationScript)
	bash.Env = append(os.Environ(), "RUNNER_TEMP="+temp, "GITHUB_ENV="+envFile)
	output, err := bash.CombinedOutput()
	require.NoError(t, err, "%s", output)
	env, err := os.ReadFile(envFile)
	require.NoError(t, err)
	var roots []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(env)), "\n") {
		if key, root, ok := strings.Cut(line, "="); ok && (key == "GH_AW_RUNTIME_ROOT" || key == "GH_AW_SETUP_ROOT") {
			roots = append(roots, root)
			t.Cleanup(func() { _ = os.RemoveAll(root) })
		}
	}
	require.Len(t, roots, 2)
	setupRoot, runtimeRoot := roots[1], roots[0]
	require.NoError(t, os.Remove(filepath.Join(runtimeRoot, ".gh-aw-runtime-owner")))
	bash = exec.CommandContext(t.Context(), "bash", "-c", runtimeEarlyCleanupScript)
	bash.Env = append(os.Environ(), strings.Split(strings.TrimSpace(string(env)), "\n")...)
	output, err = bash.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "Refusing runtime cleanup without ownership")
	require.DirExists(t, runtimeRoot)
	require.NoDirExists(t, setupRoot)
}

func TestRuntimeIsolationWindowsBootstrapsWSLBeforeAllocation(t *testing.T) {
	job := &Job{
		Name:   "agent",
		RunsOn: "runs-on: windows-latest",
		Steps: []string{
			"      - name: Setup Scripts\n        uses: github/gh-aw/actions/setup@sha\n        with:\n          destination: ${{ runner.temp }}/gh-aw/actions\n",
			"      - run: cat /tmp/gh-aw/prompt.txt\n",
			"      - uses: actions/upload-artifact@sha\n        with:\n          path: /tmp/gh-aw/output.log\n",
		},
	}
	manager := NewJobManager()
	require.NoError(t, manager.AddJob(job))
	var source strings.Builder
	manager.WriteJobsYAML(&source)
	actual, err := finalizeRunnerTempSafety(source.String())
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	steps := parsed["jobs"].(map[string]any)["agent"].(map[string]any)["steps"].([]any)
	require.Equal(t, "Initialize WSL runtime", steps[0].(map[string]any)["name"])
	require.Equal(t, "Allocate isolated runtime directories", steps[1].(map[string]any)["name"])
	require.Equal(t, "Setup Scripts", steps[2].(map[string]any)["name"])
	require.NotContains(t, actual, "ln -s")
	require.Contains(t, actual, "path: "+windowsRuntimeRootExpression+"/output.log")
	require.Contains(t, actual, "$scriptContent.Replace('${GH_AW_RUNTIME_ROOT}', $env:GH_AW_RUNTIME_ROOT)")
	require.Contains(t, actual, "'GH_AW_SETUP_ROOT'")
}

func TestRuntimeIsolationRemovesEmptyEnvironmentMappings(t *testing.T) {
	actual, err := finalizeRunnerTempSafety(`env:
  ROOT: /tmp/gh-aw/workflow
jobs:
  agent:
    runs-on: ubuntu-latest
    env:
      LOCAL: /tmp/gh-aw/job
    steps:
      - run: echo hello
`)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	require.NotContains(t, parsed, "env")
	require.NotContains(t, parsed["jobs"].(map[string]any)["agent"], "env")
}

func TestRuntimeIsolationRejectsReusableWorkflowEnvironment(t *testing.T) {
	_, err := finalizeRunnerTempSafety("env:\n  ROOT: /tmp/gh-aw\njobs:\n  called:\n    uses: ./.github/workflows/called.yml\n")
	require.ErrorContains(t, err, "runtime-dependent workflow env")
}

func TestRuntimeIsolationSetupRootsAreNeverExecutableExpressions(t *testing.T) {
	actual, err := finalizeRunnerTempSafety(`jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - run: node "${RUNNER_TEMP}/gh-aw/actions/run.cjs"
      - run: node '${RUNNER_TEMP}/gh-aw/actions/run.cjs'
      - uses: actions/github-script@sha
        with:
          script: |
            const { main } = require('${{ runner.temp }}/gh-aw/actions/run.cjs');
            require('${{ runner.temp }}/gh-aw-setup/actions/other.cjs');
            main();
      - uses: github/gh-aw/actions/setup@sha
        with:
          destination: ${{ runner.temp }}/gh-aw/actions
`)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(actual, `run: node "${GH_AW_SETUP_ROOT}/actions/run.cjs"`))
	require.Contains(t, actual, "path.join(actionsDir, 'run.cjs')")
	require.Contains(t, actual, "require(`${process.env.GH_AW_SETUP_ROOT}/actions/other.cjs`)")
	require.Contains(t, actual, "destination: "+setupRootExpression+"/actions")
	require.NotContains(t, actual, `run: node "${{ env.GH_AW_SETUP_ROOT }}`)
	require.NotContains(t, actual, "require('${{ env.GH_AW_SETUP_ROOT }}")
}

func TestRuntimeIsolationWindowsEncodedScriptsUsePrivateRoots(t *testing.T) {
	encoded, err := encodeWindowsWSLRunScript(relocateWindowsWSLScript(`node "${RUNNER_TEMP}/gh-aw-setup/actions/run.cjs"
cat /tmp/gh-aw/prompt.txt
docker logs awmg-mcpg
`))
	require.NoError(t, err)
	script := decodeWindowsWSLRunScript(t, "$compressedScript = [System.Convert]::FromBase64String('"+encoded+"')")
	require.Contains(t, script, `${GH_AW_SETUP_ROOT}/actions/run.cjs`)
	require.Contains(t, script, `${GH_AW_RUNTIME_ROOT}/prompt.txt`)
	require.Contains(t, script, `awmg-mcpg-${GH_AW_RESOURCE_SUFFIX}`)
	require.NotContains(t, script, "/tmp/gh-aw")
	require.NotContains(t, script, "${RUNNER_TEMP}/gh-aw")
	require.NotContains(t, script, "${{ env.")
}

func TestRuntimeIsolationWindowsAllocatorPreservesLegacyPatterns(t *testing.T) {
	script := decodeWindowsWSLRunScript(t, windowsRuntimeAllocationStep(map[string]string{
		"GH_AW_MCP_LOG_DIR": "/tmp/gh-aw/mcp-logs/safeoutputs",
	}))
	require.Contains(t, script, `value="${value//\/tmp\/gh-aw/$GH_AW_RUNTIME_ROOT}"`)
	require.Contains(t, script, `source="${RUNNER_TEMP}/gh-aw"`)
	require.Contains(t, script, "GH_AW_MCP_LOG_DIR")
}

func TestRuntimeIsolationRelocatesSpecializedPaths(t *testing.T) {
	input := "require(`${process.env.RUNNER_TEMP}/gh-aw-setup/actions/claim.cjs`)\n" +
		`--capture-command-output "${RUNNER_TEMP}/gh-aw-protected-capture-${GH_AW_PROTECTED_CAPTURE_ID}"` + "\n" +
		`'docker run -v '"${RUNNER_TEMP}"'/gh-aw/safeoutputs:/safeoutputs:rw'`
	actual := relocateWindowsWSLScript(input)
	require.Contains(t, actual, "require(`${process.env.GH_AW_SETUP_ROOT}/actions/claim.cjs`)")
	require.Contains(t, actual, `"${GH_AW_SETUP_ROOT}/protected-capture-${GH_AW_PROTECTED_CAPTURE_ID}"`)
	require.Contains(t, actual, `'"${GH_AW_SETUP_ROOT}"'/safeoutputs`)
	output := rewriteWindowsPathOutputs("Setup agent output environment variable", `echo "GH_AW_AGENT_OUTPUT=/tmp/gh-aw/safeoutputs/outputs.jsonl" >> "$GITHUB_OUTPUT"`)
	require.Contains(t, relocateWindowsWSLScript(output), `wslpath -w "${GH_AW_RUNTIME_ROOT}/safeoutputs/outputs.jsonl"`)
	require.Contains(t, strings.Join(windowsWSLEnvironmentSetup(nil), "\n"), "'GH_AW_CLI_PROXY_PORT'")
	require.Contains(t, strings.Join(windowsWSLEnvironmentSetup(nil), "\n"), "'GH_AW_DIFC_PROXY_PORT'")
}

func TestRuntimeIsolationScriptCachesSaveBeforeCleanup(t *testing.T) {
	source := `jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Restore runtime cache
        id: existing-cache
        if: ${{ inputs.save }}
        uses: actions/cache@immutable
        with:
          path: /tmp/gh-aw/cache-memory
          key: runtime-key
          restore-keys: runtime-
      - name: Lookup runtime cache
        uses: actions/cache@immutable
        with:
          path: /tmp/gh-aw/lookup
          key: lookup-key
          lookup-only: true
      - name: Restore dependencies
        uses: actions/cache@immutable
        with:
          path: node_modules
          key: dependency-key
      - name: Clean Scripts
        run: bash "/tmp/gh-aw/actions-source/actions/setup/clean.sh"
`
	actual, err := finalizeRunnerTempSafety(source)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	steps := parsed["jobs"].(map[string]any)["agent"].(map[string]any)["steps"].([]any)
	require.Equal(t, "Prepare isolated runtime cache", steps[1].(map[string]any)["name"])
	require.Equal(t, "${{ inputs.save }}", steps[1].(map[string]any)["if"])
	restore := steps[2].(map[string]any)
	require.Equal(t, "actions/cache/restore@immutable", restore["uses"])
	require.Equal(t, "existing-cache", restore["id"])
	require.Equal(t, true, steps[6].(map[string]any)["with"].(map[string]any)["lookup-only"])
	require.Equal(t, "actions/cache@immutable", steps[9].(map[string]any)["uses"])
	require.Contains(t, steps[10].(map[string]any)["if"], "steps.existing-cache.outputs.cache-hit != 'true'")
	save := steps[12].(map[string]any)
	require.Equal(t, "actions/cache/save@immutable", save["uses"])
	require.Equal(t, "${{ steps.existing-cache.outputs.cache-primary-key }}", save["with"].(map[string]any)["key"])
	require.Equal(t, restore["with"].(map[string]any)["path"], save["with"].(map[string]any)["path"])
	require.Equal(t, "Clean Scripts", steps[len(steps)-2].(map[string]any)["name"])
	require.Equal(t, "Clean isolated runtime directories", steps[len(steps)-1].(map[string]any)["name"])
	require.Equal(t, 2, strings.Count(actual, "actions/cache/save@immutable"))
}
