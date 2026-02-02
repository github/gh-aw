package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var manualApprovalLog = logger.New("workflow:manual_approval")

// extractManualApprovalFromOn extracts the manual-approval value from the on: section
func (c *Compiler) extractManualApprovalFromOn(frontmatter map[string]any) (string, error) {
	onSection, exists := frontmatter["on"]
	if !exists {
		manualApprovalLog.Print("No on: section found in frontmatter")
		return "", nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no manual-approval possible
		manualApprovalLog.Printf("on: section is simple string format: %s", on)
		return "", nil
	case map[string]any:
		// Complex object format - look for manual-approval
		if manualApproval, exists := on["manual-approval"]; exists {
			if str, ok := manualApproval.(string); ok {
				manualApprovalLog.Printf("Found manual-approval configuration: %s", str)
				return str, nil
			}
			return "", fmt.Errorf("manual-approval value must be a string, got %T. Example: manual-approval: \"production\"", manualApproval)
		}
		manualApprovalLog.Print("on: section is object format but no manual-approval field found")
		return "", nil
	default:
		return "", fmt.Errorf("invalid on: section format, got %T. Expected string or object. Example: on: push or on:\n  push:\n    branches: [main]", onSection)
	}
}

// processManualApprovalConfiguration extracts manual-approval configuration from frontmatter
func (c *Compiler) processManualApprovalConfiguration(frontmatter map[string]any, workflowData *WorkflowData) error {
	manualApprovalLog.Print("Processing manual-approval configuration")

	// Extract manual-approval from the on: section
	manualApproval, err := c.extractManualApprovalFromOn(frontmatter)
	if err != nil {
		manualApprovalLog.Printf("Failed to extract manual-approval: %v", err)
		return err
	}
	workflowData.ManualApproval = manualApproval

	if manualApproval != "" {
		manualApprovalLog.Printf("Manual approval configured for workflow: %s", manualApproval)
	} else {
		manualApprovalLog.Print("No manual approval configured for workflow")
	}

	return nil
}
