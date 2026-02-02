package cli

import (
	"errors"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var validatorsLog = logger.New("cli:validators")

// workflowNameRegex validates workflow names contain only alphanumeric characters, hyphens, and underscores
var workflowNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateWorkflowName checks if the provided workflow name is valid.
// It ensures the name is not empty and contains only alphanumeric characters, hyphens, and underscores.
func ValidateWorkflowName(s string) error {
	validatorsLog.Printf("Validating workflow name: %s", s)
	if s == "" {
		validatorsLog.Print("Workflow name validation failed: empty name")
		return errors.New("workflow name cannot be empty")
	}
	if !workflowNameRegex.MatchString(s) {
		validatorsLog.Printf("Workflow name validation failed: invalid characters in %s", s)
		return errors.New("workflow name must contain only alphanumeric characters, hyphens, and underscores")
	}
	validatorsLog.Printf("Workflow name validated successfully: %s", s)
	return nil
}

// ValidateWorkflowIntent checks if the provided workflow intent is valid.
// It ensures the intent has meaningful content with at least 20 characters
// and is not just whitespace.
func ValidateWorkflowIntent(s string) error {
	validatorsLog.Printf("Validating workflow intent: length=%d", len(s))
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		validatorsLog.Print("Workflow intent validation failed: empty content")
		return errors.New("workflow instructions cannot be empty")
	}
	if len(trimmed) < 20 {
		validatorsLog.Printf("Workflow intent validation failed: too short (%d chars)", len(trimmed))
		return errors.New("please provide at least 20 characters of instructions")
	}
	validatorsLog.Printf("Workflow intent validated successfully: %d chars", len(trimmed))
	return nil
}
