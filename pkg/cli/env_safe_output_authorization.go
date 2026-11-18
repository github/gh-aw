package cli

import (
	"fmt"

	"github.com/github/gh-aw/pkg/workflow"
)

func validateSafeOutputAuthorizationDefaults(file *defaultsFile) error {
	if endpoint := file.DefaultSafeOutputAuthorizationURL; endpoint != nil && *endpoint != "" {
		if err := workflow.ValidateSafeOutputAuthorization(*endpoint, "validation"); err != nil {
			return fmt.Errorf("default_safe_output_authorization_url: %w", err)
		}
	}
	if audience := file.DefaultSafeOutputAuthorizationAudience; audience != nil && *audience != "" {
		if err := workflow.ValidateSafeOutputAuthorization("https://validation.invalid", *audience); err != nil {
			return fmt.Errorf("default_safe_output_authorization_audience: %w", err)
		}
	}
	return nil
}
