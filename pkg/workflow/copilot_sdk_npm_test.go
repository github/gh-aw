//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotSDKInstallDependenciesArePinned(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"node", "typescript"} {
		t.Run(profile, func(t *testing.T) {
			manifest, err := copilotSDKNpmManifests.ReadFile("data/copilot-sdk/" + profile + "/package.json")
			require.NoError(t, err)
			var pkg struct {
				Dependencies map[string]string `json:"dependencies"`
			}
			require.NoError(t, json.Unmarshal(manifest, &pkg))
			require.Equal(t, string(constants.DefaultCopilotSDKVersion), pkg.Dependencies["@github/copilot-sdk"])
			require.Contains(t, pkg.Dependencies, "undici")
			if profile == "typescript" {
				assert.Equal(t, "10.9.2", pkg.Dependencies["ts-node"])
				assert.Equal(t, "5.8.3", pkg.Dependencies["typescript"])
			}
			exactVersion := regexp.MustCompile(`^\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$`)
			for name, version := range pkg.Dependencies {
				assert.Regexp(t, exactVersion, version, name)
			}
			lockData, err := copilotSDKNpmManifests.ReadFile("data/copilot-sdk/" + profile + "/package-lock.json")
			require.NoError(t, err)
			var lock struct {
				Packages map[string]struct {
					Version      string            `json:"version"`
					Resolved     string            `json:"resolved"`
					Integrity    string            `json:"integrity"`
					Dependencies map[string]string `json:"dependencies"`
				} `json:"packages"`
			}
			require.NoError(t, json.Unmarshal(lockData, &lock))
			assert.Equal(t, pkg.Dependencies, lock.Packages[""].Dependencies)
			for name, version := range pkg.Dependencies {
				assert.Equal(t, version, lock.Packages["node_modules/"+name].Version)
			}
			for name, entry := range lock.Packages {
				if name == "" {
					continue
				}
				assert.Regexp(t, exactVersion, entry.Version, name)
				assert.True(t, strings.HasPrefix(entry.Resolved, "https://registry.npmjs.org/"), name)
				assert.True(t, strings.HasPrefix(entry.Integrity, "sha512-"), name)
			}
		})
	}
}

func TestCopilotSDKGeneratedLockedInstall(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]copilotSDKInstallSpec{
		"node":              getCopilotSDKInstallSpec("node driver.js"),
		"typescript":        getCopilotSDKInstallSpec("ts-node driver.ts"),
		"inline-node":       getInlineCopilotSDKInstallSpec("node"),
		"inline-typescript": getInlineCopilotSDKInstallSpec("typescript"),
	} {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"private":true}`), 0600))
			require.NoError(t, os.Mkdir(filepath.Join(workspace, "node_modules"), 0700))
			bin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\n"+
				`test "$*" = "ci --ignore-scripts --no-audit --no-fund" && test -f package.json && test -f package-lock.json`+"\n"), 0700))
			script := strings.Join(spec.runLines, "\n")
			require.Contains(t, script, copilotSDKNpmCICommand)
			require.NotContains(t, script, "npm install")
			cmd := exec.Command("bash", "-e", "-c", script)
			cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+workspace, "PATH="+bin+":"+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			got, err := os.ReadFile(filepath.Join(workspace, "package.json"))
			require.NoError(t, err)
			assert.Equal(t, `{"private":true}`, string(got))
			_, err = os.Stat(filepath.Join(workspace, "node_modules"))
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(workspace, ".gh-aw", "copilot-sdk", "package-lock.json"))
			require.NoError(t, err)
			expected, err := copilotSDKNpmManifests.ReadFile("data/copilot-sdk/" + spec.runtimeID + "/package-lock.json")
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(lock))
		})
	}
}

func TestCompilerDefaultNpmAndPipInstallVersions(t *testing.T) {
	t.Parallel()
	exactNpmPackage := regexp.MustCompile(`^(@[^/]+/)?[^@]+@\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$`)
	for _, engine := range []CodingAgentEngine{NewClaudeEngine(), NewCodexEngine(), NewGeminiEngine(), NewPiEngine()} {
		t.Run(engine.GetID(), func(t *testing.T) {
			found := false
			for _, step := range engine.GetInstallationSteps(&WorkflowData{}) {
				for _, line := range step {
					if _, packages, ok := strings.Cut(line, "npm install "); ok {
						found = true
						for field := range strings.FieldsSeq(packages) {
							if !strings.HasPrefix(field, "-") {
								assert.Regexp(t, exactNpmPackage, field)
							}
						}
					}
				}
			}
			require.True(t, found)
		})
	}
	t.Run("lsp", func(t *testing.T) {
		servers := make(map[string]LSPServerConfig)
		for language, spec := range lspInstallSpecs {
			if len(spec.NpmPackages) > 0 {
				servers[language] = LSPServerConfig{}
			}
		}
		for _, step := range NewLSPManager(servers).GenerateInstallSteps(nil) {
			for _, line := range step {
				if _, packages, ok := strings.Cut(line, "npm install "); ok {
					for pkg := range strings.FieldsSeq(packages) {
						if !strings.HasPrefix(pkg, "-") {
							assert.Regexp(t, exactNpmPackage, pkg)
						}
					}
				}
			}
		}
	})
	for name, spec := range map[string]copilotSDKInstallSpec{
		"python":        getCopilotSDKInstallSpec("python3 driver.py"),
		"inline-python": getInlineCopilotSDKInstallSpec("python"),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, spec.command, "python3 -m pip install ")
			assert.Contains(t, spec.command, "github-copilot-sdk=="+string(constants.DefaultCopilotSDKVersion))
			assert.NotContains(t, spec.command, "--upgrade")
		})
	}
}

func TestCopilotSDKIsolatedModuleResolution(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	workspace := t.TempDir()
	for dir, value := range map[string]string{
		"node_modules/undici":                    "workspace",
		".gh-aw/copilot-sdk/node_modules/undici": "locked",
	} {
		path := filepath.Join(workspace, dir)
		require.NoError(t, os.MkdirAll(path, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(path, "index.js"), []byte("module.exports = '"+value+"'"), 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".gh-aw/copilot-sdk/resolve.cjs"), []byte(copilotSDKNpmResolver), 0600))
	for name, source := range map[string]string{
		"commonjs": `console.log(require("undici"))`,
		"esm":      `import("undici").then(m => console.log(m.default))`,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("bash", "-e", "-c", nodePathSetupCommandForCopilotSDK+`; exec "$GH_AW_NODE_BIN" -e "$DRIVER_SOURCE"`)
			cmd.Dir = workspace
			cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+workspace, "GH_AW_NODE_BIN="+node, "DRIVER_SOURCE="+source)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			assert.Equal(t, "locked\n", string(output))
		})
	}
}
