package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpNetworkCodemodLog = logger.New("cli:codemod_mcp_network")

// getMCPNetworkMigrationCodemod creates a codemod for migrating per-server MCP network configuration to top-level network configuration
func getMCPNetworkMigrationCodemod() Codemod {
	return Codemod{
		ID:           "mcp-network-to-top-level-migration",
		Name:         "Migrate MCP network config to top-level",
		Description:  "Moves per-server MCP 'network.allowed' configuration to top-level workflow 'network.allowed'. Per-server network configuration is deprecated.",
		IntroducedIn: "0.6.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if mcp-servers section exists
			mcpServersValue, hasMCPServers := frontmatter["mcp-servers"]
			if !hasMCPServers {
				return content, false, nil
			}

			mcpServersMap, ok := mcpServersValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Collect all network.allowed domains from MCP servers
			var allAllowedDomains []string
			serversWithNetwork := make(map[string]bool)

			for serverName, serverValue := range mcpServersMap {
				serverConfig, ok := serverValue.(map[string]any)
				if !ok {
					continue
				}

				// Check if this server has a network configuration
				networkValue, hasNetwork := serverConfig["network"]
				if !hasNetwork {
					continue
				}

				networkMap, ok := networkValue.(map[string]any)
				if !ok {
					continue
				}

				// Extract allowed domains
				allowedValue, hasAllowed := networkMap["allowed"]
				if !hasAllowed {
					continue
				}

				// Convert allowed to []string
				switch allowed := allowedValue.(type) {
				case []any:
					for _, domain := range allowed {
						if domainStr, ok := domain.(string); ok {
							allAllowedDomains = append(allAllowedDomains, domainStr)
						}
					}
					// Only mark server as having network if it has domains
					if len(allowed) > 0 {
						serversWithNetwork[serverName] = true
					}
				case []string:
					allAllowedDomains = append(allAllowedDomains, allowed...)
					// Only mark server as having network if it has domains
					if len(allowed) > 0 {
						serversWithNetwork[serverName] = true
					}
				}
			}

			// If no servers have network configuration, nothing to do
			if len(serversWithNetwork) == 0 {
				return content, false, nil
			}

			// Remove duplicates from collected domains
			allAllowedDomains = uniqueStrings(allAllowedDomains)

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Remove network fields from all MCP servers
			result := frontmatterLines
			var modified bool
			for serverName := range serversWithNetwork {
				var serverModified bool
				result, serverModified = removeFieldFromMCPServer(result, serverName, "network")
				if serverModified {
					modified = true
					mcpNetworkCodemodLog.Printf("Removed network configuration from MCP server '%s'", serverName)
				}
			}

			if !modified {
				return content, false, nil
			}

			// Check if top-level network configuration already exists
			existingNetworkValue, hasTopLevelNetwork := frontmatter["network"]
			var existingAllowed []string

			if hasTopLevelNetwork {
				if existingNetworkMap, ok := existingNetworkValue.(map[string]any); ok {
					if existingAllowedValue, hasExistingAllowed := existingNetworkMap["allowed"]; hasExistingAllowed {
						switch allowed := existingAllowedValue.(type) {
						case []any:
							for _, domain := range allowed {
								if domainStr, ok := domain.(string); ok {
									existingAllowed = append(existingAllowed, domainStr)
								}
							}
						case []string:
							existingAllowed = append(existingAllowed, allowed...)
						}
					}
				}
			}

			// Merge existing and new domains, remove duplicates
			mergedDomains := append(existingAllowed, allAllowedDomains...)
			mergedDomains = uniqueStrings(mergedDomains)

			// Add or update top-level network configuration
			if hasTopLevelNetwork {
				// Update existing network.allowed
				result = updateNetworkAllowed(result, mergedDomains)
				mcpNetworkCodemodLog.Printf("Updated top-level network.allowed with %d domains", len(mergedDomains))
			} else {
				// Add new top-level network configuration
				result = addTopLevelNetwork(result, mergedDomains)
				mcpNetworkCodemodLog.Printf("Added top-level network.allowed with %d domains", len(mergedDomains))
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			mcpNetworkCodemodLog.Print("Applied MCP network migration to top-level")
			return newContent, true, nil
		},
	}
}

// removeFieldFromMCPServer removes a field from a specific MCP server configuration
func removeFieldFromMCPServer(lines []string, serverName string, fieldName string) ([]string, bool) {
	var result []string
	var modified bool
	var inMCPServers bool
	var mcpServersIndent string
	var inServerBlock bool
	var serverIndent string
	var inFieldBlock bool
	var fieldIndent string

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Track if we're in mcp-servers block
		if strings.HasPrefix(trimmedLine, "mcp-servers:") {
			inMCPServers = true
			mcpServersIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left mcp-servers block
		if inMCPServers && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			if hasExitedBlock(line, mcpServersIndent) {
				inMCPServers = false
				inServerBlock = false
			}
		}

		// Track if we're in the specific server block
		if inMCPServers && strings.HasPrefix(trimmedLine, serverName+":") {
			inServerBlock = true
			serverIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left the server block
		if inServerBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			currentIndent := getIndentation(line)
			// Exit if we're back at mcp-servers level or less
			if len(currentIndent) <= len(serverIndent) && strings.Contains(line, ":") {
				inServerBlock = false
			}
		}

		// Remove field line if in server block
		if inServerBlock && strings.HasPrefix(trimmedLine, fieldName+":") {
			modified = true
			inFieldBlock = true
			fieldIndent = getIndentation(line)
			mcpNetworkCodemodLog.Printf("Removed %s from mcp-server '%s' on line %d", fieldName, serverName, i+1)
			continue
		}

		// Skip nested properties under the field
		if inFieldBlock {
			// Empty lines within the field block should be removed
			if len(trimmedLine) == 0 {
				continue
			}

			currentIndent := getIndentation(line)

			// Comments need to check indentation
			if strings.HasPrefix(trimmedLine, "#") {
				if len(currentIndent) > len(fieldIndent) {
					// Comment is nested under field, remove it
					continue
				}
				// Comment is at same or less indentation, exit field block and keep it
				inFieldBlock = false
				result = append(result, line)
				continue
			}

			// If this line has more indentation than field, it's a nested property
			if len(currentIndent) > len(fieldIndent) {
				continue
			}
			// We've exited the field block
			inFieldBlock = false
		}

		result = append(result, line)
	}

	return result, modified
}

// addTopLevelNetwork adds a new top-level network configuration
func addTopLevelNetwork(lines []string, domains []string) []string {
	// Find a good place to insert (after on: field, or at the beginning)
	insertIndex := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "on:") {
			// Insert after the on: block
			insertIndex = i + 1
			// Skip any nested content under on:
			if !strings.Contains(trimmed, "on: ") || strings.HasPrefix(trimmed, "on:") && len(trimmed) == 3 {
				// on: is a block, find the end
				onIndent := getIndentation(line)
				for j := i + 1; j < len(lines); j++ {
					nextLine := lines[j]
					nextTrimmed := strings.TrimSpace(nextLine)
					if len(nextTrimmed) == 0 {
						continue
					}
					if hasExitedBlock(nextLine, onIndent) {
						insertIndex = j
						break
					}
				}
			}
			break
		}
	}

	// Build network configuration lines
	var networkLines []string
	networkLines = append(networkLines, "network:")
	networkLines = append(networkLines, "  allowed:")
	for _, domain := range domains {
		networkLines = append(networkLines, fmt.Sprintf("    - %s", domain))
	}

	// Insert at the determined position
	result := make([]string, 0, len(lines)+len(networkLines))
	result = append(result, lines[:insertIndex]...)
	result = append(result, networkLines...)
	result = append(result, lines[insertIndex:]...)

	return result
}

// updateNetworkAllowed updates the existing top-level network.allowed configuration
func updateNetworkAllowed(lines []string, domains []string) []string {
	var result []string
	var inNetworkBlock bool
	var networkIndent string
	var inAllowedBlock bool
	var allowedIndent string
	var replacedAllowed bool

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Track if we're in network block
		if strings.HasPrefix(trimmedLine, "network:") {
			inNetworkBlock = true
			networkIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left network block
		if inNetworkBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			if hasExitedBlock(line, networkIndent) {
				inNetworkBlock = false
				inAllowedBlock = false
			}
		}

		// Track if we're in allowed block within network
		if inNetworkBlock && strings.HasPrefix(trimmedLine, "allowed:") {
			inAllowedBlock = true
			allowedIndent = getIndentation(line)
			replacedAllowed = true
			// Replace the allowed block
			result = append(result, line)
			for _, domain := range domains {
				result = append(result, fmt.Sprintf("%s  - %s", allowedIndent, domain))
			}
			continue
		}

		// Skip existing allowed array items
		if inAllowedBlock {
			currentIndent := getIndentation(line)

			// Empty lines - skip
			if len(trimmedLine) == 0 {
				continue
			}

			// Comments at deeper indentation - skip
			if strings.HasPrefix(trimmedLine, "#") && len(currentIndent) > len(allowedIndent) {
				continue
			}

			// Array items (lines starting with -)
			if strings.HasPrefix(trimmedLine, "-") && len(currentIndent) > len(allowedIndent) {
				continue
			}

			// We've exited the allowed block
			inAllowedBlock = false
		}

		result = append(result, line)
	}

	// If we didn't find an allowed block, add it to the network block
	if !replacedAllowed {
		// Find the end of the network block and insert allowed
		result = addAllowedToNetwork(result, domains)
	}

	return result
}

// addAllowedToNetwork adds an allowed field to an existing network block
func addAllowedToNetwork(lines []string, domains []string) []string {
	var result []string
	var inNetworkBlock bool
	var networkIndent string
	var insertIndex = -1

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, "network:") {
			inNetworkBlock = true
			networkIndent = getIndentation(line)
		}

		if inNetworkBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			if hasExitedBlock(line, networkIndent) {
				// Found the end of network block
				insertIndex = i
				break
			}
		}

		result = append(result, line)
	}

	if insertIndex > 0 {
		// Insert allowed before the next top-level block
		allowedLines := []string{
			fmt.Sprintf("%s  allowed:", networkIndent),
		}
		for _, domain := range domains {
			allowedLines = append(allowedLines, fmt.Sprintf("%s    - %s", networkIndent, domain))
		}

		result = append(result, allowedLines...)
		result = append(result, lines[insertIndex:]...)
	} else {
		// Append at the end of network block
		networkIndentStr := ""
		for i := len(result) - 1; i >= 0; i-- {
			trimmed := strings.TrimSpace(result[i])
			if strings.HasPrefix(trimmed, "network:") {
				networkIndentStr = getIndentation(result[i])
				break
			}
		}
		result = append(result, fmt.Sprintf("%s  allowed:", networkIndentStr))
		for _, domain := range domains {
			result = append(result, fmt.Sprintf("%s    - %s", networkIndentStr, domain))
		}
	}

	return result
}

// uniqueStrings removes duplicates from a string slice while preserving order
func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
