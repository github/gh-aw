//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPluginSHA = "1f181b37d3fe5862ab590648f25a292e345b5de6"

func TestValidatePlugins(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))

	for _, plugin := range []string{
		"octo-org/agent-plugin@main",
		"octo-org/agent-plugins/plugins/example@v1.2.3",
		"octo-org/agent-plugin@" + testPluginSHA,
	} {
		t.Run(plugin, func(t *testing.T) {
			require.NoError(t, compiler.validatePlugins(&WorkflowData{Plugins: []string{plugin}}))
		})
	}

	for _, test := range []struct {
		name   string
		plugin string
		error  string
	}{
		{name: "missing ref", plugin: "octo-org/agent-plugin", error: "expected owner/repository[/path]@ref"},
		{name: "empty ref", plugin: "octo-org/agent-plugin@", error: "expected owner/repository[/path]@ref"},
		{name: "local path", plugin: "./plugins/example", error: "expected owner/repository[/path]@ref"},
		{name: "expression", plugin: "${{ inputs.plugin }}", error: "expected owner/repository[/path]@ref"},
		{name: "truncated SHA", plugin: "octo-org/agent-plugin@1f181b3", error: "truncated or malformed commit SHA"},
		{name: "unsafe ref", plugin: "octo-org/agent-plugin@main..next", error: "unsupported characters"},
		{name: "path traversal segment", plugin: "octo-org/agent-plugin/../evil@main", error: "must not contain '.' or '..' segments"},
		{name: "current-dir traversal segment", plugin: "octo-org/agent-plugin/./evil@main", error: "must not contain '.' or '..' segments"},
		{name: "uppercase SHA", plugin: "octo-org/agent-plugin@" + strings.ToUpper(testPluginSHA), error: "must be lowercase hexadecimal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := compiler.validatePlugins(&WorkflowData{Plugins: []string{test.plugin}})
			require.ErrorContains(t, err, test.error)
		})
	}
}

func TestValidatePluginsMergesOverlappingVersions(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{Plugins: []string{
		"octo-org/agent-plugin@v1.2.0",
		"octo-org/another-plugin@main",
		"octo-org/agent-plugin@v1.3.0",
		"octo-org/agent-plugin@v1",
	}}

	require.NoError(t, compiler.validatePlugins(data))
	assert.Equal(t, []string{
		"octo-org/agent-plugin@v1.3.0",
		"octo-org/another-plugin@main",
	}, data.Plugins)
}

func TestValidatePluginsMergesThreeOverlappingVersionsToHighest(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{Plugins: []string{
		"org/plugin@v1.0.0",
		"org/plugin@v1.3.0",
		"org/plugin@v1.2.0",
	}}

	require.NoError(t, compiler.validatePlugins(data))
	assert.Equal(t, []string{"org/plugin@v1.3.0"}, data.Plugins)
}

func TestValidatePluginsRejectsOverlappingVersionConflicts(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))

	err := compiler.validatePlugins(&WorkflowData{Plugins: []string{
		"octo-org/agent-plugin@v1",
		"octo-org/agent-plugin@v2",
	}})
	require.ErrorContains(t, err, `plugin "octo-org/agent-plugin" is declared with incompatible semantic versions "v1" and "v2"`)

	err = compiler.validatePlugins(&WorkflowData{Plugins: []string{
		"octo-org/agent-plugin@main",
		"octo-org/agent-plugin@stable",
	}})
	require.ErrorContains(t, err, `plugin "octo-org/agent-plugin" is declared with conflicting refs "main" and "stable"`)
}

func TestValidatePluginSupport(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))

	copilot := NewCopilotEngine()
	assert.True(t, copilot.GetCapabilities().Plugins)
	_, implementsInstaller := any(copilot).(PluginInstallationProvider)
	assert.True(t, implementsInstaller)

	require.NoError(t, compiler.validatePluginSupport(&WorkflowData{
		AI:      "copilot",
		Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
	}))

	claude := NewClaudeEngine()
	assert.True(t, claude.GetCapabilities().Plugins)
	_, implementsInstaller = any(claude).(PluginInstallationProvider)
	assert.True(t, implementsInstaller)

	require.NoError(t, compiler.validatePluginSupport(&WorkflowData{
		AI:      "claude",
		Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
	}))

	codex := NewCodexEngine()
	assert.True(t, codex.GetCapabilities().Plugins)
	_, implementsInstaller = any(codex).(PluginInstallationProvider)
	assert.True(t, implementsInstaller)

	require.NoError(t, compiler.validatePluginSupport(&WorkflowData{
		AI:      "codex",
		Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
	}))

	err := compiler.validatePluginSupport(&WorkflowData{
		AI:      "gemini",
		Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
	})
	require.ErrorContains(t, err, `plugins are not supported by engine "gemini"`)
}

func TestPluginsEmitExperimentalWarning(t *testing.T) {
	expectedMessage := "Using experimental feature: plugins"

	for _, test := range []struct {
		name          string
		plugins       []string
		expectWarning bool
	}{
		{name: "plugins declared", plugins: []string{"octo-org/agent-plugin@" + testPluginSHA}, expectWarning: true},
		{name: "no plugins declared", expectWarning: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion("dev"))
			compiler.SetBatchMode(true)
			compiler.emitExperimentalFeatureWarnings(&WorkflowData{Plugins: test.plugins})

			if test.expectWarning {
				assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[expectedMessage])
				assert.Positive(t, compiler.GetWarningCount())
				return
			}
			assert.Zero(t, compiler.GetExperimentalFeatureUsage()[expectedMessage])
		})
	}
}

func TestCompileWorkflowRejectsImportedPluginsOnUnsupportedEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "unsupported-imported-plugin")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
plugins:
  - octo-org/agent-plugin@`+testPluginSHA+`
---

Shared plugin configuration.
`), 0o644))

	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: gemini
imports:
  - shared.md
---

Run the workflow.
`), 0o644))

	err := NewCompiler(WithVersion("dev")).CompileWorkflow(workflowPath)
	require.ErrorContains(t, err, `plugins are not supported by engine "gemini"`)
}

func TestCompileWorkflowInstallsImportedPlugins(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imported-plugin")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
plugins:
  - octo-org/agent-plugin@`+testPluginSHA+`
---

Shared plugin configuration.
`), 0o644))

	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
imports:
  - shared.md
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	assert.Contains(t, string(lockContent), "name: Install agent plugin octo-org/agent-plugin")
	assert.Contains(t, string(lockContent), "ref: "+testPluginSHA)
}

func TestCompileWorkflowInstallsAuthenticatedImportedPlugin(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imported-private-plugin")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
plugins:
  - plugin: octo-org/private-plugin@`+testPluginSHA+`
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}
---

Shared plugin configuration.
`), 0o644))

	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
imports:
  - shared.md
---

Run the workflow.
`), 0o644))

	require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(workflowPath))
	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	assert.Regexp(t, `(?s)name: Checkout agent plugin octo-org/private-plugin.*?token: \$\{\{ secrets\.PRIVATE_PLUGIN_TOKEN \}\}`, string(lockContent))
}

func TestCompileWorkflowMergesImportedPluginVersionsToHighestCompatibleVersion(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imported-plugin-merge")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
plugins:
  - org/plugin@v1.2.0
---

Shared plugin configuration.
`), 0o644))

	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
plugins:
  - org/plugin@v1.3.0
imports:
  - shared.md
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	cache := compiler.GetSharedActionCache()
	cache.Set("org/plugin", "v1.2.0", "1111111111111111111111111111111111111111")
	cache.Set("org/plugin", "v1.3.0", "2222222222222222222222222222222222222222")

	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockText := string(lockContent)
	assert.Contains(t, lockText, "name: Checkout agent plugin org/plugin")
	assert.Contains(t, lockText, "ref: 2222222222222222222222222222222222222222")
	assert.NotContains(t, lockText, "ref: 1111111111111111111111111111111111111111")
	assert.Equal(t, 1, strings.Count(lockText, "name: Checkout agent plugin org/plugin"))
	assert.Equal(t, 1, strings.Count(lockText, "name: Install agent plugin org/plugin"))
}

func TestResolveFrontmatterPluginRefs(t *testing.T) {
	t.Run("pins a ref from the action cache", func(t *testing.T) {
		cache := NewActionCache(testutil.TempDir(t, "plugin-ref-cache"))
		cache.Set("octo-org/agent-plugins/plugins/example", "main", testPluginSHA)

		data := &WorkflowData{
			Plugins:        []string{"octo-org/agent-plugins/plugins/example@main"},
			Ctx:            context.Background(),
			ActionResolver: NewActionResolver(cache),
		}
		err := NewCompiler(WithVersion("dev")).resolveFrontmatterPluginRefs(data)

		require.NoError(t, err)
		assert.Equal(t, []string{"octo-org/agent-plugins/plugins/example@" + testPluginSHA}, data.Plugins)
	})

	t.Run("keeps an exact SHA without a resolver", func(t *testing.T) {
		data := &WorkflowData{Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA}}
		err := NewCompiler(WithVersion("dev")).resolveFrontmatterPluginRefs(data)

		require.NoError(t, err)
		assert.Equal(t, "octo-org/agent-plugin@"+testPluginSHA, data.Plugins[0])
	})

	t.Run("fails when a ref cannot be pinned", func(t *testing.T) {
		data := &WorkflowData{Plugins: []string{"octo-org/agent-plugin@main"}}
		err := NewCompiler(WithVersion("dev")).resolveFrontmatterPluginRefs(data)

		require.ErrorContains(t, err, "no GitHub reference resolver is available")
		assert.Equal(t, "octo-org/agent-plugin@main", data.Plugins[0])
	})

	t.Run("fails when resolution fails", func(t *testing.T) {
		cache := NewActionCache(testutil.TempDir(t, "plugin-ref-failure"))
		resolver := NewActionResolver(cache)
		resolver.failedResolutions[formatActionCacheKey("octo-org/agent-plugin", "missing")] = struct{}{}
		data := &WorkflowData{
			Plugins:        []string{"octo-org/agent-plugin@missing"},
			ActionResolver: resolver,
		}

		err := NewCompiler(WithVersion("dev")).resolveFrontmatterPluginRefs(data)
		require.ErrorContains(t, err, "failed to resolve")
	})
}

func TestCopilotPluginInstallationSteps(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("installs a pinned root plugin", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		checkout := strings.Join(steps[0], "\n")
		assert.Contains(t, checkout, "name: Checkout agent plugin octo-org/agent-plugin")
		assert.Contains(t, checkout, "uses: actions/checkout@")
		assert.NotContains(t, checkout, "uses: actions/checkout@v")
		assert.Contains(t, checkout, "repository: octo-org/agent-plugin")
		assert.Contains(t, checkout, "ref: "+testPluginSHA)
		assert.Contains(t, checkout, "path: .gh-aw-plugins/plugin-0")
		assert.Contains(t, checkout, "persist-credentials: false")

		install := strings.Join(steps[1], "\n")
		assert.Contains(t, install, "name: Install agent plugin octo-org/agent-plugin")
		assert.Contains(t, install, "copilot plugin install ./.gh-aw-plugins/plugin-0")
	})

	t.Run("installs a plugin from a repository subpath", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugins/plugins/example@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		assert.Contains(t, strings.Join(steps[0], "\n"), "repository: octo-org/agent-plugins")
		assert.Contains(t, strings.Join(steps[1], "\n"), "copilot plugin install ./.gh-aw-plugins/plugin-0/plugins/example")
	})

	t.Run("uses a custom engine command", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			EngineConfig: &EngineConfig{Command: "/opt/copilot"},
			Plugins:      []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		assert.Contains(t, strings.Join(steps[1], "\n"), "/opt/copilot plugin install")
	})

	t.Run("returns no steps without plugins", func(t *testing.T) {
		assert.Empty(t, engine.GetPluginInstallationSteps(&WorkflowData{}))
	})
}

func TestPluginInstallationFollowsEngineInstallation(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{
		Name:         "plugins-order",
		AI:           "copilot",
		EngineConfig: &EngineConfig{ID: "copilot"},
		Plugins:      []string{"octo-org/agent-plugin@" + testPluginSHA},
	}
	var generated strings.Builder

	_, err := compiler.generateEngineInstallAndPreAgentSteps(&generated, data, false)
	require.NoError(t, err)

	output := generated.String()
	engineInstallIndex := strings.Index(output, "name: Install GitHub Copilot CLI")
	pluginCheckoutIndex := strings.Index(output, "name: Checkout agent plugin")
	pluginInstallIndex := strings.Index(output, "name: Install agent plugin")
	require.NotEqual(t, -1, engineInstallIndex)
	require.NotEqual(t, -1, pluginCheckoutIndex)
	require.NotEqual(t, -1, pluginInstallIndex)
	assert.Less(t, engineInstallIndex, pluginCheckoutIndex)
	assert.Less(t, pluginCheckoutIndex, pluginInstallIndex)
}

func TestClaudePluginInstallation(t *testing.T) {
	engine := NewClaudeEngine()

	t.Run("checks plugins out without a CLI install command", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugins/plugins/example@" + testPluginSHA},
		})

		require.Len(t, steps, 1)
		checkout := strings.Join(steps[0], "\n")
		assert.Contains(t, checkout, "name: Checkout agent plugin octo-org/agent-plugins/plugins/example")
		assert.Contains(t, checkout, "repository: octo-org/agent-plugins")
		assert.Contains(t, checkout, "path: .gh-aw-plugins/plugin-0")
	})

	t.Run("loads plugin directories through --plugin-dir", func(t *testing.T) {
		args, _, _ := engine.buildClaudeCliArgs(&WorkflowData{
			Plugins: []string{
				"octo-org/agent-plugin@" + testPluginSHA,
				"octo-org/agent-plugins/plugins/example@" + testPluginSHA,
			},
		}, nil, "log.txt")

		joined := strings.Join(args, " ")
		assert.Contains(t, joined, "--plugin-dir ./.gh-aw-plugins/plugin-0")
		assert.Contains(t, joined, "--plugin-dir ./.gh-aw-plugins/plugin-1/plugins/example")
	})

	t.Run("adds no plugin flags without plugins", func(t *testing.T) {
		args, _, _ := engine.buildClaudeCliArgs(&WorkflowData{}, nil, "log.txt")
		assert.NotContains(t, strings.Join(args, " "), "--plugin-dir")
	})
}

func TestCodexPluginInstallation(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("registers a pinned root plugin as a local marketplace", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		checkout := strings.Join(steps[0], "\n")
		assert.Contains(t, checkout, "name: Checkout agent plugin octo-org/agent-plugin")
		assert.Contains(t, checkout, "repository: octo-org/agent-plugin")
		assert.Contains(t, checkout, "path: .gh-aw-plugins/plugin-0")

		install := strings.Join(steps[1], "\n")
		assert.Contains(t, install, "name: Install agent plugin octo-org/agent-plugin")
		assert.Contains(t, install, "jq -r '.name // empty' \".gh-aw-plugins/plugin-0/plugin.json\"")
		assert.Contains(t, install, `--arg path "."`)
		assert.Contains(t, install, "codex plugin marketplace add \"./.gh-aw-plugins/plugin-0\"")
		assert.Contains(t, install, `codex plugin add "$PLUGIN_NAME@gh-aw-plugin-0"`)
	})

	t.Run("registers a plugin from a repository subpath", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugins/plugins/example@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		install := strings.Join(steps[1], "\n")
		assert.Contains(t, install, "jq -r '.name // empty' \".gh-aw-plugins/plugin-0/plugins/example/plugin.json\"")
		assert.Contains(t, install, `--arg path "./plugins/example"`)
	})

	t.Run("uses a custom engine command", func(t *testing.T) {
		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			EngineConfig: &EngineConfig{Command: "/opt/codex"},
			Plugins:      []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		install := strings.Join(steps[1], "\n")
		assert.Contains(t, install, "/opt/codex plugin marketplace add")
		assert.Contains(t, install, "/opt/codex plugin add")
	})

	t.Run("returns no steps without plugins", func(t *testing.T) {
		assert.Empty(t, engine.GetPluginInstallationSteps(&WorkflowData{}))
	})
}

func TestBehaviorDefinedEnginePluginInstallation(t *testing.T) {
	newEngine := func(t *testing.T, plugins *EnginePluginsDefinition) *BehaviorDefinedEngine {
		t.Helper()
		engine, err := NewBehaviorDefinedEngine(&EngineDefinition{
			ID:          "custom",
			DisplayName: "Custom",
			Behaviors: &EngineBehaviorDefinition{
				Plugins:   plugins,
				Execution: &EngineExecutionDefinition{CommandName: "custom-cli"},
			},
		})
		require.NoError(t, err)
		return engine
	}

	t.Run("stages plugins in the engine plugin directory", func(t *testing.T) {
		engine := newEngine(t, &EnginePluginsDefinition{Directory: ".kiro/powers"})
		assert.True(t, engine.GetCapabilities().Plugins)

		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugins/plugins/example@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		stage := strings.Join(steps[1], "\n")
		assert.Contains(t, stage, "name: Stage agent plugin octo-org/agent-plugins/plugins/example")
		assert.Contains(t, stage, `mkdir -p ".kiro/powers"`)
		assert.Contains(t, stage, `rm -rf ".kiro/powers/plugin-0-plugins__example"`)
		assert.Contains(t, stage, `cp -R "./.gh-aw-plugins/plugin-0/plugins/example" ".kiro/powers/plugin-0-plugins__example"`)
	})

	t.Run("stages plugins with the same basename to distinct destinations", func(t *testing.T) {
		engine := newEngine(t, &EnginePluginsDefinition{Directory: ".kiro/powers"})

		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{
				"octo-org/a/plugins/example@" + testPluginSHA,
				"octo-org/b/plugins/example@" + testPluginSHA,
			},
		})

		require.Len(t, steps, 4)
		firstStage := strings.Join(steps[1], "\n")
		secondStage := strings.Join(steps[3], "\n")
		assert.Contains(t, firstStage, `".kiro/powers/plugin-0-plugins__example"`)
		assert.Contains(t, secondStage, `".kiro/powers/plugin-1-plugins__example"`)
		assert.NotEqual(t, firstStage, secondStage, "plugins with the same basename must stage to distinct destinations")
	})

	t.Run("expands home-relative plugin directories", func(t *testing.T) {
		engine := newEngine(t, &EnginePluginsDefinition{Directory: "~/.cursor/plugins/local"})

		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		assert.Contains(t, strings.Join(steps[1], "\n"), `cp -R "./.gh-aw-plugins/plugin-0" "$HOME/.cursor/plugins/local/plugin-0-octo-org__agent-plugin"`)
	})

	t.Run("installs plugins through the engine CLI", func(t *testing.T) {
		engine := newEngine(t, &EnginePluginsDefinition{InstallArgs: []string{"plugin", "install"}})

		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 2)
		assert.Contains(t, strings.Join(steps[1], "\n"), "custom-cli plugin install ./.gh-aw-plugins/plugin-0")
	})

	t.Run("stages plugins and installs them through the engine CLI when both are configured", func(t *testing.T) {
		engine := newEngine(t, &EnginePluginsDefinition{
			Directory:   ".kiro/powers",
			InstallArgs: []string{"plugin", "install"},
		})

		steps := engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		})

		require.Len(t, steps, 3)
		assert.Contains(t, strings.Join(steps[0], "\n"), "name: Checkout agent plugin octo-org/agent-plugin")
		assert.Contains(t, strings.Join(steps[1], "\n"), "name: Stage agent plugin octo-org/agent-plugin")
		assert.Contains(t, strings.Join(steps[2], "\n"), "name: Install agent plugin octo-org/agent-plugin")
	})

	t.Run("disables plugins without a plugins behavior", func(t *testing.T) {
		engine := newEngine(t, nil)
		assert.False(t, engine.GetCapabilities().Plugins)
		assert.Empty(t, engine.GetPluginInstallationSteps(&WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@" + testPluginSHA},
		}))
	})

	t.Run("rejects incomplete or unsafe plugin behaviors", func(t *testing.T) {
		_, err := NewBehaviorDefinedEngine(&EngineDefinition{
			ID:        "custom",
			Behaviors: &EngineBehaviorDefinition{Plugins: &EnginePluginsDefinition{}},
		})
		require.ErrorContains(t, err, "without 'directory' or 'install-args'")

		_, err = NewBehaviorDefinedEngine(&EngineDefinition{
			ID:        "custom",
			Behaviors: &EngineBehaviorDefinition{Plugins: &EnginePluginsDefinition{Directory: "../escape"}},
		})
		require.ErrorContains(t, err, "unsupported behaviors.plugins.directory")
	})
}

func TestValidatePluginsAcceptsObjectFormWithGitHubToken(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{
		Plugins: []string{"octo-org/private-plugin@main"},
		PluginReferences: []PluginReference{
			{Plugin: "octo-org/private-plugin@main", GitHubToken: "${{ secrets.PRIVATE_PLUGIN_TOKEN }}"},
		},
	}

	require.NoError(t, compiler.validatePlugins(data))
	require.Len(t, data.PluginReferences, 1)
	assert.Equal(t, "${{ secrets.PRIVATE_PLUGIN_TOKEN }}", data.PluginReferences[0].GitHubToken)
	assert.Equal(t, []string{"octo-org/private-plugin@main"}, data.Plugins)
}

func TestPluginTokenExpression(t *testing.T) {
	t.Run("returns empty string without a token or app", func(t *testing.T) {
		data := &WorkflowData{
			Plugins:          []string{"octo-org/agent-plugin@main"},
			PluginReferences: []PluginReference{{Plugin: "octo-org/agent-plugin@main"}},
		}
		assert.Empty(t, pluginTokenExpression(data, 0))
	})

	t.Run("returns the configured github-token expression", func(t *testing.T) {
		data := &WorkflowData{
			Plugins: []string{"octo-org/private-plugin@main"},
			PluginReferences: []PluginReference{
				{Plugin: "octo-org/private-plugin@main", GitHubToken: "${{ secrets.PRIVATE_PLUGIN_TOKEN }}"},
			},
		}
		assert.Equal(t, "${{ secrets.PRIVATE_PLUGIN_TOKEN }}", pluginTokenExpression(data, 0))
	})

	t.Run("returns a minted app token expression", func(t *testing.T) {
		data := &WorkflowData{
			Plugins: []string{"octo-org/private-plugin@main"},
			PluginReferences: []PluginReference{
				{Plugin: "octo-org/private-plugin@main", GitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.PLUGIN_APP_CLIENT_ID }}",
					PrivateKey: "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}",
				}},
			},
		}
		assert.Equal(t, "${{ steps.plugin-app-token-0.outputs.token }}", pluginTokenExpression(data, 0))
	})

	t.Run("returns empty string for an out-of-range index", func(t *testing.T) {
		data := &WorkflowData{Plugins: []string{"octo-org/agent-plugin@main"}}
		assert.Empty(t, pluginTokenExpression(data, 5))
	})
}

func TestGeneratePluginAuthTokenSteps(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))

	t.Run("no steps without any github-app plugin", func(t *testing.T) {
		data := &WorkflowData{
			Plugins: []string{"octo-org/agent-plugin@main"},
			PluginReferences: []PluginReference{
				{Plugin: "octo-org/agent-plugin@main", GitHubToken: "${{ secrets.MY_TOKEN }}"},
			},
		}
		assert.Empty(t, compiler.generatePluginAuthTokenSteps(data))
	})

	t.Run("mints a token for each github-app plugin", func(t *testing.T) {
		data := &WorkflowData{
			Plugins: []string{"octo-org/private-plugin@main"},
			PluginReferences: []PluginReference{
				{Plugin: "octo-org/private-plugin@main", GitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.PLUGIN_APP_CLIENT_ID }}",
					PrivateKey: "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}",
				}},
			},
		}
		steps := compiler.generatePluginAuthTokenSteps(data)
		require.NotEmpty(t, steps)
		joined := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, joined, "id: plugin-app-token-0")
		assert.Contains(t, joined, "actions/create-github-app-token")
		assert.Contains(t, joined, "owner: octo-org")
		assert.Contains(t, joined, "repositories: private-plugin")
	})
}

func TestCompileWorkflowInstallsPrivatePluginWithGitHubToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "private-plugin-token")
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
plugins:
  - plugin: octo-org/private-plugin@`+testPluginSHA+`
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockText := string(lockContent)
	assert.Contains(t, lockText, "name: Checkout agent plugin octo-org/private-plugin")
	assert.Contains(t, lockText, "token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}")
}

func TestCompileWorkflowInstallsPrivatePluginWithGitHubApp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "private-plugin-app")
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
plugins:
  - plugin: octo-org/private-plugin@`+testPluginSHA+`
    github-app:
      client-id: ${{ vars.PLUGIN_APP_CLIENT_ID }}
      private-key: ${{ secrets.PLUGIN_APP_PRIVATE_KEY }}
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockText := string(lockContent)
	assert.Contains(t, lockText, "id: plugin-app-token-0")
	assert.Contains(t, lockText, "name: Checkout agent plugin octo-org/private-plugin")
	assert.Contains(t, lockText, "token: ${{ steps.plugin-app-token-0.outputs.token }}")
}

// TestCompileWorkflowIssuesDistinctTokensPerPlugin is a regression test ensuring that
// when several plugins are declared together, each plugin's checkout step receives its
// own credential (or none) rather than a token belonging to a different plugin.
func TestCompileWorkflowIssuesDistinctTokensPerPlugin(t *testing.T) {
	tmpDir := testutil.TempDir(t, "multi-plugin-tokens")
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
plugins:
  - octo-org/public-plugin@`+testPluginSHA+`
  - plugin: octo-org/private-plugin-token@`+testPluginSHA+`
    github-token: ${{ secrets.TOKEN_FOR_PLUGIN_1 }}
  - plugin: octo-org/private-plugin-app@`+testPluginSHA+`
    github-app:
      client-id: ${{ vars.APP_CLIENT_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockText := string(lockContent)

	// The public plugin's checkout step must not receive any token override.
	publicIdx := strings.Index(lockText, "name: Checkout agent plugin octo-org/public-plugin")
	require.GreaterOrEqual(t, publicIdx, 0)
	tokenIdx := strings.Index(lockText, "name: Checkout agent plugin octo-org/private-plugin-token")
	require.Greater(t, tokenIdx, publicIdx)
	publicBlock := lockText[publicIdx:tokenIdx]
	assert.NotContains(t, publicBlock, "token:")

	// The github-token plugin's checkout step must carry exactly its own token.
	appIdx := strings.Index(lockText, "name: Checkout agent plugin octo-org/private-plugin-app")
	require.Greater(t, appIdx, tokenIdx)
	tokenBlock := lockText[tokenIdx:appIdx]
	assert.Contains(t, tokenBlock, "token: ${{ secrets.TOKEN_FOR_PLUGIN_1 }}")

	// The github-app plugin's checkout step must reference its own minted token step.
	appBlock := lockText[appIdx:]
	assert.Contains(t, lockText, "id: plugin-app-token-2")
	assert.Contains(t, appBlock, "token: ${{ steps.plugin-app-token-2.outputs.token }}")
}

// TestCompileWorkflowIssuesDistinctAppTokensForMultipleGitHubAppPlugins is a regression
// test ensuring that when several plugins each declare their own github-app credential,
// the compiler mints a separate token step per plugin and wires each checkout step to
// its own minted step rather than reusing another plugin's app credentials or token.
func TestCompileWorkflowIssuesDistinctAppTokensForMultipleGitHubAppPlugins(t *testing.T) {
	tmpDir := testutil.TempDir(t, "multi-plugin-app-tokens")
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
plugins:
  - plugin: octo-org/private-plugin-app-a@`+testPluginSHA+`
    github-app:
      client-id: ${{ vars.APP_A_CLIENT_ID }}
      private-key: ${{ secrets.APP_A_PRIVATE_KEY }}
  - plugin: octo-org/private-plugin-app-b@`+testPluginSHA+`
    github-app:
      client-id: ${{ vars.APP_B_CLIENT_ID }}
      private-key: ${{ secrets.APP_B_PRIVATE_KEY }}
---

Run the workflow.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockText := string(lockContent)

	mintAIdx := strings.Index(lockText, "id: plugin-app-token-0")
	mintBIdx := strings.Index(lockText, "id: plugin-app-token-1")
	require.GreaterOrEqual(t, mintAIdx, 0)
	require.Greater(t, mintBIdx, mintAIdx)

	mintABlock := lockText[mintAIdx:mintBIdx]
	assert.Contains(t, mintABlock, "client-id: ${{ vars.APP_A_CLIENT_ID }}")
	assert.Contains(t, mintABlock, "private-key: ${{ secrets.APP_A_PRIVATE_KEY }}")
	assert.NotContains(t, mintABlock, "APP_B")

	checkoutAIdx := strings.Index(lockText, "name: Checkout agent plugin octo-org/private-plugin-app-a")
	checkoutBIdx := strings.Index(lockText, "name: Checkout agent plugin octo-org/private-plugin-app-b")
	require.Greater(t, checkoutAIdx, mintBIdx)
	require.Greater(t, checkoutBIdx, checkoutAIdx)

	checkoutABlock := lockText[checkoutAIdx:checkoutBIdx]
	assert.Contains(t, checkoutABlock, "token: ${{ steps.plugin-app-token-0.outputs.token }}")
	assert.NotContains(t, checkoutABlock, "plugin-app-token-1")

	checkoutBBlock := lockText[checkoutBIdx:]
	assert.Contains(t, checkoutBBlock, "token: ${{ steps.plugin-app-token-1.outputs.token }}")
	assert.NotContains(t, checkoutBBlock, "name: Checkout agent plugin octo-org/private-plugin-app-a")
}

// TestValidatePluginsMergePreservesAuthWhenHigherVersionHasNone is a regression test for a
// bug where merging the same plugin path declared twice (for example once in the main
// workflow with a per-plugin credential, and once via an import at a higher compatible
// version without one) silently dropped the credential once the higher version won,
// causing the generated checkout step to fall back to the workflow's default token
// instead of the explicitly configured one.
func TestValidatePluginsMergePreservesAuthWhenHigherVersionHasNone(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{
		Plugins: []string{
			"org/plugin@v1.2.0",
			"org/plugin@v1.3.0",
		},
		PluginReferences: []PluginReference{
			{Plugin: "org/plugin@v1.2.0", GitHubToken: "${{ secrets.ORG_PLUGIN_TOKEN }}"},
			{Plugin: "org/plugin@v1.3.0"},
		},
	}

	require.NoError(t, compiler.validatePlugins(data))
	require.Len(t, data.PluginReferences, 1)
	assert.Equal(t, "org/plugin@v1.3.0", data.PluginReferences[0].Plugin)
	assert.Equal(t, "${{ secrets.ORG_PLUGIN_TOKEN }}", data.PluginReferences[0].GitHubToken,
		"the credential from the lower-version declaration must survive the version merge")
}

// TestValidatePluginsMergeRejectsConflictingAuth is a regression test ensuring that when
// the same plugin path is declared twice with two different credentials, validation fails
// loudly instead of silently picking one of them (which could install with the wrong
// identity or leak the discarded credential's intended scope).
func TestValidatePluginsMergeRejectsConflictingAuth(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{
		Plugins: []string{
			"org/plugin@v1.2.0",
			"org/plugin@v1.3.0",
		},
		PluginReferences: []PluginReference{
			{Plugin: "org/plugin@v1.2.0", GitHubToken: "${{ secrets.TOKEN_A }}"},
			{Plugin: "org/plugin@v1.3.0", GitHubToken: "${{ secrets.TOKEN_B }}"},
		},
	}

	err := compiler.validatePlugins(data)
	require.ErrorContains(t, err, `plugin "org/plugin" is declared with conflicting github-token/github-app credentials`)
}
