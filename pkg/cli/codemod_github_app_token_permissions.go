package cli

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var githubAppTokenPermissionsCodemodLog = logger.New("cli:codemod_github_app_token_permissions")

const githubAppTokenPermissionsWarning = "TODO: review GitHub App token permissions; inference was unavailable and permission-contents: none is a placeholder"

var githubAppTokenOutputRE = regexp.MustCompile(`(?i)steps\s*\.\s*([A-Za-z_][A-Za-z0-9_-]*)\s*\.\s*outputs\s*\.\s*token\b`)

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
		warningPaths, err := updateGitHubAppTokenPermissions(f)
		if err != nil {
			return err
		}
		for _, path := range warningPaths {
			if err := f.Comment(path, githubAppTokenPermissionsWarning); err != nil {
				return err
			}
		}
		return nil
	})
}

func updateGitHubAppTokenPermissions(f *codemod.Frontmatter) ([][]string, error) {
	var warningPaths [][]string
	err := f.Edit(func(document *aw.Document) error {
		frontmatter := document.Frontmatter
		updateGitHubAppTokenStepSections(frontmatter, nil, &warningPaths)
		if jobs, ok := frontmatter["jobs"].(map[string]any); ok {
			for jobName, jobValue := range jobs {
				if job, ok := jobValue.(map[string]any); ok {
					updateGitHubAppTokenStepSections(job, []string{"jobs", jobName}, &warningPaths)
				}
			}
		}
		return nil
	})
	return warningPaths, err
}

func updateGitHubAppTokenStepSections(sections map[string]any, path []string, warningPaths *[][]string) {
	for key, value := range sections {
		if !isGitHubAppTokenStepSection(key) {
			continue
		}
		if steps, ok := value.([]any); ok {
			sectionPath := append(append([]string(nil), path...), key)
			updateGitHubAppTokenSteps(steps, sectionPath, warningPaths)
		}
	}
}

func isGitHubAppTokenStepSection(key string) bool {
	switch key {
	case "steps", "setup-steps", "pre-steps", "pre-agent-steps", "post-steps":
		return true
	default:
		return false
	}
}

func updateGitHubAppTokenSteps(steps []any, sectionPath []string, warningPaths *[][]string) {
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
		appendGitHubAppTokenWarningPath(warningPaths, sectionPath)
		githubAppTokenPermissionsCodemodLog.Print("Added permission-contents: none placeholder; token permissions require manual review")
	}
}

func appendGitHubAppTokenWarningPath(paths *[][]string, path []string) {
	for _, existing := range *paths {
		if strings.Join(existing, "\x00") == strings.Join(path, "\x00") {
			return
		}
	}
	*paths = append(*paths, append([]string(nil), path...))
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
	var scripts []string
	for _, stepValue := range steps[tokenStepIndex+1:] {
		step, ok := stepValue.(map[string]any)
		if !ok {
			continue
		}
		if !containsTokenOutput(step, tokenStepID) {
			continue
		}
		if !hasDirectGitHubTokenBinding(step, tokenStepID) {
			return nil, false
		}
		script, ok := step["run"].(string)
		if !ok || strings.TrimSpace(script) == "" || step["uses"] != nil {
			return nil, false
		}
		scripts = append(scripts, script)
	}
	return scripts, len(scripts) > 0
}

func containsTokenOutput(value any, tokenStepID string) bool {
	switch value := value.(type) {
	case string:
		return containsGitHubTokenOutputReference(value, tokenStepID)
	case map[string]any:
		for _, child := range value {
			if containsTokenOutput(child, tokenStepID) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsTokenOutput(child, tokenStepID) {
				return true
			}
		}
	}
	return false
}

func hasDirectGitHubTokenBinding(step map[string]any, tokenStepID string) bool {
	env, ok := step["env"].(map[string]any)
	if !ok {
		return false
	}
	found := false
	for key, value := range step {
		if strings.EqualFold(key, "env") {
			for envKey, envValue := range env {
				envString, ok := envValue.(string)
				if !ok || !containsGitHubTokenOutputReference(envString, tokenStepID) {
					continue
				}
				if (!strings.EqualFold(envKey, "GH_TOKEN") && !strings.EqualFold(envKey, "GITHUB_TOKEN")) ||
					!isDirectGitHubTokenExpression(envString, tokenStepID) {
					return false
				}
				found = true
			}
			continue
		}
		if containsTokenOutput(value, tokenStepID) {
			return false
		}
	}
	return found
}

func containsGitHubTokenOutputReference(value, tokenStepID string) bool {
	match := githubAppTokenOutputRE.FindStringSubmatch(value)
	return len(match) == 2 && strings.EqualFold(match[1], tokenStepID) //nolint:uncheckedsliceindex // The match-length check requires the captured identifier.
}

func isDirectGitHubTokenExpression(value string, tokenStepID string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${{") || !strings.HasSuffix(trimmed, "}}") {
		return false
	}
	expression := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "${{"), "}}"))
	match := githubAppTokenOutputRE.FindStringSubmatch(expression)
	return len(match) == 2 && strings.EqualFold(match[1], tokenStepID) && len(match[0]) == len(expression) //nolint:uncheckedsliceindex // The match-length check requires both regex captures.
}
