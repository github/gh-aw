package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var workflowDispatchAwContextCodemodLog = logger.New("cli:codemod_workflow_dispatch_aw_context")

func getWorkflowDispatchAwContextRemovalCodemod() Codemod {
	return Codemod{
		ID:           "workflow-dispatch-aw-context-removal",
		Name:         "Remove reserved workflow_dispatch aw_context input",
		Description:  "Removes on.workflow_dispatch.inputs.aw_context, which is reserved and managed by the compiler.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			on, ok := frontmatter["on"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			dispatch, ok := on["workflow_dispatch"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			inputs, ok := dispatch["inputs"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			if _, exists := inputs["aw_context"]; !exists {
				return content, false, nil
			}

			newContent, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				onLine := findFrontmatterKeyLine(lines, "on", 0, len(lines), -1)
				if onLine < 0 || onLine >= len(lines) {
					return lines, false
				}
				onEnd := frontmatterBlockEnd(lines, onLine, len(getIndentation(lines[onLine])))
				onIndent := len(getIndentation(lines[onLine]))
				dispatchLine := findFrontmatterKeyLine(lines, "workflow_dispatch", onLine+1, onEnd, onIndent)
				if dispatchLine < 0 || dispatchLine >= len(lines) {
					return lines, false
				}
				dispatchEnd := frontmatterBlockEnd(lines, dispatchLine, len(getIndentation(lines[dispatchLine])))
				block, modified := removeFieldFromBlock(lines[dispatchLine:dispatchEnd], "aw_context", "inputs")
				if !modified {
					return lines, false
				}
				result := make([]string, 0, len(lines)-(dispatchEnd-dispatchLine)+len(block))
				result = append(result, lines[:dispatchLine]...)
				result = append(result, block...)
				result = append(result, lines[dispatchEnd:]...)
				return result, true
			})
			if applied {
				workflowDispatchAwContextCodemodLog.Print("Removed reserved on.workflow_dispatch.inputs.aw_context")
			}
			return newContent, applied, err
		},
	}
}

func findFrontmatterKeyLine(lines []string, key string, start, end, parentIndent int) int {
	childIndent := -1
	for i, line := range lines {
		if i < start || i >= end {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(getIndentation(line))
		if parentIndent >= 0 {
			if indent <= parentIndent {
				continue
			}
			if childIndent < 0 {
				childIndent = indent
			}
			if indent < childIndent {
				return -1
			}
			if indent > childIndent {
				continue
			}
		}
		if frontmatterLineKey(line) == key {
			return i
		}
	}
	return -1
}

func frontmatterBlockEnd(lines []string, start, parentIndent int) int {
	for i, line := range lines {
		if i <= start {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(getIndentation(line)) <= parentIndent {
			return i
		}
	}
	return len(lines)
}

func frontmatterLineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	key, _, found := strings.Cut(trimmed, ":")
	if !found {
		return ""
	}
	return strings.Trim(key, `"'`)
}
