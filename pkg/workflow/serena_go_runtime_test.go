//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestSerenaGoRuntimeExposure(t *testing.T) {
	for _, engine := range []string{"copilot", "codex"} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"serena.md", "serena-go.md"} {
				content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "shared", "mcp", name))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
			}
			source := "---\non: workflow_dispatch\nengine: " + engine + "\nimports:\n  - serena-go.md\nsafe-outputs:\n  add-comment:\n    max: 1\n---\nAnalyze Go with Serena.\n"
			workflowPath := filepath.Join(dir, "test.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0o600))
			compiler := NewCompiler()
			data, err := compiler.ParseWorkflowFile(workflowPath)
			require.NoError(t, err)
			require.Empty(t, data.Runtimes, "host-only Serena setup must not opt into runtime network domains")
			require.NoError(t, compiler.CompileWorkflow(workflowPath))
			content, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			lock := string(content)

			require.Contains(t, lock, `GH_AW_SERENA_GO_PATH: ${{ env.GH_AW_SERENA_GO_PATH }}`)
			require.Contains(t, lock, ` -e GH_AW_SERENA_GO_PATH`)
			start := strings.Index(lock, `"serena": {`)
			require.GreaterOrEqual(t, start, 0)
			// The first engine config may contain only client timeouts.
			serverStart := strings.Index(lock[start:], `"entrypoint": "/workspaces/serena/.venv/bin/serena"`)
			require.GreaterOrEqual(t, serverStart, 0)
			server := lock[start+serverStart:]
			end := strings.Index(server, "\n              }")
			require.Positive(t, end)
			server = server[:end]
			require.Contains(t, server, `\${GH_AW_SERENA_GO_PATH}:\${PATH}`)
			require.Contains(t, server, `\${RUNNER_TOOL_CACHE}:\${RUNNER_TOOL_CACHE}:ro`)
			require.Contains(t, server, `"start-mcp-server"`)
			require.Contains(t, server, `\${GITHUB_WORKSPACE}`)
			pathParts := strings.SplitN(server, `"PATH": "`, 2)
			require.Len(t, pathParts, 2)
			serverPath := strings.SplitN(pathParts[1], `"`, 2)[0]

			var compiled struct {
				Jobs map[string]struct {
					Steps []struct {
						Name           string            `yaml:"name"`
						Uses           string            `yaml:"uses"`
						Run            string            `yaml:"run"`
						TimeoutMinutes string            `yaml:"timeout-minutes"`
						With           map[string]string `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			var exposure, healthCheck string
			nodeSetup, exposureStep, healthCheckStep, gatewayStep := -1, -1, -1, -1
			for i, step := range compiled.Jobs["agent"].Steps {
				if step.Name == "Setup Node.js for Serena" {
					nodeSetup = i
					require.True(t, strings.HasPrefix(step.Uses, "actions/setup-node@"))
					require.Equal(t, "24", step.With["node-version"])
				}
				if step.Name == "Expose Go to Serena container" {
					exposureStep = i
					exposure = step.Run
				}
				if step.Name == "Check Serena Go semantic analysis" {
					healthCheckStep = i
					healthCheck = step.Run
					require.Equal(t, "5", step.TimeoutMinutes)
				}
				if step.Name == "Start MCP Gateway" {
					gatewayStep = i
				}
			}
			require.NotEmpty(t, exposure)
			require.NotEmpty(t, healthCheck)
			require.GreaterOrEqual(t, nodeSetup, 0)
			require.GreaterOrEqual(t, exposureStep, 0)
			require.GreaterOrEqual(t, healthCheckStep, 0)
			require.GreaterOrEqual(t, gatewayStep, 0)
			require.Less(t, nodeSetup, exposureStep)
			require.Less(t, exposureStep, healthCheckStep)
			require.Less(t, healthCheckStep, gatewayStep)
			require.Contains(t, compiled.Jobs, "detection")
			for jobID, job := range compiled.Jobs {
				jobYAML, err := yaml.Marshal(job)
				require.NoError(t, err)
				for _, domain := range []string{"nodejs.org", "npmjs.org", "npmjs.com", "storage.googleapis.com"} {
					require.NotContains(t, string(jobYAML), domain, "job %s must not gain Node runtime network domains", jobID)
				}
			}
			testSerenaGoExposureScript(t, exposure, serverPath)
			testSerenaGoHealthCheckScript(t, healthCheck)
		})
	}
}

func testSerenaGoExposureScript(t *testing.T, script, serverPath string) {
	t.Helper()
	for _, scenario := range []string{"mounted", "outside-mount", "missing-binary", "install-failure", "gopls-failure"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			cache := filepath.Join(dir, "tool cache")
			root := filepath.Join(cache, "go", "1.27.4", "x64")
			if scenario == "outside-mount" {
				root = filepath.Join(dir, "outside")
			}
			mockBin := filepath.Join(dir, "mock-bin")
			nodeBin := filepath.Join(cache, "node", "24", "x64", "bin")
			require.NoError(t, os.MkdirAll(mockBin, 0o700))
			require.NoError(t, os.MkdirAll(nodeBin, 0o700))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0o700))
			mock := `#!/bin/sh
if [ "$1" = env ] && [ "$2" = GOROOT ]; then
  [ -z "${GOROOT:-}" ] && [ "$GOTOOLCHAIN" = local ] || exit 1
  printf '%s\n' "$TEST_GO_ROOT"
elif [ "$1" = install ]; then
  [ "$GOROOT" = "$TEST_GO_ROOT" ] || exit 1
  [ "$TEST_SCENARIO" != install-failure ] || exit 1
  [ "$2" = golang.org/x/tools/gopls@v0.22.0 ] || exit 1
  [ "$CGO_ENABLED" = 0 ] && [ "$GOTOOLCHAIN" = local ] || exit 1
  mkdir -p "$GOBIN"
  printf '#!/bin/sh\n[ "$TEST_SCENARIO" != gopls-failure ] || exit 1\nprintf "gopls mocked\\n"\n' > "$GOBIN/gopls"
  chmod +x "$GOBIN/gopls"
else
  [ "$GOROOT" = "$TEST_GO_ROOT" ] || exit 1
  printf 'go version mocked\n'
fi
`
			require.NoError(t, os.WriteFile(filepath.Join(mockBin, "go"), []byte(mock), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(nodeBin, "node"), []byte("#!/bin/sh\nprintf 'node mocked\\n'\n"), 0o700))
			if scenario != "missing-binary" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "bin", "go"), []byte(mock), 0o700))
			}
			envFile := filepath.Join(dir, "github-env")
			require.NoError(t, os.WriteFile(envFile, nil, 0o600))
			cmd := exec.Command("bash", "-c", script)
			runnerPath := mockBin + ":" + nodeBin + ":/usr/bin:/bin"
			cmd.Env = []string{"PATH=" + runnerPath, "TEST_GO_ROOT=" + root, "RUNNER_TOOL_CACHE=" + cache, "GITHUB_ENV=" + envFile, "TEST_SCENARIO=" + scenario, "GOROOT=" + filepath.Join(cache, "go", "1.26.9", "x64")}
			output, err := cmd.CombinedOutput()
			exported, readErr := os.ReadFile(envFile)
			require.NoError(t, readErr)
			if scenario == "mounted" {
				require.NoError(t, err, "%s", output)
				require.Equal(t, "GOROOT="+root+"\nGH_AW_SERENA_GO_PATH="+root+"/bin:"+cache+"/gopls/0.22.0/bin:"+runnerPath+"\n", string(exported))
				// Model the gateway's variable expansion with its own PATH, not the runner's.
				exposedPath := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(string(exported), "\n", 2)[1], "GH_AW_SERENA_GO_PATH="), "\n")
				resolvedPath := os.Expand(strings.ReplaceAll(serverPath, `\$`, `$`), func(name string) string {
					switch name {
					case "GH_AW_SERENA_GO_PATH":
						return exposedPath
					case "PATH":
						return "/usr/bin:/bin"
					default:
						t.Fatalf("unexpected server PATH variable: %s", name)
						return ""
					}
				})
				probe := exec.Command("bash", "-c", "command -v go; command -v node; command -v gopls")
				probe.Env = []string{"PATH=" + resolvedPath}
				paths, err := probe.CombinedOutput()
				require.NoError(t, err, "%s", paths)
				require.Equal(t, root+"/bin/go\n"+nodeBin+"/node\n"+cache+"/gopls/0.22.0/bin/gopls\n", string(paths))
			} else {
				require.Error(t, err)
				require.Contains(t, string(output), "Serena Go")
				require.Empty(t, exported)
				if scenario == "install-failure" {
					require.Contains(t, string(output), "could not install gopls v0.22.0")
				}
				if scenario == "gopls-failure" {
					require.Contains(t, string(output), "installed gopls cannot start")
				}
			}
		})
	}
}

func testSerenaGoHealthCheckScript(t *testing.T, script string) {
	t.Helper()
	for _, scenario := range []string{"healthy", "startup-failure", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "args")
			mock := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_ARGS\"\ncase \"$TEST_SCENARIO\" in startup-failure) exit 1;; timeout) exit 124;; esac\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(mock), 0o700))
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"GITHUB_WORKSPACE=/workspace with spaces",
				"RUNNER_TOOL_CACHE=/tool cache",
				"GH_AW_SERENA_GO_PATH=/tool cache/go/bin:/tool cache/gopls/0.22.0/bin",
				"TEST_ARGS=" + argsFile,
				"TEST_SCENARIO=" + scenario,
			}
			output, err := cmd.CombinedOutput()
			args, readErr := os.ReadFile(argsFile)
			require.NoError(t, readErr)
			require.Contains(t, string(args), "type=bind,source=/workspace with spaces,target=/workspace with spaces\n")
			require.Contains(t, string(args), "type=bind,source=/tool cache,target=/tool cache,readonly\n")
			require.Contains(t, string(args), "PATH=/tool cache/go/bin:/tool cache/gopls/0.22.0/bin:")
			require.Contains(t, string(args), "--entrypoint\n/usr/bin/timeout\nghcr.io/oraios/serena:1.7.0@sha256:6c9459e4246a39c9deaa4f23fb05a526ac6e237b24c8e84a927a098fa1ab6730\n240\n/workspaces/serena/.venv/bin/serena\nproject\nhealth-check\n/workspace with spaces\n")
			if scenario == "healthy" {
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "Serena Go preflight passed")
			} else {
				require.Error(t, err)
				require.Contains(t, string(output), "Serena Go preflight failed")
				require.Contains(t, string(output), "the agent will not start")
				require.NotContains(t, string(output), "preflight passed")
			}
		})
	}
}
