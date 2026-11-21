package lint

import (
	"maps"
	"slices"
	"strings"
)

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

func executableActionInputKeys[T any](uses any, inputs map[string]T) ([]string, bool) {
	key, javascript := executableActionInput(uses)
	var keys []string
	if key != "" {
		for _, authored := range slices.Sorted(maps.Keys(inputs)) {
			if strings.EqualFold(key, authored) {
				keys = append(keys, authored)
			}
		}
	}
	return keys, javascript
}
