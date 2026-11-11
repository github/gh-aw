package workflow

import (
	"errors"
	"regexp"
	"strings"
)

// CreateSecurityAdvisoryConfig enables draft advisories in the workflow repository.
type CreateSecurityAdvisoryConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

func (c *Compiler) parseCreateSecurityAdvisoryConfig(outputMap map[string]any) *CreateSecurityAdvisoryConfig {
	value, exists := outputMap["create-security-advisory"]
	if !exists {
		return nil
	}
	config := &CreateSecurityAdvisoryConfig{}
	config.Max = defaultIntStr(1)
	if configMap, ok := value.(map[string]any); ok {
		c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)
	}
	return config
}

var defaultActionsAdvisoryTokenPattern = regexp.MustCompile(`(?i)(github\s*(\.\s*token\b|\[\s*['"]token['"]\s*\])|secrets\s*(\.\s*github_token\b|\[\s*['"]github_token['"]\s*\]))`)

func resolveSecurityAdvisoryGitHubToken(config *SafeOutputsConfig) string {
	handler := config.CreateSecurityAdvisory
	if token := resolveHandlerGitHubToken(handler.GitHubApp, "create-security-advisory", handler.GitHubToken); token != "" {
		return token
	}
	if config.GitHubApp != nil {
		return "${{ steps.safe-outputs-app-token.outputs.token }}"
	}
	return config.GitHubToken
}

func validateSafeOutputsCreateSecurityAdvisory(config *SafeOutputsConfig) error {
	if config == nil || config.CreateSecurityAdvisory == nil {
		return nil
	}
	if isHandlerStaged(templatableBoolIsTrue(config.Staged), config.CreateSecurityAdvisory.Staged) {
		return nil
	}
	token := resolveSecurityAdvisoryGitHubToken(config)
	if strings.TrimSpace(token) != "" && !defaultActionsAdvisoryTokenPattern.MatchString(token) {
		return nil
	}
	return errors.New("safe-outputs.create-security-advisory: requires an external github-token or github-app with Repository security advisories write permission; GITHUB_TOKEN cannot create draft advisories.\n\nExample:\n  safe-outputs:\n    create-security-advisory:\n      github-token: ${{ secrets.SECURITY_ADVISORY_TOKEN }}")
}
