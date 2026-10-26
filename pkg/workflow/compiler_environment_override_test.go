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
		if name == "admission" {
			assert.NotContains(t, job, "environment")
			continue
		}
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
	assert.Equal(t, map[string]any{"name": "production", "url": "https://production.example.com"}, compiled.Jobs["update_cache_memory"]["environment"])
}

func TestPersistenceJobsInheritWorkflowEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		environment string
		want        string
	}{
		{name: "absent"},
		{name: "literal", environment: "environment: production", want: "environment: production"},
		{name: "expression", environment: "environment: ${{ vars.RUNTIME_ENVIRONMENT }}", want: "environment: ${{ vars.RUNTIME_ENVIRONMENT }}"},
		{name: "object", environment: "environment:\n  name: production\n  url: https://example.com", want: "environment:\n      name: production\n      url: https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			data := &WorkflowData{
				Environment:        tt.environment,
				ManualApproval:     "approval-only",
				SafeOutputs:        &SafeOutputsConfig{Environment: "environment: writes-only"},
				CacheMemoryConfig:  &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}},
				DriveMemoryConfig:  &DriveMemoryConfig{Drives: defaultDriveMemoryEntries()},
				RepoMemoryConfig:   &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "default", BranchName: "memory"}}},
				LedgerConfig:       &LedgerToolConfig{Ledgers: []LedgerConfig{{Name: "state", Type: "map"}}},
				Experiments:        map[string][]string{"test": {"a", "b"}},
				ExperimentsStorage: ExperimentsStorageRepo,
				Evals:              &EvalsConfig{Questions: []EvalDefinition{{ID: "test", Question: "Did it work?"}}},
			}
			builders := map[string]func() (*Job, error){
				"cache":       func() (*Job, error) { return compiler.buildUpdateCacheMemoryJob(data, true) },
				"drive":       func() (*Job, error) { return compiler.buildUpdateDriveMemoryJob(data, true) },
				"repo":        func() (*Job, error) { return compiler.buildPushRepoMemoryJob(data, true) },
				"ledger":      func() (*Job, error) { return compiler.buildPushLedgerChangesJob(data, true) },
				"experiments": func() (*Job, error) { return compiler.buildPushExperimentsStateJob(data) },
				"evals":       func() (*Job, error) { return compiler.buildPushEvalsStateJob(data) },
			}
			for name, build := range builders {
				t.Run(name, func(t *testing.T) {
					job, err := build()
					require.NoError(t, err)
					require.NotNil(t, job)
					assert.Equal(t, tt.want, job.Environment)
				})
			}
		})
	}
}
