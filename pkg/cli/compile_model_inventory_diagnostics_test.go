//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/scanfindings"
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

func TestMCPDevelopmentModelInventoryUsesCachedArtifacts(t *testing.T) {
	for _, test := range []struct {
		name     string
		observed []observedModelRow
	}{
		{name: "cached inventory", observed: []observedModelRow{{Model: "cached-model"}}},
		{name: "empty inventory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := compileBuildModelsReport
			t.Cleanup(func() { compileBuildModelsReport = original })
			compileBuildModelsReport = func(_ context.Context, opts modelsReportOptions) modelsReport {
				assert.False(t, opts.refreshObserved)
				return modelsReport{Observed: test.observed}
			}

			config := CompileConfig{DryRun: true, MCPProgressFile: "results.json"}
			PrepareCompileModelValidation(context.Background(), &config)
			assert.Empty(t, config.modelValidationWarnings)
			if len(test.observed) == 0 {
				assert.Nil(t, config.activeModels)
				messages := configuredModelValidationMessages(
					&workflow.WorkflowData{ModelPolicyAllowed: []string{"configured-model"}},
					config.activeModels,
					true,
				)
				require.Len(t, messages, 1)
				assert.Contains(t, messages[0], "inventory is unavailable")
			} else {
				require.NotNil(t, config.activeModels)
				assert.True(t, config.activeModels.contains("cached-model", nil))
			}

			result := ValidationResult{Workflow: "a.md", Valid: true}
			appendMCPCachedModelInventoryWarning(config, &result)
			require.Len(t, result.Warnings, 1)
			assert.Equal(t, "cached_model_inventory", result.Warnings[0].Type)
			assert.Equal(t, mcpCachedModelInventoryWarning, result.Warnings[0].Message)
			assert.Equal(t, scanfindings.SeverityInfo, result.Warnings[0].Severity)
			results := []ValidationResult{result}
			require.NoError(t, enforceDevelopmentDiagnostics(config, workflow.NewCompiler(), &CompilationStats{}, &results))
			assert.True(t, results[0].Valid)
		})
	}
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
