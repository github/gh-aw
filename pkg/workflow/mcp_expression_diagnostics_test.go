//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPExpressionCompatibilityDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, field, value string
		wantError          bool
		wantWarnings       int
	}{
		{"ordinary expression", "url", "https://example.test/${{ github.ref }}", false, 0},
		{"simple runner", "url", "https://example.test/$PORT", false, 0},
		{"gateway only", "headers", `Bearer \${TOKEN}`, false, 0},
		{"legacy secret", "headers", "Bearer ${{ secrets.TOKEN }}", false, 0},
		{"mixed Actions expressions", "headers", "${{ secrets.TOKEN }}:${{ github.ref }}", false, 0},
		{"legacy and runner", "env", "${{ secrets.TOKEN }}:$GITHUB_RUN_ID", false, 0},
		{"expression and gateway", "headers", `${{ github.ref }}:\${TOKEN}`, true, 0},
		{"runner and gateway", "env", `$GITHUB_RUN_ID:\${TOKEN}`, true, 0},
		{"argument and gateway", "args", `${{ github.ref }}:\${TOKEN}`, true, 0},
		{"mount and gateway", "mounts", `${{ github.workspace }}/\${CACHE}:/cache`, true, 0},
		{"entrypoint argument and gateway", "entrypointArgs", `${{ github.ref }}:\${TOKEN}`, true, 0},
		{"certificate path and runner", "ca-cert-file", "${{ github.workspace }}/$CERT_FILE", true, 0},
		{"certificate path compatibility", "ca-cert-file", "${{ github.workspace }}/cert.pem", false, 0},
		{"potential boundary assembly is dynamic", "args", `$${{ github.ref }}{TOKEN}`, false, 0},
		{"computed default", "env", "${WORKSPACE_FALLBACK:-/tmp}", false, 1},
		{"parameter length", "args", "${#TOKEN}", false, 1},
		{"indirect parameter", "args", "${!TOKEN}", false, 1},
		{"command computation", "url", "https://example.test/$(compute)", false, 1},
		{"arithmetic computation", "entrypointArgs", "$((1+2))", false, 1},
		{"backtick computation", "args", "`compute`", false, 1},
		{"special shell parameter", "args", "$?", false, 1},
		{"escaped command text", "args", `\$(compute)`, false, 0},
		{"escaped backtick text", "args", "\\`compute\\`", false, 0},
		{"escaped default text", "args", `\${TOKEN:-default}`, false, 0},
		{"expression literal is runtime data", "args", "${{ format('$(literal)-{0}', github.ref) }}", false, 0},
		{"no warning for ordinary dollars", "args", "price $USD", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var value any = test.value
			switch test.field {
			case "headers", "env":
				value = map[string]any{"VALUE": value}
			case "args", "entrypointArgs", "mounts":
				value = []any{value}
			}
			warnings, err := mcpConfigExpressionDiagnostics("example", map[string]any{test.field: value})
			if test.wantError {
				require.ErrorContains(t, err, "cannot mix transported")
				assert.Contains(t, err.Error(), "mcp-servers.example."+test.field)
				assert.NotContains(t, err.Error(), "TOKEN", "diagnostics must not echo credential names or values")
			} else {
				require.NoError(t, err)
			}
			require.Len(t, warnings, test.wantWarnings)
			for _, warning := range warnings {
				assert.Contains(t, warning, "mcp-servers.example."+test.field)
				assert.Contains(t, warning, "setup step")
				assert.NotContains(t, warning, "TOKEN")
				assert.NotContains(t, warning, "WORKSPACE_FALLBACK")
			}
		})
	}
}

func TestMCPCompatibilityDiagnosticsOrderingAndPrivacy(t *testing.T) {
	t.Parallel()
	config := map[string]any{
		"container": "example/mcp:latest",
		"args":      []string{"$(private-argument)"},
		"env":       map[string]string{"Z": "${PRIVATE_Z:-secret-z}", "A": "${PRIVATE_A:-secret-a}"},
	}
	warnings, err := mcpConfigExpressionDiagnostics("example", config)
	require.NoError(t, err)
	require.Len(t, warnings, 3)
	assert.Contains(t, warnings[0], ".args[0]:")
	assert.Contains(t, warnings[1], ".env.A:")
	assert.Contains(t, warnings[2], ".env.Z:")
	assert.NotContains(t, strings.Join(warnings, "\n"), "PRIVATE")
	assert.NotContains(t, strings.Join(warnings, "\n"), "secret-")
}

func TestMCPCompatibilityDiagnosticsCompilerPaths(t *testing.T) {
	for _, api := range []string{"file", "string"} {
		for _, imported := range []bool{false, true} {
			for _, invalid := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/imported=%t/invalid=%t", api, imported, invalid), func(t *testing.T) {
					dir := t.TempDir()
					path := filepath.Join(dir, "workflow.md")
					value := "${WORKSPACE_FALLBACK:-/tmp}"
					if invalid {
						value = `${{ github.ref }}:\${PRIVATE_CREDENTIAL}`
					}
					mcp := "mcp-servers:\n  example:\n    type: http\n    url: https://example.test/mcp\n    headers:\n      X-Value: '" + value + "'\n"
					if imported {
						require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\n"+mcp+"---\n"), 0600))
						mcp = "imports:\n  - shared.md\n"
					}
					source := "---\non: push\nstrict: false\npermissions:\n  contents: read\n" + mcp + "---\nInspect the configured tool.\n"
					require.NoError(t, os.WriteFile(path, []byte(source), 0600))
					compiler := NewCompiler()
					var compileErr error
					stderr := testutil.CaptureStderr(t, func() {
						if api == "file" {
							compileErr = compiler.CompileWorkflow(path)
						} else {
							data, err := compiler.ParseWorkflowString(source, path)
							compileErr = err
							if err == nil {
								_, compileErr = compiler.CompileToYAML(data, path)
							}
						}
					})
					if invalid {
						require.ErrorContains(t, compileErr, "mcp-servers.example.headers.X-Value")
						assert.Contains(t, compileErr.Error(), "startup would reject")
						assert.NotContains(t, compileErr.Error(), "PRIVATE_CREDENTIAL")
						require.NoFileExists(t, filepath.Join(dir, "workflow.lock.yml"))
					} else {
						require.NoError(t, compileErr)
						assert.Contains(t, stderr, "mcp-servers.example.headers.X-Value")
						assert.Equal(t, 1, strings.Count(stderr, "shell computations and parameter operators remain literal"))
						assert.Positive(t, compiler.GetWarningCount())
						assert.NotContains(t, stderr, "WORKSPACE_FALLBACK")
					}
				})
			}
		}
	}
}
