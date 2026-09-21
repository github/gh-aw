//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotSDKMCPStagingExportMatrix(t *testing.T) {
	for _, test := range []struct {
		name string
		sdk  bool
		mcp  bool
	}{
		{"SDK with MCP", true, true},
		{"SDK without MCP", true, false},
		{"CLI with MCP", false, true},
		{"CLI without MCP", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: test.sdk}}
			if test.mcp {
				data.SafeOutputs = &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}
			}
			script := buildCopilotMCPConfigExport(data)
			assert.Contains(t, script, `export XDG_CONFIG_HOME="$HOME"`)
			if test.sdk && test.mcp {
				assert.Contains(t, script, `cp "$HOME/.copilot/mcp-config.json" "${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json"`)
				assert.Contains(t, script, `chmod 600 "${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json"`)
				assert.Contains(t, script, `export GH_AW_MCP_CONFIG="${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json"`)
			} else {
				assert.NotContains(t, script, "cp ")
				assert.NotContains(t, script, "mkdir ")
				assert.NotContains(t, script, "chmod ")
				if test.mcp {
					assert.Contains(t, script, `export GH_AW_MCP_CONFIG="$HOME/.copilot/mcp-config.json"`)
				} else {
					assert.Equal(t, "export XDG_CONFIG_HOME=\"$HOME\"\n", script)
				}
			}
		})
	}
}

func TestCopilotSDKMCPStagingExecutesFailClosed(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("generated staging script requires bash: %v", err)
	}
	for _, test := range []struct {
		name          string
		inject        string
		missingSource bool
		missingTemp   bool
		wantError     string
	}{
		{name: "byte preserving success"},
		{name: "mkdir failure", inject: "mkdir() { return 31; }\n", wantError: "Failed to create"},
		{name: "copy failure", inject: "cp() { return 31; }\n", wantError: "Failed to stage"},
		{name: "missing source", missingSource: true, wantError: "Failed to stage"},
		{name: "directory chmod failure", inject: "chmod() { return 31; }\n", wantError: "Failed to secure"},
		{
			name:      "file chmod failure",
			inject:    "chmod() { if [ \"$1\" = 600 ]; then return 31; fi; command chmod \"$@\"; }\n",
			wantError: "Failed to secure",
		},
		{name: "missing runner temp", missingTemp: true, wantError: "RUNNER_TEMP is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home with spaces")
			runnerTemp := filepath.Join(root, "runner temp")
			require.NoError(t, os.MkdirAll(filepath.Join(home, ".copilot"), 0o700))
			fixture := "{\n  \"mcpServers\": {\"safeoutputs\": {\"type\":\"http\", \"headers\":{\"Authorization\":\"Bearer test-credential\"}, \"tools\":[\"noop\"]}}\n}\n"
			if !test.missingSource {
				require.NoError(t, os.WriteFile(filepath.Join(home, ".copilot", "mcp-config.json"), []byte(fixture), 0o600))
			}
			staged := filepath.Join(runnerTemp, "gh-aw", "mcp-config", "copilot-sdk.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o700))
			require.NoError(t, os.WriteFile(staged, []byte("stale config"), 0o600))
			launchMarker := filepath.Join(root, "launched")
			exportMarker := filepath.Join(root, "exported")
			data := &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true},
				SafeOutputs:  &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}},
			}
			script := "set +e\n" + test.inject +
				"trap 'printf \"%s\" \"$GH_AW_MCP_CONFIG\" > \"$GH_AW_TEST_EXPORT\"' EXIT\n" +
				buildCopilotMCPConfigExport(data) +
				"printf launched > \"$GH_AW_TEST_LAUNCH\"\n"
			tempEnv := filepath.ToSlash(runnerTemp)
			if test.missingTemp {
				tempEnv = ""
			}
			command := exec.CommandContext(t.Context(), bash, "-c", script)
			command.Env = append(os.Environ(),
				"HOME="+filepath.ToSlash(home), "RUNNER_TEMP="+tempEnv,
				"GH_AW_MCP_CONFIG=not-staged",
				"GH_AW_TEST_LAUNCH="+filepath.ToSlash(launchMarker),
				"GH_AW_TEST_EXPORT="+filepath.ToSlash(exportMarker),
			)
			output, runErr := command.CombinedOutput()
			assert.NotContains(t, string(output), "test-credential", "config credentials must never be printed")
			exported, err := os.ReadFile(exportMarker)
			require.NoError(t, err)
			if test.wantError != "" {
				require.Error(t, runErr)
				assert.Contains(t, string(output), test.wantError)
				_, err := os.Stat(launchMarker)
				require.ErrorIs(t, err, os.ErrNotExist, "failed staging must terminate even without errexit")
				assert.Equal(t, "not-staged", string(exported), "never export a stale or unprotected config")
				return
			}
			require.NoError(t, runErr, "%s", output)
			assert.Equal(t, filepath.ToSlash(staged), string(exported))
			actual, err := os.ReadFile(staged)
			require.NoError(t, err)
			assert.Equal(t, fixture, string(actual), "converted MCP config must be copied byte-for-byte")
			_, err = os.Stat(launchMarker)
			require.NoError(t, err)
			if runtime.GOOS != "windows" {
				info, err := os.Stat(staged)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}
		})
	}
}

func TestCopilotSDKMCPStagingAfterARCHomeBeforeAWF(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
	steps := NewCopilotEngine().GetExecutionSteps(data, "/tmp/gh-aw/test.log")
	require.Len(t, steps, 1, "staging belongs in the existing execution step")
	script := strings.Join([]string(steps[0]), "\n")
	home := strings.Index(script, `export HOME=`)
	copyConfig := strings.Index(script, `cp "$HOME/.copilot/mcp-config.json"`)
	exportConfig := strings.Index(script, `export GH_AW_MCP_CONFIG="${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json"`)
	harness := strings.Index(script, `copilot_harness.cjs`)
	require.NotEqual(t, -1, home)
	require.NotEqual(t, -1, copyConfig)
	require.NotEqual(t, -1, exportConfig)
	require.NotEqual(t, -1, harness)
	assert.Less(t, home, copyConfig)
	assert.Less(t, copyConfig, exportConfig)
	assert.Less(t, exportConfig, harness)
	assert.Contains(t, script, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
}
