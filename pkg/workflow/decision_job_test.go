package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionJobRunsAfterResolvedAdmission(t *testing.T) {
	for _, admission := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "admission"}[admission], func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "decision.md")
			admissionField := ""
			if admission {
				admissionField = "  admission: true\n"
			}
			source := "---\non:\n  workflow_dispatch:\n  roles: all\n" + admissionField +
				"  decision:\n    questions: [Run?]\nengine: copilot\nmax-ai-credits: 12\n---\nPerform the work.\n"
			require.NoError(t, os.WriteFile(file, []byte(source), 0600))
			compiler := NewCompiler()
			compiler.repoConfig = &RepoConfig{Admission: admission}
			compiler.repoConfigLoaded = true
			require.NoError(t, compiler.CompileWorkflow(file))
			lock, err := os.ReadFile(filepath.Join(dir, "decision.lock.yml"))
			require.NoError(t, err)
			var document struct {
				Jobs map[string]struct {
					Needs   any               `yaml:"needs"`
					If      string            `yaml:"if"`
					Env     map[string]string `yaml:"env"`
					Outputs map[string]string `yaml:"outputs"`
					Steps   []map[string]any  `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &document))
			decision := document.Jobs["decision"]
			needs, err := yaml.Marshal(decision.Needs)
			require.NoError(t, err)
			assert.Contains(t, string(needs), "pre_activation")
			assert.Equal(t, "12", decision.Env["GH_AW_MAX_AI_CREDITS"])
			assert.Equal(t, admission, strings.Contains(string(needs), "admission"))
			if admission {
				assert.Equal(t, "${{ needs.admission.outputs.max-ai-credits }}", decision.Env["GH_AW_ADMISSION_MAX_AI_CREDITS"])
			}
			activation := document.Jobs["activation"]
			needs, err = yaml.Marshal(activation.Needs)
			require.NoError(t, err)
			assert.Contains(t, string(needs), "decision")
			assert.Contains(t, activation.If, "needs.decision.result == 'success'")
			assert.Contains(t, activation.If, "needs.decision.outputs.remaining-ai-credits != '0'")
			assert.Equal(t, "${{ needs.decision.outputs.remaining-ai-credits }}", activation.Outputs["decision-remaining-ai-credits"])
			assert.NotContains(t, document.Jobs["pre_activation"].Outputs["activated"], "decision_gate")
			require.NoError(t, actionslanguageservice.ValidateWorkflow(file, string(lock)))
		})
	}
}

func TestDecisionQuestionStepInputsAreForwarded(t *testing.T) {
	data := &WorkflowData{Decision: &DecisionConfig{Questions: []string{"Run for ${{ steps.check.outputs.value }}?", "${{ steps.check.outcome == 'success' }}"}}}
	questions, outputs := decisionQuestionInputs(data)
	assert.Equal(t, "Run for ${{ needs.pre_activation.outputs.decision-input-0 }}?", questions[0])
	assert.Equal(t, "${{ needs.pre_activation.outputs.decision-input-1 == 'success' }}", questions[1])
	assert.Equal(t, "${{ steps['check']['outputs']['value'] }}", outputs["decision-input-0"])
	assert.Equal(t, "${{ steps['check']['outcome'] }}", outputs["decision-input-1"])
	assert.Contains(t, data.Decision.Questions[0], "steps.check")
}

func TestDecisionRemainingBudgetOverridesAgentAllowance(t *testing.T) {
	data := &WorkflowData{Decision: &DecisionConfig{}, EngineConfig: &EngineConfig{MaxAICredits: 1000}}
	script, _ := buildMaxAICreditsExport(AWFCommandConfig{WorkflowData: data}, `{"apiProxy":{"maxRuns":1}}`)
	for _, remainder := range []string{"8", "0", "", "-1", "1.5"} {
		t.Run("remainder="+remainder, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", script+"\nprintf '%s' \"$GH_AW_MAX_AI_CREDITS\"")
			cmd.Env = append(os.Environ(), "GH_AW_ADMISSION_RESULT=admitted", "GH_AW_ADMISSION_MAX_AI_CREDITS=12", "GH_AW_DECISION_REMAINING_AI_CREDITS="+remainder)
			output, err := cmd.CombinedOutput()
			if remainder == "8" {
				require.NoError(t, err, string(output))
				assert.True(t, strings.HasSuffix(string(output), "8"), string(output))
			} else {
				require.Error(t, err)
				assert.Contains(t, string(output), "ERR_DECISION")
			}
		})
	}
}
