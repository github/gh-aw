package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var defaultActionsAdvisoryTokenPattern = regexp.MustCompile(`(?i)(github\s*(\.\s*token\b|\[\s*['"]token['"]\s*\])|secrets\s*(\.\s*github_token\b|\[\s*['"]github_token['"]\s*\]))`)

func resolveSecurityAdvisoryGitHubToken(config *SafeOutputsConfig, key string, handler *BaseSafeOutputConfig) string {
	if token := resolveHandlerGitHubToken(handler.GitHubApp, key, handler.GitHubToken); token != "" {
		return token
	}
	if config.GitHubApp != nil {
		return "${{ steps.safe-outputs-app-token.outputs.token }}"
	}
	return config.GitHubToken
}

func validateSecurityAdvisoryAuthentication(config *SafeOutputsConfig, key string, handler *BaseSafeOutputConfig) error {
	if isHandlerStaged(templatableBoolIsTrue(config.Staged), handler.Staged) {
		return nil
	}
	if handler.GitHubApp != nil || (handler.GitHubToken == "" && config.GitHubApp != nil) {
		return fmt.Errorf("safe-outputs.%s: requires an external github-token with Repository security advisories write permission; GitHub App authentication is unsupported because the token action does not expose an advisory permission input, and GITHUB_TOKEN cannot perform advisory writes.", key)
	}
	token := resolveSecurityAdvisoryGitHubToken(config, key, handler)
	if strings.TrimSpace(token) != "" && !defaultActionsAdvisoryTokenPattern.MatchString(token) {
		return nil
	}
	return fmt.Errorf("safe-outputs.%s: requires an external github-token with Repository security advisories write permission; GITHUB_TOKEN cannot perform advisory writes.\n\nExample:\n  safe-outputs:\n    %s:\n      github-token: ${{ secrets.SECURITY_ADVISORY_TOKEN }}", key, key)
}
