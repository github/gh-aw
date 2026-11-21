//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependabotCompiledEngineDependencies(t *testing.T) {
	engines := []struct {
		id, pkg string
		version constants.Version
	}{
		{"copilot", "@github/copilot", constants.DefaultCopilotVersion},
		{"claude", "@anthropic-ai/claude-code", constants.DefaultClaudeCodeVersion},
		{"codex", "@openai/codex", constants.DefaultCodexVersion},
		{"gemini", "@google/gemini-cli", constants.DefaultGeminiVersion},
		{"pi", "@earendil-works/pi-coding-agent", constants.DefaultPiVersion},
	}
	for _, engine := range engines {
		for _, override := range []string{"", "1.2.3"} {
			t.Run(engine.id+"/"+override, func(t *testing.T) {
				c := NewCompiler()
				source := "engine: " + engine.id + "\n"
				expected := string(engine.version)
				if override != "" {
					source = fmt.Sprintf("engine:\n  id: %s\n  version: %q\n", engine.id, override)
					expected = override
				}
				data, err := c.ParseWorkflowString("---\non: push\n"+source+"---\n# Test\n", "dependabot-engine.md")
				require.NoError(t, err)
				assert.Equal(t, []NpmDependency{{Name: engine.pkg, Version: expected}}, c.collectNpmDependencies([]*WorkflowData{data}))
				assert.Empty(t, extractNpxPackages(data), "engine dependencies must not enter general npm validation")
			})
		}
	}
	t.Run("omitted engine and compiler override", func(t *testing.T) {
		for _, override := range []string{"", "claude"} {
			c := NewCompiler(WithEngineOverride(override))
			data, err := c.ParseWorkflowString("---\non: push\n---\n# Test\n", "dependabot-default.md")
			require.NoError(t, err)
			expected := NpmDependency{Name: "@github/copilot", Version: string(constants.DefaultCopilotVersion)}
			if override == "claude" {
				expected = NpmDependency{Name: "@anthropic-ai/claude-code", Version: string(constants.DefaultClaudeCodeVersion)}
			}
			assert.Equal(t, []NpmDependency{expected}, c.collectNpmDependencies([]*WorkflowData{data}))
		}
	})
}

func TestDependabotEngineDependencyPrecedence(t *testing.T) {
	c := NewCompiler()
	data := []*WorkflowData{
		{AI: "claude", EngineConfig: &EngineConfig{ID: "codex", Version: "1.0.0"}, CustomSteps: "npx zod@3.0.0"},
		{AI: "claude", EngineConfig: &EngineConfig{Version: "2.0.0"}},
		{AI: "codex", EngineConfig: &EngineConfig{Version: "3.0.0"}, CustomSteps: "npx @openai/codex@4.0.0\nnpx zod@2.0.0"},
	}
	assert.Equal(t, []NpmDependency{
		{Name: "@anthropic-ai/claude-code", Version: "2.0.0"},
		{Name: "@openai/codex", Version: "4.0.0"},
		{Name: "zod", Version: "2.0.0"},
	}, c.collectNpmDependencies(data))
	for _, id := range []string{"copilot", "claude", "codex", "gemini", "pi", "unknown", "custom", "agy"} {
		assert.Empty(t, c.collectNpmDependencies([]*WorkflowData{{AI: id, EngineConfig: &EngineConfig{Version: "${{ inputs.version }}"}}}))
		assert.Empty(t, c.collectNpmDependencies([]*WorkflowData{{AI: id, EngineConfig: &EngineConfig{Command: "my-cli"}}}))
	}
	assert.Empty(t, c.collectNpmDependencies([]*WorkflowData{{AI: "unknown"}, {AI: "custom"}, {AI: "agy"}}))
	assert.Equal(t, []NpmDependency{{Name: "@openai/codex", Version: string(constants.DefaultCodexVersion)}},
		c.collectNpmDependencies([]*WorkflowData{{AI: "codex-experimental"}}))
}

func TestDependabotDetectionEngineDependencies(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		AI: "claude", EngineConfig: &EngineConfig{ID: "claude", Version: "2.0.0"},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
			EngineConfig: &EngineConfig{ID: "codex", Version: "3.0.0"},
		}},
	}
	assert.Equal(t, []NpmDependency{
		{Name: "@anthropic-ai/claude-code", Version: "2.0.0"},
		{Name: "@openai/codex", Version: "3.0.0"},
	}, c.collectNpmDependencies([]*WorkflowData{data}))
	data.SafeOutputs.ThreatDetection.EngineDisabled = true
	assert.Equal(t, []NpmDependency{{Name: "@anthropic-ai/claude-code", Version: "2.0.0"}},
		c.collectNpmDependencies([]*WorkflowData{data}))
	data.SafeOutputs.ThreatDetection = &ThreatDetectionConfig{}
	data.AI = "pi"
	data.EngineConfig = &EngineConfig{ID: "pi", Version: "4.0.0"}
	assert.Equal(t, []NpmDependency{
		{Name: "@earendil-works/pi-coding-agent", Version: "4.0.0"},
		{Name: "@github/copilot", Version: string(constants.DefaultCopilotVersion)},
	}, c.collectNpmDependencies([]*WorkflowData{data}))
}

func TestDependabotCompiledSDKDependencies(t *testing.T) {
	c := NewCompiler()
	data, err := c.ParseWorkflowString("---\non: push\nengine:\n  id: copilot\n  version: '1.2.3'\n  copilot-sdk: true\n---\n# Test\n", "dependabot-sdk.md")
	require.NoError(t, err)
	assert.Equal(t, []NpmDependency{
		{Name: "@github/copilot", Version: "1.2.3"},
	}, c.collectNpmDependencies([]*WorkflowData{data}))
}

func TestDependabotExternalDetectorInstallsCopilotDespiteCustomCommand(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		AI: "copilot", EngineConfig: &EngineConfig{ID: "copilot", Version: "1.2.3", Command: "custom-copilot"},
		Features:    map[string]any{string(constants.GHAWDetectionFeatureFlag): true},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}
	assert.Equal(t, []NpmDependency{{Name: "@github/copilot", Version: "1.2.3"}},
		c.collectNpmDependencies([]*WorkflowData{data}))
	data.Features[string(constants.GHAWDetectionFeatureFlag)] = false
	assert.Empty(t, c.collectNpmDependencies([]*WorkflowData{data}))
	data.Features[string(constants.GHAWDetectionFeatureFlag)] = true
	data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{ID: "copilot", Version: "2.3.4", CopilotSDK: true}
	assert.Equal(t, []NpmDependency{
		{Name: "@github/copilot", Version: "2.3.4"},
	}, c.collectNpmDependencies([]*WorkflowData{data}))
	assert.Equal(t, []string{"/.github/gh-aw-dependencies/copilot-sdk/node"},
		requireFrozenSDKInventories(t, c, []*WorkflowData{data}, testutil.TempDir(t, "detector-sdk-inventory")))
}

func TestDependabotCopilotSDKDependencies(t *testing.T) {
	for _, test := range []struct {
		name, command, driver, inline, ecosystem, pkg, version string
	}{
		{"node", "", "", "", "npm", "node", ""},
		{"typescript", "ts-node driver.ts", "", "", "npm", "typescript", ""},
		{"python driver", "", "driver.py", "", "pip", "github-copilot-sdk", "==" + string(constants.DefaultCopilotSDKVersion)},
		{"python command", "python3 driver.py", "", "", "pip", "github-copilot-sdk", "==" + string(constants.DefaultCopilotSDKVersion)},
		{"go command", "go run driver.go", "", "", "go", "github.com/github/copilot-sdk/go", "v" + string(constants.DefaultCopilotSDKVersion)},
		{"inline node", "", "", "javascript", "npm", "node", ""},
		{"inline python", "", "", "python", "pip", "github-copilot-sdk", "==" + string(constants.DefaultCopilotSDKVersion)},
		{"inline go", "", "", "go", "go", "github.com/github/copilot-sdk/go", "v" + string(constants.DefaultCopilotSDKVersion)},
		{"rust is not npm", "cargo run", "", "", "npm", "", ""},
		{"dotnet is not npm", "dotnet run", "", "", "npm", "", ""},
		{"java is not npm", "java driver.java", "", "", "npm", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewCompiler()
			config := &EngineConfig{ID: "copilot", Version: "1.2.3", CopilotSDK: true, Command: test.command, Driver: test.driver}
			if test.inline != "" {
				config.InlineDriver = &InlineEngineDriver{Runtime: test.inline, Source: "inline"}
			}
			data := []*WorkflowData{{AI: "copilot", EngineConfig: config}}
			switch test.ecosystem {
			case "npm":
				dirs := requireFrozenSDKInventories(t, c, data, testutil.TempDir(t, "sdk-inventory"))
				if test.pkg != "" {
					assert.Equal(t, []string{"/.github/gh-aw-dependencies/copilot-sdk/" + test.pkg}, dirs)
				} else {
					assert.Empty(t, dirs)
				}
				if test.command == "" {
					assert.Equal(t, []NpmDependency{{Name: "@github/copilot", Version: "1.2.3"}}, c.collectNpmDependencies(data))
				} else {
					assert.Empty(t, c.collectNpmDependencies(data))
				}
			case "pip":
				assert.Equal(t, []PipDependency{{Name: test.pkg, Version: test.version}}, c.collectPipDependencies(data))
				assert.NotContains(t, c.collectNpmDependencies(data), NpmDependency{Name: "@github/copilot-sdk", Version: string(constants.DefaultCopilotSDKVersion)})
			case "go":
				assert.Equal(t, []GoDependency{{Path: test.pkg, Version: test.version}}, c.collectGoDependencies(data))
			}
		})
	}
}

func requireFrozenSDKInventories(t *testing.T, c *Compiler, data []*WorkflowData, configDir string) []string {
	t.Helper()
	dirs, err := c.generateCopilotSDKNpmInventories(data, configDir)
	require.NoError(t, err)
	return dirs
}

func TestGenerateDependabotManifests_FrozenSDKGraphs(t *testing.T) {
	c := NewCompiler()
	c.SetStrictMode(true)
	configDir := filepath.Join(testutil.TempDir(t, "frozen-sdk"), ".github")
	workflowDir := filepath.Join(configDir, "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	setupFakeFailingNpm(t) // A frozen-only inventory does not invoke npm at all.
	configPath := filepath.Join(configDir, "dependabot.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`version: 2
registries:
  private-npm:
    type: npm-registry
    url: https://registry.example.com
updates:
  - package-ecosystem: npm
    directory: /.github/workflows
    registries: [private-npm]
    schedule:
      interval: weekly
      day: monday
`), 0o644))

	data := []*WorkflowData{
		{AI: "copilot", EngineConfig: &EngineConfig{CopilotSDK: true, Command: "node driver.js"}},
		{AI: "copilot", EngineConfig: &EngineConfig{CopilotSDK: true, Command: "ts-node driver.ts"}},
	}
	require.NoError(t, c.GenerateDependabotManifests(context.Background(), data, workflowDir, false))
	require.NoError(t, c.GenerateDependabotManifests(context.Background(), data, workflowDir, false)) // no duplicate entries
	assert.NoFileExists(t, filepath.Join(workflowDir, "package.json"))
	assert.NoFileExists(t, filepath.Join(workflowDir, "package-lock.json"))

	configYAML, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var config DependabotConfig
	require.NoError(t, yaml.Unmarshal(configYAML, &config))
	assert.Contains(t, config.Extra, "registries")
	require.Len(t, config.Updates, 3)
	assert.Equal(t, "/.github/workflows", config.Updates[0].Directory)
	assert.Contains(t, config.Updates[0].Extra, "registries")
	assert.Equal(t, "monday", config.Updates[0].Schedule.Extra["day"])
	for i, graph := range []struct {
		variant, manifest, lock string
		direct                  []string
	}{
		{"node", setup.CopilotSDKNodeManifest, setup.CopilotSDKNodeLock, []string{"@github/copilot-sdk", "undici"}},
		{"typescript", setup.CopilotSDKTypeScriptManifest, setup.CopilotSDKTypeScriptLock, []string{"@github/copilot-sdk", "undici", "ts-node", "typescript"}},
	} {
		update := config.Updates[i+1]
		assert.Equal(t, "npm", update.PackageEcosystem)
		assert.Equal(t, "/.github/gh-aw-dependencies/copilot-sdk/"+graph.variant, update.Directory)
		assert.NotContains(t, update.Extra, "registries")
		if assert.NotNil(t, update.PullRequestLimit) {
			assert.Zero(t, *update.PullRequestLimit)
		}

		dir := filepath.Join(configDir, "gh-aw-dependencies", "copilot-sdk", graph.variant)
		require.NoFileExists(t, filepath.Join(dir, ".npmrc"))
		manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
		require.NoError(t, err)
		lock, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
		require.NoError(t, err)
		assert.Equal(t, graph.manifest, string(manifest))
		assert.Equal(t, graph.lock, string(lock))
		var pkg PackageJSON
		require.NoError(t, json.Unmarshal(manifest, &pkg))
		for _, dep := range graph.direct {
			assert.Contains(t, pkg.Dependencies, dep)
		}
	}
}

func TestDependabotDeclarativeEngineDependencies(t *testing.T) {
	c := NewCompiler()
	dir := testutil.TempDir(t, "dependabot-engines")
	var workflows []*WorkflowData
	for i, manager := range []string{"npm", "pip", "go"} {
		pkg := []string{"@example/agent", "example-agent", "example.com/agent"}[i]
		version := []string{"1.2.3", "2.3.4", "v3.4.5"}[i]
		definition := fmt.Sprintf(`---
engine:
  id: example-agent
  behaviors:
    installation:
      package-manager: %s
      package-name: %q
      version: %q
    execution:
      command-name: example-agent
---
`, manager, pkg, version)
		shared := filepath.Join(dir, manager+".md")
		require.NoError(t, os.WriteFile(shared, []byte(definition), 0644))
		main := filepath.Join(dir, "workflow-"+manager+".md")
		require.NoError(t, os.WriteFile(main, []byte("---\non: push\nimports:\n  - "+manager+".md\n---\n# Test\n"), 0644))
		data, err := c.ParseWorkflowFile(main)
		require.NoError(t, err)
		workflows = append(workflows, data)
	}

	// All three metadata snapshots must survive registry resets and reused IDs.
	assert.Equal(t, []NpmDependency{{Name: "@example/agent", Version: "1.2.3"}}, c.collectNpmDependencies(workflows))
	assert.Equal(t, []PipDependency{{Name: "example-agent", Version: "==2.3.4"}}, c.collectPipDependencies(workflows))
	assert.Equal(t, []GoDependency{{Path: "example.com/agent", Version: "v3.4.5"}}, c.collectGoDependencies(workflows))
	workflows[0].EngineConfig.Version = "4.5.6"
	assert.Equal(t, []NpmDependency{{Name: "@example/agent", Version: "4.5.6"}}, c.collectNpmDependencies(workflows))
	workflows[0].EngineConfig.Version = "${{ inputs.version }}"
	assert.Empty(t, c.collectNpmDependencies(workflows))
}

func TestDependabotDeclarativeVersionAndDetectionMetadata(t *testing.T) {
	c := NewCompiler()
	dir := testutil.TempDir(t, "dependabot-definition")
	shared := filepath.Join(dir, "engine.md")
	require.NoError(t, os.WriteFile(shared, []byte(`---
engine:
  id: example-agent
  version: "1.9.0"
  detection-engine: codex
  behaviors:
    installation:
      package-manager: npm
      package-name: "@example/agent"
      version: "1.2.3"
    execution:
      command-name: example-agent
---
`), 0644))
	main := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(main, []byte("---\non: push\nimports:\n  - engine.md\n---\n# Test\n"), 0644))
	data, err := c.ParseWorkflowFile(main)
	require.NoError(t, err)
	assert.Equal(t, "1.9.0", data.EngineConfig.Version)
	assert.Equal(t, []NpmDependency{{Name: "@example/agent", Version: "1.9.0"}},
		c.collectNpmDependencies([]*WorkflowData{data}))
	// Simulate a later workflow replacing the custom catalog entry.
	c.engineCatalog.Register(&EngineDefinition{ID: "example-agent", DetectionEngine: "claude"})
	data.SafeOutputs = &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}}
	assert.Equal(t, []NpmDependency{
		{Name: "@example/agent", Version: "1.9.0"},
		{Name: "@openai/codex", Version: string(constants.DefaultCodexVersion)},
	}, c.collectNpmDependencies([]*WorkflowData{data}))
}
