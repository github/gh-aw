package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

type appTokenStepKey struct {
	jobName, id, clientID, privateKey string
}

func validateRepoMemoryAppConfig(memory RepoMemoryEntry) error {
	for _, config := range []struct {
		field string
		app   *GitHubAppConfig
	}{
		{"github-app", memory.GitHubApp},
		{"github-app-read", memory.GitHubAppRead},
		{"github-app-write", memory.GitHubAppWrite},
	} {
		if config.app == nil {
			continue
		}
		if !config.app.IgnoreIfMissing && !config.app.hasRequiredCredentials() {
			return fmt.Errorf("tools.repo-memory.%s requires client-id (or app-id) and private-key; configure both credentials or set ignore-if-missing: true", config.field)
		}
		if len(config.app.Permissions) > 0 {
			return fmt.Errorf("tools.repo-memory.%s does not support permissions; repo-memory requests only contents permission", config.field)
		}
	}
	return nil
}

func validateRepoMemoryAppTarget(memory RepoMemoryEntry) error {
	if (memory.githubApp(PermissionRead) == nil && memory.githubApp(PermissionWrite) == nil) || memory.TargetRepo == "" {
		return nil
	}
	if !strings.Contains(memory.TargetRepo, "${{") {
		if !repoMemoryTargetPattern.MatchString(memory.TargetRepo) {
			return errors.New("tools.repo-memory.target-repo must use owner/repo format when github-app is configured")
		}
		return nil
	}
	for _, app := range []*GitHubAppConfig{memory.githubApp(PermissionRead), memory.githubApp(PermissionWrite)} {
		if app == nil {
			continue
		}
		if app.Owner != "" && !repoMemoryTargetPartPattern.MatchString(app.Owner) {
			return errors.New("tools.repo-memory.github-app.owner must be a literal owner for expression target-repo authorization")
		}
		for _, repository := range app.Repositories {
			if !repoMemoryTargetPartPattern.MatchString(repository) {
				return errors.New("tools.repo-memory.github-app.repositories must contain literal repository names for expression target-repo authorization")
			}
		}
	}
	return nil
}

func (c *Compiler) hasGeneratedWildcardAppTokenStep(jobName string, step, with map[string]any, seen map[appTokenStepKey]bool) bool {
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
	key := appTokenStepKey{jobName: jobName, id: id, clientID: clientID, privateKey: privateKey}
	if id == "" || clientID == "" || privateKey == "" || !c.wildcardAppTokenSteps[key] || seen[key] {
		return false
	}
	seen[key] = true
	return true
}

func (c *Compiler) recordGeneratedWildcardAppTokenStep(jobName string, app *GitHubAppConfig, stepID string) {
	if jobName == "" || app == nil || len(app.Repositories) != 1 {
		return
	}
	for _, repository := range app.Repositories {
		if repository != "*" {
			return
		}
	}
	if c.wildcardAppTokenSteps == nil {
		c.wildcardAppTokenSteps = make(map[appTokenStepKey]bool)
	}
	c.wildcardAppTokenSteps[appTokenStepKey{jobName: jobName, id: stepID, clientID: app.AppID, privateKey: app.PrivateKey}] = true
}

func (c *Compiler) buildGitHubAppTokenMintStepForJob(jobName string, app *GitHubAppConfig, permissions *Permissions, fallbackRepoExpr string, ownerSourceRepository string, stepName string, stepID string, conditions ...string) []string {
	steps := c.buildGitHubAppTokenMintStepWithMeta(app, permissions, fallbackRepoExpr, ownerSourceRepository, stepName, stepID, conditions...)
	c.recordGeneratedWildcardAppTokenStep(jobName, app, stepID)
	return steps
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

func hasAppTokenRevocation(with map[string]any) bool {
	for key, value := range with {
		if !strings.EqualFold(key, "skip-token-revoke") {
			continue
		}
		switch value := value.(type) {
		case bool:
			if value {
				return false
			}
		case string:
			if !strings.EqualFold(strings.TrimSpace(value), "false") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// validateAppTokenPermissions checks the compiled jobs, including imported and
// compiler-generated steps, for scope and mandatory gateway token cleanup.
func (c *Compiler) validateAppTokenPermissions(workflow map[string]any, strict bool, requireRevocation bool) error {
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok {
		return nil
	}
	seenGeneratedWildcardSteps := make(map[appTokenStepKey]bool)
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
			if requireRevocation && !hasAppTokenRevocation(with) {
				return fmt.Errorf("gateway-dispatch requires actions/create-github-app-token in job %q to revoke its token; remove skip-token-revoke or set it to false", jobName)
			}
			if !hasExplicitAppTokenRepositories(with) && !c.hasGeneratedWildcardAppTokenStep(jobName, step, with, seenGeneratedWildcardSteps) {
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
