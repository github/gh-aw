package cli

import (
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var actionsStorageLog = logger.New("cli:actions_storage")

// validateActionsStorageValue runs before encryption, logging, or upload.
// CLI authentication is separate from the value being persisted.
func validateActionsStorageValue(value string) error {
	if strings.Contains(value, "gho_") {
		// Never log the value itself: it is the candidate secret/token under validation.
		actionsStorageLog.Print("Rejecting Actions storage value: matches the GitHub OAuth token prefix (gho_)")
		return errors.New("refusing to store a GitHub OAuth token in Actions secrets or variables: never use the gh auth login OAuth token as a workflow credential; elicit the user to create a scoped PAT and replace the token through GitHub's secure Actions secrets UI")
	}
	return nil
}
