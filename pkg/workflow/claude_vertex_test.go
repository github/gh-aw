//go:build !integration

package workflow

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeVertexTestData() *WorkflowData {
	return &WorkflowData{
		Name:        "claude-vertex",
		Model:       "claude-sonnet-4-6",
		ParsedTools: &ToolsConfig{},
		Tools:       map[string]any{},
		EngineConfig: &EngineConfig{
			ID: "claude",
			Auth: &EngineAuthConfig{
				Type:                           "github-oidc",
				Provider:                       "gcp",
				GoogleWorkloadIdentityProvider: "projects/123/locations/global/workloadIdentityPools/pool/providers/github",
				GoogleServiceAccount:           "claude@my-project.iam.gserviceaccount.com",
				GoogleProject:                  "my-project",
			},
		},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}},
	}
}

func TestIsClaudeVertexWIF(t *testing.T) {
	assert.False(t, isClaudeVertexWIF(nil))
	assert.False(t, isClaudeVertexWIF(&WorkflowData{}))
	assert.False(t, isClaudeVertexWIF(&WorkflowData{EngineConfig: &EngineConfig{}}))
	for _, field := range []string{"complete", "type", "provider", "workload-identity-provider", "service-account", "project"} {
		t.Run(field, func(t *testing.T) {
			data := claudeVertexTestData()
			auth := data.EngineConfig.Auth
			switch field {
			case "type":
				auth.Type = "other"
			case "provider":
				auth.Provider = "anthropic"
			case "workload-identity-provider":
				auth.GoogleWorkloadIdentityProvider = ""
			case "service-account":
				auth.GoogleServiceAccount = ""
			case "project":
				auth.GoogleProject = ""
			}
			assert.Equal(t, field == "complete", isClaudeVertexWIF(data))
			assert.Equal(t, isGeminiVertexWIF(data), isClaudeVertexWIF(data))
		})
	}
}

func TestClaudeVertexEnvironment(t *testing.T) {
	engine := NewClaudeEngine()
	for _, runtime := range supportedAgentRuntimes {
		for _, phase := range []string{"agent", "detection", "evals"} {
			t.Run(string(runtime)+"/"+phase, func(t *testing.T) {
				data := claudeVertexTestData()
				data.SandboxConfig.Agent.Runtime = runtime
				data.IsDetectionRun = phase == "detection"
				data.IsEvalsRun = phase == "evals"
				env := engine.buildClaudeCommandEnv(data)
				host := constants.AWFAPIProxyHostname
				if runtime == AgentRuntimeDockerSudoIptables ||
					(phase != "agent" && runtime != AgentRuntimeNVX) {
					host = "host.docker.internal"
				}
				assert.Equal(t, "http://"+host+":10004/v1", env["ANTHROPIC_VERTEX_BASE_URL"])
				assert.Equal(t, "my-project", env["ANTHROPIC_VERTEX_PROJECT_ID"])
				assert.Equal(t, "vertex", env["GH_AW_LLM_PROVIDER"])
				assert.Equal(t, "global", env["CLOUD_ML_REGION"])
				assert.Equal(t, "1", env["CLAUDE_CODE_USE_VERTEX"])
				assert.Equal(t, "1", env["CLAUDE_CODE_SKIP_VERTEX_AUTH"])
				assert.Equal(t, "8", env["CLAUDE_CODE_MAX_RETRIES"])
				assert.Equal(t, data.Model, env["ANTHROPIC_MODEL"])
				for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
					assert.Equal(t, "${{ '' }}", env[key], key)
				}
				assert.Contains(t, strings.Split(env["NO_PROXY"], ","), host+":10004")
				assert.NotContains(t, env, "no_proxy")
				proxyURL, err := url.Parse(env["ANTHROPIC_VERTEX_BASE_URL"])
				require.NoError(t, err)
				assert.Equal(t, "10004", proxyURL.Port())
			})
		}
	}
}

func TestClaudeVertexEnvironmentOverrides(t *testing.T) {
	engine := NewClaudeEngine()
	for _, source := range []string{"engine", "agent"} {
		t.Run(source, func(t *testing.T) {
			data := claudeVertexTestData()
			data.EngineConfig.Auth.GoogleLocation = "europe-west1"
			overrides := map[string]string{
				"CLAUDE_CODE_USE_VERTEX":       "0",
				"CLAUDE_CODE_SKIP_VERTEX_AUTH": "0",
				"ANTHROPIC_VERTEX_BASE_URL":    "https://example.com",
				"ANTHROPIC_VERTEX_PROJECT_ID":  "other-project",
				"CLOUD_ML_REGION":              "us-central1",
				"CLAUDE_CODE_MAX_RETRIES":      "0",
				"ANTHROPIC_API_KEY":            "${{ secrets.UNRELATED_KEY }}",
				"ANTHROPIC_AUTH_TOKEN":         "unrelated-token",
				"CLAUDE_CODE_OAUTH_TOKEN":      "unrelated-token",
				"ANTHROPIC_BASE_URL":           "https://api.anthropic.com",
				"NO_PROXY":                     "example.com",
				"no_proxy":                     "example.com",
			}
			if source == "engine" {
				data.EngineConfig.Env = maps.Clone(overrides)
			} else {
				data.SandboxConfig.Agent.Env = maps.Clone(overrides)
			}
			env := engine.buildClaudeCommandEnv(data)
			assert.Equal(t, "europe-west1", env["CLOUD_ML_REGION"])
			assert.Equal(t, "my-project", env["ANTHROPIC_VERTEX_PROJECT_ID"])
			assert.Equal(t, "http://api-proxy:10004/v1", env["ANTHROPIC_VERTEX_BASE_URL"])
			assert.Equal(t, "1", env["CLAUDE_CODE_USE_VERTEX"])
			assert.Equal(t, "1", env["CLAUDE_CODE_SKIP_VERTEX_AUTH"])
			assert.Equal(t, "8", env["CLAUDE_CODE_MAX_RETRIES"])
			assert.Equal(t, constants.AWFVertexNoProxyHosts, env["NO_PROXY"])
			assert.NotContains(t, env, "no_proxy")
			for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
				assert.Equal(t, "${{ '' }}", env[key], key)
			}
			assert.NotContains(t, engine.GetRequiredSecretNames(data), "ANTHROPIC_API_KEY")
			assert.Empty(t, engine.GetSecretValidationStep(data))
			filtered := FilterEnvForSecrets(env, engine.GetRequiredSecretNames(data))
			assert.Equal(t, "${{ '' }}", filtered["ANTHROPIC_API_KEY"])
		})
	}
}

func TestClaudeVertexRejectsConflictingModelProviders(t *testing.T) {
	t.Run("GitHub provider", func(t *testing.T) {
		data := claudeVertexTestData()
		data.EngineConfig.LLMProvider = "github"
		require.ErrorContains(t, validateSandboxConfig(data), "Claude Vertex WIF cannot be combined with a non-Anthropic engine.model-provider")
	})
	t.Run("Copilot model", func(t *testing.T) {
		data := claudeVertexTestData()
		data.EngineConfig.LLMProvider = "anthropic"
		data.Model = "copilot/auto"
		require.ErrorContains(t, validateSandboxConfig(data), "Claude Vertex WIF cannot be combined with a copilot/ model")
	})
}

func TestClaudeVertexPreservesAnthropicModes(t *testing.T) {
	engine := NewClaudeEngine()
	for _, wif := range []bool{false, true} {
		t.Run(strconv.FormatBool(wif), func(t *testing.T) {
			data := claudeVertexTestData()
			data.EngineConfig.Auth = nil
			if wif {
				data.EngineConfig.Auth = &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"}
			}
			assert.Equal(t, !wif, len(engine.GetSecretValidationStep(data)) > 0)
			assert.Equal(t, !wif, strings.Contains(strings.Join(engine.GetRequiredSecretNames(data), ","), "ANTHROPIC_API_KEY"))
			env := engine.buildClaudeCommandEnv(data)
			assert.Equal(t, "${{ secrets.ANTHROPIC_API_KEY }}", env["ANTHROPIC_API_KEY"])
			assert.Equal(t, "0", env["CLAUDE_CODE_MAX_RETRIES"])
			assert.NotContains(t, env, "CLAUDE_CODE_USE_VERTEX")
			assert.NotContains(t, env, "ANTHROPIC_VERTEX_BASE_URL")
		})
	}
}

func TestClaudeVertexRequiresSandbox(t *testing.T) {
	data := claudeVertexTestData()
	data.SandboxConfig.Agent.Disabled = true
	data.Features = map[string]any{"dangerously-disable-sandbox-agent": true}
	require.ErrorContains(t, validateSandboxConfig(data), "Claude Vertex WIF requires the agent sandbox")
	data.EngineConfig.Auth = nil
	require.NoError(t, validateSandboxConfig(data))
}

func TestClaudeVertexCompilation(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "unrelated-runner-key")
	for _, runtime := range []AgentRuntime{AgentRuntimeDocker, AgentRuntimeDockerSudoIptables} {
		t.Run(string(runtime), func(t *testing.T) {
			source := fmt.Sprintf(`---
on: workflow_dispatch
strict: false
engine:
  id: claude
  auth:
    type: github-oidc
    provider: gcp
    workload-identity-provider: projects/123/locations/global/workloadIdentityPools/pool/providers/github
    service-account: claude@my-project.iam.gserviceaccount.com
    project: my-project
    location: global
  env:
    ANTHROPIC_API_KEY: ${{ secrets.UNRELATED_KEY }}
    ANTHROPIC_VERTEX_BASE_URL: https://api.anthropic.com
    ANTHROPIC_VERTEX_PROJECT_ID: wrong-project
    CLOUD_ML_REGION: us-central1
    CLAUDE_CODE_MAX_RETRIES: "0"
model: claude-sonnet-4-6
permissions:
  contents: read
  id-token: write
sandbox:
  agent:
    runtime: %s
    env:
      CLOUD_ML_REGION: wrong-region
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
---
Call noop once.
`, runtime)
			workflowPath := filepath.Join(t.TempDir(), "claude-vertex.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(workflowPath))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			agent := extractJobSection(string(lock), "agent")
			host := "api-proxy"
			if runtime == AgentRuntimeDockerSudoIptables {
				host = "host.docker.internal"
			}
			for _, value := range []string{
				"AWF_AUTH_TYPE: github-oidc",
				"AWF_AUTH_PROVIDER: gcp",
				"GH_AW_LLM_PROVIDER: vertex",
				"AWF_AUTH_GCP_WORKLOAD_IDENTITY_PROVIDER: projects/123/locations/global/workloadIdentityPools/pool/providers/github",
				"AWF_AUTH_GCP_SERVICE_ACCOUNT: claude@my-project.iam.gserviceaccount.com",
				"ANTHROPIC_VERTEX_BASE_URL: http://" + host + ":10004/v1",
				"ANTHROPIC_VERTEX_PROJECT_ID: my-project",
				"CLOUD_ML_REGION: global",
				"CLAUDE_CODE_USE_VERTEX: 1",
				"CLAUDE_CODE_SKIP_VERTEX_AUTH: 1",
				"CLAUDE_CODE_MAX_RETRIES: 8",
				"NO_PROXY: " + constants.AWFVertexNoProxyHosts,
				`export no_proxy="${NO_PROXY:-}"`,
				"ANTHROPIC_API_KEY: ${{ '' }}",
				"ANTHROPIC_AUTH_TOKEN: ${{ '' }}",
				"CLAUDE_CODE_OAUTH_TOKEN: ${{ '' }}",
				"ANTHROPIC_BASE_URL: ${{ '' }}",
				"--exclude-env ANTHROPIC_API_KEY",
				"--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN",
			} {
				assert.Contains(t, agent, value)
			}
			for _, value := range []string{
				"Validate ANTHROPIC_API_KEY",
				"secrets.ANTHROPIC_API_KEY",
				"secrets.UNRELATED_KEY",
				"unrelated-runner-key",
				"ANTHROPIC_BASE_URL: https://api.anthropic.com",
				"no_proxy:",
				"GOOGLE_APPLICATION_CREDENTIALS:",
			} {
				assert.NotContains(t, agent, value)
			}
		})
	}
}
