package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
)

// listAvailableServers shows a list of available MCP servers from the registry
func listAvailableServers(registryURL string, verbose bool) error {
	// Create registry client
	registryClient := NewMCPRegistryClient(registryURL)

	// Search for all servers (empty query)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fetching available MCP servers from registry: %s", registryClient.registryURL)))
	}

	servers, err := registryClient.SearchServers("")
	if err != nil {
		return fmt.Errorf("failed to fetch MCP servers: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Retrieved %d servers from registry", len(servers))))
		if len(servers) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("First server example - Name: '%s', Description: '%s'",
				servers[0].Name, servers[0].Description)))
		}
	}

	if len(servers) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No MCP servers found in the registry"))
		return nil
	}

	// Prepare table data
	headers := []string{"Name", "Description"}
	rows := make([][]string, 0, len(servers))

	for _, server := range servers {
		// Use server name as the primary identifier
		name := server.Name
		if name == "" {
			name = "<unnamed>" // fallback if no name
		}

		// Truncate long descriptions for table display
		description := server.Description
		if len(description) > 80 {
			description = description[:77] + "..."
		}
		if description == "" {
			description = "-"
		}

		rows = append(rows, []string{
			name,
			description,
		})
	}

	// Create and render table
	tableConfig := console.TableConfig{
		Title:     fmt.Sprintf("MCP registry: %s", registryClient.registryURL),
		Headers:   headers,
		Rows:      rows,
		ShowTotal: true,
		TotalRow:  []string{fmt.Sprintf("Total: %d servers", len(servers)), ""},
	}

	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Usage: gh aw mcp add <workflow-file> <server-name>"))

	return nil
}
