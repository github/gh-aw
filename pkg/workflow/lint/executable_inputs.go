package lint

import "strings"

// executableActionInput lists reviewed code inputs, not arbitrary fields named
// "script". The boolean selects JavaScript-specific remediation.
func executableActionInput(uses any) (string, bool) {
	switch actionName(uses) {
	case "actions/github-script":
		return "script", true
	case "amadevus/pwsh-script", "jannekem/run-python-script-action":
		return "script", false
	case "cardinalby/js-eval-action":
		return "expression", true
	case "addnab/docker-run-action":
		return "run", false
	default:
		return "", false
	}
}

func executableInputKey(fields map[string]any, key string) (string, any, bool) {
	for authored, value := range fields {
		if strings.EqualFold(authored, key) {
			return authored, value, true
		}
	}
	return "", nil, false
}
