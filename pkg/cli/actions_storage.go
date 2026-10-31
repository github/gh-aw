package cli

import (
	"errors"
	"strings"
)

// validateActionsStorageValue runs before encryption, logging, or upload.
// CLI authentication is separate from the value being persisted.
func validateActionsStorageValue(value string) error {
	if strings.Contains(value, "gho_") {
		return errors.New("refusing to store a GitHub OAuth token in Actions secrets or variables: never use the gh auth login OAuth token as a workflow credential; elicit the user to create a scoped PAT and replace the token through GitHub's secure Actions secrets UI")
	}
	return nil
}
