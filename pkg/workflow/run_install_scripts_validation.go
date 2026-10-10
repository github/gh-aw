// This file implements run-install-scripts validation for agentic workflows.
//
// # Run Scripts
//
// By default, the runtime manager adds --ignore-scripts to generated npm install
// commands to prevent pre/post install scripts from executing. This is a supply
// chain security measure: malicious packages can use install hooks to exfiltrate
// secrets or corrupt the runner environment.
//
// Users can opt in to install scripts by setting runtimes.node.run-install-scripts: true
// in their workflow frontmatter. This emits a security warning in non-strict mode and
// is rejected as an error in strict mode.
//
// # Supported Flags (by package manager)
//
//   - npm / yarn / pnpm: --ignore-scripts
//   - pip / uv: no pre/post install lifecycle scripts (N/A)
//   - go / gem / bundle / dotnet / elixir / haskell / java / ruby: N/A
//
// # Configuration
//
// Per-runtime (node only, since it is the only runtime that generates install commands):
//
//	runtimes:
//	  node:
//	    run-install-scripts: true

package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var runInstallScriptsLog = logger.New("workflow:run_install_scripts_validation")

// resolveRunInstallScripts determines whether install scripts should be allowed based on
// the workflow frontmatter and any merged settings from imported shared workflows.
//
// Returns true (allow scripts) when any of the following is set:
//   - runtimes.node.run-install-scripts: true in the frontmatter
//   - mergedRunInstallScripts is true (any imported shared workflow enables run-install-scripts)
func resolveRunInstallScripts(runtimes map[string]any, mergedRunInstallScripts bool) bool {
	// Already enabled by an imported shared workflow
	if mergedRunInstallScripts {
		runInstallScriptsLog.Print("run-install-scripts enabled by imported shared workflow")
		return true
	}

	// Check per-runtime run-install-scripts for node (the only runtime that generates npm install commands)
	if nodeAny, ok := runtimes["node"]; ok {
		if nodeMap, ok := nodeAny.(map[string]any); ok {
			if rsAny, ok := nodeMap["run-install-scripts"]; ok {
				if rsBool, ok := rsAny.(bool); ok && rsBool {
					runInstallScriptsLog.Print("run-install-scripts enabled via runtimes.node.run-install-scripts: true")
					return true
				}
			}
		}
	}

	return false
}

// validateRunInstallScripts emits a warning (non-strict mode) or returns an error (strict mode)
// when run-install-scripts is enabled in the workflow. This alerts users to the supply chain
// attack risk introduced by allowing npm pre/post install scripts.
func (c *Compiler) validateRunInstallScripts(workflowData *WorkflowData) error {
	if !workflowData.RunInstallScripts {
		runInstallScriptsLog.Print("run-install-scripts not enabled, skipping validation")
		return nil
	}

	runInstallScriptsLog.Print("run-install-scripts is enabled, emitting supply chain warning")

	warningMsg := "run-install-scripts: true is set – npm pre/post install scripts will execute during package installation. " +
		"This is a supply chain security risk: malicious or compromised packages can use install hooks to " +
		"exfiltrate secrets or tamper with the runner environment. " +
		"Remove run-install-scripts: true unless you fully trust all installed packages and their transitive dependencies."

	if c.strictMode {
		return fmt.Errorf("strict mode: %s", warningMsg)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
	c.IncrementWarningCount()
	return nil
}
