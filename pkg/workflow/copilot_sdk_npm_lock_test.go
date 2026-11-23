//go:build !integration

package workflow

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCopilotSDKCompilesWithFrozenNPMGraph(t *testing.T) {
	for _, tt := range []struct {
		name, engine string
		extra        map[string]string
	}{
		{name: "built-in"},
		{name: "inline", engine: "\n  driver:\n    node: console.log('driver')"},
		{
			name: "typescript command", engine: "\n  command: ts-node driver.ts\n  cooldown: 7",
			extra: map[string]string{"ts-node": "10.9.2", "typescript": "5.8.3"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sdk.md")
			c := NewCompiler(WithSkipValidation(true))
			data, err := c.ParseWorkflowString("---\non: workflow_dispatch\nengine:\n  id: copilot\n  copilot-sdk: true"+tt.engine+"\n---\nTest the SDK compiler output.\n", path)
			require.NoError(t, err)
			content, err := c.CompileToYAML(data, path)
			require.NoError(t, err)

			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						Name string            `yaml:"name"`
						Run  string            `yaml:"run"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(content), &workflow))
			if tt.extra == nil {
				require.Contains(t, content, `install_copilot_cli.sh" 1.0.113 --checksums-sha256 `+constants.DefaultCopilotSDKNPMCLIChecksumsSHA256)
			} else {
				require.NotContains(t, content, `install_copilot_cli.sh"`)
			}
			for _, step := range workflow.Jobs["agent"].Steps {
				if !strings.HasPrefix(step.Name, "Install GitHub Copilot SDK (") {
					continue
				}
				require.NotContains(t, step.Env, "NPM_CONFIG_MIN_RELEASE_AGE")
				require.NotContains(t, step.Run, "npm install")
				project := copilotSDKNodeProject
				manifestJSON, lockJSON := setup.CopilotSDKNodeManifest, setup.CopilotSDKNodeLock
				if tt.extra != nil {
					project = copilotSDKTypeScriptProject
					manifestJSON, lockJSON = setup.CopilotSDKTypeScriptManifest, setup.CopilotSDKTypeScriptLock
				}
				require.Equal(t, project.installCommand(), step.Run)
				var manifest struct {
					Dependencies map[string]string `json:"dependencies"`
				}
				require.NoError(t, json.Unmarshal([]byte(manifestJSON), &manifest))
				want := map[string]string{"@microsoft/copilot-sdk": string(constants.DefaultCopilotSDKNPMVersion), "undici": "6.29.1"}
				maps.Copy(want, tt.extra)
				require.Equal(t, want, manifest.Dependencies)

				var lock struct {
					LockfileVersion int `json:"lockfileVersion"`
					Packages        map[string]struct {
						Version      string            `json:"version"`
						Dependencies map[string]string `json:"dependencies"`
						Resolved     string            `json:"resolved"`
						Integrity    string            `json:"integrity"`
					} `json:"packages"`
				}
				require.NoError(t, json.Unmarshal([]byte(lockJSON), &lock))
				require.Equal(t, 3, lock.LockfileVersion)
				require.Equal(t, want, lock.Packages[""].Dependencies)
				for pkg, version := range want {
					require.Equal(t, version, lock.Packages["node_modules/"+pkg].Version, pkg)
				}
				for _, platform := range []string{"linux-x64", "darwin-arm64"} {
					require.Equal(t, string(constants.DefaultCopilotSDKNPMVersion), lock.Packages["node_modules/@microsoft/copilot-sdk-"+platform].Version)
				}
				for path, pkg := range lock.Packages {
					if path == "" {
						continue
					}
					require.True(t, strings.HasPrefix(pkg.Resolved, "https://registry.npmjs.org/"), path)
					encoded, ok := strings.CutPrefix(pkg.Integrity, "sha512-")
					require.True(t, ok, path)
					digest, err := base64.StdEncoding.DecodeString(encoded)
					require.NoError(t, err, path)
					require.Len(t, digest, sha512.Size, path)
				}

				return
			}
			t.Fatal("compiled workflow is missing its SDK install step")
		})
	}
}

func TestCopilotSDKFrontmatterPin(t *testing.T) {
	digest := strings.Repeat("a", 64)
	pin := "    version: '1.2.3'\n    sha256: '" + digest + "'\n"
	for _, tc := range []struct {
		name, fields, extra, topLevel, variant, wantError string
	}{
		{name: "Node", fields: pin, variant: "node"},
		{name: "TypeScript", fields: pin, extra: "  command: ts-node driver.ts\n", variant: "typescript"},
		{name: "missing digest", fields: "    version: '1.2.3'\n", wantError: "error:"},
		{name: "invalid version", fields: strings.Replace(pin, "'1.2.3'", "latest", 1), wantError: "version"},
		{name: "invalid digest", fields: strings.Replace(pin, digest, "invalid", 1), wantError: "error:"},
		{name: "unsupported driver", fields: pin, extra: "  command: python3 driver.py\n", wantError: "npm-backed"},
		{name: "checkout disabled", fields: pin, topLevel: "checkout: false\n", wantError: "require a checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sdk.md")
			c := NewCompiler(WithSkipValidation(true))
			source := "---\non: workflow_dispatch\n" + tc.topLevel + "engine:\n  id: copilot\n  version: '1.0.103'\n  copilot-sdk:\n" + tc.fields + tc.extra + "---\nInspect the repository.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			err := c.CompileWorkflow(path)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(filepath.Dir(path), "sdk.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(content), copilotSDKNodeInstallCommand+" "+tc.variant+" "+digest+" 1.2.3")
			metadata, legacy, err := ExtractMetadataFromLockFile(string(content))
			require.NoError(t, err)
			require.False(t, legacy)
			require.NotNil(t, metadata)
			require.Equal(t, "1.2.3", metadata.EngineVersions["copilot-sdk"])
			require.Equal(t, "1.0.103", metadata.EngineVersions["copilot"])
		})
	}
}

func TestCopilotSDKInstallerVerifiesCustomPin(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required", tool)
		}
	}
	script, err := filepath.Abs("../../actions/setup/sh/install_copilot_sdk.sh")
	require.NoError(t, err)
	for _, tc := range []struct{ name, version, digest, wantError string }{
		{name: "matching pin", version: string(constants.DefaultCopilotSDKNPMVersion), digest: copilotSDKNodeProject.digest},
		{name: "mismatched version", version: "0.0.0", digest: copilotSDKNodeProject.digest, wantError: "Expected a registry-only npm v3 lockfile"},
		{name: "mismatched digest", version: string(constants.DefaultCopilotSDKNPMVersion), digest: strings.Repeat("0", 64), wantError: "does not match the compiled workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, ".github", "aw", "copilot-sdk", tc.version, "node")
			require.NoError(t, os.MkdirAll(project, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(project, "package.json"), []byte(setup.CopilotSDKNodeManifest), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(project, "package-lock.json"), []byte(setup.CopilotSDKNodeLock), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "npm"), []byte("#!/bin/bash\nprintf 'npm %s\\n' \"$*\"\n"), 0o700))
			cmd := exec.CommandContext(t.Context(), "bash", script, "node", tc.digest, tc.version)
			cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+root, "RUNNER_TEMP="+root, "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if tc.wantError != "" {
				require.Error(t, err)
				require.Contains(t, string(output), tc.wantError)
				require.NotContains(t, string(output), "npm ci")
				return
			}
			require.NoError(t, err, "%s", output)
			require.Contains(t, string(output), "npm ci --ignore-scripts")
			require.Contains(t, string(output), "--ignore-scripts")
		})
	}
}
