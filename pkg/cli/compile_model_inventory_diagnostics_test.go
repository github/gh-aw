//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentModelInventoryRefreshWarnings(t *testing.T) {
	original := compileBuildModelsReport
	t.Cleanup(func() { compileBuildModelsReport = original })
	compileBuildModelsReport = func(_ context.Context, opts modelsReportOptions) modelsReport {
		assert.True(t, opts.refreshObserved)
		return modelsReport{
			Observed: []observedModelRow{{Model: "example-model"}},
			Warnings: []string{"observed-model refresh failed: access denied"},
		}
	}
	config := CompileConfig{DryRun: true}
	PrepareCompileModelValidation(context.Background(), &config)
	require.NotNil(t, config.activeModels)
	assert.True(t, config.activeModels.contains("example-model", nil))
	require.Len(t, config.modelValidationWarnings, 1)
	stats := &CompilationStats{Succeeded: 1}
	results := []ValidationResult{{Workflow: "a.md", Valid: true}}
	err := enforceDevelopmentDiagnostics(config, workflow.NewCompiler(), stats, &results)
	require.ErrorContains(t, err, "observed-model refresh failed")
	assert.True(t, results[0].Valid)
	require.Len(t, results, 2)
	assert.Equal(t, "models", results[1].Workflow)
	assert.Equal(t, "batch", results[1].Scope)
	assert.False(t, results[1].Valid)
	assert.Equal(t, "model_inventory_warning", results[1].Errors[0].Type)
}

func TestModelInventoryWarningsOrdinaryCompilation(t *testing.T) {
	config := CompileConfig{Models: true, modelValidationWarnings: []string{"observed-model refresh failed: access denied"}}
	stats := &CompilationStats{Succeeded: 1}
	results := []ValidationResult{{Workflow: "a.md", Valid: true}}
	require.NoError(t, enforceDevelopmentDiagnostics(config, workflow.NewCompiler(), stats, &results))
	var err error
	output := captureStderrForGuardPolicyReportTest(func() {
		err = outputResults(stats, &results, config)
	})
	require.NoError(t, err)
	assert.Contains(t, output, "observed-model refresh failed")
	require.Len(t, results, 2)
	assert.True(t, results[0].Valid)
	assert.True(t, results[1].Valid)
	assert.Equal(t, "batch", results[1].Scope)
	assert.Equal(t, "models", results[1].Workflow)
	assert.Equal(t, "model_inventory_warning", results[1].Warnings[0].Type)
	assert.Nil(t, results[1].DryRun)
	assert.Equal(t, 1, stats.Warnings)
	assert.Zero(t, stats.Errors)
}
