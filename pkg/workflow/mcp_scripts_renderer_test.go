//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGoMakeSmokeScriptArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MCP shell scripts require bash")
	}
	source, err := os.ReadFile("../../.github/workflows/shared/go-make.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Scripts map[string]struct {
			Run string `yaml:"run"`
		} `yaml:"mcp-scripts"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	for _, tool := range []string{"go", "make"} {
		script := frontmatter.Scripts[tool].Run
		require.NotEmpty(t, script)
		for _, tc := range []struct {
			name, input, expected string
		}{
			{"build", "build", "build\n"},
			{"multiple arguments", "test -run TestCompile ./pkg/cli", "test\n-run\nTestCompile\n./pkg/cli\n"},
			{"whitespace", "fmt\tbuild\n./...", "fmt\nbuild\n./...\n"},
			{"literal wildcard", "test *", "test\n*\n"},
			{"literal shell operators", "test ; touch forbidden", "test\n;\ntouch\nforbidden\n"},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				outputPath := filepath.Join(dir, "args.txt")
				require.NoError(t, os.WriteFile(filepath.Join(dir, "unrelated.go"), nil, 0600))
				stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" > \"$ARG_OUTPUT\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, tool), []byte(stub), 0755))
				command := exec.Command("bash", "-e", "-c", script)
				command.Dir = dir
				command.Env = append(os.Environ(), "INPUT_ARGS="+tc.input, "ARG_OUTPUT="+outputPath,
					"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				args, err := os.ReadFile(outputPath)
				require.NoError(t, err)
				require.Equal(t, tc.expected, string(args))
				require.NoFileExists(t, filepath.Join(dir, "forbidden"))
			})
		}
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
