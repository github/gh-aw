package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileRejectsInvalidRunnerInstallation(t *testing.T) {
	for _, config := range []string{
		"installation: unknown",
		"installation: true",
		"installation: '${{ inputs.installation }}'",
		"topology: arc-dind\n  installation: privileged",
	} {
		t.Run(config, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runner.md")
			require.NoError(t, os.WriteFile(path, []byte("---\non: workflow_dispatch\nrunner:\n  "+config+"\n---\nTest.\n"), 0600))
			require.Error(t, NewCompiler().CompileWorkflow(path))
		})
	}
}

func TestExtractRunnerConfig(t *testing.T) {
	t.Run("nil when runner not present", func(t *testing.T) {
		fm := map[string]any{"name": "test"}
		assert.Nil(t, extractRunnerConfig(fm))
	})

	t.Run("nil when runner is not an object", func(t *testing.T) {
		fm := map[string]any{"runner": "arc-dind"}
		assert.Nil(t, extractRunnerConfig(fm))
	})

	t.Run("nil when topology is empty", func(t *testing.T) {
		fm := map[string]any{"runner": map[string]any{}}
		assert.Nil(t, extractRunnerConfig(fm))
	})

	t.Run("extracts arc-dind topology", func(t *testing.T) {
		fm := map[string]any{
			"runner": map[string]any{
				"topology": "arc-dind",
			},
		}
		cfg := extractRunnerConfig(fm)
		require.NotNil(t, cfg)
		assert.Equal(t, RunnerTopologyArcDind, cfg.Topology)
	})
	t.Run("extracts installation without topology", func(t *testing.T) {
		cfg := extractRunnerConfig(map[string]any{
			"runner": map[string]any{"installation": "rootless"},
		})
		require.NotNil(t, cfg)
		assert.Equal(t, "rootless", cfg.Installation)
		assert.Empty(t, cfg.Topology)
	})
}

func TestValidateRunnerConfig(t *testing.T) {
	t.Run("nil config is valid", func(t *testing.T) {
		assert.NoError(t, validateRunnerConfig(nil))
	})

	t.Run("arc-dind is valid", func(t *testing.T) {
		assert.NoError(t, validateRunnerConfig(&RunnerConfig{Topology: RunnerTopologyArcDind}))
	})

	t.Run("empty topology is valid", func(t *testing.T) {
		assert.NoError(t, validateRunnerConfig(&RunnerConfig{}))
	})

	t.Run("unsupported topology returns error", func(t *testing.T) {
		err := validateRunnerConfig(&RunnerConfig{Topology: "unknown"})
		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported runner.topology")
		require.ErrorContains(t, err, "unknown")
	})
	t.Run("unsupported installation returns error", func(t *testing.T) {
		require.ErrorContains(t, validateRunnerConfig(&RunnerConfig{Installation: "unknown"}), "unsupported runner.installation")
	})
	t.Run("privileged arc-dind is rejected", func(t *testing.T) {
		require.ErrorContains(t, validateRunnerConfig(&RunnerConfig{
			Topology: RunnerTopologyArcDind, Installation: "privileged",
		}), "not supported with runner.topology: arc-dind")
	})
}

func TestUseRootlessCLIInstallation(t *testing.T) {
	assert.False(t, useRootlessCLIInstallation(nil))
	for _, tc := range []struct {
		name         string
		config       *RunnerConfig
		wantRootless bool
	}{
		{"default privileged", nil, false},
		{"native rootless", &RunnerConfig{Installation: "rootless"}, true},
		{"native privileged", &RunnerConfig{Installation: "privileged"}, false},
		{"arc default", &RunnerConfig{Topology: RunnerTopologyArcDind}, true},
		{"arc explicit rootless", &RunnerConfig{Topology: RunnerTopologyArcDind, Installation: "rootless"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, validateRunnerConfig(tc.config))
			assert.Equal(t, tc.wantRootless, useRootlessCLIInstallation(&WorkflowData{RunnerConfig: tc.config}))
		})
	}
}

func TestIsArcDindTopology(t *testing.T) {
	t.Run("false when nil workflow data", func(t *testing.T) {
		assert.False(t, isArcDindTopology(nil))
	})

	t.Run("false when nil runner config", func(t *testing.T) {
		assert.False(t, isArcDindTopology(&WorkflowData{}))
	})

	t.Run("false when topology is empty", func(t *testing.T) {
		assert.False(t, isArcDindTopology(&WorkflowData{
			RunnerConfig: &RunnerConfig{},
		}))
	})

	t.Run("true when topology is arc-dind", func(t *testing.T) {
		assert.True(t, isArcDindTopology(&WorkflowData{
			RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
		}))
	})
}

func TestGetRunnerTopology(t *testing.T) {
	t.Run("empty when nil workflow data", func(t *testing.T) {
		assert.Empty(t, getRunnerTopology(nil))
	})

	t.Run("returns topology when set", func(t *testing.T) {
		wd := &WorkflowData{
			RunnerConfig: &RunnerConfig{Topology: "arc-dind"},
		}
		assert.Equal(t, RunnerTopologyArcDind, getRunnerTopology(wd))
	})
}
