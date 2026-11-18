package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

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
      - uses: actions/github-script@sha
        with:
          script: |
            const { main } = require('${{ runner.temp }}/gh-aw/actions/run.cjs');
            main();
      - uses: github/gh-aw/actions/setup@sha
        with:
          destination: ${{ runner.temp }}/gh-aw/actions
`)
	require.NoError(t, err)
	require.Contains(t, actual, `run: node "${GH_AW_SETUP_ROOT}/actions/run.cjs"`)
	require.Contains(t, actual, "path.join(process.env.GH_AW_SETUP_ROOT, 'actions')")
	require.Contains(t, actual, "destination: "+setupRootExpression+"/actions")
	require.NotContains(t, actual, `run: node "${{ env.GH_AW_SETUP_ROOT }}`)
	require.NotContains(t, actual, "require('${{ env.GH_AW_SETUP_ROOT }}")
}

func TestRuntimeIsolationWindowsEncodedScriptsUsePrivateRoots(t *testing.T) {
	encoded, err := encodeWindowsWSLRunScript(`node "${RUNNER_TEMP}/gh-aw-setup/actions/run.cjs"
cat /tmp/gh-aw/prompt.txt
docker logs awmg-mcpg
`)
	require.NoError(t, err)
	script := decodeWindowsWSLRunScript(t, "$compressedScript = [System.Convert]::FromBase64String('"+encoded+"')")
	require.Contains(t, script, `${GH_AW_SETUP_ROOT}/actions/run.cjs`)
	require.Contains(t, script, `${GH_AW_RUNTIME_ROOT}/prompt.txt`)
	require.Contains(t, script, `awmg-mcpg-${GH_AW_RESOURCE_SUFFIX}`)
	require.NotContains(t, script, "/tmp/gh-aw")
	require.NotContains(t, script, "${RUNNER_TEMP}/gh-aw")
	require.NotContains(t, script, "${{ env.")
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
	require.Equal(t, "actions/cache/restore@immutable", steps[1].(map[string]any)["uses"])
	require.Equal(t, "existing-cache", steps[1].(map[string]any)["id"])
	require.Equal(t, "${{ inputs.save }}", steps[1].(map[string]any)["if"])
	require.Equal(t, "actions/cache@immutable", steps[3].(map[string]any)["uses"])
	save := steps[4].(map[string]any)
	require.Equal(t, "actions/cache/save@immutable", save["uses"])
	require.Contains(t, save["if"], "success()")
	require.Contains(t, save["if"], "steps.existing-cache.outputs.cache-hit != 'true'")
	require.Contains(t, save["if"], "steps.existing-cache.outcome == 'success'")
	require.Contains(t, save["if"], "steps.existing-cache.outputs.cache-primary-key != ''")
	require.NotContains(t, save["if"], "inputs.save")
	require.Equal(t, "${{ steps.existing-cache.outputs.cache-primary-key }}", save["with"].(map[string]any)["key"])
	require.Equal(t, "Clean Scripts", steps[5].(map[string]any)["name"])
	require.Equal(t, "Clean isolated runtime directories", steps[6].(map[string]any)["name"])
	require.Contains(t, steps[6].(map[string]any)["if"], "GH_AW_SETUP_ACTION_COMPLETE != 'true'")
	require.Equal(t, 1, strings.Count(actual, "actions/cache/save@immutable"))
}
