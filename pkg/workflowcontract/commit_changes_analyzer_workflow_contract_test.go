package workflowcontract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestCommitChangesAnalyzerExperimentContract(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "commit-changes-analyzer.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		t.Fatal("workflow must have YAML frontmatter")
	}
	var config struct {
		Permissions map[string]string
		Env         map[string]string
		Model       string
		Experiments struct {
			Storage     string
			DetailLevel struct {
				Variants         []string
				Weight           []int
				Metric           string
				SecondaryMetrics []string `yaml:"secondary_metrics"`
				MinSamples       int      `yaml:"min_samples"`
				StartDate        string   `yaml:"start_date"`
				Issue            int
				Guardrails       []struct {
					Name      string
					Direction string
					Threshold float64
				} `yaml:"guardrail_metrics"`
			} `yaml:"detail_level"`
		}
		Evals []struct {
			ID       string
			Question string
		}
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &config); err != nil {
		t.Fatal(err)
	}
	experiment := config.Experiments.DetailLevel
	if config.Experiments.Storage != "cache" || config.Permissions["contents"] != "read" || len(experiment.Weight) != 0 {
		t.Error("campaign must use balanced cache-backed assignment with read-only contents")
	}
	if !reflect.DeepEqual(experiment.Variants, []string{"comprehensive", "brief"}) ||
		experiment.Metric != "eval:reviewer_ready" || experiment.MinSamples != 82 ||
		experiment.StartDate != "2026-10-24" || experiment.Issue != 71628 {
		t.Errorf("unexpected campaign configuration: %+v", experiment)
	}
	if !reflect.DeepEqual(experiment.SecondaryMetrics, []string{"eval:detail_variant_visible", "ai_credits", "run_duration_seconds"}) {
		t.Errorf("unexpected secondary metrics: %v", experiment.SecondaryMetrics)
	}
	if len(experiment.Guardrails) != 2 ||
		experiment.Guardrails[0].Name != "eval:changes_analyzed" || experiment.Guardrails[0].Direction != "max" || experiment.Guardrails[0].Threshold != 0.95 ||
		experiment.Guardrails[1].Name != "empty_output_rate" || experiment.Guardrails[1].Direction != "min" || experiment.Guardrails[1].Threshold != 0 {
		t.Errorf("unexpected guardrails: %+v", experiment.Guardrails)
	}
	wantQuestions := map[string]string{
		"changes_analyzed":       "Did the agent analyze all repository changes since the specified commit and produce a comprehensive report?",
		"discussion_created":     "Was a discussion or report created summarizing the changes since the specified commit?",
		"reviewer_ready":         "Does the report connect at least one changed file or commit, identified by a repository link, to a concrete developer implication and a specific recommended review action?",
		"detail_variant_visible": "Does the report state its assigned detail level and follow that level in Developer Notes: brief uses at most one explanatory sentence per affected functional area, while comprehensive uses two or three?",
	}
	if len(config.Evals) != len(wantQuestions) {
		t.Fatalf("expected four evals, got %d", len(config.Evals))
	}
	for _, eval := range config.Evals {
		if question, ok := wantQuestions[eval.ID]; !ok || eval.Question != question {
			t.Errorf("unexpected eval: %+v", eval)
		}
		delete(wantQuestions, eval.ID)
	}
	if len(wantQuestions) != 0 {
		t.Errorf("missing evals: %v", wantQuestions)
	}
	if config.Env["GH_AW_ANALYZED_END_SHA"] != "${{ github.sha }}" ||
		config.Env["GH_AW_CAMPAIGN_RUN_ATTEMPT"] != "${{ github.run_attempt }}" ||
		config.Model != "copilot/gpt-5.6-luna" {
		t.Error("pinned range, attempt provenance, and analysis model must be preserved")
	}
	for _, token := range []string{
		"{{#if experiments.detail_level == 'brief' }}",
		"Use at most one explanatory sentence per affected functional area in Developer Notes.",
		"{{#else}}",
		"Use two or three explanatory sentences per affected functional area in Developer Notes.",
		"{{#endif}}",
		"Both levels must retain every required fact, table, file list, evidence link, validation step, and error-handling rule.",
		"Missing evidence is unavailable, never zero.",
		"Retries and repeated ranges are not independent samples.",
		"keep launch blocked",
	} {
		if !strings.Contains(parts[2], token) {
			t.Errorf("missing campaign prompt/protocol: %q", token)
		}
	}
	if strings.Contains(parts[2], "..HEAD") || strings.Contains(parts[2], "<SHA> HEAD") {
		t.Error("analysis commands must use END_SHA, not moving HEAD")
	}
}
