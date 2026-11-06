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
		{"outcome:accepted", "accepted", true},
		{"outcome:zero_touch", "zero_touch", true},
		{" outcome: accepted ", "accepted", true},
		{"outcome: zero_touch ", "zero_touch", true},
		{"outcome:", "", true},
		{"outcome: ", "", true},
		{"outcome:merged", "", true},
		{"outcome:ACCEPTED", "", true},
		{"outcome:accepted.value", "", true},
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
	for _, metric := range []string{"outcome:accepted", "outcome:zero_touch", "outcome:merged", "outcome:"} {
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
				if metric == "outcome:accepted" || metric == "outcome:zero_touch" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, fmt.Sprintf("experiments.quality.%s: unknown outcome metric %q. Expected outcome:accepted or outcome:zero_touch. Example: metric: outcome:accepted", field, metric))
				}
			})
		}
	}
}

func TestExperimentOutcomeWindow(t *testing.T) {
	for _, window := range []string{"", "auto", "72h", "1h30m", "0.5h", "1ns", "0", "0h", "-1h", "3d", "invalid", " 72h ", "999999999999999999999h"} {
		t.Run(window, func(t *testing.T) {
			raw := map[string]any{"variants": []any{"control", "candidate"}, "metric": "outcome:accepted"}
			if window != "" {
				raw["outcome_window"] = window
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
					assert.NotContains(t, spec, "outcome_window", "omission preserves the analysis default of auto")
				} else {
					assert.Contains(t, spec, fmt.Sprintf(`"outcome_window":%q`, window))
				}
			default:
				require.ErrorContains(t, err, "experiments.quality.outcome_window:")
				require.ErrorContains(t, err, `Expected "auto" or a positive Go duration. Example: outcome_window: 72h`)
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
		{"valid", "metric: outcome:accepted\n    secondary_metrics: [outcome:zero_touch]\n    guardrail_metrics:\n      - name: outcome:accepted\n        threshold: '>=0.9'\n    outcome_window: 48h", ""},
		{"automatic window", "metric: outcome:accepted\n    outcome_window: auto", ""},
		{"invalid primary", "metric: outcome:merged", `unknown outcome metric "outcome:merged"`},
		{"invalid secondary", "secondary_metrics: [outcome:merged]", `secondary_metrics: unknown outcome metric "outcome:merged"`},
		{"invalid guardrail", "guardrail_metrics:\n      - name: outcome:merged\n        threshold: '>=0.9'", `guardrail_metrics: unknown outcome metric "outcome:merged"`},
		{"invalid window", "outcome_window: 3d", `Expected "auto" or a positive Go duration`},
		{"zero window", "outcome_window: 0h", `Expected "auto" or a positive Go duration`},
		{"negative window", "outcome_window: -1h", `Expected "auto" or a positive Go duration`},
		{"empty window", "outcome_window: ''", "outcome_window"},
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
			if strings.Contains(tt.fields, "metric: outcome:accepted") {
				assert.Equal(t, "outcome:accepted", cfg.Metric)
			}
			if strings.Contains(tt.fields, "outcome_window: 48h") {
				assert.Equal(t, "48h", cfg.OutcomeWindow)
			}
			if strings.Contains(tt.fields, "outcome_window: auto") {
				assert.Equal(t, "auto", cfg.OutcomeWindow)
			}
		})
	}
}
