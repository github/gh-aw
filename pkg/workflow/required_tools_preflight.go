package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var promptToolOptional = regexp.MustCompile(`(?i)\b(?:if available|when available|if needed)\b`)
var promptToolAlternative = regexp.MustCompile(`(?i)\bor\s+`)

// Required MCP tools use the same conservative instruction scanner as advisory
// checks. Alternatives remain advisory rather than requiring every alternative.
func requiredPromptTools(content string, tools map[string]any) []promptToolRequirement {
	var requirements []promptToolRequirement
	var state promptToolScanState
	known := promptKnownTools()
	for line := range strings.SplitSeq(removeXMLComments(content), "\n") {
		found := state.consumeLine(line, known)
		if promptToolOptional.MatchString(line) || hasPromptToolAlternative(line, known) {
			continue
		}
		for _, requirement := range found {
			_, configured := tools[requirement.server]
			explicit := strings.Contains(line, "mcp__"+requirement.server+"__") || strings.Contains(line, "MCP server")
			if (configured || builtInToolNames[requirement.server] || explicit ||
				slices.Contains([]string{"safeoutputs", "mcpscripts", "agenticworkflows"}, requirement.server)) &&
				requirement.server != "native-read" && requirement.tool != "" {
				requirements = append(requirements, requirement)
			}
		}
	}
	return requirements
}

func hasPromptToolAlternative(line string, known map[string]promptToolRequirement) bool {
	for _, match := range promptToolAlternative.FindAllStringIndex(line, -1) {
		target := strings.TrimPrefix(line[match[1]:], "`")
		if len(promptInvocationRequirements("use", target, known)) > 0 {
			return true
		}
	}
	return false
}

func (c *Compiler) validateRequiredPromptTools(data *WorkflowData) error {
	data.RequiredMCPTools = make(map[string][]string)
	for _, content := range c.promptToolContents(data) {
		for _, requirement := range requiredPromptTools(content, data.Tools) {
			if !promptToolAvailable(data, requirement, EngineCapabilities{}) {
				return fmt.Errorf("required-tool preflight: %s", promptToolWarning(data, requirement))
			}
			tools := data.RequiredMCPTools[requirement.server]
			if !slices.Contains(tools, requirement.tool) {
				data.RequiredMCPTools[requirement.server] = append(tools, requirement.tool)
			}
		}
	}
	for _, tools := range data.RequiredMCPTools {
		slices.Sort(tools)
	}
	return nil
}
