package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var awContextLog = logger.New("workflow:compiler_aw_context")

// AwContextInputName is the name of the internal aw_context workflow_dispatch input.
// It is managed internally by the agentic workflow system and should not be surfaced to users.
const AwContextInputName = "aw_context"

// WorkQueueClaimInputName is the internal workflow_dispatch input that carries
// a trusted work queue assignment to queue-enabled workers.
const WorkQueueClaimInputName = "work_queue_claim"

// NetworkAllowedInputName is the optional workflow_call input that extends the compiled
// network allowlist at runtime for reusable workflows.
const NetworkAllowedInputName = "network_allowed"

// awContextInputDescription is the description for the aw_context workflow_dispatch input.
// It signals to users that this input is managed internally by the agentic workflow system.
const awContextInputDescription = "Agent caller context (Reserved for Agentic Workflows)."

const networkAllowedInputDescription = "Additional allowed network domains or ecosystem identifiers to union with network.allowed (comma-separated, for example: \"rust\" or \"python,github.com\")."

// injectAwContextIntoOnYAML adds the aw_context input to internal workflow triggers
// in the given on-section YAML string.
//
// The injection is string-based to preserve existing YAML comments and formatting.
// It handles these triggers independently:
//   - workflow_dispatch
//   - workflow_call
//
// For each trigger it supports two cases:
//   - Bare trigger line (no sub-keys): adds an inputs: block with aw_context
//   - Trigger with an existing inputs: sub-key: adds aw_context inside inputs
//
// The function is idempotent: calling it twice produces the same result.
func injectAwContextIntoOnYAML(onSection string) string {
	updated := injectInputIntoTrigger(onSection, "workflow_dispatch", AwContextInputName, buildAwContextInputLines)
	updated = injectInputIntoTrigger(updated, "workflow_call", AwContextInputName, buildAwContextInputLines)
	return updated
}

func injectWorkQueueClaimIntoOnYAML(onSection string) string {
	return injectInputIntoTrigger(onSection, "workflow_dispatch", WorkQueueClaimInputName, buildWorkQueueClaimInputLines)
}

func validateReservedWorkflowInputs(data *WorkflowData) error {
	if data == nil || data.RawFrontmatter == nil {
		return nil
	}
	on, ok := data.RawFrontmatter["on"].(map[string]any)
	if !ok {
		return nil
	}

	for _, trigger := range []string{"workflow_dispatch", "workflow_call"} {
		triggerConfig, ok := on[trigger].(map[string]any)
		if !ok {
			continue
		}
		inputs, ok := triggerConfig["inputs"].(map[string]any)
		if !ok {
			continue
		}
		for _, inputName := range []string{AwContextInputName, WorkQueueClaimInputName} {
			if _, exists := inputs[inputName]; exists {
				return fmt.Errorf("on.%s.inputs.%s is reserved and managed by the compiler; remove it from workflow inputs", trigger, inputName)
			}
		}
	}
	return nil
}

func injectNetworkAllowedIntoOnYAML(onSection string, network *NetworkPermissions) string {
	if network == nil || !network.AllowedInput {
		return onSection
	}
	return injectInputIntoTrigger(onSection, "workflow_call", NetworkAllowedInputName, buildNetworkAllowedInputLines)
}

func injectInputIntoTrigger(onSection string, triggerName string, inputName string, buildInputLines func(int) []string) string {
	if !strings.Contains(onSection, triggerName) {
		awContextLog.Printf("No %s trigger found, skipping %s injection", triggerName, inputName)
		return onSection
	}
	awContextLog.Printf("Injecting %s input into %s trigger", inputName, triggerName)

	onSection = expandInlineOnTriggers(onSection)
	lines := strings.Split(onSection, "\n")
	triggerLineIdx, triggerIndent := findBareTriggerLine(lines, triggerName)

	if triggerLineIdx == -1 {
		awContextLog.Printf("No bare %s: line found, skipping %s injection", triggerName, inputName)
		return onSection
	}
	awContextLog.Printf("Found %s at line %d (indent=%d), injecting %s", triggerName, triggerLineIdx, triggerIndent, inputName)

	inputsLineIdx := findTriggerInputsLine(lines, triggerLineIdx, triggerIndent)
	if triggerAlreadyHasInput(lines, inputsLineIdx, inputName) {
		awContextLog.Printf("%s already injected into %s, skipping", inputName, triggerName)
		return onSection
	}

	return insertTriggerInputLines(lines, triggerLineIdx, triggerIndent, inputsLineIdx, buildInputLines(triggerIndent))
}

func expandInlineOnTriggers(onSection string) string {
	lines := strings.Split(onSection, "\n")
	for i, line := range lines {
		if strings.TrimLeft(line, " \t") != line {
			continue
		}
		onKey := ""
		for _, candidate := range []string{"on:", `"on":`, "'on':"} {
			if strings.HasPrefix(line, candidate) {
				onKey = candidate
				break
			}
		}
		if onKey == "" {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, onKey))
		if value == "" || value == "null" || value == "~" || strings.HasPrefix(value, "#") {
			continue
		}

		var events []string
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			for event := range strings.SplitSeq(strings.TrimSpace(value[1:len(value)-1]), ",") {
				events = append(events, strings.Trim(strings.TrimSpace(event), "'\""))
			}
		} else {
			events = []string{strings.Trim(value, "'\"")}
		}
		if len(events) == 0 {
			continue
		}

		expanded := []string{onKey}
		for _, event := range events {
			if event == "" || strings.ContainsAny(event, " \t{}[]:#") {
				expanded = nil
				break
			}
			expanded = append(expanded, "  "+event+":")
		}
		if expanded != nil {
			lines = append(lines[:i], append(expanded, lines[i+1:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	return onSection
}

func findBareTriggerLine(lines []string, triggerName string) (int, int) {
	for i, line := range lines {
		stripped := strings.TrimLeft(line, " \t")
		rest, found := strings.CutPrefix(stripped, triggerName+":")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || rest == "null" || rest == "~" {
			return i, len(line) - len(stripped)
		}
	}
	return -1, 0
}

func findTriggerInputsLine(lines []string, triggerLineIdx, triggerIndent int) int {
	for i, line := range lines {
		if i <= triggerLineIdx {
			continue
		}
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		if len(line)-len(stripped) <= triggerIndent {
			break
		}
		if strings.HasPrefix(stripped, "inputs:") {
			return i
		}
		break
	}
	return -1
}

func triggerAlreadyHasInput(lines []string, inputsLineIdx int, inputName string) bool {
	if inputsLineIdx < 0 || inputsLineIdx >= len(lines) {
		return false
	}

	inputsIndent := -1
	for i, line := range lines {
		if i == inputsLineIdx {
			stripped := strings.TrimLeft(line, " \t")
			inputsIndent = len(line) - len(stripped)
			continue
		}
		if i <= inputsLineIdx || inputsIndent < 0 {
			continue
		}
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		if len(line)-len(stripped) <= inputsIndent {
			break
		}
		if strings.HasPrefix(stripped, inputName+":") {
			return true
		}
	}
	return false
}

func insertTriggerInputLines(lines []string, triggerLineIdx, triggerIndent, inputsLineIdx int, inputLines []string) string {
	result := make([]string, 0, typeutil.SafeAllocationCapacity(len(lines), len(inputLines), 1))
	for i, line := range lines {
		// When the trigger line contains an explicit null/~ value,
		// replace it with a bare trigger so sub-keys can follow.
		if i == triggerLineIdx && (strings.HasSuffix(strings.TrimSpace(line), " null") ||
			strings.HasSuffix(strings.TrimSpace(line), " ~")) {
			stripped := strings.TrimLeft(line, " \t")
			triggerKey, _, _ := strings.Cut(stripped, ":")
			line = strings.Repeat(" ", triggerIndent) + triggerKey + ":"
		}
		result = append(result, line)

		if inputsLineIdx != -1 && i == inputsLineIdx {
			result = append(result, inputLines...)
		} else if inputsLineIdx == -1 && i == triggerLineIdx {
			// Trigger is bare — add inputs: + the requested internal input.
			result = append(result, strings.Repeat(" ", triggerIndent+2)+"inputs:")
			result = append(result, inputLines...)
		}
	}

	return strings.Join(result, "\n")
}

// buildAwContextInputLines returns the indented YAML lines for the aw_context input
// definition, sized relative to the workflow_dispatch: line's indentation.
func buildAwContextInputLines(wdIndent int) []string {
	awIndent := strings.Repeat(" ", wdIndent+4)   // under inputs:
	propIndent := strings.Repeat(" ", wdIndent+6) // properties of aw_context
	return []string{
		awIndent + AwContextInputName + ":",
		propIndent + "default: \"\"",
		propIndent + "description: " + strconv.Quote(awContextInputDescription),
		propIndent + "required: false",
		propIndent + "type: string",
	}
}

func buildWorkQueueClaimInputLines(wdIndent int) []string {
	inputIndent := strings.Repeat(" ", wdIndent+4)
	propIndent := strings.Repeat(" ", wdIndent+6)
	return []string{
		inputIndent + WorkQueueClaimInputName + ":",
		propIndent + `default: ""`,
		propIndent + `description: "Trusted work queue assignment (Reserved for Agentic Workflows)."`,
		propIndent + "required: false",
		propIndent + "type: string",
	}
}

func buildNetworkAllowedInputLines(wdIndent int) []string {
	inputIndent := strings.Repeat(" ", wdIndent+4)
	propIndent := strings.Repeat(" ", wdIndent+6)
	return []string{
		inputIndent + NetworkAllowedInputName + ":",
		propIndent + "default: \"\"",
		propIndent + "description: " + strconv.Quote(networkAllowedInputDescription),
		propIndent + "required: false",
		propIndent + "type: string",
	}
}
