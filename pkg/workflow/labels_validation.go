package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var labelsValidationLog = logger.New("workflow:labels_validation")

// validateLabels validates the labels field in the workflow frontmatter.
// It checks that:
// 1. Labels is an array (if present)
// 2. Each label is a non-empty string
// 3. Labels don't have leading or trailing whitespace
func validateLabels(workflowData *WorkflowData) error {
	// If ParsedFrontmatter is nil or Labels is empty, nothing to validate
	if workflowData == nil || workflowData.ParsedFrontmatter == nil {
		return nil
	}

	labels := workflowData.ParsedFrontmatter.Labels
	if len(labels) == 0 {
		return nil
	}

	labelsValidationLog.Printf("Validating %d labels", len(labels))

	// Validate each label
	for i, label := range labels {
		// Check for empty labels (should be caught by schema minLength: 1, but double-check)
		if label == "" {
			return fmt.Errorf("labels[%d] is empty. Each label must be a non-empty string", i)
		}

		// Check for excessive whitespace
		trimmed := strings.TrimSpace(label)
		if trimmed != label {
			return fmt.Errorf("labels[%d] has leading or trailing whitespace: %q. Labels should be trimmed", i, label)
		}

		labelsValidationLog.Printf("Label %d validated: %q", i, label)
	}

	labelsValidationLog.Printf("All %d labels validated successfully", len(labels))
	return nil
}
