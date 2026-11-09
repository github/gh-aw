package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func parseSkipIfDecision(frontmatter map[string]any) ([]string, error) {
	raw, exists := frontmatter["skip-if-decision"]
	if !exists {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) > 64 {
		return nil, errors.New("skip-if-decision must be a list of at most 64 questions. Example:\nskip-if-decision:\n  - Is there no actionable work remaining in @context/task.md?")
	}
	questions := make([]string, 0, len(items))
	for index, item := range items {
		question, ok := item.(string)
		if !ok || strings.TrimSpace(question) == "" {
			return nil, fmt.Errorf("skip-if-decision question %d must be a non-empty string", index+1)
		}
		if strings.Contains(question, "${{") {
			return nil, fmt.Errorf("skip-if-decision question %d cannot contain Actions expressions: use runtime @file references for context", index+1)
		}
		questions = append(questions, question)
	}
	return questions, nil
}

func isSkipIfDecisionEnabled(data *WorkflowData) bool {
	return data != nil && !data.IsDetectionRun && !data.IsEvalsRun && len(data.SkipIfDecision) > 0
}

func validateSkipIfDecisionEngine(engine CodingAgentEngine, data *WorkflowData) error {
	if data == nil || len(data.SkipIfDecision) == 0 {
		return nil
	}
	behaviorEngine, ok := engine.(*BehaviorDefinedEngine)
	if ok && behaviorEngine.protectedSessionCaptureEnabled() {
		return errors.New("skip-if-decision is incompatible with protected-session-capture: the decision wrapper retains a capture descriptor and skipped invocations have no verified protected session; remove skip-if-decision or use an engine without protected-session-capture")
	}
	return nil
}

func wrapCustomHarnessDecisionCommand(data *WorkflowData, engineName, defaultHarness, command string) string {
	if isSkipIfDecisionEnabled(data) && data.EngineConfig != nil &&
		data.EngineConfig.HarnessScript != "" && data.EngineConfig.HarnessScript != defaultHarness {
		return buildShellHarnessCommand(engineName, command)
	}
	return command
}

func applySkipIfDecisionEnv(env map[string]string, data *WorkflowData) {
	if !isSkipIfDecisionEnabled(data) {
		return
	}
	// The questions remain data, not shell source or an AWF host-side step.
	encoded, err := json.Marshal(data.SkipIfDecision)
	if err != nil {
		panic(fmt.Sprintf("BUG: failed to marshal skip-if-decision questions: %v", err))
	}
	env["GH_AW_SKIP_IF_DECISION"] = string(encoded)
	env["AWF_REFLECT_ENABLED"] = "1"
}
