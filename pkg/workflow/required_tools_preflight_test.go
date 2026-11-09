//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestRequiredPromptTools(t *testing.T) {
	for _, prompt := range []string{
		"Use github(issue_read) or github(list_issues).",
		"Use github(issue_read) if available.",
		"Never call github(issue_read).",
		"> Call github(issue_read).",
		"Example:\n```markdown\nCall github(issue_read).\n```",
		"<!-- Call github(issue_read). -->",
		"Run `gh issue list`.",
		"Use len(items) to compute the count.",
		"Call issue_read or list_issues.",
	} {
		require.Empty(t, requiredPromptTools(prompt, nil), prompt)
	}
	require.Len(t, requiredPromptTools("Call github(issue_read) and github(list_issues).", nil), 2)
	require.Len(t, requiredPromptTools("Call github(issue_read) to inspect open or closed issues.", nil), 1)
	require.Len(t, requiredPromptTools("Call mcp__inventory__lookup.", nil), 1)
	require.Len(t, requiredPromptTools("Call inventory(lookup).", map[string]any{"inventory": nil}), 1)
}

func TestRequiredPromptToolImports(t *testing.T) {
	data := &WorkflowData{
		AI: "copilot",
		Tools: map[string]any{"github": map[string]any{
			"mode": "local", "toolsets": []any{"issues"}, "allowed": []any{"issue_read"},
		}},
		MarkdownContent:  "Use issue_read.",
		ImportedMarkdown: "Call github(issue_read).",
		PromptImports:    []parser.PromptImportEntry{{Markdown: "Use issue_read."}},
	}
	compiler := NewCompiler()
	require.NoError(t, compiler.validateRequiredPromptTools(data))
	require.Equal(t, map[string][]string{"github": {"issue_read"}}, data.RequiredMCPTools)
	data.ImportedMarkdown = "Call github(list_issues)."
	require.ErrorContains(t, compiler.validateRequiredPromptTools(data), "github(list_issues)")
	require.Equal(t, []any{"issue_read"}, data.Tools["github"].(map[string]any)["allowed"])
}

func TestRequiredPromptToolGHProxy(t *testing.T) {
	data := &WorkflowData{
		AI: "copilot",
		Tools: map[string]any{"github": map[string]any{
			"mode": "gh-proxy",
		}},
		MarkdownContent: "Use issue_read.",
	}
	require.ErrorContains(t, NewCompiler().validateRequiredPromptTools(data), "github(issue_read)")
}

func TestRequiredPromptToolMissingFrameworkServer(t *testing.T) {
	data := &WorkflowData{MarkdownContent: "Call agenticworkflows(status)."}
	require.ErrorContains(t, NewCompiler().validateRequiredPromptTools(data), "agenticworkflows(status)")
	data.Tools = map[string]any{"agentic-workflows": nil}
	require.NoError(t, NewCompiler().validateRequiredPromptTools(data))
}

func TestRequiredPromptToolsRuntimeEnvironment(t *testing.T) {
	dir := testutil.TempDir(t, "required-tools")
	path := filepath.Join(dir, ".github", "workflows", "test.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	content := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\ntools:\n  cli-proxy: false\n  github:\n    mode: local\n    toolsets: [issues]\n    allowed: [issue_read]\n---\nUse issue_read.\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(path))
	lock, err := os.ReadFile(filepath.Join(filepath.Dir(path), "test.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(lock), `GH_AW_REQUIRED_MCP_TOOLS: "{\"github\":[\"issue_read\"]}"`)
}

func TestReportedRequiredToolsExposure(t *testing.T) {
	for _, tt := range []struct {
		workflow string
		tools    []string
	}{
		{"metrics-collector", []string{"actions_list", "list_discussions"}},
		{"smoke-crush", []string{"list_pull_requests", "pull_request_read"}},
		{"smoke-codex", []string{"list_issue_fields", "list_pull_requests", "pull_request_read"}},
	} {
		t.Run(tt.workflow, func(t *testing.T) {
			lock, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", tt.workflow+".lock.yml"))
			require.NoError(t, err)
			manifest, err := ExtractGHAWManifestFromLockFile(string(lock))
			require.NoError(t, err)
			require.NotNil(t, manifest)
			var exposed []string
			for _, server := range manifest.MCPServers {
				if server.Name == "github" {
					exposed = server.Tools
				}
			}
			for _, tool := range tt.tools {
				require.Contains(t, exposed, tool)
			}
		})
	}
	source, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "smoke-copilot-aoai-entra.md"))
	require.NoError(t, err)
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(string(source), "---", 3)[1]), &frontmatter))
	safeOutputs := frontmatter["safe-outputs"].(map[string]any)
	comment := safeOutputs["add-comment"].(map[string]any)
	require.Equal(t, true, comment["discussions"])
}
