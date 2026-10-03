package workflow

import "strings"

// Bare deny rules remove tools even when a user explicitly selects auto or acceptEdits.
func claudeDisabledTools(data *WorkflowData, allowed string) []string {
	granted := make(map[string]struct{})
	for tool := range strings.SplitSeq(allowed, ",") {
		name, _, _ := strings.Cut(tool, "(")
		granted[name] = struct{}{}
	}
	has := func(name string) bool {
		_, ok := granted[name]
		return ok
	}
	denied := []string{"AskUserQuestion"}
	if data.BashDisabled || isBashExplicitlyRefused(data.Tools) || !has("Bash") {
		denied = append(denied, "Bash")
	}
	for _, tool := range []string{"WebFetch", "WebSearch"} {
		if !has(tool) {
			denied = append(denied, tool)
		}
	}
	for _, tool := range []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} {
		if !has(tool) {
			denied = append(denied, tool)
		}
	}
	return denied
}
