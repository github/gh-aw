//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAiderHarnessProviderRouting(t *testing.T) {
	for _, test := range []struct {
		name, provider, model, endpointProvider, modelsURL, wantModel, baseKey, keyName, wantBase string
	}{
		{"copilot", "github", "copilot/claude-sonnet-4-5", "copilot", "http://localhost:43123/models", "openai/claude-sonnet-4.5", "OPENAI_API_BASE", "OPENAI_API_KEY", "http://localhost:43123"},
		{"openai", "openai", "openai/gpt-5", "openai", "http://localhost:43124/v1/models", "openai/gpt-5", "OPENAI_API_BASE", "OPENAI_API_KEY", "http://localhost:43124/v1"},
		{"codex", "openai", "codex/gpt-5", "openai", "http://localhost:43124/v1/models", "openai/gpt-5", "OPENAI_API_BASE", "OPENAI_API_KEY", "http://localhost:43124/v1"},
		{"anthropic", "anthropic", "anthropic/claude-sonnet-4-5", "anthropic", "http://localhost:43125/v1/models", "anthropic/claude-sonnet-4-5", "ANTHROPIC_API_BASE", "ANTHROPIC_API_KEY", "http://localhost:43125"},
		{"anthropic path prefix", "anthropic", "anthropic/claude-sonnet-4-5", "anthropic", "http://localhost:43125/provider/v1/models", "anthropic/claude-sonnet-4-5", "ANTHROPIC_API_BASE", "ANTHROPIC_API_KEY", "http://localhost:43125/provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "inspect.cjs")
			require.NoError(t, os.WriteFile(fixture, []byte(`process.stdout.write(JSON.stringify({args: process.argv.slice(2), env: process.env}));`), 0o600))
			configPath := filepath.Join(dir, ".aider.conf.yml")
			config := []byte("read: CONVENTIONS.md\n")
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			cmd := aiderHarnessCommand(t, dir, "node", fixture)
			reflectResult := map[string]any{"ok": true, "reflectData": map[string]any{
				"endpoints": []any{
					map[string]any{"provider": "other", "configured": true, "models_url": "http://localhost:1/models"},
					map[string]any{"provider": test.endpointProvider, "configured": true, "models_url": test.modelsURL},
				},
			}}
			reflectJSON, err := json.Marshal(reflectResult)
			require.NoError(t, err)
			cmd.Env = append(cmd.Env, "AWF_REFLECT_ENABLED=1", "GH_AW_AIDER_TEST_REFLECT="+string(reflectJSON),
				"AIDER_MODEL="+test.model, "GH_AW_LLM_PROVIDER="+test.provider,
				"COPILOT_GITHUB_TOKEN=not-for-child", "GITHUB_COPILOT_TOKEN=not-for-child",
				"ANTHROPIC_API_KEY=not-for-child", "CODEX_API_KEY=not-for-child")
			output, err := cmd.Output()
			require.NoError(t, err)
			var result struct {
				Args []string
				Env  map[string]string
			}
			require.NoError(t, json.Unmarshal(output, &result))
			assert.Equal(t, test.wantModel, result.Env["AIDER_MODEL"])
			assert.Equal(t, test.wantBase, result.Env[test.baseKey])
			assert.Equal(t, "awf-proxy", result.Env[test.keyName])
			assert.NotContains(t, result.Env, "COPILOT_GITHUB_TOKEN")
			assert.NotContains(t, result.Env, "GITHUB_COPILOT_TOKEN")
			assert.NotContains(t, result.Env, "CODEX_API_KEY")
			assert.Equal(t, []string{"--message-file", filepath.Join(dir, "prompt with spaces.md")}, result.Args)
			preservedConfig, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Equal(t, config, preservedConfig)
		})
	}
}

func TestAiderHarnessRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		env  []string
		want string
	}{
		{"model", []string{"AIDER_MODEL=missing-provider"}, "AIDER_MODEL must use provider/model format"},
		{"empty model", []string{"AIDER_MODEL=openai/"}, "AIDER_MODEL must use provider/model format"},
		{"provider", []string{"GH_AW_LLM_PROVIDER=unknown"}, "GH_AW_LLM_PROVIDER must be github, anthropic, or openai"},
		{"prompt", []string{"GH_AW_PROMPT="}, "GH_AW_PROMPT is not set"},
		{"missing prompt", []string{"GH_AW_PROMPT=/missing-aider-prompt"}, "ENOENT"},
		{"credentials", []string{"OPENAI_API_KEY=", "CODEX_API_KEY="}, "Aider provider API key is required without AWF"},
		{"copilot without AWF", []string{"GH_AW_LLM_PROVIDER=github"}, "Aider Copilot routing requires the AWF sandbox"},
		{"reflect failure", []string{"AWF_REFLECT_ENABLED=1", `GH_AW_AIDER_TEST_REFLECT={"ok":false}`}, "Unable to discover the Aider LLM endpoint from /reflect"},
		{"wrong endpoint", []string{"AWF_REFLECT_ENABLED=1", `GH_AW_AIDER_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"copilot","configured":true,"models_url":"http://localhost/models"}]}}`}, "No configured /reflect models endpoint found for provider openai"},
		{"unconfigured endpoint", []string{"AWF_REFLECT_ENABLED=1", `GH_AW_AIDER_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"openai","configured":false,"models_url":"http://localhost/models"}]}}`}, "No configured /reflect models endpoint found for provider openai"},
		{"invalid endpoint URL", []string{"AWF_REFLECT_ENABLED=1", `GH_AW_AIDER_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"openai","configured":true,"models_url":"invalid-url"}]}}`}, "Invalid models URL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := aiderHarnessCommand(t, t.TempDir(), "must-not-spawn")
			cmd.Env = append(cmd.Env, test.env...)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.Contains(t, string(output), test.want)
			assert.NotContains(t, string(output), "spawning:")
		})
	}
}

func TestAiderHarnessDirectProviderRouting(t *testing.T) {
	for _, test := range []struct{ provider, model, baseName, baseURL, keyName, wantBase string }{
		{"openai", "openai/gpt-5", "OPENAI_BASE_URL", "http://localhost:43124/proxy/v1", "OPENAI_API_KEY", "http://localhost:43124/proxy/v1"},
		{"anthropic", "anthropic/claude-sonnet-4-5", "ANTHROPIC_BASE_URL", "http://localhost:43125/v1/", "ANTHROPIC_API_KEY", "http://localhost:43125"},
	} {
		t.Run(test.provider, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "inspect.cjs")
			require.NoError(t, os.WriteFile(fixture, []byte(`process.stdout.write(JSON.stringify(process.env));`), 0o600))
			cmd := aiderHarnessCommand(t, dir, "node", fixture)
			cmd.Env = append(cmd.Env, "AIDER_MODEL="+test.model, "GH_AW_LLM_PROVIDER="+test.provider,
				test.baseName+"="+test.baseURL, test.keyName+"=direct-provider-key")
			output, err := cmd.Output()
			require.NoError(t, err)
			var env map[string]string
			require.NoError(t, json.Unmarshal(output, &env))
			assert.Equal(t, "direct-provider-key", env[test.keyName])
			assert.Equal(t, test.wantBase, env[test.baseName])
			assert.Equal(t, test.model, env["AIDER_MODEL"])
		})
	}
}
