package workflow

import (
	"fmt"
	"strings"
)

func validateSafeOutputsRequiredAuthors(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}

	if config.UpdatePullRequests != nil {
		if err := validateSafeOutputRequiredAuthor("update-pull-request", config.UpdatePullRequests.RequiredAuthor); err != nil {
			return err
		}
	}
	if config.PushToPullRequestBranch != nil {
		if err := validateSafeOutputRequiredAuthor("push-to-pull-request-branch", config.PushToPullRequestBranch.RequiredAuthor); err != nil {
			return err
		}
	}
	return nil
}

func validateSafeOutputRequiredAuthor(handler, value string) error {
	if value == "" {
		return nil
	}
	field := "safe-outputs." + handler + ".required-author"
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s cannot be empty or whitespace-only", field)
	}
	if hasExpressionMarker(value) && !isExpression(value) {
		return fmt.Errorf("%s must be a complete GitHub Actions expression; mixed literal/expression values are not supported", field)
	}
	return nil
}
