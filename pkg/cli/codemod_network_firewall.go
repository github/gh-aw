package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var networkFirewallCodemodLog = logger.New("cli:codemod_network_firewall")

// getNetworkFirewallCodemod creates a codemod for migrating network.firewall to sandbox.agent
func getNetworkFirewallCodemod() Codemod {
	return Codemod{
		ID:           "network-firewall-migration",
		Name:         "Migrate network.firewall to sandbox.agent",
		Description:  "Removes deprecated 'network.firewall' field (firewall is now always enabled via sandbox.agent: awf default)",
		IntroducedIn: "0.1.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if network.firewall exists
			networkValue, hasNetwork := frontmatter["network"]
			if !hasNetwork {
				return content, false, nil
			}

			networkMap, ok := networkValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if firewall field exists in network
			firewallValue, hasFirewall := networkMap["firewall"]
			if !hasFirewall {
				return content, false, nil
			}

			// Note: We no longer set sandbox.agent: false since the firewall is mandatory
			// The firewall is always enabled via the default sandbox.agent: awf

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Remove the firewall field from the network block
			result, modified := removeFieldFromBlock(frontmatterLines, "firewall", "network")
			if !modified {
				return content, false, nil
			}

			// Add sandbox.agent if not already present AND if firewall was explicitly true
			// (no need to add sandbox.agent: awf if firewall was false, since awf is now the default)
			_, hasSandbox := frontmatter["sandbox"]
			if !hasSandbox && firewallValue == true {
				// Only add sandbox.agent: awf if firewall was explicitly set to true
				sandboxLines := []string{
					"sandbox:",
					"  agent: awf  # Firewall enabled (migrated from network.firewall)",
				}

				// Try to place it after network block
				insertIndex := -1
				inNet := false
				for i, line := range result {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "network:") {
						inNet = true
					} else if inNet && len(trimmed) > 0 {
						// Check if this is a top-level key (no leading whitespace)
						if isTopLevelKey(line) {
							// Found next top-level key
							insertIndex = i
							break
						}
					}
				}

				if insertIndex >= 0 {
					// Insert after network block
					newLines := make([]string, 0, len(result)+len(sandboxLines))
					newLines = append(newLines, result[:insertIndex]...)
					newLines = append(newLines, sandboxLines...)
					newLines = append(newLines, result[insertIndex:]...)
					result = newLines
				} else {
					// Append at the end
					result = append(result, sandboxLines...)
				}

				networkFirewallCodemodLog.Print("Added sandbox.agent: awf (firewall was explicitly enabled)")
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			networkFirewallCodemodLog.Printf("Applied network.firewall removal (firewall: %v removed, firewall now always enabled via sandbox.agent: awf default)", firewallValue)
			return newContent, true, nil
		},
	}
}
