package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

// validateAppTokenPermissions checks the compiled jobs, including imported and
// compiler-generated steps, before an installation token can inherit every scope.
func (c *Compiler) validateAppTokenPermissions(workflow map[string]any, strict bool) error {
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok {
		return nil
	}
	for jobName, jobValue := range jobs {
		job, ok := jobValue.(map[string]any)
		if !ok {
			continue
		}
		steps, ok := job["steps"].([]any)
		if !ok {
			continue
		}
		for i, stepValue := range steps {
			step, ok := stepValue.(map[string]any)
			if !ok {
				continue
			}
			uses, ok := step["uses"].(string)
			if !ok {
				continue
			}
			if !strings.HasPrefix(uses, "actions/create-github-app-token@") {
				continue
			}
			with, ok := step["with"].(map[string]any)
			if !ok {
				with = nil
			}
			scoped := false
			for key, value := range with {
				if !strings.HasPrefix(key, "permission-") {
					continue
				}
				level, ok := value.(string)
				if ok && (level == "read" || level == "write") {
					scoped = true
					break
				}
			}
			if scoped {
				continue
			}
			name, ok := step["name"].(string)
			if !ok || name == "" {
				name = fmt.Sprintf("step %d", i+1)
			}
			msg := fmt.Sprintf("actions/create-github-app-token in job %q (%s) has no explicit permission-* inputs; add only the required permission scopes under with: to avoid minting a fully scoped installation token", jobName, name)
			if strict {
				return fmt.Errorf("strict mode: %s", msg)
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
			c.IncrementWarningCount()
		}
	}
	return nil
}
