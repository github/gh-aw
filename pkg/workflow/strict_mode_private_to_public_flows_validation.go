// Package workflow provides private-to-public flow validation.
package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
)

// validatePrivateToPublicFlowsPolicy enforces the security policy for
// tools.github.private-to-public-flows. Strict mode rejects the setting;
// non-strict mode warns because workflow logs are public and agents may expose
// private data through public destinations.
func (c *Compiler) validatePrivateToPublicFlowsPolicy(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.ParsedTools == nil || workflowData.ParsedTools.GitHub == nil {
		return nil
	}
	value := workflowData.ParsedTools.GitHub.PrivateToPublicFlows
	if value == nil {
		return nil
	}

	const reason = "private-to-public flows can expose private data through public action logs or public destinations used by the agent"
	if c.effectiveStrictMode(workflowData.RawFrontmatter) {
		return NewValidationError(
			"tools.github.private-to-public-flows",
			fmt.Sprintf("%v", value),
			"strict mode: tools.github.private-to-public-flows is not allowed because "+reason,
			"Remove private-to-public-flows:\n\ntools:\n  github:\n    mode: local",
		)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		"tools.github.private-to-public-flows is enabled; "+reason+". Remove this setting to keep private data isolated.",
	))
	c.IncrementWarningCount()
	return nil
}
