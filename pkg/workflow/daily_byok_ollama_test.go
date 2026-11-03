//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyBYOKOllamaStartup(t *testing.T) {
	var source struct {
		Steps []struct {
			Name string            `yaml:"name"`
			Env  map[string]string `yaml:"env"`
			Run  string            `yaml:"run"`
		} `yaml:"steps"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/daily-byok-ollama-test.md", &source)
	for _, step := range source.Steps {
		if step.Name != "Start Ollama service" {
			continue
		}
		// llama.cpp b9840 reads this inherited flag before loading model weights.
		assert.Equal(t, "0", step.Env["LLAMA_ARG_REPACK"])
		assert.Equal(t, "32768", step.Env["OLLAMA_CONTEXT_LENGTH"])
		assert.Contains(t, step.Run, `nohup ollama serve > "$OLLAMA_LOG" 2>&1 < /dev/null &`)
		return
	}
	t.Fatal("missing Start Ollama service step")
}

func TestDailyBYOKOllamaContextWindow(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required for the Ollama context check")
	}
	var source struct {
		Steps []struct {
			Name string            `yaml:"name"`
			Env  map[string]string `yaml:"env"`
			Run  string            `yaml:"run"`
		} `yaml:"steps"`
		Tools       map[string]any `yaml:"tools"`
		SafeOutputs map[string]any `yaml:"safe-outputs"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/daily-byok-ollama-test.md", &source)
	assert.Equal(t, false, source.Tools["github"], "a write-only task does not need GitHub read schemas")
	assert.Contains(t, source.SafeOutputs, "create-issue")
	var script string
	for _, step := range source.Steps {
		if step.Name == "Verify Ollama context window" {
			script = step.Run
			require.Equal(t, "32768", step.Env["OLLAMA_MIN_CONTEXT"])
		}
	}
	require.NotEmpty(t, script)
	for _, tc := range []struct {
		name     string
		response string
		curlExit string
		wantOK   bool
	}{
		{"full context", `{"models":[{"name":"qwen2.5:0.5b","context_length":32768}]}`, "0", true},
		{"truncated run context", `{"models":[{"name":"qwen2.5:0.5b","context_length":4096}]}`, "0", false},
		{"wrong model", `{"models":[{"name":"other","context_length":32768}]}`, "0", false},
		{"unloaded model", `{"models":[]}`, "0", false},
		{"missing context", `{"models":[{"name":"qwen2.5:0.5b"}]}`, "0", false},
		{"invalid context", `{"models":[{"name":"qwen2.5:0.5b","context_length":"32768"}]}`, "0", false},
		{"invalid response", `{`, "0", false},
		{"server unavailable", ``, "7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-eu", "-c", `curl() { printf '%s' "$TEST_MODELS"; return "$TEST_CURL_EXIT"; }
`+script)
			cmd.Env = append(os.Environ(), "OLLAMA_MODEL=qwen2.5:0.5b", "OLLAMA_MIN_CONTEXT=32768",
				"TEST_MODELS="+tc.response, "TEST_CURL_EXIT="+tc.curlExit)
			output, err := cmd.CombinedOutput()
			if tc.wantOK {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "%s", output)
				assert.Contains(t, string(output), "::error::")
			}
		})
	}
}

func TestDailyBYOKOllamaRejectsInputTruncation(t *testing.T) {
	var source struct {
		PostSteps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"post-steps"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/daily-byok-ollama-test.md", &source)
	require.Len(t, source.PostSteps, 1)
	for _, tc := range []struct {
		name    string
		content string
		missing bool
		wantOK  bool
	}{
		{"no truncation", "POST /v1/chat/completions 200\n", false, true},
		{"input truncated", `level=WARN msg="truncating input prompt" limit=2050 prompt=25870 keep=4 new=2050`, false, false},
		{"missing evidence", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "ollama.log")
			if !tc.missing {
				require.NoError(t, os.WriteFile(log, []byte(tc.content), 0o600))
			}
			cmd := exec.Command("bash", "-eu", "-c", `ollama() { return 0; }
pgrep() { return 1; }
`+source.PostSteps[0].Run)
			cmd.Env = append(os.Environ(), "OLLAMA_LOG="+log)
			output, err := cmd.CombinedOutput()
			if tc.wantOK {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "%s", output)
				assert.Contains(t, string(output), "::error::")
			}
		})
	}
}

func TestDailyBYOKOllamaDetectionUsesHostedProvider(t *testing.T) {
	var source struct {
		Engine      EngineConfig      `yaml:"engine"`
		Permissions map[string]string `yaml:"permissions"`
		SafeOutputs map[string]any    `yaml:"safe-outputs"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/daily-byok-ollama-test.md", &source)
	detection := NewCompiler().parseThreatDetectionConfig(source.SafeOutputs)
	require.NotNil(t, detection)
	require.NotNil(t, detection.EngineConfig)
	require.Equal(t, "write", source.Permissions["copilot-requests"])
	data := &WorkflowData{
		AI: "copilot", EngineConfig: &source.Engine,
		Permissions:        "contents: read\ncopilot-requests: write",
		SafeOutputs:        &SafeOutputsConfig{ThreatDetection: detection},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	assert.Equal(t, "http://host.docker.internal:11434/v1", source.Engine.Env["COPILOT_PROVIDER_BASE_URL"])
	assert.Equal(t, "qwen2.5:0.5b", source.Engine.Env["COPILOT_MODEL"])
	compiler := NewCompiler()
	require.Equal(t, "claude", compiler.getThreatDetectionEngineID(data))
	detector := buildExternalDetectorWorkflowData(data, "claude")
	assert.Empty(t, detector.EngineConfig.Env)
	assert.Equal(t, LLMProviderGitHub, NewClaudeEngine().ResolveLLMProvider(detector))
	for name, build := range map[string]func(*WorkflowData) []string{
		"external": compiler.buildExternalDetectorExecutionStep,
		"inline":   compiler.buildDetectionEngineExecutionStep,
	} {
		t.Run(name, func(t *testing.T) {
			steps := strings.Join(build(data), "\n")
			assert.NotContains(t, steps, "host.docker.internal:11434")
			assert.NotContains(t, steps, "env.OLLAMA_API_KEY")
			assert.NotContains(t, steps, "qwen2.5:0.5b")
			assert.Contains(t, steps, "COPILOT_GITHUB_TOKEN: ${{ github.token }}")
			assert.Contains(t, steps, "claude-haiku-4.5")
			assert.NotContains(t, steps, "secrets.ANTHROPIC_API_KEY")
		})
	}
}
