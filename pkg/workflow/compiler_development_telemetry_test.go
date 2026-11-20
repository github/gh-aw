//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveDryRunTelemetryEnv(t *testing.T) {
	const source = `name: test
env: &shared
  OTEL_EXPORTER_OTLP_HEADERS: |
    Authorization=private
    X-Tenant=private
  GH_AW_OTLP_ENDPOINTS: '[{"url":"https://traces.example.com"}]'
  GITHUB_AW_OTEL_TRACE_ID: private-trace
  GITHUB_AW_OTEL_PARENT_SPAN_ID: private-span
  TRACEPARENT: private-parent
  TRACESTATE: private-state
  BAGGAGE: private-baggage
  CLAUDE_CODE_ENABLE_TELEMETRY: "1"
  START_DATE: 2026-01-01
  ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"
  KEEP: |
    first
    OTEL_LITERAL: keep this value
jobs:
  test:
    env: *shared
    container:
      image: node:24
      env: {OTEL_SERVICE_NAME: container, TRACEPARENT: private-parent, KEEP: container}
    services:
      server:
        image: node:24
        env:
          OTEL_TRACES_EXPORTER: otlp
          TRACESTATE: private-state
          KEEP: service
    steps:
      - env:
          OTEL_SERVICE_NAME: first
          BAGGAGE: private-baggage
          KEEP: first
        run: echo first
      - run: |
          echo "OTEL_SERVICE_NAME: this is script content"
          echo "env:"
          echo "  OTEL_EXPORTER_OTLP_ENDPOINT: unchanged"
        env:
          OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com
          GH_AW_OTLP_ATTRIBUTES: '{}'
          KEEP: step
      - uses: example/action@v1
        with:
          env:
            OTEL_SERVICE_NAME: action-input
      - run: echo empty
        env: {OTEL_SERVICE_NAME: removed}
`
	content, err := removeDryRunTelemetryEnv(source)
	require.NoError(t, err)
	var compiled struct {
		Env  map[string]string
		Jobs map[string]struct {
			Env       map[string]string
			Container struct{ Env map[string]string }
			Services  map[string]struct{ Env map[string]string }
			Steps     []struct {
				Env  map[string]string
				Run  string
				With struct{ Env map[string]string }
			}
		}
	}
	require.NoError(t, yaml.Unmarshal([]byte(content), &compiled))
	assert.Equal(t, map[string]string{
		"KEEP":                     "first\nOTEL_LITERAL: keep this value\n",
		"START_DATE":               "2026-01-01",
		"ANTHROPIC_CUSTOM_HEADERS": "x-aw-gw-github-repo: ${{ github.repository }}",
	}, compiled.Env)
	job := compiled.Jobs["test"]
	assert.Equal(t, compiled.Env, job.Env)
	assert.Equal(t, map[string]string{"KEEP": "container"}, job.Container.Env)
	assert.Equal(t, map[string]string{"KEEP": "service"}, job.Services["server"].Env)
	require.Len(t, job.Steps, 4)
	assert.Equal(t, map[string]string{"KEEP": "first"}, job.Steps[0].Env)
	assert.Equal(t, "echo first", job.Steps[0].Run)
	assert.Equal(t, map[string]string{"KEEP": "step"}, job.Steps[1].Env)
	assert.Equal(t, "echo \"OTEL_SERVICE_NAME: this is script content\"\necho \"env:\"\necho \"  OTEL_EXPORTER_OTLP_ENDPOINT: unchanged\"\n", job.Steps[1].Run)
	assert.Equal(t, map[string]string{"OTEL_SERVICE_NAME": "action-input"}, job.Steps[2].With.Env)
	assert.Empty(t, job.Steps[3].Env)
	assert.NotContains(t, content, "Authorization=private")
	assert.NotContains(t, content, "private-trace")
	assert.NotContains(t, content, "private-span")
	assert.NotContains(t, content, "private-parent")
	assert.NotContains(t, content, "private-state")
	assert.NotContains(t, content, "private-baggage")
	assert.Contains(t, content, `echo "OTEL_SERVICE_NAME: this is script content"`)
	assert.Contains(t, content, "env: &shared")
	assert.Contains(t, content, `ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"`)
	assert.Equal(t, source[:strings.Index(source, "env:")], content[:strings.Index(content, "env:")])
}

func TestRemoveDryRunTelemetryEnvPreservesUnrelatedYAML(t *testing.T) {
	const source = "name: test\njobs:\n  test:\n    steps:\n      - run: echo test\n        env:\n          KEEP: value\n"
	content, err := removeDryRunTelemetryEnv(source)
	require.NoError(t, err)
	assert.Equal(t, source, content)
	_, err = removeDryRunTelemetryEnv("env: [invalid\n")
	require.ErrorContains(t, err, "cannot remove dry-run telemetry")
	_, err = removeDryRunTelemetryEnv("env: not-a-mapping\n")
	require.ErrorContains(t, err, "requires an env mapping")
}

func TestRemoveDryRunTelemetryEnvOmitsEmptyMappings(t *testing.T) {
	const source = `name: test
env:
  OTEL_SERVICE_NAME: workflow
jobs:
  test:
    steps:
      - run: echo test
        env:
          GH_AW_OTLP_ATTRIBUTES: '{}'
`
	content, err := removeDryRunTelemetryEnv(source)
	require.NoError(t, err)
	assert.NotContains(t, content, "env:")
	assert.NotContains(t, content, "OTEL_SERVICE_NAME")
	assert.NotContains(t, content, "GH_AW_OTLP_ATTRIBUTES")
	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(content), &workflow))
	assert.NotContains(t, workflow, "env")
}

func TestRemoveDryRunTelemetryEnvPreservesScalarText(t *testing.T) {
	for _, value := range []string{"2026-01-01", "2026-01-01T00:00:00Z", "1e3", "0012", "false"} {
		t.Run(value, func(t *testing.T) {
			content, err := removeDryRunTelemetryEnv("env:\n  OTEL_SERVICE_NAME: removed\n  KEEP: " + value + "\n")
			require.NoError(t, err)
			var compiled struct{ Env map[string]string }
			require.NoError(t, yaml.Unmarshal([]byte(content), &compiled))
			assert.Equal(t, map[string]string{"KEEP": value}, compiled.Env)
		})
	}
}

func TestDisableDryRunTelemetryEnv(t *testing.T) {
	for _, workflowEnv := range []string{
		"",
		"env: {}\n",
		"env: null\n",
		"env: &shared\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n  DO_NOT_TRACK: '0'\n  KEEP: workflow\n",
	} {
		t.Run(workflowEnv, func(t *testing.T) {
			source := workflowEnv + `jobs:
  test:
    env:
      OTEL_SDK_DISABLED: "false"
      GH_TELEMETRY: "true"
      KEEP: job
    container:
      image: node:24
      env: {DISABLE_TELEMETRY: "0"}
    services:
      server:
        image: node:24
        env: {DISABLE_ERROR_REPORTING: "0"}
    steps:
      - run: echo test
        env:
          CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "0"
          KEEP: step
      - run: echo inherited
      - run: |
          echo "env:"
          echo "  DO_NOT_TRACK: 0"
`
			content, err := disableDryRunTelemetryEnv(source)
			require.NoError(t, err)
			var document map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(content), &document))
			assertNoDryRunTelemetryEnv(t, document)
			env := document["env"].(map[string]any)
			assertDryRunTelemetryOptOuts(t, env, true)
			if strings.Contains(workflowEnv, "KEEP") {
				assert.Equal(t, "workflow", env["KEEP"])
				assert.Contains(t, content, "env: &shared")
			}
			assert.Contains(t, content, `echo "  DO_NOT_TRACK: 0"`)
			repeated, err := disableDryRunTelemetryEnv(content)
			require.NoError(t, err)
			assert.Equal(t, content, repeated)
		})
	}
}

func assertDryRunTelemetryOptOuts(t *testing.T, env map[string]any, requireAll bool) {
	t.Helper()
	for name, expected := range map[string]string{
		"DO_NOT_TRACK":                             "1",
		"OTEL_SDK_DISABLED":                        "true",
		"GH_TELEMETRY":                             "false",
		"DISABLE_TELEMETRY":                        "1",
		"DISABLE_ERROR_REPORTING":                  "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	} {
		if actual, exists := env[name]; exists || requireAll {
			assert.Equal(t, expected, actual, name)
		}
	}
}

func TestDryRunTelemetryConfiguration(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{"otlp": map[string]any{
				"endpoint":   "https://traces.example.com",
				"github-app": map[string]any{"audience": "collector"},
			}},
		},
		ParsedFrontmatter: &FrontmatterConfig{Observability: &ObservabilityConfig{OTLP: &OTLPConfig{
			Endpoint:  "https://traces.example.com",
			GitHubApp: &OTLPGitHubAppConfig{Audience: "collector"},
		}}},
		OTLPEndpoint: "https://traces.example.com", OTLPHeaders: "Authorization=private",
		OTLPEndpoints: `[{"url":"https://traces.example.com"}]`, OTLPUsesEnterpriseDefaults: true,
		Env: "env:\n  OTEL_EXPORTER_OTLP_HEADERS: ${{ secrets.OTLP_HEADERS }}\n  GH_AW_OTLP_ATTRIBUTES: '{}'\n  TRACEPARENT: private-parent\n  KEEP: preserved\n",
		EnvSources: map[string]string{
			"OTEL_EXPORTER_OTLP_HEADERS": "(main workflow)",
			"GH_AW_OTLP_ATTRIBUTES":      "(main workflow)",
			"TRACEPARENT":                "(main workflow)",
			"KEEP":                       "(main workflow)",
		},
		EngineConfig: &EngineConfig{Env: map[string]string{
			"TRACEPARENT":                  "${{ secrets.TRACE_PARENT }}",
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"ENGINE_KEEP":                  "preserved",
		}},
	}
	compiler.SetDryRun(true)
	result, err := compiler.prepareDryRunWorkflowData(data)
	require.NoError(t, err)
	assert.Empty(t, result.OTLPEndpoint)
	assert.Empty(t, result.OTLPHeaders)
	assert.Empty(t, result.OTLPEndpoints)
	assert.False(t, result.OTLPUsesEnterpriseDefaults)
	assert.NotContains(t, result.RawFrontmatter, "observability")
	assert.Nil(t, result.ParsedFrontmatter.Observability)
	assert.False(t, isOTLPHeadersPresent(result))
	assert.False(t, isOTLPAttributesPresent(result))
	assert.False(t, isOTLPEnabled(result))
	assert.Equal(t, map[string]string{"KEEP": "(main workflow)"}, result.EnvSources)
	assert.Contains(t, result.Env, "KEEP: preserved")
	assert.Equal(t, map[string]string{"ENGINE_KEEP": "preserved"}, result.EngineConfig.Env)
	assert.Len(t, data.EngineConfig.Env, 3)
	assert.Contains(t, data.RawFrontmatter, "observability")
	assert.NotNil(t, data.ParsedFrontmatter.Observability)
	assert.Equal(t, "https://traces.example.com", data.OTLPEndpoint)
	assert.True(t, isOTLPHeadersPresent(data))
	assert.True(t, isOTLPAttributesPresent(data))
	assert.Len(t, data.EnvSources, 4)
	compiler.SetDryRun(false)
	normal, err := compiler.prepareDryRunWorkflowData(data)
	require.NoError(t, err)
	assert.Same(t, data, normal)
	var fresh WorkflowData
	compiler.injectOTLPConfig(&fresh)
	assert.Contains(t, fresh.Env, "OTEL_EXPORTER_OTLP_ENDPOINT:")
}

func TestPrepareDryRunWorkflowDataInvalidEnv(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetDryRun(true)
	data := &WorkflowData{Env: "env: [invalid\n"}
	result, err := compiler.prepareDryRunWorkflowData(data)
	require.ErrorContains(t, err, "cannot remove dry-run telemetry")
	assert.Nil(t, result)
	assert.Equal(t, "env: [invalid\n", data.Env)
}

func TestDryRunCompiledTelemetryEnv(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		engine       string
		reusable     bool
	}{
		{name: "defaults"},
		{name: "claude", engine: "claude"},
		{name: "codex", engine: "codex"},
		{name: "explicit", config: "observability:\n  otlp:\n    endpoint: https://traces.example.com\n    headers: ${{ secrets.OTLP_HEADERS }}\n"},
		{name: "imported", config: "imports: [shared.md]\n"},
		{name: "reusable", config: "observability:\n  otlp:\n    endpoint: https://traces.example.com\n    headers: ${{ secrets.OTLP_HEADERS }}\n", reusable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nobservability:\n  otlp:\n    endpoint: https://imported.example.com\n---\n"), 0600))
			source := `---
on: workflow_dispatch
strict: false
permissions:
  contents: read
engine:
  id: copilot
  env:
    OTEL_SERVICE_NAME: engine
    TRACEPARENT: engine-parent
    CLAUDE_CODE_ENABLE_TELEMETRY: "1"
    DO_NOT_TRACK: "0"
    OTEL_SDK_DISABLED: "false"
    GH_TELEMETRY: "true"
    DISABLE_TELEMETRY: "0"
    DISABLE_ERROR_REPORTING: "0"
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "0"
    ENGINE_KEEP: preserved
    START_DATE: "2026-01-01"
    ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"
env:
  DO_NOT_TRACK: "0"
  OTEL_SDK_DISABLED: "false"
  GH_TELEMETRY: "true"
  OTEL_CUSTOM: workflow
  OTEL_USER_SECRET: ${{ secrets.USER_TELEMETRY_ONLY }}
  GH_AW_OTLP_ENDPOINTS: '[{"url":"https://manual.example.com"}]'
  GH_AW_OTLP_ATTRIBUTES: '{"test": "value"}'
  GITHUB_AW_OTEL_TRACE_ID: private-trace
  GITHUB_AW_OTEL_PARENT_SPAN_ID: private-span
  TRACEPARENT: private-parent
  TRACESTATE: private-state
  BAGGAGE: private-baggage
  KEEP: preserved
  KEEP_LITERAL: OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_HEADERS GH_AW_OTLP_ATTRIBUTES
tools:
  github: false
` + tc.config + "---\nTest dry-run telemetry isolation.\n"
			if tc.engine != "" {
				source = strings.Replace(source, "id: copilot", "id: "+tc.engine, 1)
			}
			if tc.reusable {
				source = strings.Replace(source, "on: workflow_dispatch", "on: workflow_call", 1)
			}
			path := filepath.Join(dir, "telemetry.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			compiler.SetApprove(true)
			var normal string
			for _, enabled := range []bool{false, true, false} {
				compiler.SetDryRun(enabled)
				require.NoError(t, compiler.CompileWorkflow(path))
				content, err := os.ReadFile(filepath.Join(dir, "telemetry.lock.yml"))
				require.NoError(t, err)
				compiled := string(content)
				metadata, legacy, err := ExtractMetadataFromLockFile(compiled)
				require.NoError(t, err)
				require.NotNil(t, metadata)
				assert.False(t, legacy)
				assert.Equal(t, enabled, metadata.DryRun)
				if enabled {
					assert.Contains(t, strings.SplitN(compiled, "\n", 2)[0], `"dry_run":true`)
					var workflow map[string]any
					require.NoError(t, yaml.Unmarshal(content, &workflow))
					assertNoDryRunTelemetryEnv(t, workflow)
					assertDryRunTelemetryOptOuts(t, workflow["env"].(map[string]any), true)
					assert.NotContains(t, compiled, "secrets.GH_AW_DEFAULT_OTLP_")
					assert.NotContains(t, compiled, "secrets.OTLP_HEADERS")
					assert.NotContains(t, compiled, "USER_TELEMETRY_ONLY")
					assert.NotContains(t, compiled, "Check OTLP telemetry configuration")
					assert.NotContains(t, compiled, "Mask OTLP")
					assert.NotContains(t, compiled, "Mint OTLP")
					assert.NotContains(t, compiled, "Generate observability summary")
					assert.NotContains(t, compiled, `"opentelemetry":`)
					for _, name := range []string{"TRACEPARENT", "TRACESTATE", "BAGGAGE", "GITHUB_AW_OTEL_", "CLAUDE_CODE_ENABLE_TELEMETRY"} {
						assert.NotContains(t, compiled, name)
					}
					assert.NotContains(t, compiled, "#   - OTEL_")
					assert.NotContains(t, compiled, "#   - GH_AW_OTLP_")
					assert.Contains(t, compiled, "ENGINE_KEEP: preserved")
					assert.Contains(t, compiled, "KEEP: preserved")
					assert.Contains(t, compiled, "KEEP_LITERAL: OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_HEADERS GH_AW_OTLP_ATTRIBUTES")
					assert.Contains(t, compiled, `ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"`)
					assertDryRunEngineDate(t, content)
				} else {
					assert.NotContains(t, strings.SplitN(compiled, "\n", 2)[0], `"dry_run"`)
					assert.Contains(t, compiled, "OTEL_EXPORTER_OTLP_ENDPOINT:")
					assert.Contains(t, compiled, "OTEL_SERVICE_NAME: engine")
					assert.Contains(t, compiled, "TRACEPARENT: engine-parent")
					assert.Contains(t, compiled, "Mask OTLP telemetry headers")
					assert.Contains(t, compiled, "Mask OTLP custom attribute values")
					assert.Contains(t, compiled, "USER_TELEMETRY_ONLY")
					assert.Contains(t, compiled, `GH_TELEMETRY: "true"`)
					assert.Contains(t, compiled, `DO_NOT_TRACK: "0"`)
					if normal == "" {
						normal = compiled
					} else {
						assert.Equal(t, normal, compiled)
					}
				}
			}
		})
	}
}

func TestDryRunTelemetryAndDailyCreditsDiagnostic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diagnostic.md")
	const source = `---
on: workflow_dispatch
strict: false
permissions:
  contents: read
engine: copilot
max-daily-ai-credits: 25
safe-outputs:
  report-failure-as-issue: true
env:
  OTEL_SERVICE_NAME: diagnostic
---
Test dry-run compiler diagnostics.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	compiler.SetApprove(true)
	compiler.SetDryRun(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "diagnostic.lock.yml"))
	require.NoError(t, err)
	compiled := string(content)
	assert.NotContains(t, compiled, "env: {}")
	assert.NotContains(t, compiled, "OTEL_SERVICE_NAME")
	assert.NotContains(t, compiled, "needs.activation.outputs.daily_ai_credits_")
}

func TestDryRunCallWorkflowTelemetrySecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	worker := filepath.Join(dir, "worker.md")
	gateway := filepath.Join(dir, "gateway.md")
	require.NoError(t, os.WriteFile(worker, []byte(`---
on:
  workflow_call:
    secrets:
      REQUIRED_TOKEN:
        required: true
engine: copilot
permissions:
  contents: read
observability:
  otlp:
    endpoint:
      - url: ${{ secrets.GH_AW_OTEL_GRAFANA_ENDPOINT }}
        headers:
          Authorization: ${{ secrets.GH_AW_OTEL_GRAFANA_AUTHORIZATION }}
      - url: ${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}
        headers:
          Authorization: ${{ secrets.GH_AW_OTEL_SENTRY_AUTHORIZATION }}
---
Reusable worker.
`), 0600))
	require.NoError(t, os.WriteFile(gateway, []byte(`---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow: [worker]
---
Gateway.
`), 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	compiler.SetApprove(true)
	otelSecrets := []string{
		"GH_AW_OTEL_GRAFANA_AUTHORIZATION", "GH_AW_OTEL_GRAFANA_ENDPOINT",
		"GH_AW_OTEL_SENTRY_AUTHORIZATION", "GH_AW_OTEL_SENTRY_ENDPOINT",
	}
	for _, dryRun := range []bool{false, true, false} {
		compiler.SetDryRun(dryRun)
		// Compile the caller first in dry-run mode to cover stale normal worker locks.
		if dryRun {
			require.NoError(t, compiler.CompileWorkflow(gateway))
			lock, err := os.ReadFile(filepath.Join(dir, "gateway.lock.yml"))
			require.NoError(t, err)
			for _, name := range otelSecrets {
				assert.NotContains(t, string(lock), "secrets."+name)
			}
		}
		require.NoError(t, compiler.CompileWorkflow(worker))
		workerLock, err := os.ReadFile(filepath.Join(dir, "worker.lock.yml"))
		require.NoError(t, err)
		declared := extractWorkflowCallSecretsFromParsedLock(t, string(workerLock))
		assert.Contains(t, declared, "REQUIRED_TOKEN")
		require.NoError(t, compiler.CompileWorkflow(gateway))
		gatewayLock, err := os.ReadFile(filepath.Join(dir, "gateway.lock.yml"))
		require.NoError(t, err)
		var caller struct {
			Jobs map[string]struct {
				If      string
				Secrets map[string]string
				Uses    string
			}
		}
		require.NoError(t, yaml.Unmarshal(gatewayLock, &caller))
		call := caller.Jobs["call-worker"]
		for _, name := range otelSecrets {
			if dryRun {
				assert.NotContains(t, declared, name)
				assert.NotContains(t, call.Secrets, name)
			} else {
				assert.Contains(t, declared, name)
				assert.Contains(t, call.Secrets, name)
			}
		}
		for name := range call.Secrets {
			assert.Contains(t, declared, name, "caller must only pass declared callee secrets")
		}
		if dryRun {
			assert.Equal(t, "${{ !always() }}", call.If)
			assert.Empty(t, call.Secrets)
			assert.Empty(t, call.Uses, "disabled calls must not require a compatible callee secret contract")
		} else {
			assert.NotEqual(t, "${{ !always() }}", call.If)
		}
	}
}

func assertDryRunEngineDate(t *testing.T, content []byte) {
	t.Helper()
	var compiled struct {
		Jobs map[string]struct {
			Steps []struct{ Env map[string]string }
		}
	}
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	foundDate := false
	for _, step := range compiled.Jobs["agent"].Steps {
		if value, ok := step.Env["START_DATE"]; ok {
			assert.Equal(t, "2026-01-01", value)
			foundDate = true
		}
	}
	assert.True(t, foundDate, "engine environment must retain the original date text")
}

func assertNoDryRunTelemetryEnv(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "env" {
				if env, ok := child.(map[string]any); ok {
					for name := range env {
						if name == "OTEL_SDK_DISABLED" {
							assert.Equal(t, "true", env[name])
							continue
						}
						assert.False(t, strings.HasPrefix(name, "OTEL_") || strings.HasPrefix(name, "GH_AW_OTLP_"), name)
					}
					assertDryRunTelemetryOptOuts(t, env, false)
				}
			}
			assertNoDryRunTelemetryEnv(t, child)
		}
	case []any:
		for _, child := range value {
			assertNoDryRunTelemetryEnv(t, child)
		}
	}
}
