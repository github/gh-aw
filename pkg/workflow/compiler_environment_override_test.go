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

func TestValidateEnvironmentOverride(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "debug", "true", "123", "test: #1", strings.Repeat("a", 255), strings.Repeat("é", 255)} {
		require.NoError(t, ValidateEnvironmentOverride(name))
	}
	for _, name := range []string{" ", "\n", "debug\npermissions: write-all", "debug\t", "${{ vars.ENVIRONMENT }}", strings.Repeat("a", 256), "\xff"} {
		require.Error(t, ValidateEnvironmentOverride(name), "name %q must be rejected", name)
	}
}

func TestEnvironmentOverrideSerializesLiteralNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"debug", "true", "123", "test: #1", `debug "quoted" \ name`, "- debug", "é"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler := NewCompiler()
			compiler.SetEnvironmentOverride(name)
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "test", RunsOn: "runs-on: ubuntu-latest"}))
			require.NoError(t, compiler.applyEnvironmentOverride())

			var compiled struct {
				Jobs map[string]struct {
					Environment string `yaml:"environment"`
				} `yaml:"jobs"`
			}
			var content strings.Builder
			compiler.jobManager.WriteJobsYAML(&content)
			require.NoError(t, yaml.Unmarshal([]byte(content.String()), &compiled))
			require.Len(t, compiled.Jobs, 1)
			assert.Equal(t, name, compiled.Jobs["test"].Environment)
		})
	}
}

func TestEnvironmentOverrideReusableCallerFailsWithoutPartialChanges(t *testing.T) {
	t.Parallel()
	compiler := NewCompiler()
	compiler.SetEnvironmentOverride("debug")
	normal := &Job{Name: "normal", Environment: "environment: production"}
	require.NoError(t, compiler.jobManager.AddJob(normal))
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "caller", Uses: "./.github/workflows/reusable.yml"}))
	err := compiler.applyEnvironmentOverride()
	require.ErrorContains(t, err, "--environment cannot override job 'caller'")
	require.ErrorContains(t, err, "reusable-workflow callers")
	assert.Equal(t, "environment: production", normal.Environment)
}

func TestEnvironmentOverrideAllCompiledJobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	const source = `---
on:
  workflow_dispatch:
  roles: [admin]
  manual-approval: production-approval
strict: false
engine: copilot
environment:
  name: production
  url: https://production.example.com
tools:
  cache-memory: true
safe-outputs:
  environment: production-writes
  add-comment:
jobs:
  custom:
    runs-on: ubuntu-latest
    environment:
      name: production-custom
      url: https://custom.example.com
    steps:
      - run: echo testing
---
Test environment overrides.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	compiler.SetEnvironmentOverride("debug")
	require.NoError(t, compiler.CompileWorkflow(path))

	content, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	for _, name := range []string{"pre_activation", "activation", "agent", "detection", "safe_outputs", "conclusion", "custom", "update_cache_memory"} {
		require.Contains(t, compiled.Jobs, name)
	}
	for name, job := range compiled.Jobs {
		assert.Equal(t, "debug", job["environment"], "job %s must use the override", name)
	}

	compiler.SetEnvironmentOverride("")
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err = os.ReadFile(filepath.Join(dir, "test.lock.yml"))
	require.NoError(t, err)
	compiled.Jobs = nil
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	assert.Equal(t, "production-approval", compiled.Jobs["activation"]["environment"])
	assert.Equal(t, map[string]any{"name": "production", "url": "https://production.example.com"}, compiled.Jobs["agent"]["environment"])
	assert.Equal(t, "production-writes", compiled.Jobs["safe_outputs"]["environment"])
	assert.Equal(t, map[string]any{"name": "production-custom", "url": "https://custom.example.com"}, compiled.Jobs["custom"]["environment"])
	assert.Equal(t, "production-writes", compiled.Jobs["conclusion"]["environment"])
	assert.Equal(t, "production-writes", compiled.Jobs["pre_activation"]["environment"])
}
