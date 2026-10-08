//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestPiSubagentValidation(t *testing.T) {
	for _, test := range []struct {
		name        string
		frontmatter string
		driver      string
		wantError   string
	}{
		{"bare model", "description: Read files\nmodel: claude-haiku-4.5", "", ""},
		{"alias model", "description: Read files\nmodel: small", "", ""},
		{"inherited model", "description: Read files", "", ""},
		{"SDK mode", "description: Read files", "pi_agent_core_driver.cjs", ""},
		{"RPC mode", "description: Read files", "pi_rpc_driver.cjs", ""},
		{"missing description", "model: small", "", "description"},
		{"invalid model type", "description: Read files\nmodel: 123", "", "non-empty string"},
		{"different provider", "description: Read files\nmodel: anthropic/claude-haiku-4-5", "", "parent's provider"},
		{"glob", "description: Read files\nmodel: '*haiku*'", "", "literal"},
		{"wrong name", "description: Read files\nname: writer", "", "marker"},
		{"unsupported parameter", "description: Read files\nmodel: small?temperature=0", "", "effort"},
		{"unsupported effort", "description: Read files\nmodel: small?effort=invalid", "", "thinking effort"},
		{"invalid tools", "description: Read files\ntools: {read: true}", "", "list of tool names"},
		{"tool list", "description: Read files\ntools: [read, grep]", "", ""},
		{"custom driver", "description: Read files", "custom.cjs", "custom driver"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{
				Model:        "copilot/gpt-5.6-luna",
				EngineConfig: &EngineConfig{ID: "pi", Driver: test.driver},
				SubAgents:    []parser.InlineSubAgent{{Name: "reader", Content: "---\n" + test.frontmatter + "\n---\nRead."}},
			}
			err := validatePiSubagents(data)
			if test.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantError)
			}
		})
	}
}

func TestPiSubagentsFromImports(t *testing.T) {
	dir := t.TempDir()
	shared := "# Shared\n\n## agent: `reader`\n---\ndescription: Read files\nmodel: small\n---\nRead.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
	workflow := "---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\nengine: pi\nimports:\n  - shared.md\n---\n# Work\nDelegate to reader.\n"
	source := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(source, []byte(workflow), 0600))
	data, err := NewCompiler().ParseWorkflowFile(source)
	require.NoError(t, err)
	require.Len(t, data.SubAgents, 1)
	require.Equal(t, "reader", data.SubAgents[0].Name)
	require.NoError(t, validatePiSubagents(data))
	generated, err := NewCompiler().CompileToYAML(data, source)
	require.NoError(t, err)
	require.Contains(t, generated, "GH_AW_INFO_SUB_AGENT_MODELS")
	require.Contains(t, generated, `"name\":\"reader\"`)
	require.NoError(t, os.WriteFile(source, []byte(workflow+shared), 0600))
	_, err = NewCompiler().ParseWorkflowFile(source)
	require.ErrorContains(t, err, "defined more than once")
}

func TestPiSubagentArgumentsAreIsolated(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "pi", Bare: true, Config: `{"session":{"enabled":true,"id":"parent"}}`,
			Args: []string{"--session", "parent.jsonl", "--mode", "rpc", "--extension", "custom.cjs"}},
		Tools: map[string]any{"bash": false, "edit": false},
	}
	env := make(map[string]string)
	NewPiEngine().applyPiConfigEnv(env, data)
	args := env["GH_AW_PI_SUBAGENT_ARGS"]
	require.Contains(t, args, `"--no-session"`)
	require.Contains(t, args, `"--no-context-files"`)
	require.Contains(t, args, `"edit,write"`)
	require.NotContains(t, args, "parent.jsonl")
	require.NotContains(t, args, "rpc")
	require.NotContains(t, args, "custom.cjs")
	require.True(t, piSessionSettings(data).Enabled, "child isolation must not mutate the parent configuration")
}

func TestPiCompiledDisabledBash(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "workflow.md")
	content := "---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\nengine: pi\ntools:\n  bash: false\n  cli-proxy: false\n  edit: false\n---\n# Work\nReturn a short answer.\n"
	require.NoError(t, os.WriteFile(source, []byte(content), 0600))
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(source)
	require.NoError(t, err)
	require.True(t, data.BashDisabled)
	require.NotContains(t, data.Tools, "bash", "normalization removes the disabled tool")
	generated, err := compiler.CompileToYAML(data, source)
	require.NoError(t, err)
	require.Contains(t, generated, `GH_AW_PI_TOOL_POLICY: '{"bash":false,"edit":false}'`)
	require.Contains(t, generated, `"--exclude-tools","bash"`)
	require.Contains(t, generated, "--exclude-tools bash")
	data.EngineConfig.Driver = "custom.cjs"
	delete(data.Tools, "edit")
	require.ErrorContains(t, compiler.validatePiEngineConfig(data), "tool restrictions require")
}

func TestPiInlineActivationTargets(t *testing.T) {
	registry := GetGlobalEngineRegistry()
	for _, engine := range registry.GetSupportedEngines() {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: engine}, MarkdownContent: "## agent: `reader`\nRead."}
			var yaml strings.Builder
			(&Compiler{}).generateInterpolationAndTemplateStep(&yaml, nil, data, false)
			require.Contains(t, yaml.String(), `GH_AW_SUB_AGENT_DIR: "`+GetEngineSubAgentDir(engine)+`"`)
			require.Contains(t, yaml.String(), `GH_AW_SKILL_DIR: "`+GetEngineSkillDir(engine)+`"`)
			require.Contains(t, yaml.String(), `GH_AW_SUB_AGENT_EXT: "`+parser.GetEngineSubAgentExt(engine)+`"`)
		})
	}
	require.Equal(t, ".pi/agents", GetEngineSubAgentDir("pi"))
	require.Equal(t, ".pi/skills", GetEngineSkillDir("pi"))
	require.Equal(t, ".md", parser.GetEngineSubAgentExt("pi"))
}
