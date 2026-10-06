package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

type appTokenStepKey struct {
	id, clientID, privateKey string
}

func (c *Compiler) hasGeneratedWildcardAppTokenStep(step, with map[string]any) bool {
	id, ok := step["id"].(string)
	if !ok {
		return false
	}
	clientID, ok := with["client-id"].(string)
	if !ok {
		return false
	}
	privateKey, ok := with["private-key"].(string)
	if !ok {
		return false
	}
	return id != "" && clientID != "" && privateKey != "" &&
		c.wildcardAppTokenSteps[appTokenStepKey{id, clientID, privateKey}]
}

func hasExplicitAppTokenPermission(with map[string]any) bool {
	for key, value := range with {
		if !strings.HasPrefix(strings.ToLower(key), "permission-") {
			continue
		}
		level, ok := value.(string)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(level)) {
		case "read", "write", "none":
			return true
		}
	}
	return false
}

func hasExplicitAppTokenRepositories(with map[string]any) bool {
	for key, value := range with {
		if !strings.EqualFold(key, "repositories") {
			continue
		}
		repositories, ok := value.(string)
		return ok && strings.TrimSpace(repositories) != ""
	}
	return false
}

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
			if !strings.HasPrefix(strings.ToLower(uses), "actions/create-github-app-token@") {
				continue
			}
			with, ok := step["with"].(map[string]any)
			if !ok {
				with = nil
			}
			if !hasExplicitAppTokenRepositories(with) && !c.hasGeneratedWildcardAppTokenStep(step, with) {
				msg := fmt.Sprintf("actions/create-github-app-token in job %q has no explicit repositories input; add repositories: ${{ github.repository }} to scope the token to the current repository", jobName)
				if strict {
					return fmt.Errorf("strict mode: %s", msg)
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
				c.IncrementWarningCount()
			}
			if hasExplicitAppTokenPermission(with) {
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
