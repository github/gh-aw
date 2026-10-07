//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestPromptToolRequirements(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   []promptToolRequirement
	}{
		{"shell", "Use `shell(gh issue list)`.", []promptToolRequirement{{command: "gh issue list"}}},
		{"bash", "1. Please run Bash(npm test).", []promptToolRequirement{{command: "npm test"}}},
		{"bash prefix", "Use Bash(gh:*).", []promptToolRequirement{{command: "gh"}}},
		{"bash wildcard suffix", "Use Bash(jq *).", []promptToolRequirement{{command: "jq"}}},
		{"generic bash", "Use `bash`.", []promptToolRequirement{{server: "bash"}}},
		{"generic shell", "Call the shell tool.", []promptToolRequirement{{server: "bash"}}},
		{"inline", "- Run `some-custom-command --verify`.", []promptToolRequirement{{command: "some-custom-command --verify"}}},
		{"unquoted stem", "Run gh issue list.", []promptToolRequirement{{command: "gh issue list"}}},
		{"unquoted option", "Run jq --version.", []promptToolRequirement{{command: "jq --version"}}},
		{"qualified", "Call `mcp__github__issue_read`.", []promptToolRequirement{{server: "github", tool: "issue_read", nativeMCP: true}}},
		{"server", "Use the tool github(issue_read).", []promptToolRequirement{{server: "github", tool: "issue_read", nativeMCP: true}}},
		{"custom server", "Call inventory(lookup).", []promptToolRequirement{{server: "inventory", tool: "lookup", nativeMCP: true}}},
		{"hyphenated script", "Call mcp__mcpscripts__read-outcomes.", []promptToolRequirement{{server: "mcpscripts", tool: "read-outcomes", nativeMCP: true}}},
		{"bare github", "Use `issue_read` to inspect the issue.", []promptToolRequirement{{server: "github", tool: "issue_read"}}},
		{"bare unknown", "Use lookup to inspect the issue.", nil},
		{"native read", "Use Read(file).", []promptToolRequirement{{server: "native-read", tool: "Read"}}},
		{"native read path", "Call `Read(pkg/workflow/data.json)`.", []promptToolRequirement{{server: "native-read", tool: "Read"}}},
		{"ambiguous natural read", "Read pkg/workflow/data.json.", nil},
		{"native gemini read", "Use read_file(file).", nil},
		{"negation", "Do not use shell(curl).\nNever call mcp__github__issue_read.\nDon't run `curl example.com`.\nYou must not use github(issue_read).", nil},
		{"prose", "The shell(curl) tool is not available.\nExamples include mcp__github__issue_read.\nWe use github(issue_read) for issues.\nRun an analysis of the issue.", nil},
		{"comment", "<!--\nRun `curl example.com`.\n-->\n<!-- Call github(issue_read). -->", nil},
		{"quoted prose", "> Run `curl example.com`.", nil},
		{"example fence", "Example:\n```bash\ncurl example.com\n```", nil},
		{"task example fence", "Find examples of curl requests:\n```bash\ncurl example.com\n```", nil},
		{"task negative fence", "List commands that should not be used:\n```bash\ncurl example.com\n```", nil},
		{"task shell fence", "Find all schema files and list them:\n```bash\nfind schemas -name '*.json' | sort\n```", []promptToolRequirement{{command: "find schemas -name '*.json'", fullCommand: "find schemas -name '*.json' | sort"}, {command: "sort", fullCommand: "find schemas -name '*.json' | sort"}}},
		{"non shell fence", "Run the following:\n```json\n{\"tool\": \"shell(curl)\"}\n```", nil},
		{"negated fence", "Do not run:\n```bash\ncurl example.com\n```", nil},
		{"shell fence", "Run the following commands:\n\n```bash\n# Ignore this comment\njq '.items | map(.id)' items.json | sort\n```", []promptToolRequirement{{command: "jq '.items | map(.id)' items.json", fullCommand: "# Ignore this comment\njq '.items | map(.id)' items.json | sort"}, {command: "sort", fullCommand: "# Ignore this comment\njq '.items | map(.id)' items.json | sort"}}},
		{"multiline quote fence", "Run the following:\n```sh\njq '.items |\nmap(.id)' items.json\n# curl example.com\nsort items.json\n```", []promptToolRequirement{{command: "jq '.items |\nmap(.id)' items.json", fullCommand: "jq '.items |\nmap(.id)' items.json\n# curl example.com\nsort items.json"}, {command: "sort items.json", fullCommand: "jq '.items |\nmap(.id)' items.json\n# curl example.com\nsort items.json"}}},
		{"operators", "Run `echo 'a; b && c' && gh issue list`.", []promptToolRequirement{{command: "echo 'a; b && c'", fullCommand: "echo 'a; b && c' && gh issue list"}, {command: "gh issue list", fullCommand: "echo 'a; b && c' && gh issue list"}}},
		{"shell comment", "Run `gh issue list # curl example.com`.", []promptToolRequirement{{command: "gh issue list"}}},
		{"dynamic", "Run `$(choose-command) --verify`.", nil},
		{"unclosed quote", "Use shell(jq 'broken).", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, promptToolRequirements(tt.prompt))
		})
	}
}

func TestPromptToolAvailability(t *testing.T) {
	tests := []struct {
		name     string
		engine   string
		tools    map[string]any
		disabled bool
		request  promptToolRequirement
		want     bool
	}{
		{"absent shell", "copilot", nil, false, promptToolRequirement{command: "jq --version"}, false},
		{"false shell", "copilot", map[string]any{"bash": false}, false, promptToolRequirement{command: "jq --version"}, false},
		{"empty shell", "copilot", map[string]any{"bash": []any{}}, false, promptToolRequirement{command: "jq --version"}, false},
		{"true shell", "copilot", map[string]any{"bash": true}, false, promptToolRequirement{command: "jq --version"}, true},
		{"wildcard shell", "copilot", map[string]any{"bash": []any{"*"}}, false, promptToolRequirement{command: "jq --version"}, true},
		{"normalized wildcard", "copilot", map[string]any{"bash": []any{"jq *"}}, false, promptToolRequirement{command: "jq --version"}, true},
		{"subcommands", "claude", map[string]any{"bash": []any{"gh issue:*"}}, false, promptToolRequirement{command: "gh issue list"}, true},
		{"claude exact", "claude", map[string]any{"bash": []any{"git status"}}, false, promptToolRequirement{command: "git status"}, true},
		{"claude extra arguments", "claude", map[string]any{"bash": []any{"git status"}}, false, promptToolRequirement{command: "git status --short"}, false},
		{"claude normalized exact", "claude", map[string]any{"bash": []any{"git branch *"}}, false, promptToolRequirement{command: "git branch --show-current"}, false},
		{"claude bare executable exact", "claude", map[string]any{"bash": []any{"jq"}}, false, promptToolRequirement{command: "jq --version"}, false},
		{"claude bare colon prefix", "claude", map[string]any{"bash": []any{"jq:*"}}, false, promptToolRequirement{command: "jq --version"}, true},
		{"pi multiword exact", "pi", map[string]any{"bash": []any{"git status"}}, false, promptToolRequirement{command: "git status --short"}, false},
		{"pi trailing wildcard", "pi", map[string]any{"bash": []any{"git status *"}}, false, promptToolRequirement{command: "git status --short"}, true},
		{"restricted shell", "copilot", map[string]any{"bash": []any{"gh issue list"}}, false, promptToolRequirement{command: "gh pr list"}, false},
		{"command boundary", "copilot", map[string]any{"bash": []any{"git"}}, false, promptToolRequirement{command: "gitlab list"}, false},
		{"codex default", "codex", nil, false, promptToolRequirement{command: "jq --version"}, true},
		{"codex no per command restriction", "codex", map[string]any{"bash": []any{"echo"}}, false, promptToolRequirement{command: "jq --version"}, true},
		{"codex disabled", "codex", nil, true, promptToolRequirement{command: "jq --version"}, false},
		{"codex false", "codex", map[string]any{"bash": false}, false, promptToolRequirement{command: "jq --version"}, false},
		{"github absent", "copilot", nil, false, promptToolRequirement{server: "github", tool: "issue_read"}, false},
		{"github false", "copilot", map[string]any{"github": false}, false, promptToolRequirement{server: "github", tool: "issue_read"}, false},
		{"github defaults", "copilot", map[string]any{"github": map[string]any{}}, false, promptToolRequirement{server: "github", tool: "issue_read"}, true},
		{"github allowed", "copilot", map[string]any{"github": map[string]any{"allowed": []any{"issue_read"}}}, false, promptToolRequirement{server: "github", tool: "issue_read"}, true},
		{"github restricted", "copilot", map[string]any{"github": map[string]any{"allowed": []any{"list_issues"}}}, false, promptToolRequirement{server: "github", tool: "issue_read"}, false},
		{"github empty", "copilot", map[string]any{"github": map[string]any{"allowed": []any{}}}, false, promptToolRequirement{server: "github", tool: "issue_read"}, false},
		{"github wildcard toolset", "copilot", map[string]any{"github": map[string]any{"allowed": []any{"*"}, "toolsets": []any{"repos"}}}, false, promptToolRequirement{server: "github", tool: "issue_read"}, false},
		{"github all toolsets", "copilot", map[string]any{"github": map[string]any{"toolsets": []any{"all"}}}, false, promptToolRequirement{server: "github", tool: "search_issues"}, true},
		{"custom unrestricted", "copilot", map[string]any{"inventory": map[string]any{}}, false, promptToolRequirement{server: "inventory", tool: "lookup"}, true},
		{"custom empty", "copilot", map[string]any{"inventory": map[string]any{"allowed": []any{}}}, false, promptToolRequirement{server: "inventory", tool: "lookup"}, false},
		{"custom restricted", "copilot", map[string]any{"inventory": map[string]any{"allowed": []any{"lookup"}}}, false, promptToolRequirement{server: "inventory", tool: "delete"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{Tools: tt.tools, AI: tt.engine, BashDisabled: tt.disabled}
			engine, err := NewCompiler().getAgenticEngine(tt.engine)
			require.NoError(t, err)
			require.Equal(t, tt.want, promptToolAvailable(data, tt.request, engine.GetCapabilities()))
		})
	}
}

func TestPromptToolWarningsCountAndDeduplicate(t *testing.T) {
	data := &WorkflowData{
		Tools:            map[string]any{"bash": []any{"echo"}, "github": false},
		MarkdownContent:  "Run `curl example.com`.\nCall github(issue_read).\nUse mcp__github__issue_read.",
		ImportedMarkdown: "Use shell(curl example.com).",
		PromptImports:    []parser.PromptImportEntry{{Markdown: "Use shell(curl example.com)."}},
	}
	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() { compiler.validatePromptTools(data, "test.md") })
	require.Equal(t, 2, compiler.GetWarningCount())
	require.Equal(t, 2, strings.Count(output, "Prompt explicitly requires"))
	require.Contains(t, output, "test.md")
	require.Contains(t, output, "Align the prompt")
	require.Contains(t, output, "tools.bash")
	require.Contains(t, output, "tools.github.allowed")
	require.Equal(t, []any{"echo"}, data.Tools["bash"])
	require.Equal(t, false, data.Tools["github"])
}

func TestPromptToolCompilerIntegration(t *testing.T) {
	for _, tt := range []struct {
		name, config, prompt string
		want                 int
	}{
		{"restricted", "engine: copilot\ntools:\n  bash: [echo]\n", "Run `curl example.com`.\nUse shell(curl example.com).", 1},
		{"empty", "engine: claude\ntools:\n  bash: []\n", "Run `jq --version`.", 1},
		{"false", "engine: codex\ntools:\n  bash: false\n", "Run `jq --version`.", 1},
		{"codex default", "engine: codex\n", "Run `jq --version`.", 0},
		{"codex GitHub provider", "engine:\n  id: codex\n  model-provider: github\ntools:\n  bash: true\n", "Run `jq --version`.", 1},
		{"codex copilot model", "engine:\n  id: codex\n  model: copilot/gpt-5.3-codex\ntools:\n  bash: true\n", "Use `bash`.", 1},
		{"provider-disabled native read", "engine:\n  id: codex\n  model: copilot/gpt-5.3-codex\n", "Use `Read(pkg/workflow/data.json)`.", 1},
		{"codex OpenAI native read", "engine: codex\n", "Use `Read(pkg/workflow/data.json)`.", 0},
		{"allowed", "engine: copilot\ntools:\n  bash: [\"jq *\"]\n", "Run `jq '.items | map(.id)' file.json`.", 0},
		{"github disabled", "engine: copilot\ntools:\n  github: false\n", "Call mcp__github__issue_read.\nUse issue_read.", 1},
		{"github restricted", "engine: copilot\ntools:\n  github:\n    allowed: [list_issues]\n", "Call github(issue_read).", 1},
		{"github empty", "engine: copilot\ntools:\n  github:\n    allowed: []\n", "Call github(issue_read).", 1},
		{"github toolset", "engine: copilot\ntools:\n  github:\n    toolsets: [repos]\n", "Use issue_read.", 1},
		{"default sandbox shell", "engine: copilot\n", "Run jq --version.", 0},
		{"bash scope allowed", "engine: copilot\ntools:\n  bash: [gh]\n", "Use Bash(gh:*).", 0},
		{"bash scope denied", "engine: copilot\ntools:\n  bash: [echo]\n", "Use Bash(gh:*).", 1},
		{"CLI native mismatch", "engine: copilot\ntools:\n  cli-proxy: true\n  bash: [echo]\n", "Call mcp__github__issue_read.\nCall github(issue_read).", 1},
		{"CLI abstract tool", "engine: copilot\ntools:\n  cli-proxy: true\n  bash: [echo]\n", "Use issue_read.", 0},
		{"CLI shell grant", "engine: copilot\ntools:\n  cli-proxy: true\n  bash: [echo]\n", "Run `github issue_read`.", 0},
		{"native MCP retained", "engine: copilot\ntools:\n  cli-proxy: false\n  bash: [echo]\n", "Call mcp__github__issue_read.", 0},
		{"SDK exact denial", "engine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  bash: [\"git status\"]\n", "Run `git status --short`.", 1},
		{"SDK normalized exact denial", "engine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  bash: [\"git branch *\"]\n", "Run `git branch --show-current`.", 1},
		{"SDK colon prefix", "engine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  bash: [\"git status:*\"]\n", "Run `git status --short`.", 0},
		{"SDK exact chain denial", "engine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  bash: [\"git status\", echo]\n", "Run `git status && echo done`.", 1},
		{"SDK exact full chain", "engine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  bash: [\"git status && echo done\"]\n", "Run `git status && echo done`.", 0},
		{"Claude exact denial", "engine: claude\ntools:\n  bash: [\"git status\"]\n", "Run `git status --short`.", 1},
		{"Claude colon prefix", "engine: claude\ntools:\n  bash: [\"git status:*\"]\n", "Run `git status --short`.", 0},
		{"generic bash disabled", "engine: copilot\ntools:\n  bash: false\n", "Use `bash`.", 1},
		{"generic bash restricted", "engine: copilot\ntools:\n  bash: [echo]\n", "Use `bash`.", 0},
		{"safeoutput native mismatch", "engine: copilot\ntools:\n  cli-proxy: true\n  bash: [echo]\nsafe-outputs:\n  add-labels:\n", "Call mcp__safeoutputs__add_labels.", 1},
		{"safeoutput wrapper instruction", "engine: copilot\ntools:\n  cli-proxy: true\n  bash: [echo]\nsafe-outputs:\n  add-labels:\n", "Use add_labels to label the issue.", 0},
		{"safeoutput native retained", "engine: copilot\ntools:\n  cli-proxy: false\n  bash: [echo]\nsafe-outputs:\n  add-labels:\n", "Call mcp__safeoutputs__add_labels.", 0},
		{"task fence pipeline denied", "engine: copilot\ntools:\n  bash: [find]\n", "Find all schema files and list them:\n```bash\nfind schemas -name '*.json' | schema-sort\n```", 1},
		{"task fence pipeline allowed", "engine: copilot\ntools:\n  bash: [find, schema-sort]\n", "Find all schema files and list them:\n```bash\nfind schemas -name '*.json' | schema-sort\n```", 0},
		{"script configured", "engine: codex\nmcp-scripts:\n  read-outcomes:\n    description: Read outcomes\n    run: echo outcomes\n", "Call mcp__mcpscripts__read-outcomes.", 0},
		{"script missing", "engine: codex\nmcp-scripts:\n  read-outcomes:\n    description: Read outcomes\n    run: echo outcomes\n", "Call mcp__mcpscripts__read-other.", 1},
		{"codex GitHub configured script", "engine:\n  id: codex\n  model: copilot/gpt-5.3-codex\ntools:\n  bash: false\n  cli-proxy: false\nmcp-scripts:\n  read-outcomes:\n    description: Read outcomes\n    run: echo outcomes\n", "Call mcp__mcpscripts__read-outcomes.", 0},
		{"native read", "engine: claude\n", "Use Read(file).", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "prompt-tools")
			path := filepath.Join(dir, ".github", "workflows", "test.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
			content := "---\non: workflow_dispatch\nstrict: false\n" + tt.config + "---\n" + tt.prompt + "\n"
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			compiler := NewCompiler()
			var err error
			output := testutil.CaptureStderr(t, func() { err = compiler.CompileWorkflow(path) })
			require.NoError(t, err)
			require.Equal(t, tt.want, strings.Count(output, "Prompt explicitly requires"), output)
			if tt.name == "CLI native mismatch" {
				require.Contains(t, output, "use the github CLI instead")
				require.Contains(t, output, "disable tools.cli-proxy")
			}
			if tt.want > 0 && (strings.HasPrefix(tt.name, "codex GitHub") || tt.name == "codex copilot model") {
				require.Contains(t, output, "native shell is disabled for GitHub-backed Codex")
				require.Contains(t, output, "explicitly configured MCP tool")
				require.NotContains(t, output, "allow the specific command")
			}
			if tt.name == "provider-disabled native read" {
				require.Contains(t, output, "native reads are unavailable for GitHub-backed Codex")
				require.Contains(t, output, "explicitly configured MCP tool")
			}
			require.GreaterOrEqual(t, compiler.GetWarningCount(), tt.want)
			_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
		})
	}

}

func TestPromptToolTaskFenceSortDenied(t *testing.T) {
	data := &WorkflowData{
		AI:              "copilot",
		Tools:           map[string]any{"bash": []any{"find"}},
		MarkdownContent: "Find all schema files and list them:\n```bash\nfind schemas -name '*.json' | sort\n```",
	}
	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() { compiler.validatePromptTools(data, "test.md") })
	require.Equal(t, 1, compiler.GetWarningCount())
	require.Contains(t, output, "shell(sort)")
}

func TestPromptToolEngineCapabilities(t *testing.T) {
	request := promptToolRequirement{command: "jq --version"}
	data := &WorkflowData{AI: "custom"}
	require.True(t, promptToolAvailable(data, request, EngineCapabilities{}), "unknown allowlist behavior should not imply denial")
	require.False(t, promptToolAvailable(data, request, EngineCapabilities{BashCommandAllowlist: true}))
	data.BashDisabled = true
	require.False(t, promptToolAvailable(data, request, EngineCapabilities{BashDisable: true}))
	require.True(t, promptToolAvailable(data, request, EngineCapabilities{}), "an unenforceable disable configuration is not a runtime denial")
}

func TestPromptToolCustomCLITransport(t *testing.T) {
	data := &WorkflowData{
		AI:           "copilot",
		EngineConfig: &EngineConfig{ID: "copilot"},
		Tools: map[string]any{
			"cli-proxy": true,
			"bash":      []any{"echo"},
			"inventory": map[string]any{"command": "echo", "args": []any{}, "allowed": []any{"lookup"}},
		},
		MarkdownContent: "Call mcp__inventory__lookup.\nCall inventory(lookup).",
	}
	data.ParsedTools = NewTools(data.Tools)
	require.NoError(t, data.ParsedTools.ParseError())
	require.Contains(t, getMCPCLIExcludeFromAgentConfig(data), "inventory")
	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() { compiler.validatePromptTools(data, "test.md") })
	require.Equal(t, 1, compiler.GetWarningCount())
	require.Contains(t, output, "use the inventory CLI instead")
	data.Tools["cli-proxy"] = false
	data.ParsedTools = NewTools(data.Tools)
	compiler.ResetWarningCount()
	output = testutil.CaptureStderr(t, func() { compiler.validatePromptTools(data, "test.md") })
	require.Empty(t, output)
	require.Zero(t, compiler.GetWarningCount())
}

func TestPromptToolRuntimeImportsConfinedAndCommentAware(t *testing.T) {
	dir := testutil.TempDir(t, "prompt-tool-runtime")
	prompts := filepath.Join(dir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(prompts, 0755))
	mainPath := filepath.Join(dir, ".github", "workflows", "main.md")
	files := map[string]string{
		"direct.md":    "Run `curl example.com`.\n{{#runtime-import .github/prompts/nested.md}}\n<!-- {{#runtime-import .github/prompts/commented.md}} -->",
		"nested.md":    "Run `curl example.com`.\n{{#runtime-import .github/prompts/direct.md}}",
		"commented.md": "Call github(issue_read).",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(prompts, name), []byte(body), 0600))
	}
	outside := filepath.Join(dir, "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("Call github(issue_read)."), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(prompts, "link.md")))
	data := &WorkflowData{
		MarkdownContent: "{{#runtime-import .github/prompts/direct.md}}\n{{#runtime-import .github/prompts/link.md}}\n{{#runtime-import ../outside.md}}",
		Tools:           map[string]any{"bash": false, "github": false},
	}
	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() { compiler.validatePromptTools(data, mainPath) })
	require.Equal(t, 1, compiler.GetWarningCount())
	require.Contains(t, output, "shell(curl example.com)")
	require.NotContains(t, output, "github(issue_read)")
}

func TestPromptToolCompilerImports(t *testing.T) {
	for _, tt := range []struct {
		name, importedTools, imports string
		want                         int
	}{
		{"runtime denied", "", "imports:\n  - shared.md\n", 1},
		{"merged permission", "tools:\n  bash: [curl]\n", "imports:\n  - shared.md\n", 0},
		{"compile time expanded", "inputs:\n  command:\n    type: string\n    default: curl\n", "imports:\n  - path: shared.md\n    inputs:\n      command: curl\n", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "prompt-tool-imports")
			workflows := filepath.Join(dir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflows, 0755))
			body := "Run `curl example.com`.\n"
			if tt.name == "compile time expanded" {
				body = "Run `${{ github.aw.inputs.command }} example.com`.\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(workflows, "shared.md"), []byte("---\n"+tt.importedTools+"---\n"+body), 0600))
			main := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\ntools:\n  bash: [echo]\n" + tt.imports + "---\n# Main\n"
			path := filepath.Join(workflows, "main.md")
			require.NoError(t, os.WriteFile(path, []byte(main), 0600))
			compiler := NewCompiler()
			var err error
			output := testutil.CaptureStderr(t, func() { err = compiler.CompileWorkflow(path) })
			require.NoError(t, err)
			require.Equal(t, tt.want, strings.Count(output, "Prompt explicitly requires"), output)
		})
	}
}
