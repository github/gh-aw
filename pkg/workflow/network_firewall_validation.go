// This file provides network firewall validation functions for agentic workflow compilation.
//
// This file contains domain-specific validation functions for network firewall configuration:
//   - validateNetworkFirewallConfig() - Validates firewall configuration dependencies
//
// These validation functions are organized in a dedicated file following the validation
// architecture pattern where domain-specific validation belongs in domain validation files.
// See validation.go for the complete validation architecture documentation.

package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var networkFirewallValidationLog = logger.New("workflow:network_firewall_validation")

// validateNetworkFirewallConfig validates network firewall configuration dependencies
// Returns an error if the configuration is invalid
func validateNetworkFirewallConfig(networkPermissions *NetworkPermissions) error {
	if networkPermissions == nil {
		return nil
	}

	firewallConfig := networkPermissions.Firewall
	if firewallConfig == nil {
		return nil
	}

	networkFirewallValidationLog.Print("Validating network firewall configuration")

	// Validate allow-urls requires ssl-bump
	if len(firewallConfig.AllowURLs) > 0 && !firewallConfig.SSLBump {
		networkFirewallValidationLog.Printf("Validation error: allow-urls specified without ssl-bump: %d URLs", len(firewallConfig.AllowURLs))
		return NewValidationError(
			"network.firewall.allow-urls",
			"requires ssl-bump: true",
			"allow-urls requires ssl-bump: true to function. SSL Bump enables HTTPS content inspection, which is necessary for URL path filtering",
			"Enable SSL Bump in your firewall configuration:\n\nnetwork:\n  firewall:\n    ssl-bump: true\n    allow-urls:\n      - \"https://github.com/githubnext/*\"\n\nSee: "+string(constants.DocsNetworkURL),
		)
	}

	if len(firewallConfig.AllowURLs) > 0 {
		networkFirewallValidationLog.Printf("Validated allow-urls: %d URLs with ssl-bump enabled", len(firewallConfig.AllowURLs))
	}

	return nil
}
