package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestBuildAWFConfigJSON_ModelRouting(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			ModelRouting: &CopilotModelRoutingConfig{
				Goal:          "cost",
				Mode:          "balanced",
				AllowedModels: []string{"claude-haiku-4.5", "gpt-5.4-mini"},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData:   data,
	})
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	require.Equal(t, true, config["experimental"].(map[string]any)["modelRouting"])

	apiProxy := config["apiProxy"].(map[string]any)
	require.Equal(t, []any{"github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.4-mini"}, apiProxy["allowedModels"])
	routing := apiProxy["routing"].(map[string]any)
	require.Equal(t, map[string]any{"goal": "cost", "mode": "balanced"}, routing["objective"])
	require.Equal(t, map[string]any{"conversationFile": modelRoutingConversationFile}, routing["task"])

	images := config["container"].(map[string]any)["images"].(map[string]any)
	for _, role := range []string{"agent", "apiProxy", "squid", "router"} {
		require.Regexp(t, `@sha256:[a-f0-9]{64}$`, images[role], role)
	}
}

func TestValidateModelRoutingRequiresMinimumAWFVersion(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"}},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.28"},
		},
	}

	err := validateModelRouting(data, "copilot")
	require.ErrorContains(t, err, "requires AWF v0.28.29 or newer")
}

func TestGenerateModelRoutingConversationStep(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to exercise the generated workflow step")
	}

	var generated strings.Builder
	generateModelRoutingConversationStep(&generated, &WorkflowData{
		EngineConfig: &EngineConfig{ModelRouting: &CopilotModelRoutingConfig{}},
	})

	var workflow struct {
		Steps []struct {
			Name string            `yaml:"name"`
			Env  map[string]string `yaml:"env"`
			Run  string            `yaml:"run"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+generated.String()), &workflow))
	require.Len(t, workflow.Steps, 1)
	step := workflow.Steps[0]
	require.Equal(t, "Prepare model-routing conversation", step.Name)
	require.Contains(t, step.Env, "GH_AW_ROUTING_PROMPT")
	require.Contains(t, step.Env, "GH_AW_ROUTING_CONVERSATION_FILE")
	require.Contains(t, step.Env["GH_AW_ROUTING_CONVERSATION_FILE"], modelRoutingConversationFile)

	tests := []struct {
		name     string
		prompt   string
		system   string
		user     string
		task     string
		taskOnly bool
	}{
		{
			name:     "separates system and task",
			prompt:   "<system>\nboilerplate\n</system>\n  Do the task. \n",
			system:   "<system>\nboilerplate\n</system>",
			user:     "\n  Do the task. \n",
			task:     "Do the task.",
			taskOnly: true,
		},
		{
			name:   "falls back without leading system",
			prompt: "Do the task.",
			user:   "Do the task.",
			task:   "Do the task.",
		},
		{
			name:   "falls back without closing system tag",
			prompt: "<system>\nboilerplate\nDo the task.",
			user:   "<system>\nboilerplate\nDo the task.",
			task:   "<system>\nboilerplate\nDo the task.",
		},
		{
			name:   "falls back when task is empty",
			prompt: "<system>\nboilerplate\n</system> \n",
			user:   "<system>\nboilerplate\n</system> \n",
			task:   "<system>\nboilerplate\n</system>",
		},
		{
			name:     "keeps neutralized system text in task",
			prompt:   "<system>\nboilerplate\n</system>\nHandle (system) text.",
			system:   "<system>\nboilerplate\n</system>",
			user:     "\nHandle (system) text.",
			task:     "Handle (system) text.",
			taskOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			promptPath := filepath.Join(tempDir, "aw-prompts", "prompt.txt")
			conversationFile := filepath.Join(tempDir, "routing-conversation.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(promptPath), 0o700))
			require.NoError(t, os.WriteFile(promptPath, []byte(tt.prompt), 0o600))

			command := exec.Command("bash", "-c", step.Run)
			command.Env = append(os.Environ(),
				"GH_AW_ROUTING_PROMPT="+promptPath,
				"GH_AW_ROUTING_CONVERSATION_FILE="+conversationFile,
			)
			output, err := command.CombinedOutput()
			require.NoError(t, err, string(output))
			if tt.taskOnly {
				require.Contains(t, string(output), "(task only)")
			} else {
				require.Contains(t, string(output), "(full prompt fallback)")
			}

			promptDir := filepath.Dir(promptPath)
			system, err := os.ReadFile(filepath.Join(promptDir, "system.txt"))
			require.NoError(t, err)
			user, err := os.ReadFile(filepath.Join(promptDir, "user.txt"))
			require.NoError(t, err)
			reassembled, err := os.ReadFile(promptPath)
			require.NoError(t, err)
			require.Equal(t, tt.system, string(system))
			require.Equal(t, tt.user, string(user))
			require.Equal(t, tt.prompt, string(reassembled))

			var conversation []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}
			contents, err := os.ReadFile(conversationFile)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(contents, &conversation))
			require.Len(t, conversation, 1)
			require.Equal(t, "user", conversation[0].Role)
			require.Len(t, conversation[0].Parts, 1)
			require.Equal(t, tt.task, conversation[0].Parts[0].Text)

			if runtime.GOOS != "windows" {
				for _, name := range []string{"system.txt", "user.txt", "prompt.txt"} {
					info, err := os.Stat(filepath.Join(promptDir, name))
					require.NoError(t, err)
					require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				}
			}
		})
	}
}

func TestExtractEngineConfig_ModelRouting(t *testing.T) {
	_, config, _ := (&Compiler{}).ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"model-routing": map[string]any{
				"goal":           "cost-speed",
				"mode":           "auto",
				"allowed-models": []any{"gpt-5.4-mini"},
			},
		},
	})

	require.NotNil(t, config)
	require.Equal(t, &CopilotModelRoutingConfig{
		Goal:          "cost-speed",
		Mode:          "auto",
		AllowedModels: []string{"gpt-5.4-mini"},
	}, config.ModelRouting)
}

func TestCopilotModelRoutingDoesNotEmitCompileTimeModel(t *testing.T) {
	env := make(map[string]string)
	(&CopilotEngine{}).addCopilotModelEnv(env, &WorkflowData{
		Model: "small",
		EngineConfig: &EngineConfig{
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced"},
		},
	}, true, "GH_AW_MODEL")

	require.NotContains(t, env, "COPILOT_MODEL")
}
