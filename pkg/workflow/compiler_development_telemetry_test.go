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
  KEEP: |
    first
    OTEL_LITERAL: keep this value
jobs:
  test:
    env: *shared
    container:
      image: node:24
      env: {OTEL_SERVICE_NAME: container, KEEP: container}
    services:
      server:
        image: node:24
        env:
          OTEL_TRACES_EXPORTER: otlp
          KEEP: service
    steps:
      - env:
          OTEL_SERVICE_NAME: first
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
	assert.Equal(t, map[string]string{"KEEP": "first\nOTEL_LITERAL: keep this value\n"}, compiled.Env)
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
	assert.Contains(t, content, `echo "OTEL_SERVICE_NAME: this is script content"`)
	assert.Contains(t, content, "env: &shared")
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
	}
	compiler.SetDryRun(true)
	result := compiler.dryRunWorkflowData(data)
	assert.Empty(t, result.OTLPEndpoint)
	assert.Empty(t, result.OTLPHeaders)
	assert.Empty(t, result.OTLPEndpoints)
	assert.False(t, result.OTLPUsesEnterpriseDefaults)
	assert.NotContains(t, result.RawFrontmatter, "observability")
	assert.Nil(t, result.ParsedFrontmatter.Observability)
	assert.Contains(t, data.RawFrontmatter, "observability")
	assert.NotNil(t, data.ParsedFrontmatter.Observability)
	assert.Equal(t, "https://traces.example.com", data.OTLPEndpoint)
	compiler.SetDryRun(false)
	assert.Same(t, data, compiler.dryRunWorkflowData(data))
	var fresh WorkflowData
	compiler.injectOTLPConfig(&fresh)
	assert.Contains(t, fresh.Env, "OTEL_EXPORTER_OTLP_ENDPOINT:")
}

func TestDryRunCompiledTelemetryEnv(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		reusable     bool
	}{
		{name: "defaults"},
		{name: "explicit", config: "observability:\n  otlp:\n    endpoint: https://traces.example.com\n    headers: ${{ secrets.OTLP_HEADERS }}\n"},
		{name: "imported", config: "imports: [shared.md]\n"},
		{name: "github-app", config: "observability:\n  otlp:\n    endpoint: https://traces.example.com\n    github-app:\n      audience: collector\n"},
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
  id-token: write
engine:
  id: copilot
  env:
    OTEL_SERVICE_NAME: engine
    ENGINE_KEEP: preserved
env:
  OTEL_CUSTOM: workflow
  GH_AW_OTLP_ENDPOINTS: '[{"url":"https://manual.example.com"}]'
  KEEP: preserved
tools:
  github: false
pre-steps:
  - run: echo pre
    env: {OTEL_SERVICE_NAME: pre, KEEP: pre}
post-steps:
  - run: echo post
    env: {OTEL_SERVICE_NAME: post, KEEP: post}
jobs:
  custom:
    runs-on: ubuntu-latest
    env: {OTEL_SERVICE_NAME: job, KEEP: job}
    steps:
      - run: echo custom
        env: {OTEL_SERVICE_NAME: step, KEEP: step}
` + tc.config + "---\nTest dry-run telemetry isolation.\n"
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
				if enabled {
					var workflow map[string]any
					require.NoError(t, yaml.Unmarshal(content, &workflow))
					assertNoDryRunTelemetryEnv(t, workflow)
					assert.NotContains(t, compiled, "secrets.GH_AW_DEFAULT_OTLP_")
					assert.NotContains(t, compiled, "secrets.OTLP_HEADERS")
					assert.NotContains(t, compiled, "Check OTLP telemetry configuration")
					assert.NotContains(t, compiled, "Mint OTLP")
					assert.NotContains(t, compiled, `"opentelemetry":`)
					assert.Contains(t, compiled, "ENGINE_KEEP: preserved")
					assert.Contains(t, compiled, "KEEP: preserved")
				} else {
					assert.Contains(t, compiled, "OTEL_EXPORTER_OTLP_ENDPOINT:")
					assert.Contains(t, compiled, "OTEL_SERVICE_NAME: engine")
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

func assertNoDryRunTelemetryEnv(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "env" {
				if env, ok := child.(map[string]any); ok {
					for name := range env {
						assert.False(t, strings.HasPrefix(name, "OTEL_") || strings.HasPrefix(name, "GH_AW_OTLP_"), name)
					}
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
