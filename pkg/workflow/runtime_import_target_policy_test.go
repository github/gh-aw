//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeImportRejectsNonLocalTargets(t *testing.T) {
	for _, target := range []string{
		"https://example.com/file.md", "HTTPS://example.com/file.md",
		"https://raw.githubusercontent.com/org/repo/main/file.md",
		"https://127.0.0.1/file.md", "https://[::1]/file.md",
		"https://169.254.169.254/latest/meta-data",
		"https:example.com/file.md",
		"https://example.com/${{ github.actor }}",
		"https://example.com/${GH_AW_EXPR_1234ABCD}",
		"https://example.com/__GH_AW_EXPERIMENTS_STYLE__",
		"http://example.com/file.md", "HTTP://example.com/file.md",
		"http://127.0.0.1/file.md", "http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd", "ftp://example.com/file.md", "data:text/plain,content",
		"//example.com/file.md", `\\example.com\file.md`,
		"http://example.com/file.md?token=query-secret#fragment-secret",
		"https://example.com/file.md?token=query-secret#fragment-secret",
		"wss://example.com/file.md", "git+https://example.com/file.md",
	} {
		t.Run(target, func(t *testing.T) {
			for _, macro := range []string{"runtime-import", "runtime-import?", "import", "import?", "import?:"} {
				_, err := validateRuntimeImportFiles("{{#"+macro+" "+target+"}}", t.TempDir())
				require.ErrorContains(t, err, "Runtime imports require local files")
				require.ErrorContains(t, err, "inside the AWF sandbox")
				require.NotContains(t, err.Error(), "query-secret")
				require.NotContains(t, err.Error(), "fragment-secret")
			}
		})
	}
}

func TestRuntimeImportRejectsURLCredentialsWithoutDisclosure(t *testing.T) {
	for _, macro := range []string{"runtime-import", "runtime-import?", "import", "import?:"} {
		for _, target := range []string{
			"https:example.com/file.md",
			"https://user:password-secret@example.com/file.md",
			"https://example.com/${{ github.actor }}",
			"https://example.com/?q=${{ env.MY_VAR }}",
			"https://example.com/${GH_AW_EXPR_1234ABCD}",
			"https://example.com/__GH_AW_EXPERIMENTS_STYLE__",
		} {
			_, err := validateRuntimeImportFiles("{{#"+macro+" "+target+"}}", t.TempDir())
			require.ErrorContains(t, err, "Runtime imports require local files")
			require.NotContains(t, err.Error(), "password-secret")
		}
	}
}

func TestCompilerRejectsURLRuntimeImports(t *testing.T) {
	for _, mode := range []string{"runtime", "inline", "inlined-imports"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(directory, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "outer.md"), []byte("{{#runtime-import? https://example.com/nested.md}}"), 0o600))
			agentsDirectory := filepath.Join(root, ".agents")
			require.NoError(t, os.MkdirAll(agentsDirectory, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(agentsDirectory, "outer.md"), []byte("{{#import?: https://example.com/nested.md:1-3}}"), 0o600))
			config := ""
			if mode == "inlined-imports" {
				config = "inlined-imports: true\n"
			}
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			compiler.inlinePrompt = mode == "inline"
			for _, body := range []string{
				"{{#runtime-import http://example.com/direct.md}}",
				"{{#runtime-import https://example.com/static.md}}",
				"{{#runtime-import? HTTPS://example.com/static.md:1-3}}",
				"{{#if false}}\n{{#runtime-import? https://example.com/hidden.md}}\n{{/if}}",
				"{{#runtime-import outer.md}}",
				"{{#runtime-import .github/workflows/outer.md}}",
				"{{#runtime-import //.github/workflows/outer.md}}",
				"{{#runtime-import /.agents/outer.md}}",
			} {
				file := filepath.Join(directory, "test.md")
				source := "---\non: workflow_dispatch\nengine: copilot\n" + config + "---\n" + body
				require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
				require.ErrorContains(t, compiler.CompileWorkflow(file), "Runtime imports require local files")
			}
			require.NoError(t, os.WriteFile(filepath.Join(directory, "test.md"), []byte("---\non: workflow_dispatch\nengine: copilot\n"+config+"---\n{{#runtime-import https://example.com/${{ github.actor }}}}"), 0o600))
			require.ErrorContains(t, compiler.CompileWorkflow(filepath.Join(directory, "test.md")), "Runtime imports require local files")
			source := "---\non: workflow_dispatch\nengine: copilot\n" + config + "---\nFetch https://example.com/static.md inside the AWF sandbox."
			require.NoError(t, os.WriteFile(filepath.Join(directory, "test.md"), []byte(source), 0o600))
			require.NoError(t, compiler.CompileWorkflow(filepath.Join(directory, "test.md")))
		})
	}
}
