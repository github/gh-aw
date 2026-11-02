package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

const (
	evalsDir              = "/tmp/gh-aw/evals"
	evalsResultsPath      = "/tmp/gh-aw/" + string(constants.EvalsResultFilename)
	evalsStatusPath       = "/tmp/gh-aw/" + string(constants.EvalsStatusFilename)
	defaultDecisionsModel = "copilot/gpt-6-luna-decisions"
)

// Evals use a standalone Decisions client, not an agent engine or MCP tools.
func (c *Compiler) buildEvalsJobSteps(data *WorkflowData) []string {
	if !data.Evals.HasEvals() {
		return nil
	}
	steps := generateComponentExecutionEvidenceStep("evals", "not_started", evalsExecutionEvidencePath, "always()")
	steps = append(steps, buildPrepareEvalsFilesStep()...)
	steps = append(steps, c.buildEvalsDecisionSteps(data)...)
	steps = append(steps, c.buildParseEvalsResultsStep(data)...)
	steps = append(steps, c.buildRedactEvalsSecretsStep(data)...)
	steps = append(steps, c.buildRenderEvalsSummaryStep(data)...)
	return append(steps, c.buildUploadEvalsArtifactStep(data)...)
}

func buildPrepareEvalsFilesStep() []string {
	return []string{
		"      - name: Prepare evals files\n",
		"        run: |\n",
		"          mkdir -p " + evalsDir + "\n",
		"          if [ -f /tmp/gh-aw/agent_output.json ]; then cp /tmp/gh-aw/agent_output.json " + evalsDir + "/agent_output.json; fi\n",
	}
}

// Only the selected Decisions provider determines credentials.
// The main agent's command, model, harness, tools, and routing are not inherited.
func (c *Compiler) buildEvalsDecisionData(data *WorkflowData) *WorkflowData {
	providerID := c.getEvalsProviderID(data)
	result := &WorkflowData{
		AI:                providerID,
		Model:             c.resolveEvalsExecutionModel(data),
		EngineConfig:      &EngineConfig{ID: providerID},
		Permissions:       data.Permissions,
		CachedPermissions: data.CachedPermissions,
		IsEvalsRun:        true,
	}
	inheritTokenBrokerConfig(data, result)
	return result
}

func (c *Compiler) buildEvalsDecisionSteps(data *WorkflowData) []string {
	evalsData := c.buildEvalsDecisionData(data)
	var questions []EvalDefinition
	if data.Evals != nil {
		questions = data.Evals.Questions
	}
	steps := c.buildTokenBrokerRequestStep(evalsData, "")
	baseURL := "https://api.githubcopilot.com/v1"
	apiKey := "${{ secrets.COPILOT_GITHUB_TOKEN }}"
	provider := "github-copilot"
	if evalsData.AI == "codex" {
		baseURL = "https://api.openai.com/v1"
		apiKey = "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}"
		provider = "openai"
	} else if usesTokenBrokerInference(evalsData) {
		apiKey = tokenBrokerCopilotRequestOutput
	} else if hasCopilotRequestsWritePermission(evalsData) {
		apiKey = "${{ github.token }}"
	}
	steps = append(steps,
		"      - name: Execute BinEval Decisions\n",
		"        id: evals_decisions_execution\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		"          GH_AW_DECISIONS_BASE_URL: "+baseURL+"\n",
		"          GH_AW_DECISIONS_API_KEY: "+apiKey+"\n",
		"          GH_AW_DECISIONS_PROVIDER: "+provider+"\n",
		fmt.Sprintf("          GH_AW_EVALS_QUESTIONS: '%s'\n", escapeYAMLSingleQuoted(marshalEvalsQuestions(questions))),
	)
	steps = appendEvalsModelEnvLines(steps, c.resolveEvalsExecutionModel(data))
	return append(steps,
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { executeMain } = require('"+SetupActionDestination+"/run_evals.cjs');\n",
		"            await executeMain();\n",
	)
}

func (c *Compiler) buildParseEvalsResultsStep(data *WorkflowData) []string {
	if data.Evals == nil {
		return nil
	}
	steps := []string{
		"      - name: Parse BinEval results\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_EVALS_QUESTIONS: '%s'\n", escapeYAMLSingleQuoted(marshalEvalsQuestions(data.Evals.Questions))),
	}
	steps = appendEvalsModelEnvLines(steps, c.resolveEvalsExecutionModel(data))
	steps = append(steps,
		"          GH_AW_EVALS_PHASE: parse\n",
		"          GH_AW_EVALS_EXECUTION_OUTCOME: ${{ steps.evals_decisions_execution.outcome }}\n",
		"          GITHUB_RUN_ID: ${{ github.run_id }}\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('"+SetupActionDestination+"/run_evals.cjs');\n",
		"            await main();\n",
	)
	return steps
}

func (c *Compiler) buildRedactEvalsSecretsStep(data *WorkflowData) []string {
	steps := []string{
		"      - name: Redact secrets in evals results\n",
		"        id: redact_evals_results\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
	}
	refs := c.collectEvalsSecretReferences(data)
	if len(refs) > 0 {
		steps = append(steps, "        env:\n")
		escaped := make([]string, 0, len(refs))
		for _, ref := range refs {
			escaped = append(escaped, escapeSingleQuoteBackslash(ref))
		}
		steps = append(steps, fmt.Sprintf("          GH_AW_SECRET_NAMES: '%s'\n", strings.Join(escaped, ",")))
		for _, ref := range refs {
			ref = escapeSingleQuoteBackslash(ref)
			steps = append(steps, fmt.Sprintf("          SECRET_%s: ${{ secrets.%s }}\n", ref, ref))
		}
	}
	return append(steps,
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('"+SetupActionDestination+"/redact_evals_results.cjs');\n",
		"            await main();\n",
	)
}

func (c *Compiler) buildRenderEvalsSummaryStep(data *WorkflowData) []string {
	return []string{
		"      - name: Render evals results to step summary\n",
		"        if: always() && steps.redact_evals_results.outcome == 'success'\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { main } = require('" + SetupActionDestination + "/render_evals_summary.cjs');\n",
		"            await main();\n",
	}
}

func (c *Compiler) buildUploadEvalsArtifactStep(data *WorkflowData) []string {
	name := artifactPrefixExprForDownstreamJob(data) + constants.EvalsArtifactName.String()
	steps := []string{
		"      - name: Parse evals token usage for step summary\n",
		"        id: parse_evals_token_usage\n",
		"        if: always()\n",
		"        continue-on-error: true\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            const { summarizeUsageMain } = require('" + SetupActionDestination + "/run_evals.cjs');\n",
		"            await summarizeUsageMain();\n",
	}
	return append(steps,
		"      - name: Upload evals results\n",
		"        if: always() && steps.redact_evals_results.outcome == 'success'\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: "+name+"\n",
		"          path: |\n",
		"            "+evalsResultsPath+"\n",
		"            "+evalsStatusPath+"\n",
		"            /tmp/gh-aw/evals_token_usage.jsonl\n",
		"            "+evalsExecutionEvidencePath+"\n",
		"          if-no-files-found: ignore\n",
		"      - name: Upload evals accounting after failure\n",
		"        if: always() && steps.redact_evals_results.outcome != 'success'\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: "+name+"\n",
		"          path: |\n",
		"            /tmp/gh-aw/evals_token_usage.jsonl\n",
		"            "+evalsExecutionEvidencePath+"\n",
		"          if-no-files-found: ignore\n",
	)
}

func (c *Compiler) getEvalsProviderID(data *WorkflowData) string {
	model := c.resolveEvalsExecutionModel(data)
	if strings.HasPrefix(model, "openai/") || strings.HasPrefix(model, "codex/") {
		return "codex"
	}
	return "copilot"
}

func (c *Compiler) resolveEvalsExecutionModel(data *WorkflowData) string {
	if data.Evals != nil && data.Evals.Model != "" {
		return data.Evals.Model
	}
	return compilerenv.ResolveDefaultEvalsModel(defaultDecisionsModel)
}

func appendEvalsModelEnvLines(steps []string, model string) []string {
	value := fmt.Sprintf("%q", model)
	if containsExpression(model) {
		value = model
	}
	steps = append(steps, fmt.Sprintf("          GH_AW_EVALS_MODEL: %s\n", value))
	if containsExpression(model) {
		steps = append(steps, fmt.Sprintf("          %s: %s\n", constants.EnvVarModelFallback, buildEvalsModelFallbackExpression()))
	}
	return steps
}

func buildEvalsModelFallbackExpression() string {
	return fmt.Sprintf("${{ vars.%s || '%s' }}", compilerenv.DefaultEvalsModel, defaultDecisionsModel)
}

func (c *Compiler) collectEvalsSecretReferences(data *WorkflowData) []string {
	if data.Evals == nil {
		return nil
	}
	return CollectSecretReferences(marshalEvalsQuestions(data.Evals.Questions) + "\n" + c.resolveEvalsExecutionModel(data))
}

func marshalEvalsQuestions(questions []EvalDefinition) string {
	type question struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	}
	result := make([]question, 0, len(questions))
	for _, q := range questions {
		result = append(result, question(q))
	}
	encoded, _ := json.Marshal(result) //nolint:jsonmarshalignoredeerror // marshaling strings cannot fail
	return string(encoded)
}
