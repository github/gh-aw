// This file validates that agent-job step outputs are not referenced in the
// workflow prompt (markdown body). The prompt is built in the activation job;
// agent-job steps run after the activation job and their outputs are therefore
// unavailable when the prompt is assembled. Steps that need to pass data into
// the prompt should write results to files instead.

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var stepsOutputInPromptLog = logger.New("workflow:steps_output_in_prompt_validation")

// validateStepsOutputsNotInPrompt checks that no agent-job step outputs are
// referenced in the prompt body (MarkdownContent). The prompt is rendered in
// the activation job; steps defined in the frontmatter steps: / pre-steps: /
// pre-agent-steps: / post-steps: sections run inside the agent job (after
// activation) and their outputs are not available at prompt-creation time.
//
// Use files (e.g. /tmp/gh-aw/agent/result.txt) to pass data from agent-job
// steps into the prompt instead of step outputs.
func validateStepsOutputsNotInPrompt(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.MarkdownContent == "" {
		return nil
	}
	if !strings.Contains(workflowData.MarkdownContent, "${{") {
		return nil
	}

	agentStepIDs := extractAgentJobStepIDs(workflowData)
	if len(agentStepIDs) == 0 {
		stepsOutputInPromptLog.Print("No agent-job step IDs found; skipping prompt step-output check")
		return nil
	}

	seen := make(map[string]struct{})
	var offending []string
	for _, path := range interpolationReferences(workflowData.MarkdownContent) {
		if len(path) < 2 || !strings.EqualFold(path[0], "steps") {
			continue
		}
		stepID := path[1]
		if _, isAgentStep := agentStepIDs[stepID]; isAgentStep {
			if _, already := seen[stepID]; !already {
				offending = append(offending, stepID)
				seen[stepID] = struct{}{}
			}
		}
	}

	if len(offending) == 0 {
		stepsOutputInPromptLog.Print("No agent-job step outputs found in prompt")
		return nil
	}

	stepsOutputInPromptLog.Printf("Found %d agent-job step output(s) in prompt: %v", len(offending), offending)

	return NewValidationError(
		"steps-output-in-prompt",
		"steps referenced in prompt: "+strings.Join(offending, ", "),
		fmt.Sprintf(
			"step(s) %s are defined in the agent job and run after the prompt is rendered in the activation job; "+
				"their outputs are not available at prompt-creation time",
			strings.Join(offending, ", "),
		),
		"Write step results to a file (e.g. /tmp/gh-aw/agent/result.txt) and reference that file path in the prompt instead of using ${{ steps.STEP_ID.outputs.* }}.",
	)
}

// extractAgentJobStepIDs returns the set of step IDs defined across all
// agent-job step sections: steps:, pre-steps:, pre-agent-steps:, post-steps:.
func extractAgentJobStepIDs(workflowData *WorkflowData) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, stepsYAML := range []string{
		workflowData.CustomSteps,
		workflowData.PreSteps,
		workflowData.PreAgentSteps,
		workflowData.PostSteps,
	} {
		if stepsYAML == "" {
			continue
		}
		addStepIDsFromYAML(stepsYAML, ids)
	}
	return ids
}

// addStepIDsFromYAML parses a YAML string that wraps a steps list under a
// top-level key (e.g. "steps:", "pre-steps:") and collects each step's id.
func addStepIDsFromYAML(stepsYAML string, ids map[string]struct{}) {
	var wrapper map[string]any
	if err := yaml.Unmarshal([]byte(stepsYAML), &wrapper); err != nil {
		stepsOutputInPromptLog.Printf("Failed to parse steps YAML: %v", err)
		return
	}
	// The root key varies ("steps", "pre-steps", "pre-agent-steps", "post-steps"),
	// so iterate over all top-level values and look for step lists.
	for _, value := range wrapper {
		steps, ok := value.([]any)
		if !ok {
			continue
		}
		for _, step := range steps {
			stepMap, ok := step.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := stepMap["id"].(string); ok && id != "" {
				ids[id] = struct{}{}
			}
		}
	}
}
