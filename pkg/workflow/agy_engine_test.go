//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agyDefinition(t *testing.T) *EngineDefinition {
	t.Helper()
	for _, def := range loadBuiltinEngineDefinitions() {
		if def.ID == string(constants.AgyEngine) {
			return def
		}
	}
	t.Fatal("missing embedded Agy definition")
	return nil
}

func TestAgyBuiltInRegistration(t *testing.T) {
	def := agyDefinition(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, engine.IsExperimental())
	assert.Equal(t, "1.3.1", def.Version)
	assert.False(t, engine.GetCapabilities().BashCommandAllowlist)
	assert.False(t, engine.GetCapabilities().BashDisable)
	assert.Contains(t, NewEngineCatalog(NewEngineRegistry()).IDs(), "agy")
	assert.Equal(t, "copilot", def.DetectionEngine)
	assert.NotNil(t, constants.GetEngineOption("agy"))
	assert.Contains(t, constants.GetEngineOption("agy").Label, "Experimental")
	assert.Contains(t, engine.GetAgentManifestFiles(), "AGENTS.md")
	assert.Contains(t, engine.GetAgentManifestFiles(), "GEMINI.md")
	assert.Contains(t, engine.GetAgentManifestPathPrefixes(), ".agents/")
	assert.Contains(t, engine.GetAgentManifestPathPrefixes(), ".gemini/")
	assert.Contains(t, NewEngineRegistry().GetAllAgentManifestFolders(), ".agents")
	assert.Equal(t, "1.3.1", getVersionForSetup(&WorkflowData{AI: "agy"}, NewEngineRegistry()))
	assert.Equal(t, string(constants.CopilotEngine), constants.EngineOptions[0].Value)
}

func TestAgyCompilerSelectionAndRestrictions(t *testing.T) {
	for _, tt := range []struct {
		name, selection, tools, failure string
	}{
		{"short form", "agy", "bash: [\"*\"]", ""},
		{"object form", "\n  id: agy\n  model: gemini-3.8-flash-medium", "bash: [\"*\"]", ""},
		{"WIF", "\n  id: agy\n  auth:\n    type: github-oidc\n    provider: gcp\n    workload-identity-provider: projects/1/locations/global/workloadIdentityPools/test/providers/test\n    service-account: test@example.iam.gserviceaccount.com", "bash: [\"*\"]", "Retain engine: gemini"},
		{"extra CLI arguments", "\n  id: agy\n  args: [\"--prompt\", \"override\"]", "bash: [\"*\"]", "headless profile"},
		{"custom harness", "\n  id: agy\n  harness: custom.cjs", "bash: [\"*\"]", "engine.harness"},
		{"harness watchdog", "\n  id: agy\n  harness:\n    watchdog-timeout: 1", "bash: [\"*\"]", "engine.harness"},
		{"subdirectory", "\n  id: agy\n  cwd: packages/app", "bash: [\"*\"]", "engine.cwd"},
		{"native turn limit", "\n  id: agy\n  max-turns: 3", "bash: [\"*\"]", "max-turns"},
		{"unverified version", "\n  id: agy\n  version: \"9.9.9\"", "bash: [\"*\"]", "verified native archive"},
		{"shell allowlist", "agy", "bash: [\"echo\"]", "allow-list"},
		{"disabled shell", "agy", "bash: false", "bash"},
		{"disabled edit", "agy", "edit: false", "cannot enforce"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "agy.md")
			require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nconcurrency:\n  job-discriminator: ${{ github.run_id }}\npermissions:\n  contents: read\nengine: "+tt.selection+"\ntools:\n  github: false\n  "+tt.tools+"\n---\nSay hello.\n"), 0o600))
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(source)
			if tt.failure != "" {
				require.Error(t, err)
				assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.failure))
				return
			}
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(dir, "agy.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(lock), "agy_harness.cjs")
			assert.Contains(t, string(lock), "GH_AW_ENGINE_VERSION: 1.3.1")
			assert.Contains(t, string(lock), "GH_AW_AGY_MODEL: gemini-3.8-flash-medium")
			assert.Contains(t, string(lock), "--exclude-env GEMINI_API_KEY")
			assert.Contains(t, string(lock), `"gemini"`)
		})
	}
}

func TestAgyExperimentalDiagnosticIsInformational(t *testing.T) {
	compiler := NewCompiler()
	engine, _, err := compiler.resolveEngineRuntimeConfig("agy", &EngineConfig{ID: "agy"})
	require.NoError(t, err)
	assert.True(t, engine.IsExperimental())
	assert.Zero(t, compiler.GetWarningCount())
}

func TestAgyUsesExistingGeminiProviderTarget(t *testing.T) {
	data := &WorkflowData{AI: "agy", EngineConfig: &EngineConfig{
		Env: map[string]string{"GOOGLE_GEMINI_BASE_URL": "https://gemini-proxy.example/api"},
	}}
	engine, err := NewEngineRegistry().GetEngine("agy")
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-proxy.example"}, getEngineAPIHosts(data, engine))
	assert.Equal(t, DefaultGeminiAPITarget, GetGeminiAPITarget(data, "gemini"), "Agy endpoint configuration must not change Gemini behavior")
	assert.Equal(t, DefaultGeminiAPITarget, GetGeminiAPITarget(nil, "agy"))
}

func TestAgyCLIOverrideUsesBuiltInDefaults(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "override.md")
	require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nengine: gemini\ntools:\n  github: false\n  bash: [\"*\"]\n---\nSay hello.\n"), 0o600))
	require.NoError(t, NewCompiler(WithEngineOverride("agy")).CompileWorkflow(source))
	lock, err := os.ReadFile(filepath.Join(dir, "override.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "GH_AW_ENGINE_VERSION: 1.3.1")
	assert.Contains(t, string(lock), "GH_AW_AGY_MODEL: gemini-3.8-flash-medium")
	assert.Contains(t, string(lock), "agy_harness.cjs")
	assert.NotContains(t, string(lock), "@google/gemini-cli")
}

func TestAgyProductionConformanceIsBoundedAndReadOnly(t *testing.T) {
	type job struct {
		Permissions    map[string]string `yaml:"permissions"`
		TimeoutMinutes int               `yaml:"timeout-minutes"`
		Secrets        map[string]string `yaml:"secrets"`
	}
	var callee, caller struct {
		Jobs map[string]job `yaml:"jobs"`
	}
	lock, err := os.ReadFile("../../.github/workflows/engine-conformance-agy.lock.yml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(lock, &callee))
	for name, config := range callee.Jobs {
		for permission, level := range config.Permissions {
			assert.NotEqual(t, "write", level, "%s must not grant %s write permission", name, permission)
		}
	}
	assert.Equal(t, 10, callee.Jobs["agent"].TimeoutMinutes)
	assert.Equal(t, 2, callee.Jobs["safe_outputs"].TimeoutMinutes)
	assert.Contains(t, string(lock), `"maxAiCredits":5`)
	assert.Contains(t, string(lock), `"maxCacheMisses":12`)
	parent, err := os.ReadFile("../../.github/workflows/credentials-check.yml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(parent, &caller))
	binding := caller.Jobs["agy-conformance"]
	assert.Equal(t, map[string]string{"GEMINI_API_KEY": "${{ secrets.GEMINI_API_KEY }}"}, binding.Secrets)
	for name, config := range callee.Jobs {
		for permission, level := range config.Permissions {
			assert.Equal(t, level, binding.Permissions[permission], "caller must allow %s required by %s", permission, name)
		}
	}
}

func TestAgyRejectsUnverifiedEngineProfiles(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config EngineConfig
		field  string
	}{
		{"provider", EngineConfig{LLMProvider: "openai"}, "engine.provider"},
		{"permission mode", EngineConfig{PermissionMode: "plan"}, "engine.permission-mode"},
		{"configuration", EngineConfig{Config: `{"modelProvider":"vertex"}`}, "engine.config"},
		{"driver", EngineConfig{Driver: "custom.cjs"}, "engine.driver"},
		{"retry policy", EngineConfig{HarnessMaxRetries: "3"}, "engine.harness"},
		{"continuations", EngineConfig{MaxContinuations: 3}, "max-continuations"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgyEngineConfig(&tt.config)
			require.ErrorContains(t, err, tt.field)
		})
	}
}
