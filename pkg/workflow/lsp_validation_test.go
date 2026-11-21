//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateLSPSupport(t *testing.T) {
	compiler := NewCompiler()

	validLSP := map[string]LSPServerConfig{
		"typescript": {
			Command: "typescript-language-server",
			Args:    []string{"--stdio"},
			FileExtensions: map[string]string{
				".ts": "typescript",
			},
		},
	}

	t.Run("copilot engine accepts lsp", func(t *testing.T) {
		err := compiler.validateLSPSupport(&WorkflowData{
			AI:  "copilot",
			LSP: validLSP,
		})
		require.NoError(t, err)
	})

	t.Run("non-copilot engine rejects lsp", func(t *testing.T) {
		err := compiler.validateLSPSupport(&WorkflowData{
			AI:  "codex",
			LSP: validLSP,
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "only supported for engine: copilot")
	})

	t.Run("invalid lsp config fails validation", func(t *testing.T) {
		err := compiler.validateLSPSupport(&WorkflowData{
			AI: "copilot",
			LSP: map[string]LSPServerConfig{
				"python": {
					Command: "",
					FileExtensions: map[string]string{
						".py": "python",
					},
				},
			},
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "lsp.python.command is required")
	})
}

func TestValidateTypeScriptLSPLockedVersion(t *testing.T) {
	const unsupported = "0.0.0"
	for _, version := range []string{"", typescriptLSPVersion, "v" + typescriptLSPVersion, unsupported} {
		t.Run(version, func(t *testing.T) {
			config := LSPServerConfig{
				Command:        "typescript-language-server",
				FileExtensions: map[string]string{".ts": "typescript"},
				Version:        version,
			}
			data := &WorkflowData{LSP: map[string]LSPServerConfig{"TypeScript": config}}
			err := NewCompiler().validateLSPSupport(data)
			if version == unsupported {
				require.ErrorContains(t, err, "lsp.typescript.version")
				require.ErrorContains(t, err, `version: "`+typescriptLSPVersion+`"`)
				require.ErrorContains(t, err, "lsp.typescript-custom")
				data.LSP = map[string]LSPServerConfig{"typescript-custom": config}
				require.NoError(t, NewCompiler().validateLSPSupport(data))
				require.Empty(t, NewLSPManager(data.LSP).GenerateInstallSteps(data))
				return
			}
			require.NoError(t, err)
		})
	}
}
