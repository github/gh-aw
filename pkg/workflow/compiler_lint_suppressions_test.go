package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/require"
)

func TestCompilerLintCommentSuppression(t *testing.T) {
	const source = "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      # gh-aw: ignore[template-injection] -- Reviewed false positive\n" +
		"      - run: echo '${{ github.event.comment.body }}'\n"
	compiler := NewCompiler(WithLint(true))
	require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, source, "test.md"))
	require.ErrorContains(t, compiler.lintCompiledWorkflow(&WorkflowData{}, strings.ReplaceAll(source, "template-injection]", "typo-rule]"), "test.md"), "invalid lint suppression")

	document, err := aw.Parse("---\nsafe-outputs:\n  create-issue:\n    # gh-aw: ignore[allowed-labels] -- Reviewed false positive\n    allowed-labels: ['*']\n---\nPrompt.\n")
	require.NoError(t, err)
	safe := strings.ReplaceAll(source, "${{ github.event.comment.body }}", "public")
	require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{SourceDocument: document}, safe, "test.md"))
}

func TestCompilerLintCommentsDoNotBypassStrictPermissions(t *testing.T) {
	const source = "---\non: workflow_dispatch\nstrict: true\n" +
		"# gh-aw: ignore[permission-scope] -- Reviewed false positive\npermissions: write-all\n" +
		"tools: {github: false}\n---\nPrompt.\n"
	file := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
	compiler := NewCompiler(WithLint(true))
	compiler.SetSkipValidation(true)
	require.ErrorContains(t, compiler.CompileWorkflow(file), "strict mode: write permission")
}

func TestCompilerLintOrdinaryGHAWDescription(t *testing.T) {
	source := "---\ndescription: 'gh-aw: daily triage'\non: workflow_dispatch\nstrict: false\n" +
		"tools: {github: false}\n---\nPrompt.\n"
	file := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
	compiler := NewCompiler(WithLint(true))
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(file))
	content, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
	require.NoError(t, err)
	require.Contains(t, string(content), "# gh-aw: daily triage")
}

func TestCompilerLintSuppressedMCPImageArgument(t *testing.T) {
	const source = "---\nmcp-servers:\n  fixture:\n" +
		"    # gh-aw: ignore[image-argument] -- Reviewed MCP image\n    container: '-fixture'\n---\nPrompt.\n"
	document, err := aw.Parse(source)
	require.NoError(t, err)
	const actions = "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo public\n"
	compiler := NewCompiler(WithLint(true))
	require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{SourceDocument: document}, actions, "test.md"))
	document, err = aw.Parse(strings.ReplaceAll(source, "    # gh-aw: ignore[image-argument] -- Reviewed MCP image\n", ""))
	require.NoError(t, err)
	require.ErrorContains(t, compiler.lintCompiledWorkflow(&WorkflowData{SourceDocument: document}, actions, "test.md"), "[image-argument]")
}
