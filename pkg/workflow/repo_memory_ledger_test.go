//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoMemoryLedgerBareYAMLKey(t *testing.T) {
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte("on: workflow_dispatch\nengine: copilot\ntools:\n  repo-memory:\n    ledger:\n"), &frontmatter))
	require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md"))

	toolsMap, ok := frontmatter["tools"].(map[string]any)
	require.True(t, ok)
	tools, err := ParseToolsConfig(toolsMap)
	require.NoError(t, err)
	config, err := NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.NoError(t, err)
	require.Len(t, config.Memories, 1)
	assert.Equal(t, &RepoMemoryLedgerConfig{}, config.Memories[0].Ledger)
}

func TestRepoMemoryLedgerMCPSetup(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"repo-memory": map[string]any{"ledger": map[string]any{}}},
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{
			ID: "default", Ledger: &RepoMemoryLedgerConfig{},
		}}},
	}
	var yaml strings.Builder
	require.NoError(t, NewCompiler().generateMCPSetup(&yaml, data.Tools, NewClaudeEngine(), data))
	assert.Contains(t, yaml.String(), `"ledger": {`)
	assert.Contains(t, yaml.String(), `GH_AW_MEMORY_DIR: /tmp/gh-aw/repo-memory/default`)
	assert.Contains(t, yaml.String(), `ledger_mcp_server.cjs`)
	assert.NotContains(t, yaml.String(), "GH_AW_LEDGER_SCHEMA")
}

func TestRepoMemoryLedgerSchema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		memory any
		valid  bool
	}{
		{"empty object", map[string]any{"ledger": map[string]any{}}, true},
		{"null value", map[string]any{"ledger": nil}, true},
		{"schema object", map[string]any{"ledger": map[string]any{"schema": "schemas/events.json"}}, true},
		{"array entry", []any{map[string]any{"id": "events", "ledger": map[string]any{}}}, true},
		{"invalid value", map[string]any{"ledger": true}, false},
		{"invalid field", map[string]any{"ledger": map[string]any{"typo": true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch", "engine": "copilot", "tools": map[string]any{"repo-memory": tc.memory},
			}, "workflow.md")
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	tools, err := ParseToolsConfig(map[string]any{"repo-memory": []any{
		map[string]any{"id": "../other", "ledger": map[string]any{}},
	}})
	require.NoError(t, err)
	_, err = NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.ErrorContains(t, err, "memory id")

	tools, err = ParseToolsConfig(map[string]any{"repo-memory": []any{
		map[string]any{"id": "first", "ledger": map[string]any{}},
		map[string]any{"id": "second", "ledger": map[string]any{}},
	}})
	require.NoError(t, err)
	_, err = NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.ErrorContains(t, err, "exactly one repo-memory entry")

	tools, err = ParseToolsConfig(map[string]any{"repo-memory": []any{
		map[string]any{"id": "plain"},
		map[string]any{"id": "events", "ledger": map[string]any{}},
	}})
	require.NoError(t, err)
	_, err = NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.ErrorContains(t, err, "exactly one repo-memory entry")
}

func TestRepoMemoryLedgerGeneratedMCPAndPrompt(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"repo-memory": map[string]any{}},
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{
			ID: "events", Ledger: &RepoMemoryLedgerConfig{Schema: "schemas/events.json"},
		}}},
	}
	assert.Contains(t, collectMCPTools(data), "ledger")
	env := collectMCPEnvironmentVariables(data.Tools, collectMCPTools(data), data, false)
	assert.Equal(t, "/tmp/gh-aw/repo-memory/events", env["GH_AW_MEMORY_DIR"])
	assert.Equal(t, "schemas/events.json", env["GH_AW_LEDGER_SCHEMA"])
	assert.NotContains(t, env, "GH_AW_LEDGER_SCHEMA_ROOT")

	var setup strings.Builder
	require.NoError(t, NewCompiler().generateMCPSetup(&setup, data.Tools, NewClaudeEngine(), data))
	assert.NotContains(t, setup.String(), "npm ci")

	var restore strings.Builder
	generateRepoMemorySteps(&restore, data)
	assert.NotContains(t, restore.String(), "Checkout trusted ledger schema")

	var rendered strings.Builder
	NewMCPConfigRenderer(MCPRendererOptions{
		Format: "json", IncludeCopilotFields: true, IsLast: true,
	}).RenderLedgerMCP(&rendered, data)
	assert.Contains(t, rendered.String(), `"ledger": {`)
	assert.Contains(t, rendered.String(), `ledger_mcp_server.cjs`)
	assert.Contains(t, rendered.String(), constants.DefaultGhAwMount)
	assert.Contains(t, rendered.String(), constants.DefaultTmpGhAwMount)
	assert.Contains(t, rendered.String(), `\${GH_AW_MEMORY_DIR}`)
	assert.Contains(t, rendered.String(), `\${GH_AW_LEDGER_SCHEMA}`)
	assert.NotContains(t, rendered.String(), "GH_AW_LEDGER_SCHEMA_ROOT")

	rendered.Reset()
	NewMCPConfigRenderer(MCPRendererOptions{Format: "toml"}).RenderLedgerMCP(&rendered, data)
	assert.Contains(t, rendered.String(), "[mcp_servers.ledger]")
	assert.Contains(t, rendered.String(), constants.DefaultGhAwMount)
	assert.Contains(t, rendered.String(), constants.DefaultTmpGhAwMount)

	sections := NewCompiler().collectPromptSections(data)
	assert.Contains(t, sections, PromptSection{Content: "Use `ledger_append`, `ledger_query`, `ledger_get`, and `ledger_status` to record and inspect structured events. Do not edit ledger storage directly."})
}

func TestRepoMemoryLedgerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ledger any
		schema string
	}{
		{"null enabled defaults", nil, ""},
		{"defaults", map[string]any{}, ""},
		{"schema", map[string]any{"schema": "schemas/events.schema.json"}, "schemas/events.schema.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, err := ParseToolsConfig(map[string]any{"repo-memory": map[string]any{"ledger": tc.ledger}})
			require.NoError(t, err)
			config, err := NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
			require.NoError(t, err)
			require.Len(t, config.Memories, 1)
			require.NotNil(t, config.Memories[0].Ledger)
			assert.Equal(t, tc.schema, config.Memories[0].Ledger.Schema)
			assert.Equal(t, "memory/ledger-test", config.Memories[0].BranchName)
		})
	}

	tools, err := ParseToolsConfig(map[string]any{"repo-memory": []any{
		map[string]any{"id": "events", "ledger": map[string]any{}},
	}})
	require.NoError(t, err)
	config, err := NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.NoError(t, err)
	assert.NotNil(t, config.Memories[0].Ledger)
}

func TestRepoMemoryLedgerPersistsNestedShards(t *testing.T) {
	tools, err := ParseToolsConfig(map[string]any{"repo-memory": map[string]any{
		"file-glob":          []any{"*.md"},
		"allowed-extensions": []any{".md", ".jsonl"},
		"ledger":             map[string]any{},
	}})
	require.NoError(t, err)
	config, err := NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.NoError(t, err)
	assert.Equal(t, []string{"*.md", "ledger/shards/*.jsonl"}, config.Memories[0].FileGlob)
	assert.Contains(t, config.Memories[0].AllowedExtensions, ".jsonl")

	tools, err = ParseToolsConfig(map[string]any{"repo-memory": map[string]any{
		"allowed-extensions": []any{".md"},
		"ledger":             map[string]any{},
	}})
	require.NoError(t, err)
	_, err = NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
	require.ErrorContains(t, err, "requires .jsonl in allowed-extensions")
}

func TestRepoMemoryLedgerRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"boolean", true},
		{"unknown", map[string]any{"unknown": true}},
		{"schema type", map[string]any{"schema": 2}},
		{"empty", map[string]any{"schema": ""}},
		{"absolute", map[string]any{"schema": "/etc/passwd"}},
		{"traversal", map[string]any{"schema": "../secret.json"}},
		{"nested traversal", map[string]any{"schema": "schemas/../secret.json"}},
		{"windows path", map[string]any{"schema": `C:\secret.json`}},
		{"backslash", map[string]any{"schema": `schemas\events.json`}},
		{"expression", map[string]any{"schema": "${{ inputs.schema }}"}},
		{"newline", map[string]any{"schema": "events\n.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, err := ParseToolsConfig(map[string]any{"repo-memory": map[string]any{"ledger": tc.value}})
			require.NoError(t, err)
			_, err = NewCompiler().extractRepoMemoryConfig(tools, "ledger-test")
			require.ErrorContains(t, err, "tools.repo-memory.ledger")
		})
	}
}
