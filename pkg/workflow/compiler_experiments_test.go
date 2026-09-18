//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── sortedExperimentNames ─────────────────────────────────────────────────

func TestSortedExperimentNames(t *testing.T) {
	experiments := map[string][]string{
		"z_exp": {"A", "B"},
		"a_exp": {"X", "Y"},
		"m_exp": {"P", "Q"},
	}
	got := sortedExperimentNames(experiments)
	require.Equal(t, []string{"a_exp", "m_exp", "z_exp"}, got, "names should be sorted alphabetically")
}

// ── buildExperimentSpecJSON ───────────────────────────────────────────────

func TestBuildExperimentSpecJSON(t *testing.T) {
	experiments := map[string][]string{
		"feature1": {"A", "B"},
		"style":    {"concise", "detailed"},
	}
	names := []string{"feature1", "style"}
	// Without configs, bare-array fallback is used.
	got := buildExperimentSpecJSON(experiments, nil, names)
	assert.JSONEq(t, `{"feature1":["A","B"],"style":["concise","detailed"]}`, got, "JSON spec should match expected structure")
}

func TestBuildExperimentSpecJSONWithConfigs(t *testing.T) {
	experiments := map[string][]string{
		"style": {"concise", "detailed"},
	}

	configs := map[string]*ExperimentConfig{
		"style": {
			Variants:    []string{"concise", "detailed"},
			Description: "Test prompt style",
			Weight:      []int{70, 30},
			StartDate:   "2026-01-01",
			EndDate:     "2026-12-31",
		},
	}
	names := []string{"style"}
	got := buildExperimentSpecJSON(experiments, configs, names)
	// Full config object should be embedded.
	assert.Contains(t, got, `"variants"`, "should include variants key")
	assert.Contains(t, got, `"weight"`, "should include weight key")
	assert.Contains(t, got, `"start_date"`, "should include start_date key")
	assert.Contains(t, got, `"end_date"`, "should include end_date key")
	assert.Contains(t, got, "concise", "should include variant value")
}

func TestBuildExperimentSpecJSONWithContinualConfig(t *testing.T) {
	experiments := map[string][]string{"optimization": {"control", "candidate"}}
	configs := map[string]*ExperimentConfig{
		"optimization": {
			Variants: experiments["optimization"],
			Continual: &ContinualExperimentConfig{
				Seed: "stable-seed",
				Ramp: []int{10, 25, 50},
			},
		},
	}
	got := buildExperimentSpecJSON(experiments, configs, []string{"optimization"})
	assert.JSONEq(t, `{"optimization":{"variants":["control","candidate"],"continual":{"seed":"stable-seed","ramp":[10,25,50]}}}`, got)
}

func TestValidateContinualRamp(t *testing.T) {
	require.NoError(t, validateContinualRamp("optimization", &ContinualExperimentConfig{Ramp: []int{5, 20, 50}}))
	require.Error(t, validateContinualRamp("optimization", &ContinualExperimentConfig{Ramp: []int{20, 10}}))
}

func TestBuildExperimentSpecJSONEscaping(t *testing.T) {
	experiments := map[string][]string{
		`quote"test`: {`val"1`, `val\2`},
	}
	names := []string{`quote"test`}
	got := buildExperimentSpecJSON(experiments, nil, names)
	assert.Contains(t, got, `\"`, "double quotes should be escaped in JSON")
}

// ── generateExperimentSteps ───────────────────────────────────────────────

func TestGenerateExperimentSteps_Empty(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{}
	steps := c.generateExperimentSteps(data)
	assert.Empty(t, steps, "no steps should be generated when experiments is nil")
}

func TestGenerateExperimentSteps_Generated(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{
		WorkflowID: "my-workflow",
		Experiments: map[string][]string{
			"feature1": {"A", "B"},
		},
		// Default storage is repo; no ExperimentsStorage means repo.
	}
	steps := c.generateExperimentSteps(data)
	require.NotEmpty(t, steps, "steps should be generated when experiments are declared")

	joined := strings.Join(steps, "")
	// Repo storage: restore via GitHub API, no cache save step.
	assert.Contains(t, joined, "Restore experiment state from git", "should include git restore step for repo storage")
	assert.Contains(t, joined, "load_experiment_state_from_repo.cjs", "should reference load helper for repo storage")
	assert.Contains(t, joined, "GH_AW_EXPERIMENT_BRANCH", "should set branch env var")
	assert.Contains(t, joined, "experiments/myworkflow", "branch should include sanitized workflow ID")
	assert.Contains(t, joined, "Pick experiment variants", "should include pick step")
	assert.Contains(t, joined, "pick_experiment.cjs", "should reference pick_experiment.cjs")
	assert.NotContains(t, joined, "Save experiment state", "repo storage should not include cache save step")
	assert.Contains(t, joined, "Upload experiment artifact", "should include artifact upload step")
	assert.Contains(t, joined, "myworkflow-experiment", "artifact name should include sanitized workflow ID and 'experiment'")
	assert.NotContains(t, joined, "GH_AW_WORKFLOW_ID_SANITIZED", "branch name must not reference unset env var")
}

func TestGenerateExperimentSteps_CacheStorage(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{
		WorkflowID: "my-workflow",
		Experiments: map[string][]string{
			"feature1": {"A", "B"},
		},
		ExperimentsStorage: ExperimentsStorageCache,
	}
	steps := c.generateExperimentSteps(data)
	require.NotEmpty(t, steps, "steps should be generated when experiments are declared")

	joined := strings.Join(steps, "")
	// Cache storage: classic actions/cache restore and save.
	assert.Contains(t, joined, "Restore experiment state", "should include cache restore step")
	assert.NotContains(t, joined, "load_experiment_state_from_repo.cjs", "cache storage should not reference git load helper")
	assert.Contains(t, joined, "Pick experiment variants", "should include pick step")
	assert.Contains(t, joined, "pick_experiment.cjs", "should reference pick_experiment.cjs")
	assert.Contains(t, joined, "Save experiment state", "should include cache save step")
	assert.Contains(t, joined, "Upload experiment artifact", "should include artifact upload step")
	assert.Contains(t, joined, "myworkflow-experiment", "artifact name should include sanitized workflow ID and 'experiment'")
	// Cache key must embed the literal sanitized workflow ID, not the env var.
	assert.Contains(t, joined, "experiments-myworkflow-", "cache key should include the sanitized workflow ID")
	assert.NotContains(t, joined, "GH_AW_WORKFLOW_ID_SANITIZED", "cache key must not reference unset env var")
}

func TestGenerateExperimentSteps_SpecJSON(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{
		FrontmatterHash: "frontmatter-hash",
		BodyHash:        "body-hash",
		Experiments: map[string][]string{
			"style": {"concise", "detailed"},
		},
	}
	steps := c.generateExperimentSteps(data)
	joined := strings.Join(steps, "")
	assert.Contains(t, joined, `{"style":["concise","detailed"]}`, "spec JSON should be embedded in the step")
	assert.Contains(t, joined, "GH_AW_HARNESS_VERSION: frontmatter-hash:body-hash", "assignment should use the compiled workflow hashes")
}

func TestExperimentHarnessVersionUsesAvailableCompiledHashes(t *testing.T) {
	assert.Equal(t, "frontmatter-hash:body-hash", experimentHarnessVersion(&WorkflowData{FrontmatterHash: "frontmatter-hash", BodyHash: "body-hash"}))
	assert.Equal(t, "frontmatter-hash", experimentHarnessVersion(&WorkflowData{FrontmatterHash: "frontmatter-hash"}))
	assert.Equal(t, "body-hash", experimentHarnessVersion(&WorkflowData{BodyHash: "body-hash"}))
	assert.Equal(t, "unknown", experimentHarnessVersion(&WorkflowData{}))
}

func TestGenerateExperimentSteps_SingleQuoteEscaping(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{
		Experiments: map[string][]string{
			"variant": {"Bob's choice", "Alice's choice"},
		},
	}
	steps := c.generateExperimentSteps(data)
	joined := strings.Join(steps, "")
	// Single quotes in JSON string values must be doubled for YAML single-quoted scalar.
	assert.Contains(t, joined, "Bob''s", "single quotes in variant values must be escaped as '' in YAML")
	assert.Contains(t, joined, "Alice''s", "single quotes in variant values must be escaped as '' in YAML")
}

func TestExperimentExpressionMappings(t *testing.T) {
	experiments := map[string][]string{
		"caveman": {"yes", "no"},
		"style":   {"concise", "detailed"},
	}
	mappings := ExperimentExpressionMappings(experiments)
	require.Len(t, mappings, 2, "one mapping per experiment")

	// Build a lookup by EnvVar for easier assertions
	byEnvVar := make(map[string]*ExpressionMapping, len(mappings))
	for _, m := range mappings {
		byEnvVar[m.EnvVar] = m
	}

	m := byEnvVar["GH_AW_EXPERIMENTS_CAVEMAN"]
	require.NotNil(t, m, "mapping for GH_AW_EXPERIMENTS_CAVEMAN should exist")
	assert.Equal(t, "steps.pick-experiment.outputs.caveman", m.Content, "content should be the step output expression")
	assert.Equal(t, "${{ experiments.caveman }}", m.Original, "original should be the experiments expression")

	m2 := byEnvVar["GH_AW_EXPERIMENTS_STYLE"]
	require.NotNil(t, m2, "mapping for GH_AW_EXPERIMENTS_STYLE should exist")
	assert.Equal(t, "steps.pick-experiment.outputs.style", m2.Content, "content should be the step output expression")
}

func TestRewriteExperimentsReferenceForDownstreamJobs(t *testing.T) {
	experiments := map[string][]string{"model": {"sonnet", "opus"}}
	assert.Equal(t, "${{ needs.activation.outputs.model }}", RewriteExperimentsReferenceForDownstreamJobs("${{ experiments.model }}", experiments))
	assert.Equal(t, "no reference here", RewriteExperimentsReferenceForDownstreamJobs("no reference here", experiments))
	// Undeclared experiment names are left untouched.
	assert.Equal(t, "${{ experiments.unknown }}", RewriteExperimentsReferenceForDownstreamJobs("${{ experiments.unknown }}", experiments))
}

func TestRewriteActivationOutputsToLocalStepOutputs(t *testing.T) {
	experiments := map[string][]string{"model": {"sonnet", "opus"}}
	assert.Equal(t, "${{ steps.pick-experiment.outputs.model }}", RewriteActivationOutputsToLocalStepOutputs("${{ needs.activation.outputs.model }}", experiments))
	assert.Equal(t, "no reference here", RewriteActivationOutputsToLocalStepOutputs("no reference here", experiments))
	// Undeclared experiment names are left untouched.
	assert.Equal(t, "${{ needs.activation.outputs.unknown }}", RewriteActivationOutputsToLocalStepOutputs("${{ needs.activation.outputs.unknown }}", experiments))
}

// TestRewriteExperimentPrefixCollision guards against one declared experiment name being a
// prefix of another (e.g. "model" and "model_variant"); the word-boundary-anchored regex
// rewrite must not partially corrupt the longer name's reference while rewriting the shorter
// one.
func TestRewriteExperimentPrefixCollision(t *testing.T) {
	experiments := map[string][]string{
		"model":         {"a", "b"},
		"model_variant": {"x", "y"},
	}
	got := RewriteExperimentsReferenceForDownstreamJobs("${{ experiments.model }} ${{ experiments.model_variant }}", experiments)
	assert.Equal(t, "${{ needs.activation.outputs.model }} ${{ needs.activation.outputs.model_variant }}", got)

	back := RewriteActivationOutputsToLocalStepOutputs(got, experiments)
	assert.Equal(t, "${{ steps.pick-experiment.outputs.model }} ${{ steps.pick-experiment.outputs.model_variant }}", back)
}

// TestRewriteExperimentsReferenceOnlyRewritesExpressionTokens guards against the rewrite
// mutating text that merely looks like an experiment reference: plain text outside an
// expression, string literals inside an expression, and property chains where
// `experiments.<name>` is not a standalone reference.
func TestRewriteExperimentsReferenceOnlyRewritesExpressionTokens(t *testing.T) {
	experiments := map[string][]string{"model": {"sonnet", "opus"}}

	// Plain text outside ${{ }} is not an expression.
	assert.Equal(t, "experiments.model", RewriteExperimentsReferenceForDownstreamJobs("experiments.model", experiments))

	// String literals inside an expression are data, not references.
	literal := "${{ format('experiments.model-{0}', inputs.suffix) }}"
	assert.Equal(t, literal, RewriteExperimentsReferenceForDownstreamJobs(literal, experiments))

	// A literal containing an escaped quote must not desynchronize literal detection.
	escaped := "${{ format('it''s experiments.model', experiments.model) }}"
	assert.Equal(t, "${{ format('it''s experiments.model', needs.activation.outputs.model) }}",
		RewriteExperimentsReferenceForDownstreamJobs(escaped, experiments))

	// Property chains are not experiment placeholders.
	nested := "${{ fromJSON(inputs.config).experiments.model }}"
	assert.Equal(t, nested, RewriteExperimentsReferenceForDownstreamJobs(nested, experiments))
	suffixed := "${{ experiments.model.foo }}"
	assert.Equal(t, suffixed, RewriteExperimentsReferenceForDownstreamJobs(suffixed, experiments))

	// Composite expressions still rewrite the standalone reference.
	assert.Equal(t, "${{ needs.activation.outputs.model || 'sonnet' }}",
		RewriteExperimentsReferenceForDownstreamJobs("${{ experiments.model || 'sonnet' }}", experiments))
}

// ── buildExperimentArtifactDownloadSteps ──────────────────────────────────

func TestBuildExperimentArtifactDownloadStep_Empty(t *testing.T) {
	steps := buildExperimentArtifactDownloadSteps(&WorkflowData{WorkflowID: "test-wf"}, getActionPin)
	assert.Empty(t, steps, "no steps when experiments is nil")

	steps = buildExperimentArtifactDownloadSteps(&WorkflowData{WorkflowID: "test-wf", Experiments: map[string][]string{}}, getActionPin)
	assert.Empty(t, steps, "no steps when experiments is empty")
}

func TestBuildExperimentArtifactDownloadStep_Generated(t *testing.T) {
	// workflow_call trigger: artifact name uses the runtime prefix expression.
	data := &WorkflowData{
		WorkflowID:  "my-wf",
		Experiments: map[string][]string{"caveman": {"yes", "no"}},
		On:          "workflow_call:",
	}
	steps := buildExperimentArtifactDownloadSteps(data, getActionPin)
	require.NotEmpty(t, steps, "steps should be generated when experiments are declared")
	joined := strings.Join(steps, "")
	assert.Contains(t, joined, "Download experiment artifact", "should include download step name")
	assert.Contains(t, joined, "experiment", "should reference experiment artifact")
	assert.Contains(t, joined, experimentsCacheDir, "should download to experiments cache dir")
	assert.Contains(t, joined, "actions/download-artifact", "should use download-artifact action")
	assert.Contains(t, joined, "${{ needs.activation.outputs.artifact_prefix }}", "workflow_call should use runtime prefix")
}

func TestBuildExperimentArtifactDownloadStep_NoPrefix(t *testing.T) {
	// Non-workflow_call workflows use the sanitized workflow ID as prefix.
	data := &WorkflowData{
		WorkflowID:  "smoke-copilot",
		Experiments: map[string][]string{"style": {"A", "B"}},
	}
	steps := buildExperimentArtifactDownloadSteps(data, getActionPin)
	require.NotEmpty(t, steps, "steps should be generated")
	joined := strings.Join(steps, "")
	// Artifact name should include the sanitized workflow ID as prefix.
	assert.Contains(t, joined, "          name: smokecopilot-experiment\n", "artifact name should include sanitized workflow ID")
	assert.NotContains(t, joined, "          pattern: smokecopilot-experiment\n", "single-artifact downloads should avoid ambiguous pattern matching")
	assert.NotContains(t, joined, "          merge-multiple: true\n", "single-artifact downloads should not merge multiple matches")
}

// ── extractExperimentConfigsFromFrontmatter ───────────────────────────────

func TestExtractExperimentConfigsFromFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		check       func(t *testing.T, got map[string]*ExperimentConfig)
	}{
		{
			name:        "nil returns nil",
			frontmatter: map[string]any{},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				assert.Nil(t, got, "nil when no experiments")
			},
		},
		{
			name: "bare array form returns config with variants only",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"feature1": []any{"A", "B"},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["feature1"]
				require.NotNil(t, cfg, "feature1 config should exist")
				assert.Equal(t, []string{"A", "B"}, cfg.Variants, "variants should match")
				assert.Empty(t, cfg.Description, "no description")
				assert.Nil(t, cfg.Weight, "no weight")
			},
		},
		{
			name: "object form with all metadata fields",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"prompt_style": map[string]any{
						"variants":    []any{"concise", "verbose"},
						"description": "Test prompt styles",
						"metric":      "aic",
						"weight":      []any{60.0, 40.0},
						"issue":       float64(1234),
						"start_date":  "2026-05-01",
						"end_date":    "2026-06-15",
					},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["prompt_style"]
				require.NotNil(t, cfg, "prompt_style config should exist")
				assert.Equal(t, []string{"concise", "verbose"}, cfg.Variants, "variants should match")
				assert.Equal(t, "Test prompt styles", cfg.Description, "description should match")
				assert.Equal(t, "aic", cfg.Metric, "metric should match")
				assert.Equal(t, []int{60, 40}, cfg.Weight, "weight should match")
				assert.Equal(t, 1234, cfg.Issue, "issue should match")
				assert.Equal(t, "2026-05-01", cfg.StartDate, "start_date should match")
				assert.Equal(t, "2026-06-15", cfg.EndDate, "end_date should match")
			},
		},
		{
			name: "object form with new extended metadata fields",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"prompt_style": map[string]any{
						"variants":          []any{"concise", "detailed"},
						"hypothesis":        "H0: no change. H1: concise reduces tokens by >=15%",
						"secondary_metrics": []any{"duration_ms", "discussion_word_count"},
						"guardrail_metrics": []any{
							map[string]any{"name": "success_rate", "direction": "max", "threshold": ">=0.95"},
							map[string]any{"name": "empty_output_rate", "direction": "min", "threshold": "==0"},
						},
						"min_samples": float64(25),
					},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["prompt_style"]
				require.NotNil(t, cfg, "prompt_style config should exist")
				assert.Equal(t, "H0: no change. H1: concise reduces tokens by >=15%", cfg.Hypothesis, "hypothesis should match")
				assert.Equal(t, []string{"duration_ms", "discussion_word_count"}, cfg.SecondaryMetrics, "secondary_metrics should match")
				require.Len(t, cfg.GuardrailMetrics, 2, "guardrail_metrics should have 2 entries")
				assert.Equal(t, "success_rate", cfg.GuardrailMetrics[0].Name, "first guardrail name")
				assert.Equal(t, "max", cfg.GuardrailMetrics[0].Direction, "first guardrail direction")
				assert.Equal(t, ">=0.95", cfg.GuardrailMetrics[0].Threshold, "first guardrail threshold")
				assert.Equal(t, "empty_output_rate", cfg.GuardrailMetrics[1].Name, "second guardrail name")
				assert.Equal(t, "min", cfg.GuardrailMetrics[1].Direction, "second guardrail direction")
				assert.Equal(t, "==0", cfg.GuardrailMetrics[1].Threshold, "second guardrail threshold")
				assert.Equal(t, 25, cfg.MinSamples, "min_samples should match")
			},
		},
		{
			name: "mixed bare array and object form in same map",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"bare":   []any{"X", "Y"},
					"object": map[string]any{"variants": []any{"P", "Q"}, "weight": []any{30.0, 70.0}},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "configs should exist")
				require.Len(t, got, 2, "two experiments")
				assert.Equal(t, []string{"X", "Y"}, got["bare"].Variants, "bare variants")
				assert.Equal(t, []string{"P", "Q"}, got["object"].Variants, "object variants")
				assert.Equal(t, []int{30, 70}, got["object"].Weight, "object weight")
			},
		},
		{
			name: "guardrail entry with empty threshold is skipped",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"exp": map[string]any{
						"variants": []any{"A", "B"},
						"guardrail_metrics": []any{
							map[string]any{"name": "success_rate"},                      // missing threshold — should be skipped
							map[string]any{"name": "success_rate", "threshold": ""},     // empty threshold — should be skipped
							map[string]any{"name": "error_rate", "threshold": "<=0.05"}, // valid
						},
					},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["exp"]
				require.NotNil(t, cfg, "exp config should exist")
				require.Len(t, cfg.GuardrailMetrics, 1, "only the valid guardrail entry should be kept")
				assert.Equal(t, "error_rate", cfg.GuardrailMetrics[0].Name, "guardrail name")
				assert.Equal(t, "<=0.05", cfg.GuardrailMetrics[0].Threshold, "guardrail threshold")
			},
		},
		{
			name: "numeric guardrail threshold is parsed",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"exp": map[string]any{
						"variants": []any{"A", "B"},
						"guardrail_metrics": []any{
							map[string]any{"name": "empty_dispatch_rate", "direction": "min", "threshold": 0.0},
						},
					},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["exp"]
				require.NotNil(t, cfg, "exp config should exist")
				require.Len(t, cfg.GuardrailMetrics, 1, "guardrail_metrics should have 1 entry")
				assert.Equal(t, "empty_dispatch_rate", cfg.GuardrailMetrics[0].Name, "guardrail name")
				assert.Equal(t, "min", cfg.GuardrailMetrics[0].Direction, "guardrail direction")
				assert.Equal(t, "0", cfg.GuardrailMetrics[0].Threshold, "guardrail threshold")
			},
		},
		{
			// goccy/go-yaml returns YAML integers as uint64, not int/int64/float64.
			// This test ensures min_samples and issue are parsed correctly from uint64 values.
			name: "uint64 integer values for min_samples and issue are parsed correctly",
			frontmatter: map[string]any{
				"experiments": map[string]any{
					"exp": map[string]any{
						"variants":    []any{"A", "B"},
						"min_samples": uint64(30),
						"issue":       uint64(999),
						"weight":      []any{uint64(60), uint64(40)},
					},
				},
			},
			check: func(t *testing.T, got map[string]*ExperimentConfig) {
				require.NotNil(t, got, "config should exist")
				cfg := got["exp"]
				require.NotNil(t, cfg, "exp config should exist")
				assert.Equal(t, 30, cfg.MinSamples, "min_samples parsed from uint64")
				assert.Equal(t, 999, cfg.Issue, "issue parsed from uint64")
				assert.Equal(t, []int{60, 40}, cfg.Weight, "weight items parsed from uint64")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExperimentConfigsFromFrontmatter(tt.frontmatter)
			tt.check(t, got)
		})
	}
}

// ── generateExperimentSteps with ExperimentConfigs ────────────────────────

func TestGenerateExperimentSteps_WithConfigs(t *testing.T) {
	c := &Compiler{}
	data := &WorkflowData{
		Experiments: map[string][]string{
			"style": {"concise", "detailed"},
		},
		ExperimentConfigs: map[string]*ExperimentConfig{
			"style": {
				Variants:  []string{"concise", "detailed"},
				Weight:    []int{70, 30},
				StartDate: "2026-01-01",
			},
		},
	}
	steps := c.generateExperimentSteps(data)
	joined := strings.Join(steps, "")
	// The spec should embed the full config object.
	assert.Contains(t, joined, `"variants"`, "spec should include variants key")
	assert.Contains(t, joined, `"weight"`, "spec should include weight key")
	assert.Contains(t, joined, `"start_date"`, "spec should include start_date key")
}

// ── extractExperimentsStorageFromFrontmatter ──────────────────────────────

func TestExtractExperimentsStorageFromFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        ExperimentStorageMode
	}{
		{
			name:        "no experiments key returns repo default",
			frontmatter: map[string]any{},
			want:        ExperimentsStorageRepo,
		},
		{
			name:        "nil experiments returns repo default",
			frontmatter: map[string]any{"experiments": nil},
			want:        ExperimentsStorageRepo,
		},
		{
			name:        "experiments without storage key returns repo default",
			frontmatter: map[string]any{"experiments": map[string]any{"my_exp": []any{"A", "B"}}},
			want:        ExperimentsStorageRepo,
		},
		{
			name:        "storage: repo returns repo",
			frontmatter: map[string]any{"experiments": map[string]any{"storage": "repo"}},
			want:        ExperimentsStorageRepo,
		},
		{
			name:        "storage: cache returns cache",
			frontmatter: map[string]any{"experiments": map[string]any{"storage": "cache"}},
			want:        ExperimentsStorageCache,
		},
		{
			name:        "unknown storage value returns repo default",
			frontmatter: map[string]any{"experiments": map[string]any{"storage": "unknown"}},
			want:        ExperimentsStorageRepo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExperimentsStorageFromFrontmatter(tt.frontmatter)
			assert.Equal(t, tt.want, got, "storage should match expected value")
		})
	}
}

// ── experimentsBranchName ─────────────────────────────────────────────────

func TestExperimentsBranchName(t *testing.T) {
	tests := []struct {
		workflowID string
		want       string
	}{
		{"my-workflow", "experiments/myworkflow"},
		{"smoke-copilot", "experiments/smokecopilot"},
		{"", "experiments/default"},
	}
	for _, tt := range tests {
		t.Run(tt.workflowID, func(t *testing.T) {
			got := experimentsBranchName(tt.workflowID)
			assert.Equal(t, tt.want, got, "branch name should use experiments/ prefix")
		})
	}
}

// ── storage key is excluded from experiment configs ───────────────────────

func TestExtractExperimentConfigsFromFrontmatter_StorageKeyIsSkipped(t *testing.T) {
	frontmatter := map[string]any{
		"experiments": map[string]any{
			"storage": "repo",
			"my_exp":  []any{"A", "B"},
		},
	}
	got := extractExperimentConfigsFromFrontmatter(frontmatter)
	require.NotNil(t, got, "experiment configs should not be nil")
	assert.Contains(t, got, "my_exp", "my_exp should be present")
	assert.NotContains(t, got, "storage", "storage key should be excluded from experiment configs")
}

func TestParseExperimentMetricEvalReference(t *testing.T) {
	tests := []struct {
		name      string
		metric    string
		wantID    string
		wantMatch bool
	}{
		{name: "empty metric", metric: "", wantID: "", wantMatch: false},
		{name: "normal metric", metric: "aic", wantID: "", wantMatch: false},
		{name: "eval colon format", metric: "eval:builds", wantID: "builds", wantMatch: true},
		{name: "eval dotted format", metric: "evals.builds", wantID: "builds", wantMatch: true},
		{name: "eval dotted with suffix", metric: "evals.builds.yes_rate", wantID: "builds", wantMatch: true},
		{name: "eval empty id", metric: "eval:", wantID: "", wantMatch: true},
		{name: "evals empty id", metric: "evals.", wantID: "", wantMatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := ParseExperimentMetricEvalReference(tt.metric)
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantMatch, gotMatch)
		})
	}
}

func TestParseExperimentMetricGraderReference(t *testing.T) {
	tests := []struct {
		name      string
		metric    string
		wantID    string
		wantMatch bool
	}{
		{name: "empty metric", metric: "", wantID: "", wantMatch: false},
		{name: "normal metric", metric: "aic", wantID: "", wantMatch: false},
		{name: "grader colon format", metric: "grader:loops", wantID: "loops", wantMatch: true},
		{name: "grader dotted format", metric: "graders.loops", wantID: "loops", wantMatch: true},
		{name: "grader dotted with suffix", metric: "graders.loops.value", wantID: "loops", wantMatch: true},
		{name: "grader empty id", metric: "grader:", wantID: "", wantMatch: true},
		{name: "graders empty id", metric: "graders.", wantID: "", wantMatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotMatch := ParseExperimentMetricGraderReference(tt.metric)
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantMatch, gotMatch)
		})
	}
}

func TestValidateExperimentMetricReferences(t *testing.T) {
	tests := []struct {
		name    string
		configs map[string]*ExperimentConfig
		evals   *EvalsConfig
		graders *GradersConfig
		wantErr string
	}{
		{
			name: "non eval metric is accepted",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "aic"},
			},
			evals:   nil,
			graders: nil,
			wantErr: "",
		},
		{
			name: "eval metric references existing eval",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "eval:builds"},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "builds", Question: "Does it build?"}},
			},
			graders: nil,
			wantErr: "",
		},
		{
			name: "eval dotted metric references existing eval",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "evals.builds.yes_rate"},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "builds", Question: "Does it build?"}},
			},
			graders: nil,
			wantErr: "",
		},
		{
			name: "eval reference rejected when eval id is unknown",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "eval:builds"},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "tests", Question: "Do tests pass?"}},
			},
			graders: nil,
			wantErr: `references unknown eval "builds"`,
		},
		{
			name: "eval reference rejected when evals are not declared",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "eval:builds"},
			},
			evals:   nil,
			graders: nil,
			wantErr: `references eval "builds" but no evals are declared`,
		},
		{
			name: "eval reference requires non empty id",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "eval:"},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "builds", Question: "Does it build?"}},
			},
			graders: nil,
			wantErr: "expected eval reference format eval:<question_id>",
		},
		{
			name: "eval colon metric trims whitespace from id",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "eval: builds "},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "builds", Question: "Does it build?"}},
			},
			graders: nil,
			wantErr: "",
		},
		{
			name: "grader metric references existing grader",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "grader:loops"},
			},
			evals: nil,
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{
					"loops": {ID: "loops"},
				},
			},
			wantErr: "",
		},
		{
			name: "grader dotted metric references existing grader",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "graders.loops.value"},
			},
			evals: nil,
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{
					"loops": {ID: "loops"},
				},
			},
			wantErr: "",
		},
		{
			name: "grader reference rejected when grader id is unknown",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "grader:loops"},
			},
			evals: nil,
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{
					"retries": {ID: "retries"},
				},
			},
			wantErr: `references unknown grader "loops"`,
		},
		{
			name: "grader reference rejected when graders are not declared",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "grader:loops"},
			},
			evals:   nil,
			graders: nil,
			wantErr: `references grader "loops" but no graders are declared`,
		},
		{
			name: "grader reference requires non empty id",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "grader:"},
			},
			evals: nil,
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{
					"loops": {ID: "loops"},
				},
			},
			wantErr: "expected grader reference format grader:<grader_id>",
		},
		{
			name: "grader colon metric trims whitespace from id",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {Metric: "grader: loops "},
			},
			evals: nil,
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{
					"loops": {ID: "loops"},
				},
			},
			wantErr: "",
		},
		{
			name: "grader guardrail references existing grader",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {
					GuardrailMetrics: []GuardrailMetric{{Name: "grader:loops", Threshold: "<=1"}},
				},
			},
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{"loops": {ID: "loops"}},
			},
			wantErr: "",
		},
		{
			name: "grader guardrail rejects unknown grader",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {
					GuardrailMetrics: []GuardrailMetric{{Name: "grader:loops", Threshold: "<=1"}},
				},
			},
			graders: &GradersConfig{
				Graders: map[string]*GraderDefinition{"retries": {ID: "retries"}},
			},
			wantErr: `guardrail_metrics: references unknown grader "loops"`,
		},
		{
			name: "eval guardrail references existing eval",
			configs: map[string]*ExperimentConfig{
				"prompt_style": {
					GuardrailMetrics: []GuardrailMetric{{Name: "eval:quality", Threshold: ">=0.9"}},
				},
			},
			evals: &EvalsConfig{
				Questions: []EvalDefinition{{ID: "quality", Question: "Is quality acceptable?"}},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExperimentMetricReferences(tt.configs, tt.evals, tt.graders)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
