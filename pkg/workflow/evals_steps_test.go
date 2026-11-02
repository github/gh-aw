//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalsTestData() *WorkflowData {
	return &WorkflowData{Evals: &EvalsConfig{Questions: []EvalDefinition{
		{ID: "builds", Question: "Does it build?"},
		{ID: "tests", Question: "Did tests pass?"},
	}}}
}

func TestEvalsDecisionsIndependentOfAgentEngine(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "pi", "custom"} {
		t.Run(engine, func(t *testing.T) {
			data := evalsTestData()
			data.AI = engine
			data.Model = "unrelated-agent-model"
			data.EngineConfig = &EngineConfig{ID: engine, Command: "custom-command", HarnessScript: "custom.cjs", Agent: "custom-agent"}
			steps := strings.Join(NewCompiler().buildEvalsJobSteps(data), "")
			assert.Contains(t, steps, "name: Execute BinEval Decisions")
			assert.Contains(t, steps, "id: evals_decisions_execution")
			assert.Contains(t, steps, `GH_AW_EVALS_MODEL: "gpt-6-luna"`)
			assert.Contains(t, steps, "GH_AW_DECISIONS_BASE_URL: https://api.githubcopilot.com/v1\n")
			assert.Contains(t, steps, "GH_AW_DECISIONS_API_KEY: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
			assert.Contains(t, steps, "await executeMain();")
			for _, excluded := range []string{"copilot_harness", "claude_harness", "codex_harness", "custom-command", "custom-agent", "custom.cjs", "unrelated-agent-model", "Install GitHub Copilot", "Install Claude", "Install Codex", "Install AWF", "Setup Node.js", "Download container images", "firewall", "api-proxy", "--env-all", "GH_AW_MAX_AI_CREDITS"} {
				assert.NotContains(t, steps, excluded)
			}
			assert.Equal(t, "custom-command", data.EngineConfig.Command)
		})
	}
}

func TestEvalsDecisionsQuestionsAreYAMLData(t *testing.T) {
	data := evalsTestData()
	data.Evals.Questions = []EvalDefinition{{ID: `id'"\\;$(touch /tmp/pwned)`, Question: "line one\nline two; `echo unsafe`"}}
	var steps []struct {
		Env map[string]string `yaml:"env"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(NewCompiler().buildEvalsDecisionSteps(data), "")), &steps))
	require.Len(t, steps, 1)
	var decoded []struct{ ID, Question string }
	require.NoError(t, json.Unmarshal([]byte(steps[0].Env["GH_AW_EVALS_QUESTIONS"]), &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, data.Evals.Questions[0].ID, decoded[0].ID)
	assert.Equal(t, data.Evals.Questions[0].Question, decoded[0].Question)
}

func TestEvalsDecisionsModelAndProvider(t *testing.T) {
	compiler := NewCompiler()
	data := evalsTestData()
	data.Model = "claude-sonnet-4.6"
	assert.Equal(t, "gpt-6-luna", compiler.resolveEvalsExecutionModel(data))
	t.Setenv(compilerenv.DefaultEvalsModel, "copilot/decision-model")
	assert.Equal(t, "copilot/decision-model", compiler.resolveEvalsExecutionModel(data))
	data.Evals.Model = "openai/gpt-6-luna"
	steps := strings.Join(compiler.buildEvalsDecisionSteps(data), "")
	assert.Contains(t, steps, "GH_AW_DECISIONS_BASE_URL: https://api.openai.com/v1")
	assert.Contains(t, steps, "GH_AW_DECISIONS_PROVIDER: openai")
	assert.Contains(t, steps, "GH_AW_DECISIONS_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}")
	assert.NotContains(t, steps, "COPILOT_GITHUB_TOKEN")
}

func TestEvalsDecisionsIndependentOfSandboxTopology(t *testing.T) {
	compiler := NewCompiler()
	data := evalsTestData()
	expected := strings.Join(compiler.buildEvalsDecisionSteps(data), "")
	data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
	data.Runtimes = map[string]any{"node": map[string]any{"version": "24.21.0"}}
	assert.Equal(t, expected, strings.Join(compiler.buildEvalsDecisionSteps(data), ""))
}

func TestEvalsDecisionsFailureAndPublicationContract(t *testing.T) {
	data := evalsTestData()
	data.Evals.Model = "${{ inputs.model }}"
	steps := strings.Join(NewCompiler().buildEvalsJobSteps(data), "")
	assert.Contains(t, steps, "GH_AW_EVALS_MODEL: ${{ inputs.model }}")
	assert.Contains(t, steps, "GH_AW_MODEL_FALLBACK: ${{ vars.GH_AW_DEFAULT_EVALS_MODEL || 'gpt-6-luna' }}")
	assert.Contains(t, steps, "GH_AW_EVALS_EXECUTION_OUTCOME: ${{ steps.evals_decisions_execution.outcome }}")
	assert.NotContains(t, steps, "steps.evals_decisions_execution.conclusion")
	require.Greater(t, strings.Index(steps, "id: evals_decisions_execution"), strings.Index(steps, "name: Initialize evals execution evidence"))
	redact := strings.Index(steps, "name: Redact secrets")
	render := strings.Index(steps, "name: Render evals results")
	upload := strings.Index(steps, "name: Upload evals results")
	require.Greater(t, render, redact)
	require.Greater(t, upload, render)
	assert.Equal(t, 2, strings.Count(steps, "if: always() && steps.redact_evals_results.outcome == 'success'"))
	for _, expected := range []string{evalsExecutionEvidencePath, evalsResultsPath, evalsStatusPath, "/tmp/gh-aw/evals_token_usage.jsonl", "name: Upload evals accounting after failure"} {
		assert.Contains(t, steps, expected)
	}
}

func TestEvalsDecisionsSecretRedaction(t *testing.T) {
	data := evalsTestData()
	data.Evals.Model = "${{ secrets.EVAL_MODEL }}"
	data.Evals.Questions[0].Question = "Does it use ${{ secrets.EVAL_PROMPT }}?"
	redaction := strings.Join(NewCompiler().buildRedactEvalsSecretsStep(data), "")
	assert.Contains(t, redaction, "SECRET_EVAL_MODEL: ${{ secrets.EVAL_MODEL }}")
	assert.Contains(t, redaction, "SECRET_EVAL_PROMPT: ${{ secrets.EVAL_PROMPT }}")
}

func TestEvalsDecisionsPermissions(t *testing.T) {
	data := evalsTestData()
	data.Permissions = "permissions:\n  contents: read\n  copilot-requests: write"
	compiler := NewCompiler()
	_, permissions := compiler.buildEvalsJobPermissions(data)
	assert.Contains(t, permissions, "copilot-requests: write")
	assert.Contains(t, strings.Join(compiler.buildEvalsDecisionSteps(data), ""), "GH_AW_DECISIONS_API_KEY: ${{ github.token }}")
	data.Evals.Model = "openai/gpt-6-luna"
	_, permissions = compiler.buildEvalsJobPermissions(data)
	assert.NotContains(t, permissions, "copilot-requests")
	assert.Contains(t, permissions, "contents: read")
	assert.NotContains(t, permissions, "id-token")
}

func compileClaudeEvalsWorkflow(t *testing.T, engineYAML string) (string, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "claude-evals.md")
	source := "---\non: workflow_dispatch\npermissions:\n  contents: read\n" + engineYAML +
		"safe-outputs:\n  add-comment:\nevals:\n  - id: docs\n    question: Were the docs reviewed?\n---\nReview documentation.\n"
	require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
	require.NoError(t, err)
	return extractJobSection(string(content), "agent"), extractJobSection(string(content), "evals")
}

func TestEvalsDecisionsCompiledJob(t *testing.T) {
	agent, evals := compileClaudeEvalsWorkflow(t, "engine:\n  id: claude\n")
	assert.Contains(t, agent, "claude_harness.cjs")
	assert.Contains(t, evals, "Execute BinEval Decisions")
	assert.Contains(t, evals, "run_evals.cjs")
	assert.Contains(t, evals, `GH_AW_EVALS_MODEL: "gpt-6-luna"`)
	for _, excluded := range []string{"claude_harness.cjs", "copilot_harness.cjs", "Install AWF", "Parse MCP Gateway logs", "firewall", "api-proxy"} {
		assert.NotContains(t, evals, excluded)
	}
	assert.Contains(t, evals, "id: parse_evals_token_usage")
	assert.Contains(t, evals, "${{ steps.parse_evals_token_usage.outputs.aic }}")
	assert.Contains(t, evals, "summarizeUsageMain")
}

func TestDailyAICEvalsAccountingTransport(t *testing.T) {
	steps := strings.Join(NewCompiler().buildUploadEvalsArtifactStep(&WorkflowData{}), "")
	for _, expected := range []string{
		"name: Parse evals token usage", "if: always()", "/tmp/gh-aw/evals_token_usage.jsonl",
		evalsExecutionEvidencePath, evalsResultsPath, evalsStatusPath,
		"if: always() && steps.redact_evals_results.outcome == 'success'",
		"name: Upload evals accounting after failure", "if: always() && steps.redact_evals_results.outcome != 'success'",
	} {
		assert.Contains(t, steps, expected)
	}
	usage := strings.Join(buildUsageArtifactUploadSteps("", true, "", false, func(action string) string { return action }), "")
	for _, expected := range []string{"/tmp/gh-aw/usage/evals/token_usage.jsonl", "/tmp/gh-aw/usage/evals/execution.json", "/tmp/gh-aw/usage/evals/evals_status.json"} {
		assert.Contains(t, usage, expected)
	}
	script, err := os.ReadFile("../../actions/setup/sh/collect_usage_artifact_files.sh")
	require.NoError(t, err)
	assert.Contains(t, string(script), "cp /tmp/gh-aw/evals/evals_token_usage.jsonl /tmp/gh-aw/usage/evals/token_usage.jsonl")
	assert.Contains(t, string(script), "cp /tmp/gh-aw/evals/execution.json /tmp/gh-aw/usage/evals/execution.json")
	assert.Contains(t, string(script), "cp /tmp/gh-aw/evals/evals_status.json /tmp/gh-aw/usage/evals/evals_status.json")
}
