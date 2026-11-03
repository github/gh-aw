//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPScriptsPerToolEnvironmentBindings(t *testing.T) {
	config := &MCPScriptsConfig{Tools: map[string]*MCPScriptToolConfig{
		"alpha": {Name: "alpha", Script: "return process.env.API_KEY", Env: map[string]string{
			"API_KEY": "${{ secrets.ALPHA }}", "ALPHA_ONLY": "${{ secrets.ALPHA_ONLY }}",
		}},
		"beta": {Name: "beta", Py: "print('beta')", Env: map[string]string{
			"API_KEY": "${{ secrets.BETA }}", "BETA_ONLY": "${{ secrets.BETA_ONLY }}",
		}},
	}}
	bindings := collectMCPScriptsSecrets(config)
	if len(bindings) != 4 {
		t.Fatalf("expected four distinct bindings, got %v", bindings)
	}
	var manifest MCPScriptsConfigJSON
	manifestJSON := GenerateMCPScriptsToolsConfig(config)
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		t.Fatal(err)
	}
	var setup strings.Builder
	if err := generateMCPScriptsSetup(&setup, &WorkflowData{MCPScripts: config}); err != nil {
		t.Fatal(err)
	}
	startup := strings.Split(setup.String(), "      - name: Start MCP Scripts HTTP Server\n")[1]
	for _, tool := range manifest.Tools {
		for name, source := range tool.Env {
			expected := config.Tools[tool.Name].Env[name]
			if bindings[source] != expected || !strings.Contains(startup, source+": "+expected+"\n") {
				t.Errorf("binding %s.%s does not preserve %s", tool.Name, name, expected)
			}
			if strings.Contains(startup, "          "+name+": ") {
				t.Errorf("startup exposes unscoped variable %s", name)
			}
		}
	}
	if strings.Contains(manifestJSON, "secrets.") {
		t.Fatal("manifest must contain references, not secret expressions")
	}
	gatewayEnv := collectMCPEnvironmentVariables(nil, nil, &WorkflowData{MCPScripts: config}, false)
	for source := range bindings {
		if _, exists := gatewayEnv[source]; exists {
			t.Errorf("gateway must not receive tool binding %s", source)
		}
	}
	if gatewayEnv["GH_AW_MCP_SCRIPTS_API_KEY"] == "" || gatewayEnv["GH_AW_MCP_SCRIPTS_PORT"] == "" {
		t.Fatal("gateway must retain MCP script connection settings")
	}
	if manifest.Tools[0].Env["API_KEY"] == manifest.Tools[1].Env["API_KEY"] {
		t.Fatal("overlapping variable names must use distinct transport references")
	}
	if mcpScriptEnvBindingName("a-b", "C") == mcpScriptEnvBindingName("a_b", "C") ||
		mcpScriptEnvBindingName("a", "b_c") == mcpScriptEnvBindingName("a_b", "c") {
		t.Fatal("binding names must not collide after normalization or joining")
	}
}

func TestCollectMCPScriptsSecrets(t *testing.T) {
	tests := []struct {
		name        string
		config      *MCPScriptsConfig
		expectedLen int
	}{
		{
			name:        "nil config",
			config:      nil,
			expectedLen: 0,
		},
		{
			name: "tool with secrets",
			config: &MCPScriptsConfig{
				Tools: map[string]*MCPScriptToolConfig{
					"test": {
						Name: "test",
						Env: map[string]string{
							"API_KEY": "${{ secrets.API_KEY }}",
						},
					},
				},
			},
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectMCPScriptsSecrets(tt.config)

			if len(result) != tt.expectedLen {
				t.Errorf("Expected %d secrets, got %d", tt.expectedLen, len(result))
			}
		})
	}
}

func TestCollectMCPScriptsSecretsStability(t *testing.T) {
	config := &MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
			"zebra-tool": {
				Name: "zebra-tool",
				Env: map[string]string{
					"ZEBRA_SECRET": "${{ secrets.ZEBRA }}",
					"ALPHA_SECRET": "${{ secrets.ALPHA }}",
				},
			},
			"alpha-tool": {
				Name: "alpha-tool",
				Env: map[string]string{
					"BETA_SECRET": "${{ secrets.BETA }}",
				},
			},
		},
	}

	// Test collectMCPScriptsSecrets stability
	iterations := 10
	secretResults := make([]map[string]string, iterations)
	for i := range iterations {
		secretResults[i] = collectMCPScriptsSecrets(config)
	}

	// All iterations should produce same key set
	for i := 1; i < iterations; i++ {
		if len(secretResults[i]) != len(secretResults[0]) {
			t.Errorf("collectMCPScriptsSecrets produced different number of secrets on iteration %d", i+1)
		}
		for key, val := range secretResults[0] {
			if secretResults[i][key] != val {
				t.Errorf("collectMCPScriptsSecrets produced different value for key %s on iteration %d", key, i+1)
			}
		}
	}
}
