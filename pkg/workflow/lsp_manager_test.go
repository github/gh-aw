//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPManagerValidate(t *testing.T) {
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {
			Command: "typescript-language-server",
			Args:    []string{"--stdio"},
			FileExtensions: map[string]string{
				".ts": "typescript",
			},
		},
	})
	require.NoError(t, manager.Validate())

	invalid := NewLSPManager(map[string]LSPServerConfig{
		"python": {
			Command: "pyright-langserver",
		},
	})
	require.Error(t, invalid.Validate())
}

func TestLSPManagerDuplicateKeyNormalization(t *testing.T) {
	tests := []struct {
		name     string
		servers  map[string]LSPServerConfig
		expected string
	}{
		{
			name: "uppercase sorts before lowercase",
			servers: map[string]LSPServerConfig{
				"TypeScript": {
					Command: "first-server",
					FileExtensions: map[string]string{
						".ts": "typescript",
					},
				},
				"typescript": {
					Command: "second-server",
					FileExtensions: map[string]string{
						".ts": "typescript",
					},
				},
			},
			expected: "first-server",
		},
		{
			name: "lexicographically first lowercase variant wins",
			servers: map[string]LSPServerConfig{
				"TYPESCRIPT": {
					Command: "second-server",
					FileExtensions: map[string]string{
						".ts": "typescript",
					},
				},
				"typescript": {
					Command: "first-server",
					FileExtensions: map[string]string{
						".ts": "typescript",
					},
				},
			},
			expected: "second-server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewLSPManager(tt.servers)
			servers := manager.CopilotLSPServers()
			require.Len(t, servers, 1)
			assert.Equal(t, tt.expected, servers["typescript"].Command)
		})
	}
}

func TestLSPManagerGenerateInstallSteps(t *testing.T) {
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {
			Command: "typescript-language-server",
			FileExtensions: map[string]string{
				".ts": "typescript",
			},
		},
		"unknown": {
			Command: "my-lsp",
			FileExtensions: map[string]string{
				".foo": "foo",
			},
		},
	})

	// Default: scripts disabled + cooldown enabled + pinned versions
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "Install TypeScript LSP dependencies")
	assert.Contains(t, content, "npm install -g typescript@5.8.3 typescript-language-server@4.3.3")
	assert.NotContains(t, content, "--ignore-scripts")
	assert.Contains(t, content, fmt.Sprintf("        env:\n          NPM_CONFIG_IGNORE_SCRIPTS: 'true'\n          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays))
}

func TestLSPManagerGenerateInstallSteps_RunInstallScripts(t *testing.T) {
	// When run-install-scripts is enabled, script suppression must be omitted.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {
			Command: "typescript-language-server",
			FileExtensions: map[string]string{
				".ts": "typescript",
			},
		},
	})
	workflowData := &WorkflowData{
		RunInstallScripts: true,
	}
	steps := manager.GenerateInstallSteps(workflowData)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "npm install -g typescript@5.8.3 typescript-language-server@4.3.3")
	assert.NotContains(t, content, "--ignore-scripts")
	assert.NotContains(t, content, "NPM_CONFIG_IGNORE_SCRIPTS")
	assert.Contains(t, content, fmt.Sprintf("        env:\n          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays))
}

func TestLSPManagerGenerateInstallSteps_CooldownDisabled(t *testing.T) {
	// When runtimes.node.cooldown: false, NPM_CONFIG_MIN_RELEASE_AGE must not appear.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"yaml": {
			Command: "yaml-language-server",
			FileExtensions: map[string]string{
				".yaml": "yaml",
			},
		},
	})
	falseVal := false
	workflowData := &WorkflowData{
		ParsedFrontmatter: &FrontmatterConfig{
			RuntimesTyped: &RuntimesConfig{
				Node: &RuntimeConfig{Cooldown: &falseVal},
			},
		},
	}
	steps := manager.GenerateInstallSteps(workflowData)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "        env:\n          NPM_CONFIG_IGNORE_SCRIPTS: 'true'")
	assert.NotContains(t, content, "--ignore-scripts")
	assert.NotContains(t, content, "NPM_CONFIG_MIN_RELEASE_AGE")
}

func TestLSPManagerGenerateInstallSteps_RunInstallScriptsAndCooldownDisabled(t *testing.T) {
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {
			Command:        "typescript-language-server",
			FileExtensions: map[string]string{".ts": "typescript"},
		},
	})
	falseVal := false
	workflowData := &WorkflowData{
		RunInstallScripts: true,
		ParsedFrontmatter: &FrontmatterConfig{
			RuntimesTyped: &RuntimesConfig{
				Node: &RuntimeConfig{Cooldown: &falseVal},
			},
		},
	}
	steps := manager.GenerateInstallSteps(workflowData)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "npm install -g typescript@5.8.3 typescript-language-server@4.3.3")
	assert.NotContains(t, content, "--ignore-scripts")
	assert.NotContains(t, content, "NPM_CONFIG_IGNORE_SCRIPTS")
	assert.NotContains(t, content, "NPM_CONFIG_MIN_RELEASE_AGE")
	assert.NotContains(t, content, "        env:")
}

func TestLSPManagerGenerateInstallSteps_DefaultVersionPinning(t *testing.T) {
	// Default pinned versions are injected for known npm-based servers.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"yaml": {
			Command:        "yaml-language-server",
			FileExtensions: map[string]string{".yaml": "yaml"},
		},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "yaml-language-server@1.15.0")
}

func TestLSPManagerGenerateInstallSteps_VersionOverride(t *testing.T) {
	// A Version field in the frontmatter overrides the pinned default for the primary package.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {
			Command:        "typescript-language-server",
			Version:        "5.0.0",
			FileExtensions: map[string]string{".ts": "typescript"},
		},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	// typescript-language-server is the primary (last) package; its version is overridden.
	assert.Contains(t, content, "typescript-language-server@5.0.0")
	// typescript (secondary package) retains its default version.
	assert.Contains(t, content, "typescript@5.8.3")
}

func TestLSPManagerGenerateInstallSteps_GoDefaultVersion(t *testing.T) {
	// gopls is installed with its pinned default version.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"go": {Command: "gopls", FileExtensions: map[string]string{".go": "go"}},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "go install golang.org/x/tools/gopls@v0.18.1")
}

func TestLSPManagerGenerateInstallSteps_GoVersionOverride(t *testing.T) {
	// A Version field overrides gopls install version.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"go": {Command: "gopls", Version: "0.17.0", FileExtensions: map[string]string{".go": "go"}},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "go install golang.org/x/tools/gopls@v0.17.0")
}

func TestLSPManagerGenerateInstallSteps_GoVersionOverride_VPrefix(t *testing.T) {
	// A Version field with a leading 'v' prefix must not produce '@vv...' in the command.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"go": {Command: "gopls", Version: "v0.17.0", FileExtensions: map[string]string{".go": "go"}},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "go install golang.org/x/tools/gopls@v0.17.0")
	assert.NotContains(t, content, "@vv")
}

func TestLSPManagerGenerateInstallSteps_RubyDefaultVersion(t *testing.T) {
	// solargraph is installed with its pinned default version.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"ruby": {Command: "solargraph", FileExtensions: map[string]string{".rb": "ruby"}},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "gem install solargraph -v 0.50.0")
}

func TestLSPManagerGenerateInstallSteps_RubyVersionOverride(t *testing.T) {
	// A Version field overrides solargraph gem install version.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"ruby": {Command: "solargraph", Version: "0.48.0", FileExtensions: map[string]string{".rb": "ruby"}},
	})
	steps := manager.GenerateInstallSteps(nil)
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "gem install solargraph -v 0.48.0")
}

func TestLSPManagerRuntimeRequirements_NodeBased(t *testing.T) {
	// Node.js-based LSP servers (typescript, python/pyright, bash, php, yaml) should all
	// resolve to the "node" runtime — deduplicated to a single requirement.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"typescript": {Command: "typescript-language-server", FileExtensions: map[string]string{".ts": "typescript"}},
		"python":     {Command: "pyright-langserver", FileExtensions: map[string]string{".py": "python"}},
	})
	reqs := manager.RuntimeRequirements()
	require.Len(t, reqs, 1, "typescript and python both need node; expect exactly one node requirement")
	assert.Equal(t, "node", reqs[0].Runtime.ID)
}

func TestLSPManagerRuntimeRequirements_GoLSP(t *testing.T) {
	// gopls requires the Go runtime.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"go": {Command: "gopls", FileExtensions: map[string]string{".go": "go"}},
	})
	reqs := manager.RuntimeRequirements()
	require.Len(t, reqs, 1)
	assert.Equal(t, "go", reqs[0].Runtime.ID)
}

func TestLSPManagerRuntimeRequirements_RubyLSP(t *testing.T) {
	// solargraph requires the Ruby runtime.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"ruby": {Command: "solargraph", FileExtensions: map[string]string{".rb": "ruby"}},
	})
	reqs := manager.RuntimeRequirements()
	require.Len(t, reqs, 1)
	assert.Equal(t, "ruby", reqs[0].Runtime.ID)
}

func TestLSPManagerRuntimeRequirements_RustLSP(t *testing.T) {
	// rust-analyzer uses rustup; Rust is not in knownRuntimes so no runtime requirement is returned.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"rust": {Command: "rust-analyzer", FileExtensions: map[string]string{".rs": "rust"}},
	})
	reqs := manager.RuntimeRequirements()
	assert.Empty(t, reqs, "Rust is not in knownRuntimes; expect no runtime requirement")
}

func TestLSPManagerRuntimeRequirements_UnknownLanguage(t *testing.T) {
	// A language not in lspInstallSpecs contributes no runtime requirement.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"cobol": {Command: "cobol-lsp", FileExtensions: map[string]string{".cbl": "cobol"}},
	})
	reqs := manager.RuntimeRequirements()
	assert.Empty(t, reqs)
}

func TestLSPManagerRuntimeRequirements_MultipleRuntimes(t *testing.T) {
	// A workflow with both a Go LSP and a TypeScript LSP needs both Go and Node.js.
	manager := NewLSPManager(map[string]LSPServerConfig{
		"go":         {Command: "gopls", FileExtensions: map[string]string{".go": "go"}},
		"typescript": {Command: "typescript-language-server", FileExtensions: map[string]string{".ts": "typescript"}},
	})
	reqs := manager.RuntimeRequirements()
	require.Len(t, reqs, 2)
	ids := map[string]bool{}
	for _, r := range reqs {
		ids[r.Runtime.ID] = true
	}
	assert.True(t, ids["go"], "expected go runtime requirement")
	assert.True(t, ids["node"], "expected node runtime requirement")
}

func TestDetectRuntimeRequirements_LSPServers(t *testing.T) {
	// DetectRuntimeRequirements should pick up runtime requirements from LSP config.
	data := &WorkflowData{
		LSP: map[string]LSPServerConfig{
			"go": {Command: "gopls", FileExtensions: map[string]string{".go": "go"}},
		},
	}
	reqs := DetectRuntimeRequirements(data)
	found := false
	for _, r := range reqs {
		if r.Runtime.ID == "go" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected Go runtime requirement from LSP config")
}
