//go:build !integration

package workflow

import (
	"fmt"
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
	engine := NewAgyEngine()
	assert.Nil(t, def.Behaviors)
	registered, err := NewEngineRegistry().GetEngine("agy")
	require.NoError(t, err)
	assert.IsType(t, engine, registered)
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
		{"custom trusted executable", "\n  id: agy\n  command: /opt/trusted/agy", "bash: [\"*\"]", ""},
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
			assert.Contains(t, string(lock), "convert_gateway_config_agy.cjs")
			assert.Contains(t, string(lock), "parse_agy_log.cjs")
			assert.NotContains(t, string(lock), "GHAW_HARNESS_SCRIPT")
			assert.NotContains(t, string(lock), "GHAW_MCP_CONFIG_ADAPTER_SCRIPT")
			assert.Contains(t, string(lock), "generativelanguage.googleapis.com")
			assert.Contains(t, string(lock), `GH_AW_INFO_FIREWALL_ENABLED: "true"`)
			if tt.name == "custom trusted executable" {
				assert.Contains(t, string(lock), "agy_harness.cjs /opt/trusted/agy")
				assert.NotContains(t, string(lock), "agy_harness.cjs agy'")
			}
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

func TestAgyInstallationDoesNotMutateOptionalConfiguration(t *testing.T) {
	engine := NewAgyEngine()
	for _, tt := range []struct {
		name string
		data *WorkflowData
	}{
		{"nil workflow", nil},
		{"nil engine config", &WorkflowData{AI: "agy"}},
		{"empty version", &WorkflowData{AI: "agy", EngineConfig: &EngineConfig{ID: "agy"}}},
		{"explicit version", &WorkflowData{AI: "agy", EngineConfig: &EngineConfig{ID: "agy", Version: "1.3.1"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var version string
			if tt.data != nil && tt.data.EngineConfig != nil {
				version = tt.data.EngineConfig.Version
			}
			steps := engine.GetInstallationSteps(tt.data)
			require.NotEmpty(t, steps)
			assert.Equal(t, GenerateNodeJsSetupStep(), steps[0])
			if tt.data != nil {
				assert.Equal(t, "1.3.1", getInstallationVersion(tt.data, engine, NewEngineRegistry()))
				if tt.data.EngineConfig != nil {
					assert.Equal(t, version, tt.data.EngineConfig.Version)
				}
			}
		})
	}
}

func TestAgyStepSummaryIsolation(t *testing.T) {
	for _, firewallEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("firewall=%t", firewallEnabled), func(t *testing.T) {
			data := &WorkflowData{
				AI:                 "agy",
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: firewallEnabled}},
			}
			steps := NewAgyEngine().GetExecutionSteps(data, "agent-stdio.log")
			require.Len(t, steps, 1)
			content := strings.Join(steps[0], "\n")
			assert.Contains(t, content, "GITHUB_STEP_SUMMARY: "+AgentStepSummaryPath)
			assert.Contains(t, content, "touch "+AgentStepSummaryPath)
			assert.Less(t, strings.Index(content, "touch "+AgentStepSummaryPath), strings.Index(content, "agy_harness.cjs"))
		})
	}
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

func TestBehaviorDefinedUnknownInferenceHostsPreservePriorBehavior(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(&EngineDefinition{
		ID: "unknown-hosts",
		Behaviors: &EngineBehaviorDefinition{
			SecretStrategy: behaviorSecretStrategyUniversalLLMConsumer,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, getEngineAPIHosts(nil, engine), "installation and infrastructure domains are not inference hosts")
	data := &WorkflowData{EngineConfig: &EngineConfig{APITarget: "explicit.example"}}
	assert.Equal(t, []string{"explicit.example"}, getEngineAPIHosts(data, engine))
	assert.IsType(t, &PiEngine{}, NewPiEngine(), "Pi retains its dedicated runtime")
	assert.Nil(t, getEngineAPIHosts(nil, NewPiEngine()), "Pi's unknown-host behavior is unchanged")
}

func TestAgyDryRunAllowsBuiltInNativePermissions(t *testing.T) {
	for _, selection := range []string{"agy", "\n  id: agy", "\n  id: agy\n  command: /opt/trusted/agy"} {
		t.Run(selection, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "agy.md")
			require.NoError(t, os.WriteFile(source, []byte("---\nstrict: false\non: workflow_dispatch\nengine: "+selection+"\n---\nSay hello.\n"), 0o600))
			compiler := NewCompiler()
			compiler.SetDryRun(true)
			compiler.SetStrictMode(true)
			require.NoError(t, compiler.CompileWorkflow(source))
			content, err := os.ReadFile(filepath.Join(dir, "agy.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(content), "agy_harness.cjs")
			assert.Contains(t, string(content), "awf --config")
		})
	}
}

func TestAgyNativeRuntimeDefaults(t *testing.T) {
	engine := NewAgyEngine()
	data := &WorkflowData{
		AI:                 "agy",
		EngineConfig:       &EngineConfig{ID: "agy"},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "agy_harness.cjs agy")
	assert.Contains(t, content, "awf --config")
	assert.Contains(t, content, "AWF_REFLECT_ENABLED: 1")
	assert.Contains(t, content, "GH_AW_ENGINE_VERSION: 1.3.1")
	assert.Contains(t, content, "GH_AW_AGY_MODEL: gemini-3.8-flash-medium")
	assert.Contains(t, content, "--exclude-env GEMINI_API_KEY")
	assert.Equal(t, "parse_agy_log", engine.GetLogParserScriptId())
	assert.Nil(t, engine.GetMCPConfigAdapterWriteStep())
	assert.Equal(t, []string{"GEMINI_API_KEY"}, engine.GetRequiredSecretNames(nil))
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

func TestAgyNativeRuntimeConfiguration(t *testing.T) {
	engine := NewAgyEngine()
	data := &WorkflowData{
		AI:    "agy",
		Model: "gemini-3.8-pro-high",
		EngineConfig: &EngineConfig{
			ID:      "agy",
			Command: "/opt/trusted/agy",
			Version: "1.3.1",
			Env:     map[string]string{"CONFORMANCE_MARKER": "agy"},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeCloudHypervisor},
		},
	}
	steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1)
	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "agy_harness.cjs /opt/trusted/agy")
	assert.Contains(t, content, "engine-cli/bin:$PATH")
	assert.Contains(t, content, "GH_AW_AGY_MODEL: gemini-3.8-pro-high")
	assert.Contains(t, content, "CONFORMANCE_MARKER: agy")

	data.NetworkPermissions.Firewall.Enabled = false
	data.SandboxConfig.Agent.Disabled = true
	steps = engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1)
	content = strings.Join(steps[0], "\n")
	assert.NotContains(t, content, "awf --config")
	assert.NotContains(t, content, "AWF_REFLECT_ENABLED")
	assert.Contains(t, content, "GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}")
	assert.Contains(t, content, "agy_harness.cjs /opt/trusted/agy")
	assert.Contains(t, content, "GH_AW_AGY_MODEL: gemini-3.8-pro-high")
}

type agyConformanceJob struct {
	Permissions    map[string]string `yaml:"permissions"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	Secrets        map[string]string `yaml:"secrets"`
	Uses           string            `yaml:"uses"`
	If             string            `yaml:"if"`
	Steps          []map[string]any  `yaml:"steps"`
}

type agyConformanceWorkflow struct {
	On          map[string]any               `yaml:"on"`
	Permissions map[string]string            `yaml:"permissions"`
	Concurrency map[string]string            `yaml:"concurrency"`
	Jobs        map[string]agyConformanceJob `yaml:"jobs"`
}

func TestAgyProductionConformancePermissionsAreBounded(t *testing.T) {
	var caller agyConformanceWorkflow
	parent, err := os.ReadFile("../../.github/workflows/credentials-check.yml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(parent, &caller))
	binding := caller.Jobs["agy-conformance"]
	assert.Equal(t, map[string]string{"GEMINI_API_KEY": "${{ secrets.GEMINI_API_KEY }}"}, binding.Secrets)
	assert.Equal(t, map[string]string{"actions": "read", "contents": "read"}, binding.Permissions)
	assert.Equal(t, "${{ inputs['agy-conformance'] }}", binding.If)
	assert.Equal(t, "./.github/workflows/agy-conformance-reusable.lock.yml", binding.Uses)
	for _, entry := range []struct {
		id, trigger string
		credits     int
	}{
		{"engine-conformance-agy", "workflow_dispatch", 5},
		{"agy-conformance-reusable", "workflow_call", 5},
		{"smoke-agy", "workflow_dispatch", 50},
	} {
		t.Run(entry.id, func(t *testing.T) {
			lock, err := os.ReadFile("../../.github/workflows/" + entry.id + ".lock.yml")
			require.NoError(t, err)
			var callee agyConformanceWorkflow
			require.NoError(t, yaml.Unmarshal(lock, &callee))
			require.Len(t, callee.On, 1)
			assert.Contains(t, callee.On, entry.trigger)
			assert.Empty(t, callee.Permissions)
			if entry.trigger == "workflow_call" {
				assert.Contains(t, callee.Concurrency["group"], "${{ github.run_id }}")
			}
			for name, config := range callee.Jobs {
				allowedPermissions := binding.Permissions
				if entry.id == "engine-conformance-agy" {
					switch name {
					case "activation":
						allowedPermissions = map[string]string{"actions": "read", "contents": "write"}
					case "safe_outputs":
						allowedPermissions = map[string]string{"actions": "write", "contents": "write"}
					}
				}
				if entry.id == "smoke-agy" && (name == "pre_activation" || name == "activation") {
					allowedPermissions = map[string]string{
						"actions": "read", "contents": "read", "issues": "read", "pull-requests": "read",
					}
				}
				for permission, level := range config.Permissions {
					expectedLevel, allowed := allowedPermissions[permission]
					assert.True(t, allowed, "%s must not grant unscoped %s permission", name, permission)
					assert.Equal(t, expectedLevel, level, "%s must use the scoped %s permission", name, permission)
				}
			}
			assert.Equal(t, 10, callee.Jobs["agent"].TimeoutMinutes)
			assert.Equal(t, 2, callee.Jobs["safe_outputs"].TimeoutMinutes)
			assert.Contains(t, string(lock), fmt.Sprintf(`"maxAiCredits":%d,`, entry.credits))
			assert.Contains(t, string(lock), `"maxCacheMisses":12`)
			assert.Contains(t, string(lock), `GH_AW_SAFE_OUTPUTS_STAGED: "true"`)
			assert.Contains(t, string(lock), `"threat_detection":{"mode":"disabled"}`)
			assert.NotContains(t, callee.Jobs, "detection")
			assertAgyConformanceProbes(t, callee, entry.id == "engine-conformance-agy")
		})
	}
}

func TestAgyConformanceEntryPointsShareConfiguration(t *testing.T) {
	var canonical map[string]any
	readConformanceFrontmatter(t, "../../.github/workflows/engine-conformance-agy.md", &canonical)
	assert.Equal(t, map[string]any{"workflow_dispatch": nil}, canonical["on"])
	assert.Equal(t, []any{"shared/agy-conformance.md", "shared/engine-conformance-worker.md"}, canonical["imports"])
	assert.EqualValues(t, 5, canonical["max-ai-credits"])
	delete(canonical, "name")
	delete(canonical, "description")
	delete(canonical, "on")
	delete(canonical, "max-ai-credits")
	delete(canonical, "imports")
	delete(canonical, "tools")
	for _, entry := range []struct {
		id, trigger string
		credits     int
	}{
		{"agy-conformance-reusable", "workflow_call", 5},
		{"smoke-agy", "workflow_dispatch", 50},
	} {
		t.Run(entry.id, func(t *testing.T) {
			var source map[string]any
			readConformanceFrontmatter(t, "../../.github/workflows/"+entry.id+".md", &source)
			expectedTriggers := map[string]any{entry.trigger: nil}
			if entry.id == "smoke-agy" {
				expectedTriggers["slash_command"] = map[string]any{
					"name":     "smoke-agy",
					"strategy": "centralized",
					"events":   []any{"issues", "issue_comment", "pull_request", "pull_request_comment"},
				}
				expectedTriggers["label_command"] = map[string]any{
					"name":         "smoke",
					"events":       []any{"pull_request"},
					"remove_label": false,
				}
				expectedTriggers["reaction"] = "none"
				expectedTriggers["status-comment"] = false
			}
			assert.Equal(t, expectedTriggers, source["on"])
			assert.Equal(t, []any{"shared/agy-conformance.md"}, source["imports"])
			assert.EqualValues(t, entry.credits, source["max-ai-credits"])
			delete(source, "name")
			delete(source, "description")
			delete(source, "on")
			delete(source, "max-ai-credits")
			delete(source, "imports")
			delete(source, "tools")
			assert.Equal(t, canonical, source, "all compilation paths must retain identical gate configuration")
		})
	}
}

func TestAgySmokeSlashCommandIsCentrallyRouted(t *testing.T) {
	router, err := os.ReadFile("../../.github/workflows/agentic_commands.yml")
	require.NoError(t, err)
	assert.Contains(t, string(router), `"smoke-agy":[{"workflow":"smoke-agy","events":["issue_comment","issues","pull_request","pull_request_comment"]}]`)
	lock, err := os.ReadFile("../../.github/workflows/smoke-agy.lock.yml")
	require.NoError(t, err)
	assert.Contains(t, string(lock), `GH_AW_COMMANDS: "[\"smoke-agy\"]"`)
}

func TestAgySmokeLabelCommandIsCentrallyRouted(t *testing.T) {
	router, err := os.ReadFile("../../.github/workflows/agentic_commands.yml")
	require.NoError(t, err)
	assert.Contains(t, string(router), `"smoke":[{"workflow":"smoke-agy","events":["pull_request"]}`)
	lock, err := os.ReadFile("../../.github/workflows/smoke-agy.lock.yml")
	require.NoError(t, err)
	assert.Contains(t, string(lock), `fromJSON(github.event.inputs.aw_context || '{}').trigger_label == 'smoke'`)
	assert.NotContains(t, string(lock), "remove_trigger_label")
}

func assertAgyConformanceProbes(t *testing.T, compiled agyConformanceWorkflow, workQueueWorker bool) {
	t.Helper()
	steps, environment, commands := agyConformanceStepContent(compiled)
	execution := steps["Execute experimental Agy CLI"]
	require.NotNil(t, execution)
	assert.Equal(t, "agentic_execution", execution["id"])
	assert.EqualValues(t, 10, execution["timeout-minutes"])
	for _, path := range []string{"shared/agy-conformance.md", "shared/engine-conformance.md"} {
		var source struct {
			PreAgentSteps []map[string]any `yaml:"pre-agent-steps"`
			PostSteps     []map[string]any `yaml:"post-steps"`
			MCPScripts    map[string]struct {
				Script string `yaml:"script"`
			} `yaml:"mcp-scripts"`
		}
		readConformanceFrontmatter(t, "../../.github/workflows/"+path, &source)
		for _, expected := range append(source.PreAgentSteps, source.PostSteps...) {
			assertAgyConformanceStep(t, steps[expected["name"].(string)], expected)
		}
		for name, tool := range source.MCPScripts {
			indented := strings.ReplaceAll(strings.TrimSpace(tool.Script), "\n", "\n    ")
			assert.Contains(t, commands, indented, "%s handler must survive compilation", name)
		}
	}
	for _, expected := range []string{"conformance-agy", "mcpscripts conformance_challenge", "safeoutputs noop", "native MCP server `agy-native`", "native MCP server\n`mcpscripts`", "native MCP server\n`safeoutputs`"} {
		assert.Contains(t, environment, expected, "compiled environment/prompt must retain %s", expected)
	}
	for _, expected := range []string{`"agy-native"`, `"native-challenge"`, "--exclude-env GEMINI_API_KEY"} {
		assert.Contains(t, commands, expected, "compiled commands must retain %s", expected)
	}
	if workQueueWorker {
		assert.Contains(t, commands, `export GH_AW_MCP_CLI_SERVERS='["agy-native","mcpscripts","safeoutputs","work-queue"]'`)
	} else {
		assert.NotContains(t, commands, "export GH_AW_MCP_CLI_SERVERS=")
	}
}

func agyConformanceStepContent(compiled agyConformanceWorkflow) (map[string]map[string]any, string, string) {
	steps := make(map[string]map[string]any)
	var environment, commands strings.Builder
	for _, config := range compiled.Jobs {
		for _, step := range config.Steps {
			if name, ok := step["name"].(string); ok {
				steps[name] = step
			}
			if env, ok := step["env"].(map[string]any); ok {
				for _, value := range env {
					if text, ok := value.(string); ok {
						environment.WriteString(text + "\n")
					}
				}
			}
			if run, ok := step["run"].(string); ok {
				commands.WriteString(run + "\n")
			}
		}
	}
	return steps, environment.String(), commands.String()
}

func assertAgyConformanceStep(t *testing.T, actual, expected map[string]any) {
	t.Helper()
	name := expected["name"].(string)
	require.NotNil(t, actual, "compiled gate must retain %s", name)
	encoded, err := yaml.Marshal(expected)
	require.NoError(t, err)
	encoded = []byte(strings.ReplaceAll(string(encoded), "${{ github.aw.import-inputs.engine-id }}", "agy"))
	require.NoError(t, yaml.Unmarshal(encoded, &expected))
	if with, ok := expected["with"].(map[string]any); ok {
		with["retention-days"] = "${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '2' }}"
	}
	for _, key := range []string{"run", "if", "env", "with"} {
		if value, exists := expected[key]; exists {
			assert.Equal(t, value, actual[key], "%s must retain %s", name, key)
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
