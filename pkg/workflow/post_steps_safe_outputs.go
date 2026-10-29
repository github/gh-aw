package workflow

import (
	"encoding/json"
	"regexp"
	"strings"
)

var literalSafeOutputsToolRE = regexp.MustCompile(`\Asafeoutputs [a-z][a-z0-9_]* '([^'\r\n]*)'\n?\z`)

// IsLiteralSafeOutputsToolStep identifies self-contained tool calls that can run
// before gateway shutdown and output collection. Do not accept conditions that
// depend on outputs from later compiler-generated steps.
func IsLiteralSafeOutputsToolStep(step map[string]any) bool {
	for key := range step {
		switch key {
		case "name", "run", "shell", "if":
		default:
			return false
		}
	}
	if shell, ok := step["shell"]; ok && shell != "bash" && shell != "sh" {
		return false
	}
	if condition, ok := step["if"]; ok && condition != "success()" && condition != "always()" && condition != "failure()" {
		return false
	}
	run, ok := step["run"].(string)
	if !ok || strings.Contains(run, "${{") {
		return false
	}
	for index, value := range literalSafeOutputsToolRE.FindStringSubmatch(run) {
		if index == 1 {
			var args map[string]json.RawMessage
			return json.Unmarshal([]byte(value), &args) == nil && args != nil
		}
	}
	return false
}
