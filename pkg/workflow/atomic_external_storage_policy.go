// Package workflow provides the policy for Atomic external storage.
package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

// validateAtomicExternalStoragePolicy rejects external Atomic storage in strict
// mode and warns otherwise because agent-controlled data is persisted before
// post-run threat detection can review it.
func (c *Compiler) validateAtomicExternalStoragePolicy(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil ||
		!strings.EqualFold(workflowData.EngineConfig.ID, "atomic") ||
		!strings.EqualFold(strings.TrimSpace(workflowData.EngineConfig.Env["ATOMIC_STORAGE"]), "on") {
		return nil
	}

	const reason = "Atomic external storage writes agent-controlled session and workflow data to an external database before post-run threat detection, without staging those writes through safe outputs"
	if c.effectiveStrictMode(workflowData.RawFrontmatter) {
		return NewValidationError(
			"engine.env.ATOMIC_STORAGE",
			workflowData.EngineConfig.Env["ATOMIC_STORAGE"],
			"strict mode: Atomic external storage is not allowed because "+reason,
			"Remove the setting or disable external storage:\n\nengine:\n  env:\n    ATOMIC_STORAGE: \"off\"",
		)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		"Atomic external storage is enabled; "+reason+". Set engine.env.ATOMIC_STORAGE to \"off\" or remove it to keep external storage disabled.",
	))
	c.IncrementWarningCount()
	return nil
}
