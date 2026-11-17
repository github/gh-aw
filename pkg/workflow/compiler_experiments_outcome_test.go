//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExperimentMetricOutcomeReference(t *testing.T) {
	tests := []struct {
		metric string
		kind   string
		match  bool
	}{
		{"", "", false},
		{"ai_credits_total", "", false},
		{"grader:accepted", "", false},
		{"outcomes.accepted", "", false},
		{"adoption:accepted", "accepted", true},
		{"adoption:zero_touch", "zero_touch", true},
		{" adoption: accepted ", "accepted", true},
		{"adoption: zero_touch ", "zero_touch", true},
		{"adoption:", "", true},
		{"adoption: ", "", true},
		{"adoption:merged", "", true},
		{"adoption:ACCEPTED", "", true},
		{"outcome:accepted", "", false},
		{"adoption:accepted.value", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			kind, match := ParseExperimentMetricOutcomeReference(tt.metric)
			assert.Equal(t, tt.kind, kind)
			assert.Equal(t, tt.match, match)
		})
	}
}

func TestValidateExperimentOutcomeMetricReferences(t *testing.T) {
	for _, metric := range []string{"adoption:accepted", "adoption:zero_touch", "adoption:merged", "adoption:"} {
		for _, field := range []string{"metric", "secondary_metrics", "guardrail_metrics"} {
			t.Run(field+"/"+metric, func(t *testing.T) {
				cfg := &ExperimentConfig{}
				switch field {
				case "metric":
					cfg.Metric = metric
				case "secondary_metrics":
					cfg.SecondaryMetrics = []string{"ai_credits_total", metric}
				case "guardrail_metrics":
					cfg.GuardrailMetrics = []GuardrailMetric{{Name: metric, Threshold: ">=0.9"}}
				}
				err := validateExperimentMetricReferences(map[string]*ExperimentConfig{"quality": cfg}, nil, nil)
				if metric == "adoption:accepted" || metric == "adoption:zero_touch" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, fmt.Sprintf("experiments.quality.%s: unknown adoption metric %q. Expected adoption:accepted or adoption:zero_touch. Example: metric: adoption:accepted", field, metric))
				}
			})
		}
	}
}

func TestRemovedExperimentOutcomeMetric(t *testing.T) {
	for _, field := range []string{"metric", "secondary_metrics", "guardrail_metrics"} {
		t.Run(field, func(t *testing.T) {
			cfg := &ExperimentConfig{}
			switch field {
			case "metric":
				cfg.Metric = "outcome:accepted"
			case "secondary_metrics":
				cfg.SecondaryMetrics = []string{"outcome:accepted"}
			case "guardrail_metrics":
				cfg.GuardrailMetrics = []GuardrailMetric{{Name: "outcome:accepted", Threshold: ">=0.9"}}
			}
			err := validateExperimentMetricReferences(map[string]*ExperimentConfig{"quality": cfg}, nil, nil)
			require.EqualError(t, err, "experiments.quality."+field+": outcome: metrics have been removed; use adoption:accepted or adoption:zero_touch")
		})
	}
}

func TestExperimentOutcomeWindow(t *testing.T) {
	for _, window := range []string{"", "auto", "72h", "1h30m", "0.5h", "1ns", "0", "0h", "-1h", "3d", "invalid", " 72h ", "999999999999999999999h"} {
		t.Run(window, func(t *testing.T) {
			raw := map[string]any{"variants": []any{"control", "candidate"}, "metric": "adoption:accepted"}
			if window != "" {
				raw["adoption_window"] = window
			}
			cfg := extractOneExperimentConfig("quality", raw)
			require.NotNil(t, cfg)
			assert.Equal(t, window, cfg.OutcomeWindow)
			err := validateExperimentMetricReferences(map[string]*ExperimentConfig{"quality": cfg}, nil, nil)
			switch window {
			case "", "auto", "72h", "1h30m", "0.5h", "1ns":
				require.NoError(t, err)
				spec := buildExperimentSpecJSON(nil, map[string]*ExperimentConfig{"quality": cfg}, []string{"quality"})
				if window == "" {
					assert.NotContains(t, spec, "adoption_window", "omission preserves the analysis default of auto")
				} else {
					assert.Contains(t, spec, fmt.Sprintf(`"adoption_window":%q`, window))
				}
			default:
				require.ErrorContains(t, err, "experiments.quality.adoption_window:")
				require.ErrorContains(t, err, `Expected "auto" or a positive Go duration. Example: adoption_window: 72h`)
			}
		})
	}
}

func TestParseWorkflowExperimentOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fields  string
		wantErr string
	}{
		{"valid", "metric: adoption:accepted\n    secondary_metrics: [adoption:zero_touch]\n    guardrail_metrics:\n      - name: adoption:accepted\n        threshold: '>=0.9'\n    adoption_window: 48h", ""},
		{"automatic window", "metric: adoption:accepted\n    adoption_window: auto", ""},
		{"invalid primary", "metric: adoption:merged", `unknown adoption metric "adoption:merged"`},
		{"invalid secondary", "secondary_metrics: [adoption:merged]", `secondary_metrics: unknown adoption metric "adoption:merged"`},
		{"invalid guardrail", "guardrail_metrics:\n      - name: adoption:merged\n        threshold: '>=0.9'", `guardrail_metrics: unknown adoption metric "adoption:merged"`},
		{"invalid window", "adoption_window: 3d", `Expected "auto" or a positive Go duration`},
		{"zero window", "adoption_window: 0h", `Expected "auto" or a positive Go duration`},
		{"negative window", "adoption_window: -1h", `Expected "auto" or a positive Go duration`},
		{"empty window", "adoption_window: ''", "adoption_window"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "workflow.md")
			content := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\nexperiments:\n  quality:\n    variants: [control, candidate]\n    " + tt.fields + "\n---\n# Test\nCompare outcomes.\n"
			require.NoError(t, os.WriteFile(source, []byte(content), 0600))
			data, err := NewCompiler().ParseWorkflowFile(source)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			cfg := data.ExperimentConfigs["quality"]
			require.NotNil(t, cfg)
			if strings.Contains(tt.fields, "metric: adoption:accepted") {
				assert.Equal(t, "adoption:accepted", cfg.Metric)
			}
			if strings.Contains(tt.fields, "adoption_window: 48h") {
				assert.Equal(t, "48h", cfg.OutcomeWindow)
			}
			if strings.Contains(tt.fields, "adoption_window: auto") {
				assert.Equal(t, "auto", cfg.OutcomeWindow)
			}
		})
	}
}
