//go:build !integration

package workflow

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCopilotSDKCompilesWithFrozenNPMGraph(t *testing.T) {
	for _, tt := range []struct {
		name, engine string
		extra        map[string]string
		cooldown     string
	}{
		{name: "built-in", cooldown: "3"},
		{name: "inline", engine: "\n  driver:\n    node: console.log('driver')", cooldown: "3"},
		{
			name: "typescript command", engine: "\n  command: ts-node driver.ts\n  cooldown: 7", cooldown: "7",
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
			for _, step := range workflow.Jobs["agent"].Steps {
				if !strings.HasPrefix(step.Name, "Install GitHub Copilot SDK (") {
					continue
				}
				require.Equal(t, tt.cooldown, step.Env["NPM_CONFIG_MIN_RELEASE_AGE"])
				require.NotContains(t, step.Run, "npm install")
				require.Contains(t, step.Run, copilotSDKNodeInstallCommand)
				require.Less(t, len(step.Run), MaxExpressionSize)
				payload := func(delimiter string) []byte {
					_, rest, ok := strings.Cut(step.Run, "<<'"+delimiter+"'\n")
					require.True(t, ok)
					value, _, ok := strings.Cut(rest, "\n"+delimiter+"\n")
					require.True(t, ok)
					return []byte(value)
				}
				var project struct {
					Manifest json.RawMessage `json:"manifest"`
					Lock     json.RawMessage `json:"lock"`
				}
				require.NoError(t, json.Unmarshal(payload("GH_AW_SDK_PROJECT"), &project))
				var manifest struct {
					Dependencies map[string]string `json:"dependencies"`
				}
				require.NoError(t, json.Unmarshal(project.Manifest, &manifest))
				want := map[string]string{"@github/copilot-sdk": string(constants.DefaultCopilotSDKVersion), "undici": "6.28.0"}
				for pkg, version := range tt.extra {
					want[pkg] = version
				}
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
				require.NoError(t, json.Unmarshal(project.Lock, &lock))
				require.Equal(t, 3, lock.LockfileVersion)
				require.Equal(t, want, lock.Packages[""].Dependencies)
				for pkg, version := range want {
					require.Equal(t, version, lock.Packages["node_modules/"+pkg].Version, pkg)
				}
				require.Contains(t, lock.Packages, "node_modules/@github/copilot-sdk-linux-x64")
				require.Contains(t, lock.Packages, "node_modules/@github/copilot-sdk-darwin-arm64")
				require.Contains(t, lock.Packages, "node_modules/koffi")
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
