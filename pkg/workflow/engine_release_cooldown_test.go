//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type engineReleaseRoundTripper func(*http.Request) (*http.Response, error)

func (f engineReleaseRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func engineReleaseResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func newCooldownTestCompiler() *Compiler {
	c := NewCompiler(WithSkipValidation(true))
	c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		timestamp := "2020-01-01T00:00:00Z"
		if request.URL.Host == "api.github.com" {
			tag := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
			body, _ := json.Marshal(map[string]any{"tag_name": tag, "published_at": timestamp})
			return engineReleaseResponse(string(body), http.StatusOK), nil
		}
		name := strings.TrimPrefix(request.URL.Path, "/")
		versions := map[string]string{
			string(constants.DefaultClaudeCodeVersion): timestamp,
			string(constants.DefaultCodexVersion):      timestamp,
			string(constants.DefaultGeminiVersion):     timestamp,
			string(constants.DefaultPiVersion):         timestamp,
			"1.2.3":                                    timestamp,
		}
		body, _ := json.Marshal(map[string]any{"name": name, "time": versions})
		return engineReleaseResponse(string(body), http.StatusOK), nil
	})}
	return c
}

func TestEngineReleaseCooldownMetadata(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		body    string
		status  int
		young   bool
		warning bool
	}{
		{"old", `{"tag_name":"v1.2.3","published_at":"2026-01-01T00:00:00Z"}`, 200, false, false},
		{"boundary", `{"tag_name":"v1.2.3","published_at":"2026-01-07T00:00:00Z"}`, 200, false, false},
		{"young", `{"tag_name":"v1.2.3","published_at":"2026-01-08T00:00:00Z"}`, 200, true, false},
		{"wrong-tag", `{"tag_name":"v9.9.9","published_at":"2026-01-01T00:00:00Z"}`, 200, false, true},
		{"draft", `{"tag_name":"v1.2.3","published_at":"2026-01-01T00:00:00Z","draft":true}`, 200, false, true},
		{"missing-time", `{"tag_name":"v1.2.3"}`, 200, false, true},
		{"invalid-time", `{"tag_name":"v1.2.3","published_at":"yesterday"}`, 200, false, true},
		{"future-time", `{"tag_name":"v1.2.3","published_at":"2026-01-11T00:00:00Z"}`, 200, false, true},
		{"malformed-json", `{`, 200, false, true},
		{"unreachable", "", 503, false, true},
		{"not-found", "", 404, false, true},
		{"rate-limit", "", 429, false, true},
		{"oversize", strings.Repeat("x", 8*1024*1024+1), 200, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewCompiler()
			c.engineReleaseNow = func() time.Time { return now }
			calls := 0
			c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "https://api.github.com/repos/github/copilot-cli/releases/tags/v1.2.3", request.URL.String())
				assert.Empty(t, request.Header.Get("Authorization"))
				_, deadline := request.Context().Deadline()
				assert.True(t, deadline)
				return engineReleaseResponse(test.body, test.status), nil
			})}
			data := &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{ID: "copilot", Version: "v1.2.3"}}
			output := captureStderrOutput(t, func() {
				for range 2 {
					err := c.validateEngineReleaseCooldown(data, "workflow.md")
					if test.young {
						require.ErrorContains(t, err, "3-day cooldown")
					} else {
						require.NoError(t, err)
					}
				}
			})
			assert.Equal(t, 1, calls, "successes and failures are cached")
			if test.warning {
				assert.Equal(t, 1, c.GetWarningCount())
				assert.Contains(t, output, "validate the release age manually")
			} else {
				assert.Zero(t, c.GetWarningCount())
			}
		})
	}
}

func TestEngineReleaseCooldownCachePolicyAndExpiry(t *testing.T) {
	c := NewCompiler()
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	c.engineReleaseNow = func() time.Time { return now }
	calls := 0
	c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return engineReleaseResponse(`{"tag_name":"v1.2.3","published_at":"2026-01-07T00:00:00Z"}`, 200), nil
	})}
	days := 3
	data := &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{Version: "1.2.3", Cooldown: &days}}
	require.NoError(t, c.validateEngineReleaseCooldown(data, "first.md"))
	days = 7
	require.Error(t, c.validateEngineReleaseCooldown(data, "second.md"))
	assert.Equal(t, 1, calls, "cache must not remember a successful policy decision")
	days = math.MaxInt
	require.Error(t, c.validateEngineReleaseCooldown(data, "second.md"), "days must not overflow a duration")
	now = now.Add(4 * 24 * time.Hour)
	days = 7
	require.NoError(t, c.validateEngineReleaseCooldown(data, "third.md"))
	assert.Equal(t, 2, calls, "expired metadata is refreshed")
}

func TestEngineReleaseCooldownSkippedLookups(t *testing.T) {
	for _, test := range []struct {
		name    string
		config  EngineConfig
		offline bool
		editor  bool
		warn    bool
	}{
		{"zero", EngineConfig{ID: "copilot", Version: "1.2.3", Cooldown: new(int)}, false, false, false},
		{"zero-offline", EngineConfig{ID: "copilot", Version: "1.2.3", Cooldown: new(int)}, true, false, false},
		{"expression", EngineConfig{ID: "copilot", Version: "${{ inputs.version }}"}, false, false, false},
		{"tag", EngineConfig{ID: "codex", Version: "latest"}, false, false, false},
		{"range", EngineConfig{ID: "codex", Version: "^1.2.3"}, false, false, false},
		{"command", EngineConfig{ID: "copilot", Version: "1.2.3", Command: "custom"}, false, false, false},
		{"offline", EngineConfig{ID: "copilot", Version: "1.2.3"}, true, false, true},
		{"editor", EngineConfig{ID: "copilot", Version: "1.2.3"}, false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewCompiler()
			c.verifyAuthorization = test.offline
			if test.editor {
				c.contentOverride = "content"
			}
			c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("unexpected metadata lookup")
				return nil, errors.New("unexpected lookup")
			})}
			output := captureStderrOutput(t, func() {
				require.NoError(t, c.validateEngineReleaseCooldown(&WorkflowData{AI: test.config.ID, EngineConfig: &test.config}, "workflow.md"))
			})
			if test.warn {
				assert.Contains(t, output, "validate the release age manually")
			} else {
				assert.Empty(t, output)
			}
			assert.Nil(t, c.engineReleaseCache, "offline and zero must not populate or poison metadata caches")
		})
	}
}

func TestEngineReleaseCooldownNPMAndDefaults(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex", "pi", "gemini", "custom"} {
		t.Run(id, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{AI: id, EngineConfig: &EngineConfig{ID: id}}
			if id == "custom" {
				data.engineDefinition = &EngineDefinition{ID: id, Behaviors: &EngineBehaviorDefinition{Installation: &EngineInstallationDefinition{
					PackageManager: "npm", PackageName: "@example/custom", Version: "1.2.3",
				}}}
			}
			calls := 0
			c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				version := getVersionForSetup(data, c.engineRegistry)
				if id == "custom" {
					version = "1.2.3"
				}
				require.True(t, literalEngineReleaseVersion(version), "defaults are literal and checked")
				var body []byte
				if id == "copilot" {
					body, _ = json.Marshal(map[string]any{"tag_name": "v" + version, "published_at": "2020-01-01T00:00:00Z"})
				} else {
					assert.Equal(t, "registry.npmjs.org", request.URL.Host)
					body, _ = json.Marshal(map[string]any{"name": strings.TrimPrefix(request.URL.Path, "/"), "time": map[string]string{version: "2020-01-01T00:00:00.123Z"}})
				}
				return engineReleaseResponse(string(body), 200), nil
			})}
			require.NoError(t, c.validateEngineReleaseCooldown(data, "workflow.md"))
			assert.Equal(t, 1, calls)
			assert.Zero(t, c.GetWarningCount())
		})
	}
	for _, body := range []string{
		`{"name":"wrong","time":{"1.2.3":"2020-01-01T00:00:00Z"}}`,
		`{"name":"@openai/codex","time":{}}`,
		`{"name":"@openai/codex","time":{"1.2.3":"bad"}}`,
	} {
		c := NewCompiler()
		c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(*http.Request) (*http.Response, error) {
			return engineReleaseResponse(body, 200), nil
		})}
		output := captureStderrOutput(t, func() {
			require.NoError(t, c.validateEngineReleaseCooldown(&WorkflowData{AI: "codex", EngineConfig: &EngineConfig{ID: "codex", Version: "1.2.3"}}, "workflow.md"))
		})
		assert.Contains(t, output, "validate the release age manually")
	}
}

func TestEngineReleaseCooldownDetection(t *testing.T) {
	c := newCooldownTestCompiler()
	days := 7
	data := &WorkflowData{AI: "pi", EngineConfig: &EngineConfig{ID: "pi", Cooldown: &days},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}}}
	c.engineReleaseNow = func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.github.com" {
			body, _ := json.Marshal(map[string]any{"tag_name": "v" + string(constants.DefaultCopilotVersion), "published_at": "2026-01-08T00:00:00Z"})
			return engineReleaseResponse(string(body), 200), nil
		}
		body, _ := json.Marshal(map[string]any{"name": "@earendil-works/pi-coding-agent", "time": map[string]string{string(constants.DefaultPiVersion): "2020-01-01T00:00:00Z"}})
		return engineReleaseResponse(string(body), 200), nil
	})}
	require.ErrorContains(t, c.validateEngineReleaseCooldowns(data, "workflow.md"), "safe-outputs.threat-detection")
	zero := 0
	data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{ID: "copilot", Cooldown: &zero}
	require.NoError(t, c.validateEngineReleaseCooldowns(data, "workflow.md"))
	data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{ID: "copilot", Version: "${{ inputs.version }}"}
	require.NoError(t, c.validateEngineReleaseCooldowns(data, "workflow.md"))
}

func TestEngineReleaseCooldownNetworkSafety(t *testing.T) {
	t.Setenv("GH_HOST", "enterprise.example")
	t.Setenv("GH_TOKEN", "must-not-send")
	t.Setenv("NPM_CONFIG_REGISTRY", "https://enterprise.example/npm")
	c := NewCompiler()
	calls := 0
	c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, "api.github.com", request.URL.Host)
		assert.Empty(t, request.Header.Get("Authorization"))
		response := engineReleaseResponse("", 302)
		response.Header.Set("Location", "https://enterprise.example/private")
		return response, nil
	})}
	output := captureStderrOutput(t, func() {
		require.NoError(t, c.validateEngineReleaseCooldown(&WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{Version: "1.2.3"}}, "workflow.md"))
	})
	assert.Equal(t, 1, calls, "redirects must not reach arbitrary hosts")
	assert.Contains(t, output, "validate the release age manually")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = NewCompiler()
	c.ctx = ctx
	c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	require.ErrorIs(t, c.validateEngineReleaseCooldown(&WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{Version: "1.2.3"}}, "workflow.md"), context.Canceled)
}

func TestCopilotLiteralCooldownRuntimePolicy(t *testing.T) {
	for _, version := range []string{"", "v1.2.3", "1.2.3", "latest", "${{ inputs.version }}"} {
		steps := stepsToString(generateCopilotInstallerSteps(version, "Install Copilot", false, "", "", "", testEngineChecksumsPin, 7))
		if literalEngineReleaseVersion(version) {
			assert.Contains(t, steps, "--cooldown-days 0")
		} else {
			assert.Contains(t, steps, "--cooldown-days 7")
		}
		assert.Contains(t, steps, "--checksums-sha256 "+testEngineChecksumsPin)
	}
}

func TestEngineReleaseCooldownResolvedTargets(t *testing.T) {
	c := NewCompiler()
	for _, id := range []string{"copilot-experimental", "codex-experimental"} {
		data := &WorkflowData{AI: id, EngineConfig: &EngineConfig{ID: id}}
		target, supported := c.engineReleaseTarget(data)
		require.True(t, supported)
		engine, err := c.engineRegistry.GetEngineByPrefix(id)
		require.NoError(t, err)
		assert.Equal(t, getInstallationVersion(data, engine, c.engineRegistry), target.version)
	}
	def := &EngineDefinition{ID: "crush", Version: "0.98.1", Behaviors: &EngineBehaviorDefinition{
		Installation: &EngineInstallationDefinition{PackageManager: "npm", PackageName: "@charmland/crush"},
	}}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	require.NoError(t, c.engineRegistry.Register(engine))
	target, supported := c.engineReleaseTarget(&WorkflowData{AI: "crush", EngineConfig: &EngineConfig{ID: "crush", Version: def.Version}, engineDefinition: def})
	require.True(t, supported, "built-in behavior top-level version must not be overwritten by an empty installation.version")
	assert.Equal(t, "0.98.1", target.version)
	assert.Equal(t, "@charmland/crush", target.packageName)
}

func TestEngineReleaseCooldownExternalCommand(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(strconv.FormatBool(external), func(t *testing.T) {
			c := newCooldownTestCompiler()
			c.engineReleaseNow = func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
			calls := 0
			c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				body, _ := json.Marshal(map[string]any{"tag_name": "v" + string(constants.DefaultCopilotVersion), "published_at": "2026-01-09T00:00:00Z"})
				return engineReleaseResponse(string(body), 200), nil
			})}
			data := &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{ID: "copilot", Command: "custom"},
				Features:    map[string]any{string(constants.GHAWDetectionFeatureFlag): external},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}}}
			err := c.validateEngineReleaseCooldowns(data, "workflow.md")
			if external {
				require.ErrorContains(t, err, "safe-outputs.threat-detection")
				assert.Equal(t, 1, calls, "external detection installs the standard CLI despite main command")
			} else {
				require.NoError(t, err)
				assert.Zero(t, calls, "inline detection retains the custom command")
			}
			assert.Equal(t, "custom", data.EngineConfig.Command, "validation must not mutate the main config")
		})
	}
}

func TestCompileEngineReleaseCooldownLiteral(t *testing.T) {
	for _, id := range []string{"copilot", "codex"} {
		t.Run(id, func(t *testing.T) {
			c := newCooldownTestCompiler()
			c.engineReleaseNow = func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
			c.engineReleaseClient = &http.Client{Transport: engineReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
				body := `{"tag_name":"v1.2.3","published_at":"2026-01-09T00:00:00Z"}`
				if id == "codex" {
					body = `{"name":"@openai/codex","time":{"1.2.3":"2026-01-09T00:00:00Z"}}`
				}
				return engineReleaseResponse(body, 200), nil
			})}
			pin := ""
			if id == "copilot" {
				pin = "  checksums-sha256: " + testEngineChecksumsPin + "\n"
			}
			path := filepath.Join(t.TempDir(), "literal.md")
			content := fmt.Sprintf("---\non: workflow_dispatch\nnetwork: defaults\nengine:\n  id: %s\n  version: 1.2.3\n%s---\nInspect the repository.\n", id, pin)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
			require.ErrorContains(t, c.CompileWorkflow(path), "3-day cooldown", "schema skipping does not bypass cooldown validation")
			_, err := os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			assert.True(t, os.IsNotExist(err), "young releases must not emit a lock")
		})
	}
}

func TestEngineReleaseCooldownChecksumWithoutVersion(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{ID: "copilot", ChecksumsSHA256: testEngineChecksumsPin}}
	require.ErrorContains(t, c.validateReleaseIntegrityPins(data), "explicit semantic release version")
	steps := stepsToString(generateCopilotInstallerSteps("", "Install", false, "", "", "", testEngineChecksumsPin, 7))
	assert.Contains(t, steps, "--cooldown-days 7", "an unresolved selection must not disable runtime enforcement")
}
