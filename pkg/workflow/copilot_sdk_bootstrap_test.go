//go:build !integration

package workflow

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCopilotSDKNodeBootstrapIsolatedFromRepository(t *testing.T) {
	for _, tool := range []string{"npm", "node", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is required for the SDK bootstrap test")
		}
	}

	fixtures := t.TempDir()
	packages := map[string]string{}
	for name, version := range map[string]string{
		"@github/copilot-sdk": string(constants.DefaultCopilotSDKVersion),
		"undici":              strings.TrimPrefix(copilotSDKWebFetchDependency, "undici@"),
	} {
		archivePath := filepath.Join(fixtures, filepath.Base(name)+".tgz")
		archive, err := os.Create(archivePath)
		require.NoError(t, err)
		gz := gzip.NewWriter(archive)
		tw := tar.NewWriter(gz)
		manifest, err := json.Marshal(map[string]string{"name": name, "version": version, "main": "index.cjs"})
		require.NoError(t, err)
		for path, content := range map[string]string{
			"package/package.json": string(manifest),
			"package/index.cjs":    `module.exports = { bootstrapFixture: true };`,
		} {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(content))}))
			_, err := tw.Write([]byte(content))
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		require.NoError(t, archive.Close())
		packages[name+"@"+version] = archivePath
	}

	for _, inline := range []bool{false, true} {
		name := "normal"
		if inline {
			name = "inline"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "pnpm workspace")
			runnerTemp := filepath.Join(root, "runner temp")
			require.NoError(t, os.MkdirAll(workspace, 0o755))
			originals := map[string]string{
				"package.json": `{
  "name": "sdk-bootstrap-repro",
  "private": true,
  "devEngines": {
    "packageManager": { "name": "pnpm", "version": "^12.20.0", "onFail": "download" }
  },
  "dependencies": { "catalog-dep": "catalog:", "workspace-dep": "workspace:*" },
  "scripts": { "preinstall": "exit 99" }
}`,
				"package-lock.json": `{"name":"sdk-bootstrap-repro","lockfileVersion":3,"packages":{}}`,
				"pnpm-lock.yaml":    "lockfileVersion: '9.0'\n",
				".npmrc":            "engine-strict=true\n",
			}
			for path, content := range originals {
				require.NoError(t, os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o600))
			}
			env := append(os.Environ(),
				"GITHUB_WORKSPACE="+workspace, "RUNNER_TEMP="+runnerTemp,
				"HOME="+root, "NODE_PATH="+filepath.Join(root, "existing-modules"),
				"GH_AW_NODE_BIN=/nonexistent-node-after-runtime-switch",
				"NPM_CONFIG_USERCONFIG="+filepath.Join(root, "empty-npmrc"),
				"NPM_CONFIG_CACHE="+filepath.Join(root, "npm-cache"),
				"NPM_CONFIG_PREFIX="+filepath.Join(root, "different-node-global"),
				"NPM_CONFIG_OFFLINE=true", "NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false",
			)
			run := func(script string) []byte {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
				cmd.Dir = workspace
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				return output
			}
			config := &EngineConfig{ID: "copilot", CopilotSDK: true}
			probe := `const assert = require("node:assert/strict");
const path = require("node:path");
for (const name of ["@github/copilot-sdk", "undici"]) {
  assert.equal(require(name).bootstrapFixture, true);
  assert.ok(require.resolve(name).startsWith(path.join(process.env.RUNNER_TEMP, "gh-aw", "copilot-sdk", "node_modules") + path.sep));
}
assert.ok(process.env.NODE_PATH.endsWith(path.join(process.env.HOME, "existing-modules")));
console.log("isolated SDK loaded");`
			probePath := filepath.Join(runnerTemp, "gh-aw", "actions", "sdk-probe.cjs")
			if inline {
				config.InlineDriver = &InlineEngineDriver{Runtime: "node", Source: probe}
				config.Driver = inlineCopilotSDKDriverWrapperPath
				probePath = filepath.Join(workspace, inlineCopilotSDKDriverNodePath)
				wrapperPath := filepath.Join(workspace, inlineCopilotSDKDriverWrapperPath)
				require.NoError(t, os.MkdirAll(filepath.Dir(wrapperPath), 0o755))
				require.NoError(t, os.WriteFile(wrapperPath, []byte(config.InlineDriver.wrapperScript()), 0o755))
			}
			data := &WorkflowData{EngineConfig: config}
			var install []struct {
				Run string `yaml:"run"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(strings.Join(buildCopilotSDKInstallStep(data), "\n")), &install))
			require.Len(t, install, 1)
			require.Contains(t, install[0].Run, `npm install --prefix "${RUNNER_TEMP}/gh-aw/copilot-sdk"`)
			// Substitute local tarballs, leaving the generated npm invocation and repository policy intact.
			command := install[0].Run
			for dependency, archive := range packages {
				command = strings.ReplaceAll(command, dependency, shellEscapeArg(archive))
			}
			run(command)

			for path, content := range originals {
				actual, err := os.ReadFile(filepath.Join(workspace, path))
				require.NoError(t, err)
				require.Equal(t, content, string(actual), "application file changed: %s", path)
			}
			require.NoFileExists(t, filepath.Join(workspace, "npm-shrinkwrap.json"))
			require.NoDirExists(t, filepath.Join(workspace, "node_modules"))
			require.NoError(t, os.MkdirAll(filepath.Dir(probePath), 0o755))
			require.NoError(t, os.WriteFile(probePath, []byte(probe), 0o600))
			probeCommand := nodeRuntimeResolutionCommandForCopilotSDK + " " + shellEscapeArg(probePath)
			if inline {
				probeCommand = nodeRuntimeResolutionCommandForCopilotSDK + ` -e 'require("node:child_process").execFileSync(process.env.GITHUB_WORKSPACE + "/` + inlineCopilotSDKDriverWrapperPath + `", {stdio: "inherit"})'`
			}
			require.Contains(t, string(run(probeCommand)), "isolated SDK loaded")

			data.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
			sandboxStep := flattenStepText(NewCopilotEngine().GetExecutionSteps(data, "/tmp/gh-aw/test.log"))
			require.Contains(t, sandboxStep, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
			require.Contains(t, sandboxStep, nodePathSetupCommandForCopilotSDK)
		})
	}
}
