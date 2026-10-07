//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDevelopmentCompileMode(t *testing.T) {
	t.Parallel()
	original := CompileConfig{EnvironmentOverride: "debug"}
	assert.Equal(t, original, applyDevelopmentCompileMode(original))
	config := applyDevelopmentCompileMode(CompileConfig{DryRun: true, EnvironmentOverride: "debug"})
	assert.Equal(t, "debug", config.EnvironmentOverride)
	for _, enabled := range []bool{
		config.Strict, config.Staged, config.Validate, config.Shellcheck, config.Models,
	} {
		assert.True(t, enabled)
	}
	assert.False(t, config.RequireSelfHostedRunners)
	assert.False(t, config.Approve)
	assert.False(t, config.AllowActionRefs)
	assert.Empty(t, config.ActionMode, "--dry-run must not change action reference mode")
	require.NoError(t, validateCompileConfig(config), "a test environment is not mandatory")
	require.NoError(t, validateCompileConfig(applyDevelopmentCompileMode(CompileConfig{DryRun: true})))
}

func TestDevelopmentCompileModeKeepsDockerChecksOptional(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		config := applyDevelopmentCompileMode(CompileConfig{
			DryRun: true, ValidateImages: enabled, Actionlint: enabled, Zizmor: enabled,
			Poutine: enabled, RunnerGuard: enabled, Syft: enabled, Grype: enabled,
			Grant: enabled, Yamllint: enabled,
		})
		for _, actual := range []bool{
			config.ValidateImages, config.Actionlint, config.Zizmor, config.Poutine,
			config.RunnerGuard, config.Syft, config.Grype, config.Grant, config.Yamllint,
		} {
			assert.Equal(t, enabled, actual, "dry-run must preserve opt-in Docker checks")
		}
		for _, name := range []string{
			"validate-images", "actionlint", "zizmor", "poutine", "runner-guard",
			"syft", "grype", "grant", "yamllint",
		} {
			config.ExplicitBoolFlags = map[string]bool{name: enabled}
			require.NoError(t, validateCompileConfig(config), "Docker checks may be explicitly enabled or disabled")
		}
		compiler := createAndConfigureCompiler(config)
		assert.Equal(t, enabled, reflect.ValueOf(compiler).Elem().FieldByName("requireDocker").Bool())
	}
}

func TestDevelopmentCompileFailsWithoutShellcheckOrDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GH_HOST", "github.com")

	_, err := CompileWorkflows(context.Background(), CompileConfig{
		DryRun: true, activeModels: &activeModelInventory{},
	})
	require.ErrorContains(t, err, "shellcheck not available")
	assert.Contains(t, err.Error(), "install shellcheck")
}

func TestDevelopmentCompileModeRejectsBypasses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		flag   string
		config CompileConfig
	}{
		{"--no-emit", CompileConfig{NoEmit: true}},
		{"--watch", CompileConfig{Watch: true}},
		{"--approve", CompileConfig{Approve: true}},
		{"--allow-action-refs", CompileConfig{AllowActionRefs: true}},
	} {
		t.Run(test.flag, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateDevelopmentCompileMode(test.config))
			test.config.DryRun = true
			require.ErrorContains(t, validateCompileConfig(test.config), test.flag)
			_, err := CompileWorkflows(context.Background(), test.config)
			require.ErrorContains(t, err, strings.TrimPrefix(test.flag, "--"))
		})
	}
	require.ErrorContains(t, validateCompileConfig(CompileConfig{EnvironmentOverride: "debug\nunsafe"}), "--environment")
}

func TestDevelopmentCompilerFlags(t *testing.T) {
	t.Parallel()
	compiler := createAndConfigureCompiler(applyDevelopmentCompileMode(CompileConfig{DryRun: true, EnvironmentOverride: "debug"}))
	state := reflect.ValueOf(compiler).Elem()
	assert.True(t, state.FieldByName("strictMode").Bool())
	assert.True(t, state.FieldByName("forceStaged").Bool())
	assert.True(t, state.FieldByName("dryRun").Bool())
	assert.False(t, state.FieldByName("requireDocker").Bool())
	assert.False(t, state.FieldByName("skipValidation").Bool())
	assert.Equal(t, "debug", state.FieldByName("environmentOverride").String())
	regular := createAndConfigureCompiler(CompileConfig{})
	assert.False(t, reflect.ValueOf(regular).Elem().FieldByName("dryRun").Bool(), "regular compilation must not disable persistence jobs")
	assert.False(t, reflect.ValueOf(regular).Elem().FieldByName("configuredModelValidator").IsNil(), "regular compilation must check model pricing")
}

func TestDevelopmentModelChecksRequireInventoryWhenConfigured(t *testing.T) {
	t.Parallel()
	require.Empty(t, configuredModelValidationMessages(nil, nil, true))
	require.Empty(t, configuredModelValidationMessages(&workflow.WorkflowData{}, nil, true))
	for _, data := range []*workflow.WorkflowData{
		{ModelPolicyAllowed: []string{"example-model"}},
		{ModelPolicyBlocked: []string{"example-model"}},
		{RawFrontmatter: map[string]any{"engine": map[string]any{"models": map[string]any{"default": "example-model"}}}},
		{RawFrontmatter: map[string]any{"engine": map[string]any{"models": map[string]any{"supported": []any{"example-model"}}}}},
	} {
		require.Empty(t, configuredModelValidationMessages(data, nil, false), "ordinary compilation remains best-effort")
		messages := configuredModelValidationMessages(data, nil, true)
		require.Len(t, messages, 1)
		assert.Contains(t, messages[0], "inventory is unavailable")
		inventory := &activeModelInventory{models: []string{"example-model"}}
		assert.Empty(t, configuredModelValidationMessages(data, inventory, true))
	}
}

func TestConfiguredModelPricingWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    *workflow.WorkflowData
		warning bool
	}{
		{
			name: "unpriced model",
			data: &workflow.WorkflowData{
				Model:        "gpt-future-model",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
			},
			warning: true,
		},
		{
			name: "reported model has pricing",
			data: &workflow.WorkflowData{
				Model:        "gpt-6.1-sol",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
			},
		},
		{
			name: "model with a known prefix but no exact price",
			data: &workflow.WorkflowData{
				Model:        "gpt-6.1-sol-preview",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
			},
			warning: true,
		},
		{
			name: "configured alias to priced model",
			data: &workflow.WorkflowData{
				Model:         "preferred-model",
				EngineConfig:  &workflow.EngineConfig{ID: "codex"},
				ModelMappings: map[string][]string{"preferred-model": {"gpt-6.1-sol"}},
			},
		},
		{
			name: "builtin sonnet alias",
			data: &workflow.WorkflowData{
				Model:         "sonnet",
				ModelMappings: workflow.BuiltinModelAliases(),
			},
		},
		{
			name: "builtin haiku alias",
			data: &workflow.WorkflowData{
				Model:         "haiku",
				ModelMappings: workflow.BuiltinModelAliases(),
			},
		},
		{
			name: "configured fallback",
			data: &workflow.WorkflowData{
				Model:                   "gpt-future-model",
				EngineConfig:            &workflow.EngineConfig{ID: "codex"},
				DefaultAiCreditsPricing: &workflow.AiCreditsPricingConfig{Input: 1, Output: 1},
			},
		},
		{
			name: "frontmatter model pricing",
			data: &workflow.WorkflowData{
				Model:        "gpt-future-model",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
				ModelCosts: map[string]any{
					"providers": map[string]any{
						"openai": map[string]any{
							"models": map[string]any{
								"gpt-future-model": map[string]any{
									"cost": map[string]string{"input": "0.000001", "output": "0.000001"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "empty model entry does not count as pricing",
			data: &workflow.WorkflowData{
				Model:        "gpt-future-model",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
				ModelCosts: map[string]any{
					"providers": map[string]any{
						"openai": map[string]any{
							"models": map[string]any{
								"gpt-future-model": map[string]any{},
							},
						},
					},
				},
			},
			warning: true,
		},
		{
			name: "empty cost entry does not count as pricing",
			data: &workflow.WorkflowData{
				Model:        "gpt-future-model",
				EngineConfig: &workflow.EngineConfig{ID: "codex"},
				ModelCosts: map[string]any{
					"providers": map[string]any{
						"openai": map[string]any{
							"models": map[string]any{
								"gpt-future-model": map[string]any{
									"cost": map[string]any{},
								},
							},
						},
					},
				},
			},
			warning: true,
		},
		{
			name: "dynamic model",
			data: &workflow.WorkflowData{
				Model:        "auto",
				EngineConfig: &workflow.EngineConfig{ID: "copilot"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message := configuredModelPricingWarning(test.data)
			if test.warning {
				require.Contains(t, message, "has no AI credits pricing")
				assert.Contains(t, message, "models.default-ai-credits-pricing")
				assert.Contains(t, message, "HTTP 400")
			} else {
				assert.Empty(t, message)
			}
		})
	}
}

func TestEnforceDevelopmentDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		setup   func(*workflow.Compiler, *CompilationStats, *[]ValidationResult)
		scanErr error
		message string
	}{
		{"clean", nil, nil, ""},
		{"compiler warnings", func(c *workflow.Compiler, _ *CompilationStats, _ *[]ValidationResult) { c.IncrementWarningCount() }, nil, "compiler reported warnings"},
		{"aggregate warnings", func(_ *workflow.Compiler, s *CompilationStats, _ *[]ValidationResult) { s.Warnings++ }, nil, "compiler reported warnings"},
		{"safe update warnings", func(c *workflow.Compiler, _ *CompilationStats, _ *[]ValidationResult) {
			c.AddSafeUpdateWarning("new restricted secret")
		}, nil, "new restricted secret"},
		{"structured warnings", func(_ *workflow.Compiler, _ *CompilationStats, r *[]ValidationResult) {
			(*r)[0].Warnings = []ValidationIssue{{Message: "unknown model"}}
		}, nil, "unknown model"},
		{"scanner error", nil, errors.New("scanner unavailable"), "scanner unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler := workflow.NewCompiler()
			stats := &CompilationStats{Total: 1, Succeeded: 1}
			results := []ValidationResult{{Workflow: "test.md", Valid: true}}
			if test.setup != nil {
				test.setup(compiler, stats, &results)
			}
			require.NoError(t, enforceDevelopmentDiagnostics(CompileConfig{}, compiler, stats, &results, test.scanErr))
			assert.True(t, results[0].Valid)
			err := enforceDevelopmentDiagnostics(CompileConfig{DryRun: true}, compiler, stats, &results, test.scanErr)
			if test.message == "" {
				require.NoError(t, err)
				assert.True(t, results[0].Valid)
				return
			}
			require.ErrorContains(t, err, test.message)
			if test.name == "structured warnings" {
				assert.False(t, results[0].Valid)
				require.Len(t, results[0].Errors, 1)
				assert.Equal(t, "development_validation", results[0].Errors[0].Type)
				assert.Zero(t, stats.Succeeded)
			} else {
				assert.True(t, results[0].Valid, "unscoped failures must not blame a workflow")
				require.Len(t, results, 2)
				assert.Equal(t, "batch", results[1].Scope)
				assert.False(t, results[1].Valid, "JSON must report the batch failure")
				require.Len(t, results[1].Errors, 1)
				assert.Contains(t, results[1].Errors[0].Message, test.message)
				assert.Equal(t, 1, stats.Succeeded)
			}
			assert.Equal(t, 1, stats.Errors)
		})
	}
}

func TestDevelopmentExplicitlyDisabledChecks(t *testing.T) {
	t.Parallel()
	for _, name := range DryRunRequiredBoolFlags() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := CompileConfig{DryRun: true, ExplicitBoolFlags: map[string]bool{name: false}}
			require.ErrorContains(t, validateCompileConfig(config), "--"+name+"=false")
			require.ErrorContains(t, validateCompileConfig(applyDevelopmentCompileMode(config)), "--"+name+"=false")
			_, err := CompileWorkflows(context.Background(), config)
			require.ErrorContains(t, err, "--"+name+"=false")
			config.DryRun = false
			require.NoError(t, validateCompileConfig(config))
			config.DryRun = true
			config.ExplicitBoolFlags[name] = true
			require.NoError(t, validateCompileConfig(config))
		})
	}
}

func TestDevelopmentDiagnosticsKeepWorkflowWarningsLocal(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()
	compiler.IncrementWarningCount()
	compiler.AddSafeUpdateWarning("batch safe-update warning")
	stats := &CompilationStats{Total: 2, Succeeded: 2, Warnings: 1}
	results := []ValidationResult{
		{Workflow: "a.md", Valid: true, Warnings: []ValidationIssue{{Type: "model", Message: "A has an unknown model", File: "a.md", Line: 3}}},
		{Workflow: "b.md", Valid: true, Errors: []ValidationIssue{}, Warnings: []ValidationIssue{}},
	}
	err := enforceDevelopmentDiagnostics(CompileConfig{DryRun: true}, compiler, stats, &results)
	require.ErrorContains(t, err, "A has an unknown model")
	require.ErrorContains(t, err, "batch safe-update warning")
	require.Len(t, results, 3)
	assert.False(t, results[0].Valid)
	require.Len(t, results[0].Errors, 1)
	assert.Equal(t, "a.md", results[0].Errors[0].File)
	assert.Equal(t, 3, results[0].Errors[0].Line)
	assert.True(t, results[1].Valid)
	assert.Empty(t, results[1].Errors)
	assert.Empty(t, results[1].Warnings)
	assert.Equal(t, "batch", results[2].Scope)
	assert.Equal(t, "compiler", results[2].Workflow)
	assert.False(t, results[2].Valid)
	require.Len(t, results[2].Errors, 2)
	assert.Equal(t, 1, stats.Succeeded)
	assert.Equal(t, 3, stats.Errors)

	output, err := formatValidationOutput(results)
	require.NoError(t, err)
	var decoded []ValidationResult
	require.NoError(t, json.Unmarshal([]byte(output), &decoded))
	assert.Equal(t, results, decoded)
}
