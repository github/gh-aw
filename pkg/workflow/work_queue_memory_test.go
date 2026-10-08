package workflow

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func workQueueMemoryFixture() map[string]any {
	return map[string]any{
		"path": "memory/snapshot.json", "target-repo": "owner/repo",
		"base-revision": strings.Repeat("a", 40), "branch-prefix": "memory/runs",
		"schema": map[string]any{"type": "object", "additionalProperties": false,
			"required": []any{"work_id"}, "properties": map[string]any{"work_id": map[string]any{"type": "string"}}},
	}
}

func memoryWorker(raw any) *WorkflowData {
	return &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true, "memory": raw}}}
}

func TestWorkQueueMemoryLowering(t *testing.T) {
	data := memoryWorker(workQueueMemoryFixture())
	require.NoError(t, validateWorkQueueConfiguration(data))
	script := data.SafeOutputs.Scripts["persist_work_queue_memory"]
	require.NotNil(t, script)
	require.Equal(t, 1, script.Max)
	require.Equal(t, "object", script.Inputs["memory"].Type)
	require.True(t, script.Inputs["memory"].Required)
	require.Equal(t, "git_tree", data.SafeOutputs.ClaimAdapters["persist_work_queue_memory"].EffectType)
	require.NoError(t, validateWorkQueueConfiguration(data))
	tool := generateCustomScriptToolDefinition("persist_work_queue_memory", script)
	schema := tool["inputSchema"].(map[string]any)
	require.Equal(t, script.MemorySchema, schema["properties"].(map[string]any)["memory"])
	config := map[string]any{}
	addSafeScriptsConfig(config, data.SafeOutputs.Scripts)
	require.Equal(t, 1, config["persist_work_queue_memory"].(map[string]any)["max"])
	require.Contains(t, generateSafeOutputScriptContent("persist_work_queue_memory", script), "prepareMemorySnapshot(item, JSON.parse(")
}

func TestWorkQueueMemoryValidation(t *testing.T) {
	for _, test := range []struct {
		name, field string
		value       any
	}{
		{"traversal", "path", "../memory.json"}, {"git-metadata", "path", ".GIT/memory.json"},
		{"control", "path", "memory\t.json"}, {"absolute", "path", "/memory.json"},
		{"mutable-base", "base-revision", "main"}, {"expression", "branch-prefix", "${{ inputs.prefix }}"},
		{"repository", "target-repo", "${{ github.repository }}"}, {"reserved", "name", "work_queue_read"},
		{"builtin", "name", "create_issue"}, {"maximum", "max-bytes", 262145}, {"zero", "max-bytes", 0},
		{"null-name", "name", nil}, {"null-limit", "max-bytes", nil},
		{"root-array-type", "schema", map[string]any{"type": []any{"object"}}},
		{"root-map-type", "schema", map[string]any{"type": map[string]any{}}},
		{"nested-array-type", "schema", map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": []any{"string"}}}}},
		{"ref", "schema", map[string]any{"type": "object", "$ref": "https://example.com/schema"}},
		{"regex", "schema", map[string]any{"type": "object", "pattern": ".*"}},
		{"schema-size", "schema", map[string]any{"type": "object", "description": strings.Repeat("a", 16384)}},
		{"metadata", "schema", map[string]any{"type": "object", "required": []any{1}}},
		{"literal-metadata", "schema", map[string]any{"type": "object", "description": "${{ github.actor }}"}},
		{"object-enum", "schema", map[string]any{"type": "object", "enum": []any{map[string]any{}}}},
		{"unknown", "validator", "return true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := workQueueMemoryFixture()
			raw[test.field] = test.value
			require.Error(t, configureWorkQueueMemory(memoryWorker(raw)))
		})
	}
	require.Error(t, configureWorkQueueMemory(memoryWorker(nil)))
	worker := memoryWorker(workQueueMemoryFixture())
	worker.Tools["work-queue"].(map[string]any)["worker"] = false
	require.ErrorContains(t, configureWorkQueueMemory(worker), "worker")
	ordinary := &WorkflowData{SafeOutputs: &SafeOutputsConfig{Scripts: map[string]*SafeScriptConfig{"custom": {Script: "return item;"}}}}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Nil(t, ordinary.SafeOutputs.ClaimAdapters)
	require.Len(t, ordinary.SafeOutputs.Scripts, 1)
}

func TestWorkQueueMemoryConflictsAndRepeatedValidation(t *testing.T) {
	raw := workQueueMemoryFixture()
	raw["name"] = "persist_memory"
	for _, outputs := range []*SafeOutputsConfig{
		{Scripts: map[string]*SafeScriptConfig{"persist-memory": {}}},
		{Jobs: map[string]*SafeJobConfig{"persist.memory": {}}},
		{Actions: map[string]*SafeOutputActionConfig{"persist_memory": {}}},
		{ClaimAdapters: map[string]*WorkQueueClaimAdapter{"persist_memory": {}}},
	} {
		data := memoryWorker(maps.Clone(raw))
		data.SafeOutputs = outputs
		require.ErrorContains(t, configureWorkQueueMemory(data), "conflicts")
	}
	for _, mutate := range []func(*WorkflowData){
		func(data *WorkflowData) { data.SafeOutputs.Scripts["persist_memory"].Script = "return item;" },
		func(data *WorkflowData) { data.SafeOutputs.ClaimAdapters["persist_memory"].TargetRepo = "foreign/repo" },
		func(data *WorkflowData) { data.SafeOutputs.Scripts["persist-memory"] = &SafeScriptConfig{} },
	} {
		data := memoryWorker(maps.Clone(raw))
		require.NoError(t, configureWorkQueueMemory(data))
		mutate(data)
		require.Error(t, configureWorkQueueMemory(data))
	}
}

func TestWorkQueueMemoryGeneratedWrapper(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated preparation wrapper")
	}
	data := memoryWorker(workQueueMemoryFixture())
	require.NoError(t, configureWorkQueueMemory(data))
	directory := t.TempDir()
	runtime, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	for _, name := range []string{"work_queue_memory.cjs", "sanitize_content.cjs"} {
		require.NoError(t, os.Symlink(filepath.Join(runtime, name), filepath.Join(directory, name)))
	}
	filename := filepath.Join(directory, "safe_output_script_persist_work_queue_memory.cjs")
	require.NoError(t, os.WriteFile(filename, []byte(generateSafeOutputScriptContent("persist_work_queue_memory", data.SafeOutputs.Scripts["persist_work_queue_memory"])), 0o600))
	assignment := `{"version":3,"dispatch_id":"d","request_id":"r","commit_id":"c","policy_epoch":"e","pool":"p","worker_profile":"w","claims":[{"handle":"h1","claim_id":"c1","work_id":"w1","work":{},"result_refs":[]}]}`
	assignmentFile := filepath.Join(directory, "assignment.json")
	require.NoError(t, os.WriteFile(assignmentFile, []byte(assignment), 0o400))
	command := exec.Command(node, "-e", `(async()=>{const execute=await require(process.argv[1]).main();process.stdout.write(JSON.stringify(await execute({memory:{work_id:"w1"}})));})().catch(error=>{console.error(error);process.exitCode=1;});`, filename)
	command.Env = append(os.Environ(), "GH_AW_CLAIM_ASSIGNMENT="+assignmentFile)
	output, err := command.Output()
	require.NoError(t, err)
	var prepared map[string]any
	require.NoError(t, json.Unmarshal(output, &prepared))
	require.Equal(t, []any{map[string]any{"path": "memory/snapshot.json", "content": "{\"work_id\":\"w1\"}\n"}}, prepared["files"])
}
