//go:build !integration

package workflow

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
			if os.Getenv("CI") == "true" {
				t.Fatalf("%s is required for the SDK bootstrap test in CI: %v", tool, err)
			}
			t.Skip(tool + " is required for the SDK bootstrap test")
		}
	}

	resolverPath, err := filepath.Abs("../../actions/setup/js/copilot_sdk_module_resolution_hooks.cjs")
	require.NoError(t, err)
	missingRunnerTempEnv := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "RUNNER_TEMP=") {
			missingRunnerTempEnv = append(missingRunnerTempEnv, value)
		}
	}
	missingRunnerTemp := exec.CommandContext(t.Context(), "node", "-e", `require(process.argv[1])`, resolverPath)
	missingRunnerTemp.Env = missingRunnerTempEnv
	output, err := missingRunnerTemp.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "RUNNER_TEMP must be set for Copilot SDK module resolution")

	fixtures := t.TempDir()
	dependencies := map[string]string{
		"@github/copilot-sdk": string(constants.DefaultCopilotSDKVersion),
		"undici":              "6.28.0",
	}
	packages := map[string]any{"": map[string]any{"dependencies": dependencies}}
	for name, version := range dependencies {
		archivePath := filepath.Join(fixtures, filepath.Base(name)+".tgz")
		archive, err := os.Create(archivePath)
		require.NoError(t, err)
		gz := gzip.NewWriter(archive)
		tw := tar.NewWriter(gz)
		manifest, err := json.Marshal(map[string]any{
			"name": name, "version": version,
			"exports": map[string]string{"import": "./index.mjs", "require": "./index.cjs"},
			"scripts": map[string]string{"install": "exit 99"},
		})
		require.NoError(t, err)
		for path, content := range map[string]string{
			"package/package.json": string(manifest),
			"package/index.cjs":    `module.exports = { bootstrapFixture: true };`,
			"package/index.mjs":    `export const esmFixture = true; export default { bootstrapFixture: true };`,
		} {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(content))}))
			_, err := tw.Write([]byte(content))
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		require.NoError(t, archive.Close())
		tarball, err := os.ReadFile(archivePath)
		require.NoError(t, err)
		digest := sha512.Sum512(tarball)
		packages["node_modules/"+name] = map[string]any{
			"version": version, "resolved": "file:" + archivePath,
			"integrity": "sha512-" + base64.StdEncoding.EncodeToString(digest[:]), "hasInstallScript": true,
		}
	}
	fixtureLock, err := json.Marshal(map[string]any{"lockfileVersion": 3, "packages": packages})
	require.NoError(t, err)

	for _, name := range []string{"normal", "esm", "inline", "legacy-node"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "pnpm workspace")
			runnerTemp := filepath.Join(root, "runner temp")
			require.NoError(t, os.MkdirAll(workspace, 0o755))
			actionsDir := filepath.Join(runnerTemp, "gh-aw", "actions")
			require.NoError(t, os.MkdirAll(actionsDir, 0o755))
			script, err := os.ReadFile("../../actions/setup/sh/install_copilot_sdk.sh")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(actionsDir, "install_copilot_sdk.sh"), script, 0o600))
			resolverPath := filepath.Join(actionsDir, "copilot_sdk_module_resolution.cjs")
			for _, name := range []string{"copilot_sdk_module_resolution.cjs", "copilot_sdk_module_resolution_hooks.cjs"} {
				resolver, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(actionsDir, name), resolver, 0o600))
			}
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
			globalRootCommand := exec.CommandContext(t.Context(), "npm", "root", "-g")
			globalRootCommand.Env = env
			globalRoot, err := globalRootCommand.Output()
			require.NoError(t, err)
			for name := range dependencies {
				globalPackage := filepath.Join(strings.TrimSpace(string(globalRoot)), filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(globalPackage, 0o755))
				manifest, err := json.Marshal(map[string]string{"name": name, "version": "0.0.0-conflict", "main": "index.cjs"})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(globalPackage, "package.json"), manifest, 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(globalPackage, "index.cjs"), []byte(`module.exports = { bootstrapFixture: false };`), 0o600))
			}
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
  assert.ok(require.resolve(name).startsWith(path.join(process.env.RUNNER_TEMP, "gh-aw", "engine-cli", "copilot-sdk", "node_modules") + path.sep));
}
assert.equal(require("./relative.cjs"), "relative helper");
assert.ok(process.env.NODE_PATH.endsWith(path.join(process.env.HOME, "existing-modules")));
Promise.all(["@github/copilot-sdk", "undici"].map(name => import(name))).then(async modules => {
  for (const module of modules) {
    assert.equal(module.default.bootstrapFixture, true);
    assert.equal(module.esmFixture, true);
  }
  assert.equal((await import("./consumer/index.mjs")).default, "own undici");
  console.log("isolated SDK loaded");
}).catch(error => { console.error(error); process.exitCode = 1; });`
			probePath := filepath.Join(runnerTemp, "gh-aw", "actions", "sdk-probe.cjs")
			if name == "esm" {
				config.Driver = "driver.mjs"
				probePath = filepath.Join(workspace, config.Driver)
				probe = `import { createRequire } from "node:module";
import sdk from "@github/copilot-sdk";
import relative from "./relative.cjs";
const require = createRequire(import.meta.url);
if (!sdk.bootstrapFixture || relative !== "relative helper") throw new Error("ESM resolution failed");
` + probe
			}
			if name == "inline" {
				config.InlineDriver = &InlineEngineDriver{Runtime: "node", Source: probe}
				config.Driver = inlineCopilotSDKDriverWrapperPath
				probePath = filepath.Join(workspace, inlineCopilotSDKDriverNodePath)
				wrapperPath := filepath.Join(workspace, inlineCopilotSDKDriverWrapperPath)
				require.NoError(t, os.MkdirAll(filepath.Dir(wrapperPath), 0o755))
				require.NoError(t, os.WriteFile(wrapperPath, []byte(config.InlineDriver.wrapperScript()), 0o755))
			}
			data := &WorkflowData{EngineConfig: config}
			var install []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(strings.Join(buildCopilotSDKInstallStep(data), "\n")), &install))
			require.Len(t, install, 1)
			require.Contains(t, install[0].Run, copilotSDKNodeInstallCommand)
			for key, value := range install[0].Env {
				env = append(env, key+"="+value)
			}
			// Use equivalent lockfile inputs with local tarballs so npm ci runs offline.
			require.NoError(t, os.WriteFile(filepath.Join(actionsDir, "copilot-sdk-node-package.json"), []byte(copilotSDKNodeProject.manifest), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(actionsDir, "copilot-sdk-node-package-lock.json"), fixtureLock, 0o600))
			originalHash := fmt.Sprintf("%x", sha256.Sum256([]byte(copilotSDKNodeProject.lock)))
			fixtureHash := fmt.Sprintf("%x", sha256.Sum256(fixtureLock))
			command := strings.Replace(install[0].Run, originalHash, fixtureHash, 1)
			require.NotEqual(t, install[0].Run, command)
			if name == "normal" {
				// Exercise a positive cooldown that an already locked graph would otherwise bypass.
				blocked := exec.CommandContext(t.Context(), "bash", "-c", command)
				blocked.Dir = workspace
				blocked.Env = append(env, "NPM_CONFIG_MIN_RELEASE_AGE=1000000")
				output, err := blocked.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), "do not satisfy engine.cooldown")
				require.NoDirExists(t, filepath.Join(runnerTemp, "gh-aw", "engine-cli", "copilot-sdk"))
				tampered := exec.CommandContext(t.Context(), "bash", "-c", install[0].Run)
				tampered.Dir = workspace
				tampered.Env = env
				output, err = tampered.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), "Copilot SDK asset hash mismatch")
				require.NoFileExists(t, filepath.Join(runnerTemp, "gh-aw", "engine-cli", "copilot-sdk", "package.json"))
				require.NoError(t, os.Remove(filepath.Join(runnerTemp, "gh-aw", "engine-cli", "copilot-sdk")))
				require.NoError(t, os.Symlink(workspace, filepath.Join(runnerTemp, "gh-aw", "engine-cli", "copilot-sdk")))
				symlinked := exec.CommandContext(t.Context(), "bash", "-c", command)
				symlinked.Dir = workspace
				symlinked.Env = env
				output, err = symlinked.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), "Unexpected symlink")
				require.NoError(t, os.Remove(filepath.Join(runnerTemp, "gh-aw", "engine-cli", "copilot-sdk")))
				require.NoError(t, os.Remove(filepath.Join(runnerTemp, "gh-aw", "engine-cli")))
				require.NoError(t, os.Symlink(workspace, filepath.Join(runnerTemp, "gh-aw", "engine-cli")))
				symlinked = exec.CommandContext(t.Context(), "bash", "-c", command)
				symlinked.Dir = workspace
				symlinked.Env = env
				output, err = symlinked.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), "Unexpected symlink")
				require.NoDirExists(t, filepath.Join(workspace, "copilot-sdk"))
				require.NoError(t, os.Remove(filepath.Join(runnerTemp, "gh-aw", "engine-cli")))
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
			require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(probePath), "relative.cjs"), []byte(`module.exports = "relative helper";`), 0o600))
			for path, content := range map[string]string{
				"consumer/index.mjs":                        `import { localVersion } from "undici"; export default localVersion;`,
				"consumer/node_modules/undici/package.json": `{"name":"undici","type":"module","exports":"./index.mjs"}`,
				"consumer/node_modules/undici/index.mjs":    `export const localVersion = "own undici";`,
			} {
				target := filepath.Join(filepath.Dir(probePath), path)
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
				require.NoError(t, os.WriteFile(target, []byte(content), 0o600))
			}
			probeCommand := nodeRuntimeResolutionCommandForCopilotSDK + " --import " + shellEscapeArg(resolverPath) + " " + shellEscapeArg(probePath)
			if name == "inline" {
				probeCommand = nodeRuntimeResolutionCommandForCopilotSDK + ` -e 'require("node:child_process").execFileSync(process.env.GITHUB_WORKSPACE + "/` + inlineCopilotSDKDriverWrapperPath + `", {stdio: "inherit"})'`
			}
			if name == "legacy-node" {
				probeCommand = nodeRuntimeResolutionCommandForCopilotSDK + ` -e 'require("node:module").registerHooks = undefined; require(process.argv[1]); require(process.argv[2]);' ` + shellEscapeArg(resolverPath) + " " + shellEscapeArg(probePath)
			}
			require.Contains(t, string(run(probeCommand)), "isolated SDK loaded")

			data.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
			sandboxStep := flattenStepText(NewCopilotEngine().GetExecutionSteps(data, "/tmp/gh-aw/test.log"))
			require.Contains(t, sandboxStep, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
			require.Contains(t, sandboxStep, nodePathSetupCommandForCopilotSDK)
		})
	}
}
