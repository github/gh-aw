// Package workflow provides the check-for-updates validation for strict mode.
//
// The check-for-updates flag controls whether the version update check step runs in the
// activation job. Setting check-for-updates: false disables the step, which is useful when
// running in air-gapped environments or when the update check is not desired.
//
// Security policy:
//   - In strict mode: setting check-for-updates: false raises a compilation error.
//   - In non-strict mode: setting check-for-updates: false emits a warning.
//
// See: https://github.github.com/gh-aw/reference/frontmatter/#check-for-updates
package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var updateCheckValidationLog = logger.New("workflow:strict_mode_update_check_validation")

// validateUpdateCheck enforces the policy for the check-for-updates: false flag.
// In strict mode it returns an error; in non-strict mode it emits a warning.
func (c *Compiler) validateUpdateCheck(frontmatter map[string]any) error {
	// Determine whether check-for-updates: false is set
	updateCheckDisabled := false
	if rawVal, ok := frontmatter["check-for-updates"]; ok {
		if boolVal, ok := rawVal.(bool); ok && !boolVal {
			updateCheckDisabled = true
		}
	}

	if !updateCheckDisabled {
		updateCheckValidationLog.Printf("check-for-updates is enabled (default), skipping validation")
		return nil
	}

	updateCheckValidationLog.Printf("check-for-updates: false detected")

	if c.strictMode {
		return errors.New("strict mode: 'check-for-updates: false' is not allowed. The version update check must remain enabled in strict mode to ensure the workflow uses a supported compile-agentic version")
	}

	// Non-strict mode: emit a warning and continue
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		"'check-for-updates: false' disables the compile-agentic version check. "+
			"The workflow will not verify that it was compiled with a supported version of gh-aw. "+
			"It is strongly recommended to keep check-for-updates enabled.",
	))
	c.IncrementWarningCount()

	return nil
}
