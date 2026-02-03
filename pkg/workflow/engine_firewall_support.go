package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var engineFirewallSupportLog = logger.New("workflow:engine_firewall_support")

// hasNetworkRestrictions checks if the workflow has network restrictions defined
// Network restrictions exist if:
// - network.allowed has domains specified (non-empty list) AND it's not just "defaults"
func hasNetworkRestrictions(networkPermissions *NetworkPermissions) bool {
	if networkPermissions == nil {
		return false
	}

	// If allowed domains are specified and it's not just the defaults ecosystem, we have restrictions
	if len(networkPermissions.Allowed) > 0 {
		// Check if it's ONLY "defaults" (which means use default ecosystem, not a restriction)
		if len(networkPermissions.Allowed) == 1 && networkPermissions.Allowed[0] == "defaults" {
			return false
		}
		return true
	}

	// Empty allowed list [] means deny-all, which is a restriction
	if networkPermissions.ExplicitlyDefined && len(networkPermissions.Allowed) == 0 {
		return true
	}

	return false
}

// checkNetworkSupport validates that the selected engine supports network restrictions
// when network restrictions are defined in the workflow
func (c *Compiler) checkNetworkSupport(engine CodingAgentEngine, networkPermissions *NetworkPermissions) error {
	engineFirewallSupportLog.Printf("Checking network support: engine=%s, strict_mode=%t", engine.GetID(), c.strictMode)

	// First, check for explicit firewall disable
	if err := c.checkFirewallDisable(engine, networkPermissions); err != nil {
		return err
	}

	// Check if network restrictions exist
	if !hasNetworkRestrictions(networkPermissions) {
		engineFirewallSupportLog.Print("No network restrictions defined, skipping validation")
		// No restrictions, no validation needed
		return nil
	}

	// Check if engine supports firewall
	if engine.SupportsFirewall() {
		engineFirewallSupportLog.Printf("Engine supports firewall: %s", engine.GetID())
		// Engine supports firewall, no issue
		return nil
	}

	engineFirewallSupportLog.Printf("Warning: engine does not support firewall but network restrictions exist: %s", engine.GetID())
	// Engine does not support firewall, but network restrictions are present
	message := fmt.Sprintf(
		"Selected engine '%s' does not support network firewalling; workflow specifies network restrictions (network.allowed). Network may not be sandboxed.",
		engine.GetID(),
	)

	if c.strictMode {
		// In strict mode, this is an error
		return fmt.Errorf("strict mode: engine must support firewall when network restrictions (network.allowed) are set")
	}

	// In non-strict mode, emit a warning
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
	c.IncrementWarningCount()

	return nil
}

// checkFirewallDisable validates firewall: "disable" configuration
// - Warning if allowed != * (unrestricted)
// - Error in strict mode if allowed is not * or engine does not support firewall
func (c *Compiler) checkFirewallDisable(engine CodingAgentEngine, networkPermissions *NetworkPermissions) error {
	if networkPermissions == nil || networkPermissions.Firewall == nil {
		return nil
	}

	// Check if firewall is explicitly disabled
	if !networkPermissions.Firewall.Enabled {
		// Check if network has restrictions (allowed list specified with domains)
		hasRestrictions := len(networkPermissions.Allowed) > 0

		if hasRestrictions {
			message := "Firewall is disabled but network restrictions are specified (network.allowed). Network may not be properly sandboxed."

			if c.strictMode {
				// In strict mode, this is an error
				return fmt.Errorf("strict mode: cannot disable firewall when network restrictions (network.allowed) are set")
			}

			// In non-strict mode, emit a warning
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
			c.IncrementWarningCount()
		}

		// Also check if engine doesn't support firewall in strict mode when there are no restrictions
		if c.strictMode && !hasRestrictions && !engine.SupportsFirewall() {
			return fmt.Errorf("strict mode: engine '%s' does not support firewall", engine.GetID())
		}
	}

	return nil
}
