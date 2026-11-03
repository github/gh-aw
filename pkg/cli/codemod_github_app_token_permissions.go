package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var githubAppTokenPermissionsCodemodLog = logger.New("cli:codemod_github_app_token_permissions")

const githubAppTokenPermissionsWarning = "TODO: review GitHub App token permissions; inference was unavailable and permission-contents: none is a placeholder"

func getGitHubAppTokenPermissionsCodemod() Codemod {
	return Codemod{
		ID:           "github-app-token-permissions",
		Name:         "Add explicit GitHub App token permissions",
		Description:  "Infers read-only permission-* inputs from directly consumed GitHub CLI tokens, or adds a marked none placeholder for manual review.",
		IntroducedIn: "1.5.0",
		Apply: func(content string, _ map[string]any) (string, bool, error) {
			return applyGitHubAppTokenPermissionsCodemod(content)
		},
	}
}

func applyGitHubAppTokenPermissionsCodemod(content string) (string, bool, error) {
	return codemod.Apply(content, func(f *codemod.Frontmatter) error {
		usedPlaceholder, err := updateGitHubAppTokenPermissions(f)
		if err != nil {
			return err
		}
		if usedPlaceholder {
			return f.Comment([]string{"jobs"}, githubAppTokenPermissionsWarning)
		}
		return nil
	})
}

func updateGitHubAppTokenPermissions(f *codemod.Frontmatter) (bool, error) {
	usedPlaceholder := false
	err := f.Edit(func(document *aw.Document) error {
		jobs, ok := document.Frontmatter["jobs"].(map[string]any)
		if !ok {
			return nil
		}
		for _, jobValue := range jobs {
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
				if !ok || !isGitHubAppTokenStep(step) {
					continue
				}
				with, ok := step["with"].(map[string]any)
				if hasAppTokenPermissionInput(with) {
					continue
				}
				if !ok {
					with = make(map[string]any)
					step["with"] = with
				}

				var inferred map[string]string
				if stepID, ok := step["id"].(string); ok {
					scripts, canInfer := downstreamGitHubTokenScripts(steps, i, stepID)
					if canInfer {
						inferred, _ = workflow.InferGitHubAppTokenPermissionsFromShellScripts(scripts)
					}
				}
				if len(inferred) > 0 {
					for key, value := range inferred {
						with[key] = value
					}
					githubAppTokenPermissionsCodemodLog.Printf("Inferred explicit GitHub App token permissions: %v", inferred)
					continue
				}

				with["permission-contents"] = "none"
				usedPlaceholder = true
				githubAppTokenPermissionsCodemodLog.Print("Added permission-contents: none placeholder; token permissions require manual review")
			}
		}
		return nil
	})
	return usedPlaceholder, err
}

func isGitHubAppTokenStep(step map[string]any) bool {
	uses, ok := step["uses"].(string)
	return ok && strings.HasPrefix(strings.ToLower(uses), "actions/create-github-app-token@")
}

func hasAppTokenPermissionInput(with map[string]any) bool {
	for key := range with {
		if strings.HasPrefix(strings.ToLower(key), "permission-") {
			return true
		}
	}
	return false
}

func downstreamGitHubTokenScripts(steps []any, tokenStepIndex int, tokenStepID string) ([]string, bool) {
	tokenOutput := strings.ToLower("${{ steps." + tokenStepID + ".outputs.token }}")
	var scripts []string
	for _, stepValue := range steps[tokenStepIndex+1:] {
		step, ok := stepValue.(map[string]any)
		if !ok {
			continue
		}
		if !containsTokenOutput(step, tokenOutput) {
			continue
		}
		env, ok := step["env"].(map[string]any)
		if !ok || !usesMintedToken(env, tokenOutput) {
			return nil, false
		}
		script, ok := step["run"].(string)
		if !ok || strings.TrimSpace(script) == "" {
			return nil, false
		}
		scripts = append(scripts, script)
	}
	return scripts, len(scripts) > 0
}

func containsTokenOutput(value any, tokenOutput string) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(value), tokenOutput)
	case map[string]any:
		for _, child := range value {
			if containsTokenOutput(child, tokenOutput) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsTokenOutput(child, tokenOutput) {
				return true
			}
		}
	}
	return false
}

func usesMintedToken(env map[string]any, tokenOutput string) bool {
	for key, value := range env {
		if !strings.EqualFold(key, "GH_TOKEN") && !strings.EqualFold(key, "GITHUB_TOKEN") {
			continue
		}
		if value, ok := value.(string); ok && strings.Contains(strings.ToLower(value), tokenOutput) {
			return true
		}
	}
	return false
}
