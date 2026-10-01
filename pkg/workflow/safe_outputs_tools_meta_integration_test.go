//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolsMetaJSONContainsDescriptionSuffixes verifies that the compiled workflow YAML
// embeds a tools_meta.json with the correct description suffixes instead of the large
// inlined tools.json used in the old approach.
func TestToolsMetaJSONCompiledWorkflowEmbedsMeta(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-meta-compiled-test")

	testContent := `---
on: push
name: Test Compiled Meta
engine: copilot
safe-outputs:
  create-issue:
    max: 2
    title-prefix: "[auto] "
    labels:
      - generated
  create-pull-request:
    max: 1
    allowed-repos:
      - org/other-repo
  missing-tool: {}
  noop: {}
---

Test workflow to verify tools_meta in compiled output.
`

	testFile := filepath.Join(tmpDir, "test-compiled-meta.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "should write test file")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile), "compilation should succeed")

	lockFile := filepath.Join(tmpDir, "test-compiled-meta.lock.yml")
	yamlBytes, err := os.ReadFile(lockFile)
	require.NoError(t, err, "should read lock file")
	yamlStr := string(yamlBytes)

	// Structural checks: new strategy is in use
	assert.Contains(t, yamlStr, "GH_AW_TOOLS_META_JSON", "lock file should contain tools_meta env var")
	assert.Contains(t, yamlStr, "generate_safe_outputs_tools.cjs", "lock file should invoke JS generator")
	assert.NotContains(t, yamlStr, `cat > ${RUNNER_TEMP}/gh-aw/safeoutputs/tools.json`, "lock file should NOT inline tools.json")
	assert.NotContains(t, yamlStr, "node ${RUNNER_TEMP}/gh-aw/actions/generate_safe_outputs_tools.cjs", "lock file should NOT use direct node invocation")

	// tools_meta.json must contain create_issue description suffix with constraint text
	assert.Contains(t, yamlStr, "CONSTRAINTS:", "tools_meta should contain constraint text")
	assert.Contains(t, yamlStr, "Maximum 2 issue", "tools_meta should contain max constraint for create_issue")
	assert.Contains(t, yamlStr, `[auto]`, "tools_meta should contain title prefix for create_issue")
	assert.Contains(t, yamlStr, `generated`, "tools_meta should contain label for create_issue")

	// tools_meta.json must contain repo_params for create_pull_request (has allowed-repos)
	assert.Contains(t, yamlStr, `"repo_params"`, "tools_meta should contain repo_params section")

	meta := extractToolsMetaFromLockFile(t, yamlStr)

	// Verify tools without constraints do NOT get description suffixes
	_, hasMissingTool := meta.DescriptionSuffixes["missing_tool"]
	assert.False(t, hasMissingTool, "missing_tool should not have a description suffix")
	_, hasNoop := meta.DescriptionSuffixes["noop"]
	assert.False(t, hasNoop, "noop should not have a description suffix")
}

// TestToolsMetaJSONPushRepoMemory verifies that push_repo_memory appears in
// description_suffixes when repo-memory is configured.
func TestToolsMetaJSONEmptyWhenNoSafeOutputs(t *testing.T) {
	data := &WorkflowData{SafeOutputs: nil}

	metaJSON, err := generateToolsMetaJSON(data, ".github/workflows/test.md")
	require.NoError(t, err, "generateToolsMetaJSON should not error for nil safe-outputs")

	var meta ToolsMeta
	require.NoError(t, json.Unmarshal([]byte(metaJSON), &meta), "meta JSON should parse")

	assert.Empty(t, meta.DescriptionSuffixes, "description_suffixes should be empty when no safe-outputs")
	assert.Empty(t, meta.RepoParams, "repo_params should be empty when no safe-outputs")
	assert.Empty(t, meta.DynamicTools, "dynamic_tools should be empty when no safe-outputs")
}

func extractToolsMetaFromLockFile(t *testing.T, yamlStr string) ToolsMeta {
	t.Helper()

	// Find the GH_AW_TOOLS_META_JSON env var literal block scalar
	marker := "GH_AW_TOOLS_META_JSON: |\n"
	start := strings.Index(yamlStr, marker)
	require.NotEqual(t, -1, start, "lock file should contain GH_AW_TOOLS_META_JSON env var")
	contentStart := start + len(marker)

	// Extract content lines from the YAML literal block scalar (12-space indented)
	const indent = "            " // 12 spaces
	var sb strings.Builder
	for _, line := range strings.Split(yamlStr[contentStart:], "\n") {
		if !strings.HasPrefix(line, indent) {
			break // End of literal block scalar content
		}
		sb.WriteString(strings.TrimPrefix(line, indent))
		sb.WriteString("\n")
	}

	var meta ToolsMeta
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(sb.String())), &meta),
		"GH_AW_TOOLS_META_JSON from lock file should be valid JSON")
	return meta
}

func TestBuiltinLedgerSmokeCompiledTools(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-builtin-ledgers.md")
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "smoke-builtin-ledgers.md")
	require.NoError(t, os.WriteFile(path, source, 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(path))

	compiled, err := os.ReadFile(filepath.Join(dir, "smoke-builtin-ledgers.lock.yml"))
	require.NoError(t, err)
	meta := extractToolsMetaFromLockFile(t, string(compiled))
	names := make(map[string]map[string]any)
	for _, tool := range meta.DynamicTools {
		names[tool["name"].(string)] = tool
	}
	for name, ledgerType := range map[string]string{
		"ledger_map_put": "map", "ledger_map_delete": "map",
		"ledger_work_pool_submit": "work-pool", "ledger_work_pool_cancel": "work-pool",
		"ledger_work_pool_acquire": "work-pool", "ledger_work_pool_acquire_next": "work-pool",
		"ledger_work_pool_finish": "work-pool", "ledger_work_pool_abandon": "work-pool",
	} {
		require.Contains(t, names, name)
		assert.Equal(t, ledgerType, names[name]["_ledger_type"])
		assert.NotEmpty(t, names[name]["_ledger_operation"])
	}
}

// TestToolsMetaJSONCompiledWorkflowZeroArgCustomJob verifies that a custom safe-output job
// with no inputs compiles to a dynamic_tools entry with an empty properties schema and no
// required key. This protects the compiler/bridge contract: the bridge must pass {} through
// to MCP tools/call rather than treating it as a schema-discovery probe.
func TestToolsMetaJSONCompiledWorkflowZeroArgCustomJob(t *testing.T) {
	tmpDir := testutil.TempDir(t, "zero-arg-job-compiled-test")

	// Inputs are intentionally omitted — the supported way to declare a zero-input custom job.
	testContent := `---
on: push
name: Test Zero-Arg Custom Job
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  jobs:
    dispatch-code-factory:
      description: "Record a dispatch code factory safe-output item"
      runs-on: ubuntu-latest
      steps:
        - name: dispatch
          run: echo "dispatched"
---

Test workflow to verify zero-argument custom job schema.
`
	testFile := filepath.Join(tmpDir, "test-zero-arg-job.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "should write test file")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile), "compilation should succeed")

	lockFile := filepath.Join(tmpDir, "test-zero-arg-job.lock.yml")
	yamlBytes, err := os.ReadFile(lockFile)
	require.NoError(t, err, "should read lock file")

	meta := extractToolsMetaFromLockFile(t, string(yamlBytes))

	// Find the dispatch_code_factory entry in dynamic_tools
	var dispatchTool map[string]any
	for _, tool := range meta.DynamicTools {
		if tool["name"] == "dispatch_code_factory" {
			dispatchTool = tool
			break
		}
	}
	require.NotNil(t, dispatchTool, "dynamic_tools should contain dispatch_code_factory")

	schema, ok := dispatchTool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be a map")

	assert.Equal(t, "object", schema["type"], "schema type should be object")

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")
	assert.Empty(t, props, "properties should be empty for a zero-input tool")

	_, hasRequired := schema["required"]
	assert.False(t, hasRequired, "required key must be absent for a zero-input tool")

	assert.Equal(t, false, schema["additionalProperties"], "additionalProperties should be false")
}

// constraint-bearing configurations to ensure no tool type regresses silently.
