//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestGoRepositoryProfileSDKInstallationAndResolution(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("SDK installation fixture requires bash: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("SDK module resolution fixture requires Node.js: %v", err)
	}
	data := parseGoRepositoryProfileTestWorkflow(t)
	var steps []struct {
		Run string `yaml:"run"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(buildCopilotSDKInstallStep(data), "\n")), &steps))
	require.Len(t, steps, 1)
	require.NotContains(t, steps[0].Run, "GITHUB_WORKSPACE")
	require.Contains(t, steps[0].Run, "npm install --ignore-scripts --no-save @github/copilot-sdk@"+string(constants.DefaultCopilotSDKVersion))
	require.Contains(t, steps[0].Run, copilotSDKWebFetchDependency)
	require.NotContains(t, nodeRuntimeResolutionCommandForGoRepository, "GITHUB_WORKSPACE")
	require.NotContains(t, nodeRuntimeResolutionCommandForGoRepository, "npm root")

	root := t.TempDir()
	workspace := filepath.Join(root, "checkout")
	runnerTemp := filepath.Join(root, "runner temp")
	ambient := filepath.Join(root, "ambient")
	require.NoError(t, os.MkdirAll(workspace, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("untouched"), 0o600))
	ambientPackage := filepath.Join(ambient, "@github", "copilot-sdk")
	require.NoError(t, os.MkdirAll(ambientPackage, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ambientPackage, "index.js"), []byte(`module.exports = "unexpected ambient package";`), 0o600))
	argsFile := filepath.Join(root, "npm-args")
	env := append(os.Environ(),
		"GITHUB_WORKSPACE="+filepath.ToSlash(workspace),
		"RUNNER_TEMP="+filepath.ToSlash(runnerTemp),
		"GH_AW_TEST_ARGS="+filepath.ToSlash(argsFile),
		"GH_AW_NODE_BIN="+filepath.ToSlash(node),
		"NODE_PATH="+filepath.ToSlash(ambient),
	)
	// Stand in for npm without network access or package installation.
	mockNPM := `npm() {
  printf '%s\n' "$@" > "$GH_AW_TEST_ARGS"
  mkdir -p node_modules/@github/copilot-sdk
  printf '%s\n' 'module.exports = "trusted fixture";' > node_modules/@github/copilot-sdk/index.js
}
`
	install := exec.CommandContext(t.Context(), bash, "-c", mockNPM+steps[0].Run)
	install.Dir = workspace
	install.Env = env
	output, err := install.CombinedOutput()
	require.NoError(t, err, "%s", output)
	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	require.Contains(t, string(args), "--ignore-scripts\n--no-save\n")
	_, err = os.Stat(filepath.Join(runnerTemp, "gh-aw", "copilot-sdk", "node_modules", "@github", "copilot-sdk", "index.js"))
	require.NoError(t, err)
	files, err := os.ReadDir(workspace)
	require.NoError(t, err)
	require.Len(t, files, 1, "SDK setup must not dirty the reviewed checkout")
	require.Equal(t, "README.md", files[0].Name())

	resolve := func(runtimeTemp string) ([]byte, error) {
		t.Helper()
		command := exec.CommandContext(t.Context(), bash, "-c", nodeRuntimeResolutionCommandForGoRepository+` -e 'process.stdout.write(require("@github/copilot-sdk"))'`)
		command.Dir = workspace
		command.Env = append(append([]string{}, env...), "RUNNER_TEMP="+filepath.ToSlash(runtimeTemp))
		return command.CombinedOutput()
	}
	output, err = resolve(runnerTemp)
	require.NoError(t, err, "%s", output)
	require.Equal(t, "trusted fixture", string(output))
	output, err = resolve(filepath.Join(root, "not-installed"))
	require.Error(t, err, "a missing private SDK must not fall back to ambient NODE_PATH")
	require.NotContains(t, string(output), "unexpected ambient package")

	install = exec.CommandContext(t.Context(), bash, "-c", mockNPM+steps[0].Run)
	install.Dir = workspace
	install.Env = append(append([]string{}, env...), "RUNNER_TEMP=")
	output, err = install.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "RUNNER_TEMP is required")
}
