package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerMainJobLog = logger.New("workflow:compiler_main_job")

func isBuiltinJobName(jobName string) bool {
	_, isBuiltIn := constants.KnownBuiltInJobNames[jobName]
	return isBuiltIn
}

// buildMainJob creates the main agent job that runs the AI agent with the configured engine and tools.
// This job depends on the activation job if it exists, and handles the main workflow logic.
// The bulk of the construction is delegated to focused helpers in compiler_main_job_helpers.go.
func (c *Compiler) buildMainJob(data *WorkflowData, activationJobCreated bool) (*Job, error) {
	workflowLog.Printf("Building main job for workflow: %s", data.Name)
	var steps []string

	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		compilerMainJobLog.Printf("Adding actions-folder checkout and setup steps (ref=%q, scriptMode=%v)", setupActionRef, c.actionMode.IsScript())
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		agentTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		agentParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("agent", data, setupActionRef, SetupActionDestination, false, agentTraceID, agentParentSpanID, "")...)
	}
	// Set runtime paths that depend on RUNNER_TEMP via $GITHUB_ENV.
	// These cannot be set in job-level env: because the runner context is not
	// available there (only in step-level env: and run: blocks).
	if data.SafeOutputs != nil {
		compilerMainJobLog.Print("Adding runtime-paths step for safe-outputs")
		steps = append(steps, c.generateSetRuntimePathsStep()...)
	}
	jobCondition := c.buildMainJobCondition(data, activationJobCreated)
	depends, engineEnvContent := c.buildMainJobDependencies(data, activationJobCreated)
	c.warnBuiltinJobEnvReferences(depends, engineEnvContent)

	// Build agent step content (checkout app tokens minted here to avoid masked-value drops).
	var stepBuilder strings.Builder
	if err := c.generateMainJobSteps(&stepBuilder, data); err != nil {
		return nil, fmt.Errorf("failed to generate main job steps: %w", err)
	}
	if stepsContent := stepBuilder.String(); stepsContent != "" {
		steps = append(steps, stepsContent)
	}

	outputs := c.buildMainJobOutputs(data)
	env := c.buildMainJobEnv(data)
	agentConcurrency := GenerateJobConcurrencyConfig(data)
	permissions, err := c.buildMainJobPermissions(data)
	if err != nil {
		return nil, err
	}

	if c.actionMode.IsScript() {
		compilerMainJobLog.Print("Adding script-mode cleanup step")
		steps = append(steps, c.generateScriptModeCleanupStep())
	}

	compilerMainJobLog.Printf("Built main job: steps=%d, needs=%v, outputs=%d", len(steps), depends, len(outputs))
	return &Job{
		Name:                     string(constants.AgentJobName),
		If:                       jobCondition,
		RunsOn:                   c.indentYAMLLines(data.RunsOn, "    "),
		Environment:              c.indentYAMLLines(data.Environment, "    "),
		Container:                c.indentYAMLLines(data.Container, "    "),
		Services:                 c.indentYAMLLines(data.Services, "    "),
		Permissions:              c.indentYAMLLines(permissions, "    "),
		Concurrency:              c.indentYAMLLines(agentConcurrency, "    "),
		Env:                      env,
		Steps:                    steps,
		Needs:                    depends,
		Outputs:                  outputs,
		TimeoutMinutesExpression: resolveAgentJobTimeoutValue(data),
	}, nil
}
