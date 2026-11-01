package workflow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNpmEngineCooldownAtInstallationTime(t *testing.T) {
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm is required for the local registry installation test")
	}
	configJSON, err := exec.Command(npm, "config", "list", "--json").Output()
	require.NoError(t, err)
	var config map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(configJSON, &config))
	if _, supported := config["min-release-age"]; !supported {
		t.Skip("npm with built-in min-release-age support is required")
	}

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, content := range map[string]string{
		"package/package.json": `{"name":"cooldown-test","version":"1.2.3","bin":{"cooldown-test":"cli.js"}}`,
		"package/cli.js":       "#!/usr/bin/env node\n",
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	for _, test := range []struct {
		name    string
		ageDays int
		days    int
		allowed bool
	}{
		{"recent-pinned-release", 1, 3, false},
		{"old-pinned-release", 8, 3, true},
		{"custom-cooldown-rejects", 4, 7, false},
		{"custom-cooldown-allows", 8, 7, true},
		{"disabled-cooldown", 1, 0, true},
	} {
		for _, microVM := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/host", true: "/microVM"}[microVM], func(t *testing.T) {
				var downloads atomic.Int32
				var registry *httptest.Server
				registry = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/cooldown-test":
						w.Header().Set("Content-Type", "application/json")
						metadata := map[string]any{
							"name":      "cooldown-test",
							"dist-tags": map[string]string{"latest": "1.2.3"},
							"versions": map[string]any{"1.2.3": map[string]any{
								"name": "cooldown-test", "version": "1.2.3",
								"bin":  map[string]string{"cooldown-test": "cli.js"},
								"dist": map[string]string{"tarball": registry.URL + "/archive.tgz"},
							}},
							"time": map[string]string{"1.2.3": time.Now().Add(-time.Duration(test.ageDays) * 24 * time.Hour).UTC().Format(time.RFC3339)},
						}
						if err := json.NewEncoder(w).Encode(metadata); err != nil {
							t.Errorf("encode package metadata: %v", err)
						}
					case "/archive.tgz":
						downloads.Add(1)
						_, _ = w.Write(archive.Bytes())
					default:
						http.NotFound(w, r)
					}
				}))
				defer registry.Close()

				dir := t.TempDir()
				days := test.days
				step := GenerateNpmInstallStepsWithScope("cooldown-test", "${{ inputs.version }}", "Install", "cooldown-test", NPMInstallOptions{
					CooldownDays: &days,
				})[0]
				if microVM {
					step = GenerateMicroVMNpmCLIInstallStep("cooldown-test", "${{ inputs.version }}", "Install", "cooldown-test", false, true, days)
				}
				var parsed []struct {
					Run string            `yaml:"run"`
					Env map[string]string `yaml:"env"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(strings.Join(step, "\n")), &parsed))
				require.Len(t, parsed, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-e", "-c", parsed[0].Run)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(),
					"HOME="+dir, "RUNNER_TEMP="+dir,
					"NPM_CONFIG_REGISTRY="+registry.URL,
					"NPM_CONFIG_CACHE="+filepath.Join(dir, "cache"),
					"NPM_CONFIG_USERCONFIG="+filepath.Join(dir, ".npmrc"),
					"NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false",
					"NPM_CONFIG_FETCH_RETRIES=0",
				)
				for name, value := range parsed[0].Env {
					if name == "ENGINE_VERSION" {
						value = "1.2.3"
					}
					cmd.Env = append(cmd.Env, name+"="+value)
				}
				output, err := cmd.CombinedOutput()
				if test.allowed {
					require.NoError(t, err, "%s", output)
					assert.Positive(t, downloads.Load())
				} else {
					require.Error(t, err, "%s", output)
					assert.Contains(t, string(output), "ETARGET")
					assert.Zero(t, downloads.Load(), "recent releases must be rejected before downloading the archive")
				}
			})
		}
	}
}

func TestNpmEngineInstallPathsUseBuiltinCooldown(t *testing.T) {
	for _, days := range []int{0, 3, 7} {
		hostSteps := GenerateNpmInstallSteps("example-engine", "1.2.3", "Install", "example", NPMInstallOptions{
			CooldownDays: &days,
		})
		host := strings.Join(hostSteps[0], "\n")
		assert.Contains(t, host, "run: npm install --ignore-scripts -g example-engine@1.2.3")
		microVM := strings.Join(GenerateMicroVMNpmCLIInstallStep("example-engine", "1.2.3", "Install microVM", "example", false, true, days), "\n")
		for name, step := range map[string]string{"host": host, "microVM": microVM} {
			t.Run(fmt.Sprintf("%s/%d", name, days), func(t *testing.T) {
				assert.Contains(t, step, fmt.Sprintf("NPM_CONFIG_MIN_RELEASE_AGE: '%d'", days))
				assert.NotContains(t, step, "npm --version")
				assert.NotContains(t, step, "NPM_VERSION")
				assert.NotContains(t, step, "awk")
			})
		}
	}
}

// TestResolveRuntimeCooldown_NilWorkflowData verifies nil WorkflowData defaults to
// cooldown enabled (true).
func TestResolveRuntimeCooldown_NilWorkflowData(t *testing.T) {
	assert.True(t, resolveRuntimeCooldown(nil, "node"))
}

// TestResolveRuntimeCooldown_TypedRuntimeConfig covers the RuntimesTyped path for
// every supported runtime ID, both when Cooldown is explicitly set and when it is nil.
func TestResolveRuntimeCooldown_TypedRuntimeConfig(t *testing.T) {
	tests := []struct {
		name      string
		runtimeID string
		configure func(rt *RuntimesConfig, cfg *RuntimeConfig)
	}{
		{"node", "node", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Node = cfg }},
		{"python", "python", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Python = cfg }},
		{"go", "go", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Go = cfg }},
		{"uv", "uv", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.UV = cfg }},
		{"bun", "bun", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Bun = cfg }},
		{"deno", "deno", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Deno = cfg }},
		{"dotnet", "dotnet", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Dotnet = cfg }},
		{"elixir", "elixir", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Elixir = cfg }},
		{"gh-aw", "gh-aw", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.GhAw = cfg }},
		{"haskell", "haskell", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Haskell = cfg }},
		{"java", "java", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Java = cfg }},
		{"ruby", "ruby", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Ruby = cfg }},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/cooldown-false", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: boolPtr(false)}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
			}
			assert.False(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})

		t.Run(tt.name+"/cooldown-true", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: boolPtr(true)}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
			}
			assert.True(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})

		t.Run(tt.name+"/cooldown-nil-falls-through", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: nil}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
				Runtimes:          map[string]any{},
			}
			// Cooldown nil on typed config => falls through to legacy Runtimes map,
			// which is empty here, so it should default to true.
			assert.True(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})
	}
}

// TestResolveRuntimeCooldown_UnknownRuntimeID verifies an unrecognized runtime ID
// leaves runtimeConfig nil in the typed switch and falls through to the legacy path.
func TestResolveRuntimeCooldown_UnknownRuntimeID(t *testing.T) {
	wd := &WorkflowData{
		ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: &RuntimesConfig{}},
		Runtimes:          map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd, "unknown-runtime"))
}

// TestResolveRuntimeCooldown_NoParsedFrontmatter verifies the function falls
// through to the legacy Runtimes map when ParsedFrontmatter or RuntimesTyped is nil.
func TestResolveRuntimeCooldown_NoParsedFrontmatter(t *testing.T) {
	wd := &WorkflowData{
		Runtimes: map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd, "node"))

	wd2 := &WorkflowData{
		ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: nil},
		Runtimes:          map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd2, "node"))
}

// TestResolveRuntimeCooldown_LegacyRuntimesMap covers the legacy map[string]any
// path: missing runtime key, wrong type entry, missing cooldown key, non-bool
// cooldown value, and valid bool cooldown values.
func TestResolveRuntimeCooldown_LegacyRuntimesMap(t *testing.T) {
	t.Run("missing runtime key defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("runtime entry not a map defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": "not-a-map"}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown key missing defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown value not a bool defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": "false"}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown explicitly false", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": false}}}
		assert.False(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown explicitly true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": true}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})
}
