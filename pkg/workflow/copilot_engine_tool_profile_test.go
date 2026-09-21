//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goRepositoryProfileTestMarkdown = `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: copilot
  copilot-sdk: true
  tool-profile: go-repository
  harness:
    max-retries: 0
sandbox:
  agent: awf
tools:
  bash: false
  cli-proxy: false
  github:
    mode: local
    min-integrity: none
    allowed: [get_file_contents]
safe-outputs:
  create-pull-request:
  threat-detection: false
---

# Repository policy fixture

Update the Go repository through the configured tools.
`

func parseGoRepositoryProfileTestWorkflow(t *testing.T) *WorkflowData {
	t.Helper()
	return parseGoRepositoryProfileTestSource(t, goRepositoryProfileTestMarkdown)
}

func parseGoRepositoryProfileTestSource(t *testing.T, markdown string) *WorkflowData {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "profile.md")
	require.NoError(t, os.WriteFile(filename, []byte(markdown), 0o600))
	// The file pipeline extracts publication policy before applying tool defaults.
	data, err := NewCompiler().ParseWorkflowFile(filename)
	require.NoError(t, err)
	return data
}

func TestGoRepositoryProfileDefaultsAndContract(t *testing.T) {
	for _, bash := range []string{"false", "[]"} {
		t.Run(bash, func(t *testing.T) {
			markdown := strings.Replace(goRepositoryProfileTestMarkdown, "bash: false", "bash: "+bash, 1)
			data := parseGoRepositoryProfileTestSource(t, markdown)
			require.NoError(t, validateCopilotToolProfile(data))
			assert.True(t, data.BashDisabled)
			assert.True(t, needsGitCommands(data.SafeOutputs), "publication infrastructure must remain enabled")
			assert.Equal(t, "0", data.EngineConfig.HarnessMaxRetries)

			_, args := NewCopilotEngine().buildCopilotArgs(data)
			config := buildCopilotSDKToolConfig(data, args)
			assert.Equal(t, 2, config.Version)
			assert.Equal(t, copilotSDKToolCapabilities{Edit: true, MCP: true}, config.Capabilities)
			assert.Equal(t, []string{"bash", "cli-proxy"}, config.ExplicitlyDisabledTools)
			assert.Equal(t, []string{"github(get_file_contents)", "go_repository", "read", "safeoutputs", "write"}, config.Permissions.AllowedTools)
			require.NotNil(t, config.Profile)
			assert.Equal(t, "go-repository", config.Profile.ID)
			assert.Equal(t, goRepositoryDefaultBranchExpression, config.Profile.RepositoryDefaultBranch)
			for _, permission := range extractCopilotAllowedTools(args) {
				assert.NotEqual(t, "*", permission)
				assert.False(t, strings.HasPrefix(permission, "shell"))
			}
		})
	}
}

func TestGoRepositoryProfileLegacyPRDefaults(t *testing.T) {
	markdown := strings.Replace(goRepositoryProfileTestMarkdown, "  tool-profile: go-repository\n", "", 1)
	data := parseGoRepositoryProfileTestSource(t, markdown)
	assert.False(t, data.BashDisabled)
	assert.Contains(t, data.Tools["bash"], "git commit:*")
	assert.Contains(t, data.Tools["bash"], constants.DefaultBashTools[0])
	_, args := NewCopilotEngine().buildCopilotArgs(data)
	config := buildCopilotSDKToolConfig(data, args)
	assert.Equal(t, 1, config.Version)
	assert.Nil(t, config.Profile)
	assert.True(t, config.Capabilities.Bash)
	assert.NotContains(t, config.Permissions.AllowedTools, "go_repository")
}

func TestGoRepositoryProfileValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*WorkflowData)
		want   string
	}{
		{"unknown profile", func(d *WorkflowData) { d.EngineConfig.ToolProfile = "other" }, "unsupported engine.tool-profile"},
		{"non Copilot", func(d *WorkflowData) { d.EngineConfig.ID = "claude" }, "effective engine"},
		{"effective override", func(d *WorkflowData) { d.AI = "codex" }, "effective engine"},
		{"CLI mode", func(d *WorkflowData) { d.EngineConfig.CopilotSDK = false }, "copilot-sdk: true"},
		{"command", func(d *WorkflowData) { d.EngineConfig.Command = "custom-copilot" }, "bundled"},
		{"driver", func(d *WorkflowData) { d.EngineConfig.Driver = "custom-driver.cjs" }, "bundled"},
		{"inline driver", func(d *WorkflowData) {
			d.EngineConfig.InlineDriver = &InlineEngineDriver{Runtime: "node", Source: "process.exit(0)"}
		}, "bundled"},
		{"harness", func(d *WorkflowData) { d.EngineConfig.HarnessScript = "custom-harness.cjs" }, "bundled"},
		{"cwd", func(d *WorkflowData) { d.EngineConfig.Cwd = "other" }, "overrides"},
		{"args", func(d *WorkflowData) { d.EngineConfig.Args = []string{"--allow-all"} }, "overrides"},
		{"agent", func(d *WorkflowData) { d.EngineConfig.Agent = "custom" }, "overrides"},
		{"extensions", func(d *WorkflowData) { d.EngineConfig.Extensions = []string{"custom"} }, "overrides"},
		{"AWF command override", func(d *WorkflowData) { d.SandboxConfig.Agent.Command = "custom-awf" }, "standard AWF"},
		{"no AWF", func(d *WorkflowData) {
			d.SandboxConfig = nil
			d.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: false}}
		}, "sandbox.agent: awf"},
		{"no PR", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests = nil }, "create-pull-request"},
		{"dynamic target", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests.TargetRepoSlug = "${{ inputs.repository }}" }, "current-repository"},
		{"wildcard target", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests.TargetRepoSlug = "*" }, "current-repository"},
		{"fork", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests.HeadRepoSlug = "other/fork" }, "forks"},
		{"allowed repos", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests.AllowedRepos = []string{"other/repo"} }, "allowed-repos"},
		{"disabled checkout", func(d *WorkflowData) { d.CheckoutDisabled = true }, "enabled current-repository checkout"},
		{"multiple checkouts", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{}, {}} }, "one enabled"},
		{"foreign checkout", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{Repository: "other/repo"}} }, "full current-repository checkout"},
		{"alternate path", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{Path: "src"}} }, "workspace root"},
		{"sparse checkout", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{SparseCheckout: "src/"}} }, "sparse checkout"},
		{"wiki checkout", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{Wiki: true}} }, "wiki"},
		{"submodules", func(d *WorkflowData) { d.CheckoutConfigs = []*CheckoutConfig{{Submodules: "true"}} }, "submodules"},
		{"bash enabled", func(d *WorkflowData) { d.Tools["bash"] = true }, "explicit tools.bash"},
		{"bash omitted", func(d *WorkflowData) { delete(d.ExplicitlyDisabledTools, "bash") }, "explicit tools.bash"},
		{"CLI proxy", func(d *WorkflowData) { d.Tools["cli-proxy"] = true }, "explicit tools.cli-proxy"},
		{"CLI proxy omitted", func(d *WorkflowData) { delete(d.Tools, "cli-proxy") }, "explicit tools.cli-proxy"},
		{"editing disabled", func(d *WorkflowData) { d.Tools["edit"] = false }, "editing"},
		{"MCP name collision", func(d *WorkflowData) {
			d.Tools["go_repository"] = map[string]any{"type": "http", "url": "https://example.com/mcp"}
		}, "reserves"},
		{"imported MCP name collision", func(d *WorkflowData) { d.ResolvedMCPServers = map[string]any{"go_repository": map[string]any{}} }, "reserves"},
		{"secret policy", func(d *WorkflowData) { d.SafeOutputs.CreatePullRequests.BaseBranch = "${{ secrets.BASE }}" }, "non-secret"},
		{"dynamic protection exclusion", func(d *WorkflowData) {
			d.SafeOutputs.CreatePullRequests.ProtectedFilesExclude = []string{"${{ inputs.exclude }}"}
		}, "literal protected-files.exclude"},
		{"excluded metadata", func(d *WorkflowData) { d.ExcludedEnv = []string{goRepositoryDefaultBranchEnv} }, "runtime binding"},
		{"excluded config", func(d *WorkflowData) { d.ExcludedEnv = []string{constants.CopilotSDKToolConfigEnvVar} }, "runtime binding"},
		{"excluded policy input", func(d *WorkflowData) {
			d.SafeOutputs.CreatePullRequests.BaseBranch = "${{ inputs.base }}"
			d.ExcludedEnv = []string{"GH_AW_INPUT_BASE"}
		}, "runtime binding"},
		{"secret binding collision", func(d *WorkflowData) {
			d.EngineConfig.Env = map[string]string{goRepositoryDefaultBranchEnv: "${{ secrets.OVERRIDE }}"}
		}, "runtime binding"},
		{"engine env cwd", func(d *WorkflowData) { d.EngineConfig.Env = map[string]string{"GH_AW_ENGINE_CWD": "other"} }, "overriding GH_AW_ENGINE_CWD"},
		{"workflow env cwd", func(d *WorkflowData) { d.Env = "env:\n  GH_AW_ENGINE_CWD: other\n" }, "overriding GH_AW_ENGINE_CWD"},
		{"engine env repository", func(d *WorkflowData) { d.EngineConfig.Env = map[string]string{"GITHUB_REPOSITORY": "other/repo"} }, "overriding GITHUB_REPOSITORY"},
		{"engine env runner temp", func(d *WorkflowData) { d.EngineConfig.Env = map[string]string{"RUNNER_TEMP": "/other"} }, "overriding RUNNER_TEMP"},
		{"agent env workspace", func(d *WorkflowData) { d.SandboxConfig.Agent.Env = map[string]string{"GITHUB_WORKSPACE": "/other"} }, "overriding GITHUB_WORKSPACE"},
		{"custom checkout", func(d *WorkflowData) { d.CustomSteps = "steps:\n  - uses: actions/checkout@v7\n" }, "compiler-managed checkout"},
		{"pre step checkout", func(d *WorkflowData) { d.PreSteps = "steps:\n  - uses: Actions/Checkout@v7\n" }, "compiler-managed checkout"},
		{"pre agent checkout", func(d *WorkflowData) { d.PreAgentSteps = "steps:\n  - uses: actions/checkout@v7\n" }, "compiler-managed checkout"},
		{"detection execution", func(d *WorkflowData) { d.IsDetectionRun = true }, "main agent execution"},
		{"evaluation execution", func(d *WorkflowData) { d.IsEvalsRun = true }, "main agent execution"},
		{"samples", func(d *WorkflowData) { d.UseSamples = true }, "main agent execution"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := parseGoRepositoryProfileTestWorkflow(t)
			test.mutate(data)
			err := validateCopilotToolProfile(data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestGoRepositoryProfilePreservesNonCheckoutSetupSteps(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.CustomSteps = "steps:\n  - run: echo setup\n    env:\n      uses: actions/checkout@v7\n"
	require.NoError(t, validateCopilotToolProfile(data), "a literal env value must not be mistaken for a checkout action")
}

func TestGoRepositoryProfileRejectsEffectiveEngineOverride(t *testing.T) {
	compiler := NewCompiler(WithEngineOverride("claude"))
	data, err := compiler.ParseWorkflowString(goRepositoryProfileTestMarkdown, "profile.md")
	if err == nil {
		err = validateCopilotToolProfile(data)
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copilot")
}

func TestGoRepositoryProfileRejectsExplicitSecondaryUse(t *testing.T) {
	for _, name := range []string{"evals", "safe-outputs.threat-detection"} {
		t.Run(name, func(t *testing.T) {
			data := parseGoRepositoryProfileTestWorkflow(t)
			data.EngineConfig.ToolProfile = ""
			nested := map[string]any{"engine": map[string]any{"id": "copilot", "tool-profile": "go-repository"}}
			if name == "evals" {
				data.RawFrontmatter["evals"] = nested
			} else {
				data.RawFrontmatter["safe-outputs"] = map[string]any{"threat-detection": nested}
			}
			require.ErrorContains(t, validateCopilotToolProfile(data), name+".engine.tool-profile")
		})
	}
}

func TestGoRepositoryProfileIsNotInheritedBySecondaryEngines(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.Evals = &EvalsConfig{Questions: []EvalDefinition{{ID: "check", Question: "Did the agent finish?"}}}
	data.SafeOutputs.ThreatDetection = nil
	compiler := NewCompiler()
	evals := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	detection := strings.Join(compiler.buildDetectionEngineExecutionStep(data), "")
	for _, steps := range []string{evals, detection} {
		assert.NotEmpty(t, steps)
		assert.NotContains(t, steps, "go_repository")
		assert.NotContains(t, steps, goRepositoryDefaultBranchEnv)
	}
	var steps []struct {
		Env map[string]string `yaml:"env"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(evals), &steps))
	found := false
	for _, step := range steps {
		if raw := step.Env[constants.CopilotSDKToolConfigEnvVar]; raw != "" {
			var config copilotSDKToolConfig
			require.NoError(t, json.Unmarshal([]byte(raw), &config))
			assert.Equal(t, 1, config.Version)
			assert.Nil(t, config.Profile)
			found = true
		}
	}
	assert.True(t, found, "evaluations must retain SDK mode while clearing the profile")
	assert.Equal(t, "go-repository", data.EngineConfig.ToolProfile, "secondary engines must not mutate the main config")
}
