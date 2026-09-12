package workflow

import (
	"strings"
)

// generateMainJobSteps generates the complete sequence of steps for the main agent execution job
// This is the heart of the workflow, orchestrating all steps from checkout through AI execution to artifact upload
func (c *Compiler) generateMainJobSteps(yaml *strings.Builder, data *WorkflowData) error {
	compilerYamlLog.Printf("Generating main job steps for workflow: %s", data.Name)

	// Phase 1: Initial setup, checkout, and repository imports
	checkoutMgr, needsCheckout, err := c.generateInitialAndCheckoutSteps(yaml, data)
	if err != nil {
		return err
	}
	compilerYamlLog.Printf("Initial and checkout steps generated (needsCheckout=%v)", needsCheckout)

	for _, line := range generateComponentExecutionEvidenceStep("agent", "not_started", agentExecutionEvidencePath, "") {
		yaml.WriteString(line)
	}

	// Phase 2: Runtime detection, custom steps, and workspace setup
	customStepsContainCheckout := c.generateRuntimeAndWorkspaceSetupSteps(yaml, data, needsCheckout)
	needsGitConfig := needsCheckout || customStepsContainCheckout

	// Phase 3: Engine installation, MCP setup, and pre-agent preparation
	engine, err := c.generateEngineInstallAndPreAgentSteps(yaml, data, needsGitConfig)
	if err != nil {
		return err
	}

	// Pre-warm the allowed domains cache so that engine execution steps (GetExecutionSteps)
	// can reuse the pre-computed value instead of re-running the expensive map+sort
	// operation inside each engine's domain helper.  The result is stored on WorkflowData
	// and is also used later by generateOutputCollectionStep.
	_, _ = c.computeAllowedDomainsForSanitization(data)

	// Phase 4: Agent execution and immediate post-agent steps
	artifactPaths, logFileFull, err := c.generateAgentRunSteps(yaml, data, engine, needsGitConfig)
	if err != nil {
		return err
	}

	// Phase 5: Artifact collection, log parsing, upload, and cleanup
	compilerYamlLog.Printf("Main job steps generation complete for workflow: %s", data.Name)
	return c.generatePostAgentCollectionAndUpload(yaml, data, engine, artifactPaths, logFileFull, checkoutMgr)
}
