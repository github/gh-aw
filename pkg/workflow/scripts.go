// This file provides embedded JavaScript scripts for GitHub Actions workflows.
//
// # Script Registry Pattern
//
// This file previously managed embedded JavaScript scripts, but inline bundling
// has been removed. Scripts are now provided by the actions/setup action at runtime.
//
// See script_registry.go for the ScriptRegistry implementation.

package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var scriptsLog = logger.New("workflow:scripts")

// init registers scripts with the DefaultScriptRegistry.
// Note: Embedded scripts have been removed - scripts are now provided by actions/setup at runtime.
func init() {
	scriptsLog.Print("Script registration completed (embedded scripts removed)")
}
